package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	DefaultUsername = "admin"
	DefaultPassword = "12345678"
	minPasswordLen  = 8
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrWeakPassword       = errors.New("password does not meet policy")
	ErrPasswordChangeReq  = errors.New("initial password must be changed")
)

// User represents an authenticated Door panel operator.
type User struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Session represents a persisted login session.
type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AuditLog records administrative actions.
type AuditLog struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// Service handles user authentication, forced initial password rotation, sessions, and audit logging.
type Service struct {
	db         *sql.DB
	sessionTTL time.Duration
}

// NewService creates an auth Service backed by SQLite.
func NewService(db *sql.DB, sessionTTL time.Duration) *Service {
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}
	return &Service{db: db, sessionTTL: sessionTTL}
}

// EnsureDefaultAdmin seeds admin/12345678 with must_change_password=true when no users exist.
func (s *Service) EnsureDefaultAdmin(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default admin password: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO users (username, password_hash, must_change_password)
VALUES (?, ?, 1)
`, DefaultUsername, string(hash))
	if err != nil {
		return fmt.Errorf("insert default admin: %w", err)
	}
	return nil
}

// Login validates credentials and creates a persistent session token.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (Session, User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return Session{}, User{}, ErrInvalidCredentials
	}

	var (
		user       User
		hash       string
		mustChange int
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, must_change_password, created_at, updated_at
FROM users WHERE username = ?
`, username).Scan(&user.ID, &user.Username, &hash, &mustChange, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, User{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, User{}, fmt.Errorf("query user: %w", err)
	}
	user.MustChangePassword = mustChange == 1

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return Session{}, User{}, ErrInvalidCredentials
	}

	token, err := generateToken(32)
	if err != nil {
		return Session{}, User{}, fmt.Errorf("generate session token: %w", err)
	}

	expiresAt := time.Now().UTC().Add(s.sessionTTL)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (token, user_id, expires_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?)
`, token, user.ID, expiresAt, ip, userAgent)
	if err != nil {
		return Session{}, User{}, fmt.Errorf("insert session: %w", err)
	}

	// Best-effort cleanup of expired sessions.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC())

	return Session{Token: token, UserID: user.ID, ExpiresAt: expiresAt}, user, nil
}

// Authenticate validates a session token and returns the associated User.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, ErrUnauthorized
	}

	var (
		user       User
		mustChange int
		expiresAt  time.Time
	)
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.username, u.must_change_password, u.created_at, u.updated_at, s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token = ?
`, token).Scan(&user.ID, &user.Username, &mustChange, &user.CreatedAt, &user.UpdatedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	if err != nil {
		return User{}, fmt.Errorf("authenticate session: %w", err)
	}

	if time.Now().UTC().After(expiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
		return User{}, ErrUnauthorized
	}

	user.MustChangePassword = mustChange == 1
	return user, nil
}

// Logout invalidates the given session token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// ChangePassword rotates the user's password and clears must_change_password.
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	if len(newPassword) < minPasswordLen {
		return fmt.Errorf("%w: password must be at least %d characters", ErrWeakPassword, minPasswordLen)
	}
	if newPassword == DefaultPassword {
		return fmt.Errorf("%w: new password cannot be the default password", ErrWeakPassword)
	}
	if newPassword == oldPassword {
		return fmt.Errorf("%w: new password must differ from current password", ErrWeakPassword)
	}

	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("load user password: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
UPDATE users
SET password_hash = ?, must_change_password = 0, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, string(newHash), userID)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return nil
}

// RecordAudit writes an audit log entry.
func (s *Service) RecordAudit(ctx context.Context, username, action, target, detail, ip string) {
	if username == "" {
		username = "system"
	}
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO audit_logs (username, action, target, detail, ip)
VALUES (?, ?, ?, ?, ?)
`, username, action, target, detail, ip)
}

// ListAuditLogs returns the most recent audit records.
func (s *Service) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, username, action, target, detail, ip, created_at
FROM audit_logs
ORDER BY id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.Username, &l.Action, &l.Target, &l.Detail, &l.IP, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func generateToken(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
