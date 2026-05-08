package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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

func TestBootstrapAllowsOnlyOneConcurrentAdmin(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	service := New(store)
	var successes int64
	var closed int64
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := service.Bootstrap(ctx, fmt.Sprintf("admin-%d", i), "secret123", "Admin")
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, ErrBootstrapClosed):
				atomic.AddInt64(&closed, 1)
			default:
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Bootstrap() unexpected error = %v", err)
	}
	if successes != 1 || closed != 7 {
		t.Fatalf("successes=%d closed=%d, want 1 and 7", successes, closed)
	}
	count, err := store.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUsers() = %d, want 1", count)
	}
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("auth_test")+"?mode=memory&cache=shared&_fk=1")
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
