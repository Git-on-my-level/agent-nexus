package router

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type StateStore struct {
	path string

	mu     sync.Mutex
	values map[string]any
}

func NewStateStore(path string) (*StateStore, error) {
	store := &StateStore{
		path:   path,
		values: map[string]any{},
	}
	if strings.TrimSpace(path) == "" {
		return store, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read router state: %w", err)
	}
	if err := json.Unmarshal(payload, &store.values); err != nil {
		return nil, fmt.Errorf("decode router state: %w", err)
	}
	return store, nil
}

func (s *StateStore) LastEventID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return anyString(s.values["last_event_id"])
}

func (s *StateStore) SetLastEventID(value string) error {
	return s.Update(map[string]any{"last_event_id": value})
}

func (s *StateStore) LastEventTS() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return anyString(s.values["last_event_ts"])
}

func (s *StateStore) SetLastEventCursor(ts string, id string) error {
	return s.Update(map[string]any{
		"last_event_ts": ts,
		"last_event_id": id,
	})
}

func (s *StateStore) Update(updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range updates {
		if value == nil {
			continue
		}
		s.values[key] = value
	}
	return s.saveLocked()
}

func (s *StateStore) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]any, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out
}

func (s *StateStore) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	content, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal router state: %w", err)
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("mkdir router state dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return fmt.Errorf("write router state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename router state: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}
