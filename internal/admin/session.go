package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
)

const (
	sessionCookieName = "kiosk_admin_session"
	// Admin session expires after this idle period (sliding on each request).
	sessionTTL = 2 * time.Minute
)

type session struct {
	username  string
	expiresAt time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]session)}
}

func (s *SessionStore) Create(username string) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.sessions[token] = session{
		username:  username,
		expiresAt: time.Now().Add(sessionTTL),
	}
	return token, nil
}

// Get returns the username for a valid session and extends its TTL (sliding idle).
func (s *SessionStore) Get(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	now := time.Now()
	if now.After(sess.expiresAt) {
		delete(s.sessions, token)
		return "", false
	}
	sess.expiresAt = now.Add(sessionTTL)
	s.sessions[token] = sess
	return sess.username, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *SessionStore) cleanupLocked(now time.Time) {
	for token, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, token)
		}
	}
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func credentialsMatch(cfg config.AdminConfig, username, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.Username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(cfg.Password)) == 1
	return userOK && passOK
}

func setSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, token, int(sessionTTL.Seconds()), "/", "", false, true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", false, true)
}

func sessionTokenFromRequest(c *gin.Context) string {
	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return token
}
