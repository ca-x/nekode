package weixin

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

var ErrUnauthorizedWebhook = errors.New("unauthorized wechat webhook")

type Config struct {
	EndpointID      string
	Mode            string
	BotToken        string
	BotID           string
	UserID          string
	BaseURL         string
	CDNBaseURL      string
	AppID           string
	AppSecret       string
	Token           string
	DefaultTarget   string
	DefaultThreadID string
	APIBaseURL      string
	AccessToken     string
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
	Echo    string
}

type Runtime struct {
	Config      Config
	Store       *storage.Store
	HTTPClient  *http.Client
	TokenCache  *TokenCache
	RateLimiter imadapter.OutgoingRateWaiter
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []CustomerServiceMessage
}

type CustomerServiceMessage struct {
	ErrCode int64  `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type TokenCache struct {
	mu          sync.Mutex
	AccessToken string
	ExpiresUnix int64
}

// Get returns the cached token if it is still valid for at least leeway seconds.
func (c *TokenCache) Get(leeway int64) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.AccessToken != "" && c.ExpiresUnix > time.Now().Unix()+leeway {
		return c.AccessToken, true
	}
	return "", false
}

// Set stores the token and its absolute expiry.
func (c *TokenCache) Set(token string, expiresUnix int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AccessToken = token
	c.ExpiresUnix = expiresUnix
}

type Query struct {
	Signature string
	Timestamp string
	Nonce     string
	Echo      string
}

type officialXMLMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgID        string   `xml:"MsgId"`
	MediaID      string   `xml:"MediaId"`
	PicURL       string   `xml:"PicUrl"`
	Format       string   `xml:"Format"`
	Recognition  string   `xml:"Recognition"`
	Title        string   `xml:"Title"`
	Description  string   `xml:"Description"`
	URL          string   `xml:"Url"`
	Event        string   `xml:"Event"`
	EventKey     string   `xml:"EventKey"`
	Encrypt      string   `xml:"Encrypt"`
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("wechat config: %w", err)
		}
	}
	return Config{
		EndpointID:      endpoint.ID,
		Mode:            firstNonEmpty(stringValue(raw, "mode"), "ilink"),
		BotToken:        stringValue(raw, "bot_token"),
		BotID:           stringValue(raw, "bot_id"),
		UserID:          stringValue(raw, "user_id"),
		BaseURL:         stringValue(raw, "base_url"),
		CDNBaseURL:      stringValue(raw, "cdn_base_url"),
		AppID:           stringValue(raw, "app_id"),
		AppSecret:       firstNonEmpty(stringValue(raw, "app_secret"), stringValue(raw, "app_secret_ref")),
		Token:           firstNonEmpty(stringValue(raw, "token"), stringValue(raw, "webhook_token"), stringValue(raw, "token_ref")),
		DefaultTarget:   stringValue(raw, "default_target"),
		DefaultThreadID: stringValue(raw, "default_thread_id"),
		APIBaseURL:      stringValue(raw, "api_base_url"),
		AccessToken:     stringValue(raw, "access_token"),
	}, nil
}

func VerifySignature(token string, query Query) bool {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(query.Signature) == "" || strings.TrimSpace(query.Timestamp) == "" || strings.TrimSpace(query.Nonce) == "" {
		return false
	}
	parts := []string{strings.TrimSpace(token), strings.TrimSpace(query.Timestamp), strings.TrimSpace(query.Nonce)}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return subtleConstantTimeCompare(strings.ToLower(strings.TrimSpace(query.Signature)), hex.EncodeToString(sum[:]))
}

func Signature(token, timestamp, nonce string) string {
	parts := []string{strings.TrimSpace(token), strings.TrimSpace(timestamp), strings.TrimSpace(nonce)}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func (w Webhook) VerifyURL(query Query) (string, error) {
	cfg := w.Config.normalize()
	if !VerifySignature(cfg.Token, query) {
		return "", ErrUnauthorizedWebhook
	}
	return query.Echo, nil
}

func (w Webhook) Handle(ctx context.Context, query Query, body []byte) (WebhookResult, error) {
	cfg := w.Config.normalize()
	if !VerifySignature(cfg.Token, query) {
		return WebhookResult{}, ErrUnauthorizedWebhook
	}
	if strings.EqualFold(query.Echo, "") && len(bytes.TrimSpace(body)) == 0 {
		return WebhookResult{Ignored: true, Reason: "empty callback"}, nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return WebhookResult{Echo: query.Echo}, nil
	}
	official, err := ParseOfficialAccountXML(body)
	if err != nil {
		return WebhookResult{}, err
	}
	if strings.TrimSpace(official.Encrypt) != "" {
		return WebhookResult{}, errors.New("wechat encrypted callbacks require encoding_aes_key support")
	}
	eventBody, externalMessageID, err := official.ToNormalizerJSON()
	if err != nil {
		return WebhookResult{}, err
	}
	event := imadapter.WeChatRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:        cfg.EndpointID,
		ExternalMessageID: externalMessageID,
		ReceivedUnix:      w.now().Unix(),
		Body:              eventBody,
		Metadata:          map[string]any{"transport": "official_account_callback", "mode": cfg.Mode},
	})
	msg, err := w.normalizer().NormalizeInbound(ctx, event)
	if err != nil {
		return WebhookResult{}, err
	}
	msg = applyConfig(msg, cfg)
	draft, err := msg.Draft()
	if err != nil {
		return WebhookResult{}, err
	}
	if w.Coordinator == nil {
		return WebhookResult{}, errors.New("wechat webhook coordinator is required")
	}
	result, err := w.Coordinator.Handle(ctx, draft)
	if errors.Is(err, storage.ErrConflict) {
		return WebhookResult{Ignored: true, Reason: "duplicate"}, nil
	}
	if errors.Is(err, imcoord.ErrStaleDraft) {
		return WebhookResult{Ignored: true, Reason: "stale"}, nil
	}
	if err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Message: result.Message}, nil
}

func ParseOfficialAccountXML(body []byte) (officialXMLMessage, error) {
	var msg officialXMLMessage
	if err := xml.Unmarshal(bytes.TrimSpace(body), &msg); err != nil {
		return officialXMLMessage{}, err
	}
	if strings.TrimSpace(msg.FromUserName) == "" || strings.TrimSpace(msg.ToUserName) == "" || strings.TrimSpace(msg.MsgType) == "" {
		return officialXMLMessage{}, errors.New("wechat callback missing from/to/msg_type")
	}
	return msg, nil
}

func (m officialXMLMessage) ToNormalizerJSON() ([]byte, string, error) {
	messageID := firstNonEmpty(m.MsgID, fmt.Sprintf("%d", m.CreateTime))
	item := map[string]any{"type": 0}
	switch strings.ToLower(strings.TrimSpace(m.MsgType)) {
	case "text":
		item["type"] = 1
		item["text_item"] = map[string]any{"text": m.Content}
	case "image":
		item["type"] = 2
		item["image_item"] = map[string]any{"url": m.PicURL, "media_id": m.MediaID}
	case "voice":
		item["type"] = 3
		item["voice_item"] = map[string]any{"text": firstNonEmpty(m.Recognition, "[voice]"), "media_id": m.MediaID, "format": m.Format}
	case "video", "shortvideo":
		item["type"] = 5
		item["video_item"] = map[string]any{"media_id": m.MediaID}
	case "link":
		item["type"] = 1
		item["text_item"] = map[string]any{"text": firstNonEmpty(m.Title, m.Description, m.URL)}
	case "event":
		item["type"] = 1
		item["text_item"] = map[string]any{"text": "[event: " + firstNonEmpty(m.Event, m.EventKey, "unknown") + "]"}
	default:
		item["type"] = 0
		item["text_item"] = map[string]any{"text": "[" + strings.ToLower(strings.TrimSpace(m.MsgType)) + "]"}
	}
	payload := map[string]any{
		"message_id":   parseInt64OrZero(messageID),
		"from_user_id": m.FromUserName,
		"session_id":   m.FromUserName,
		"item_list":    []map[string]any{item},
		"official_account": map[string]any{
			"to_user_name": m.ToUserName,
			"create_time":  m.CreateTime,
			"msg_type":     m.MsgType,
		},
	}
	out, err := json.Marshal(payload)
	return out, messageID, err
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("wechat runtime store is required")
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
		return SendResult{}, errors.New("wechat runtime store is required")
	}
	cfg := r.Config.normalize()
	if cfg.EndpointID == "" {
		cfg.EndpointID = delivery.EndpointID
	}
	if cfg.Mode == "ilink" {
		return r.sendILinkDelivery(ctx, cfg, delivery)
	}
	message, err := r.Store.GetMessage(ctx, delivery.Target, delivery.MessageID)
	if err != nil {
		return SendResult{}, err
	}
	openID := ""
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		openID = wechatConversationID(source.MetadataJSON)
	}
	if openID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "wechat official account openid is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("wechat official account openid is required")
	}
	if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderWeixin); err != nil {
		return SendResult{Delivery: delivery}, err
	}
	resp, err := r.sendCustomerServiceText(ctx, cfg, openID, message.Content)
	if err != nil {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
		return SendResult{Delivery: failed}, err
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: []CustomerServiceMessage{resp}}, nil
}

func (r Runtime) sendILinkDelivery(ctx context.Context, cfg Config, delivery storage.OutboundDelivery) (SendResult, error) {
	message, err := r.Store.GetMessage(ctx, delivery.Target, delivery.MessageID)
	if err != nil {
		return SendResult{}, err
	}
	toUserID := cfg.UserID
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		toUserID = firstNonEmpty(wechatConversationID(source.MetadataJSON), toUserID)
	}
	if cfg.BotToken == "" || toUserID == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "weixin ilink binding is required before send", 0, 0)
		return SendResult{Delivery: failed}, errors.New("weixin ilink binding is required before send")
	}
	contextToken := ""
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		contextToken = wechatContextToken(source.MetadataJSON)
	}
	client := NewILinkClient(cfg.BaseURL, cfg.CDNBaseURL, cfg.BotToken)
	if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderWeixin); err != nil {
		return SendResult{Delivery: delivery}, err
	}
	err = client.SendText(ctx, toUserID, contextToken, message.Content, r.httpClient())
	if err != nil {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", err.Error(), 0, 0)
		return SendResult{Delivery: failed}, err
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: []CustomerServiceMessage{{ErrCode: 0, ErrMsg: "ilink delivered"}}}, nil
}

func (r Runtime) sendCustomerServiceText(ctx context.Context, cfg Config, openID string, text string) (CustomerServiceMessage, error) {
	accessToken, err := r.accessToken(ctx, cfg)
	if err != nil {
		return CustomerServiceMessage{}, err
	}
	payload := map[string]any{"touser": openID, "msgtype": "text", "text": map[string]any{"content": outboundText(text)}}
	body, err := json.Marshal(payload)
	if err != nil {
		return CustomerServiceMessage{}, err
	}
	url := strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, "https://api.weixin.qq.com"), "/") + "/cgi-bin/message/custom/send?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CustomerServiceMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return CustomerServiceMessage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CustomerServiceMessage{}, fmt.Errorf("wechat customer-service send HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out CustomerServiceMessage
	if err := json.Unmarshal(data, &out); err != nil {
		return CustomerServiceMessage{}, err
	}
	if out.ErrCode != 0 {
		return CustomerServiceMessage{}, fmt.Errorf("wechat customer-service send rejected %d: %s", out.ErrCode, out.ErrMsg)
	}
	return out, nil
}

func (r Runtime) accessToken(ctx context.Context, cfg Config) (string, error) {
	if cfg.AccessToken != "" {
		return cfg.AccessToken, nil
	}
	if tok, ok := r.TokenCache.Get(60); ok {
		return tok, nil
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return "", errors.New("wechat app_id and app_secret are required for access_token")
	}
	url := strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, "https://api.weixin.qq.com"), "/") + "/cgi-bin/token?grant_type=client_credential&appid=" + cfg.AppID + "&secret=" + cfg.AppSecret
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("wechat access_token HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		ErrCode     int64  `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("wechat access_token rejected %d: %s", out.ErrCode, out.ErrMsg)
	}
	r.TokenCache.Set(out.AccessToken, time.Now().Unix()+out.ExpiresIn)
	return out.AccessToken, nil
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

func wechatConversationID(metadataJSON string) string {
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

func wechatContextToken(metadataJSON string) string {
	var metadata struct {
		ContextToken string `json:"context_token"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return ""
	}
	return strings.TrimSpace(metadata.ContextToken)
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode == "" {
		c.Mode = "ilink"
	}
	c.BotToken = strings.TrimSpace(c.BotToken)
	c.BotID = strings.TrimSpace(c.BotID)
	c.UserID = strings.TrimSpace(c.UserID)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.CDNBaseURL = strings.TrimSpace(c.CDNBaseURL)
	c.AppID = strings.TrimSpace(c.AppID)
	c.AppSecret = strings.TrimSpace(c.AppSecret)
	c.Token = strings.TrimSpace(c.Token)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.APIBaseURL = strings.TrimSpace(c.APIBaseURL)
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	return c
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

func (r Runtime) outgoingLimiter() imadapter.OutgoingRateWaiter {
	if r.RateLimiter != nil {
		return r.RateLimiter
	}
	return imadapter.DefaultOutgoingRateLimiter()
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseInt64OrZero(value string) int64 {
	var out int64
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int64(r-'0')
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

func outboundText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(empty response)"
	}
	return value
}
