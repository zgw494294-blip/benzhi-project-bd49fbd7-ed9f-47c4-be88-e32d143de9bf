package staletimelinecache_test

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"path/filepath"
	"testing"
)

func TestTimelineCacheInvalidatesAfterSuccessfulDraftUpdate(t *testing.T) {
	ctx := context.Background()
	repository, err := store.New(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatalf("打开仓储失败: %v", err)
	}
	service := app.New(repository)
	profile := domain.SegmentProfile{
		StartMarker:       "A",
		EndMarker:         "B",
		Material:          "球墨铸铁",
		DiameterMM:        100,
		LengthM:           100,
		VolumeM3:          1,
		TargetChlorineMin: 0.3,
		TargetChlorineMax: 1.0,
	}
	created, err := service.CreateBatchIdempotent(ctx, "SEG-OLD", "北区水厂", "创建员", profile, "create-key")
	if err != nil {
		t.Fatalf("创建草稿失败: %v", err)
	}
	before, err := service.TimelineView(ctx, created.ID)
	if err != nil {
		t.Fatalf("首次读取时间线失败: %v", err)
	}
	if before.Summary.Version != created.Version || len(before.Events) != 1 {
		t.Fatalf("首次时间线基线异常: version=%d events=%d", before.Summary.Version, len(before.Events))
	}

	updated, err := service.SaveDraft(ctx, created.ID, created.Version, "SEG-NEW", "南区水厂", *created.Profile, "更新员", "update-key")
	if err != nil {
		t.Fatalf("更新草稿失败: %v", err)
	}
	persisted, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("读取已提交草稿失败: %v", err)
	}
	if persisted.Version != updated.Version || persisted.SegmentID != "SEG-NEW" {
		t.Fatalf("草稿更新没有真实提交: version=%d segment=%s", persisted.Version, persisted.SegmentID)
	}

	after, err := service.TimelineView(ctx, created.ID)
	if err != nil {
		t.Fatalf("更新后读取时间线失败: %v", err)
	}
	if after.Summary.Version != updated.Version || after.Summary.SegmentID != "SEG-NEW" || len(after.Events) != 2 {
		t.Fatalf("成功更新后仍返回旧时间线缓存: version=%d segment=%s events=%d", after.Summary.Version, after.Summary.SegmentID, len(after.Events))
	}
}
