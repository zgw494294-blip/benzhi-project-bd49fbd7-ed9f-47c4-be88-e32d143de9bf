package rules

import (
	"aquaflush-release-workbench/internal/domain"
	"math"
	"strings"
)

func PreparePlan(profile *domain.SegmentProfile, plan domain.Plan) (domain.Plan, []domain.ValidationIssue) {
	issues := plan.Validate()
	if profile == nil {
		return plan, append(issues, domain.ValidationIssue{Field: "profile", Message: "缺少管段参数"})
	}
	for i := range plan.SamplingPoints {
		plan.SamplingPoints[i] = strings.TrimSpace(plan.SamplingPoints[i])
	}
	if plan.DisinfectantTarget < profile.TargetChlorineMin || plan.DisinfectantTarget > profile.TargetChlorineMax {
		issues = append(issues, domain.ValidationIssue{Field: "disinfectantTarget", Message: "消毒剂目标浓度必须位于管段目标余氯范围内"})
	}
	volume := plan.FlowRateM3H * plan.DurationMin / 60
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		issues = append(issues, domain.ValidationIssue{Field: "flowRateM3h", Message: "流量与时长乘积超出可表示范围，请使用合理的数值"})
		return plan, issues
	}
	if plan.FlowRateM3H > 0 && plan.DurationMin > 0 && profile.VolumeM3 > 0 && volume < profile.VolumeM3 {
		issues = append(issues, domain.ValidationIssue{Field: "flowRateM3h", Message: "计划流量与时长不足以完成一次换水"})
	}
	if profile.VolumeM3 > 0 {
		plan.Summary.EstimatedExchangeRatio = volume / profile.VolumeM3
	}
	plan.Summary.MinimumExecutionVolumeM3 = volume
	left := plan.DisinfectantTarget - profile.TargetChlorineMin
	right := profile.TargetChlorineMax - plan.DisinfectantTarget
	if left >= 0 && right >= 0 {
		plan.Summary.ChlorineDeviationThresholdMgL = math.Min(left, right)
	}
	return plan, issues
}
