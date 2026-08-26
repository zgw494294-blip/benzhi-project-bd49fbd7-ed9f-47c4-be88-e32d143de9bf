package store

import (
	"aquaflush-release-workbench/internal/domain"
	"encoding/json"
)

func cloneBatch(b *domain.Batch) *domain.Batch {
	if b == nil {
		return nil
	}
	raw, _ := json.Marshal(b)
	var cp domain.Batch
	_ = json.Unmarshal(raw, &cp)
	return &cp
}

func cloneData(in fileData) fileData {
	raw, _ := json.Marshal(in)
	var out fileData
	_ = json.Unmarshal(raw, &out)
	if out.Batches == nil {
		out.Batches = map[string]*domain.Batch{}
	}
	if out.Events == nil {
		out.Events = []domain.AuditEvent{}
	}
	if out.Idempotency == nil {
		out.Idempotency = map[string]idempotencyRecord{}
	}
	return out
}
