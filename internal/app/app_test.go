package app

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testService(t *testing.T) *Service {
	t.Helper()
	repository, err := store.New(filepath.Join(t.TempDir(), "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	return New(repository)
}

func testDraft(t *testing.T, service *Service) *domain.Batch {
	t.Helper()
	b, err := service.CreateBatchIdempotent(context.Background(), "SEG-01", "北区水厂", "负责人", domain.SegmentProfile{
		StartMarker: "A", EndMarker: "B", Material: "球墨铸铁", VolumeM3: 10,
		TargetChlorineMin: .3, TargetChlorineMax: 1,
	}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func freezeTestBatch(t *testing.T, service *Service, b *domain.Batch) *domain.Batch {
	t.Helper()
	b, err := service.FreezeWithKey(context.Background(), b.ID, b.Version, domain.Plan{
		FlowRateM3H: 600000, DurationMin: .001, DisinfectantTarget: .6, SamplingPoints: []string{" P1 "},
	}, "负责人", "freeze-1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Plan.Version != 1 || b.Plan.Summary.EstimatedExchangeRatio != 1 || b.Plan.SamplingPoints[0] != "P1" {
		t.Fatalf("冻结快照不完整: %#v", b.Plan)
	}
	return b
}

func validRoundTimes(b *domain.Batch) (time.Time, time.Time) {
	started := b.Plan.FrozenAt.Add(time.Millisecond)
	return started, started.Add(60 * time.Millisecond)
}

func TestRoundIdempotencyAndFinishGate(t *testing.T) {
	service := testService(t)
	b := freezeTestBatch(t, service, testDraft(t, service))
	started, ended := validRoundTimes(b)
	round := domain.ExecutionRound{Sequence: 1, RoundType: "flush", StartedAt: started, EndedAt: ended, FlowRateM3H: 600000, ChlorineMgL: .6}
	first, err := service.RoundWithKey(context.Background(), b.ID, b.Version, round, "操作员", "round-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.RoundWithKey(context.Background(), b.ID, b.Version, round, "操作员", "round-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != second.Version || first.Rounds[0].ID != second.Rounds[0].ID {
		t.Fatal("相同幂等请求未返回首次结果")
	}
	changed := round
	changed.ChlorineMgL = .1
	if _, err = service.RoundWithKey(context.Background(), b.ID, b.Version, changed, "操作员", "round-1"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("占用同一幂等键的不同请求应冲突: %v", err)
	}
	events, _ := service.Timeline(context.Background(), b.ID)
	if len(events) != 3 {
		t.Fatalf("幂等重放追加了审计事件: %d", len(events))
	}
}

func TestSamplingReviewReleaseAndVerification(t *testing.T) {
	ctx := context.Background()
	service := testService(t)
	b := freezeTestBatch(t, service, testDraft(t, service))
	started, ended := validRoundTimes(b)
	b, err := service.RoundWithKey(ctx, b.ID, b.Version, domain.ExecutionRound{Sequence: 1, StartedAt: started, EndedAt: ended, FlowRateM3H: 600000, ChlorineMgL: .6}, "操作员", "round-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err = service.Finish(ctx, b.ID, b.Version, "操作员")
	if err != nil {
		t.Fatal(err)
	}
	b, err = service.Sample(ctx, b.ID, b.Version, domain.WaterSample{SamplingPoint: "P1", Witness: "见证人", SampledAt: b.ExecutionFinishedAt.Add(time.Minute), TurbidityNTU: .2, ChlorineMgL: .6, ColonyCFUML: 2}, "检测员")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domain.StatusReview {
		t.Fatalf("完整达标样本应进入复核: %s", b.Status)
	}
	if _, err = service.Sample(ctx, b.ID, b.Version, domain.WaterSample{SamplingPoint: "P1", Witness: "见证人", SampledAt: b.ExecutionFinishedAt.Add(2 * time.Minute)}, "检测员"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("复核状态不应接受直接重复样本: %v", err)
	}
	b, err = service.ReviewDecision(ctx, b.ID, b.Version, "复核员", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := service.Release(ctx, b.ID, b.Version, "复核员")
	if err != nil {
		t.Fatal(err)
	}
	verification, err := service.VerifyCertificate(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Passed || verification.AuditSequence != certificate.AuditSequence {
		t.Fatalf("凭据核验失败: %#v", verification)
	}
	if _, err = service.Release(ctx, b.ID, certificate.BatchVersion, "复核员"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("重复签发应返回状态错误: %v", err)
	}
}

func TestCorrectiveReinspectionRetainsSource(t *testing.T) {
	ctx := context.Background()
	service := testService(t)
	b := freezeTestBatch(t, service, testDraft(t, service))
	started, ended := validRoundTimes(b)
	b, _ = service.RoundWithKey(ctx, b.ID, b.Version, domain.ExecutionRound{Sequence: 1, StartedAt: started, EndedAt: ended, FlowRateM3H: 600000, ChlorineMgL: .6}, "操作员", "round-1")
	b, _ = service.Finish(ctx, b.ID, b.Version, "操作员")
	b, _ = service.Sample(ctx, b.ID, b.Version, domain.WaterSample{SamplingPoint: "P1", Witness: "见证人", SampledAt: b.ExecutionFinishedAt.Add(time.Minute), TurbidityNTU: 2, ChlorineMgL: .6, ColonyCFUML: 2}, "检测员")
	sourceID := b.Samples[0].ID
	b, err := service.Correct(ctx, b.ID, b.Version, domain.CorrectiveAction{SourceSampleID: sourceID, Reason: "浊度偏高", Measure: "继续冲洗", AffectedPoints: []string{"P1"}}, "负责人")
	if err != nil {
		t.Fatal(err)
	}
	actionID := b.Actions[0].ID
	b, err = service.Reinspect(ctx, b.ID, b.Version, actionID, domain.WaterSample{SamplingPoint: "P1", Witness: "见证人", SampledAt: b.ExecutionFinishedAt.Add(2 * time.Minute), TurbidityNTU: .2, ChlorineMgL: .6, ColonyCFUML: 2}, "检测员")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Samples) != 2 || b.Samples[1].SupersedesSampleID != sourceID || b.Actions[0].Status != "closed" || b.Status != domain.StatusReview {
		t.Fatalf("整改复检链不完整: %#v %#v", b.Samples, b.Actions)
	}
}
