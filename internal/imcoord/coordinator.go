package imcoord

import (
	"context"
	"errors"
	"strings"

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
}

func New(store Store, loadAttachments AttachmentLoader) *Coordinator {
	return &Coordinator{
		store:           store,
		loadAttachments: loadAttachments,
		queue:           newSessionQueue(),
	}
}

func (c *Coordinator) Handle(ctx context.Context, draft Draft) (Result, error) {
	draft = normalizeDraft(draft)
	if err := validateDraft(draft); err != nil {
		return Result{}, err
	}
	key := sessionKey(draft)
	command, args := parseCommand(draft.Content)
	if command != "" {
		return c.handleCommand(ctx, key, command, args)
	}
	return c.queue.enqueue(ctx, key, func(qctx context.Context) (Result, error) {
		select {
		case <-qctx.Done():
			return Result{}, ErrAborted
		default:
		}
		message, err := c.createMessage(qctx, draft)
		if err != nil {
			return Result{}, err
		}
		return Result{Message: message, SessionKey: key}, nil
	})
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

func parseCommand(content string) (string, string) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") {
		return "", ""
	}
	command, args, _ := strings.Cut(content, " ")
	return strings.ToLower(command), strings.TrimSpace(args)
}

var _ = parseCommand
