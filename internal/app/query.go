package app

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/rules"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type ReviewChecklist struct {
	PlanVersion    int                       `json:"planVersion"`
	PlanFrozenAt   time.Time                 `json:"planFrozenAt,omitempty"`
	RoundResults   []domain.ExecutionRound   `json:"roundResults"`
	CoverageMatrix []domain.SampleCoverage   `json:"coverageMatrix"`
	OpenActions    []domain.CorrectiveAction `json:"openActions"`
	Assessment     rules.ReleaseAssessment   `json:"assessment"`
}

type BatchSummary struct {
	ID                    string                        `json:"id"`
	SegmentID             string                        `json:"segmentId"`
	Status                string                        `json:"status"`
	Version               int                           `json:"version"`
	RoundCount            int                           `json:"roundCount"`
	SampleCount           int                           `json:"sampleCount"`
	OpenActionCount       int                           `json:"openActionCount"`
	LastCompletedSequence int                           `json:"lastCompletedSequence"`
	NextSequence          int                           `json:"nextSequence"`
	CanContinue           bool                          `json:"canContinue"`
	ReleaseEligible       bool                          `json:"releaseEligible"`
	ReleaseMessage        string                        `json:"releaseMessage"`
	ReleaseBlockers       []string                      `json:"releaseBlockers"`
	PlanSummary           *domain.PlanSummary           `json:"planSummary,omitempty"`
	CoverageMatrix        []domain.SampleCoverage       `json:"coverageMatrix"`
	PendingSamplingPoints []string                      `json:"pendingSamplingPoints"`
	WaterSource           string                        `json:"waterSource"`
	CreatedBy             string                        `json:"createdBy"`
	UpdatedAt             time.Time                     `json:"updatedAt"`
	CurrentStage          string                        `json:"currentStage"`
	NextAllowedAction     string                        `json:"nextAllowedAction"`
	PendingSampleCount    int                           `json:"pendingSampleCount"`
	ExecutionProgress     domain.ExecutionProgress      `json:"executionProgress"`
	WaterQuality          domain.WaterQualityConclusion `json:"waterQuality"`
	CorrectiveProgress    []domain.CorrectiveProgress   `json:"correctiveProgress"`
	SampleHistories       []domain.SamplePointHistory   `json:"sampleHistories,omitempty"`
}

type TimelineView struct {
	Events         []domain.AuditEvent `json:"events"`
	Summary        BatchSummary        `json:"summary"`
	Checklist      ReviewChecklist     `json:"checklist"`
	ReviewEvidence ReviewEvidence      `json:"reviewEvidence"`
}

type CertificateVerification struct {
	Passed             bool     `json:"passed"`
	CertificateID      string   `json:"certificateId,omitempty"`
	BatchID            string   `json:"batchId"`
	BatchVersion       int      `json:"batchVersion"`
	AuditSequence      int64    `json:"auditSequence"`
	TimelineContinuous bool     `json:"timelineContinuous"`
	Reasons            []string `json:"reasons"`
	Message            string   `json:"message"`
}

func (s *Service) Summary(ctx context.Context, id string) (BatchSummary, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return BatchSummary{}, err
	}
	return summaryFor(b), nil
}

func summaryFor(b *domain.Batch) BatchSummary {
	b.RefreshDerived()
	assessment := rules.AssessReleaseDetail(b)
	open := 0
	for _, action := range b.Actions {
		if action.Status != "closed" {
			open++
		}
	}
	var planSummary *domain.PlanSummary
	if b.Plan != nil {
		summary := b.Plan.Summary
		planSummary = &summary
	}
	return BatchSummary{
		ID: b.ID, SegmentID: b.SegmentID, Status: string(b.Status), Version: b.Version,
		RoundCount: len(b.Rounds), SampleCount: len(b.Samples), OpenActionCount: open,
		LastCompletedSequence: b.LastCompletedSequence, NextSequence: b.NextSequence, CanContinue: b.CanContinue,
		ReleaseEligible: assessment.Eligible, ReleaseMessage: assessment.Message, ReleaseBlockers: assessment.Blockers,
		PlanSummary: planSummary, CoverageMatrix: b.CoverageMatrix, PendingSamplingPoints: b.PendingSamplingPoints,
		WaterSource: b.WaterSource, CreatedBy: b.CreatedBy, UpdatedAt: b.UpdatedAt, CurrentStage: currentStage(b.Status), NextAllowedAction: nextAction(b), PendingSampleCount: len(b.PendingSamplingPoints), ExecutionProgress: b.ExecutionProgress, WaterQuality: b.WaterQuality, CorrectiveProgress: b.CorrectiveProgress, SampleHistories: b.SampleHistories,
	}
}

func currentStage(status domain.Status) string {
	labels := map[domain.Status]string{domain.StatusDraft: "建批", domain.StatusFrozen: "待执行", domain.StatusExecuting: "现场执行", domain.StatusSampling: "水质采样", domain.StatusRemediation: "整改复检", domain.StatusReview: "复核", domain.StatusReleased: "已放行"}
	return labels[status]
}
func nextAction(b *domain.Batch) string {
	switch b.Status {
	case domain.StatusDraft:
		return "预检并冻结方案"
	case domain.StatusFrozen, domain.StatusExecuting:
		return "登记下一轮次"
	case domain.StatusSampling:
		return "登记待补样本"
	case domain.StatusRemediation:
		return "处理整改复检任务"
	case domain.StatusReview:
		if b.Review != nil && b.Review.Approved {
			return "签发放行凭据"
		}
		return "生成证据快照并复核"
	case domain.StatusReleased:
		return "核验时间线与凭据"
	}
	return "查看详情"
}

func checklistFor(b *domain.Batch) ReviewChecklist {
	b.RefreshDerived()
	checklist := ReviewChecklist{RoundResults: b.Rounds, CoverageMatrix: b.CoverageMatrix, Assessment: rules.AssessReleaseDetail(b), OpenActions: []domain.CorrectiveAction{}}
	if b.Plan != nil {
		checklist.PlanVersion, checklist.PlanFrozenAt = b.Plan.Version, b.Plan.FrozenAt
	}
	for _, action := range b.Actions {
		if action.Status == "open" {
			checklist.OpenActions = append(checklist.OpenActions, action)
		}
	}
	return checklist
}

func (s *Service) TimelineView(ctx context.Context, id string) (TimelineView, error) {
	if cached, ok, err := s.cachedTimelineView(id); err != nil || ok {
		return cached, err
	}
	events, err := s.Timeline(ctx, id)
	if err != nil {
		return TimelineView{}, err
	}
	summary, err := s.Summary(ctx, id)
	if err != nil {
		return TimelineView{}, err
	}
	b, err := s.Get(ctx, id)
	if err != nil {
		return TimelineView{}, err
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	evidenceSnapshot := evidenceFor(b)
	view := TimelineView{Events: events, Summary: summary, Checklist: checklistFor(b), ReviewEvidence: ReviewEvidence{Snapshot: evidenceSnapshot, ReviewToken: reviewToken(evidenceSnapshot), GeneratedAt: time.Now().UTC()}}
	if err := s.rememberTimelineView(id, view); err != nil {
		return TimelineView{}, err
	}
	return view, nil
}

func (s *Service) cachedTimelineView(id string) (TimelineView, bool, error) {
	s.timelineMu.RLock()
	raw, ok := s.timelineCache[id]
	raw = append([]byte(nil), raw...)
	s.timelineMu.RUnlock()
	if !ok {
		return TimelineView{}, false, nil
	}
	var view TimelineView
	if err := json.Unmarshal(raw, &view); err != nil {
		return TimelineView{}, false, fmt.Errorf("解析时间线缓存: %w", err)
	}
	return view, true, nil
}

func (s *Service) rememberTimelineView(id string, view TimelineView) error {
	raw, err := json.Marshal(view)
	if err != nil {
		return fmt.Errorf("生成时间线缓存: %w", err)
	}
	s.timelineMu.Lock()
	s.timelineCache[id] = append([]byte(nil), raw...)
	s.timelineMu.Unlock()
	return nil
}

func VerifyCertificate(b *domain.Batch) (bool, string) {
	if b == nil || b.Certificate == nil {
		return false, "凭据不存在"
	}
	cert := b.Certificate
	if b.Status != domain.StatusReleased {
		return false, "批次不是已签发状态"
	}
	if cert.BatchID != b.ID {
		return false, "凭据批次不一致"
	}
	if cert.BatchVersion != b.Version {
		return false, "凭据版本与当前批次不一致"
	}
	wantBusiness := businessDigest(b)
	if cert.BusinessDigest != wantBusiness {
		return false, "业务摘要不一致"
	}
	wantVerification := verificationDigest(b.ID, b.Version, b.Status, wantBusiness, cert.AuditSequence)
	if cert.VerificationDigest != wantVerification {
		return false, "校验摘要不一致"
	}
	return true, fmt.Sprintf("凭据 %s 校验通过", cert.ID)
}

func (s *Service) VerifyCertificate(ctx context.Context, id string) (CertificateVerification, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return CertificateVerification{}, err
	}
	result := CertificateVerification{BatchID: b.ID, Reasons: []string{}}
	if b.Certificate != nil {
		result.CertificateID, result.BatchVersion, result.AuditSequence = b.Certificate.ID, b.Certificate.BatchVersion, b.Certificate.AuditSequence
	}
	if ok, message := VerifyCertificate(b); !ok {
		result.Reasons = append(result.Reasons, message)
	}
	events, err := s.Timeline(ctx, id)
	if err != nil {
		return CertificateVerification{}, err
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	continuous := len(events) > 0
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			continuous = false
			break
		}
	}
	if b.Certificate != nil {
		foundRelease := false
		for _, event := range events {
			if event.Sequence == b.Certificate.AuditSequence && event.Action == "release.issue" {
				foundRelease = true
				break
			}
		}
		if !foundRelease {
			result.Reasons = append(result.Reasons, "凭据审计序号未对应签发事件")
		}
		if int64(len(events)) != b.Certificate.AuditSequence {
			result.Reasons = append(result.Reasons, "签发事件不是当前时间线末项")
		}
	}
	result.TimelineContinuous = continuous
	if !continuous {
		result.Reasons = append(result.Reasons, "审计时间线序号不连续")
	}
	result.Passed = len(result.Reasons) == 0
	if result.Passed {
		result.Message = fmt.Sprintf("凭据 %s、批次版本和连续审计时间线核验通过", result.CertificateID)
	} else {
		result.Message = "凭据核验失败"
	}
	return result, nil
}
