# IM Real Provider Runtime Plan

Status: task #205 live smoke/docs gate
Reference baseline: CherryHQ/stella at `120eced`

## Current Truth

Nekode now has real local Terminal, Telegram webhook/runtime through
`telebot.v4`, Feishu callback/runtime through the official
`larksuite/oapi-sdk-go/v3`, QQ BotGo, Weixin official-account plus
`github.com/lib-x/ilink` iLink bot boundaries, and ServerChan bot-go thin
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
| Telegram | Webhook route, secret-token validation, inbound normalization/storage, `telebot.v4` send boundary, and delivery status updates are implemented. | Live bot smoke still requires operator-owned token and webhook URL. |
| Feishu | Plain callback route, verification-token URL challenge/event auth, inbound normalization/storage, official `larksuite/oapi-sdk-go/v3` tenant-token/message send boundary, and delivery status updates are implemented. | Encrypted callback decrypt support and live tenant smoke require operator-owned app credentials/callback URL. |
| QQ | Schema/config validation, normalizer, BotGo token/OpenAPI/WebSocket boundary, group/C2C send calls, and mocked send/storage tests are implemented. | QQ sandbox/live smoke still requires operator-owned bot credentials; BotGo webhook-vs-WebSocket compliance should stay visible in release notes. |
| WeChat/Weixin | Canonical provider id is `weixin`; `wechat` is accepted as an alias. Official-account callback/signature handling and customer-service send are implemented through the WeChat public-account HTTP API. `mode=ilink` bot messaging uses `github.com/lib-x/ilink`; QR ticket/status remains a narrow compatibility adapter because the SDK exposes only a blocking login wait flow. | Generic QR binding API/Web panel is task #206; Weixin QR binding adapter/openid send gating is task #207; live public-account/iLink smoke requires operator-owned credentials and callback/QR environment. |
| ServerChan | Schema/config validation, Nekobot-derived bot-go polling receive, sendMessage delivery, normalizer, and mocked send/storage tests are implemented. | Live ServerChan send/poll smoke requires operator-owned bot token and chat ID. |

## Task #205 Live Smoke Gate Matrix

Gate baseline: `main@b5d31c9`.

This gate separates deterministic local evidence from real external-provider
smoke evidence. The local test suite proves that each thin runtime can validate
provider-shaped input, normalize into Nekode, consume source-only outbound
deliveries, and mark delivery status when the provider API is represented by
`httptest` or a fake SDK boundary. It does not prove production connectivity to
Telegram, Feishu, QQ, Weixin, or ServerChan.

No operator-owned external provider credentials, public callback URL, QQ
sandbox account, Weixin public-account/iLink account, or ServerChan bot token
were available in this environment. Therefore every external-provider live
smoke below is explicitly `Not-tested`. Release notes, Web copy, and final
regression reports must not describe those providers as production connected or
release green until the corresponding live smoke is rerun with real credentials.

Local verification commands run for this gate:

- `go test ./internal/imadapter ./internal/imbinding ./internal/imchannels/terminal ./internal/imchannels/telegram ./internal/imchannels/feishu ./internal/imchannels/qq ./internal/imchannels/weixin ./internal/imchannels/serverchan ./internal/server -count=1`
- `go test ./... -count=1 -timeout=180s`
- `go build ./...`
- `npm --prefix web run typecheck -- --pretty false`
- `npm --prefix web run build`
- `git diff --check`

| Provider | Local verification result | Real external credentials/callback status | Release gate judgment |
| --- | --- | --- | --- |
| Terminal | `Passed`: local channel input normalizes into the shared IM DTO shape, drafts keep endpoint/target/thread metadata, and outbound rendering includes delivery status. Evidence: `go test ./internal/imchannels/terminal ./internal/imadapter ./internal/imbinding ./internal/server -count=1`. | Not applicable: Terminal is local-only and has no external provider account or callback. | `Green for local-dev provider only`. It may be used as the local IM smoke path in task #199, but it does not prove any external provider. |
| Telegram | `Passed locally`: webhook secret-token validation, group mention filtering, inbound storage, source-only outbound delivery, `telebot.v4` `Send` boundary, and delivered status are covered with local `httptest`. | `Not-tested`: requires operator-owned bot token, configured `setWebhook` URL, reachable public HTTPS callback, and a real chat/channel ID. | `Implemented, not live-smoked`. Keep `runtimeStatus=implemented_not_live_smoked`; do not mark Telegram production-ready until live bot receive/send smoke passes. |
| Feishu | `Passed locally`: URL verification challenge, plain callback verification-token auth, encrypted-payload rejection, group mention filtering, official SDK tenant-token/message send boundary, and delivered status are covered with local `httptest`. | `Not-tested`: requires operator-owned Feishu app credentials, event subscription, verification token, public callback URL, tenant access, and real chat/open/union ID. Encrypted callbacks are intentionally unsupported in the current callback surface. | `Implemented, not live-smoked`. Plain callbacks can remain available with explicit encrypted-callback caveat; do not mark Feishu production-ready until tenant live smoke passes. |
| QQ | `Passed locally`: BotGo DTO group receive, normalized storage, source-only outbound delivery, group/C2C send boundary, and delivered status are covered with a fake QQ OpenAPI boundary. | `Not-tested`: requires operator-owned QQ bot app ID/secret, sandbox or live bot access, event delivery mode validation, and real group/C2C target. | `Implemented, not live-smoked`. QQ must remain live-gated; BotGo WebSocket-vs-webhook delivery-mode caveat stays visible in release notes. |
| Weixin official account | `Passed locally`: callback signature URL verification, XML text callback normalization/storage, customer-service access-token fetch shape, send request shape, and delivered status are covered with local `httptest`. | `Not-tested`: requires operator-owned WeChat public account or test account, app credentials, callback token, public callback URL, user openid, and a valid customer-service reply window. | `Implemented, not live-smoked`. Official-account mode must not be called production connected until public-account live receive/send smoke passes. |
| Weixin iLink QR | `Passed locally`: generic QR binding session contract from task #206 is consumed only for `mode=ilink`, iLink QR ticket/status calls are represented by `httptest`, bound bot config is persisted, official-account endpoints reject QR session creation, unbound iLink sends fail, and bound bot sends go through `github.com/lib-x/ilink` and mark delivery delivered. | `Not-tested`: requires operator-owned iLink/Weixin environment, QR scan by a real account, bound bot token/user ID, and live send path. | `Implemented, not live-smoked`. Web may show QR binding only for Weixin endpoints whose config is `mode=ilink`; iLink remains not production green until live QR bind/send smoke passes. |
| ServerChan | `Passed locally`: Nekobot-derived polling update shape, allow-list rejection, inbound storage, source-only outbound delivery, `sendMessage` request shape, and delivered status are covered with local `httptest`. | `Not-tested`: requires operator-owned ServerChan bot token, real chat ID, and live poll/send confirmation. | `Implemented, not live-smoked`. ServerChan is a product-level provider in the schema, but not release green until live bot token smoke passes. |

The task #205 gate is considered complete only for documentation/review
purposes: the implementation is locally verified and the live-smoke gaps are
explicit. It does not upgrade any external provider from
`implemented_not_live_smoked` to production-ready.

## Stella Reference Matrix

Stella is useful as the reference implementation for channel shape and provider
edge handling, but Nekode must keep provider runtimes behind the existing
`InteractionEndpoint`, `iminbound.RawEvent`, `MessageCoordinator`, and
`OutboundDelivery` boundaries.

| Provider | Stella dependency/API | Stella receive path | Stella send path | Nekode decision |
| --- | --- | --- | --- | --- |
| Telegram | `gopkg.in/telebot.v4` | `tele.LongPoller`, handlers for commands/messages/callbacks, group-mode guard. | `Notify` sends MarkdownV2 messages, supports numeric chat IDs and `@channel`, chunks long text. | Use Stella's config, guard, normalization, and render ideas. Prefer webhook mode for server deployment because task #179 requires it and Nekode already exposes HTTP APIs; allow long polling only as an optional local-dev mode. |
| Feishu | `github.com/larksuite/oapi-sdk-go/v3` | WebSocket client with `dispatcher.NewEventDispatcher(verificationToken, encryptKey)`, `OnP2MessageReceiveV1`, reaction handler, message ID dedupe. | `client.Im.Message.Create` with `receive_id_type` derived from `ou_`, `on_`, or `oc_` prefixes. | Use the official SDK for tenant-token/message send. The first callback surface still accepts plain URL verification and `im.message.receive_v1` HTTP callbacks behind Nekode's normalizer; encrypted callbacks remain explicitly rejected until decrypt support lands. |
| QQ | `github.com/tencent-connect/botgo` | Token refresh, OpenAPI client, event handlers for C2C and group at-message, WebSocket session manager. | `PostGroupMessage` for `qq:group:` and `PostC2CMessage` for `qq:c2c:`/default C2C. | Use BotGo and copy Stella's target ID convention. Keep live availability gated until QQ sandbox/live credentials prove the receive/send path. |
| WeChat/Weixin | `github.com/lib-x/ilink` for iLink bot messaging; WeChat public-account official HTTP API for `official_account`; narrow direct QR/status adapter for iLink binding sessions until the SDK exposes nonblocking poll with base-URL injection. | Official-account callbacks arrive through WeChat HTTP XML callbacks. iLink receive can use the SDK's `ListenAndServe`; Nekode's current local gate focuses on binding and outbound send. | Official-account sends use customer-service HTTP send. iLink sends use `Client.Send` with bound `bot_token` and cached `context_token`. | Keep `weixin` as the canonical id. Do not conflate `official_account` and `ilink`; live production claims still need account/compliance and credential smoke for each mode. |
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
- Weixin has two separate modes. `official_account` is the WeChat public-account
  HTTP API and is not covered by the iLink SDK. `ilink` is the iLink bot
  protocol; Nekode uses `github.com/lib-x/ilink` for bot messaging while keeping
  direct QR/status calls only where the SDK has no nonblocking poll surface.

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
   - Weixin uses official-account callback/send plus the lib-x iLink bot SDK
     boundary for `mode=ilink`; QR/status remains a narrow compatibility
     adapter until the SDK exposes that server-side binding surface.
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
