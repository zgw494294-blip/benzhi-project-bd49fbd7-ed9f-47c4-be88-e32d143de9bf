package stablepaginationinsert

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCursorKeepsPositionWhenNewBatchIsInserted(t *testing.T) {
	db := filepath.Join(t.TempDir(), "workbench.db")
	profile := &domain.SegmentProfile{StartMarker: "A", EndMarker: "B", Material: "球墨铸铁", VolumeM3: 10, TargetChlorineMin: .3, TargetChlorineMax: 1}
	batches := map[string]*domain.Batch{}
	for i, item := range []struct {
		id string
		at time.Time
	}{
		{"b1", time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"b2", time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)},
		{"b3", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		batches[item.id] = &domain.Batch{ID: item.id, SegmentID: "SEG", WaterSource: "北区水厂", CreatedBy: "负责人", Status: domain.StatusDraft, Version: 1, CreatedAt: item.at, UpdatedAt: item.at, Profile: profile, Rounds: []domain.ExecutionRound{}, Samples: []domain.WaterSample{}, Actions: []domain.CorrectiveAction{}}
		_ = i
	}
	raw, err := json.Marshal(map[string]any{"batches": batches, "events": []domain.AuditEvent{}, "idempotency": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(db, raw, 0600); err != nil {
		t.Fatal(err)
	}
	repository, err := store.New(db)
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repository)
	ctx := context.Background()
	first, err := service.ListBatches(ctx, app.BatchFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != "b1" || first.NextCursor == "" {
		t.Fatalf("初始分页结果异常: %#v", first)
	}
	_, err = service.CreateBatchIdempotent(ctx, "SEG-NEW", "北区水厂", "负责人", *profile, "insert-after-first-page")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ListBatches(ctx, app.BatchFilter{Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "b2" {
		t.Fatalf("游标应在插入后继续到 b2，实际结果: %#v", second.Items)
	}
}
