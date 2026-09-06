package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"webssh/internal/store"
)

const CookieName = "webssh_session"

type Session struct {
	ID        string
	CSRF      string
	User      store.User
	ExpiresAt time.Time
	LastSeen  time.Time
}
type attempt struct {
	Count        int
	Last         time.Time
	BlockedUntil time.Time
}
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	attempts map[string]attempt
	idle     time.Duration
	secure   bool
}

func NewManager(idle time.Duration, secure bool) *Manager {
	return &Manager{sessions: map[string]*Session{}, attempts: map[string]attempt{}, idle: idle, secure: secure}
}
func Token(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func HashPassword(password string) (string, error) {
	b, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), e
}
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Login(w http.ResponseWriter, u store.User) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &Session{ID: Token(32), CSRF: Token(24), User: u, ExpiresAt: time.Now().Add(m.idle), LastSeen: time.Now()}
	m.sessions[s.ID] = s
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: s.ID, Path: "/", HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteStrictMode, MaxAge: int(m.idle.Seconds())})
	return s
}
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie(CookieName); e == nil {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func (m *Manager) Get(r *http.Request) (*Session, bool) {
	c, e := r.Cookie(CookieName)
	if e != nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[c.Value]
	if !ok {
		return nil, false
	}
	now := time.Now()
	if now.After(s.ExpiresAt) {
		delete(m.sessions, c.Value)
		return nil, false
	}
	s.LastSeen = now
	s.ExpiresAt = now.Add(m.idle)
	copy := *s
	return &copy, true
}
func (m *Manager) AllowLogin(key string) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[key]
	now := time.Now()
	if now.Before(a.BlockedUntil) {
		return false, time.Until(a.BlockedUntil)
	}
	if now.Sub(a.Last) > 10*time.Minute {
		delete(m.attempts, key)
	}
	return true, 0
}
func (m *Manager) FailedLogin(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[key]
	if time.Since(a.Last) > 10*time.Minute {
		a.Count = 0
	}
	a.Count++
	a.Last = time.Now()
	if a.Count >= 5 {
		d := time.Duration(1<<min(a.Count-5, 6)) * time.Second
		a.BlockedUntil = time.Now().Add(d)
	}
	m.attempts[key] = a
}
func (m *Manager) ClearAttempts(key string) { m.mu.Lock(); delete(m.attempts, key); m.mu.Unlock() }
