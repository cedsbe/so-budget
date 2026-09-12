package web

import (
	"sync"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/service"
)

// Session holds a logged-in principal, including the unwrapped data key, in memory only.
type Session struct {
	ID         string
	CSRF       string
	P          service.Principal
	CreatedAt  time.Time
	LastSeen   time.Time
	LastSync   time.Time
	SyncErrors []string
	Flash      string
	Import     any // staging area for the CSV import (Task 14)
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

func (st *SessionStore) Create(p service.Principal) *Session {
	id, _ := crypto.RandomToken(32)
	csrf, _ := crypto.RandomToken(16)
	now := time.Now()
	s := &Session{ID: id, CSRF: csrf, P: p, CreatedAt: now, LastSeen: now}
	st.mu.Lock()
	st.sessions[id] = s
	st.mu.Unlock()
	return s
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
