package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi/options"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"
)

var (
	ErrIgnoredEvent = errors.New("ignored qq event")
)

type Config struct {
	EndpointID      string
	AppID           string
	AppSecret       string
	DefaultTarget   string
	DefaultThreadID string
	DefaultChatID   string
	GroupMode       string
}

type Runtime struct {
	Config        Config
	Store         *storage.Store
	Coordinator   *imcoord.Coordinator
	Normalizer    imadapter.Normalizer
	Now           func() time.Time
	API           OpenAPI
	RateLimiter   imadapter.OutgoingRateWaiter
	TokenSource   oauth2.TokenSource
	Session       SessionManager
	NewOpenAPI    func(context.Context, Config) (OpenAPI, oauth2.TokenSource, error)
	NewSession    func() SessionManager
	SequenceStart uint32
}

type OpenAPI interface {
	WS(ctx context.Context, params map[string]string, body string) (*dto.WebsocketAP, error)
	PostGroupMessage(ctx context.Context, groupID string, msg dto.APIMessage, opt ...options.Option) (*dto.Message, error)
	PostC2CMessage(ctx context.Context, userID string, msg dto.APIMessage, opt ...options.Option) (*dto.Message, error)
}

type SessionManager interface {
	Start(apInfo *dto.WebsocketAP, token oauth2.TokenSource, intents *dto.Intent) error
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []Message
}

type Message struct {
	ID        string `json:"id"`
	GroupID   string `json:"group_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("qq config: %w", err)
		}
	}
	return Config{
		EndpointID:      endpoint.ID,
		AppID:           stringValue(raw, "app_id"),
		AppSecret:       stringValue(raw, "app_secret"),
		DefaultTarget:   stringValue(raw, "default_target"),
		DefaultThreadID: stringValue(raw, "default_thread_id"),
		DefaultChatID:   firstNonEmpty(stringValue(raw, "default_target_id"), stringValue(raw, "channel_id")),
		GroupMode:       firstNonEmpty(strings.ToLower(stringValue(raw, "group_mode")), "mention"),
	}, nil
}

func (r Runtime) Start(ctx context.Context) error {
	cfg := r.Config.normalize()
	api, source, err := r.openAPI(ctx, cfg)
	if err != nil {
		return err
	}
	intent := event.RegisterHandlers(
		func(_ *dto.WSPayload, data *dto.WSC2CMessageData) error {
			_, err := r.HandleC2CMessage(ctx, (*dto.Message)(data))
			return err
		},
		func(_ *dto.WSPayload, data *dto.WSGroupATMessageData) error {
			_, err := r.HandleGroupMessage(ctx, (*dto.Message)(data))
			return err
		},
	)
	wsInfo, err := api.WS(ctx, nil, "")
	if err != nil {
		return fmt.Errorf("qq websocket info: %w", err)
	}
	session := r.session()
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Start(wsInfo, source, &intent)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (r Runtime) HandleC2CMessage(ctx context.Context, msg *dto.Message) (storage.Message, error) {
	return r.handleMessage(ctx, msg, false)
}

func (r Runtime) HandleGroupMessage(ctx context.Context, msg *dto.Message) (storage.Message, error) {
	cfg := r.Config.normalize()
	if cfg.GroupMode == "disabled" {
		return storage.Message{}, ErrIgnoredEvent
	}
	return r.handleMessage(ctx, msg, true)
}

func (r Runtime) handleMessage(ctx context.Context, msg *dto.Message, group bool) (storage.Message, error) {
	if msg == nil {
		return storage.Message{}, errors.New("qq message is nil")
	}
	cfg := r.Config.normalize()
	event, err := RawEventFromMessage(cfg.EndpointID, msg, group, r.now())
	if err != nil {
		return storage.Message{}, err
	}
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
			return storage.Message{}, errors.New("qq coordinator or store is required")
		}
		coord = imcoord.New(r.Store, nil)
	}
	result, err := coord.Handle(ctx, draft)
	if errors.Is(err, storage.ErrConflict) {
		return storage.Message{}, ErrIgnoredEvent
	}
	if errors.Is(err, imcoord.ErrStaleDraft) {
		return storage.Message{}, ErrIgnoredEvent
	}
	if err != nil {
		return storage.Message{}, err
	}
	return result.Message, nil
}

func RawEventFromMessage(endpointID string, msg *dto.Message, group bool, received time.Time) (iminbound.RawEvent, error) {
	if msg == nil {
		return iminbound.RawEvent{}, errors.New("qq message is nil")
	}
	authorID, authorName := "", ""
	if msg.Author != nil {
		authorID = msg.Author.ID
		authorName = msg.Author.Username
	}
	groupID := ""
	if group {
		groupID = firstNonEmpty(msg.GroupID, msg.ChannelID)
	}
	payload := map[string]any{
		"id":         msg.ID,
		"content":    msg.Content,
		"group_id":   groupID,
		"channel_id": msg.ChannelID,
		"author": map[string]any{
			"id":       authorID,
			"username": authorName,
		},
	}
	if strings.TrimSpace(string(msg.Timestamp)) != "" {
		payload["timestamp"] = string(msg.Timestamp)
		if parsed, err := msg.Timestamp.Time(); err == nil {
			received = parsed
		}
	}
	if len(msg.Attachments) > 0 {
		attachments := make([]map[string]any, 0, len(msg.Attachments))
		for _, attachment := range msg.Attachments {
			if attachment == nil {
				continue
			}
			attachments = append(attachments, map[string]any{
				"url":          attachment.URL,
				"filename":     attachment.FileName,
				"content_type": attachment.ContentType,
				"size":         attachment.Size,
			})
		}
		payload["attachments"] = attachments
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return iminbound.RawEvent{}, err
	}
	return imadapter.QQRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:        endpointID,
		ExternalMessageID: msg.ID,
		ReceivedUnix:      received.Unix(),
		Body:              body,
		Metadata:          map[string]any{"transport": "websocket"},
	}), nil
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("qq runtime store is required")
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
		return SendResult{}, errors.New("qq runtime store is required")
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
		conversationID = firstNonEmpty(conversationID, qqConversationID(source.MetadataJSON))
	}
	if conversationID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "qq target id is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("qq target id is required")
	}
	frames := imadapter.RenderQQOutbound(imadapter.OutboundRenderInput{
		Provider:                 imadapter.ProviderQQ,
		ConversationID:           conversationID,
		ReplyToExternalMessageID: delivery.ExternalMessageID,
		Text:                     message.Content,
		SequenceStart:            r.SequenceStart,
	})
	api, _, err := r.openAPI(ctx, cfg)
	if err != nil {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
		return SendResult{Delivery: failed}, err
	}
	sent := make([]Message, 0, len(frames))
	for _, frame := range frames {
		if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderQQ); err != nil {
			return SendResult{Delivery: delivery, Messages: sent}, err
		}
		msg, err := sendFrame(ctx, api, frame)
		if err != nil {
			failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
			return SendResult{Delivery: failed, Messages: sent}, err
		}
		sent = append(sent, Message{ID: msg.ID, GroupID: msg.GroupID, ChannelID: msg.ChannelID})
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: sent}, nil
}

func sendFrame(ctx context.Context, api OpenAPI, frame imadapter.OutboundFrame) (*dto.Message, error) {
	msg := dto.MessageToCreate{
		Content: frame.Text,
		MsgType: dto.TextMsg,
		MsgID:   frame.ReplyToExternalMessageID,
		MsgSeq:  frame.Sequence,
	}
	switch frame.TargetKind {
	case "group":
		return api.PostGroupMessage(ctx, frame.TargetID, msg)
	case "c2c":
		return api.PostC2CMessage(ctx, frame.TargetID, msg)
	default:
		return nil, fmt.Errorf("qq unsupported target kind %q", frame.TargetKind)
	}
}

func (r Runtime) openAPI(ctx context.Context, cfg Config) (OpenAPI, oauth2.TokenSource, error) {
	if r.API != nil {
		return r.API, r.TokenSource, nil
	}
	if r.NewOpenAPI != nil {
		return r.NewOpenAPI(ctx, cfg)
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, nil, errors.New("qq app_id and app_secret are required")
	}
	creds := &token.QQBotCredentials{AppID: cfg.AppID, AppSecret: cfg.AppSecret}
	source := token.NewQQBotTokenSource(creds)
	if err := token.StartRefreshAccessToken(ctx, source); err != nil {
		return nil, nil, fmt.Errorf("qq token refresh: %w", err)
	}
	return botgo.NewOpenAPI(creds.AppID, source).WithTimeout(10 * time.Second), source, nil
}

func (r Runtime) session() SessionManager {
	if r.Session != nil {
		return r.Session
	}
	if r.NewSession != nil {
		return r.NewSession()
	}
	return botgo.NewSessionManager()
}

func (r Runtime) outgoingLimiter() imadapter.OutgoingRateWaiter {
	if r.RateLimiter != nil {
		return r.RateLimiter
	}
	return imadapter.DefaultOutgoingRateLimiter()
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

func qqConversationID(metadataJSON string) string {
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
	c.AppID = strings.TrimSpace(c.AppID)
	c.AppSecret = strings.TrimSpace(c.AppSecret)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.DefaultChatID = strings.TrimSpace(c.DefaultChatID)
	c.GroupMode = strings.ToLower(strings.TrimSpace(c.GroupMode))
	if c.GroupMode == "" {
		c.GroupMode = "mention"
	}
	return c
}

func (r Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
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
