package auth

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const DefaultPasskeySessionTTL = 5 * time.Minute

type PasskeySessionKind string

const (
	PasskeySessionKindRegistration      PasskeySessionKind = "registration"
	PasskeySessionKindLoginKnown        PasskeySessionKind = "login_known"
	PasskeySessionKindLoginDiscoverable PasskeySessionKind = "login_discoverable"
)

type PasskeySession struct {
	ID              string
	Kind            PasskeySessionKind
	DisplayName     string
	UserHandle      []byte
	SessionData     webauthn.SessionData
	OnboardingClaim OnboardingClaim
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type PasskeySessionStore struct {
	mu       sync.Mutex
	sessions map[string]PasskeySession
	ttl      time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func NewPasskeySessionStore(ttl time.Duration) *PasskeySessionStore {
	if ttl <= 0 {
		ttl = DefaultPasskeySessionTTL
	}
	store := &PasskeySessionStore{
		sessions: make(map[string]PasskeySession),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go store.reapLoop()
	return store
}

func (s *PasskeySessionStore) Save(session PasskeySession) string {
	if s == nil {
		return ""
	}

	now := time.Now().UTC()
	if session.ID == "" {
		session.ID = "passkey_session_" + uuid.NewString()
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = now.Add(s.ttl)
	}
	if session.SessionData.Expires.IsZero() {
		session.SessionData.Expires = session.ExpiresAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return session.ID
}

func (s *PasskeySessionStore) Consume(id string) (PasskeySession, bool) {
	if s == nil {
		return PasskeySession{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return PasskeySession{}, false
	}
	delete(s.sessions, id)
	if session.ExpiresAt.Before(time.Now().UTC()) {
		return PasskeySession{}, false
	}
	return session, true
}

func (s *PasskeySessionStore) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.doneCh:
		return
	default:
	}
	close(s.stopCh)
	<-s.doneCh
}

func (s *PasskeySessionStore) reapLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer close(s.doneCh)

	for {
		select {
		case <-ticker.C:
			s.reapExpired()
		case <-s.stopCh:
			return
		}
	}
}

func (s *PasskeySessionStore) reapExpired() {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, session := range s.sessions {
		if session.ExpiresAt.Before(now) {
			delete(s.sessions, id)
		}
	}
}
