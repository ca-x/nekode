package serverchan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

const (
	DefaultAPIBaseURL  = "https://bot-go.apijia.cn"
	defaultPollTimeout = 5
)

var ErrIgnoredUpdate = errors.New("ignored serverchan update")

type Config struct {
	EndpointID      string
	BotToken        string
	DefaultTarget   string
	DefaultThreadID string
	DefaultChatID   string
	APIBaseURL      string
	AllowFrom       []string
}

type Runtime struct {
	Config      Config
	Store       *storage.Store
	HTTPClient  *http.Client
	Coordinator *imcoord.Coordinator
	Normalizer  imadapter.Normalizer
	RateLimiter imadapter.OutgoingRateWaiter
	Now         func() time.Time
}

type Update struct {
	UpdateID int64   `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	MessageID int64       `json:"message_id"`
	ChatID    int64       `json:"chat_id,omitempty"`
	Text      string      `json:"text"`
	Date      int64       `json:"date,omitempty"`
	Chat      MessageChat `json:"chat,omitempty"`
	From      MessageFrom `json:"from,omitempty"`
}

type MessageChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type,omitempty"`
}

type MessageFrom struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

type APIResponse struct {
	OK          bool     `json:"ok"`
	Description string   `json:"description,omitempty"`
	Result      []Update `json:"result,omitempty"`
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []SentMessage
}

type SentMessage struct {
	MessageID int64 `json:"message_id,omitempty"`
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("serverchan config: %w", err)
		}
	}
	return Config{
		EndpointID:      endpoint.ID,
		BotToken:        stringValue(raw, "bot_token"),
		DefaultTarget:   stringValue(raw, "default_target"),
		DefaultThreadID: stringValue(raw, "default_thread_id"),
		DefaultChatID:   firstNonEmpty(stringValue(raw, "default_chat_id"), stringValue(raw, "chat_id")),
		APIBaseURL:      stringValue(raw, "api_base_url"),
		AllowFrom:       stringSliceValue(raw, "allow_from"),
	}, nil
}

func (r Runtime) FetchUpdates(ctx context.Context, offset int64) ([]Update, error) {
	cfg := r.Config.normalize()
	if cfg.BotToken == "" {
		return nil, errors.New("serverchan bot_token is required")
	}
	values := url.Values{}
	values.Set("timeout", strconv.Itoa(defaultPollTimeout))
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL(cfg)+"/bot"+cfg.BotToken+"/getUpdates?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("serverchan getUpdates: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("serverchan getUpdates HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out APIResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("serverchan getUpdates rejected: %s", firstNonEmpty(out.Description, "ok=false"))
	}
	return out.Result, nil
}

func (r Runtime) HandleUpdate(ctx context.Context, update Update) (storage.Message, error) {
	cfg := r.Config.normalize()
	chatID := update.Message.Chat.ID
	if chatID == 0 {
		chatID = update.Message.ChatID
	}
	userID := update.Message.From.ID
	if userID == 0 {
		userID = chatID
	}
	if !allowed(cfg.AllowFrom, formatInt(userID), formatInt(chatID)) {
		return storage.Message{}, ErrIgnoredUpdate
	}
	if strings.TrimSpace(update.Message.Text) == "" {
		return storage.Message{}, ErrIgnoredUpdate
	}
	body, err := json.Marshal(update)
	if err != nil {
		return storage.Message{}, err
	}
	event := imadapter.ServerChanRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:        cfg.EndpointID,
		ExternalMessageID: formatInt(update.Message.MessageID),
		ReceivedUnix:      firstNonZero(update.Message.Date, r.now().Unix()),
		Body:              body,
		Metadata:          map[string]any{"transport": "polling"},
	})
	normalizer := r.Normalizer
	if normalizer.Now == nil {
		normalizer.Now = r.Now
	}
	inbound, err := normalizer.NormalizeInbound(ctx, event)
	if err != nil {
		return storage.Message{}, err
	}
	inbound = applyConfig(inbound, cfg)
	draft, err := inbound.Draft()
	if err != nil {
		return storage.Message{}, err
	}
	coord := r.Coordinator
	if coord == nil {
		if r.Store == nil {
			return storage.Message{}, errors.New("serverchan coordinator or store is required")
		}
		coord = imcoord.New(r.Store, nil)
	}
	result, err := coord.Handle(ctx, draft)
	if errors.Is(err, storage.ErrConflict) {
		return storage.Message{}, ErrIgnoredUpdate
	}
	if errors.Is(err, imcoord.ErrStaleDraft) {
		return storage.Message{}, ErrIgnoredUpdate
	}
	if err != nil {
		return storage.Message{}, err
	}
	return result.Message, nil
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("serverchan runtime store is required")
	}
	cfg := r.Config.normalize()
	deliveries, err := r.Store.ListOutboundDeliveries(ctx, storage.OutboundDeliveryListOptions{
		EndpointID: cfg.EndpointID,
		Statuses:   []string{"pending", "retrying"},
		Limit:      limit,
		ReadyUnix:  time.Now().Unix(),
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
		return SendResult{}, errors.New("serverchan runtime store is required")
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
		conversationID = firstNonEmpty(conversationID, serverChanConversationID(source.MetadataJSON))
	}
	if conversationID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "serverchan chat id is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("serverchan chat id is required")
	}
	frames := imadapter.RenderServerChanOutbound(imadapter.OutboundRenderInput{
		Provider:                 imadapter.ProviderServerChan,
		ConversationID:           conversationID,
		ReplyToExternalMessageID: delivery.ExternalMessageID,
		Text:                     message.Content,
	})
	sent := make([]SentMessage, 0, len(frames))
	for _, frame := range frames {
		if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderServerChan); err != nil {
			return SendResult{Delivery: delivery, Messages: sent}, err
		}
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

func (r Runtime) sendFrame(ctx context.Context, cfg Config, frame imadapter.OutboundFrame) (SentMessage, error) {
	if cfg.BotToken == "" {
		return SentMessage{}, errors.New("serverchan bot_token is required")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(frame.TargetID), 10, 64)
	if err != nil || chatID == 0 {
		return SentMessage{}, fmt.Errorf("serverchan invalid chat id %q", frame.TargetID)
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       frame.Text,
		"parse_mode": frame.ParseMode,
		"silent":     frame.Silent,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SentMessage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL(cfg)+"/bot"+cfg.BotToken+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return SentMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return SentMessage{}, fmt.Errorf("serverchan sendMessage: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SentMessage{}, fmt.Errorf("serverchan sendMessage HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		OK          bool        `json:"ok"`
		Description string      `json:"description,omitempty"`
		Result      SentMessage `json:"result,omitempty"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return SentMessage{}, err
	}
	if !out.OK {
		return SentMessage{}, fmt.Errorf("serverchan sendMessage rejected: %s", firstNonEmpty(out.Description, "ok=false"))
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

func serverChanConversationID(metadataJSON string) string {
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
	return strings.TrimPrefix(firstNonEmpty(metadata.IM.Conversation.ExternalID, metadata.IM.Conversation.ExternalIDCompat), "serverchan:")
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.BotToken = strings.TrimSpace(c.BotToken)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.DefaultChatID = strings.TrimPrefix(strings.TrimSpace(c.DefaultChatID), "serverchan:")
	c.APIBaseURL = strings.TrimSpace(c.APIBaseURL)
	c.AllowFrom = cleanStrings(c.AllowFrom...)
	return c
}

func (r Runtime) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return http.DefaultClient
}

func (r Runtime) outgoingLimiter() imadapter.OutgoingRateWaiter {
	if r.RateLimiter != nil {
		return r.RateLimiter
	}
	return imadapter.DefaultOutgoingRateLimiter()
}

func (r Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func apiBaseURL(cfg Config) string {
	return strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, DefaultAPIBaseURL), "/")
}

func allowed(allowFrom []string, ids ...string) bool {
	if len(allowFrom) == 0 {
		return true
	}
	for _, allowed := range allowFrom {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" {
			return true
		}
		for _, id := range ids {
			if allowed == strings.TrimSpace(id) && allowed != "" {
				return true
			}
		}
	}
	return false
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceValue(values map[string]any, key string) []string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return cleanStrings(out...)
	case []string:
		return cleanStrings(typed...)
	case string:
		return cleanStrings(strings.Split(typed, ",")...)
	default:
		return cleanStrings(fmt.Sprint(value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func cleanStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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

func formatInt(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}
