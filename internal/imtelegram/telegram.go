package imtelegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

const SecretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

var (
	ErrUnauthorizedWebhook = errors.New("unauthorized telegram webhook")
	ErrIgnoredUpdate       = errors.New("ignored telegram update")
)

type Config struct {
	EndpointID      string
	Token           string
	SecretToken     string
	BotUsername     string
	DefaultTarget   string
	DefaultThreadID string
	GroupMode       string
	DefaultChatID   string
	APIBaseURL      string
}

type Webhook struct {
	Config      Config
	Normalizer  imadapter.Normalizer
	Coordinator *imcoord.Coordinator
	Now         func() time.Time
}

type WebhookResult struct {
	Message storage.Message
	Ignored bool
	Reason  string
}

type Runtime struct {
	Config     Config
	Store      *storage.Store
	HTTPClient *http.Client
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []TelegramMessage
}

type TelegramMessage struct {
	MessageID int64 `json:"message_id"`
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("telegram config: %w", err)
		}
	}
	return Config{
		EndpointID:      endpoint.ID,
		Token:           stringValue(raw, "token"),
		SecretToken:     firstNonEmpty(stringValue(raw, "secret_token"), stringValue(raw, "webhook_secret")),
		BotUsername:     normalizeBotUsername(stringValue(raw, "bot_username")),
		DefaultTarget:   stringValue(raw, "default_target"),
		DefaultThreadID: stringValue(raw, "default_thread_id"),
		GroupMode:       firstNonEmpty(strings.ToLower(stringValue(raw, "group_mode")), "mention"),
		DefaultChatID:   stringValue(raw, "channel_id"),
		APIBaseURL:      stringValue(raw, "api_base_url"),
	}, nil
}

func (w Webhook) Handle(ctx context.Context, headers http.Header, body []byte) (WebhookResult, error) {
	cfg := w.Config.normalize()
	if err := cfg.verifySecret(headers); err != nil {
		return WebhookResult{}, err
	}
	event := imadapter.TelegramRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:   cfg.EndpointID,
		ReceivedUnix: w.now().Unix(),
		Headers:      cloneHeaders(headers),
		Body:         bytes.TrimSpace(body),
		Metadata:     map[string]any{"transport": "webhook"},
	})
	msg, err := w.normalizer().NormalizeInbound(ctx, event)
	if err != nil {
		return WebhookResult{}, err
	}
	msg = applyConfig(msg, cfg)
	if ignored, reason := shouldIgnore(msg, cfg); ignored {
		return WebhookResult{Ignored: true, Reason: reason}, nil
	}
	draft, err := msg.Draft()
	if err != nil {
		return WebhookResult{}, err
	}
	if w.Coordinator == nil {
		return WebhookResult{}, errors.New("telegram webhook coordinator is required")
	}
	result, err := w.Coordinator.Handle(ctx, draft)
	if errors.Is(err, storage.ErrConflict) {
		return WebhookResult{Ignored: true, Reason: "duplicate"}, nil
	}
	if err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Message: result.Message}, nil
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("telegram runtime store is required")
	}
	cfg := r.Config.normalize()
	deliveries, err := r.Store.ListOutboundDeliveries(ctx, storage.OutboundDeliveryListOptions{
		EndpointID: cfg.EndpointID,
		Statuses:   []string{"pending", "retrying"},
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	results := make([]SendResult, 0, len(deliveries))
	for _, delivery := range deliveries {
		result, err := r.SendDelivery(ctx, delivery)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r Runtime) SendDelivery(ctx context.Context, delivery storage.OutboundDelivery) (SendResult, error) {
	if r.Store == nil {
		return SendResult{}, errors.New("telegram runtime store is required")
	}
	cfg := r.Config.normalize()
	if cfg.EndpointID == "" {
		cfg.EndpointID = delivery.EndpointID
	}
	message, err := r.Store.GetMessage(ctx, delivery.Target, delivery.MessageID)
	if err != nil {
		return SendResult{}, err
	}
	conversationID := cfg.DefaultChatID
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		conversationID = firstNonEmpty(conversationID, telegramConversationID(source.MetadataJSON))
	}
	if conversationID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "telegram chat id is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("telegram chat id is required")
	}
	frames := imadapter.RenderTelegramOutbound(imadapter.OutboundRenderInput{
		Provider:                 imadapter.ProviderTelegram,
		ConversationID:           conversationID,
		ReplyToExternalMessageID: delivery.ExternalMessageID,
		Text:                     message.Content,
	})
	sent := make([]TelegramMessage, 0, len(frames))
	for _, frame := range frames {
		msg, err := r.sendFrame(ctx, cfg, frame)
		if err != nil {
			failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
			return SendResult{Delivery: failed, Messages: sent}, err
		}
		sent = append(sent, msg)
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: sent}, nil
}

func (r Runtime) sendFrame(ctx context.Context, cfg Config, frame imadapter.OutboundFrame) (TelegramMessage, error) {
	if cfg.Token == "" {
		return TelegramMessage{}, errors.New("telegram bot token is required")
	}
	payload := map[string]any{
		"chat_id": frame.TargetID,
		"text":    frame.Text,
	}
	if frame.ParseMode != "" {
		payload["parse_mode"] = frame.ParseMode
	}
	if frame.Silent {
		payload["disable_notification"] = true
	}
	if id, ok := parseTelegramMessageID(frame.ReplyToExternalMessageID); ok {
		payload["reply_parameters"] = map[string]any{"message_id": id}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return TelegramMessage{}, err
	}
	url := strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, "https://api.telegram.org"), "/") + "/bot" + cfg.Token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return TelegramMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return TelegramMessage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TelegramMessage{}, fmt.Errorf("telegram sendMessage HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      TelegramMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return TelegramMessage{}, err
	}
	if !out.OK {
		return TelegramMessage{}, fmt.Errorf("telegram sendMessage rejected: %s", out.Description)
	}
	return out.Result, nil
}

func applyConfig(msg iminbound.Message, cfg Config) iminbound.Message {
	if msg.Conversation.TargetHint == "" {
		msg.Conversation.TargetHint = cfg.DefaultTarget
	}
	if msg.Conversation.ThreadID == "" {
		msg.Conversation.ThreadID = cfg.DefaultThreadID
	}
	return msg
}

func shouldIgnore(msg iminbound.Message, cfg Config) (bool, string) {
	if !msg.Conversation.IsGroup {
		return false, ""
	}
	switch cfg.GroupMode {
	case "disabled":
		return true, "group disabled"
	case "always":
		return false, ""
	default:
		if cfg.BotUsername == "" {
			return false, ""
		}
		if strings.Contains(strings.ToLower(msg.Text()), "@"+strings.ToLower(cfg.BotUsername)) {
			return false, ""
		}
		return true, "group mention required"
	}
}

func sourceForReply(ctx context.Context, store *storage.Store, msg storage.Message) (storage.Message, bool) {
	if msg.SourceEndpointID != "" && msg.ExternalMessageID != "" {
		return msg, true
	}
	if msg.ReplyToMessageID == "" {
		return storage.Message{}, false
	}
	source, err := store.GetMessage(ctx, msg.Target, msg.ReplyToMessageID)
	if err == nil && source.SourceEndpointID != "" && source.ExternalMessageID != "" {
		return source, true
	}
	return storage.Message{}, false
}

func telegramConversationID(metadataJSON string) string {
	var metadata struct {
		IM struct {
			Conversation struct {
				ExternalID       string `json:"external_id"`
				ExternalIDCompat string `json:"externalID"`
			} `json:"conversation"`
		} `json:"im"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return ""
	}
	return firstNonEmpty(metadata.IM.Conversation.ExternalID, metadata.IM.Conversation.ExternalIDCompat)
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.Token = strings.TrimSpace(c.Token)
	c.SecretToken = strings.TrimSpace(c.SecretToken)
	c.BotUsername = normalizeBotUsername(c.BotUsername)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.GroupMode = strings.ToLower(strings.TrimSpace(c.GroupMode))
	if c.GroupMode == "" {
		c.GroupMode = "mention"
	}
	c.DefaultChatID = strings.TrimSpace(c.DefaultChatID)
	c.APIBaseURL = strings.TrimSpace(c.APIBaseURL)
	return c
}

func (c Config) verifySecret(headers http.Header) error {
	if c.SecretToken == "" {
		return nil
	}
	if subtleConstantTimeCompare(headers.Get(SecretTokenHeader), c.SecretToken) {
		return nil
	}
	return ErrUnauthorizedWebhook
}

func (w Webhook) normalizer() imadapter.Normalizer {
	if w.Normalizer.Now != nil {
		return w.Normalizer
	}
	return imadapter.Normalizer{Now: w.Now}
}

func (w Webhook) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (r Runtime) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return http.DefaultClient
}

func parseTelegramMessageID(value string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id, err == nil && id > 0
}

func normalizeBotUsername(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "@")
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(values[key]))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func subtleConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
