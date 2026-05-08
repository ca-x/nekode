# IM Real Provider Runtime Plan

Status: task #177 SDK/API selection and integration boundary
Reference baseline: CherryHQ/stella at `120eced`

## Current Truth

Nekode does not yet have real Telegram, QQ, Feishu, or WeChat provider
runtimes. The current IM code provides:

- provider schemas, validation, and credential redaction;
- inbound `iminbound.RawEvent` and normalizer contracts;
- `MessageCoordinator` routing into existing Nekode messages;
- outbound delivery records, retry/status lifecycle, and mock gate fixtures;
- Telegram/QQ frame rendering helpers;
- Terminal local RawEvent/render helpers.

That is enough to test Nekode's internal IM boundary, but it is not a live
provider integration. A provider is live only after it can receive real
provider events, verify provider auth/signature, normalize into Nekode, send
real provider API messages from `OutboundDelivery`, and pass live smoke with
operator-owned credentials.

## Provider Status Matrix

| Provider | Nekode today | Live gap |
| --- | --- | --- |
| Terminal | Local input can be turned into `RawEvent`; outbound can render a terminal-readable line. | No end-to-end local runtime/smoke that creates endpoint, injects input, stores message, emits delivery, renders reply, and records delivery status. |
| Telegram | Schema, config validation, normalizer, and outbound frame rendering. | No Bot API SDK/runtime, webhook secret validation, inbound HTTP handler, `sendMessage`, or live smoke. |
| Feishu | Schema, config validation, normalizer, and mock fixtures. | No official SDK runtime, event verification/decryption, event callback/WebSocket receiver, `im.v1.message.create`, or live smoke. |
| QQ | Schema, config validation, normalizer, and outbound frame rendering. | No BotGo runtime, token refresh, event callback/WebSocket receiver, group/C2C send calls, or sandbox smoke. |
| WeChat/Weixin | Schema, config validation, normalizer, and mock fixtures. | No confirmed official/compliant runtime path, no iLink client, no QR/auth/session flow, no polling/send runtime, and no live smoke. |

## Stella Reference Matrix

Stella is useful as the reference implementation for channel shape and provider
edge handling, but Nekode must keep provider runtimes behind the existing
`InteractionEndpoint`, `iminbound.RawEvent`, `MessageCoordinator`, and
`OutboundDelivery` boundaries.

| Provider | Stella dependency/API | Stella receive path | Stella send path | Nekode decision |
| --- | --- | --- | --- | --- |
| Telegram | `gopkg.in/telebot.v4` | `tele.LongPoller`, handlers for commands/messages/callbacks, group-mode guard. | `Notify` sends MarkdownV2 messages, supports numeric chat IDs and `@channel`, chunks long text. | Use Stella's config, guard, normalization, and render ideas. Prefer webhook mode for server deployment because task #179 requires it and Nekode already exposes HTTP APIs; allow long polling only as an optional local-dev mode. |
| Feishu | `github.com/larksuite/oapi-sdk-go/v3` | WebSocket client with `dispatcher.NewEventDispatcher(verificationToken, encryptKey)`, `OnP2MessageReceiveV1`, reaction handler, message ID dedupe. | `client.Im.Message.Create` with `receive_id_type` derived from `ou_`, `on_`, or `oc_` prefixes. | Use official SDK v3. Support HTTP callback verification/decryption first if deployment wants provider callbacks; keep WebSocket mode as an accepted alternative when public callback ingress is not available. |
| QQ | `github.com/tencent-connect/botgo` | Token refresh, OpenAPI client, event handlers for C2C and group at-message, WebSocket session manager. | `PostGroupMessage` for `qq:group:` and `PostC2CMessage` for `qq:c2c:`/default C2C. | Use BotGo and copy Stella's target ID convention. Because BotGo README notes WebSocket is being phased down and webhook callbacks are in gray rollout, task #181 must decide the compliant receive mode before coding the runtime. |
| WeChat/Weixin | Custom iLink REST client over `go-resty/resty/v2`; no public Go SDK in Stella. | Long polling `GetUpdates`; QR endpoints for bot login; in-memory cursor/context tokens. | `/ilink/bot/sendmessage` using `bot_token` headers and cached `context_token`. | Treat as feasibility/compliance work, not a ready production channel. Do not ship as "official WeChat" until account terms, API availability, token/session persistence, and live test environment are confirmed. |
| Terminal | Local channel shape in Nekode, not external SDK. | Local operator input becomes the same inbound DTO shape as providers. | Render `OutboundDelivery` as terminal lines and mark status. | task #178 should finish first because it validates the runtime boundary without external accounts. |

## Official API Checks

- Telegram Bot API supports both `getUpdates` long polling and outgoing
  webhooks; these receive paths are mutually exclusive. `setWebhook` sends
  HTTPS POST updates and supports a `secret_token` header
  (`X-Telegram-Bot-Api-Secret-Token`). `sendMessage` sends text to `chat_id`.
  Source: <https://core.telegram.org/bots/api>.
- Feishu/Lark's official Go SDK is `github.com/larksuite/oapi-sdk-go/v3`; its
  README describes server API calls and event subscription handling. The SDK's
  IM package exposes `CreateMessageReqBuilder`, `ReceiveIdType`, and
  `CreateMessageResp.Success()` for message sending. Sources:
  <https://github.com/larksuite/oapi-sdk-go> and
  <https://pkg.go.dev/github.com/larksuite/oapi-sdk-go/v3/service/im/v1>.
- QQ BotGo is the official QQ bot Go SDK. Its README shows credential-based
  token refresh, OpenAPI initialization, event handler registration, webhook
  HTTP handler setup, and notes that WebSocket event delivery is being phased
  down while webhook callbacks are in gray rollout. Source:
  <https://github.com/tencent-connect/botgo>.
- Stella's Weixin runtime uses iLink REST endpoints; this plan did not find a
  broadly documented official public Go SDK equivalent in the project
  references. task #181 must validate compliance and account availability
  before implementation.

## Nekode Runtime Boundary

Add a provider runtime layer that is separate from the existing mock gate:

1. Runtime manager loads outbound-capable `InteractionEndpoint` records with
   `kind=im`, `provider=<provider>`, `inboundEnabled`, and `outboundEnabled`.
2. Each provider runtime resolves secrets from secret storage or an explicitly
   operator-owned local config source. It must not read raw secrets from
   redacted `ConfigJSON` placeholders.
3. Inbound HTTP callbacks, WebSocket events, long-poll updates, or local
   terminal input are converted into `iminbound.RawEvent`.
4. The existing `imadapter.Normalizer` and `imcoord.Coordinator` remain the only
   path from provider events into Nekode messages, threads, tasks, runs, and
   commands.
5. Provider runtimes must not call daemon execution or agent runtime APIs
   directly. Agent work continues through the existing Nekode server/daemon
   flow.
6. Outbound provider workers list pending `OutboundDelivery` records for their
   endpoint, render provider payloads, call the provider send API, then record
   `delivered`, `failed`, or `retrying` through the existing delivery lifecycle.
7. Runtime health must be visible without secrets: endpoint ID, provider,
   running/stopped/error, last inbound time, last outbound attempt, last error,
   and provider mode (`webhook`, `websocket`, `polling`, or `local`).

## Provider Implementation Order

1. task #178 Terminal live local channel smoke.
   - No external account required.
   - Acceptance: create terminal endpoint, inject local text, store normalized
     message with source metadata, enqueue outbound reply, render terminal
     output, mark delivery delivered, and cover it with focused tests/smoke.
2. task #179 Telegram webhook and send integration.
   - Use `telebot.v4` if it fits webhook handling cleanly; otherwise use the
     official Bot API directly for the thin webhook/send surface.
   - Acceptance: `setWebhook`/secret-token setup docs, HTTP webhook route,
     secret header validation, update dedupe, message normalization, group-mode
     guard, chunked `sendMessage`, delivery status updates, live bot smoke.
3. task #180 Feishu callback and send integration.
   - Use `larksuite/oapi-sdk-go/v3`.
   - Acceptance: verification token/encrypt key handling, callback challenge or
     WebSocket event path as selected, `im.message.receive_v1` normalization,
     dedupe, `Message.Create` send by chat/open/union ID, live tenant smoke.
4. task #181 QQ and WeChat feasibility plus compliant runtime plan.
   - QQ: verify whether the account can use webhook callback now or must use
     legacy WebSocket while available; use BotGo for token/OpenAPI/send.
   - WeChat: verify official or contractually allowed iLink/public account path,
     test account availability, QR/session/token persistence, media limits, and
     outbound context-token constraints before runtime coding.

## Acceptance Criteria for Provider Runtime Tasks

Every real provider task must prove these points before moving In Review:

- dependency/API choice is recorded with official doc links;
- credentials are redacted in API responses, UI, logs, and tests;
- inbound provider auth/signature/token checks are tested;
- provider retries or duplicate callbacks do not create duplicate Nekode
  messages;
- inbound messages reach `storage.Message` through `MessageCoordinator`;
- outbound sends consume `OutboundDelivery` and update delivery status;
- provider errors map to retryable/non-retryable status with visible `lastError`;
- live smoke evidence uses operator-owned credentials and does not commit
  secrets;
- mock fixtures remain for deterministic CI, but are not described as live
  provider coverage.

## Documentation Language Rule

Until #178-#181 pass their live smoke gates, release notes and UI copy must use
phrases like "IM adapter boundary", "provider schema", "mock gate", or "planned
runtime". They must not say "Telegram/QQ/Feishu/WeChat are connected" or "real
IM channels are available" unless the relevant provider runtime task is In
Review with live smoke evidence.
