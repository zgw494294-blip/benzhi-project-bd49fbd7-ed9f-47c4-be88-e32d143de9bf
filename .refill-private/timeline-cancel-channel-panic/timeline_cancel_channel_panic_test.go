package timeline_cancel_channel_panic_test

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"errors"
	"sync"
	"testing"
)

type cancelAfterFirstEventContext struct {
	context.Context
	mu     sync.Mutex
	checks int
	done   chan struct{}
	once   sync.Once
}

func newCancelAfterFirstEventContext() *cancelAfterFirstEventContext {
	return &cancelAfterFirstEventContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *cancelAfterFirstEventContext) Done() <-chan struct{} { return c.done }

func (c *cancelAfterFirstEventContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks++
	if c.checks < 2 {
		return nil
	}
	c.once.Do(func() { close(c.done) })
	return context.Canceled
}

func TestTimelineCancellationDoesNotPanicEventProducer(t *testing.T) {
	dbPath := t.TempDir() + "/store.json"
	repository, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repository)
	batch, err := service.CreateBatchIdempotent(context.Background(), "SEG-CANCEL", "北区水厂", "负责人", domain.SegmentProfile{
		StartMarker: "K0", EndMarker: "K1", Material: "球墨铸铁", DiameterMM: 100,
		LengthM: 100, VolumeM3: 0.785398, TargetChlorineMin: 0.3, TargetChlorineMax: 0.8,
	}, "create-cancel-timeline")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveDraft(context.Background(), batch.ID, batch.Version, "SEG-CANCEL-UPDATED", "北区水厂", *batch.Profile, "负责人", "update-cancel-timeline")
	if err != nil {
		t.Fatal(err)
	}

	ctx := newCancelAfterFirstEventContext()
	if _, err = service.Timeline(ctx, batch.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后的时间线读取应返回 context.Canceled，实际为 %v", err)
	}
	events, err := service.Timeline(context.Background(), batch.ID)
	if err != nil {
		t.Fatalf("取消读取不应损坏后续时间线查询: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("后续时间线应保留 2 条完整事件，实际为 %d", len(events))
	}
}
