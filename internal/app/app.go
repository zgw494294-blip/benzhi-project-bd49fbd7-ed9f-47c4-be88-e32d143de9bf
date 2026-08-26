package app

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/rules"
	"aquaflush-release-workbench/internal/store"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Service struct{ Store *store.Store }

func New(s *store.Store) *Service { return &Service{Store: s} }

func requestDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func auditDetail(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func requireActor(actor string) error {
	if strings.TrimSpace(actor) == "" {
		return domain.NewValidationError(domain.ValidationIssue{Field: "actor", Message: "操作者不能为空"})
	}
	return nil
}

func (s *Service) CreateBatch(ctx context.Context, segment, source, creator string, p domain.SegmentProfile) (*domain.Batch, error) {
	return s.CreateBatchIdempotent(ctx, segment, source, creator, p, "")
}

func (s *Service) CreateBatchIdempotent(ctx context.Context, segment, source, creator string, p domain.SegmentProfile, key string) (*domain.Batch, error) {
	if issues := domain.ValidateBatchInput(segment, source, creator, p); len(issues) > 0 {
		return nil, domain.NewValidationError(issues...)
	}
	p.StartMarker, p.EndMarker, p.Material = strings.TrimSpace(p.StartMarker), strings.TrimSpace(p.EndMarker), strings.TrimSpace(p.Material)
	digest := requestDigest(struct {
		Segment, Source, Creator string
		Profile                  domain.SegmentProfile
	}{segment, source, creator, p})
	if saved, ok, err := s.Store.Replay(ctx, "create", key, digest); err != nil || ok {
		return saved, err
	}
	b := domain.NewBatch(segment, source, creator, p)
	b.Profile.BatchID = b.ID
	b.GeometryCheck = rules.CheckGeometry(b.Profile)
	refreshProjection(b)
	detail := auditDetail(map[string]any{"changedFields": []string{"segmentId", "waterSource", "profile"}, "before": nil, "after": map[string]any{"segmentId": b.SegmentID, "waterSource": b.WaterSource, "profile": b.Profile, "geometryCheck": b.GeometryCheck}})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "draft.create", creator, detail, "create", key, digest)
	return saved, err
}

func (s *Service) SaveDraft(ctx context.Context, id string, expected int, segment, source string, p domain.SegmentProfile, actor, key string) (*domain.Batch, error) {
	digest := requestDigest(struct {
		ID, Segment, Source, Actor string
		Expected                   int
		Profile                    domain.SegmentProfile
	}{id, segment, source, actor, expected, p})
	scope := "draft:" + id
	if saved, ok, err := s.Store.Replay(ctx, scope, key, digest); err != nil || ok {
		return saved, err
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Status != domain.StatusDraft {
		return nil, domain.ErrImmutable
	}
	if b.Version != expected {
		return nil, domain.ErrConflict
	}
	if issues := domain.ValidateBatchInput(segment, source, actor, p); len(issues) > 0 {
		return nil, domain.NewValidationError(issues...)
	}
	before := struct {
		Segment, Source string
		Profile         *domain.SegmentProfile
		GeometryCheck   *domain.GeometryVolumeCheck
	}{b.SegmentID, b.WaterSource, b.Profile, b.GeometryCheck}
	p.StartMarker, p.EndMarker, p.Material = strings.TrimSpace(p.StartMarker), strings.TrimSpace(p.EndMarker), strings.TrimSpace(p.Material)
	changed := draftChangedFields(b, segment, source, p)
	if err = b.UpdateDraft(segment, source, p); err != nil {
		return nil, err
	}
	b.GeometryCheck = rules.CheckGeometry(b.Profile)
	refreshProjection(b)
	detail := auditDetail(map[string]any{"changedFields": changed, "before": before, "after": map[string]any{"segmentId": b.SegmentID, "waterSource": b.WaterSource, "profile": b.Profile, "geometryCheck": b.GeometryCheck}})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "draft.update", actor, detail, scope, key, digest)
	return saved, err
}

func draftChangedFields(b *domain.Batch, segment, source string, p domain.SegmentProfile) []string {
	changed := []string{}
	if b.SegmentID != strings.TrimSpace(segment) {
		changed = append(changed, "segmentId")
	}
	if b.WaterSource != strings.TrimSpace(source) {
		changed = append(changed, "waterSource")
	}
	oldRaw, _ := json.Marshal(b.Profile)
	p.BatchID = b.ID
	newRaw, _ := json.Marshal(&p)
	if string(oldRaw) != string(newRaw) {
		changed = append(changed, "profile")
	}
	return changed
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Batch, error) {
	b, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = projectBatch(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) Freeze(ctx context.Context, id string, expected int, plan domain.Plan, actor string) (*domain.Batch, error) {
	return s.FreezeWithKey(ctx, id, expected, plan, actor, "")
}

func (s *Service) FreezeWithKey(ctx context.Context, id string, expected int, plan domain.Plan, actor, key string) (*domain.Batch, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	pre, issues := rules.PrecheckPlan(id, expected, b.Profile, plan)
	if len(issues) > 0 {
		return nil, domain.NewValidationError(issues...)
	}
	pre.ConfirmationSummary = requestDigest(struct {
		BatchID string
		Version int
		Plan    domain.Plan
	}{id, expected, pre.NormalizedPlan})
	return s.FreezeConfirmed(ctx, id, expected, plan, actor, key, pre.ConfirmationSummary, true)
}

func (s *Service) Round(ctx context.Context, id string, expected int, r domain.ExecutionRound, actor string) (*domain.Batch, error) {
	return s.RoundWithKey(ctx, id, expected, r, actor, r.IdempotencyKey)
}

func (s *Service) RoundWithKey(ctx context.Context, id string, expected int, r domain.ExecutionRound, actor, key string) (*domain.Batch, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "idempotencyKey", Message: "轮次幂等键不能为空"})
	}
	r.IdempotencyKey = strings.TrimSpace(key)
	digest := requestDigest(struct {
		ID, Actor string
		Expected  int
		Round     domain.ExecutionRound
	}{id, actor, expected, r})
	scope := "round:" + id
	if saved, ok, err := s.Store.Replay(ctx, scope, key, digest); err != nil || ok {
		return saved, err
	}
	if issues := r.Validate(); len(issues) > 0 {
		return nil, domain.NewValidationError(issues...)
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Version != expected {
		return nil, domain.ErrConflict
	}
	if b.Status != domain.StatusFrozen && b.Status != domain.StatusExecuting {
		return nil, domain.ErrInvalidState
	}
	if r.Sequence != len(b.Rounds)+1 {
		return nil, fmt.Errorf("%w: 下一轮次序号应为 %d", domain.ErrConflict, len(b.Rounds)+1)
	}
	if b.Plan != nil && !r.StartedAt.After(b.Plan.FrozenAt) {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "round.startedAt", Message: "轮次开始时间必须晚于方案冻结时间"})
	}
	if r.EndedAt.After(time.Now().UTC().Add(2 * time.Minute)) {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "round.endedAt", Message: "轮次结束时间明显晚于服务器当前时间"})
	}
	for _, existing := range b.Rounds {
		if r.StartedAt.Before(existing.EndedAt) && r.EndedAt.After(existing.StartedAt) {
			return nil, domain.NewValidationError(domain.ValidationIssue{Field: "round.startedAt", Message: fmt.Sprintf("轮次时间与第 %d 轮重叠", existing.Sequence)})
		}
	}
	r.ID = fmt.Sprintf("r-%d", time.Now().UnixNano())
	r.BatchID, r.Operator = b.ID, strings.TrimSpace(actor)
	result := rules.EvaluateRound(b.Profile, b.Plan, r)
	r.ExchangeRatio, r.DurationMin, r.ChlorineDeviation = result.ExchangeRatio, result.DurationMin, result.ChlorineDeviation
	r.ResultReason, r.FailureReasons = result.Message, result.FailureReasons
	if result.Pass {
		r.Result = "pass"
	} else {
		r.Result = "fail"
	}
	if err = b.AddRound(r); err != nil {
		return nil, err
	}
	refreshProjection(b)
	detail := auditDetail(map[string]any{"roundId": r.ID, "sequence": r.Sequence, "result": r.Result, "reason": r.ResultReason, "exchangeRatio": r.ExchangeRatio, "durationMin": r.DurationMin, "chlorineDeviation": r.ChlorineDeviation})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "execution.round", actor, detail, scope, key, digest)
	return saved, err
}

func (s *Service) Finish(ctx context.Context, id string, expected int, actor string) (*domain.Batch, error) {
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
	progress := rules.ExecutionProgress(b)
	if len(progress.FinishBlockers) > 0 {
		return nil, &BlockingError{Message: "现场执行尚不能结束", Blockers: progress.FinishBlockers}
	}
	if err = b.FinishExecution(); err != nil {
		return nil, err
	}
	refreshProjection(b)
	saved, _, err := s.Store.SaveDetailed(ctx, b, "execution.finish", actor, auditDetail(map[string]any{"roundCount": len(b.Rounds), "finishedAt": b.ExecutionFinishedAt}), "", "", "")
	return saved, err
}

func validateSampleInput(b *domain.Batch, sample *domain.WaterSample) error {
	sample.SamplingPoint, sample.Witness = strings.TrimSpace(sample.SamplingPoint), strings.TrimSpace(sample.Witness)
	if issues := sample.Validate(); len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	if b.Plan == nil {
		return domain.ErrInvalidState
	}
	known := false
	for _, point := range b.Plan.SamplingPoints {
		if point == sample.SamplingPoint {
			known = true
			break
		}
	}
	if !known {
		return domain.NewValidationError(domain.ValidationIssue{Field: "samplingPoint", Message: "采样点不在冻结方案清单中"})
	}
	if b.ExecutionFinishedAt.IsZero() || sample.SampledAt.Before(b.ExecutionFinishedAt) {
		return domain.NewValidationError(domain.ValidationIssue{Field: "sampledAt", Message: "采样时间不能早于现场结束时间"})
	}
	if sample.SampledAt.After(time.Now().UTC().Add(2 * time.Minute)) {
		return domain.NewValidationError(domain.ValidationIssue{Field: "sampledAt", Message: "采样时间明显晚于服务器当前时间"})
	}
	return nil
}

func (s *Service) Sample(ctx context.Context, id string, expected int, sample domain.WaterSample, actor string) (*domain.Batch, error) {
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
	if b.Status != domain.StatusSampling && b.Status != domain.StatusRemediation {
		return nil, domain.ErrInvalidState
	}
	if err = validateSampleInput(b, &sample); err != nil {
		return nil, err
	}
	for _, existing := range b.Samples {
		if existing.Current && existing.SamplingPoint == sample.SamplingPoint {
			return nil, fmt.Errorf("%w: 该采样点已有当前样本，请通过整改复检入口提交", domain.ErrConflict)
		}
	}
	if sample.ID == "" {
		sample.ID = fmt.Sprintf("s-%d", time.Now().UnixNano())
	}
	for _, existing := range b.Samples {
		if existing.ID == sample.ID {
			return nil, domain.ErrConflict
		}
	}
	sample.BatchID, sample.Current = b.ID, true
	sample.Checks = rules.EvaluateSampleChecks(sample, b.Profile)
	sample.Verdict = rules.VerdictFromChecks(sample.Checks)
	if err = b.AddSample(sample); err != nil {
		return nil, err
	}
	recalculateSamplingState(b)
	refreshProjection(b)
	detail := auditDetail(map[string]any{"sampleId": sample.ID, "samplingPoint": sample.SamplingPoint, "verdict": sample.Verdict, "current": true})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "sample.record", actor, detail, "", "", "")
	return saved, err
}

func recalculateSamplingState(b *domain.Batch) {
	for _, action := range b.Actions {
		if action.Status == "open" {
			b.Status = domain.StatusRemediation
			b.RefreshDerived()
			return
		}
	}
	b.RefreshDerived()
	if b.Plan != nil && len(b.PendingSamplingPoints) == 0 {
		b.Status = domain.StatusReview
	} else {
		b.Status = domain.StatusSampling
	}
}

func (s *Service) Correct(ctx context.Context, id string, expected int, action domain.CorrectiveAction, actor string) (*domain.Batch, error) {
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
	if b.Status != domain.StatusSampling && b.Status != domain.StatusRemediation {
		return nil, domain.ErrInvalidState
	}
	action.Reason, action.Measure = strings.TrimSpace(action.Reason), strings.TrimSpace(action.Measure)
	issues := []domain.ValidationIssue{}
	if action.Reason == "" {
		issues = append(issues, domain.ValidationIssue{Field: "reason", Message: "整改原因不能为空"})
	}
	if action.Measure == "" {
		issues = append(issues, domain.ValidationIssue{Field: "measure", Message: "整改措施不能为空"})
	}
	var source *domain.WaterSample
	for i := range b.Samples {
		if b.Samples[i].ID == action.SourceSampleID {
			source = &b.Samples[i]
			break
		}
	}
	if source == nil || source.Verdict != "fail" {
		issues = append(issues, domain.ValidationIssue{Field: "sourceSampleId", Message: "整改项必须引用不合格样本"})
	}
	allowed := map[string]bool{}
	if b.Plan != nil {
		for _, point := range b.Plan.SamplingPoints {
			allowed[point] = true
		}
	}
	seen, includesSource := map[string]bool{}, false
	points := []string{}
	for i, point := range action.AffectedPoints {
		point = strings.TrimSpace(point)
		if point == "" || !allowed[point] {
			issues = append(issues, domain.ValidationIssue{Field: fmt.Sprintf("affectedPoints[%d]", i), Message: "影响点必须属于冻结方案"})
			continue
		}
		if seen[point] {
			issues = append(issues, domain.ValidationIssue{Field: "affectedPoints", Message: "影响点不能重复"})
			continue
		}
		seen[point], points = true, append(points, point)
		if source != nil && point == source.SamplingPoint {
			includesSource = true
		}
	}
	if len(points) == 0 {
		issues = append(issues, domain.ValidationIssue{Field: "affectedPoints", Message: "至少指定一个影响点"})
	}
	if source != nil && !includesSource {
		issues = append(issues, domain.ValidationIssue{Field: "affectedPoints", Message: "影响点必须包含源样本点"})
	}
	if len(issues) > 0 {
		return nil, domain.NewValidationError(issues...)
	}
	if action.ID == "" {
		action.ID = fmt.Sprintf("a-%d", time.Now().UnixNano())
	}
	for _, existing := range b.Actions {
		if existing.ID == action.ID {
			return nil, domain.ErrConflict
		}
	}
	action.BatchID, action.AffectedPoints = b.ID, points
	if err = b.CreateAction(action); err != nil {
		return nil, err
	}
	refreshProjection(b)
	detail := auditDetail(map[string]any{"actionId": action.ID, "sourceSampleId": action.SourceSampleID, "affectedPoints": action.AffectedPoints, "reason": action.Reason, "measure": action.Measure})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "corrective.create", actor, detail, "", "", "")
	return saved, err
}

func (s *Service) Reinspect(ctx context.Context, id string, expected int, actionID string, sample domain.WaterSample, actor string) (*domain.Batch, error) {
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
	if b.Status != domain.StatusRemediation {
		return nil, domain.ErrInvalidState
	}
	actionIndex := -1
	for i := range b.Actions {
		if b.Actions[i].ID == actionID {
			actionIndex = i
			break
		}
	}
	if actionIndex < 0 {
		return nil, domain.ErrNotFound
	}
	action := &b.Actions[actionIndex]
	if action.Status != "open" {
		return nil, domain.ErrInvalidState
	}
	if err = validateSampleInput(b, &sample); err != nil {
		return nil, err
	}
	affected := false
	for _, point := range action.AffectedPoints {
		if point == sample.SamplingPoint {
			affected = true
			break
		}
	}
	if !affected {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "samplingPoint", Message: "复检点不属于该整改项影响范围"})
	}
	supersedes := ""
	for i := len(b.Samples) - 1; i >= 0; i-- {
		if b.Samples[i].SamplingPoint == sample.SamplingPoint && b.Samples[i].Current {
			supersedes = b.Samples[i].ID
			b.Samples[i].Current = false
			break
		}
	}
	if supersedes == "" && sample.SamplingPoint == samplePointByID(b, action.SourceSampleID) {
		supersedes = action.SourceSampleID
	}
	if supersedes == "" {
		return nil, domain.NewValidationError(domain.ValidationIssue{Field: "samplingPoint", Message: "影响点缺少可关联的原始样本"})
	}
	for _, existing := range b.Samples {
		if existing.ID == supersedes && !sample.SampledAt.After(existing.SampledAt) {
			return nil, domain.NewValidationError(domain.ValidationIssue{Field: "sampledAt", Message: "复检时间必须晚于被替代样本"})
		}
	}
	sample.ID = fmt.Sprintf("s-%d", time.Now().UnixNano())
	sample.BatchID, sample.Current, sample.SupersedesSampleID, sample.CorrectiveActionID = b.ID, true, supersedes, action.ID
	sample.Checks = rules.EvaluateSampleChecks(sample, b.Profile)
	sample.Verdict = rules.VerdictFromChecks(sample.Checks)
	if err = b.AddSample(sample); err != nil {
		return nil, err
	}
	if actionPointsPassed(b, action.ID) {
		action = &b.Actions[actionIndex]
		action.Status, action.ClosedBySampleID, action.ClosedAt = "closed", sample.ID, time.Now().UTC()
	}
	recalculateSamplingState(b)
	refreshProjection(b)
	detail := auditDetail(map[string]any{"actionId": actionID, "sampleId": sample.ID, "samplingPoint": sample.SamplingPoint, "supersedesSampleId": supersedes, "verdict": sample.Verdict, "actionStatus": b.Actions[actionIndex].Status})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "corrective.reinspect", actor, detail, "", "", "")
	return saved, err
}

func samplePointByID(b *domain.Batch, id string) string {
	for _, sample := range b.Samples {
		if sample.ID == id {
			return sample.SamplingPoint
		}
	}
	return ""
}

func actionPointsPassed(b *domain.Batch, actionID string) bool {
	var action *domain.CorrectiveAction
	for i := range b.Actions {
		if b.Actions[i].ID == actionID {
			action = &b.Actions[i]
			break
		}
	}
	if action == nil {
		return false
	}
	for _, point := range action.AffectedPoints {
		var latest *domain.WaterSample
		for _, sample := range b.Samples {
			if sample.CorrectiveActionID == actionID && sample.SamplingPoint == point && (latest == nil || sample.SampledAt.After(latest.SampledAt)) {
				copy := sample
				latest = &copy
			}
		}
		if latest == nil || latest.Verdict != "pass" {
			return false
		}
	}
	return true
}

func (s *Service) Review(ctx context.Context, id string, expected int, actor string, approved bool) (*domain.Batch, error) {
	return s.ReviewDecision(ctx, id, expected, actor, approved, "", "")
}

func (s *Service) ReviewDecision(ctx context.Context, id string, expected int, actor string, approved bool, reason, targetStage string) (*domain.Batch, error) {
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
	if b.Status != domain.StatusReview {
		return nil, domain.ErrInvalidState
	}
	assessment := rules.AssessReleaseDetail(b)
	if approved {
		if !assessment.Eligible {
			return nil, &BlockingError{Message: "复核清单未通过", Blockers: assessment.Blockers}
		}
		if err = b.ReviewApprove(actor); err != nil {
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
		if err = b.ReviewReturn(actor, reason, target); err != nil {
			return nil, err
		}
	}
	detail := auditDetail(map[string]any{"approved": approved, "reason": reason, "targetStage": targetStage, "checklist": assessment})
	refreshProjection(b)
	saved, _, err := s.Store.SaveDetailed(ctx, b, "review.decision", actor, detail, "", "", "")
	return saved, err
}

func businessDigest(b *domain.Batch) string {
	type facts struct {
		ID, SegmentID, WaterSource, CreatedBy string
		ExecutionFinishedAt                   time.Time
		Profile                               *domain.SegmentProfile
		Plan                                  *domain.Plan
		Rounds                                []domain.ExecutionRound
		Samples                               []domain.WaterSample
		Actions                               []domain.CorrectiveAction
		Review                                *domain.ReviewDecision
		ReviewedBy                            string
	}
	copyActions := append([]domain.CorrectiveAction(nil), b.Actions...)
	for i := range copyActions {
		copyActions[i].AffectedPoints = append([]string(nil), copyActions[i].AffectedPoints...)
		sort.Strings(copyActions[i].AffectedPoints)
	}
	raw, _ := json.Marshal(facts{b.ID, b.SegmentID, b.WaterSource, b.CreatedBy, b.ExecutionFinishedAt, b.Profile, b.Plan, b.Rounds, b.Samples, copyActions, b.Review, b.ReviewedBy})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func verificationDigest(batchID string, version int, status domain.Status, business string, sequence int64) string {
	raw, _ := json.Marshal(struct {
		BatchID        string
		BatchVersion   int
		Status         domain.Status
		BusinessDigest string
		AuditSequence  int64
	}{batchID, version, status, business, sequence})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Service) Release(ctx context.Context, id string, expected int, issuer string) (*domain.ReleaseCertificate, error) {
	if err := requireActor(issuer); err != nil {
		return nil, err
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Version != expected {
		return nil, domain.ErrConflict
	}
	if b.Status != domain.StatusReview || b.Review == nil || !b.Review.Valid || !b.Review.Approved || b.ReviewedBy == "" || b.Certificate != nil {
		return nil, domain.ErrInvalidState
	}
	assessment := rules.AssessReleaseDetail(b)
	if !assessment.Eligible {
		return nil, &BlockingError{Message: "无法放行", Blockers: assessment.Blockers}
	}
	sequence, err := s.Store.NextAuditSequence(ctx, id)
	if err != nil {
		return nil, err
	}
	version := b.Version + 1
	business := businessDigest(b)
	cert := domain.ReleaseCertificate{
		ID: fmt.Sprintf("cert-%d", time.Now().UnixNano()), BatchID: b.ID, BatchVersion: version,
		IssuedAt: time.Now().UTC(), Issuer: strings.TrimSpace(issuer), BusinessDigest: business,
		VerificationDigest: verificationDigest(b.ID, version, domain.StatusReleased, business, sequence), AuditSequence: sequence,
	}
	if err = b.Release(cert); err != nil {
		return nil, err
	}
	refreshProjection(b)
	detail := auditDetail(map[string]any{"certificateId": cert.ID, "batchVersion": cert.BatchVersion, "businessDigest": cert.BusinessDigest, "verificationDigest": cert.VerificationDigest, "auditSequence": cert.AuditSequence})
	saved, _, err := s.Store.SaveDetailed(ctx, b, "release.issue", issuer, detail, "", "", "")
	if err != nil {
		return nil, err
	}
	return saved.Certificate, nil
}

func (s *Service) Timeline(ctx context.Context, id string) ([]domain.AuditEvent, error) {
	return s.Store.Events(ctx, id)
}
