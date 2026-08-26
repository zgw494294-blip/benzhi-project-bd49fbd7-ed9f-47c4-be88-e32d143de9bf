package rules

import (
	"aquaflush-release-workbench/internal/domain"
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	p := &domain.SegmentProfile{VolumeM3: 10, TargetChlorineMin: .3, TargetChlorineMax: 1}
	plan, issues := PreparePlan(p, domain.Plan{FlowRateM3H: 20, DurationMin: 30, DisinfectantTarget: .6, SamplingPoints: []string{"P1"}})
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	end := time.Now().UTC()
	if !EvaluateRound(p, &plan, domain.ExecutionRound{FlowRateM3H: 20, StartedAt: end.Add(-30 * time.Minute), EndedAt: end, ChlorineMgL: .6}).Pass {
		t.Fatal("expected pass")
	}
}
