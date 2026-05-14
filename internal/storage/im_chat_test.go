package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIMChatAuthRequestAndSubscriptionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	endpoint, err := store.CreateInteractionEndpoint(ctx, InteractionEndpoint{
		Kind:            "im",
		Provider:        "wecom",
		DisplayName:     "WeCom Ops",
		TargetPrefix:    "#",
		InboundEnabled:  true,
		OutboundEnabled: true,
		AuthMode:        "webhook_signature",
		ConfigJSON:      `{"mode":"callback_app","corp_id":"corp","corp_secret":"secret","agent_id":"1001","callback_token":"token","callback_aes_key":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"}`,
	})
	if err != nil {
		t.Fatalf("CreateInteractionEndpoint() error = %v", err)
	}

	request, err := store.CreateIMChatAuthRequest(ctx, IMChatAuthRequest{
		EndpointID:        endpoint.ID,
		Provider:          endpoint.Provider,
		ConversationID:    "chat-ops",
		ExternalThreadID:  "topic-7",
		ChatTitle:         "Ops Room",
		SenderExternalID:  "zhangsan",
		TokenHash:         "sha256:token-1",
		TokenPrefix:       "nk_abcd",
		Status:            IMChatAuthRequestStatusPending,
		ExpiresUnix:       time.Now().Add(time.Hour).Unix(),
		RequestedTarget:   "#ops",
		RequestedThreadID: "im-wecom-ops",
	})
	if err != nil {
		t.Fatalf("CreateIMChatAuthRequest() error = %v", err)
	}
	if request.ID == "" || request.Status != IMChatAuthRequestStatusPending || request.CreatedUnix == 0 {
		t.Fatalf("auth request = %+v, want persisted pending request", request)
	}
	if request.TokenHash != "" {
		t.Fatalf("auth request token hash must not be returned")
	}

	pending, err := store.ListIMChatAuthRequests(ctx, IMChatAuthRequestListOptions{
		EndpointID: endpoint.ID,
		Status:     IMChatAuthRequestStatusPending,
	})
	if err != nil {
		t.Fatalf("ListIMChatAuthRequests() error = %v", err)
	}
	if len(pending) != 1 || pending[0].ID != request.ID || pending[0].TokenPrefix != "nk_abcd" {
		t.Fatalf("pending auth requests = %+v, want created request", pending)
	}

	lookup, err := store.GetIMChatAuthRequestByTokenHash(ctx, "sha256:token-1")
	if err != nil {
		t.Fatalf("GetIMChatAuthRequestByTokenHash() error = %v", err)
	}
	if lookup.ID != request.ID || lookup.TokenHash != "" {
		t.Fatalf("lookup request = %+v, want redacted created request", lookup)
	}

	approved, subscription, err := store.ApproveIMChatAuthRequest(ctx, request.ID, "admin-1", IMChatSubscription{
		EndpointID:            endpoint.ID,
		Provider:              endpoint.Provider,
		ConversationID:        request.ConversationID,
		ExternalThreadID:      request.ExternalThreadID,
		ChatTitle:             request.ChatTitle,
		Target:                request.RequestedTarget,
		ThreadID:              request.RequestedThreadID,
		SenderExternalID:      request.SenderExternalID,
		AuthorizedByRequestID: request.ID,
		Subscribed:            true,
		Verbose:               false,
	})
	if err != nil {
		t.Fatalf("ApproveIMChatAuthRequest() error = %v", err)
	}
	if approved.Status != IMChatAuthRequestStatusApproved || approved.ResolvedByUserID != "admin-1" || approved.ResolvedUnix == 0 {
		t.Fatalf("approved request = %+v", approved)
	}
	if subscription.ID == "" || !subscription.Subscribed || subscription.Verbose || subscription.AuthorizedUnix == 0 {
		t.Fatalf("subscription = %+v, want active non-verbose subscription", subscription)
	}

	loaded, err := store.GetIMChatSubscription(ctx, endpoint.ID, "chat-ops", "topic-7")
	if err != nil {
		t.Fatalf("GetIMChatSubscription() error = %v", err)
	}
	if loaded.ID != subscription.ID || loaded.Target != "#ops" || loaded.ThreadID != "im-wecom-ops" {
		t.Fatalf("loaded subscription = %+v, want upserted subscription", loaded)
	}

	verbose := true
	updated, err := store.UpdateIMChatSubscription(ctx, subscription.ID, IMChatSubscriptionPatch{Verbose: &verbose})
	if err != nil {
		t.Fatalf("UpdateIMChatSubscription(verbose) error = %v", err)
	}
	if !updated.Verbose {
		t.Fatalf("updated subscription = %+v, want verbose on", updated)
	}

	activeOnly := true
	subs, err := store.ListIMChatSubscriptions(ctx, IMChatSubscriptionListOptions{
		EndpointID: endpoint.ID,
		Subscribed: &activeOnly,
	})
	if err != nil {
		t.Fatalf("ListIMChatSubscriptions() error = %v", err)
	}
	if len(subs) != 1 || subs[0].ID != subscription.ID {
		t.Fatalf("subscriptions = %+v, want active subscription", subs)
	}

	subscribed := false
	revoked, err := store.UpdateIMChatSubscription(ctx, subscription.ID, IMChatSubscriptionPatch{Subscribed: &subscribed})
	if err != nil {
		t.Fatalf("UpdateIMChatSubscription(subscribed=false) error = %v", err)
	}
	if revoked.Subscribed {
		t.Fatalf("revoked subscription = %+v, want unsubscribed", revoked)
	}
	if err := store.DeleteInteractionEndpoint(ctx, endpoint.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteInteractionEndpoint() error = %v, want ErrConflict while chat auth records exist", err)
	}
}
