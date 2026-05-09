package terminal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

var ErrInvalidTerminalInput = errors.New("invalid terminal input")

type Config struct {
	EndpointID   string
	SessionID    string
	OperatorID   string
	OperatorName string
	Target       string
	ThreadID     string
}

type Input struct {
	MessageID    string
	SessionID    string
	OperatorID   string
	OperatorName string
	Text         string
	Target       string
	ThreadID     string
	ReceivedUnix int64
}

type Channel struct {
	Config Config
	Now    func() time.Time
}

func (c Channel) RawEvent(input Input) (iminbound.RawEvent, error) {
	cfg := c.Config.normalize()
	input = input.normalizeWith(cfg)
	if cfg.EndpointID == "" {
		return iminbound.RawEvent{}, fmt.Errorf("%w: endpoint_id is required", ErrInvalidTerminalInput)
	}
	if input.Text == "" {
		return iminbound.RawEvent{}, fmt.Errorf("%w: text is required", ErrInvalidTerminalInput)
	}
	if input.ReceivedUnix == 0 {
		input.ReceivedUnix = c.now().Unix()
	}
	if input.MessageID == "" {
		input.MessageID = terminalMessageID(input)
	}
	body, err := json.Marshal(terminalPayload{
		MessageID:    input.MessageID,
		SessionID:    input.SessionID,
		OperatorID:   input.OperatorID,
		OperatorName: input.OperatorName,
		Text:         input.Text,
		Target:       input.Target,
		ThreadID:     input.ThreadID,
	})
	if err != nil {
		return iminbound.RawEvent{}, err
	}
	return iminbound.RawEvent{
		EndpointID:        cfg.EndpointID,
		EndpointKind:      "im",
		Provider:          imadapter.ProviderTerminal,
		ExternalMessageID: input.MessageID,
		ReceivedUnix:      input.ReceivedUnix,
		Body:              body,
		Metadata: map[string]any{
			"source": "terminal",
		},
	}, nil
}

func (c Channel) NormalizeInbound(input Input) (iminbound.Message, error) {
	event, err := c.RawEvent(input)
	if err != nil {
		return iminbound.Message{}, err
	}
	return (imadapter.Normalizer{Now: c.Now}).NormalizeInbound(nil, event)
}

func RenderOutbound(message storage.Message, delivery storage.OutboundDelivery) string {
	actor := strings.TrimSpace(message.SenderDisplayName)
	if actor == "" {
		actor = strings.TrimSpace(message.SenderAgentID)
	}
	if actor == "" {
		actor = strings.TrimSpace(message.SenderKind)
	}
	if actor == "" {
		actor = "nekode"
	}
	content := strings.TrimSpace(message.Content)
	if content == "" {
		content = "[empty message]"
	}
	line := actor + ": " + content
	if delivery.ID == "" {
		return line
	}
	status := strings.TrimSpace(delivery.Status)
	if status == "" {
		status = "pending"
	}
	line += "\n[" + status + " delivery " + delivery.ID
	if delivery.ExternalMessageID != "" {
		line += " -> " + delivery.ExternalMessageID
	}
	if delivery.LastError != "" {
		line += ": " + delivery.LastError
	}
	line += "]"
	return line
}

type terminalPayload struct {
	MessageID    string `json:"message_id"`
	SessionID    string `json:"session_id"`
	OperatorID   string `json:"operator_id"`
	OperatorName string `json:"operator_name"`
	Text         string `json:"text"`
	Target       string `json:"target,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
}

func (c Channel) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.SessionID = strings.TrimSpace(c.SessionID)
	c.OperatorID = strings.TrimSpace(c.OperatorID)
	c.OperatorName = strings.TrimSpace(c.OperatorName)
	c.Target = strings.TrimSpace(c.Target)
	c.ThreadID = strings.TrimSpace(c.ThreadID)
	return c
}

func (i Input) normalizeWith(cfg Config) Input {
	i.MessageID = strings.TrimSpace(i.MessageID)
	i.SessionID = firstNonEmpty(i.SessionID, cfg.SessionID, "local")
	i.OperatorID = firstNonEmpty(i.OperatorID, cfg.OperatorID, "terminal")
	i.OperatorName = firstNonEmpty(i.OperatorName, cfg.OperatorName)
	i.Text = strings.TrimSpace(i.Text)
	i.Target = firstNonEmpty(i.Target, cfg.Target)
	i.ThreadID = firstNonEmpty(i.ThreadID, cfg.ThreadID)
	return i
}

func terminalMessageID(input Input) string {
	seed := fmt.Sprintf("%s\x00%s\x00%d\x00%s", input.SessionID, input.OperatorID, input.ReceivedUnix, input.Text)
	sum := sha256.Sum256([]byte(seed))
	return "term-" + hex.EncodeToString(sum[:])[:16]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
