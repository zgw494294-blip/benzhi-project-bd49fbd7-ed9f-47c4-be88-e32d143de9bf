package domain

import (
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	b := NewBatch("s", "w", "u", SegmentProfile{StartMarker: "A", EndMarker: "B", Material: "PE", VolumeM3: 10, TargetChlorineMin: .2, TargetChlorineMax: 1})
	if err := b.Freeze(Plan{FlowRateM3H: 1, DurationMin: 1, DisinfectantTarget: .5, SamplingPoints: []string{"p"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.StartExecution(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := b.AddRound(ExecutionRound{Sequence: 1, FlowRateM3H: 1, StartedAt: now.Add(-time.Minute), EndedAt: now, DurationMin: 1, Result: "pass"}); err != nil {
		t.Fatal(err)
	}
	if err := b.FinishExecution(); err != nil {
		t.Fatal(err)
	}
}
