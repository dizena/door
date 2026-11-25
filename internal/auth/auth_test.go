package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"door/internal/store"
)

func TestDefaultAdminAndForcedPasswordChange(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "door.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	svc := NewService(db, time.Hour)
	if err := svc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin failed: %v", err)
	}

	// Wrong password fails.
	if _, _, err := svc.Login(ctx, "admin", "wrongpass", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	// Default admin/12345678 succeeds and requires password change.
	sess, user, err := svc.Login(ctx, DefaultUsername, DefaultPassword, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("default login failed: %v", err)
	}
	if !user.MustChangePassword {
		t.Fatalf("expected MustChangePassword=true on initial login")
	}

	authed, err := svc.Authenticate(ctx, sess.Token)
	if err != nil || !authed.MustChangePassword {
		t.Fatalf("Authenticate unexpected: user=%+v err=%v", authed, err)
	}

	// Reject weak or default new password.
	if err := svc.ChangePassword(ctx, user.ID, DefaultPassword, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for short password, got %v", err)
	}
	if err := svc.ChangePassword(ctx, user.ID, DefaultPassword, DefaultPassword); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for reusing default password, got %v", err)
	}

	// Valid rotation clears MustChangePassword.
	if err := svc.ChangePassword(ctx, user.ID, DefaultPassword, "DoorPanel#2026"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	authed, err = svc.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Authenticate after rotation failed: %v", err)
	}
	if authed.MustChangePassword {
		t.Fatalf("expected MustChangePassword=false after password rotation")
	}

	// Logout invalidates token.
	if err := svc.Logout(ctx, sess.Token); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized after logout, got %v", err)
	}
}
