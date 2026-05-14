package imcoord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/imauth"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

type Store interface {
	CreateMessage(context.Context, storage.Message) (storage.Message, error)
	GetInteractionEndpoint(context.Context, string) (storage.InteractionEndpoint, error)
	CreateIMChatAuthRequest(context.Context, storage.IMChatAuthRequest) (storage.IMChatAuthRequest, error)
	GetIMChatSubscription(context.Context, string, string, string) (storage.IMChatSubscription, error)
	UpdateIMChatSubscription(context.Context, string, storage.IMChatSubscriptionPatch) (storage.IMChatSubscription, error)
}

type AttachmentLoader func(context.Context, string, []string) ([]storage.Attachment, error)

type Coordinator struct {
	store           Store
	loadAttachments AttachmentLoader
	queue           *sessionQueue
	dedupe          *imadapter.DedupeCache
	startedAt       time.Time
	authToken       func() string
}

type Option func(*Coordinator)

func WithStartTime(startedAt time.Time) Option {
	return func(c *Coordinator) {
		c.startedAt = startedAt
	}
}

func WithDedupeTTL(ttl time.Duration) Option {
	return func(c *Coordinator) {
		if c.dedupe != nil {
			c.dedupe.TTL = ttl
		}
	}
}

func WithAuthTokenGenerator(fn func() string) Option {
	return func(c *Coordinator) {
		c.authToken = fn
	}
}

func New(store Store, loadAttachments AttachmentLoader, opts ...Option) *Coordinator {
	c := &Coordinator{
		store:           store,
		loadAttachments: loadAttachments,
		queue:           newSessionQueue(),
		dedupe:          &imadapter.DedupeCache{},
		authToken:       imauth.NewToken,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Coordinator) Handle(ctx context.Context, draft Draft) (Result, error) {
	draft = normalizeDraft(draft)
	if err := validateDraft(draft); err != nil {
		return Result{}, err
	}
	if c.isStale(draft) {
		return Result{}, ErrStaleDraft
	}
	dedupeMessage := draftDedupeMessage(draft)
	if c.dedupe != nil && c.dedupe.MarkSeen(dedupeMessage) {
		return Result{}, storage.ErrConflict
	}
	policy, identity, subscription, authErr := c.resolveChatPolicy(ctx, draft)
	if authErr != nil && !errors.Is(authErr, ErrUnauthorizedChat) {
		if c.dedupe != nil {
			c.dedupe.Forget(dedupeMessage)
		}
		return Result{}, authErr
	}
	key := sessionKey(draft)
	command, args := parseCommand(draft.Content)
	if policy.RequireSubscription {
		if command == "/subscribe" {
			return c.handleSubscribe(ctx, draft, key, identity, policy)
		}
		if authErr != nil {
			return Result{
				HandledCommand: true,
				Command:        command,
				CommandArgs:    args,
				SessionKey:     key,
				Response:       "Authorization required. Send /subscribe in this chat, then approve the pending request in Nekode.",
			}, authErr
		}
		draft = applySubscriptionRoute(draft, subscription)
		key = sessionKey(draft)
	}
	if command != "" {
		return c.handleCommand(ctx, draft, key, command, args, identity, subscription, policy)
	}
	result, err := c.queue.enqueue(ctx, key, func(qctx context.Context) (Result, error) {
		select {
		case <-qctx.Done():
			return Result{}, ErrAborted
		default:
		}
		message, err := c.createMessage(qctx, draft)
		if err != nil {
			if c.dedupe != nil && !errors.Is(err, storage.ErrConflict) {
				c.dedupe.Forget(dedupeMessage)
			}
			return Result{}, err
		}
		return Result{Message: message, SessionKey: key}, nil
	})
	if err != nil && c.dedupe != nil && !errors.Is(err, storage.ErrConflict) {
		c.dedupe.Forget(dedupeMessage)
	}
	return result, err
}

func (c *Coordinator) handleCommand(ctx context.Context, draft Draft, sessionKey, command, args string, identity chatIdentity, subscription storage.IMChatSubscription, policy chatPolicy) (Result, error) {
	result := Result{
		HandledCommand: true,
		Command:        command,
		CommandArgs:    args,
		SessionKey:     sessionKey,
	}
	switch command {
	case "/subscribe":
		return c.handleSubscribe(ctx, draft, sessionKey, identity, policy)
	case "/unsubscribe":
		if subscription.ID == "" {
			result.Response = "This chat is not subscribed."
			return result, nil
		}
		subscribed := false
		if _, err := c.store.UpdateIMChatSubscription(ctx, subscription.ID, storage.IMChatSubscriptionPatch{Subscribed: &subscribed}); err != nil {
			return Result{}, err
		}
		result.Response = "Unsubscribed. Send /subscribe to request access again."
		return result, nil
	case "/verbose":
		if subscription.ID == "" {
			result.Response = "Please /subscribe first."
			return result, nil
		}
		verbose := !subscription.Verbose
		if _, err := c.store.UpdateIMChatSubscription(ctx, subscription.ID, storage.IMChatSubscriptionPatch{Verbose: &verbose}); err != nil {
			return Result{}, err
		}
		if verbose {
			result.Response = "Verbose mode: on."
		} else {
			result.Response = "Verbose mode: off."
		}
		return result, nil
	case "/help":
		result.Response = "Commands: /subscribe, /unsubscribe, /verbose, /new, /agent <name>, /abort, /help."
		return result, nil
	case "/abort":
		if c.queue.abort(sessionKey) {
			result.Response = "Aborted."
			return result, nil
		}
		result.Response = "No active message to abort."
		return result, nil
	case "/new":
		result.Response = "New session requested."
		return result, nil
	case "/agent":
		if args == "" {
			result.Response = "Agent routing requires an agent name or id."
			return result, nil
		}
		result.Response = "Agent routing requested."
		return result, nil
	default:
		result.Response = "Unsupported command."
		return result, nil
	}
}

type chatPolicy struct {
	RequireSubscription bool
	Provider            string
	DefaultTarget       string
	DefaultThreadID     string
}

type chatIdentity struct {
	ConversationID   string
	ExternalThreadID string
	ChatTitle        string
}

func (c *Coordinator) resolveChatPolicy(ctx context.Context, draft Draft) (chatPolicy, chatIdentity, storage.IMChatSubscription, error) {
	identity := chatIdentityFromDraft(draft)
	endpoint, err := c.store.GetInteractionEndpoint(ctx, draft.SourceEndpointID)
	if errors.Is(err, storage.ErrNotFound) {
		return chatPolicy{}, identity, storage.IMChatSubscription{}, nil
	}
	if err != nil {
		return chatPolicy{}, identity, storage.IMChatSubscription{}, err
	}
	policy := chatPolicyFromEndpoint(endpoint)
	if !policy.RequireSubscription {
		return policy, identity, storage.IMChatSubscription{}, nil
	}
	sub, err := c.store.GetIMChatSubscription(ctx, draft.SourceEndpointID, identity.ConversationID, identity.ExternalThreadID)
	if errors.Is(err, storage.ErrNotFound) {
		return policy, identity, storage.IMChatSubscription{}, ErrUnauthorizedChat
	}
	if err != nil {
		return policy, identity, storage.IMChatSubscription{}, err
	}
	if !sub.Subscribed {
		return policy, identity, sub, ErrUnauthorizedChat
	}
	return policy, identity, sub, nil
}

func (c *Coordinator) handleSubscribe(ctx context.Context, draft Draft, sessionKey string, identity chatIdentity, policy chatPolicy) (Result, error) {
	token := ""
	if c.authToken != nil {
		token = strings.TrimSpace(c.authToken())
	}
	if token == "" {
		token = imauth.NewToken()
	}
	if token == "" {
		return Result{}, ErrAuthToken
	}
	if identity.ConversationID == "" {
		identity = chatIdentityFromDraft(draft)
	}
	expires := time.Now().Add(10 * time.Minute).Unix()
	_, err := c.store.CreateIMChatAuthRequest(ctx, storage.IMChatAuthRequest{
		EndpointID:        draft.SourceEndpointID,
		Provider:          policy.Provider,
		ConversationID:    identity.ConversationID,
		ExternalThreadID:  identity.ExternalThreadID,
		ChatTitle:         identity.ChatTitle,
		SenderExternalID:  draft.Sender.ExternalID,
		TokenHash:         imauth.HashToken(token),
		TokenPrefix:       imauth.TokenPrefix(token),
		Status:            storage.IMChatAuthRequestStatusPending,
		ExpiresUnix:       expires,
		RequestedTarget:   firstNonEmpty(draft.Target, policy.DefaultTarget),
		RequestedThreadID: firstNonEmpty(draft.ThreadID, policy.DefaultThreadID),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		HandledCommand: true,
		Command:        "/subscribe",
		SessionKey:     sessionKey,
		Response:       fmt.Sprintf("Authorization request created. Approve it in Nekode or bind with key: %s. This key expires in 10 minutes.", token),
	}, nil
}

func (c *Coordinator) createMessage(ctx context.Context, draft Draft) (storage.Message, error) {
	attachments, err := c.attachments(ctx, draft)
	if err != nil {
		return storage.Message{}, err
	}
	message, err := c.store.CreateMessage(ctx, draftToMessage(draft, attachments))
	if errors.Is(err, storage.ErrConflict) {
		return storage.Message{}, err
	}
	if err != nil {
		return storage.Message{}, err
	}
	return message, nil
}

func (c *Coordinator) attachments(ctx context.Context, draft Draft) ([]storage.Attachment, error) {
	if len(draft.AttachmentIDs) == 0 || c.loadAttachments == nil {
		return nil, nil
	}
	return c.loadAttachments(ctx, draft.Target, draft.AttachmentIDs)
}

func (c *Coordinator) isStale(draft Draft) bool {
	if c.startedAt.IsZero() || draft.ReceivedUnix == 0 {
		return false
	}
	return time.Unix(draft.ReceivedUnix, 0).Before(c.startedAt.Add(-2 * time.Second))
}

func draftDedupeMessage(draft Draft) iminbound.Message {
	return iminbound.Message{
		EndpointID:        draft.SourceEndpointID,
		ExternalMessageID: draft.ExternalMessageID,
	}
}

func parseCommand(content string) (string, string) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") {
		return "", ""
	}
	command, args, _ := strings.Cut(content, " ")
	return normalizeCommand(command), strings.TrimSpace(args)
}

var _ = parseCommand

func normalizeCommand(command string) string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "/sub":
		return "/subscribe"
	case "/unsub":
		return "/unsubscribe"
	case "/v":
		return "/verbose"
	case "/h":
		return "/help"
	default:
		return strings.ToLower(strings.TrimSpace(command))
	}
}

func applySubscriptionRoute(d Draft, sub storage.IMChatSubscription) Draft {
	if strings.TrimSpace(sub.Target) != "" {
		d.Target = strings.TrimSpace(sub.Target)
	}
	if strings.TrimSpace(sub.ThreadID) != "" {
		d.ThreadID = strings.TrimSpace(sub.ThreadID)
	}
	return d
}

func chatPolicyFromEndpoint(endpoint storage.InteractionEndpoint) chatPolicy {
	raw := map[string]any{}
	if strings.TrimSpace(endpoint.ConfigJSON) != "" {
		_ = json.Unmarshal([]byte(endpoint.ConfigJSON), &raw)
	}
	return chatPolicy{
		RequireSubscription: boolConfig(raw, "require_subscription", false) ||
			boolConfig(raw, "chat_auth_required", false) ||
			boolConfig(raw, "require_chat_authorization", false),
		Provider:        strings.TrimSpace(endpoint.Provider),
		DefaultTarget:   stringConfig(raw, "default_target"),
		DefaultThreadID: stringConfig(raw, "default_thread_id"),
	}
}

func chatIdentityFromDraft(d Draft) chatIdentity {
	var raw struct {
		IM struct {
			Conversation struct {
				ExternalID       string `json:"external_id"`
				DisplayName      string `json:"display_name"`
				ExternalThreadID string `json:"external_thread_id"`
			} `json:"conversation"`
		} `json:"im"`
	}
	_ = json.Unmarshal([]byte(d.MetadataJSON), &raw)
	identity := chatIdentity{
		ConversationID:   strings.TrimSpace(raw.IM.Conversation.ExternalID),
		ExternalThreadID: strings.TrimSpace(raw.IM.Conversation.ExternalThreadID),
		ChatTitle:        strings.TrimSpace(raw.IM.Conversation.DisplayName),
	}
	if identity.ConversationID == "" {
		identity.ConversationID = strings.TrimSpace(d.Sender.ExternalID)
	}
	if identity.ChatTitle == "" {
		identity.ChatTitle = strings.TrimSpace(d.Sender.DisplayName)
	}
	return identity
}

func boolConfig(raw map[string]any, key string, fallback bool) bool {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func stringConfig(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
