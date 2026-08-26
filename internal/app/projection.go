package app

import (
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/rules"
	"fmt"
	"sort"
	"time"
)

func projectBatch(b *domain.Batch) error {
	b.RefreshDerived()
	for i := range b.Samples {
		if len(b.Samples[i].Checks) == 0 {
			b.Samples[i].Checks = rules.EvaluateSampleChecks(b.Samples[i], b.Profile)
			b.Samples[i].Verdict = rules.VerdictFromChecks(b.Samples[i].Checks)
		}
	}
	if b.GeometryCheck == nil {
		b.GeometryCheck = rules.CheckGeometry(b.Profile)
	}
	b.ExecutionProgress = rules.ExecutionProgress(b)
	histories, err := sampleHistories(b)
	if err != nil {
		return err
	}
	b.SampleHistories = histories
	b.CorrectiveProgress = correctiveProgress(b)
	b.WaterQuality = waterConclusion(b)
	return nil
}

func refreshProjection(b *domain.Batch) { _ = projectBatch(b) }

func sampleHistories(b *domain.Batch) ([]domain.SamplePointHistory, error) {
	byID := map[string]domain.WaterSample{}
	current := map[string]int{}
	replacedBy := map[string]int{}
	for _, s := range b.Samples {
		if _, exists := byID[s.ID]; exists {
			return nil, fmt.Errorf("%w: 样本编号 %s 重复", domain.ErrDataIntegrity, s.ID)
		}
		byID[s.ID] = s
		if s.Current {
			current[s.SamplingPoint]++
		}
	}
	for point, n := range current {
		if n > 1 {
			return nil, fmt.Errorf("%w: 点位 %s 存在多个当前样本", domain.ErrDataIntegrity, point)
		}
	}
	for _, s := range b.Samples {
		if s.SupersedesSampleID != "" {
			replacedBy[s.SupersedesSampleID]++
			if replacedBy[s.SupersedesSampleID] > 1 {
				return nil, fmt.Errorf("%w: 样本 %s 被多个复检样本替代", domain.ErrDataIntegrity, s.SupersedesSampleID)
			}
			previous, ok := byID[s.SupersedesSampleID]
			if !ok {
				return nil, fmt.Errorf("%w: 样本 %s 的替代引用断裂", domain.ErrDataIntegrity, s.ID)
			}
			if previous.SamplingPoint != s.SamplingPoint {
				return nil, fmt.Errorf("%w: 样本 %s 存在跨点位替代", domain.ErrDataIntegrity, s.ID)
			}
		}
	}
	for _, start := range b.Samples {
		seen := map[string]bool{}
		cursor := start
		for cursor.SupersedesSampleID != "" {
			if seen[cursor.ID] {
				return nil, fmt.Errorf("%w: 样本 %s 的替代链存在循环", domain.ErrDataIntegrity, start.ID)
			}
			seen[cursor.ID] = true
			cursor = byID[cursor.SupersedesSampleID]
		}
	}
	groups := map[string][]domain.WaterSample{}
	for _, s := range b.Samples {
		groups[s.SamplingPoint] = append(groups[s.SamplingPoint], s)
	}
	points := make([]string, 0, len(groups))
	for p := range groups {
		points = append(points, p)
	}
	sort.Strings(points)
	out := []domain.SamplePointHistory{}
	for _, point := range points {
		samples := groups[point]
		sort.SliceStable(samples, func(i, j int) bool {
			if samples[i].SampledAt.Equal(samples[j].SampledAt) {
				return samples[i].ID < samples[j].ID
			}
			return samples[i].SampledAt.Before(samples[j].SampledAt)
		})
		h := domain.SamplePointHistory{SamplingPoint: point, Samples: samples, Deltas: []domain.SampleDelta{}}
		for _, s := range samples {
			if s.Current {
				h.CurrentSampleID = s.ID
			}
			if s.SupersedesSampleID != "" {
				prev := byID[s.SupersedesSampleID]
				h.Deltas = append(h.Deltas, domain.SampleDelta{FromSampleID: prev.ID, ToSampleID: s.ID, TurbidityNTU: roundProjection(s.TurbidityNTU - prev.TurbidityNTU), ChlorineMgL: roundProjection(s.ChlorineMgL - prev.ChlorineMgL), ColonyCFUML: roundProjection(s.ColonyCFUML - prev.ColonyCFUML), BecamePass: prev.Verdict != "pass" && s.Verdict == "pass"})
			}
		}
		out = append(out, h)
	}
	return out, nil
}

func roundProjection(v float64) float64 {
	if v >= 0 {
		return float64(int64(v*1_000_000+.5)) / 1_000_000
	}
	return float64(int64(v*1_000_000-.5)) / 1_000_000
}

func correctiveProgress(b *domain.Batch) []domain.CorrectiveProgress {
	out := []domain.CorrectiveProgress{}
	for _, a := range b.Actions {
		p := domain.CorrectiveProgress{ActionID: a.ID, TotalPoints: len(a.AffectedPoints), NextPoints: []string{}, Blockers: []string{}, Tasks: []domain.CorrectivePointTask{}}
		for _, point := range a.AffectedPoints {
			task := domain.CorrectivePointTask{SamplingPoint: point, Status: "pending_reinspection", LastOperatedAt: a.CreatedAt}
			for i := range b.Samples {
				sample := &b.Samples[i]
				if sample.SamplingPoint == point && !sample.SampledAt.After(a.CreatedAt) && (task.SourceSampleID == "" || sample.SampledAt.After(sampleTimeByID(b, task.SourceSampleID))) {
					task.SourceSampleID = sample.ID
				}
			}
			var latest *domain.WaterSample
			for i := range b.Samples {
				s := &b.Samples[i]
				if s.CorrectiveActionID == a.ID && s.SamplingPoint == point && (latest == nil || s.SampledAt.After(latest.SampledAt)) {
					latest = s
				}
			}
			if latest != nil {
				task.CurrentSampleID = latest.ID
				task.LastOperatedAt = latest.SampledAt
				if latest.Verdict == "pass" {
					task.Status = "reinspection_pass"
					p.CompletedPoints++
				} else {
					task.Status = "reinspection_fail"
				}
			}
			if task.Status != "reinspection_pass" {
				p.NextPoints = append(p.NextPoints, point)
				if task.Status == "pending_reinspection" {
					p.Blockers = append(p.Blockers, fmt.Sprintf("点位 %s 待复检", point))
				} else {
					p.Blockers = append(p.Blockers, fmt.Sprintf("点位 %s 复检仍不合格", point))
				}
			}
			p.Tasks = append(p.Tasks, task)
		}
		if p.TotalPoints > 0 {
			p.CompletionPercent = roundProjection(float64(p.CompletedPoints) / float64(p.TotalPoints) * 100)
		}
		out = append(out, p)
	}
	return out
}

func waterConclusion(b *domain.Batch) domain.WaterQualityConclusion {
	result := domain.WaterQualityConclusion{Category: "pending_sampling", QualityStatus: "pending_sampling", WorkflowStatus: "sampling", Message: "存在待采样点", PointIssues: map[string][]string{}}
	if b.Plan == nil {
		result.Message = "方案尚未冻结，等待进入采样"
		return result
	}
	current := map[string]domain.WaterSample{}
	for _, s := range b.Samples {
		if s.Current {
			current[s.SamplingPoint] = s
		}
	}
	missing := false
	failed := false
	for _, point := range func() []string {
		if b.Plan == nil {
			return nil
		}
		return b.Plan.SamplingPoints
	}() {
		s, ok := current[point]
		if !ok {
			missing = true
			result.PointIssues[point] = []string{"尚无当前样本"}
			continue
		}
		parameters := rules.FailedParameters(s)
		if len(parameters) > 0 {
			failed = true
			result.PointIssues[point] = parameters
		}
	}
	if missing {
		return result
	}
	open := false
	for _, a := range b.Actions {
		if a.Status == "open" {
			open = true
		}
	}
	if open {
		result.Category = "remediation_reinspection"
		result.QualityStatus = "parameter_failed"
		result.WorkflowStatus = "remediation_reinspection"
		result.Message = "整改复检中"
		return result
	}
	if failed {
		result.Category = "awaiting_corrective"
		result.QualityStatus = "parameter_failed"
		result.WorkflowStatus = "awaiting_corrective"
		result.Message = "参数不合格，等待创建整改"
		return result
	}
	result.Category = "all_pass"
	result.QualityStatus = "all_pass"
	result.WorkflowStatus = "all_pass"
	result.Message = "全部采样点参数合格"
	return result
}

func sampleTimeByID(b *domain.Batch, id string) time.Time {
	for _, sample := range b.Samples {
		if sample.ID == id {
			return sample.SampledAt
		}
	}
	return time.Time{}
}
