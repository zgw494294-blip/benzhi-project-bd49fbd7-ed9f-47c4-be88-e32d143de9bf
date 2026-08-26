package rules

import (
	"aquaflush-release-workbench/internal/domain"
	"fmt"
	"strings"
)

func EvaluateSample(s domain.WaterSample, p *domain.SegmentProfile) string {
	return VerdictFromChecks(EvaluateSampleChecks(s, p))
}

func AssessReleaseDetail(b *domain.Batch) ReleaseAssessment {
	blockers := []string{}
	if b == nil || b.Plan == nil {
		return ReleaseAssessment{Message: "方案未冻结", Blockers: []string{"方案未冻结"}}
	}
	if len(b.Rounds) == 0 {
		blockers = append(blockers, "尚无现场轮次")
	}
	for _, r := range b.Rounds {
		if r.Result != "pass" {
			blockers = append(blockers, fmt.Sprintf("第 %d 轮未达标: %s", r.Sequence, r.ResultReason))
		}
	}
	progress := ExecutionProgress(b)
	for _, blocker := range progress.FinishBlockers {
		blockers = append(blockers, "执行进度: "+blocker)
	}
	current := map[string]domain.WaterSample{}
	for _, s := range b.Samples {
		if s.Current {
			current[s.SamplingPoint] = s
		}
	}
	for _, point := range b.Plan.SamplingPoints {
		sample, exists := current[point]
		if !exists {
			blockers = append(blockers, fmt.Sprintf("采样点 %s 尚无当前样本", point))
			continue
		}
		if sample.Verdict != "pass" {
			blockers = append(blockers, fmt.Sprintf("采样点 %s 当前样本未达标", point))
		}
	}
	for _, a := range b.Actions {
		if a.Status != "closed" {
			blockers = append(blockers, fmt.Sprintf("整改项 %s 尚未闭环", a.ID))
		}
	}
	if len(blockers) > 0 {
		return ReleaseAssessment{Message: strings.Join(blockers, "；"), Blockers: blockers}
	}
	return ReleaseAssessment{Eligible: true, Message: "满足放行条件", Blockers: []string{}}
}

func AssessRelease(b *domain.Batch) (bool, string) {
	a := AssessReleaseDetail(b)
	return a.Eligible, a.Message
}
