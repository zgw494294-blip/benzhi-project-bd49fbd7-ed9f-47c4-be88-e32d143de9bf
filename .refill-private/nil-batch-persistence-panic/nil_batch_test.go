package nilbatchpersistencepanic

import (
	"aquaflush-release-workbench/internal/store"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistedNullBatchReturnsIntegrityErrorsInsteadOfPanics(t *testing.T) {
	db := filepath.Join(t.TempDir(), "workbench.db")
	if err := os.WriteFile(db, []byte(`{"batches":{"broken":null},"events":[],"idempotency":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	repository, err := store.New(db)
	if err != nil {
		t.Fatal(err)
	}
	failures := []string{}
	if panicked, callErr := capture(func() error {
		_, getErr := repository.Get(context.Background(), "broken")
		return getErr
	}); panicked || callErr == nil {
		failures = append(failures, "Get 未返回数据一致性错误")
	}
	if panicked, callErr := capture(func() error {
		_, listErr := repository.List(context.Background())
		return listErr
	}); panicked || callErr == nil {
		failures = append(failures, "List 未返回数据一致性错误")
	}
	if len(failures) > 0 {
		t.Fatalf("持久化 null 批次进入读取链后发生运行时失效: %s", strings.Join(failures, "；"))
	}
}

func capture(fn func() error) (panicked bool, err error) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	err = fn()
	return panicked, err
}
