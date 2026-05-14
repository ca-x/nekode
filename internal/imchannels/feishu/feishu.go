package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var (
	ErrUnauthorizedCallback = errors.New("unauthorized feishu callback")
	ErrEncryptedUnsupported = errors.New("encrypted feishu callback is not supported")
	ErrIgnoredEvent         = errors.New("ignored feishu event")
)

type Config struct {
	EndpointID         string
	AppID              string
	AppSecret          string
	VerificationToken  string
	EncryptKey         string
	DefaultTarget      string
	DefaultThreadID    string
	DefaultReceiveID   string
	DefaultReceiveType string
	GroupMode          string
	APIBaseURL         string
}

type Callback struct {
	Config      Config
	Normalizer  imadapter.Normalizer
	Coordinator *imcoord.Coordinator
	Now         func() time.Time
}

type CallbackResult struct {
	Message   storage.Message
	Ignored   bool
	Reason    string
	Challenge string
}

type Runtime struct {
	Config      Config
	Store       *storage.Store
	HTTPClient  *http.Client
	Client      FeishuClient
	RateLimiter imadapter.OutgoingRateWaiter
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []Message
}

type Message struct {
	MessageID string `json:"message_id"`
}

type FeishuClient interface {
	CreateMessage(ctx context.Context, receiveType, receiveID, text string) (Message, error)
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("feishu config: %w", err)
		}
	}
	return Config{
		EndpointID:         endpoint.ID,
		AppID:              stringValue(raw, "app_id"),
		AppSecret:          stringValue(raw, "app_secret"),
		VerificationToken:  stringValue(raw, "verification_token"),
		EncryptKey:         stringValue(raw, "encrypt_key"),
		DefaultTarget:      stringValue(raw, "default_target"),
		DefaultThreadID:    stringValue(raw, "default_thread_id"),
		DefaultReceiveID:   firstNonEmpty(stringValue(raw, "default_receive_id"), stringValue(raw, "default_chat_id")),
		DefaultReceiveType: stringValue(raw, "default_receive_id_type"),
		GroupMode:          firstNonEmpty(strings.ToLower(stringValue(raw, "group_mode")), "mention"),
		APIBaseURL:         stringValue(raw, "api_base_url"),
	}, nil
}

func (c Callback) Handle(ctx context.Context, headers http.Header, body []byte) (CallbackResult, error) {
	cfg := c.Config.normalize()
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return CallbackResult{}, errors.New("empty feishu callback body")
	}
	env, err := parseEnvelope(body)
	if err != nil {
		return CallbackResult{}, err
	}
	if env.Encrypt != "" {
		return CallbackResult{}, ErrEncryptedUnsupported
	}
	if env.Challenge != "" || env.Type == "url_verification" {
		if err := cfg.verifyToken(env.token()); err != nil {
			return CallbackResult{}, err
		}
		return CallbackResult{Challenge: env.Challenge}, nil
	}
	if err := cfg.verifyToken(env.token()); err != nil {
		return CallbackResult{}, err
	}
	event := imadapter.FeishuRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:   cfg.EndpointID,
		ReceivedUnix: c.now().Unix(),
		Headers:      cloneHeaders(headers),
		Body:         body,
		Metadata: map[string]any{
			"transport":  "webhook",
			"event_type": env.Header.EventType,
			"tenant_key": env.Header.TenantKey,
		},
	})
	msg, err := c.normalizer().NormalizeInbound(ctx, event)
	if err != nil {
		return CallbackResult{}, err
	}
	msg = applyConfig(msg, cfg)
	if ignored, reason := shouldIgnore(msg, cfg); ignored {
		return CallbackResult{Ignored: true, Reason: reason}, nil
	}
	draft, err := msg.Draft()
	if err != nil {
		return CallbackResult{}, err
	}
	if c.Coordinator == nil {
		return CallbackResult{}, errors.New("feishu callback coordinator is required")
	}
	result, err := c.Coordinator.Handle(ctx, draft)
	if errors.Is(err, storage.ErrConflict) {
		return CallbackResult{Ignored: true, Reason: "duplicate"}, nil
	}
	if errors.Is(err, imcoord.ErrStaleDraft) {
		return CallbackResult{Ignored: true, Reason: "stale"}, nil
	}
	if err != nil {
		return CallbackResult{}, err
	}
	return CallbackResult{Message: result.Message}, nil
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("feishu runtime store is required")
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
		return SendResult{}, errors.New("feishu runtime store is required")
	}
	cfg := r.Config.normalize()
	if cfg.EndpointID == "" {
		cfg.EndpointID = delivery.EndpointID
	}
	message, err := r.Store.GetMessage(ctx, delivery.Target, delivery.MessageID)
	if err != nil {
		return SendResult{}, err
	}
	receiveID, receiveType := cfg.DefaultReceiveID, cfg.DefaultReceiveType
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		if receiveID == "" {
			receiveID = feishuConversationID(source.MetadataJSON)
		}
		if receiveType == "" {
			receiveType = inferReceiveIDType(receiveID)
		}
	}
	if receiveID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "feishu receive id is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("feishu receive id is required")
	}
	if receiveType == "" {
		receiveType = inferReceiveIDType(receiveID)
	}
	client, err := r.feishuClient(cfg)
	if err != nil {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
		return SendResult{Delivery: failed}, err
	}
	if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderFeishu); err != nil {
		return SendResult{Delivery: delivery}, err
	}
	sent, err := client.CreateMessage(ctx, receiveType, receiveID, message.Content)
	if err != nil {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
		return SendResult{Delivery: failed}, err
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: []Message{sent}}, nil
}

func (r Runtime) feishuClient(cfg Config) (FeishuClient, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, errors.New("feishu app_id and app_secret are required")
	}
	if r.Client != nil {
		return r.Client, nil
	}
	opts := []lark.ClientOptionFunc{
		lark.WithHttpClient(r.httpClient()),
	}
	if cfg.APIBaseURL != "" {
		opts = append(opts, lark.WithOpenBaseUrl(apiBaseURL(cfg)))
	}
	return feishuSDKClient{client: lark.NewClient(cfg.AppID, cfg.AppSecret, opts...)}, nil
}

type feishuSDKClient struct {
	client *lark.Client
}

func (c feishuSDKClient) CreateMessage(ctx context.Context, receiveType, receiveID, text string) (Message, error) {
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return Message{}, err
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveType).
		Body(&larkim.CreateMessageReqBody{
			ReceiveId: larkcore.StringPtr(receiveID),
			MsgType:   larkcore.StringPtr(larkim.MsgTypeText),
			Content:   larkcore.StringPtr(string(content)),
		}).
		Build()
	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return Message{}, err
	}
	if !resp.Success() {
		return Message{}, fmt.Errorf("feishu message create rejected: %s", firstNonEmpty(resp.Msg, fmt.Sprintf("code %d", resp.Code)))
	}
	if resp.Data == nil || resp.Data.MessageId == nil || *resp.Data.MessageId == "" {
		return Message{}, errors.New("feishu message create returned empty message_id")
	}
	return Message{MessageID: *resp.Data.MessageId}, nil
}

type envelope struct {
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	Encrypt   string `json:"encrypt"`
	Header    struct {
		Token     string `json:"token"`
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		TenantKey string `json:"tenant_key"`
	} `json:"header"`
}

func parseEnvelope(body []byte) (envelope, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return envelope{}, err
	}
	return env, nil
}

func (e envelope) token() string {
	return firstNonEmpty(e.Token, e.Header.Token)
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
		if hasFeishuMention(msg.Metadata["mentions"]) {
			return false, ""
		}
		return true, "group mention required"
	}
}

func hasFeishuMention(value any) bool {
	switch mentions := value.(type) {
	case []imadapter.FeishuMention:
		return len(mentions) > 0
	case []any:
		return len(mentions) > 0
	default:
		return false
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

func feishuConversationID(metadataJSON string) string {
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

func inferReceiveIDType(receiveID string) string {
	receiveID = strings.TrimSpace(receiveID)
	switch {
	case strings.HasPrefix(receiveID, "oc_"):
		return "chat_id"
	case strings.HasPrefix(receiveID, "ou_"):
		return "open_id"
	case strings.HasPrefix(receiveID, "on_"):
		return "union_id"
	case strings.HasPrefix(receiveID, "user_"):
		return "user_id"
	default:
		return "chat_id"
	}
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.AppID = strings.TrimSpace(c.AppID)
	c.AppSecret = strings.TrimSpace(c.AppSecret)
	c.VerificationToken = strings.TrimSpace(c.VerificationToken)
	c.EncryptKey = strings.TrimSpace(c.EncryptKey)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.DefaultReceiveID = strings.TrimSpace(c.DefaultReceiveID)
	c.DefaultReceiveType = strings.TrimSpace(c.DefaultReceiveType)
	c.GroupMode = strings.ToLower(strings.TrimSpace(c.GroupMode))
	if c.GroupMode == "" {
		c.GroupMode = "mention"
	}
	c.APIBaseURL = strings.TrimSpace(c.APIBaseURL)
	return c
}

func (c Config) verifyToken(token string) error {
	if c.VerificationToken == "" {
		return nil
	}
	if subtleConstantTimeCompare(token, c.VerificationToken) {
		return nil
	}
	return ErrUnauthorizedCallback
}

func (c Callback) normalizer() imadapter.Normalizer {
	if c.Normalizer.Now != nil {
		return c.Normalizer
	}
	return imadapter.Normalizer{Now: c.Now}
}

func (c Callback) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
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

func apiBaseURL(cfg Config) string {
	return strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, "https://open.feishu.cn"), "/")
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
