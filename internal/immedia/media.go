package immedia

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/storage"
)

const DefaultMaxAttachmentBytes int64 = 32 << 20

var (
	ErrInvalidInput = errors.New("invalid media input")
	ErrTooLarge     = errors.New("media exceeds maximum size")
)

type StoreInput struct {
	Target    string
	OwnerID   string
	Filename  string
	MimeType  string
	Content   io.Reader
	MaxBytes  int64
	CreatedAt time.Time
}

type StoredMedia struct {
	Attachment storage.Attachment
	Kind       string
}

func Store(dataDir string, input StoreInput) (StoredMedia, error) {
	target := strings.TrimSpace(input.Target)
	if target == "" {
		return StoredMedia{}, fmt.Errorf("%w: target is required", ErrInvalidInput)
	}
	if input.Content == nil {
		return StoredMedia{}, fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxAttachmentBytes
	}

	id := storage.NewID("att")
	filename := SafeFilename(input.Filename)
	dir := filepath.Join(dataDir, "attachments", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return StoredMedia{}, err
	}
	relativeStorageRef := filepath.Join("attachments", id, filename)
	contentPath := filepath.Join(dataDir, relativeStorageRef)
	out, err := os.OpenFile(contentPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return StoredMedia{}, err
	}
	size, copyErr := io.Copy(out, io.LimitReader(input.Content, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.RemoveAll(dir)
		if copyErr != nil {
			return StoredMedia{}, copyErr
		}
		return StoredMedia{}, closeErr
	}
	if size > maxBytes {
		_ = os.RemoveAll(dir)
		return StoredMedia{}, ErrTooLarge
	}

	mimeType := NormalizeMimeType(input.MimeType)
	if mimeType == "" || mimeType == "application/octet-stream" {
		detected, err := DetectFileContentType(contentPath)
		if err == nil {
			mimeType = detected
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	attachment := storage.Attachment{
		ID:          id,
		Target:      target,
		OwnerID:     strings.TrimSpace(input.OwnerID),
		Filename:    filename,
		MimeType:    mimeType,
		SizeBytes:   size,
		StorageRef:  filepath.ToSlash(relativeStorageRef),
		DownloadURL: "/api/attachments/" + id + "/content",
		CreatedUnix: createdAt.Unix(),
	}
	if err := SaveMetadata(dataDir, attachment); err != nil {
		_ = os.RemoveAll(dir)
		return StoredMedia{}, err
	}
	return StoredMedia{Attachment: attachment, Kind: KindFromMimeType(mimeType)}, nil
}

func MetadataPath(dataDir string, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return "", storage.ErrNotFound
	}
	return filepath.Join(dataDir, "attachments", id, "metadata.json"), nil
}

func SaveMetadata(dataDir string, attachment storage.Attachment) error {
	metadataPath, err := MetadataPath(dataDir, attachment.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(attachment, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0o600)
}

func ReadMetadata(dataDir string, id string) (storage.Attachment, error) {
	metadataPath, err := MetadataPath(dataDir, id)
	if err != nil {
		return storage.Attachment{}, err
	}
	data, err := os.ReadFile(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return storage.Attachment{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Attachment{}, err
	}
	var attachment storage.Attachment
	if err := json.Unmarshal(data, &attachment); err != nil {
		return storage.Attachment{}, err
	}
	if attachment.ID == "" {
		return storage.Attachment{}, storage.ErrNotFound
	}
	return attachment, nil
}

func ContentPath(dataDir string, attachment storage.Attachment) string {
	return filepath.Join(dataDir, filepath.FromSlash(attachment.StorageRef))
}

func SafeFilename(value string) string {
	name := strings.TrimSpace(filepath.Base(value))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "attachment"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '"', '\r', '\n', 0:
			return -1
		default:
			return r
		}
	}, name)
}

func NormalizeMimeType(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
}

func DetectFileContentType(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var head [512]byte
	n, err := file.Read(head[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}

func IsInlineAttachment(mimeType string) bool {
	mimeType = NormalizeMimeType(mimeType)
	return strings.HasPrefix(mimeType, "image/") || mimeType == "text/html" || mimeType == "text/plain"
}

func KindFromMimeType(mimeType string) string {
	mimeType = NormalizeMimeType(mimeType)
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case mimeType == "text/html" || strings.HasPrefix(mimeType, "text/") || mimeType == "application/pdf":
		return "document"
	default:
		return "file"
	}
}
