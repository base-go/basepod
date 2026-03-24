// Package auth provides authentication for the basepod API.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Session represents an authenticated session
type Session struct {
	Token     string
	UserID    string // empty for legacy admin sessions
	UserEmail string
	UserRole  string // "admin", "deployer", "viewer"
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Manager handles authentication and sessions
type Manager struct {
	passwordHash string
	sessions     map[string]*Session
	mu           sync.RWMutex
}

// NewManager creates a new auth manager
func NewManager(passwordHash string) *Manager {
	return &Manager{
		passwordHash: passwordHash,
		sessions:     make(map[string]*Session),
	}
}

// hashPasswordLegacy hashes a password using SHA-256 (legacy, kept for backward compatibility)
func hashPasswordLegacy(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword verifies a password against a hash.
// It tries bcrypt first, then falls back to legacy SHA-256 for backward compatibility.
func CheckPassword(hash, password string) bool {
	// Try bcrypt first
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err == nil {
		return true
	}
	// Fall back to legacy SHA-256
	return hash == hashPasswordLegacy(password)
}

// ValidatePassword checks if the password matches the stored hash
func (m *Manager) ValidatePassword(password string) bool {
	if m.passwordHash == "" {
		return false // No password configured - require setup first
	}
	return CheckPassword(m.passwordHash, password)
}

// IsAuthRequired returns true if authentication is configured
func (m *Manager) IsAuthRequired() bool {
	return m.passwordHash != ""
}

// CreateSession creates a new session for authenticated user (legacy admin)
func (m *Manager) CreateSession() (*Session, error) {
	return m.CreateUserSession("", "", "admin")
}

// CreateUserSession creates a session for a specific user
func (m *Manager) CreateUserSession(userID, email, role string) (*Session, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}

	session := &Session{
		Token:     hex.EncodeToString(token),
		UserID:    userID,
		UserEmail: email,
		UserRole:  role,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	m.mu.Lock()
	m.sessions[session.Token] = session
	m.mu.Unlock()

	return session, nil
}

// GetSession returns the session details for a valid token
func (m *Manager) GetSession(token string) *Session {
	if token == "" {
		return nil
	}
	m.mu.RLock()
	session, exists := m.sessions[token]
	m.mu.RUnlock()
	if !exists || time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session
}

// ValidateSession checks if a session token is valid
func (m *Manager) ValidateSession(token string) bool {
	if m.passwordHash == "" {
		return false // No password configured - require setup first
	}

	if token == "" {
		return false
	}

	m.mu.RLock()
	session, exists := m.sessions[token]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(session.ExpiresAt) {
		m.DeleteSession(token)
		return false
	}

	return true
}

// NeedsSetup returns true if no password has been configured yet
func (m *Manager) NeedsSetup() bool {
	return m.passwordHash == ""
}

// SetPassword sets the initial password (only works if no password is set)
func (m *Manager) SetPassword(password string) bool {
	if m.passwordHash != "" {
		return false // Password already set, use UpdatePassword instead
	}
	if password == "" {
		return false // Cannot set empty password
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false
	}
	m.passwordHash = hash
	return true
}

// DeleteSession removes a session
func (m *Manager) DeleteSession(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// UpdatePassword updates the password hash
func (m *Manager) UpdatePassword(newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	m.passwordHash = hash
	return nil
}

// GetPasswordHash returns the current password hash
func (m *Manager) GetPasswordHash() string {
	return m.passwordHash
}

// CleanupExpiredSessions removes expired sessions
func (m *Manager) CleanupExpiredSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for token, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, token)
		}
	}
}
