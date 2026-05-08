# IM Channel Integration

Status: task #165 documentation baseline
Audience: implementers, reviewers, and deployment operators

This document defines the first-version IM integration plan for Nekode. It is
the deployment and attribution companion to tasks #156-#165.

## Design Boundary

IM is an `InteractionEndpoint` ingress and egress layer for Nekode. It is not a
second chat system.

Nekode remains the source of truth for targets, threads, messages, tasks, runs,
activity, attachments, notification routing, and daemon execution. IM adapters
only translate provider events and provider API calls at the edge.

The shared flow is:

1. A platform adapter receives a webhook, websocket, long-poll event, or SDK
   callback from Feishu, WeChat, Telegram, QQ, or another provider.
2. The adapter verifies the request, normalizes the provider payload into
   `internal/iminbound.Message`, and deduplicates with
   `EndpointID + ExternalMessageID`.
3. The coordinator maps endpoint, external conversation, sender, mention mode,
   and commands onto existing Nekode target/thread/session state.
4. Inbound content is stored as an existing `storage.Message` with
   `SourceEndpointID`, `ExternalMessageID`, attachment IDs, and metadata JSON.
5. Agent execution uses the existing Nekode daemon/run/direct-message flow. IM
   adapters must not call runtime processes directly.
6. Outbound delivery renders Nekode messages, activity, run updates, and
   notifications back through provider-specific send APIs with retry and
   delivery status.

## Task Map

The first-version launch loop is covered by:

| Task | Scope |
| --- | --- |
| task #156 | Inbound adapter contract and normalized DTO |
| task #157 | MessageCoordinator identity routing, commands, and session queue |
| task #158 | Outbound dispatcher retry and delivery status lifecycle |
| task #159 | Shared adapter registry/runtime plus Feishu and WeChat adapters |
| task #160 | Media normalization and attachment persistence |
| task #161 | Per-group system prompt and tool policy overrides |
| task #162 | Notification preferences and endpoint routing |
| task #163 | Endpoint/channel configuration UI and credential redaction |
| task #164 | End-to-end integration gate and mock platform fixtures |
| task #165 | Deployment docs and CherryHQ/stella attribution |
| task #166 | Standby or follow-up split for Telegram and QQ if task #159 is parallelized |
| task #167 | Standby or follow-up split for Terminal if task #159 is parallelized |

Do not create parallel tasks for the same first-version gaps without first
checking this map. Command authorization belongs in tasks #157 and #161.
Delivery receipts and retry state belong in tasks #158 and #162. Mock fixtures,
debug logs, and gate evidence belong in task #164.

## InteractionEndpoint Shape

Each IM channel instance is represented by `storage.InteractionEndpoint`.

Required fields:

- `Kind`: `im`
- `Provider`: provider identifier such as `feishu`, `weixin`, `telegram`, `qq`,
  or `custom`
- `DisplayName`: operator-facing channel name
- `TargetPrefix`: default Nekode target prefix, usually `#`
- `InboundEnabled`: whether the adapter accepts provider messages
- `OutboundEnabled`: whether Nekode can send messages back through the provider
- `AuthMode`: provider edge auth mode, usually `signature`, `bearer`, or
  `custom`
- `ConfigJSON`: non-secret provider and routing configuration only

Sensitive values must not be stored directly in `ConfigJSON`. Store secret
references or redacted placeholders there, and let task #163 define the storage
and UI redaction behavior.

Example non-secret config shape:

```json
{
    "provider": "feishu",
  "app_id": "cli_xxx",
  "app_secret_ref": "secret:im/feishu/prod/app_secret",
  "verification_token_ref": "secret:im/feishu/prod/verification_token",
  "encrypt_key_ref": "secret:im/feishu/prod/encrypt_key",
  "webhook_path": "/api/im/feishu/events",
  "default_target": "#general",
  "group_mode": "mention",
  "allowed_conversations": ["oc_xxx"],
  "agent_profile_id": "agent_profile_default",
  "system_prompt_id": "prompt_default",
  "tool_policy_id": "tool_policy_default"
}
```

The exact provider fields should follow the adapter implementation, but the
rules are stable:

- keep provider credentials in secret storage, not plain JSON;
- store external user/group identifiers as external identities, not Nekode user
  IDs;
- bind groups and private chats to Nekode targets through endpoint routing;
- default group behavior to `mention`, with `always` and `disabled` available;
- keep platform-specific fields under provider-scoped keys when possible.

## Provider Coverage

The first version should cover the IM providers that Stella already supports:
Telegram, QQ, Feishu, WeChat, and Terminal. Task #159 owns the shared provider
registry/config/normalizer and the first concrete Feishu and WeChat adapters.
Tasks #166 and #167 were created as split points for Telegram/QQ and Terminal;
if they stay closed as duplicate/standby tasks, task #159 remains responsible
for keeping the shared adapter family compatible with those providers and task
#164 remains responsible for five-provider fixtures.

The common acceptance bar for every provider is:

- provider config can be represented as a redacted `InteractionEndpoint`
  config;
- inbound provider events normalize into `internal/iminbound.Message`;
- provider message IDs feed `ExternalMessageID` for dedupe;
- provider user and conversation IDs stay external and are not treated as
  Nekode user IDs;
- media references use the task #160 attachment path when applicable;
- outbound rendering can be mocked by task #164 fixtures;
- Stella-derived structure, field names, or copied code are attributed.

Real credential smoke can be phased by provider availability, but mock fixtures
for all five providers are part of the first-version gate.

## Channel Configuration UX

Task #163 should make IM channel creation feel like provider onboarding, not a
raw JSON editor. The UI can reference Stella's channel onboarding/config
patterns for provider selection and setup flow while still writing Nekode's
`InteractionEndpoint` records.

Expected channel add flow:

- provider picker for Telegram, QQ, Feishu, WeChat, Terminal, and custom;
- provider-specific required fields with clear labels and validation;
- secret fields presented as write-only inputs after save;
- redacted API responses and list/detail views;
- callback URL and setup checklist generated from `NEKODE_BASE_URL` and the
  selected provider;
- inbound/outbound enable toggles;
- group mode selection (`mention`, `always`, `disabled`);
- default target/channel binding;
- visible connection or last-event status once adapters provide it;
- README/docs attribution that the channel creation/config interaction follows
  CherryHQ/stella where compatible.

Expected channel binding flow:

- bind an IM endpoint conversation to a Nekode channel, thread, default target,
  or agent route;
- show provider conversation identity separately from Nekode target identity;
- allow a default route for unknown conversations when enabled by policy;
- configure group strategy per binding: `mention`, `always`, or `disabled`;
- select a default agent/profile when a binding should trigger agent replies;
- show whether inbound and outbound are enabled for that binding;
- keep binding changes auditable through existing activity/message metadata
  where the implementation provides it.

The UI must not expose raw credential values after creation. If a credential
needs rotation, the operator should replace it through a write-only field.

## Deployment Steps

For each IM channel:

1. Register the provider app or bot in the provider console.
2. Create a Nekode `InteractionEndpoint` with `kind=im`, the provider name,
   routing defaults, and redacted/secret-referenced credentials.
3. Configure the provider callback URL to the Nekode public base URL and the
   adapter path, for example:

   ```text
   https://nekode.example.com/api/im/<provider>/events
   ```

4. Enable signature or token verification before accepting traffic.
5. Send a provider test event and confirm task #156 normalization records the
   provider message ID, sender, conversation, content blocks, and metadata.
6. Confirm the coordinator writes an existing Nekode message with
   `SourceEndpointID` and `ExternalMessageID`.
7. Confirm agent replies and notifications go through the outbound dispatcher,
   not through direct runtime calls from the adapter.
8. Run the task #164 mock platform gate before treating the channel as usable.

For local development, use mock platform fixtures first. Real provider
credentials should be used only in an operator-owned environment and must never
be committed to the repository.

## Media Handling

IM media must use Nekode's existing attachment path.

Provider adapters may download provider files or resolve provider media URLs,
but persistence should flow through the reusable attachment service from task
#160. The resulting Nekode attachment IDs are attached to the normalized
inbound message and stored on `storage.Message.Attachments`.

Do not add a provider-specific media store for first-version IM support.

## Outbound Delivery

Outbound delivery should render existing Nekode messages, activity, and
notifications into provider-specific payloads.

The dispatcher owns:

- endpoint selection;
- idempotency;
- retry scheduling;
- delivery status;
- provider response mapping;
- external provider message IDs.

Provider adapters own only the API translation and response parsing.

## CherryHQ/stella Attribution

Nekode's IM channel work references
[CherryHQ/stella](https://github.com/CherryHQ/stella). The local reference used
for task #155 was commit `120eced`.

Stella is MIT licensed:

```text
MIT License
Copyright (c) 2024 Vaayne
```

The first-version Nekode IM plan intentionally references Stella's channel
architecture:

- managed channel runtime shape;
- provider-specific channel creation/config fields;
- Telegram, QQ, Feishu, WeChat, and Terminal adapter organization;
- normalized inbound event boundary;
- central coordinator responsibilities;
- per-session FIFO queue and abort behavior;
- command parsing structure;
- streaming/output renderer shape;
- media and attachment flow ideas;
- notification routing patterns.

When code is copied or substantially derived from Stella, keep the MIT license
notice with the copied portion or add it to the relevant project notice file.
When only design structure or field naming is reused, keep this document and the
README reference as the attribution trail.

Before marking platform adapter work done, reviewers should check:

- no Stella-derived code was added without MIT attribution;
- provider secrets are redacted in logs, API responses, Web UI, and tests;
- copied provider field names are documented as Stella-compatible where that is
  intentional;
- task #164 covers mock fixtures for Telegram, QQ, Feishu, WeChat, and
  Terminal;
- dependencies introduced by provider SDKs are reviewed separately and are not
  added implicitly by documentation work.

## Acceptance Checklist

- `README.md` links to this document.
- The IM architecture is described as an `InteractionEndpoint` integration.
- Deployment steps include provider setup, callback URL, signature verification,
  and mock gate validation.
- Provider coverage includes Telegram, QQ, Feishu, WeChat, and Terminal.
- `ConfigJSON` is documented as non-secret; credentials use secret references or
  redacted placeholders.
- Stella reference scope and MIT attribution are explicit.
- Task #164 remains the required end-to-end gate for runnable validation.
