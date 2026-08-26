package store

import (
	"aquaflush-release-workbench/internal/domain"
	"encoding/json"
)

type idempotencyRecord struct {
	Digest string          `json:"digest"`
	Result json.RawMessage `json:"result"`
}

type fileData struct {
	Batches     map[string]*domain.Batch     `json:"batches"`
	Events      []domain.AuditEvent          `json:"events"`
	Idempotency map[string]idempotencyRecord `json:"idempotency"`
}
