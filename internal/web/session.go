package web

import (
	"sync"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/service"
)

// Session holds a logged-in principal, including the unwrapped data key, in memory only.
// ID, CSRF, P, CreatedAt and LastSeen are set once at creation (LastSeen is then only
// touched by SessionStore.Get, itself serialized by the store's mutex) and may be read
// without locking. The remaining fields are mutated by concurrent htmx requests on the
// same session, so they live behind mu and are only reachable through the accessors below.
type Session struct {
	ID        string
	CSRF      string
	P         service.Principal
	CreatedAt time.Time
	LastSeen  time.Time

	mu         sync.Mutex
	lastSync   time.Time
	syncErrors []string
	flash      string
	imp        any // staging area for the CSV import (Task 14)
}

// TakeFlash returns the pending flash message, if any, and clears it.
func (s *Session) TakeFlash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.flash
	s.flash = ""
	return f
}

// SetFlash sets the flash message to be shown on the next render.
func (s *Session) SetFlash(msg string) {
	s.mu.Lock()
	s.flash = msg
	s.mu.Unlock()
}

// ShouldSync atomically checks whether a sync is due and, if so, claims it by
// stamping lastSync as now. Only one of several concurrent callers will see true.
func (s *Session) ShouldSync(interval time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastSync) < interval {
		return false
	}
	s.lastSync = time.Now()
	return true
}

// MarkSynced records that a sync just completed, e.g. right after LinkSimpleFIN.
func (s *Session) MarkSynced() {
	s.mu.Lock()
	s.lastSync = time.Now()
	s.mu.Unlock()
}

// ForceStale pushes lastSync into the past so the next ShouldSync call succeeds.
// Test helper only.
func (s *Session) ForceStale() {
	s.mu.Lock()
	s.lastSync = time.Now().Add(-24 * time.Hour)
	s.mu.Unlock()
}

// SetSyncErrors records the errors (if any) from the most recent sync attempt.
func (s *Session) SetSyncErrors(errs []string) {
	s.mu.Lock()
	s.syncErrors = errs
	s.mu.Unlock()
}

// SyncErrors returns a copy of the errors from the most recent sync attempt.
func (s *Session) SyncErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.syncErrors))
	copy(out, s.syncErrors)
	return out
}

// SetImport stashes the pending CSV import preview (or nil to clear it).
func (s *Session) SetImport(v any) {
	s.mu.Lock()
	s.imp = v
	s.mu.Unlock()
}

// Import returns the pending CSV import preview, if any.
func (s *Session) Import() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imp
}

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	idle     time.Duration
	absolute time.Duration
}

func NewSessionStore(idle, absolute time.Duration) *SessionStore {
	return &SessionStore{sessions: map[string]*Session{}, idle: idle, absolute: absolute}
}

func (st *SessionStore) Create(p service.Principal) (*Session, error) {
	id, err := crypto.RandomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := crypto.RandomToken(16)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := &Session{ID: id, CSRF: csrf, P: p, CreatedAt: now, LastSeen: now}
	st.mu.Lock()
	st.sessions[id] = s
	st.mu.Unlock()
	return s, nil
}

// Get returns the session if it exists and has not expired, touching LastSeen.
func (st *SessionStore) Get(id string) (*Session, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	if !ok {
		return nil, false
	}
	now := time.Now()
	if now.Sub(s.LastSeen) > st.idle || now.Sub(s.CreatedAt) > st.absolute {
		delete(st.sessions, id)
		return nil, false
	}
	s.LastSeen = now
	return s, true
}

func (st *SessionStore) Delete(id string) {
	st.mu.Lock()
	delete(st.sessions, id)
	st.mu.Unlock()
}
