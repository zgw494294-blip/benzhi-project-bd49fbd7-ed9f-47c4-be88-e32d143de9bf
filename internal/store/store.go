package store

import (
	"aquaflush-release-workbench/internal/domain"
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
	data fileData
}

func New(path string) (*Store, error) {
	s := &Store{path: path, data: fileData{Batches: map[string]*domain.Batch{}, Events: []domain.AuditEvent{}, Idempotency: map[string]idempotencyRecord{}}}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err = json.Unmarshal(raw, &s.data); err != nil {
			return nil, err
		}
	}
	if s.data.Batches == nil {
		s.data.Batches = map[string]*domain.Batch{}
	}
	if s.data.Events == nil {
		s.data.Events = []domain.AuditEvent{}
	}
	if s.data.Idempotency == nil {
		s.data.Idempotency = map[string]idempotencyRecord{}
	}
	return s, nil
}

func (s *Store) Close() error { return nil }

func (s *Store) persist(data fileData) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Save(ctx context.Context, b *domain.Batch, action, actor string) error {
	_, _, err := s.SaveDetailed(ctx, b, action, actor, "状态变更", "", "", "")
	return err
}

func (s *Store) SaveDetailed(ctx context.Context, b *domain.Batch, action, actor, detail, scope, key, digest string) (*domain.Batch, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	idemKey := ""
	if key != "" {
		idemKey = scope + ":" + key
	}
	if idemKey != "" {
		if record, ok := s.data.Idempotency[idemKey]; ok {
			if record.Digest != digest {
				return nil, false, domain.ErrConflict
			}
			var saved domain.Batch
			if err := json.Unmarshal(record.Result, &saved); err != nil {
				return nil, false, err
			}
			return &saved, true, nil
		}
	}
	old, exists := s.data.Batches[b.ID]
	if exists {
		if old.Version != b.Version-1 {
			return nil, false, domain.ErrConflict
		}
	} else if b.Version != 1 {
		return nil, false, domain.ErrConflict
	}
	candidate := cloneData(s.data)
	saved := cloneBatch(b)
	saved.RefreshDerived()
	candidate.Batches[b.ID] = saved
	sequence := int64(1)
	for _, event := range candidate.Events {
		if event.BatchID == b.ID && event.Sequence >= sequence {
			sequence = event.Sequence + 1
		}
	}
	candidate.Events = append(candidate.Events, domain.AuditEvent{Sequence: sequence, BatchID: b.ID, Action: action, Actor: actor, Detail: detail, At: time.Now().UTC()})
	if idemKey != "" {
		raw, _ := json.Marshal(saved)
		candidate.Idempotency[idemKey] = idempotencyRecord{Digest: digest, Result: raw}
	}
	s.mu.Unlock()
	locked = false
	if err := s.persist(candidate); err != nil {
		return nil, false, err
	}
	s.data = candidate
	return cloneBatch(saved), false, nil
}

func (s *Store) Replay(ctx context.Context, scope, key, digest string) (*domain.Batch, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.data.Idempotency[scope+":"+key]
	if !ok {
		return nil, false, nil
	}
	if record.Digest != digest {
		return nil, false, domain.ErrConflict
	}
	var saved domain.Batch
	if err := json.Unmarshal(record.Result, &saved); err != nil {
		return nil, false, err
	}
	return &saved, true, nil
}

func (s *Store) Get(ctx context.Context, id string) (*domain.Batch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data.Batches[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := cloneBatch(b)
	out.RefreshDerived()
	return out, nil
}

func (s *Store) List(ctx context.Context) ([]*domain.Batch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.Batch, 0, len(s.data.Batches))
	for _, batch := range s.data.Batches {
		copy := cloneBatch(batch)
		copy.RefreshDerived()
		out = append(out, copy)
	}
	return out, nil
}

func (s *Store) Events(ctx context.Context, id string) ([]domain.AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.AuditEvent{}
	for _, event := range s.data.Events {
		if event.BatchID == id {
			out = append(out, event)
		}
	}
	return out, nil
}

func (s *Store) NextAuditSequence(ctx context.Context, id string) (int64, error) {
	events, err := s.Events(ctx, id)
	if err != nil {
		return 0, err
	}
	next := int64(1)
	for _, event := range events {
		if event.Sequence >= next {
			next = event.Sequence + 1
		}
	}
	return next, nil
}
