package cancelleddraftcachepollution_test

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type cancelAfterFirstCheck struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func newCancelAfterFirstCheck() *cancelAfterFirstCheck {
	ctx, cancel := context.WithCancel(context.Background())
	return &cancelAfterFirstCheck{Context: ctx, cancel: cancel}
}

func (c *cancelAfterFirstCheck) Err() error {
	err := c.Context.Err()
	if c.checks.Add(1) == 1 {
		c.cancel()
	}
	return err
}

func TestCancelledDraftMutationDoesNotPolluteServiceCache(t *testing.T) {
	repository, err := store.New(filepath.Join(t.TempDir(), "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repository)
	created, err := service.CreateBatch(context.Background(), "SEG-CACHE", "北区水厂", "创建人", domain.SegmentProfile{
		StartMarker: "A", EndMarker: "B", Material: "球墨铸铁", VolumeM3: 12,
		TargetChlorineMin: 0.3, TargetChlorineMax: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = service.Get(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	updatedProfile := *created.Profile
	updatedProfile.VolumeM3 = 18
	_, err = service.SaveDraft(newCancelAfterFirstCheck(), created.ID, created.Version, "SEG-CACHE", "南区水厂", updatedProfile, "修改人", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("受控取消应阻止仓储提交，实际错误: %v", err)
	}

	durable, err := repository.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durable.WaterSource != "北区水厂" || durable.Profile.VolumeM3 != 12 || durable.Version != created.Version {
		t.Fatalf("仓储不应提交已取消更新: %#v", durable)
	}
	observed, err := service.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observed.WaterSource != durable.WaterSource || observed.Profile.VolumeM3 != durable.Profile.VolumeM3 || observed.Version != durable.Version {
		t.Fatalf("取消后的未提交草稿污染了后续读取: cache=(source=%q volume=%.1f version=%d), durable=(source=%q volume=%.1f version=%d)",
			observed.WaterSource, observed.Profile.VolumeM3, observed.Version,
			durable.WaterSource, durable.Profile.VolumeM3, durable.Version)
	}
}
