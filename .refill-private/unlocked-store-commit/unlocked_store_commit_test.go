package unlockedstorecommit_test

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentIndependentSavesRemainAtomic(t *testing.T) {
	repository, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("创建仓储失败: %v", err)
	}

	const workers = 8
	start := make(chan struct{})
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		batch := domain.NewBatch(
			fmt.Sprintf("segment-%d", i),
			fmt.Sprintf("source-%d", i),
			fmt.Sprintf("creator-%d", i),
			domain.SegmentProfile{StartMarker: "K0", EndMarker: "K1", Material: "球墨铸铁", DiameterMM: 200, LengthM: 100, VolumeM3: 3.14, TargetChlorineMin: 0.2, TargetChlorineMax: 0.8},
		)
		batch.ID = fmt.Sprintf("batch-%d", i)
		batch.Profile.BatchID = batch.ID
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, _, saveErr := repository.SaveDetailed(context.Background(), batch, "draft.create", batch.CreatedBy, "", "", "", "")
			errors <- saveErr
		}()
	}

	close(start)
	group.Wait()
	close(errors)
	for saveErr := range errors {
		if saveErr != nil {
			t.Fatalf("并发保存返回错误: %v", saveErr)
		}
	}

	batches, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("读取并发保存结果失败: %v", err)
	}
	if len(batches) != workers {
		t.Fatalf("并发保存发生丢失更新: 期望 %d 个批次，实际为 %d", workers, len(batches))
	}
}
