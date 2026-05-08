package iminbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidMessage = errors.New("invalid inbound message")

type Adapter interface {
	NormalizeInbound(context.Context, RawEvent) (Message, error)
}

type AdapterFunc func(context.Context, RawEvent) (Message, error)

func (fn AdapterFunc) NormalizeInbound(ctx context.Context, event RawEvent) (Message, error) {
	return fn(ctx, event)
}

type RawEvent struct {
	EndpointID        string
	EndpointKind      string
	Provider          string
	ExternalMessageID string
	ReceivedUnix      int64
	Headers           map[string][]string
	Body              []byte
	Metadata          map[string]any
}

type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImage    ContentType = "image"
	ContentTypeFile     ContentType = "file"
	ContentTypeAudio    ContentType = "audio"
	ContentTypeVideo    ContentType = "video"
	ContentTypeSticker  ContentType = "sticker"
	ContentTypeLocation ContentType = "location"
	ContentTypeReaction ContentType = "reaction"
	ContentTypeUnknown  ContentType = "unknown"
)

type Sender struct {
	ExternalID   string   `json:"external_id"`
	CandidateIDs []string `json:"candidate_ids,omitempty"`
	DisplayName  string   `json:"display_name,omitempty"`
	Username     string   `json:"username,omitempty"`
	Kind         string   `json:"kind,omitempty"`
}

type Conversation struct {
	ExternalID       string `json:"external_id"`
	DisplayName      string `json:"display_name,omitempty"`
	IsGroup          bool   `json:"is_group"`
	TargetHint       string `json:"target_hint,omitempty"`
	ThreadID         string `json:"thread_id,omitempty"`
	ExternalThreadID string `json:"external_thread_id,omitempty"`
	RootMessageID    string `json:"root_message_id,omitempty"`
}

type ContentBlock struct {
	Type         ContentType    `json:"type"`
	Text         string         `json:"text,omitempty"`
	AttachmentID string         `json:"attachment_id,omitempty"`
	ExternalURL  string         `json:"external_url,omitempty"`
	Filename     string         `json:"filename,omitempty"`
	MimeType     string         `json:"mime_type,omitempty"`
	SizeBytes    int64          `json:"size_bytes,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type Message struct {
	EndpointID        string         `json:"endpoint_id"`
	EndpointKind      string         `json:"endpoint_kind,omitempty"`
	Provider          string         `json:"provider,omitempty"`
	ExternalMessageID string         `json:"external_message_id"`
	ReceivedUnix      int64          `json:"received_unix,omitempty"`
	Conversation      Conversation   `json:"conversation"`
	Sender            Sender         `json:"sender"`
	Content           []ContentBlock `json:"content"`
	AttachmentIDs     []string       `json:"attachment_ids,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type Draft struct {
	Target            string
	ThreadID          string
	Role              string
	Content           string
	ReplyToMessageID  string
	SourceEndpointID  string
	ExternalMessageID string
	MetadataJSON      string
	AttachmentIDs     []string
	Sender            Sender
}

func (m Message) Normalize() Message {
	m.EndpointID = strings.TrimSpace(m.EndpointID)
	m.EndpointKind = strings.TrimSpace(m.EndpointKind)
	m.Provider = strings.TrimSpace(m.Provider)
	m.ExternalMessageID = strings.TrimSpace(m.ExternalMessageID)
	if m.ReceivedUnix == 0 {
		m.ReceivedUnix = time.Now().Unix()
	}
	m.Conversation = m.Conversation.Normalize()
	m.Sender = m.Sender.Normalize()
	m.AttachmentIDs = cleanStrings(m.AttachmentIDs...)
	for i := range m.Content {
		m.Content[i] = m.Content[i].Normalize()
	}
	return m
}

func (c Conversation) Normalize() Conversation {
	c.ExternalID = strings.TrimSpace(c.ExternalID)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	c.TargetHint = strings.TrimSpace(c.TargetHint)
	c.ThreadID = strings.TrimSpace(c.ThreadID)
	c.ExternalThreadID = strings.TrimSpace(c.ExternalThreadID)
	c.RootMessageID = strings.TrimSpace(c.RootMessageID)
	return c
}

func (s Sender) Normalize() Sender {
	s.ExternalID = strings.TrimSpace(s.ExternalID)
	s.DisplayName = strings.TrimSpace(s.DisplayName)
	s.Username = strings.TrimSpace(s.Username)
	s.Kind = strings.TrimSpace(s.Kind)
	s.CandidateIDs = cleanStrings(append([]string{s.ExternalID}, s.CandidateIDs...)...)
	if s.ExternalID == "" && len(s.CandidateIDs) > 0 {
		s.ExternalID = s.CandidateIDs[0]
	}
	return s
}

func (b ContentBlock) Normalize() ContentBlock {
	if b.Type == "" {
		b.Type = ContentTypeText
	}
	b.Text = strings.TrimSpace(b.Text)
	b.AttachmentID = strings.TrimSpace(b.AttachmentID)
	b.ExternalURL = strings.TrimSpace(b.ExternalURL)
	b.Filename = strings.TrimSpace(b.Filename)
	b.MimeType = strings.TrimSpace(b.MimeType)
	return b
}

func (m Message) Validate() error {
	m = m.Normalize()
	var problems []string
	if m.EndpointID == "" {
		problems = append(problems, "endpoint_id is required")
	}
	if m.ExternalMessageID == "" {
		problems = append(problems, "external_message_id is required")
	}
	if m.Conversation.ExternalID == "" {
		problems = append(problems, "conversation.external_id is required")
	}
	if m.Sender.ExternalID == "" {
		problems = append(problems, "sender.external_id is required")
	}
	if len(m.Content) == 0 && len(m.AttachmentIDs) == 0 {
		problems = append(problems, "content or attachment_ids is required")
	}
	for i, block := range m.Content {
		if err := block.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("content[%d]: %v", i, err))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidMessage, strings.Join(problems, "; "))
	}
	return nil
}

func (b ContentBlock) Validate() error {
	b = b.Normalize()
	switch b.Type {
	case ContentTypeText, ContentTypeReaction, ContentTypeLocation:
		if b.Text == "" {
			return fmt.Errorf("%s block requires text", b.Type)
		}
	case ContentTypeImage, ContentTypeFile, ContentTypeAudio, ContentTypeVideo, ContentTypeSticker:
		if b.AttachmentID == "" && b.ExternalURL == "" {
			return fmt.Errorf("%s block requires attachment_id or external_url", b.Type)
		}
	case ContentTypeUnknown:
		if b.Text == "" && b.AttachmentID == "" && b.ExternalURL == "" && len(b.Metadata) == 0 {
			return errors.New("unknown block requires text, attachment_id, external_url, or metadata")
		}
	default:
		return fmt.Errorf("unsupported type %q", b.Type)
	}
	return nil
}

func (m Message) DedupeKey() string {
	m = m.Normalize()
	if m.EndpointID == "" || m.ExternalMessageID == "" {
		return ""
	}
	return m.EndpointID + ":" + m.ExternalMessageID
}

func (m Message) Text() string {
	m = m.Normalize()
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Text != "" {
			parts = append(parts, block.Text)
			continue
		}
		label := string(block.Type)
		if block.Filename != "" {
			label += ": " + block.Filename
		}
		if label != "" {
			parts = append(parts, "["+label+"]")
		}
	}
	if len(parts) == 0 && len(m.AttachmentIDs) > 0 {
		if len(m.AttachmentIDs) == 1 {
			return "[attachment]"
		}
		return fmt.Sprintf("[%d attachments]", len(m.AttachmentIDs))
	}
	return strings.Join(parts, "\n")
}

func (m Message) AllAttachmentIDs() []string {
	m = m.Normalize()
	values := make([]string, 0, len(m.AttachmentIDs)+len(m.Content))
	values = append(values, m.AttachmentIDs...)
	for _, block := range m.Content {
		values = append(values, block.AttachmentID)
	}
	return cleanStrings(values...)
}

func (m Message) MetadataJSON() (string, error) {
	m = m.Normalize()
	metadata := make(map[string]any, len(m.Metadata)+1)
	for k, v := range m.Metadata {
		if strings.TrimSpace(k) == "" {
			continue
		}
		metadata[k] = v
	}
	metadata["im"] = map[string]any{
		"endpoint_id":         m.EndpointID,
		"endpoint_kind":       m.EndpointKind,
		"provider":            m.Provider,
		"external_message_id": m.ExternalMessageID,
		"received_unix":       m.ReceivedUnix,
		"conversation":        m.Conversation,
		"sender":              m.Sender,
		"content":             m.Content,
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m Message) Draft() (Draft, error) {
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return Draft{}, err
	}
	metadataJSON, err := m.MetadataJSON()
	if err != nil {
		return Draft{}, err
	}
	return Draft{
		Target:            m.Conversation.TargetHint,
		ThreadID:          m.Conversation.ThreadID,
		Role:              "user",
		Content:           m.Text(),
		ReplyToMessageID:  m.Conversation.RootMessageID,
		SourceEndpointID:  m.EndpointID,
		ExternalMessageID: m.ExternalMessageID,
		MetadataJSON:      metadataJSON,
		AttachmentIDs:     m.AllAttachmentIDs(),
		Sender:            m.Sender,
	}, nil
}

func cleanStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
