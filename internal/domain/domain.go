package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("未找到批次")
	ErrConflict      = errors.New("版本或幂等请求冲突")
	ErrInvalidState  = errors.New("当前状态不允许此操作")
	ErrImmutable     = errors.New("批次已冻结，草稿不可修改")
	ErrDataIntegrity = errors.New("样本历史数据一致性错误")
)

type Status string

const (
	StatusDraft       Status = "draft"
	StatusFrozen      Status = "frozen"
	StatusExecuting   Status = "executing"
	StatusSampling    Status = "sampling"
	StatusRemediation Status = "remediation"
	StatusReview      Status = "review"
	StatusReleased    Status = "released"
)

type Batch struct {
	ID                  string              `json:"id"`
	SegmentID           string              `json:"segmentId"`
	WaterSource         string              `json:"waterSource"`
	CreatedBy           string              `json:"createdBy"`
	ReviewedBy          string              `json:"reviewedBy,omitempty"`
	Status              Status              `json:"status"`
	Version             int                 `json:"version"`
	CreatedAt           time.Time           `json:"createdAt"`
	UpdatedAt           time.Time           `json:"updatedAt"`
	ExecutionFinishedAt time.Time           `json:"executionFinishedAt,omitempty"`
	Profile             *SegmentProfile     `json:"profile"`
	Plan                *Plan               `json:"plan,omitempty"`
	Rounds              []ExecutionRound    `json:"rounds"`
	Samples             []WaterSample       `json:"samples"`
	Actions             []CorrectiveAction  `json:"actions"`
	Review              *ReviewDecision     `json:"review,omitempty"`
	Certificate         *ReleaseCertificate `json:"certificate,omitempty"`

	LastCompletedSequence int                    `json:"lastCompletedSequence"`
	NextSequence          int                    `json:"nextSequence"`
	CanContinue           bool                   `json:"canContinue"`
	CoverageMatrix        []SampleCoverage       `json:"coverageMatrix"`
	PendingSamplingPoints []string               `json:"pendingSamplingPoints"`
	GeometryCheck         *GeometryVolumeCheck   `json:"geometryCheck,omitempty"`
	ExecutionProgress     ExecutionProgress      `json:"executionProgress"`
	SampleHistories       []SamplePointHistory   `json:"sampleHistories"`
	WaterQuality          WaterQualityConclusion `json:"waterQuality"`
	CorrectiveProgress    []CorrectiveProgress   `json:"correctiveProgress"`
}

type GeometryVolumeCheck struct {
	Complete             bool    `json:"complete"`
	TheoreticalVolumeM3  float64 `json:"theoreticalVolumeM3"`
	DeclaredVolumeM3     float64 `json:"declaredVolumeM3"`
	AbsoluteDifferenceM3 float64 `json:"absoluteDifferenceM3"`
	DeviationPercent     float64 `json:"deviationPercent"`
	TolerancePercent     float64 `json:"tolerancePercent"`
	WithinTolerance      bool    `json:"withinTolerance"`
}

type ExecutionProgress struct {
	RequiredVolumeM3     float64                   `json:"requiredVolumeM3"`
	TotalVolumeM3        float64                   `json:"totalVolumeM3"`
	EffectiveVolumeM3    float64                   `json:"effectiveVolumeM3"`
	TotalExchangeRatio   float64                   `json:"totalExchangeRatio"`
	EffectiveDurationMin float64                   `json:"effectiveDurationMin"`
	CompletionPercent    float64                   `json:"completionPercent"`
	NextSequence         int                       `json:"nextSequence"`
	ByType               map[string]RoundAggregate `json:"byType"`
	FinishBlockers       []string                  `json:"finishBlockers"`
}

type RoundAggregate struct {
	RoundCount  int     `json:"roundCount"`
	VolumeM3    float64 `json:"volumeM3"`
	DurationMin float64 `json:"durationMin"`
}

type SegmentProfile struct {
	ID                string  `json:"id,omitempty"`
	BatchID           string  `json:"batchId,omitempty"`
	StartMarker       string  `json:"startMarker"`
	EndMarker         string  `json:"endMarker"`
	Material          string  `json:"material"`
	DiameterMM        float64 `json:"diameterMm"`
	LengthM           float64 `json:"lengthM"`
	VolumeM3          float64 `json:"volumeM3"`
	TargetChlorineMin float64 `json:"targetChlorineMin"`
	TargetChlorineMax float64 `json:"targetChlorineMax"`
}

type PlanSummary struct {
	EstimatedExchangeRatio        float64 `json:"estimatedExchangeRatio"`
	MinimumExecutionVolumeM3      float64 `json:"minimumExecutionVolumeM3"`
	ChlorineDeviationThresholdMgL float64 `json:"chlorineDeviationThresholdMgL"`
}

type Plan struct {
	Version            int                  `json:"version"`
	FlowRateM3H        float64              `json:"flowRateM3h"`
	DurationMin        float64              `json:"durationMin"`
	DisinfectantTarget float64              `json:"disinfectantTarget"`
	SamplingPoints     []string             `json:"samplingPoints"`
	FrozenAt           time.Time            `json:"frozenAt"`
	Summary            PlanSummary          `json:"summary"`
	GeometryCheck      *GeometryVolumeCheck `json:"geometryCheck,omitempty"`
}

type ExecutionRound struct {
	ID                string    `json:"id"`
	BatchID           string    `json:"batchId"`
	RoundType         string    `json:"roundType"`
	Operator          string    `json:"operator"`
	Result            string    `json:"result"`
	ResultReason      string    `json:"resultReason"`
	FailureReasons    []string  `json:"failureReasons"`
	IdempotencyKey    string    `json:"idempotencyKey,omitempty"`
	Sequence          int       `json:"sequence"`
	StartedAt         time.Time `json:"startedAt"`
	EndedAt           time.Time `json:"endedAt"`
	FlowRateM3H       float64   `json:"flowRateM3h"`
	ChlorineMgL       float64   `json:"chlorineMgL"`
	ExchangeRatio     float64   `json:"exchangeRatio"`
	DurationMin       float64   `json:"durationMin"`
	ChlorineDeviation float64   `json:"chlorineDeviation"`
}

type WaterSample struct {
	ID                 string        `json:"id"`
	BatchID            string        `json:"batchId"`
	SamplingPoint      string        `json:"samplingPoint"`
	Witness            string        `json:"witness"`
	Verdict            string        `json:"verdict"`
	SupersedesSampleID string        `json:"supersedesSampleId,omitempty"`
	CorrectiveActionID string        `json:"correctiveActionId,omitempty"`
	Current            bool          `json:"current"`
	SampledAt          time.Time     `json:"sampledAt"`
	TurbidityNTU       float64       `json:"turbidityNtu"`
	ChlorineMgL        float64       `json:"chlorineMgL"`
	ColonyCFUML        float64       `json:"colonyCfuMl"`
	Checks             []SampleCheck `json:"checks"`
}

type SampleCheck struct {
	Parameter string  `json:"parameter"`
	Value     float64 `json:"value"`
	Limit     string  `json:"limit"`
	Verdict   string  `json:"verdict"`
	Reason    string  `json:"reason"`
}

type SampleDelta struct {
	FromSampleID string  `json:"fromSampleId"`
	ToSampleID   string  `json:"toSampleId"`
	TurbidityNTU float64 `json:"turbidityNtu"`
	ChlorineMgL  float64 `json:"chlorineMgL"`
	ColonyCFUML  float64 `json:"colonyCfuMl"`
	BecamePass   bool    `json:"becamePass"`
}

type SamplePointHistory struct {
	SamplingPoint   string        `json:"samplingPoint"`
	Samples         []WaterSample `json:"samples"`
	CurrentSampleID string        `json:"currentSampleId,omitempty"`
	Deltas          []SampleDelta `json:"deltas"`
}

type WaterQualityConclusion struct {
	Category       string              `json:"category"`
	QualityStatus  string              `json:"qualityStatus"`
	WorkflowStatus string              `json:"workflowStatus"`
	Message        string              `json:"message"`
	PointIssues    map[string][]string `json:"pointIssues"`
}

type CorrectiveAction struct {
	ID               string    `json:"id"`
	BatchID          string    `json:"batchId"`
	SourceSampleID   string    `json:"sourceSampleId"`
	Reason           string    `json:"reason"`
	Measure          string    `json:"measure"`
	Status           string    `json:"status"`
	ClosedBySampleID string    `json:"closedBySampleId,omitempty"`
	AffectedPoints   []string  `json:"affectedPoints"`
	CreatedAt        time.Time `json:"createdAt"`
	ClosedAt         time.Time `json:"closedAt,omitempty"`
}

type CorrectivePointTask struct {
	SamplingPoint   string    `json:"samplingPoint"`
	Status          string    `json:"status"`
	SourceSampleID  string    `json:"sourceSampleId,omitempty"`
	CurrentSampleID string    `json:"currentSampleId,omitempty"`
	LastOperatedAt  time.Time `json:"lastOperatedAt,omitempty"`
}

type CorrectiveProgress struct {
	ActionID          string                `json:"actionId"`
	CompletedPoints   int                   `json:"completedPoints"`
	TotalPoints       int                   `json:"totalPoints"`
	CompletionPercent float64               `json:"completionPercent"`
	NextPoints        []string              `json:"nextPoints"`
	Blockers          []string              `json:"blockers"`
	Tasks             []CorrectivePointTask `json:"tasks"`
}

type ReviewDecision struct {
	Approved    bool      `json:"approved"`
	Actor       string    `json:"actor"`
	Reason      string    `json:"reason,omitempty"`
	TargetStage Status    `json:"targetStage,omitempty"`
	DecidedAt   time.Time `json:"decidedAt"`
	Valid       bool      `json:"valid"`
}

type ReleaseCertificate struct {
	ID                 string    `json:"id"`
	BatchID            string    `json:"batchId"`
	BatchVersion       int       `json:"batchVersion"`
	IssuedAt           time.Time `json:"issuedAt"`
	Issuer             string    `json:"issuer"`
	BusinessDigest     string    `json:"businessDigest"`
	VerificationDigest string    `json:"verificationDigest"`
	AuditSequence      int64     `json:"auditSequence"`
}

type AuditEvent struct {
	Sequence int64     `json:"sequence"`
	BatchID  string    `json:"batchId"`
	Action   string    `json:"action"`
	Actor    string    `json:"actor"`
	Detail   string    `json:"detail"`
	At       time.Time `json:"at"`
}

type SampleCoverage struct {
	SamplingPoint string `json:"samplingPoint"`
	SampleID      string `json:"sampleId,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Covered       bool   `json:"covered"`
}

func NewBatch(segmentID, source, creator string, profile SegmentProfile) *Batch {
	now := time.Now().UTC()
	return &Batch{
		ID: fmt.Sprintf("b-%d", now.UnixNano()), SegmentID: strings.TrimSpace(segmentID),
		WaterSource: strings.TrimSpace(source), CreatedBy: strings.TrimSpace(creator), Status: StatusDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now, Profile: &profile,
		Rounds: []ExecutionRound{}, Samples: []WaterSample{}, Actions: []CorrectiveAction{}, NextSequence: 1,
	}
}

func (b *Batch) Touch()             { b.Version++; b.UpdatedAt = time.Now().UTC() }
func (b *Batch) CanEditDraft() bool { return b.Status == StatusDraft }

func (b *Batch) UpdateDraft(segmentID, source string, profile SegmentProfile) error {
	if !b.CanEditDraft() {
		return ErrImmutable
	}
	profile.BatchID = b.ID
	b.SegmentID = strings.TrimSpace(segmentID)
	b.WaterSource = strings.TrimSpace(source)
	b.Profile = &profile
	b.Touch()
	return nil
}

func (b *Batch) Freeze(plan Plan) error {
	if !b.CanEditDraft() {
		return ErrInvalidState
	}
	plan.Version = 1
	plan.FrozenAt = time.Now().UTC()
	b.Plan = &plan
	b.Status = StatusFrozen
	b.Touch()
	return nil
}

// StartExecution 保留显式开工能力；通常首轮写入会自动完成同一状态迁移。
func (b *Batch) StartExecution() error {
	if b.Status != StatusFrozen {
		return ErrInvalidState
	}
	b.Status = StatusExecuting
	b.Touch()
	return nil
}

func (b *Batch) AddRound(r ExecutionRound) error {
	if b.Status != StatusFrozen && b.Status != StatusExecuting {
		return ErrInvalidState
	}
	if r.Sequence != len(b.Rounds)+1 {
		return fmt.Errorf("%w: 下一轮次序号应为 %d", ErrConflict, len(b.Rounds)+1)
	}
	if b.Status == StatusFrozen {
		b.Status = StatusExecuting
	}
	b.Rounds = append(b.Rounds, r)
	b.Touch()
	return nil
}

func (b *Batch) FinishExecution() error {
	if b.Status != StatusExecuting {
		return ErrInvalidState
	}
	if len(b.Rounds) == 0 {
		return errors.New("尚无现场轮次，不能结束执行")
	}
	for _, r := range b.Rounds {
		if r.Result != "pass" {
			return fmt.Errorf("第 %d 轮未达标: %s", r.Sequence, r.ResultReason)
		}
	}
	b.Status = StatusSampling
	b.ExecutionFinishedAt = time.Now().UTC()
	b.Touch()
	return nil
}

func (b *Batch) AddSample(s WaterSample) error {
	if b.Status != StatusSampling && b.Status != StatusRemediation {
		return ErrInvalidState
	}
	b.Samples = append(b.Samples, s)
	b.Touch()
	return nil
}

func (b *Batch) AddSamples(samples []WaterSample) error {
	if b.Status != StatusSampling && b.Status != StatusRemediation {
		return ErrInvalidState
	}
	b.Samples = append(b.Samples, samples...)
	b.Touch()
	return nil
}

func (b *Batch) CreateAction(a CorrectiveAction) error {
	if b.Status != StatusSampling && b.Status != StatusRemediation {
		return ErrInvalidState
	}
	a.Status = "open"
	a.CreatedAt = time.Now().UTC()
	b.Actions = append(b.Actions, a)
	b.Status = StatusRemediation
	b.Touch()
	return nil
}

func (b *Batch) ReviewApprove(actor string) error {
	if b.Status != StatusReview {
		return ErrInvalidState
	}
	b.ReviewedBy = strings.TrimSpace(actor)
	b.Review = &ReviewDecision{Approved: true, Actor: b.ReviewedBy, DecidedAt: time.Now().UTC(), Valid: true}
	b.Touch()
	return nil
}

func (b *Batch) ReviewReturn(actor, reason string, target Status) error {
	if b.Status != StatusReview {
		return ErrInvalidState
	}
	b.ReviewedBy = ""
	b.Review = &ReviewDecision{Approved: false, Actor: strings.TrimSpace(actor), Reason: strings.TrimSpace(reason), TargetStage: target, DecidedAt: time.Now().UTC(), Valid: false}
	for i := range b.Samples {
		b.Samples[i].Current = false
	}
	b.Status = target
	b.Touch()
	return nil
}

func (b *Batch) Release(cert ReleaseCertificate) error {
	if b.Status != StatusReview || b.Review == nil || !b.Review.Approved || !b.Review.Valid || b.Certificate != nil {
		return ErrInvalidState
	}
	b.Certificate = &cert
	b.Status = StatusReleased
	b.Touch()
	return nil
}

func (b *Batch) RefreshDerived() {
	b.LastCompletedSequence = len(b.Rounds)
	b.NextSequence = len(b.Rounds) + 1
	b.CanContinue = b.Status == StatusFrozen || b.Status == StatusExecuting
	b.CoverageMatrix = nil
	b.PendingSamplingPoints = nil
	if b.Plan == nil {
		return
	}
	current := map[string]WaterSample{}
	for _, sample := range b.Samples {
		if sample.Current {
			current[sample.SamplingPoint] = sample
		}
	}
	for _, point := range b.Plan.SamplingPoints {
		item := SampleCoverage{SamplingPoint: point}
		if sample, ok := current[point]; ok {
			item.SampleID, item.Verdict = sample.ID, sample.Verdict
			item.Covered = sample.Verdict == "pass"
		}
		if !item.Covered {
			b.PendingSamplingPoints = append(b.PendingSamplingPoints, point)
		}
		b.CoverageMatrix = append(b.CoverageMatrix, item)
	}
}
