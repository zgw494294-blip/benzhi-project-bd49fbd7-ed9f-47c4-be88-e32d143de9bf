package contextcancelledsave

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/store"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type gatedContext struct {
	checked  chan struct{}
	release  chan struct{}
	canceled chan struct{}
	once     sync.Once
}

func newGatedContext() *gatedContext {
	return &gatedContext{checked: make(chan struct{}), release: make(chan struct{}), canceled: make(chan struct{})}
}

func (c *gatedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *gatedContext) Done() <-chan struct{}       { return c.canceled }
func (c *gatedContext) Value(any) any               { return nil }
func (c *gatedContext) Err() error {
	first := false
	c.once.Do(func() {
		first = true
		close(c.checked)
	})
	if first {
		<-c.release
		return nil
	}
	select {
	case <-c.canceled:
		return context.Canceled
	default:
		return nil
	}
}

func TestCancelledSaveDoesNotLeaveDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := newGatedContext()
	result := make(chan error, 1)
	go func() {
		_, _, saveErr := repository.SaveDetailed(ctx, &domain.Batch{ID: "b-cancel", Status: domain.StatusDraft, Version: 1}, "draft.create", "负责人", "状态变更", "", "", "")
		result <- saveErr
	}()
	<-ctx.checked
	close(ctx.canceled)
	close(ctx.release)
	if saveErr := <-result; !errors.Is(saveErr, context.Canceled) {
		t.Fatalf("取消后的保存应返回 context.Canceled，得到 %v", saveErr)
	}

	reopened, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Get(context.Background(), "b-cancel"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("取消后的请求不应在重启后留下批次，得到 %v", err)
	}
}
