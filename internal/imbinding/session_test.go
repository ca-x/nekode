package imbinding

import (
	"strings"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

func TestStoreCreatesQRCodeSessionOnlyForCapableProvider(t *testing.T) {
	store := NewStore(time.Minute)
	endpoint := storage.InteractionEndpoint{ID: "iep_weixin", Kind: "im", Provider: "weixin"}

	session, err := store.Create(endpoint, MethodQRCode)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if session.ID == "" ||
		session.EndpointID != endpoint.ID ||
		session.Provider != "weixin" ||
		session.Method != MethodQRCode ||
		session.Status != StatusPending ||
		!strings.Contains(session.QRPayload, session.ID) ||
		session.ExpiresUnix == 0 {
		t.Fatalf("session = %+v, want pending QR session bound to endpoint", session)
	}

	if _, err := store.Create(storage.InteractionEndpoint{ID: "iep_feishu", Kind: "im", Provider: "feishu"}, MethodQRCode); err != ErrEndpointUnsupported {
		t.Fatalf("Create(feishu QR) error = %v, want ErrEndpointUnsupported", err)
	}
}

func TestStoreUpdatesCancelsAndExpiresSession(t *testing.T) {
	store := NewStore(time.Second)
	now := time.Unix(100, 0)
	store.now = func() time.Time { return now }
	endpoint := storage.InteractionEndpoint{ID: "iep_weixin", Kind: "im", Provider: "weixin"}
	session, err := store.Create(endpoint, MethodQRCode)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := store.Update(endpoint, session.ID, Patch{Status: StatusScanned, QRImageURL: "https://example.test/qr.png", Detail: "scanned"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Status != StatusScanned || updated.QRImageURL == "" || updated.Detail != "scanned" {
		t.Fatalf("updated session = %+v, want scanned QR session", updated)
	}

	canceled, err := store.Cancel(endpoint, session.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled.Status != StatusFailed {
		t.Fatalf("canceled status = %q, want failed", canceled.Status)
	}

	expiring, err := store.Create(endpoint, MethodQRCode)
	if err != nil {
		t.Fatalf("Create(expiring) error = %v", err)
	}
	now = now.Add(2 * time.Second)
	expired, err := store.Get(endpoint, expiring.ID)
	if err != nil {
		t.Fatalf("Get(expired) error = %v", err)
	}
	if expired.Status != StatusExpired {
		t.Fatalf("expired status = %q, want expired", expired.Status)
	}
}
