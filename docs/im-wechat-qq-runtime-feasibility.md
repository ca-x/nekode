# WeChat and QQ Runtime Feasibility Plan

Status: task #181 feasibility and compliant runtime plan
Baseline: `origin/main@4fff76c`
Reference baseline: CherryHQ/stella at `120eced`

## Conclusion

QQ is feasible as a real provider runtime if Nekode uses Tencent's official
QQ Bot / BotGo path and the deployment has a QQ bot application, sandbox or
production permissions, callback configuration, and the required IP allowlist.
The implementation should prefer the webhook callback path when the bot account
has access to it. Legacy WebSocket receive can be supported only as a temporary
compatibility mode because BotGo's own README says WebSocket event delivery is
being phased out while webhook callbacks are in gray rollout.

WeChat is not one generic channel. Nekode should not ship Stella's `weixin`
iLink implementation as a generally available official WeChat runtime without
operator-provided proof that the iLink API is available and contractually usable
for the deployment. The compliant default should be an Official Account /
customer-service style runtime, which can receive messages through the Official
Account server callback and send customer-service replies inside WeChat's
allowed reply window. That path is for official-account conversations, not
arbitrary personal WeChat or group chat automation.

## Source Checks

- Stella QQ uses `github.com/tencent-connect/botgo`, starts a QQ bot token
  source, creates an OpenAPI client, registers C2C and group @ handlers, starts
  a WebSocket session, and sends through `PostGroupMessage` or
  `PostC2CMessage`.
- BotGo is Tencent's official Go SDK for QQ bots. Its README states that
  WebSocket event push will be phased down and that the new webhook callback
  path is in gray rollout; it also documents bot app credentials, sandbox
  configuration, callback URL setup, and IP allowlist requirements:
  <https://github.com/tencent-connect/botgo>.
- BotGo's `interaction/webhook` package provides `HTTPHandler`, signature
  validation, heartbeat reply, callback verification reply, and dispatch through
  handlers registered with `event.RegisterHandlers`:
  <https://pkg.go.dev/github.com/tencent-connect/botgo/interaction/webhook>.
- Stella Weixin uses a custom iLink REST client, not a public Go SDK. It calls
  `/ilink/bot/getupdates`, `/ilink/bot/sendmessage`, QR-code endpoints, and CDN
  helpers with `AuthorizationType: ilink_bot_token`; it stores update cursor,
  user context tokens, and typing tickets in memory.
- WeChat Official Account server integration is a separate official path:
  configure a server URL/token, verify message signatures, receive XML events
  and messages, and reply through passive responses or customer-service message
  APIs subject to platform windows and permissions. Relevant official docs are
  under <https://developers.weixin.qq.com/doc/offiaccount/>.

## QQ Runtime Plan

### Supported Scope

- Provider name: keep `qq`.
- Runtime mode: `webhook` first; `websocket` may exist only behind an explicit
  compatibility flag for accounts that are not yet in the webhook gray rollout.
- Conversations:
  - C2C: `qq:c2c:<openid-or-user-id>`.
  - Group: `qq:group:<group-id>`.
- Inbound events:
  - C2C message create.
  - Group @ message create.
- Outbound sends:
  - `PostC2CMessage` for C2C.
  - `PostGroupMessage` for groups.
- Media: start with text-only plus provider attachment metadata. File/image
  download should be a second slice because QQ attachment URLs and provider
  permissions need separate failure handling.

### Required Endpoint Config

```json
{
  "provider": "qq",
  "mode": "webhook",
  "app_id": "qq-bot-app-id",
  "app_secret_ref": "secret:im/qq/prod/app_secret",
  "webhook_path": "/api/im/qq/events/<endpoint_id>",
  "callback_secret_ref": "secret:im/qq/prod/callback_secret",
  "group_mode": "mention",
  "allowed_conversations": ["qq:group:..."]
}
```

`app_secret`, callback secret, and access tokens must not be returned by API
responses or written to logs. If BotGo's webhook handler owns signature
validation, Nekode still needs tests proving invalid callback signatures do not
reach `MessageCoordinator`.

### Implementation Boundary

1. Add a QQ runtime package that loads a single `InteractionEndpoint` and
   resolves its secrets.
2. Register the BotGo event handlers for C2C and group @ messages.
3. Expose an authenticated-by-provider HTTP callback route. The route must use
   BotGo webhook validation before creating `iminbound.RawEvent`.
4. Convert BotGo event payloads to the existing `QQRawEvent` / normalizer input
   shape, preserving provider message ID, sender ID, group ID, and raw metadata.
5. Pass inbound messages through `imadapter.Normalizer` and
   `imcoord.Coordinator`. Do not call daemon/agent runtime code directly.
6. Add an outbound worker that lists pending `OutboundDelivery` for QQ
   endpoints, renders frames with `RenderQQOutbound`, calls the BotGo send API,
   and records delivered/failed/retrying status.
7. Expose runtime health without secrets: webhook configured, last callback,
   last send, token refresh status, last error.

### Verification Gate

- Unit: config validation/redaction; callback route rejects bad signature;
  C2C/group event normalization; duplicate provider message IDs dedupe.
- Integration: fake BotGo server/client or injectable OpenAPI interface proves
  `OutboundDelivery` status transitions on send success/failure.
- Live smoke: sandbox bot receives a C2C and group @ message, writes Nekode
  message with IM metadata, sends a reply through QQ, marks delivery delivered,
  and records no secrets in logs.
- Release note: if only WebSocket mode is tested, describe it as temporary
  compatibility, not the preferred production path.

## WeChat Runtime Plan

### Path A: Official Account / Customer-Service Runtime

This is the compliant default candidate.

Supported scope:

- Provider name should be explicit, for example `wechat_official_account`, or
  `weixin` with `mode=official_account` so it is not confused with Stella's
  iLink path.
- Inbound receive: Official Account server URL callback with token/signature
  verification and XML message parsing.
- Outbound send: customer-service message API or passive reply, subject to
  WeChat's interaction windows and account permissions.
- Conversation model: user-to-official-account conversations keyed by OpenID.
  It does not support arbitrary personal WeChat chats or general WeChat groups.

Required endpoint config:

```json
{
  "provider": "weixin",
  "mode": "official_account",
  "app_id": "wx...",
  "app_secret_ref": "secret:im/weixin/prod/app_secret",
  "token_ref": "secret:im/weixin/prod/token",
  "encoding_aes_key_ref": "secret:im/weixin/prod/aes_key",
  "webhook_path": "/api/im/weixin/events/<endpoint_id>",
  "reply_mode": "customer_service"
}
```

Implementation boundary:

1. Verify callback signatures before parsing or normalizing messages.
2. Decrypt encrypted messages when `EncodingAESKey` is configured.
3. Normalize text/image/file events into Nekode's `iminbound.RawEvent` and
   existing Weixin normalizer surface.
4. Store provider OpenID as external sender/conversation identity, not Nekode
   user ID.
5. Outbound worker sends only when the provider allows it. When the reply window
   is closed, mark delivery `failed` with a clear non-retryable error rather
   than retrying indefinitely.
6. Runtime health should show callback status, token refresh status, last
   inbound, last outbound, and last provider error.

Verification gate:

- Unit: signature verification, encrypted/plain XML parsing, invalid signature
  rejection, duplicate message dedupe, reply-window failure mapping.
- Integration: fake Official Account API verifies customer-service send and
  access-token refresh behavior.
- Live smoke: operator-owned Official Account test account receives a user
  message, Nekode stores it, sends a customer-service reply inside the allowed
  window, and records delivery status.

### Path B: Stella iLink Runtime

This is not the default compliant path.

Nekode may consider iLink only if the operator provides explicit evidence that
its deployment has legitimate iLink bot access and permission to use the
endpoints Stella calls. If accepted, it should be labeled `mode=ilink` or a
separate provider variant so UI and release notes do not imply generic official
WeChat support.

Additional requirements beyond Stella:

- Persist cursor and per-user `context_token` securely enough to survive server
  restart; Stella stores them in memory, which makes outbound unreliable after
  restart.
- Encrypt `bot_token`; never expose it through API/UI/logs.
- Persist QR login result and account identity with audit trail.
- Treat missing `context_token` as non-retryable until the user messages again.
- Record that v1 is DM-only unless group support is verified with live evidence.
- Add live smoke that proves QR login, `getupdates`, message normalization,
  sendmessage, restart recovery, and expired-session behavior.

### Rejected WeChat Scope

- Do not automate personal WeChat accounts through unofficial client protocols.
- Do not claim WeChat group support from Stella's iLink DM-only behavior.
- Do not use iLink as the default path without legal/account/API confirmation.
- Do not silently retry customer-service sends after the official reply window
  closes; that hides a product limit as an infrastructure error.

## Follow-Up Task Split

Recommended next tasks after this feasibility plan:

1. QQ webhook runtime implementation.
   - Owner can implement BotGo webhook receive + text outbound send.
   - Depends on QQ bot app credentials and sandbox/callback access.
2. WeChat Official Account runtime proof.
   - Owner should build the official-account callback/customer-service path
     with a fake API integration test, then run live smoke with a test account.
3. Optional Weixin iLink validation.
   - Only create this as implementation work after a human confirms legitimate
     iLink access and supplies a test environment.

## Release Language

After task #181, product/release copy may say:

- QQ official BotGo runtime is feasible, implementation pending live bot access.
- WeChat official-account/customer-service runtime is feasible for official
  account conversations, not personal chats/groups.
- Stella iLink remains a reference/private-path candidate pending account and
  compliance confirmation.

It must not say QQ or WeChat is live until the relevant runtime task passes its
receive/auth/send/live-smoke gate.
