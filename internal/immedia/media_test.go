package immedia

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

func TestStorePersistsNormalizedAttachment(t *testing.T) {
	dataDir := t.TempDir()
	createdAt := time.Unix(1700000000, 0)
	stored, err := Store(dataDir, StoreInput{
		Target:    "#general",
		OwnerID:   "user_123",
		Filename:  `../preview".html`,
		MimeType:  "text/html; charset=utf-8",
		Content:   strings.NewReader("<strong>safe</strong>"),
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	attachment := stored.Attachment
	if attachment.ID == "" {
		t.Fatal("attachment ID is empty")
	}
	if stored.Kind != "document" {
		t.Fatalf("kind = %q, want document", stored.Kind)
	}
	if attachment.Target != "#general" || attachment.OwnerID != "user_123" {
		t.Fatalf("attachment target/owner = %q/%q", attachment.Target, attachment.OwnerID)
	}
	if attachment.Filename != "preview.html" {
		t.Fatalf("filename = %q, want sanitized basename", attachment.Filename)
	}
	if attachment.MimeType != "text/html" {
		t.Fatalf("mime = %q, want text/html", attachment.MimeType)
	}
	if attachment.SizeBytes != int64(len("<strong>safe</strong>")) {
		t.Fatalf("size = %d", attachment.SizeBytes)
	}
	if attachment.CreatedUnix != createdAt.Unix() {
		t.Fatalf("createdUnix = %d, want %d", attachment.CreatedUnix, createdAt.Unix())
	}
	if attachment.DownloadURL != "/api/attachments/"+attachment.ID+"/content" {
		t.Fatalf("downloadURL = %q", attachment.DownloadURL)
	}

	contentPath := ContentPath(dataDir, attachment)
	content, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(content) != "<strong>safe</strong>" {
		t.Fatalf("content = %q", content)
	}
	if !strings.HasPrefix(filepath.ToSlash(contentPath), filepath.ToSlash(dataDir)+"/attachments/"+attachment.ID+"/") {
		t.Fatalf("content path = %q, want under attachment dir", contentPath)
	}
	metadata, err := ReadMetadata(dataDir, attachment.ID)
	if err != nil {
		t.Fatalf("ReadMetadata() error = %v", err)
	}
	if metadata.ID != attachment.ID || metadata.StorageRef != attachment.StorageRef {
		t.Fatalf("metadata = %+v, want %+v", metadata, attachment)
	}
}

func TestStoreDetectsMimeAndRejectsOversize(t *testing.T) {
	dataDir := t.TempDir()
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	stored, err := Store(dataDir, StoreInput{
		Target:   "#general",
		Filename: "image.bin",
		MimeType: "application/octet-stream",
		Content:  bytes.NewReader(pngHeader),
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if got := stored.Attachment.MimeType; got != "image/png" {
		t.Fatalf("mime = %q, want image/png", got)
	}
	if stored.Kind != "image" {
		t.Fatalf("kind = %q, want image", stored.Kind)
	}

	_, err = Store(dataDir, StoreInput{
		Target:   "#general",
		Filename: "large.txt",
		Content:  strings.NewReader("too big"),
		MaxBytes: 3,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Store() error = %v, want ErrTooLarge", err)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "attachments"))
	if err != nil {
		t.Fatalf("ReadDir attachments: %v", err)
	}
	for _, entry := range entries {
		metadata, err := ReadMetadata(dataDir, entry.Name())
		if err != nil {
			t.Fatalf("leftover attachment %q without metadata: %v", entry.Name(), err)
		}
		if metadata.Filename == "large.txt" {
			t.Fatalf("oversize attachment directory was not cleaned up: %q", entry.Name())
		}
	}
}

func TestMetadataRejectsUnsafeIDs(t *testing.T) {
	dataDir := t.TempDir()
	for _, id := range []string{"", "../att", "att/child", ".hidden"} {
		if _, err := MetadataPath(dataDir, id); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("MetadataPath(%q) error = %v, want ErrNotFound", id, err)
		}
		if _, err := ReadMetadata(dataDir, id); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("ReadMetadata(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := SafeFilename(""); got != "attachment" {
		t.Fatalf("SafeFilename empty = %q", got)
	}
	if got := SafeFilename(`../bad"name
.txt`); got != "badname.txt" {
		t.Fatalf("SafeFilename unsafe = %q", got)
	}
	if got := NormalizeMimeType("Image/PNG; charset=binary"); got != "image/png" {
		t.Fatalf("NormalizeMimeType = %q", got)
	}
	if !IsInlineAttachment("image/png") || !IsInlineAttachment("text/html; charset=utf-8") || !IsInlineAttachment("text/plain; charset=utf-8") {
		t.Fatal("inline attachment detection failed")
	}
	if IsInlineAttachment("application/pdf") {
		t.Fatal("pdf should not be served inline by default")
	}
	kinds := map[string]string{
		"image/jpeg":  "image",
		"video/mp4":   "video",
		"audio/mpeg":  "audio",
		"text/plain":  "document",
		"application": "file",
	}
	for mimeType, want := range kinds {
		if got := KindFromMimeType(mimeType); got != want {
			t.Fatalf("KindFromMimeType(%q) = %q, want %q", mimeType, got, want)
		}
	}
}
