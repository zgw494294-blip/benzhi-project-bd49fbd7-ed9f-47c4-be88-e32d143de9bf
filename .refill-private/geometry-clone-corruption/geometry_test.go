package geometryclonecorruption

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/httpui"
	"aquaflush-release-workbench/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOverflowingGeometryCannotPersistZeroBatch(t *testing.T) {
	db := filepath.Join(t.TempDir(), "workbench.db")
	repository, err := store.New(db)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpui.New(app.New(repository)).Routes()
	payload := map[string]any{
		"segmentId": "SEG-HUGE", "waterSource": "北区水厂", "createdBy": "负责人", "idempotencyKey": "huge-profile",
		"profile": map[string]any{"startMarker": "A", "endMarker": "B", "material": "球墨铸铁", "diameterMm": 1e308, "lengthM": 1e308, "volumeM3": 1e308, "targetChlorineMin": .3, "targetChlorineMax": 1.0},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/batches", bytes.NewReader(raw))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code < http.StatusBadRequest {
		var result map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &result)
		t.Fatalf("超大几何派生值溢出后不应成功持久化零值批次: status=%d body=%#v", recorder.Code, result)
	}
}
