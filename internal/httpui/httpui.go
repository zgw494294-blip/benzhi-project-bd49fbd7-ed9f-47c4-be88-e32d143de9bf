package httpui

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
)

//go:embed frontend/index.html frontend/style.css frontend/app.js
var frontendFiles embed.FS

type Handler struct {
	App *app.Service
	tpl *template.Template
}

func New(a *app.Service) *Handler {
	data, _ := frontendFiles.ReadFile("frontend/index.html")
	return &Handler{App: a, tpl: template.Must(template.New("index").Parse(string(data)))}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Home)
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, frontendFiles, "frontend/style.css")
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, frontendFiles, "frontend/app.js") })
	mux.HandleFunc("/api/batches", h.Batches)
	mux.HandleFunc("/api/batches/", h.BatchAction)
	return mux
}

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, errors.New("仅支持 GET"), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.tpl.Execute(w, nil)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, errors.New("请求格式无效: "+err.Error()), http.StatusBadRequest)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, errors.New("请求只能包含一个 JSON 对象"), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "响应序列化失败", "detail": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n"))
}

func writeError(w http.ResponseWriter, err error, fallback int) {
	status := fallback
	if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrImmutable) {
		status = http.StatusConflict
	}
	if errors.Is(err, domain.ErrNotFound) {
		status = http.StatusNotFound
	}
	payload := map[string]any{"error": err.Error()}
	var validation *domain.ValidationErrors
	if errors.As(err, &validation) {
		fields := map[string]string{}
		for _, issue := range validation.Issues {
			fields[issue.Field] = issue.Message
		}
		payload["error"] = "参数校验失败"
		payload["fieldErrors"] = fields
		payload["issues"] = validation.Issues
		status = http.StatusBadRequest
	}
	var blocked *app.BlockingError
	if errors.As(err, &blocked) {
		payload["error"], payload["blockers"] = blocked.Message, blocked.Blockers
		status = http.StatusBadRequest
	}
	var stale *app.StaleDecisionError
	if errors.As(err, &stale) {
		payload["error"], payload["changedCategories"] = stale.Error(), stale.Changes
		status = http.StatusConflict
	}
	if errors.Is(err, domain.ErrDataIntegrity) {
		status = http.StatusConflict
	}
	writeJSON(w, status, payload)
}

type batchRequest struct {
	BatchID         string                `json:"batchId"`
	SegmentID       string                `json:"segmentId"`
	WaterSource     string                `json:"waterSource"`
	CreatedBy       string                `json:"createdBy"`
	Actor           string                `json:"actor"`
	ExpectedVersion int                   `json:"expectedVersion"`
	IdempotencyKey  string                `json:"idempotencyKey"`
	Profile         domain.SegmentProfile `json:"profile"`
}

func (h *Handler) Batches(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if err := r.ParseForm(); err != nil {
			writeError(w, domain.NewValidationError(domain.ValidationIssue{Field: "query", Message: "查询参数格式无效"}), http.StatusBadRequest)
			return
		}
		limit := 0
		if raw := strings.TrimSpace(r.Form.Get("limit")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				writeError(w, domain.NewValidationError(domain.ValidationIssue{Field: "limit", Message: "分页大小必须为整数"}), http.StatusBadRequest)
				return
			}
			limit = value
		}
		out, err := h.App.ListBatches(r.Context(), app.BatchFilter{SegmentID: r.Form.Get("segmentId"), WaterSource: r.Form.Get("waterSource"), Status: r.Form.Get("status"), CreatedBy: r.Form.Get("createdBy"), Cursor: r.Form.Get("cursor"), Limit: limit})
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, errors.New("仅支持 GET 或 POST"), http.StatusMethodNotAllowed)
		return
	}
	var in batchRequest
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		writeError(w, domain.NewValidationError(domain.ValidationIssue{Field: "idempotencyKey", Message: "草稿幂等键不能为空"}), http.StatusBadRequest)
		return
	}
	var b *domain.Batch
	var err error
	if in.BatchID == "" {
		b, err = h.App.CreateBatchIdempotent(r.Context(), in.SegmentID, in.WaterSource, in.CreatedBy, in.Profile, in.IdempotencyKey)
	} else {
		b, err = h.App.SaveDraft(r.Context(), in.BatchID, in.ExpectedVersion, in.SegmentID, in.WaterSource, in.Profile, in.Actor, in.IdempotencyKey)
	}
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func parseID(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/batches/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

type actionRequest struct {
	Action              string                  `json:"action"`
	ExpectedVersion     int                     `json:"expectedVersion"`
	Actor               string                  `json:"actor"`
	Approved            bool                    `json:"approved"`
	Reason              string                  `json:"reason"`
	TargetStage         string                  `json:"targetStage"`
	IdempotencyKey      string                  `json:"idempotencyKey"`
	Plan                domain.Plan             `json:"plan"`
	Round               domain.ExecutionRound   `json:"round"`
	Sample              *domain.WaterSample     `json:"sample"`
	Samples             []domain.WaterSample    `json:"samples"`
	Corrective          domain.CorrectiveAction `json:"corrective"`
	ActionID            string                  `json:"actionId"`
	SourceSampleID      string                  `json:"sourceSampleId"`
	Measure             string                  `json:"measure"`
	AffectedPoints      []string                `json:"affectedPoints"`
	ConfirmationSummary string                  `json:"confirmationSummary"`
	WarningsConfirmed   bool                    `json:"warningsConfirmed"`
	ReviewToken         string                  `json:"reviewToken"`
}

func (h *Handler) BatchAction(w http.ResponseWriter, r *http.Request) {
	id, action := parseID(r.URL.Path)
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		var out any
		var err error
		switch action {
		case "":
			out, err = h.App.Get(r.Context(), id)
		case "timeline":
			out, err = h.App.TimelineView(r.Context(), id)
		case "summary":
			summary, summaryErr := h.App.Summary(r.Context(), id)
			if summaryErr != nil {
				err = summaryErr
			} else {
				evidence, evidenceErr := h.App.ReviewEvidence(r.Context(), id)
				if evidenceErr != nil {
					err = evidenceErr
				} else {
					out = struct {
						app.BatchSummary
						ReviewEvidence app.ReviewEvidence `json:"reviewEvidence"`
					}{summary, evidence}
				}
			}
		case "verify":
			out, err = h.App.VerifyCertificate(r.Context(), id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeError(w, err, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, errors.New("仅支持 GET 或 POST"), http.StatusMethodNotAllowed)
		return
	}
	var in actionRequest
	if !decode(w, r, &in) {
		return
	}
	var out any
	var err error
	switch action {
	case "freeze":
		if in.Action == "precheck" {
			out, err = h.App.PrecheckFreeze(r.Context(), id, in.ExpectedVersion, in.Plan)
		} else if in.Action == "confirm" {
			if strings.TrimSpace(in.IdempotencyKey) == "" {
				err = domain.NewValidationError(domain.ValidationIssue{Field: "idempotencyKey", Message: "冻结幂等键不能为空"})
			} else {
				out, err = h.App.FreezeConfirmed(r.Context(), id, in.ExpectedVersion, in.Plan, in.Actor, in.IdempotencyKey, in.ConfirmationSummary, in.WarningsConfirmed)
			}
		} else {
			err = domain.NewValidationError(domain.ValidationIssue{Field: "action", Message: "冻结请求 action 必须为 precheck 或 confirm"})
		}
	case "rounds":
		out, err = h.App.RoundWithKey(r.Context(), id, in.ExpectedVersion, in.Round, in.Actor, in.IdempotencyKey)
	case "finish":
		out, err = h.App.Finish(r.Context(), id, in.ExpectedVersion, in.Actor)
	case "samples":
		if in.Sample != nil && in.Samples != nil {
			err = domain.NewValidationError(domain.ValidationIssue{Field: "samples", Message: "sample 与 samples 必须互斥"})
		} else if in.Sample == nil && in.Samples == nil {
			err = domain.NewValidationError(domain.ValidationIssue{Field: "samples", Message: "必须提交 sample 或 samples"})
		} else {
			values := in.Samples
			if in.Sample != nil {
				values = []domain.WaterSample{*in.Sample}
			}
			if in.Sample != nil && strings.TrimSpace(in.IdempotencyKey) == "" {
				out, err = h.App.Sample(r.Context(), id, in.ExpectedVersion, *in.Sample, in.Actor)
			} else {
				out, err = h.App.Samples(r.Context(), id, in.ExpectedVersion, values, in.Actor, in.IdempotencyKey)
			}
		}
	case "corrective":
		corrective := in.Corrective
		if corrective.SourceSampleID == "" {
			corrective.SourceSampleID = in.SourceSampleID
		}
		if corrective.Reason == "" {
			corrective.Reason = in.Reason
		}
		if corrective.Measure == "" {
			corrective.Measure = in.Measure
		}
		if len(corrective.AffectedPoints) == 0 {
			corrective.AffectedPoints = in.AffectedPoints
		}
		out, err = h.App.Correct(r.Context(), id, in.ExpectedVersion, corrective, in.Actor)
	case "reinspect":
		if in.Sample == nil {
			err = domain.NewValidationError(domain.ValidationIssue{Field: "sample", Message: "复检样本不能为空"})
		} else {
			out, err = h.App.Reinspect(r.Context(), id, in.ExpectedVersion, in.ActionID, *in.Sample, in.Actor)
		}
	case "review":
		out, err = h.App.ReviewDecisionWithToken(r.Context(), id, in.ExpectedVersion, in.Actor, in.Approved, in.Reason, in.TargetStage, in.ReviewToken)
	case "release":
		out, err = h.App.Release(r.Context(), id, in.ExpectedVersion, in.Actor)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
