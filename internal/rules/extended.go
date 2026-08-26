package rules

import (
	"aquaflush-release-workbench/internal/domain"
	"fmt"
	"math"
	"sort"
	"strings"
)

const GeometryTolerancePercent = 10.0

func rounded(v float64) float64 { return math.Round(v*1_000_000) / 1_000_000 }

func CheckGeometry(p *domain.SegmentProfile) *domain.GeometryVolumeCheck {
	if p == nil || p.DiameterMM <= 0 || p.LengthM <= 0 {
		return nil
	}
	radiusM := p.DiameterMM / 2000
	theoretical := math.Pi * radiusM * radiusM * p.LengthM
	diff := math.Abs(p.VolumeM3 - theoretical)
	deviation := 0.0
	if theoretical > 0 {
		deviation = diff / theoretical * 100
	}
	return &domain.GeometryVolumeCheck{
		Complete: true, TheoreticalVolumeM3: rounded(theoretical), DeclaredVolumeM3: rounded(p.VolumeM3),
		AbsoluteDifferenceM3: rounded(diff), DeviationPercent: rounded(deviation),
		TolerancePercent: GeometryTolerancePercent, WithinTolerance: deviation <= GeometryTolerancePercent,
	}
}

type PlanPrecheck struct {
	BatchID                    string                   `json:"batchId"`
	ExpectedVersion            int                      `json:"expectedVersion"`
	NormalizedPlan             domain.Plan              `json:"normalizedPlan"`
	EstimatedExecutionVolumeM3 float64                  `json:"estimatedExecutionVolumeM3"`
	ExchangeRatio              float64                  `json:"exchangeRatio"`
	ChlorineMarginLowerMgL     float64                  `json:"chlorineMarginLowerMgL"`
	ChlorineMarginUpperMgL     float64                  `json:"chlorineMarginUpperMgL"`
	SamplingPoints             []string                 `json:"samplingPoints"`
	Warnings                   []domain.ValidationIssue `json:"warnings"`
	ConfirmationSummary        string                   `json:"confirmationSummary,omitempty"`
}

func PrecheckPlan(batchID string, expected int, profile *domain.SegmentProfile, input domain.Plan) (PlanPrecheck, []domain.ValidationIssue) {
	plan, issues := PreparePlan(profile, input)
	plan.GeometryCheck = CheckGeometry(profile)
	result := PlanPrecheck{BatchID: batchID, ExpectedVersion: expected, NormalizedPlan: plan, SamplingPoints: append([]string(nil), plan.SamplingPoints...), Warnings: []domain.ValidationIssue{}}
	result.EstimatedExecutionVolumeM3 = rounded(plan.FlowRateM3H * plan.DurationMin / 60)
	if profile != nil && profile.VolumeM3 > 0 {
		result.ExchangeRatio = rounded(result.EstimatedExecutionVolumeM3 / profile.VolumeM3)
		result.ChlorineMarginLowerMgL = rounded(plan.DisinfectantTarget - profile.TargetChlorineMin)
		result.ChlorineMarginUpperMgL = rounded(profile.TargetChlorineMax - plan.DisinfectantTarget)
		span := profile.TargetChlorineMax - profile.TargetChlorineMin
		if span > 0 && math.Min(result.ChlorineMarginLowerMgL, result.ChlorineMarginUpperMgL) <= span*.1 {
			result.Warnings = append(result.Warnings, domain.ValidationIssue{Field: "disinfectantTarget", Message: "目标余氯接近允许范围边界，请负责人确认"})
		}
	}
	return result, issues
}

func EvaluateSampleChecks(s domain.WaterSample, p *domain.SegmentProfile) []domain.SampleCheck {
	if p == nil {
		return []domain.SampleCheck{}
	}
	checks := []domain.SampleCheck{
		check("turbidity", s.TurbidityNTU, "≤ 1 NTU", s.TurbidityNTU <= 1, "浊度符合限值", "浊度超过 1 NTU"),
		check("chlorineMin", s.ChlorineMgL, fmt.Sprintf("≥ %.3f mg/L", p.TargetChlorineMin), s.ChlorineMgL >= p.TargetChlorineMin, "余氯不低于下限", "余氯低于目标下限"),
		check("chlorineMax", s.ChlorineMgL, fmt.Sprintf("≤ %.3f mg/L", p.TargetChlorineMax), s.ChlorineMgL <= p.TargetChlorineMax, "余氯不高于上限", "余氯高于目标上限"),
		check("colony", s.ColonyCFUML, "≤ 100 CFU/mL", s.ColonyCFUML <= 100, "菌落符合限值", "菌落超过 100 CFU/mL"),
	}
	return checks
}

func check(parameter string, value float64, limit string, pass bool, okReason, failReason string) domain.SampleCheck {
	verdict, reason := "fail", failReason
	if pass {
		verdict, reason = "pass", okReason
	}
	return domain.SampleCheck{Parameter: parameter, Value: rounded(value), Limit: limit, Verdict: verdict, Reason: reason}
}

func VerdictFromChecks(checks []domain.SampleCheck) string {
	if len(checks) == 0 {
		return "invalid"
	}
	for _, item := range checks {
		if item.Verdict != "pass" {
			return "fail"
		}
	}
	return "pass"
}

func ExecutionProgress(b *domain.Batch) domain.ExecutionProgress {
	p := domain.ExecutionProgress{NextSequence: len(b.Rounds) + 1, ByType: map[string]domain.RoundAggregate{"flush": {}, "disinfection": {}}, FinishBlockers: []string{}}
	if b.Plan != nil {
		p.RequiredVolumeM3 = rounded(b.Plan.Summary.MinimumExecutionVolumeM3)
	}
	for _, r := range b.Rounds {
		duration, volume := r.EndedAt.Sub(r.StartedAt).Minutes(), r.FlowRateM3H*r.EndedAt.Sub(r.StartedAt).Hours()
		p.TotalVolumeM3 += volume
		kind := strings.TrimSpace(r.RoundType)
		if kind == "" {
			kind = "unspecified"
		}
		a := p.ByType[kind]
		a.RoundCount++
		a.VolumeM3 += volume
		a.DurationMin += duration
		a.VolumeM3 = rounded(a.VolumeM3)
		a.DurationMin = rounded(a.DurationMin)
		p.ByType[kind] = a
		if r.Result == "pass" {
			p.EffectiveVolumeM3 += volume
			p.EffectiveDurationMin += duration
		} else {
			p.FinishBlockers = append(p.FinishBlockers, fmt.Sprintf("第 %d 轮单项未达标", r.Sequence))
		}
	}
	p.TotalVolumeM3, p.EffectiveVolumeM3, p.EffectiveDurationMin = rounded(p.TotalVolumeM3), rounded(p.EffectiveVolumeM3), rounded(p.EffectiveDurationMin)
	if b.Profile != nil && b.Profile.VolumeM3 > 0 {
		p.TotalExchangeRatio = rounded(p.TotalVolumeM3 / b.Profile.VolumeM3)
	}
	if p.RequiredVolumeM3 > 0 {
		p.CompletionPercent = rounded(p.EffectiveVolumeM3 / p.RequiredVolumeM3 * 100)
	}
	if len(b.Rounds) == 0 {
		p.FinishBlockers = append(p.FinishBlockers, "尚无现场轮次")
	}
	if p.EffectiveVolumeM3+1e-9 < p.RequiredVolumeM3 {
		p.FinishBlockers = append(p.FinishBlockers, fmt.Sprintf("累计有效水量 %.3f m³ 低于冻结基线 %.3f m³", p.EffectiveVolumeM3, p.RequiredVolumeM3))
	}
	for i, r := range b.Rounds {
		if b.Plan != nil && r.StartedAt.Before(b.Plan.FrozenAt) {
			p.FinishBlockers = append(p.FinishBlockers, fmt.Sprintf("第 %d 轮早于方案冻结时间", r.Sequence))
		}
		if i > 0 && r.StartedAt.Before(b.Rounds[i-1].EndedAt) {
			p.FinishBlockers = append(p.FinishBlockers, fmt.Sprintf("第 %d 轮与前一轮时间重叠", r.Sequence))
		}
	}
	return p
}

func FailedParameters(s domain.WaterSample) []string {
	out := []string{}
	for _, c := range s.Checks {
		if c.Verdict == "fail" {
			out = append(out, c.Parameter)
		}
	}
	sort.Strings(out)
	return out
}
