package imcoord

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

var (
	ErrInvalidDraft = errors.New("invalid im draft")
	ErrAborted      = errors.New("im session aborted")
)

type Draft = iminbound.Draft
type Sender = iminbound.Sender

type Result struct {
	HandledCommand bool
	Command        string
	CommandArgs    string
	SessionKey     string
	Message        storage.Message
	Response       string
}

func normalizeDraft(d Draft) Draft {
	d.Target = strings.TrimSpace(d.Target)
	d.ThreadID = strings.TrimSpace(d.ThreadID)
	d.Role = strings.TrimSpace(d.Role)
	if d.Role == "" {
		d.Role = "user"
	}
	d.Content = strings.TrimSpace(d.Content)
	d.ReplyToMessageID = strings.TrimSpace(d.ReplyToMessageID)
	d.SourceEndpointID = strings.TrimSpace(d.SourceEndpointID)
	d.ExternalMessageID = strings.TrimSpace(d.ExternalMessageID)
	d.MetadataJSON = normalizedJSON(d.MetadataJSON)
	d.AttachmentIDs = cleanStrings(d.AttachmentIDs...)
	d.Sender = d.Sender.Normalize()
	return d
}

func validateDraft(d Draft) error {
	d = normalizeDraft(d)
	var problems []string
	if d.Target == "" {
		problems = append(problems, "target is required")
	}
	if d.SourceEndpointID == "" {
		problems = append(problems, "source endpoint id is required")
	}
	if d.ExternalMessageID == "" {
		problems = append(problems, "external message id is required")
	}
	if d.Sender.ExternalID == "" {
		problems = append(problems, "sender external id is required")
	}
	if d.Content == "" && len(d.AttachmentIDs) == 0 {
		problems = append(problems, "content or attachments are required")
	}
	if !json.Valid([]byte(d.MetadataJSON)) {
		problems = append(problems, "metadata json must be valid")
	}
	if len(problems) > 0 {
		return errors.Join(append([]error{ErrInvalidDraft}, stringsToErrors(problems)...)...)
	}
	return nil
}

func SessionKey(d Draft) string {
	d = normalizeDraft(d)
	if d.ThreadID != "" {
		return d.Target + ":" + d.ThreadID
	}
	if d.Sender.ExternalID != "" {
		return d.Target + ":" + d.SourceEndpointID + ":" + d.Sender.ExternalID
	}
	return d.Target + ":" + d.SourceEndpointID
}

func sessionKey(d Draft) string {
	return SessionKey(d)
}

func draftToMessage(d Draft, attachments []storage.Attachment) storage.Message {
	d = normalizeDraft(d)
	return storage.Message{
		Target:            d.Target,
		ThreadID:          d.ThreadID,
		Role:              d.Role,
		Content:           d.Content,
		ReplyToMessageID:  d.ReplyToMessageID,
		SenderDisplayName: d.Sender.DisplayName,
		SenderKind:        "endpoint",
		SourceEndpointID:  d.SourceEndpointID,
		ExternalMessageID: d.ExternalMessageID,
		MetadataJSON:      d.MetadataJSON,
		Attachments:       attachments,
		RequestID:         d.SourceEndpointID + ":" + d.ExternalMessageID,
	}
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

func normalizedJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(raw)); err == nil {
		return out.String()
	}
	return raw
}

func stringsToErrors(values []string) []error {
	out := make([]error, 0, len(values))
	for _, value := range values {
		out = append(out, errors.New(value))
	}
	return out
}
