package domain

import (
	"fmt"
	"math"
	"strings"
)

func (p Plan) Validate() []ValidationIssue {
	issues := []ValidationIssue{}
	if math.IsNaN(p.FlowRateM3H) || math.IsInf(p.FlowRateM3H, 0) {
		issues = append(issues, ValidationIssue{"flowRateM3h", "流量必须为有限数值"})
	} else if p.FlowRateM3H <= 0 {
		issues = append(issues, ValidationIssue{"flowRateM3h", "流量必须大于零"})
	}
	if math.IsNaN(p.DurationMin) || math.IsInf(p.DurationMin, 0) {
		issues = append(issues, ValidationIssue{"durationMin", "时长必须为有限数值"})
	} else if p.DurationMin <= 0 {
		issues = append(issues, ValidationIssue{"durationMin", "时长必须大于零"})
	}
	if math.IsNaN(p.DisinfectantTarget) || math.IsInf(p.DisinfectantTarget, 0) {
		issues = append(issues, ValidationIssue{"disinfectantTarget", "消毒剂目标浓度必须为有限数值"})
	} else if p.DisinfectantTarget <= 0 {
		issues = append(issues, ValidationIssue{"disinfectantTarget", "消毒剂目标浓度必须大于零"})
	}
	seen := map[string]bool{}
	for i, v := range p.SamplingPoints {
		v = strings.TrimSpace(v)
		if v == "" {
			issues = append(issues, ValidationIssue{fmt.Sprintf("samplingPoints[%d]", i), "采样点不能为空"})
			continue
		}
		if seen[v] {
			issues = append(issues, ValidationIssue{"samplingPoints", "规范化后的采样点不能重复"})
		}
		seen[v] = true
	}
	if len(p.SamplingPoints) == 0 {
		issues = append(issues, ValidationIssue{"samplingPoints", "至少覆盖一个采样点"})
	}
	return issues
}
