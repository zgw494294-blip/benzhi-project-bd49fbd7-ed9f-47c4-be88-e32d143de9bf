package prechecknonfiniteresponse

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/httpui"
	"aquaflush-release-workbench/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOverflowingPlanReturnsStructuredValidationError(t *testing.T) {
	repository, err := store.New(filepath.Join(t.TempDir(), "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repository)
	b, err := service.CreateBatchIdempotent(context.Background(), "SEG-PLAN", "北区水厂", "负责人", domain.SegmentProfile{StartMarker: "A", EndMarker: "B", Material: "球墨铸铁", VolumeM3: 10, TargetChlorineMin: .3, TargetChlorineMax: 1}, "normal-profile")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"action": "precheck", "expectedVersion": b.Version,
		"plan": map[string]any{"flowRateM3h": 1e308, "durationMin": 1e308, "disinfectantTarget": .6, "samplingPoints": []string{"P1"}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/batches/"+b.ID+"/freeze", bytes.NewReader(raw))
	recorder := httptest.NewRecorder()
	httpui.New(service).Routes().ServeHTTP(recorder, req)
	if recorder.Code < http.StatusBadRequest || !json.Valid(recorder.Body.Bytes()) {
		t.Fatalf("计划乘法溢出应返回结构化 4xx，实际 status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
