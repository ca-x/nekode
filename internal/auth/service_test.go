package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/ca-x/nekode/internal/storage"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !VerifyPassword(hash, "secret") {
		t.Fatal("VerifyPassword() = false, want true")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("VerifyPassword(wrong) = true, want false")
	}
}

func TestBootstrapLoginAuthenticate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	service := New(store)

	created, err := service.Bootstrap(ctx, "admin", "secret", "Admin")
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if created.Token == "" {
		t.Fatal("Bootstrap() token is empty")
	}
	if _, err := service.Bootstrap(ctx, "other", "secret", "Other"); !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("second Bootstrap() error = %v, want %v", err, ErrBootstrapClosed)
	}

	loggedIn, err := service.Login(ctx, "admin", "secret")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	user, session, err := service.Authenticate(ctx, loggedIn.Token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if user.Username != "admin" || session.UserID != user.ID {
		t.Fatalf("Authenticate() user=%+v session=%+v", user, session)
	}
	if err := service.Logout(ctx, session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, _, err := service.Authenticate(ctx, loggedIn.Token); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Authenticate(after logout) error = %v, want %v", err, ErrInvalidCredential)
	}
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("auth_test")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
