package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imcoord"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

const defaultAPIBaseURL = "https://qyapi.weixin.qq.com"

var ErrUnauthorizedCallback = errors.New("unauthorized wecom callback")

type Config struct {
	EndpointID      string
	Mode            string
	CorpID          string
	CorpSecret      string
	AgentID         string
	CallbackToken   string
	CallbackAESKey  string
	DefaultTarget   string
	DefaultThreadID string
	DefaultUserID   string
	APIBaseURL      string
	AccessToken     string
	AllowFrom       string
	EnableMarkdown  bool
}

type Query struct {
	MsgSignature string
	Timestamp    string
	Nonce        string
	Echo         string
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
	MaxAttempts uint32
}

type SendResult struct {
	Delivery storage.OutboundDelivery
	Messages []SentMessage
}

type SentMessage struct {
	ErrCode int64  `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	MsgID   string `json:"msgid,omitempty"`
}

type TokenCache struct {
	mu          sync.Mutex
	AccessToken string
	ExpiresUnix int64
}

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

func (c *TokenCache) Set(token string, expiresUnix int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AccessToken = token
	c.ExpiresUnix = expiresUnix
}

type encryptedEnvelope struct {
	XMLName    xml.Name `xml:"xml"`
	ToUserName string   `xml:"ToUserName"`
	AgentID    string   `xml:"AgentID"`
	Encrypt    string   `xml:"Encrypt"`
}

type callbackMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	PicURL       string   `xml:"PicUrl"`
	MediaID      string   `xml:"MediaId"`
	FileName     string   `xml:"FileName"`
	Format       string   `xml:"Format"`
	MsgID        int64    `xml:"MsgId"`
	AgentID      int64    `xml:"AgentID"`
}

func ConfigFromEndpoint(endpoint storage.InteractionEndpoint) (Config, error) {
	var raw map[string]any
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.ConfigJSON), &raw); err != nil {
			return Config{}, fmt.Errorf("wecom config: %w", err)
		}
	}
	return Config{
		EndpointID:      endpoint.ID,
		Mode:            firstNonEmpty(stringValue(raw, "mode"), "callback_app"),
		CorpID:          stringValue(raw, "corp_id"),
		CorpSecret:      stringValue(raw, "corp_secret"),
		AgentID:         stringValue(raw, "agent_id"),
		CallbackToken:   stringValue(raw, "callback_token"),
		CallbackAESKey:  stringValue(raw, "callback_aes_key"),
		DefaultTarget:   stringValue(raw, "default_target"),
		DefaultThreadID: stringValue(raw, "default_thread_id"),
		DefaultUserID:   stringValue(raw, "default_user_id"),
		APIBaseURL:      stringValue(raw, "api_base_url"),
		AccessToken:     stringValue(raw, "access_token"),
		AllowFrom:       stringValue(raw, "allow_from"),
		EnableMarkdown:  boolValue(raw, "enable_markdown"),
	}, nil
}

func Signature(token, timestamp, nonce, encrypted string) string {
	parts := []string{strings.TrimSpace(token), strings.TrimSpace(timestamp), strings.TrimSpace(nonce), strings.TrimSpace(encrypted)}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func (w Webhook) VerifyURL(query Query) (string, error) {
	cfg := w.Config.normalize()
	if !verifySignature(cfg.CallbackToken, query) {
		return "", ErrUnauthorizedCallback
	}
	plain, err := decryptPayload(cfg.CallbackAESKey, cfg.CorpID, query.Echo)
	if err != nil {
		return "", err
	}
	return plain, nil
}

func (w Webhook) Handle(ctx context.Context, query Query, body []byte) (WebhookResult, error) {
	cfg := w.Config.normalize()
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return WebhookResult{Ignored: true, Reason: "empty callback"}, nil
	}
	var env encryptedEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return WebhookResult{}, err
	}
	query.Echo = env.Encrypt
	if !verifySignature(cfg.CallbackToken, query) {
		return WebhookResult{}, ErrUnauthorizedCallback
	}
	plain, err := decryptPayload(cfg.CallbackAESKey, cfg.CorpID, env.Encrypt)
	if err != nil {
		return WebhookResult{}, err
	}
	var msg callbackMessage
	if err := xml.Unmarshal([]byte(plain), &msg); err != nil {
		return WebhookResult{}, err
	}
	if msg.FromUserName == "" || msg.MsgType == "" {
		return WebhookResult{}, errors.New("wecom callback missing from_user_name or msg_type")
	}
	if !allowFrom(cfg.AllowFrom, msg.FromUserName) {
		return WebhookResult{Ignored: true, Reason: "sender not allowed"}, nil
	}
	eventBody, externalMessageID, err := msg.ToNormalizerJSON()
	if err != nil {
		return WebhookResult{}, err
	}
	event := imadapter.WeComRawEvent(imadapter.ProviderRawEventInput{
		EndpointID:        cfg.EndpointID,
		ExternalMessageID: externalMessageID,
		ReceivedUnix:      w.now().Unix(),
		Body:              eventBody,
		Metadata:          map[string]any{"transport": "callback_app", "mode": cfg.Mode},
	})
	normalized, err := w.normalizer().NormalizeInbound(ctx, event)
	if err != nil {
		return WebhookResult{}, err
	}
	normalized = applyConfig(normalized, cfg)
	draft, err := normalized.Draft()
	if err != nil {
		return WebhookResult{}, err
	}
	if w.Coordinator == nil {
		return WebhookResult{}, errors.New("wecom callback coordinator is required")
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

func (m callbackMessage) ToNormalizerJSON() ([]byte, string, error) {
	messageID := strconv.FormatInt(m.MsgID, 10)
	item := map[string]any{"type": 0}
	switch strings.ToLower(strings.TrimSpace(m.MsgType)) {
	case "text":
		item["type"] = 1
		item["text_item"] = map[string]any{"text": StripAtMentions(m.Content)}
	case "image":
		item["type"] = 2
		item["image_item"] = map[string]any{"url": m.PicURL, "media_id": m.MediaID}
	case "voice":
		item["type"] = 3
		item["voice_item"] = map[string]any{"text": "[voice]", "media_id": m.MediaID, "format": m.Format}
	case "file":
		item["type"] = 4
		item["file_item"] = map[string]any{"file_name": m.FileName, "media_id": m.MediaID}
	case "video":
		item["type"] = 5
		item["video_item"] = map[string]any{"media_id": m.MediaID}
	default:
		item["type"] = 0
		item["text_item"] = map[string]any{"text": "[" + strings.ToLower(strings.TrimSpace(m.MsgType)) + "]"}
	}
	payload := map[string]any{
		"message_id":   m.MsgID,
		"from_user_id": m.FromUserName,
		"session_id":   m.FromUserName,
		"item_list":    []map[string]any{item},
		"wecom": map[string]any{
			"to_user_name": m.ToUserName,
			"create_time":  m.CreateTime,
			"msg_type":     m.MsgType,
			"agent_id":     m.AgentID,
		},
	}
	out, err := json.Marshal(payload)
	return out, messageID, err
}

func (r Runtime) SendPending(ctx context.Context, limit int) ([]SendResult, error) {
	if r.Store == nil {
		return nil, errors.New("wecom runtime store is required")
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
		return SendResult{}, errors.New("wecom runtime store is required")
	}
	cfg := r.Config.normalize()
	if cfg.EndpointID == "" {
		cfg.EndpointID = delivery.EndpointID
	}
	message, err := r.Store.GetMessage(ctx, delivery.Target, delivery.MessageID)
	if err != nil {
		return SendResult{}, err
	}
	toUser := cfg.DefaultUserID
	if source, ok := sourceForReply(ctx, r.Store, message); ok {
		toUser = firstNonEmpty(toUser, wecomRecipientID(source.MetadataJSON))
	}
	if toUser == "" {
		failed, _ := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "failed", "wecom recipient user id is required", 0, 0)
		return SendResult{Delivery: failed}, errors.New("wecom recipient user id is required")
	}
	if err := r.outgoingLimiter().Wait(ctx, imadapter.ProviderWeCom); err != nil {
		return SendResult{Delivery: delivery}, err
	}
	sent, err := r.sendText(ctx, cfg, toUser, message.Content)
	if err != nil {
		updated, _ := r.Store.RecordOutboundDeliveryFailure(ctx, delivery.ID, err.Error(), r.maxAttempts(), time.Now().Unix())
		return SendResult{Delivery: updated}, err
	}
	updated, err := r.Store.UpdateOutboundDeliveryStatus(ctx, delivery.ID, "delivered", "", 0, time.Now().Unix())
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Delivery: updated, Messages: []SentMessage{sent}}, nil
}

func (r Runtime) sendText(ctx context.Context, cfg Config, toUser, text string) (SentMessage, error) {
	accessToken, err := r.accessToken(ctx, cfg)
	if err != nil {
		return SentMessage{}, err
	}
	msgType := "text"
	bodyKey := "text"
	content := outboundText(text)
	if cfg.EnableMarkdown {
		msgType = "markdown"
		bodyKey = "markdown"
	} else {
		content = stripMarkdown(content)
	}
	payload := map[string]any{
		"touser":  toUser,
		"msgtype": msgType,
		"agentid": parseAgentID(cfg.AgentID),
		bodyKey:   map[string]string{"content": content},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SentMessage{}, err
	}
	endpoint := strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, defaultAPIBaseURL), "/") + "/cgi-bin/message/send?access_token=" + url.QueryEscape(accessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SentMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return SentMessage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SentMessage{}, fmt.Errorf("wecom send HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out SentMessage
	if err := json.Unmarshal(data, &out); err != nil {
		return SentMessage{}, err
	}
	if out.ErrCode != 0 {
		return SentMessage{}, fmt.Errorf("wecom send rejected %d: %s", out.ErrCode, out.ErrMsg)
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
	if cfg.CorpID == "" || cfg.CorpSecret == "" {
		return "", errors.New("wecom corp_id and corp_secret are required for access_token")
	}
	endpoint := strings.TrimRight(firstNonEmpty(cfg.APIBaseURL, defaultAPIBaseURL), "/") + "/cgi-bin/gettoken?corpid=" + url.QueryEscape(cfg.CorpID) + "&corpsecret=" + url.QueryEscape(cfg.CorpSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		return "", fmt.Errorf("wecom access_token HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
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
		return "", fmt.Errorf("wecom access_token rejected %d: %s", out.ErrCode, out.ErrMsg)
	}
	r.TokenCache.Set(out.AccessToken, time.Now().Unix()+out.ExpiresIn)
	return out.AccessToken, nil
}

func decryptPayload(encodingAESKey, receiveID, encrypted string) (string, error) {
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypted))
	if err != nil {
		return "", err
	}
	if len(ciphertext) < aes.BlockSize || len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("wecom encrypted payload has invalid block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, ciphertext)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	if len(plain) < 20 {
		return "", errors.New("wecom encrypted payload is too short")
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if msgLen < 0 || 20+msgLen > len(plain) {
		return "", errors.New("wecom encrypted payload has invalid message length")
	}
	msg := string(plain[20 : 20+msgLen])
	gotReceiveID := strings.TrimSpace(string(plain[20+msgLen:]))
	if receiveID != "" && gotReceiveID != "" && gotReceiveID != strings.TrimSpace(receiveID) {
		return "", errors.New("wecom encrypted payload receive id mismatch")
	}
	return msg, nil
}

func decodeAESKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) != 43 {
		return nil, errors.New("encoding aes key must be 43 characters")
	}
	key, err := base64.StdEncoding.DecodeString(value + "=")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encoding aes key decoded to %d bytes, want 32", len(key))
	}
	return key, nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid pkcs7 data")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-pad], nil
}

func verifySignature(token string, query Query) bool {
	if strings.TrimSpace(query.MsgSignature) == "" || strings.TrimSpace(query.Timestamp) == "" || strings.TrimSpace(query.Nonce) == "" || strings.TrimSpace(query.Echo) == "" {
		return false
	}
	want := Signature(token, query.Timestamp, query.Nonce, query.Echo)
	return subtleConstantTimeCompare(strings.ToLower(strings.TrimSpace(query.MsgSignature)), want)
}

func StripAtMentions(text string) string {
	text = strings.TrimSpace(text)
	for {
		start := strings.Index(text, "<@")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], ">")
		if end < 0 {
			break
		}
		text = strings.TrimSpace(text[:start] + text[start+end+1:])
	}
	return strings.TrimSpace(text)
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

func wecomRecipientID(metadataJSON string) string {
	var metadata struct {
		IM struct {
			Conversation struct {
				ExternalID string `json:"external_id"`
			} `json:"conversation"`
			Sender struct {
				ExternalID string `json:"external_id"`
			} `json:"sender"`
		} `json:"im"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return ""
	}
	return firstNonEmpty(metadata.IM.Sender.ExternalID, metadata.IM.Conversation.ExternalID)
}

func (c Config) normalize() Config {
	c.EndpointID = strings.TrimSpace(c.EndpointID)
	c.Mode = canonicalMode(c.Mode)
	if c.Mode == "" {
		c.Mode = "callback_app"
	}
	c.CorpID = strings.TrimSpace(c.CorpID)
	c.CorpSecret = strings.TrimSpace(c.CorpSecret)
	c.AgentID = strings.TrimSpace(c.AgentID)
	c.CallbackToken = strings.TrimSpace(c.CallbackToken)
	c.CallbackAESKey = strings.TrimSpace(c.CallbackAESKey)
	c.DefaultTarget = strings.TrimSpace(c.DefaultTarget)
	c.DefaultThreadID = strings.TrimSpace(c.DefaultThreadID)
	c.DefaultUserID = strings.TrimSpace(c.DefaultUserID)
	c.APIBaseURL = strings.TrimSpace(c.APIBaseURL)
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.AllowFrom = strings.TrimSpace(c.AllowFrom)
	return c
}

func canonicalMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "websocket":
		return "websocket_bot"
	case "callback", "webhook":
		return "callback_app"
	default:
		return mode
	}
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

func (r Runtime) maxAttempts() uint32 {
	if r.MaxAttempts > 0 {
		return r.MaxAttempts
	}
	return 3
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

func boolValue(values map[string]any, key string) bool {
	value := strings.ToLower(stringValue(values, key))
	return value == "true" || value == "1" || value == "yes" || value == "on"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func allowFrom(allowList, userID string) bool {
	allowList = strings.TrimSpace(allowList)
	if allowList == "" || allowList == "*" {
		return true
	}
	for _, item := range strings.Split(allowList, ",") {
		if strings.TrimSpace(item) == strings.TrimSpace(userID) {
			return true
		}
	}
	return false
}

func parseAgentID(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func stripMarkdown(value string) string {
	replacer := strings.NewReplacer("**", "", "__", "", "`", "", "#", "", ">", "", "*", "", "_", "")
	return strings.TrimSpace(replacer.Replace(value))
}

func outboundText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(empty response)"
	}
	if len(value) <= 2000 {
		return value
	}
	return value[:2000]
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
