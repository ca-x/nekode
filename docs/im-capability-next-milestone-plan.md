# IM Capability Next Milestone Plan

Status: next milestone planning
Related tasks: task #211, task #212, task #213, task #214

This plan turns the task #211 capability-differentiation evaluation into a
bounded P1 milestone. It is not part of the current release gate. The current
release continues to validate the implemented IM contract, provider availability
gating, local Terminal smoke, and the task #205 `Not-tested` live-provider
matrix.

## Goal

Nekode should use stronger IM platforms for stronger interactions while keeping
weak platforms predictable and honest. Telegram can support richer controls
such as inline buttons and command menus. Weixin official-account, ServerChan,
and Terminal may need to stay closer to plain text, numbered replies, and basic
delivery status.

The product goal is not to make every provider equal. The goal is:

- describe provider interaction capabilities explicitly;
- plan interaction output from capability facts instead of provider name checks;
- degrade weak channels to safe text flows without pretending unsupported UI is
  available;
- add Telegram rich interactions only after the shared model and fallback policy
  exist.

## Current Boundary

Current Nekode IM schema is mostly transport and setup oriented:

- webhook, polling, streaming, and media flags;
- binding methods such as QR code;
- availability, runtime, source, and notes;
- provider-specific configuration fields;
- local and live-smoke status.

That schema is enough for channel setup, binding entry points, and release
truthfulness. It is not a complete interaction capability model. It does not
yet describe inline buttons, quick replies, command menus, message edit/delete,
reactions, mentions, provider formatting modes, thread/topic support, media
depth, cards, polls, or structured forms.

`supportsStreaming` must be clarified in the next schema pass. Today it reads
like a transport/runtime characteristic. It must not be interpreted in Web copy
as native provider streaming UX unless the provider interaction model says so.

## Capability Model

Task #212 should introduce a scoped interaction capability matrix and provider
schema extension. Keep it separate from binding capabilities such as QR code.

Suggested shape:

```json
{
  "interactionCapabilities": {
    "text": { "scope": "all" },
    "formatting": { "mode": "markdown", "scope": "dm,group" },
    "quickReplies": { "scope": "dm,group", "maxItems": 8 },
    "inlineButtons": { "scope": "dm,group", "callback": true },
    "nativeCommands": { "scope": "all", "configuredBy": "provider" },
    "messageEdit": { "scope": "dm,group" },
    "reactions": { "scope": "dm,group" },
    "threads": { "scope": "group", "providerModel": "topic" },
    "mediaUpload": { "scope": "dm,group", "maxBytes": 10485760 },
    "mentions": { "scope": "group" }
  }
}
```

Use small, explicit capability records rather than one boolean per feature. The
schema should be stable enough for Web/API clients, while provider-specific
detail can remain in `metadata` fields when exact provider semantics differ.

## Initial Provider Matrix

| Provider | Expected interaction level | Planning notes |
| --- | --- | --- |
| Telegram | Rich | Candidate for inline buttons, callback queries, native commands, markdown formatting, media, mentions, and group-topic support. Implement after task #212 and task #213. |
| QQ | Medium | Likely supports structured group/C2C interactions and media, but actual BotGo capabilities must be confirmed with sandbox/live evidence before product UI claims. |
| Feishu | Medium to rich | Supports enterprise chat primitives, mentions, cards, and richer message APIs, but first Nekode runtime is thin plain-message callback/send. Treat rich Feishu as a later provider task. |
| Weixin official account | Basic | Keep to text/customer-service constraints, QR binding only where applicable, and explicit reply-window limits. Avoid rich buttons unless the official-account API path is proven. |
| Weixin iLink | Basic to medium | QR bind/send path exists but is not live-smoked. Rich interaction claims require Stella-compatible live environment evidence. |
| ServerChan | Basic | Treat as text notifications plus simple replies/polling. Use numbered fallback for actions. |
| Terminal | Basic/local-dev | Useful for deterministic local smoke; render text, choices, delivery status, and debug metadata. |

## Planner and Fallback

Task #213 should add a capability-driven interaction planner. The planner takes
a desired interaction and provider capability facts, then returns a provider
render plan or a safe fallback.

| Desired interaction | Rich channel render | Weak channel fallback |
| --- | --- | --- |
| Choose one action | Inline buttons with callback IDs | Numbered text list: `1. Approve`, `2. Reject`; user replies with number or keyword |
| Ask for missing config | Provider form/card if supported | Plain text prompt with required fields and examples |
| Acknowledge run status | Rich status card, edit existing message when supported | New plain text status line with run id and short status |
| Mention an agent in group | Native mention if supported | Plain text `@agent-name` plus explicit target label |
| Send task actions | Native commands or buttons | `/task claim <id>` and `/task status <id> <state>` text commands |

The fallback policy should be server-owned and testable. Provider adapters
should render an already-planned interaction instead of inventing their own
product behavior.

## Telegram Rich Slice

Task #214 should be the first rich-provider implementation after task #212 and
task #213 are complete.

Telegram scope should stay narrow:

- expose Telegram capability facts in schema;
- render selected action prompts as inline keyboards;
- process callback query payloads into authorized Nekode commands;
- keep fallback text alongside or near the rich controls when useful;
- verify callback dedupe, authorization, and delivery status;
- document which Telegram features are still out of scope.

Do not implement provider-rich behavior directly in Web or task business logic.
The Web/API layer should request an interaction intent; the planner chooses
Telegram rich output or a fallback based on endpoint capabilities.

## Milestone Acceptance

The P1 IM capability milestone is complete when:

- task #212 publishes an interaction capability matrix and schema extension;
- task #213 plans and tests rich-to-basic fallback for at least three common
  interaction intents;
- task #214 proves one Telegram rich interaction end to end in local/provider
  tests, with live smoke marked separately when real credentials are available;
- weak providers do not display unsupported rich controls in Web/API responses;
- documentation clearly distinguishes transport capabilities from interaction
  capabilities.

## Non-Goals

- Do not block the current release or task #199 on this milestone.
- Do not mark Telegram, Feishu, QQ, Weixin, or ServerChan production connected
  without task #205-style live-smoke evidence.
- Do not mix binding capabilities, such as QR code binding, into the message
  interaction capability model.
- Do not hard-code rich interactions by provider name in business logic.
