package imbinding

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/storage"
)

const (
	MethodQRCode = imadapter.BindingMethodQRCode

	StatusPending = "pending"
	StatusScanned = "scanned"
	StatusBound   = "bound"
	StatusExpired = "expired"
	StatusFailed  = "failed"
)

var (
	ErrEndpointUnsupported = errors.New("endpoint does not support binding method")
	ErrInvalidStatus       = errors.New("invalid binding session status")
	ErrSessionNotFound     = errors.New("binding session not found")
)

type Session struct {
	ID          string `json:"id"`
	EndpointID  string `json:"endpointId"`
	Provider    string `json:"provider"`
	Method      string `json:"method"`
	Status      string `json:"status"`
	QRPayload   string `json:"qrPayload,omitempty"`
	QRImageURL  string `json:"qrImageUrl,omitempty"`
	ExpiresUnix int64  `json:"expiresUnix"`
	CreatedUnix int64  `json:"createdUnix"`
	UpdatedUnix int64  `json:"updatedUnix"`
	Detail      string `json:"detail,omitempty"`
}

type Patch struct {
	Status      string
	QRPayload   string
	QRImageURL  string
	ExpiresUnix int64
	Detail      string
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]Session
	ttl      time.Duration
	now      func() time.Time
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Store{sessions: map[string]Session{}, ttl: ttl, now: time.Now}
}

func (s *Store) Create(endpoint storage.InteractionEndpoint, method string) (Session, error) {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		method = MethodQRCode
	}
	if !strings.EqualFold(endpoint.Kind, "im") || !imadapter.SupportsBindingMethod(endpoint.Provider, method) {
		return Session{}, ErrEndpointUnsupported
	}
	now := s.now().Unix()
	sessionID := newSessionID()
	session := Session{
		ID:          sessionID,
		EndpointID:  endpoint.ID,
		Provider:    endpoint.Provider,
		Method:      method,
		Status:      StatusPending,
		QRPayload:   fmt.Sprintf("nekode://im-bind/%s", sessionID),
		ExpiresUnix: now + int64(s.ttl/time.Second),
		CreatedUnix: now,
		UpdatedUnix: now,
		Detail:      "Waiting for provider adapter QR ticket.",
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return session, nil
}

func (s *Store) Update(endpoint storage.InteractionEndpoint, sessionID string, patch Patch) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.getLocked(endpoint, sessionID)
	if err != nil {
		return Session{}, err
	}
	if status := strings.ToLower(strings.TrimSpace(patch.Status)); status != "" {
		if !validStatus(status) {
			return Session{}, ErrInvalidStatus
		}
		session.Status = status
	}
	if strings.TrimSpace(patch.QRPayload) != "" {
		session.QRPayload = strings.TrimSpace(patch.QRPayload)
	}
	if strings.TrimSpace(patch.QRImageURL) != "" {
		session.QRImageURL = strings.TrimSpace(patch.QRImageURL)
	}
	if patch.ExpiresUnix > 0 {
		session.ExpiresUnix = patch.ExpiresUnix
	}
	if strings.TrimSpace(patch.Detail) != "" {
		session.Detail = strings.TrimSpace(patch.Detail)
	}
	session.UpdatedUnix = s.now().Unix()
	s.sessions[session.ID] = session
	return session, nil
}

func (s *Store) Get(endpoint storage.InteractionEndpoint, sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.getLocked(endpoint, sessionID)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) Cancel(endpoint storage.InteractionEndpoint, sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.getLocked(endpoint, sessionID)
	if err != nil {
		return Session{}, err
	}
	session.Status = StatusFailed
	session.Detail = "Binding session canceled."
	session.UpdatedUnix = s.now().Unix()
	s.sessions[session.ID] = session
	return session, nil
}

func (s *Store) getLocked(endpoint storage.InteractionEndpoint, sessionID string) (Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	session, ok := s.sessions[sessionID]
	if !ok || session.EndpointID != endpoint.ID {
		return Session{}, ErrSessionNotFound
	}
	now := s.now().Unix()
	if session.Status == StatusPending && session.ExpiresUnix <= now {
		session.Status = StatusExpired
		session.Detail = "Binding session expired."
		session.UpdatedUnix = now
		s.sessions[session.ID] = session
	}
	return session, nil
}

func newSessionID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ibs_%d", time.Now().UnixNano())
	}
	return "ibs_" + hex.EncodeToString(buf[:])
}

func validStatus(status string) bool {
	switch status {
	case StatusPending, StatusScanned, StatusBound, StatusExpired, StatusFailed:
		return true
	default:
		return false
	}
}
