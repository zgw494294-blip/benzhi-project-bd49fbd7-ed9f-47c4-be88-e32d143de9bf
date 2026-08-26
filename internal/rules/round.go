package rules

import (
	"aquaflush-release-workbench/internal/domain"
	"fmt"
	"strings"
)

func EvaluateRound(p *domain.SegmentProfile, plan *domain.Plan, r domain.ExecutionRound) RoundResult {
	if p == nil || plan == nil || p.VolumeM3 <= 0 {
		return RoundResult{Message: "缺少冻结方案", FailureReasons: []string{"缺少冻结方案"}}
	}
	duration := r.EndedAt.Sub(r.StartedAt).Minutes()
	exchange := r.FlowRateM3H * duration / 60 / p.VolumeM3
	deviation := r.ChlorineMgL - plan.DisinfectantTarget
	reasons := []string{}
	if r.FlowRateM3H < plan.FlowRateM3H {
		reasons = append(reasons, fmt.Sprintf("流量 %.2f m³/h 低于基线 %.2f m³/h", r.FlowRateM3H, plan.FlowRateM3H))
	}
	if duration < plan.DurationMin {
		reasons = append(reasons, fmt.Sprintf("持续时间 %.2f 分钟低于基线 %.2f 分钟", duration, plan.DurationMin))
	}
	if r.ChlorineMgL < p.TargetChlorineMin || r.ChlorineMgL > p.TargetChlorineMax {
		reasons = append(reasons, fmt.Sprintf("余氯 %.3f mg/L 超出目标范围 %.3f-%.3f mg/L", r.ChlorineMgL, p.TargetChlorineMin, p.TargetChlorineMax))
	}
	if exchange < plan.Summary.EstimatedExchangeRatio {
		reasons = append(reasons, fmt.Sprintf("换水倍数 %.3f 低于基线 %.3f", exchange, plan.Summary.EstimatedExchangeRatio))
	}
	pass := len(reasons) == 0
	message := "轮次满足流量、时长、换水量及余氯边界"
	if !pass {
		message = strings.Join(reasons, "；")
	}
	return RoundResult{ExchangeRatio: exchange, DurationMin: duration, ChlorineDeviation: deviation, Pass: pass, Message: message, FailureReasons: reasons}
}
