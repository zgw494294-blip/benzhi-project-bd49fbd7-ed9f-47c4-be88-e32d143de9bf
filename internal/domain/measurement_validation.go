package domain

import "strings"

func (r ExecutionRound) Validate() []ValidationIssue {
	issues := []ValidationIssue{}
	if r.Sequence < 1 {
		issues = append(issues, ValidationIssue{"sequence", "轮次序号必须为正数"})
	}
	if r.StartedAt.IsZero() {
		issues = append(issues, ValidationIssue{"startedAt", "开始时间不能为空"})
	}
	if r.EndedAt.IsZero() {
		issues = append(issues, ValidationIssue{"endedAt", "结束时间不能为空"})
	}
	if !r.StartedAt.IsZero() && !r.EndedAt.IsZero() && !r.EndedAt.After(r.StartedAt) {
		issues = append(issues, ValidationIssue{"endedAt", "结束时间必须晚于开始时间"})
	}
	if r.FlowRateM3H <= 0 {
		issues = append(issues, ValidationIssue{"flowRateM3h", "轮次流量必须大于零"})
	}
	if r.ChlorineMgL < 0 {
		issues = append(issues, ValidationIssue{"chlorineMgL", "轮次余氯不能为负"})
	}
	return issues
}

func (s WaterSample) Validate() []ValidationIssue {
	issues := []ValidationIssue{}
	if strings.TrimSpace(s.SamplingPoint) == "" {
		issues = append(issues, ValidationIssue{"samplingPoint", "采样点不能为空"})
	}
	if strings.TrimSpace(s.Witness) == "" {
		issues = append(issues, ValidationIssue{"witness", "见证人不能为空"})
	}
	if s.SampledAt.IsZero() {
		issues = append(issues, ValidationIssue{"sampledAt", "采样时间不能为空"})
	}
	if s.TurbidityNTU < 0 || s.TurbidityNTU > 1000 {
		issues = append(issues, ValidationIssue{"turbidityNtu", "浊度读数超出有效范围 0-1000 NTU"})
	}
	if s.ChlorineMgL < 0 || s.ChlorineMgL > 20 {
		issues = append(issues, ValidationIssue{"chlorineMgL", "余氯读数超出有效范围 0-20 mg/L"})
	}
	if s.ColonyCFUML < 0 || s.ColonyCFUML > 1000000000 {
		issues = append(issues, ValidationIssue{"colonyCfuMl", "菌落读数超出有效范围"})
	}
	return issues
}
