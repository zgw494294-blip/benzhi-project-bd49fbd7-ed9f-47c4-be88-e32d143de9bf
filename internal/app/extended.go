package app

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/rules"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type BatchFilter struct {
	SegmentID, WaterSource, Status, CreatedBy, Cursor string
	Limit                                             int
}

type BatchList struct {
	Items      []BatchSummary `json:"items"`
	Total      int            `json:"total"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type listCursor struct {
	Filter         string `json:"filter"`
	UpdatedAtNanos int64  `json:"updatedAtNanos"`
	ID             string `json:"id"`
	Signature      string `json:"signature"`
}

func normalizedFilter(f BatchFilter) string {
	v := struct{ SegmentID, WaterSource, Status, CreatedBy string }{strings.ToLower(strings.TrimSpace(f.SegmentID)), strings.ToLower(strings.TrimSpace(f.WaterSource)), strings.ToLower(strings.TrimSpace(f.Status)), strings.ToLower(strings.TrimSpace(f.CreatedBy))}
	raw, _ := json.Marshal(v)
	return string(raw)
}

func cursorSignature(filter string, updatedAtNanos int64, id string) string {
	return requestDigest(struct {
		Scope, Filter  string
		UpdatedAtNanos int64
		ID             string
	}{"batch-list-v1", filter, updatedAtNanos, id})
}

// parseListCursor decodes the opaque cursor and returns the sort key of the
// last item emitted on the previous page. An empty cursor means start from the
// newest batches. The sort order is UpdatedAt descending, then ID ascending, so
// "after" the key means strictly later in that order.
func parseListCursor(raw, filter string) (time.Time, string, error) {
	if raw == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", domain.NewValidationError(domain.ValidationIssue{Field: "cursor", Message: "分页游标格式无效"})
	}
	var c listCursor
	if json.Unmarshal(decoded, &c) != nil || c.Signature != cursorSignature(c.Filter, c.UpdatedAtNanos, c.ID) {
		return time.Time{}, "", domain.NewValidationError(domain.ValidationIssue{Field: "cursor", Message: "分页游标格式无效或已被篡改"})
	}
	if c.Filter != filter {
		return time.Time{}, "", domain.NewValidationError(domain.ValidationIssue{Field: "cursor", Message: "分页游标与当前筛选条件不匹配"})
	}
	if c.UpdatedAtNanos < 0 {
		return time.Time{}, "", domain.NewValidationError(domain.ValidationIssue{Field: "cursor", Message: "分页游标格式无效或已被篡改"})
	}
	updatedAt := time.Unix(0, c.UpdatedAtNanos).UTC()
	return updatedAt, c.ID, nil
}

// makeListCursor encodes the sort key of the last item on the current page so
// the next request can resume strictly after it without depending on a numeric
// offset that shifts when newer batches are inserted ahead of it.
func makeListCursor(filter string, updatedAt time.Time, id string) string {
	c := listCursor{Filter: filter, UpdatedAtNanos: updatedAt.UnixNano(), ID: id, Signature: cursorSignature(filter, updatedAt.UnixNano(), id)}
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// afterCursor reports whether b sorts strictly after the page boundary key
// (UpdatedAt descending, then ID ascending). A zero cursor key matches every
// batch.
func afterCursor(b *domain.Batch, updatedAt time.Time, id string) bool {
	if updatedAt.IsZero() && id == "" {
		return true
	}
	if !b.UpdatedAt.Equal(updatedAt) {
		return b.UpdatedAt.Before(updatedAt)
	}
	return b.ID > id
}

func validStatus(v string) bool {
	for _, status := range []domain.Status{domain.StatusDraft, domain.StatusFrozen, domain.StatusExecuting, domain.StatusSampling, domain.StatusRemediation, domain.StatusReview, domain.StatusReleased} {
		if string(status) == v {
			return true
		}
	}
	return false
}

func (s *Service) ListBatches(ctx context.Context, f BatchFilter) (BatchList, error) {
	f.SegmentID, f.WaterSource, f.Status, f.CreatedBy = strings.TrimSpace(f.SegmentID), strings.TrimSpace(f.WaterSource), strings.ToLower(strings.TrimSpace(f.Status)), strings.TrimSpace(f.CreatedBy)
	if f.Status != "" && !validStatus(f.Status) {
		return BatchList{}, domain.NewValidationError(domain.ValidationIssue{Field: "status", Message: "未知批次状态"})
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit < 1 || f.Limit > 100 {
		return BatchList{}, domain.NewValidationError(domain.ValidationIssue{Field: "limit", Message: "分页大小必须在 1-100 之间"})
	}
	canonical := normalizedFilter(f)
	cursorUpdatedAt, cursorID, err := parseListCursor(strings.TrimSpace(f.Cursor), canonical)
	if err != nil {
		return BatchList{}, err
	}
	batches, err := s.Store.List(ctx)
	if err != nil {
		return BatchList{}, err
	}
	contains := func(value, query string) bool {
		return query == "" || strings.Contains(strings.ToLower(strings.TrimSpace(value)), strings.ToLower(query))
	}
	filtered := []*domain.Batch{}
	for _, b := range batches {
		if contains(b.SegmentID, f.SegmentID) && contains(b.WaterSource, f.WaterSource) && contains(b.CreatedBy, f.CreatedBy) && (f.Status == "" || string(b.Status) == f.Status) {
			filtered = append(filtered, b)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	result := BatchList{Items: []BatchSummary{}, Total: len(filtered)}
	start := 0
	for start < len(filtered) {
		if afterCursor(filtered[start], cursorUpdatedAt, cursorID) {
			break
		}
		start++
	}
	end := start + f.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	for _, b := range filtered[start:end] {
		if err = projectBatch(b); err != nil {
			return BatchList{}, err
		}
		summary := summaryFor(b)
		summary.SampleHistories = nil
		result.Items = append(result.Items, summary)
	}
	if end < len(filtered) {
		last := filtered[end-1]
		result.NextCursor = makeListCursor(canonical, last.UpdatedAt, last.ID)
	}
	return result, nil
}

type evidenceRound struct {
	ID       string `json:"id"`
	Sequence int    `json:"sequence"`
	Result   string `json:"result"`
}
type evidenceSample struct {
	ID      string `json:"id"`
	Point   string `json:"point"`
	Verdict string `json:"verdict"`
}
type EvidenceSnapshot struct {
	BatchVersion   int              `json:"batchVersion"`
	PlanVersion    int              `json:"planVersion"`
	Rounds         []evidenceRound  `json:"rounds"`
	CurrentSamples []evidenceSample `json:"currentSamples"`
	OpenActions    []string         `json:"openActions"`
}
type ReviewEvidence struct {
	Snapshot    EvidenceSnapshot `json:"snapshot"`
	ReviewToken string           `json:"reviewToken"`
	GeneratedAt time.Time        `json:"generatedAt"`
}
type tokenEnvelope struct {
	Snapshot  EvidenceSnapshot `json:"snapshot"`
	Signature string           `json:"signature"`
}

func evidenceFor(b *domain.Batch) EvidenceSnapshot {
	e := EvidenceSnapshot{BatchVersion: b.Version, Rounds: []evidenceRound{}, CurrentSamples: []evidenceSample{}, OpenActions: []string{}}
	if b.Plan != nil {
		e.PlanVersion = b.Plan.Version
	}
	for _, r := range b.Rounds {
		e.Rounds = append(e.Rounds, evidenceRound{r.ID, r.Sequence, r.Result})
	}
	for _, sample := range b.Samples {
		if sample.Current {
			e.CurrentSamples = append(e.CurrentSamples, evidenceSample{sample.ID, sample.SamplingPoint, sample.Verdict})
		}
	}
	for _, a := range b.Actions {
		if a.Status == "open" {
			e.OpenActions = append(e.OpenActions, a.ID)
		}
	}
	sort.Slice(e.CurrentSamples, func(i, j int) bool { return e.CurrentSamples[i].Point < e.CurrentSamples[j].Point })
	sort.Strings(e.OpenActions)
	return e
}
func reviewToken(e EvidenceSnapshot) string {
	env := tokenEnvelope{Snapshot: e, Signature: requestDigest(e)}
	raw, _ := json.Marshal(env)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func parseReviewToken(raw string) (EvidenceSnapshot, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return EvidenceSnapshot{}, domain.ErrConflict
	}
	var env tokenEnvelope
	if json.Unmarshal(decoded, &env) != nil || env.Signature != requestDigest(env.Snapshot) {
		return EvidenceSnapshot{}, domain.ErrConflict
	}
	return env.Snapshot, nil
}

type StaleDecisionError struct {
	Changes []string `json:"changes"`
}

func (e *StaleDecisionError) Error() string {
	return "复核证据已变化，请刷新后重新阅读"
}

func evidenceChanges(old, now EvidenceSnapshot) []string {
	out := []string{}
	if old.BatchVersion != now.BatchVersion {
		out = append(out, "version")
	}
	if old.PlanVersion != now.PlanVersion {
		out = append(out, "plan")
	}
	if requestDigest(old.Rounds) != requestDigest(now.Rounds) {
		out = append(out, "rounds")
	}
	if requestDigest(old.CurrentSamples) != requestDigest(now.CurrentSamples) {
		out = append(out, "samples")
	}
	if requestDigest(old.OpenActions) != requestDigest(now.OpenActions) {
		out = append(out, "corrective")
	}
	return out
}

func (s *Service) ReviewEvidence(ctx context.Context, id string) (ReviewEvidence, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return ReviewEvidence{}, err
	}
	e := evidenceFor(b)
	return ReviewEvidence{Snapshot: e, ReviewToken: reviewToken(e), GeneratedAt: time.Now().UTC()}, nil
}

func (s *Service) ReviewDecisionWithToken(ctx context.Context, id string, expected int, actor string, approved bool, reason, target, token string) (*domain.Batch, error) {
	if strings.TrimSpace(token) == "" {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "reviewToken", Message: "复核决定必须携带证据令牌"})
	}
	old, err := parseReviewToken(strings.TrimSpace(token))
	if err != nil {
		return nil, &StaleDecisionError{Changes: []string{"token"}}
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := evidenceFor(b)
	changes := evidenceChanges(old, now)
	if expected != b.Version && !containsString(changes, "version") {
		changes = append(changes, "version")
	}
	if len(changes) > 0 {
		return nil, &StaleDecisionError{Changes: changes}
	}
	return s.reviewDecisionUsingEvidence(ctx, b, actor, approved, reason, target, now)
}
func containsString(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Service) reviewDecisionUsingEvidence(ctx context.Context, b *domain.Batch, actor string, approved bool, reason, targetStage string, e EvidenceSnapshot) (*domain.Batch, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if b.Status != domain.StatusReview {
		return nil, domain.ErrInvalidState
	}
	assessment := rules.AssessReleaseDetail(b)
	if approved {
		if !assessment.Eligible {
			return nil, &BlockingError{Message: "复核清单未通过", Blockers: assessment.Blockers}
		}
		if err := b.ReviewApprove(actor); err != nil {
			return nil, err
		}
	} else {
		reason = strings.TrimSpace(reason)
		target := domain.Status(targetStage)
		issues := []domain.ValidationIssue{}
		if reason == "" {
			issues = append(issues, domain.ValidationIssue{Field: "reason", Message: "退回原因不能为空"})
		}
		if target != domain.StatusSampling && target != domain.StatusRemediation {
			issues = append(issues, domain.ValidationIssue{Field: "targetStage", Message: "退回环节必须为 sampling 或 remediation"})
		}
		if len(issues) > 0 {
			return nil, domain.NewValidationError(issues...)
		}
		if err := b.ReviewReturn(actor, reason, target); err != nil {
			return nil, err
		}
	}
	detail := auditDetail(map[string]any{"approved": approved, "reason": reason, "targetStage": targetStage, "checklist": assessment, "evidence": e})
	refreshProjection(b)
	saved, _, err := s.Store.SaveDetailed(ctx, b, "review.decision", actor, detail, "", "", "")
	return saved, err
}

func (s *Service) PrecheckFreeze(ctx context.Context, id string, expected int, plan domain.Plan) (rules.PlanPrecheck, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return rules.PlanPrecheck{}, err
	}
	if b.Status != domain.StatusDraft {
		return rules.PlanPrecheck{}, domain.ErrInvalidState
	}
	if b.Version != expected {
		return rules.PlanPrecheck{}, domain.ErrConflict
	}
	if b.GeometryCheck != nil && !b.GeometryCheck.WithinTolerance {
		return rules.PlanPrecheck{}, &BlockingError{Message: "管段几何容积偏差超过容差，不能冻结方案", Blockers: []string{fmt.Sprintf("申报 %.6f m³，理论 %.6f m³，允许偏差 ±%.2f%%", b.GeometryCheck.DeclaredVolumeM3, b.GeometryCheck.TheoreticalVolumeM3, b.GeometryCheck.TolerancePercent)}}
	}
	result, issues := rules.PrecheckPlan(b.ID, expected, b.Profile, plan)
	if len(issues) > 0 {
		return result, domain.NewValidationError(issues...)
	}
	result.ConfirmationSummary = requestDigest(struct {
		BatchID string
		Version int
		Plan    domain.Plan
	}{b.ID, b.Version, result.NormalizedPlan})
	return result, nil
}

func (s *Service) FreezeConfirmed(ctx context.Context, id string, expected int, plan domain.Plan, actor, key, confirmation string, warningsConfirmed bool) (*domain.Batch, error) {
	digest := requestDigest(struct {
		ID, Actor, Confirmation string
		Expected                int
		WarningsConfirmed       bool
	}{id, actor, confirmation, expected, warningsConfirmed})
	if saved, ok, err := s.Store.Replay(ctx, "freeze:"+id, key, digest); err != nil || ok {
		return saved, err
	}
	pre, err := s.PrecheckFreeze(ctx, id, expected, plan)
	if err != nil {
		return nil, err
	}
	if confirmation == "" || confirmation != pre.ConfirmationSummary {
		return nil, domain.ErrConflict
	}
	if len(pre.Warnings) > 0 && !warningsConfirmed {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "warningsConfirmed", Message: "存在预检警示，需负责人明确确认"})
	}
	return s.freezePrepared(ctx, id, expected, pre, actor, key, warningsConfirmed)
}
func (s *Service) freezePrepared(ctx context.Context, id string, expected int, pre rules.PlanPrecheck, actor, key string, warningsConfirmed bool) (*domain.Batch, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Version != expected {
		return nil, domain.ErrConflict
	}
	if err = b.Freeze(pre.NormalizedPlan); err != nil {
		return nil, err
	}
	refreshProjection(b)
	detail := auditDetail(map[string]any{"idempotencyKey": key, "planVersion": b.Plan.Version, "precheck": pre})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "plan.freeze", actor, detail, "freeze:"+id, key, requestDigest(struct {
		ID, Actor, Confirmation string
		Expected                int
		WarningsConfirmed       bool
	}{id, actor, pre.ConfirmationSummary, expected, warningsConfirmed}))
	return saved, err
}

type SampleBatchResult struct {
	RecordedSamples []domain.WaterSample    `json:"recordedSamples"`
	CoverageMatrix  []domain.SampleCoverage `json:"coverageMatrix"`
	Batch           *domain.Batch           `json:"batch"`
}

func (s *Service) Samples(ctx context.Context, id string, expected int, samples []domain.WaterSample, actor, key string) (SampleBatchResult, error) {
	if err := requireActor(actor); err != nil {
		return SampleBatchResult{}, err
	}
	if strings.TrimSpace(key) == "" {
		return SampleBatchResult{}, domain.NewValidationError(domain.ValidationIssue{Field: "idempotencyKey", Message: "样本幂等键不能为空"})
	}
	for i := range samples {
		samples[i].SamplingPoint = strings.TrimSpace(samples[i].SamplingPoint)
		samples[i].Witness = strings.TrimSpace(samples[i].Witness)
		samples[i].ID = ""
		samples[i].BatchID = ""
		samples[i].Verdict = ""
		samples[i].SupersedesSampleID = ""
		samples[i].CorrectiveActionID = ""
		samples[i].Current = false
		samples[i].Checks = nil
	}
	digest := requestDigest(struct {
		ID, Actor string
		Expected  int
		Samples   []domain.WaterSample
	}{id, actor, expected, samples})
	scope := "samples:" + id
	if saved, ok, err := s.Store.Replay(ctx, scope, key, digest); err != nil {
		return SampleBatchResult{}, err
	} else if ok {
		return sampleResultFor(saved, samples), nil
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return SampleBatchResult{}, err
	}
	if b.Version != expected {
		return SampleBatchResult{}, domain.ErrConflict
	}
	if b.Status != domain.StatusSampling && b.Status != domain.StatusRemediation {
		return SampleBatchResult{}, domain.ErrInvalidState
	}
	if len(samples) == 0 {
		return SampleBatchResult{}, domain.NewValidationError(domain.ValidationIssue{Field: "samples", Message: "至少提交一个样本"})
	}
	issues := []domain.ValidationIssue{}
	seen := map[string]bool{}
	existing := map[string]bool{}
	for _, x := range b.Samples {
		if x.Current {
			existing[x.SamplingPoint] = true
		}
	}
	for i := range samples {
		samples[i].SamplingPoint = strings.TrimSpace(samples[i].SamplingPoint)
		samples[i].Witness = strings.TrimSpace(samples[i].Witness)
		if ve := validateSampleInput(b, &samples[i]); ve != nil {
			var list *domain.ValidationErrors
			if ok := asValidation(ve, &list); ok {
				for _, issue := range list.Issues {
					issues = append(issues, domain.ValidationIssue{Field: fmt.Sprintf("samples[%d].%s", i, issue.Field), Message: issue.Message})
				}
			} else {
				issues = append(issues, domain.ValidationIssue{Field: fmt.Sprintf("samples[%d]", i), Message: ve.Error()})
			}
		}
		point := samples[i].SamplingPoint
		if point != "" && seen[point] {
			issues = append(issues, domain.ValidationIssue{Field: fmt.Sprintf("samples[%d].samplingPoint", i), Message: "批量内采样点不能重复"})
		}
		if existing[point] {
			issues = append(issues, domain.ValidationIssue{Field: fmt.Sprintf("samples[%d].samplingPoint", i), Message: "该采样点已有当前样本，请通过整改复检入口提交"})
		}
		seen[point] = true
	}
	if len(issues) > 0 {
		return SampleBatchResult{}, domain.NewValidationError(issues...)
	}
	now := time.Now().UnixNano()
	for i := range samples {
		samples[i].ID = fmt.Sprintf("s-%d-%d", now, i+1)
		samples[i].BatchID = b.ID
		samples[i].Current = true
		samples[i].Checks = rules.EvaluateSampleChecks(samples[i], b.Profile)
		samples[i].Verdict = rules.VerdictFromChecks(samples[i].Checks)
	}
	if err = b.AddSamples(samples); err != nil {
		return SampleBatchResult{}, err
	}
	recalculateSamplingState(b)
	refreshProjection(b)
	ids := []string{}
	for _, x := range samples {
		ids = append(ids, x.ID)
	}
	detail := auditDetail(map[string]any{"sampleIds": ids, "count": len(ids)})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "sample.record.batch", actor, detail, scope, key, digest)
	if err != nil {
		return SampleBatchResult{}, err
	}
	return SampleBatchResult{RecordedSamples: samples, CoverageMatrix: saved.CoverageMatrix, Batch: saved}, nil
}
func asValidation(err error, target **domain.ValidationErrors) bool {
	v, ok := err.(*domain.ValidationErrors)
	if ok {
		*target = v
	}
	return ok
}
func sampleResultFor(b *domain.Batch, input []domain.WaterSample) SampleBatchResult {
	points := map[string]bool{}
	for _, s := range input {
		points[strings.TrimSpace(s.SamplingPoint)] = true
	}
	recorded := []domain.WaterSample{}
	for _, s := range b.Samples {
		if s.Current && points[s.SamplingPoint] {
			recorded = append(recorded, s)
		}
	}
	sort.SliceStable(recorded, func(i, j int) bool {
		for n, p := range input {
			if strings.TrimSpace(p.SamplingPoint) == recorded[i].SamplingPoint {
				return n < indexPoint(input, recorded[j].SamplingPoint)
			}
		}
		return false
	})
	return SampleBatchResult{RecordedSamples: recorded, CoverageMatrix: b.CoverageMatrix, Batch: b}
}
func indexPoint(samples []domain.WaterSample, p string) int {
	for i, s := range samples {
		if strings.TrimSpace(s.SamplingPoint) == p {
			return i
		}
	}
	return len(samples)
}
