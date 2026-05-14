package imcoord

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imadapter"
	"github.com/ca-x/nekode/internal/iminbound"
	"github.com/ca-x/nekode/internal/storage"
)

type Store interface {
	CreateMessage(context.Context, storage.Message) (storage.Message, error)
}

type AttachmentLoader func(context.Context, string, []string) ([]storage.Attachment, error)

type Coordinator struct {
	store           Store
	loadAttachments AttachmentLoader
	queue           *sessionQueue
	dedupe          *imadapter.DedupeCache
	startedAt       time.Time
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

func New(store Store, loadAttachments AttachmentLoader, opts ...Option) *Coordinator {
	c := &Coordinator{
		store:           store,
		loadAttachments: loadAttachments,
		queue:           newSessionQueue(),
		dedupe:          &imadapter.DedupeCache{},
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
	key := sessionKey(draft)
	command, args := parseCommand(draft.Content)
	if command != "" {
		return c.handleCommand(ctx, key, command, args)
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

func (c *Coordinator) handleCommand(_ context.Context, sessionKey, command, args string) (Result, error) {
	result := Result{
		HandledCommand: true,
		Command:        command,
		CommandArgs:    args,
		SessionKey:     sessionKey,
	}
	switch command {
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
	return strings.ToLower(command), strings.TrimSpace(args)
}

var _ = parseCommand
