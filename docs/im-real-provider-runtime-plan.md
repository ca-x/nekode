# IM Real Provider Runtime Plan

Status: task #177 SDK/API selection and integration boundary
Reference baseline: CherryHQ/stella at `120eced`

## Current Truth

Nekode now has real local Terminal, Telegram webhook, Feishu callback, QQ
BotGo, Weixin official-account/iLink boundary, and ServerChan bot-go thin
runtimes. The shared IM boundary provides:

- provider schemas, validation, and credential redaction;
- inbound `iminbound.RawEvent` and normalizer contracts;
- `MessageCoordinator` routing into existing Nekode messages;
- outbound delivery records, retry/status lifecycle, and mock gate fixtures;
- Telegram/QQ/ServerChan frame rendering helpers;
- Terminal local RawEvent/render helpers.

That is enough to test Nekode's internal IM boundary, but it is not a live
provider integration. A provider is live only after it can receive real
provider events, verify provider auth/signature, normalize into Nekode, send
real provider API messages from `OutboundDelivery`, and pass live smoke with
operator-owned credentials.

## Provider Status Matrix

| Provider | Nekode today | Live gap |
| --- | --- | --- |
| Terminal | Local input, normalization, storage, outbound terminal render, and delivered-status smoke are implemented. | External provider smoke does not apply. |
| Telegram | Webhook route, secret-token validation, inbound normalization/storage, Bot API `sendMessage`, and delivery status updates are implemented. | Live bot smoke still requires operator-owned token and webhook URL. |
| Feishu | Plain callback route, verification-token URL challenge/event auth, inbound normalization/storage, tenant token fetch, OpenAPI `im/v1/messages`, and delivery status updates are implemented. | Encrypted callback decrypt support and live tenant smoke require operator-owned app credentials/callback URL. |
| QQ | Schema/config validation, normalizer, BotGo token/OpenAPI/WebSocket boundary, group/C2C send calls, and mocked send/storage tests are implemented. | QQ sandbox/live smoke still requires operator-owned bot credentials; BotGo webhook-vs-WebSocket compliance should stay visible in release notes. |
| WeChat/Weixin | Canonical provider id is `weixin`; `wechat` is accepted as an alias. Official-account callback/signature handling, customer-service send, and Stella iLink client fork boundary are implemented. | Generic QR binding API/Web panel is task #206; Weixin QR binding adapter/openid send gating is task #207; live public-account/iLink smoke requires operator-owned credentials and callback/QR environment. |
| ServerChan | Schema/config validation, Nekobot-derived bot-go polling receive, sendMessage delivery, normalizer, and mocked send/storage tests are implemented. | Live ServerChan send/poll smoke requires operator-owned bot token and chat ID. |

## Stella Reference Matrix

Stella is useful as the reference implementation for channel shape and provider
edge handling, but Nekode must keep provider runtimes behind the existing
`InteractionEndpoint`, `iminbound.RawEvent`, `MessageCoordinator`, and
`OutboundDelivery` boundaries.

| Provider | Stella dependency/API | Stella receive path | Stella send path | Nekode decision |
| --- | --- | --- | --- | --- |
| Telegram | `gopkg.in/telebot.v4` | `tele.LongPoller`, handlers for commands/messages/callbacks, group-mode guard. | `Notify` sends MarkdownV2 messages, supports numeric chat IDs and `@channel`, chunks long text. | Use Stella's config, guard, normalization, and render ideas. Prefer webhook mode for server deployment because task #179 requires it and Nekode already exposes HTTP APIs; allow long polling only as an optional local-dev mode. |
| Feishu | `github.com/larksuite/oapi-sdk-go/v3` | WebSocket client with `dispatcher.NewEventDispatcher(verificationToken, encryptKey)`, `OnP2MessageReceiveV1`, reaction handler, message ID dedupe. | `client.Im.Message.Create` with `receive_id_type` derived from `ou_`, `on_`, or `oc_` prefixes. | For task #180, use direct official HTTP APIs for the thin callback/send surface to avoid a broad SDK dependency: URL verification and plain `im.message.receive_v1` callbacks feed the shared normalizer, tenant token uses `/open-apis/auth/v3/tenant_access_token/internal`, and sends use `/open-apis/im/v1/messages`. Encrypted callbacks are explicitly rejected until decrypt support lands. |
| QQ | `github.com/tencent-connect/botgo` | Token refresh, OpenAPI client, event handlers for C2C and group at-message, WebSocket session manager. | `PostGroupMessage` for `qq:group:` and `PostC2CMessage` for `qq:c2c:`/default C2C. | Use BotGo and copy Stella's target ID convention. Keep live availability gated until QQ sandbox/live credentials prove the receive/send path. |
| WeChat/Weixin | Custom iLink REST client over `go-resty/resty/v2`; no public Go SDK in Stella. | Long polling `GetUpdates`; QR endpoints for bot login; in-memory cursor/context tokens. | `/ilink/bot/sendmessage` using `bot_token` headers and cached `context_token`. | Keep `weixin` as the canonical id. Official-account callback/send and iLink QR/send are implemented as thin provider paths; live production claims still need account/compliance and credential smoke. |
| ServerChan | Nekobot `pkg/channels/serverchan` bot-go shape. | Polling `getUpdates` with bot token and update offset. | `sendMessage` to numeric chat id. | Treat ServerChan as a product-level IM provider with polling receive/send, but keep it Not-live-smoked until an operator token/chat id proves it. |
| Terminal | Local channel shape in Nekode, not external SDK. | Local operator input becomes the same inbound DTO shape as providers. | Render `OutboundDelivery` as terminal lines and mark status. | Terminal validates the runtime boundary without external accounts. |

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
   - Implemented path: Nekode uses the official Bot API directly for webhook
     receive and `sendMessage`, keeping the runtime thin and testable without
     adding an SDK dependency. Configure Telegram with:
     `setWebhook(url=<base>/api/im/telegram/<endpoint_id>/webhook,
     secret_token=<secret_token>)`.
3. task #180 Feishu callback and send integration.
   - Use official Feishu OpenAPI HTTP endpoints directly for the thin runtime
     surface; keep `larksuite/oapi-sdk-go/v3` as the later broader SDK option
     if encrypted event dispatch or WebSocket receive mode is needed.
   - Acceptance: verification token/encrypt key handling, callback challenge or
     WebSocket event path as selected, `im.message.receive_v1` normalization,
     dedupe, `Message.Create` send by chat/open/union ID, live tenant smoke.
   - Implemented path: configure Feishu event callback URL as
     `<base>/api/im/feishu/<endpoint_id>/callback`, set
     `verification_token`, and subscribe to `im.message.receive_v1`. Nekode
     answers URL verification challenges, validates plain callback tokens,
     rejects encrypted payloads with an explicit unsupported error, normalizes
     events through `imcoord`, fetches tenant access tokens, sends text with
     OpenAPI `Message.Create`, and updates outbound delivery status. Local
     httptest smoke covers receive/auth/send; live tenant smoke needs
     operator-owned credentials and public callback URL.
4. task #202/#203/#207/#209 provider contract follow-up.
   - QQ uses BotGo for token/OpenAPI/send and remains live-gated until
     sandbox/live smoke proves the account delivery mode.
   - Weixin uses official-account callback/send plus Stella iLink QR binding
     and send gating; generic QR Web/API capability is task #206.
   - ServerChan uses the Nekobot bot-go polling/send shape and remains
     live-gated until an operator token/chat id proves it.
   - Detailed historical feasibility plan:
     `docs/im-wechat-qq-runtime-feasibility.md`.

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

Until task #205 passes the live smoke/docs gate, release notes and UI copy must
use phrases like "IM adapter boundary", "provider schema", "thin runtime", or
"not live-smoked". They must not say "Telegram/QQ/Feishu/Weixin/ServerChan are
production connected" or "real IM channels are available" unless the relevant
provider runtime has operator-owned credential smoke evidence.
