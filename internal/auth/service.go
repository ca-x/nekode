package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

var (
	ErrBootstrapClosed   = errors.New("bootstrap is closed")
	ErrInvalidCredential = errors.New("invalid credential")
)

type Service struct {
	store      *storage.Store
	sessionTTL time.Duration
}

type SessionToken struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expiresUnix"`
	User      storage.User `json:"user"`
}

func New(store *storage.Store) *Service {
	return &Service{
		store:      store,
		sessionTTL: 30 * 24 * time.Hour,
	}
}

func (s *Service) Bootstrap(ctx context.Context, username, password, displayName string) (SessionToken, error) {
	passwordHash, err := HashPassword(password)
	if err != nil {
		return SessionToken{}, err
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}
	user, err := s.store.CreateFirstAdmin(ctx, storage.User{
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         "admin",
	})
	if errors.Is(err, storage.ErrConflict) {
		return SessionToken{}, ErrBootstrapClosed
	}
	if err != nil {
		return SessionToken{}, err
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Login(ctx context.Context, username, password string) (SessionToken, error) {
	user, err := s.store.GetUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return SessionToken{}, ErrInvalidCredential
	}
	if !VerifyPassword(user.PasswordHash, password) {
		return SessionToken{}, ErrInvalidCredential
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Authenticate(ctx context.Context, token string) (storage.User, storage.Session, error) {
	tokenHash := HashToken(token)
	session, err := s.store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return storage.User{}, storage.Session{}, ErrInvalidCredential
	}
	user, err := s.store.GetUser(ctx, session.UserID)
	if err != nil {
		return storage.User{}, storage.Session{}, ErrInvalidCredential
	}
	return user, session, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.store.DeleteSession(ctx, sessionID)
}

func (s *Service) createUserAndSession(ctx context.Context, username, password, displayName, role string) (SessionToken, error) {
	passwordHash, err := HashPassword(password)
	if err != nil {
		return SessionToken{}, err
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}
	user, err := s.store.CreateUser(ctx, storage.User{
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         role,
	})
	if err != nil {
		return SessionToken{}, err
	}
	return s.issueSession(ctx, user)
}

func (s *Service) issueSession(ctx context.Context, user storage.User) (SessionToken, error) {
	token, err := RandomToken()
	if err != nil {
		return SessionToken{}, err
	}
	expires := time.Now().Add(s.sessionTTL).Unix()
	if _, err := s.store.CreateSession(ctx, storage.Session{
		TokenHash:   HashToken(token),
		UserID:      user.ID,
		ExpiresUnix: expires,
	}); err != nil {
		return SessionToken{}, err
	}
	return SessionToken{
		Token:     token,
		ExpiresAt: expires,
		User:      user,
	}, nil
}

func RandomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
