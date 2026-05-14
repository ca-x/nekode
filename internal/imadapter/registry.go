package imadapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProviderTelegram   = "telegram"
	ProviderQQ         = "qq"
	ProviderQQBot      = "qqbot"
	ProviderFeishu     = "feishu"
	ProviderWeixin     = "weixin"
	ProviderWeCom      = "wecom"
	ProviderSlack      = "slack"
	ProviderDiscord    = "discord"
	ProviderDingTalk   = "dingtalk"
	ProviderWeibo      = "weibo"
	ProviderLine       = "line"
	ProviderMax        = "max"
	ProviderTerminal   = "terminal"
	ProviderServerChan = "serverchan"
)

const (
	BindingMethodQRCode = "qr_code"
	SetupMethodManual   = "manual"
)

type FieldType string

const (
	FieldString  FieldType = "string"
	FieldBoolean FieldType = "boolean"
	FieldSelect  FieldType = "select"
	FieldJSON    FieldType = "json"
)

type Field struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Sensitive   bool      `json:"sensitive,omitempty"`
	Description string    `json:"description,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Options     []string  `json:"options,omitempty"`
}

type BindingMethod struct {
	Method      string `json:"method"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type SetupMethod struct {
	Method      string `json:"method"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Primary     bool   `json:"primary,omitempty"`
}

type ProviderSchema struct {
	Provider                string                   `json:"provider"`
	DisplayName             string                   `json:"displayName"`
	Transport               string                   `json:"transport"`
	Description             string                   `json:"description"`
	Canonical               bool                     `json:"canonical"`
	Availability            string                   `json:"availability"`
	RuntimeStatus           string                   `json:"runtimeStatus"`
	Source                  string                   `json:"source"`
	Notes                   []string                 `json:"notes,omitempty"`
	SupportsWebhook         bool                     `json:"supportsWebhook"`
	SupportsPolling         bool                     `json:"supportsPolling"`
	SupportsStreaming       bool                     `json:"supportsStreaming"`
	SupportsMedia           bool                     `json:"supportsMedia"`
	BindingTargets          []string                 `json:"bindingTargets"`
	BindingMethods          []BindingMethod          `json:"bindingMethods,omitempty"`
	SetupMethods            []SetupMethod            `json:"setupMethods,omitempty"`
	SetupHints              []string                 `json:"setupHints"`
	Fields                  []Field                  `json:"fields"`
	InteractionCapabilities *InteractionCapabilities `json:"interactionCapabilities,omitempty"`
}

var providerSchemas = []ProviderSchema{
	{
		Provider:          ProviderTelegram,
		DisplayName:       "Telegram",
		Transport:         "webhook",
		Description:       "Telegram Bot API channel. Nekode uses webhook updates with Telegram secret-token validation plus Bot API sendMessage delivery.",
		Canonical:         true,
		Availability:      "runtime",
		RuntimeStatus:     "implemented_not_live_smoked",
		Source:            "Nekode runtime aligned with Stella plugins/channels/telegram.",
		Notes:             []string{"Requires operator bot token and public webhook live smoke before production-ready claims."},
		SupportsWebhook:   true,
		SupportsPolling:   true,
		SupportsStreaming: true,
		SupportsMedia:     true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Create a Telegram bot with BotFather and paste the bot token.",
			"Configure Telegram setWebhook to POST to /api/im/telegram/<endpoint_id>/webhook and set the secret_token.",
			"Optional channel_id can route notifications to a default chat; otherwise replies use the source chat.",
		},
		Fields: []Field{
			{Name: "token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Description: "Telegram Bot API token."},
			{Name: "secret_token", Label: "Webhook secret token", Type: FieldString, Required: true, Sensitive: true, Description: "Expected X-Telegram-Bot-Api-Secret-Token header."},
			{Name: "bot_username", Label: "Bot username", Type: FieldString, Description: "Bot username without @, used for mention-mode group filtering."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound Telegram messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound Telegram messages."},
			{Name: "channel_id", Label: "Default chat/channel ID", Type: FieldString, Description: "Optional chat ID or @channel for notifications."},
			groupModeField(),
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: TelegramCapabilities(),
	},
	{
		Provider:          ProviderQQ,
		DisplayName:       "QQ",
		Transport:         "websocket",
		Description:       "QQ Bot channel using Tencent BotGo WebSocket receive and C2C/group send semantics adapted from Stella's QQ channel.",
		Canonical:         true,
		Availability:      "runtime",
		RuntimeStatus:     "implemented_not_live_smoked",
		Source:            "Fork-adapted from Stella plugins/channels/qq with Tencent BotGo.",
		Notes:             []string{"Requires QQ bot app credentials and sandbox/live WebSocket smoke."},
		SupportsStreaming: true,
		SupportsMedia:     true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Create a QQ bot application and configure its App ID/App Secret.",
			"Group routing uses mention mode by default.",
		},
		Fields: []Field{
			{Name: "app_id", Label: "App ID", Type: FieldString, Required: true, Description: "QQ bot app ID."},
			{Name: "app_secret", Label: "App secret", Type: FieldString, Required: true, Sensitive: true, Description: "QQ bot app secret."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound QQ messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound QQ messages."},
			{Name: "default_target_id", Label: "Default QQ target ID", Type: FieldString, Description: "Optional qq:group:<id> or qq:c2c:<id> target for outbound notifications."},
			groupModeField(),
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: QQCapabilities(),
	},
	{
		Provider:          ProviderFeishu,
		DisplayName:       "Feishu",
		Transport:         "webhook",
		Description:       "Feishu/Lark bot channel. Nekode uses plain event callbacks with verification-token challenge handling plus OpenAPI Message.Create delivery.",
		Canonical:         true,
		Availability:      "runtime",
		RuntimeStatus:     "implemented_not_live_smoked",
		Source:            "Nekode runtime aligned with Stella plugins/channels/feishu.",
		Notes:             []string{"Encrypted callbacks are rejected until the encrypt_key path is fully adapted and tested."},
		SupportsWebhook:   true,
		SupportsStreaming: true,
		SupportsMedia:     true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Create a Feishu app, enable bot events, and subscribe to im.message.receive_v1.",
			"Set the event request URL to /api/im/feishu/<endpoint_id>/callback and configure the verification token.",
			"Optional default_receive_id can route notifications to a chat_id/open_id/union_id; otherwise replies use the source chat.",
		},
		Fields: []Field{
			{Name: "app_id", Label: "App ID", Type: FieldString, Required: true, Description: "Feishu bot app ID."},
			{Name: "app_secret", Label: "App secret", Type: FieldString, Required: true, Sensitive: true, Description: "Feishu bot app secret."},
			{Name: "verification_token", Label: "Verification token", Type: FieldString, Required: true, Sensitive: true, Description: "Expected token in Feishu URL verification and callback payloads."},
			{Name: "encrypt_key", Label: "Encrypt key", Type: FieldString, Sensitive: true, Description: "Reserved Feishu event encrypt key; encrypted callbacks are not enabled in this thin runtime yet."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound Feishu messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound Feishu messages."},
			{Name: "default_receive_id", Label: "Default receive ID", Type: FieldString, Description: "Optional chat_id/open_id/union_id for outbound notifications."},
			{Name: "default_receive_id_type", Label: "Default receive ID type", Type: FieldSelect, Description: "Feishu receive_id_type for the default receive ID.", Options: []string{"chat_id", "open_id", "union_id", "user_id"}},
			groupModeField(),
			{Name: "tenant_key", Label: "Tenant key", Type: FieldString, Description: "Optional tenant key override."},
			{Name: "auto_provision", Label: "Auto provision members", Type: FieldBoolean, Description: "Create users for tenant members when supported."},
			{Name: "groups", Label: "Per-group overrides", Type: FieldJSON, Description: "Optional per-chat group mode, prompt, and tool policy overrides."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: FeishuCapabilities(),
	},
	{
		Provider:        ProviderWeixin,
		DisplayName:     "WeChat (iLink)",
		Transport:       "ilink",
		Description:     "Personal WeChat channel through the iLink bot gateway. Nekode keeps weixin scoped to the original iLink flow; enterprise WeChat is configured as WeCom.",
		Canonical:       true,
		Availability:    "runtime",
		RuntimeStatus:   "implemented_not_live_smoked",
		Source:          "iLink QR binding and bot messaging through github.com/lib-x/ilink, aligned with cc-connect's weixin setup flow.",
		Notes:           []string{"Provider id is weixin; wechat is accepted as a legacy alias.", "Use WeCom for enterprise WeChat / WeChat Work. Legacy official_account config is still accepted by the backend but is no longer the primary setup surface."},
		SupportsPolling: true,
		SupportsMedia:   true,
		BindingTargets:  defaultBindingTargets(),
		BindingMethods: []BindingMethod{
			{
				Method:      BindingMethodQRCode,
				DisplayName: "QR code",
				Description: "Start an iLink QR session that the operator scans in WeChat.",
			},
		},
		SetupMethods: []SetupMethod{
			{
				Method:      BindingMethodQRCode,
				DisplayName: "Scan QR",
				Description: "Create the endpoint and immediately start an iLink QR binding session.",
				Primary:     true,
			},
			{
				Method:      SetupMethodManual,
				DisplayName: "Existing token",
				Description: "Paste an existing iLink bot token when one has already been issued.",
			},
		},
		SetupHints: []string{
			"Use Scan QR for the normal personal WeChat iLink setup.",
			"Use Existing token only when migrating an already-issued iLink bot token.",
			"Enterprise WeChat / WeChat Work should be added as WeCom, not WeChat (iLink).",
		},
		Fields: []Field{
			{Name: "bot_token", Label: "iLink bot token", Type: FieldString, Sensitive: true, Description: "Filled after QR binding for lib-x iLink mode."},
			{Name: "bot_id", Label: "iLink bot ID", Type: FieldString, Description: "Filled after QR binding for iLink mode."},
			{Name: "user_id", Label: "iLink user ID", Type: FieldString, Description: "Filled after QR binding for iLink mode."},
			{Name: "base_url", Label: "iLink base URL", Type: FieldString, Description: "Optional iLink API base URL override."},
			{Name: "cdn_base_url", Label: "iLink CDN base URL", Type: FieldString, Description: "Optional iLink CDN base URL override."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound WeChat messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound WeChat messages."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: WeixinCapabilities(),
	},
	{
		Provider:          ProviderTerminal,
		DisplayName:       "Terminal",
		Transport:         "local",
		Description:       "Local terminal channel for development smoke tests and manual operator input.",
		Canonical:         true,
		Availability:      "runtime",
		RuntimeStatus:     "local_smoked",
		Source:            "Nekode local channel using the Stella channel boundary shape.",
		Notes:             []string{"Local development provider; external provider live smoke does not apply."},
		SupportsStreaming: true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Use Terminal as a local IM channel for development and integration smoke tests.",
			"No external provider credentials are required.",
		},
		Fields: []Field{
			{Name: "session_id", Label: "Session ID", Type: FieldString, Description: "Optional stable terminal session ID."},
			{Name: "operator_id", Label: "Operator ID", Type: FieldString, Description: "Optional stable local operator identity."},
			{Name: "operator", Label: "Operator name", Type: FieldString, Description: "Optional display name for local input."},
			{Name: "target", Label: "Default target", Type: FieldString, Description: "Optional Nekode target hint for terminal input."},
			{Name: "thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread hint for terminal input."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: TerminalCapabilities(),
	},
	{
		Provider:        ProviderServerChan,
		DisplayName:     "ServerChan",
		Transport:       "polling",
		Description:     "ServerChan Bot channel using bot-go polling receive and sendMessage delivery adapted from Nekobot's ServerChan runtime.",
		Canonical:       true,
		Availability:    "runtime",
		RuntimeStatus:   "implemented_not_live_smoked",
		Source:          "Fork-adapted from Nekobot pkg/channels/serverchan.",
		Notes:           []string{"Requires operator-owned ServerChan Bot token and live send/poll smoke before production-ready claims.", "ServerChan is treated as an IM provider with polling receive; it does not use the generic QR binding capability."},
		SupportsPolling: true,
		SupportsMedia:   false,
		BindingTargets:  defaultBindingTargets(),
		SetupHints: []string{
			"Create or reuse a ServerChan Bot token and paste bot_token.",
			"Optional default_chat_id can route notifications; replies use the source chat when present.",
			"Use allow_from to restrict accepted user/chat IDs when exposing polling receive.",
		},
		Fields: []Field{
			{Name: "bot_token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Description: "ServerChan Bot token."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound ServerChan messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound ServerChan messages."},
			{Name: "default_chat_id", Label: "Default chat ID", Type: FieldString, Description: "Optional ServerChan chat ID for outbound notifications."},
			{Name: "allow_from", Label: "Allowed IDs", Type: FieldJSON, Description: "Optional array of user/chat IDs allowed to send inbound messages; use [\"*\"] to allow all."},
			{Name: "api_base_url", Label: "API base URL", Type: FieldString, Description: "Optional ServerChan-compatible API base URL for tests or private gateways."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
		InteractionCapabilities: ServerChanCapabilities(),
	},
	{
		Provider:       ProviderSlack,
		DisplayName:    "Slack",
		Transport:      "socket_mode",
		Description:    "Slack bot using Socket Mode with bot and app-level tokens, mirroring cc-connect's Slack setup surface.",
		Canonical:      true,
		Availability:   "catalog",
		RuntimeStatus:  "reference_only",
		Source:         "Configuration fields and setup flow borrowed from cc-connect platform/slack and docs/slack.md.",
		Notes:          []string{"Nekode exposes Slack as a configurable endpoint schema; receive/send runtime is not wired yet."},
		SupportsMedia:  true,
		BindingTargets: defaultBindingTargets(),
		SetupHints: []string{
			"Create a Slack app, enable Socket Mode, and add bot/app-level tokens.",
			"Subscribe to app_mention events and invite the bot to channels where it should respond.",
		},
		Fields: []Field{
			{Name: "bot_token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Placeholder: "xoxb-...", Description: "Slack Bot User OAuth token."},
			{Name: "app_token", Label: "App token", Type: FieldString, Required: true, Sensitive: true, Placeholder: "xapp-...", Description: "Slack app-level Socket Mode token."},
			{Name: "allow_from", Label: "Allowed users/channels", Type: FieldString, Description: "Optional comma-separated Slack user/channel IDs; empty or * allows all."},
			{Name: "share_session_in_channel", Label: "Share session in channel", Type: FieldBoolean, Description: "Use one agent session per channel instead of per channel/user pair."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: SlackCapabilities(),
	},
	{
		Provider:       ProviderDiscord,
		DisplayName:    "Discord",
		Transport:      "gateway",
		Description:    "Discord bot over the Gateway API with optional thread isolation and card-style progress, based on cc-connect's Discord platform.",
		Canonical:      true,
		Availability:   "catalog",
		RuntimeStatus:  "reference_only",
		Source:         "Configuration fields and setup flow borrowed from cc-connect platform/discord and docs/discord.md.",
		Notes:          []string{"Nekode exposes Discord as a configurable endpoint schema; receive/send runtime is not wired yet."},
		SupportsMedia:  true,
		BindingTargets: defaultBindingTargets(),
		SetupHints: []string{
			"Create a Discord application and bot, enable Message Content Intent, and paste the bot token.",
			"Invite the bot to the target server with message, thread, and attachment permissions.",
		},
		Fields: []Field{
			{Name: "token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Description: "Discord bot token."},
			{Name: "allow_from", Label: "Allowed users/channels", Type: FieldString, Description: "Optional comma-separated Discord IDs; empty or * allows all."},
			{Name: "guild_id", Label: "Guild ID", Type: FieldString, Description: "Optional server ID for faster command registration."},
			{Name: "group_reply_all", Label: "Reply to all channel messages", Type: FieldBoolean, Description: "Respond without requiring an explicit mention."},
			{Name: "share_session_in_channel", Label: "Share session in channel", Type: FieldBoolean, Description: "Use one agent session per channel."},
			{Name: "thread_isolation", Label: "Thread isolation", Type: FieldBoolean, Description: "Create or reuse a Discord thread for each agent session."},
			{Name: "respond_to_at_everyone_and_here", Label: "Respond to @everyone/@here", Type: FieldBoolean, Description: "Allow broad mentions to trigger the bot."},
			{Name: "progress_style", Label: "Progress style", Type: FieldSelect, Options: []string{"legacy", "compact", "card"}, Description: "How intermediate progress is displayed."},
			{Name: "proxy", Label: "Proxy URL", Type: FieldString, Description: "Optional HTTP proxy for Discord API traffic."},
			{Name: "proxy_username", Label: "Proxy username", Type: FieldString, Description: "Optional proxy username."},
			{Name: "proxy_password", Label: "Proxy password", Type: FieldString, Sensitive: true, Description: "Optional proxy password."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: DiscordCapabilities(),
	},
	{
		Provider:       ProviderDingTalk,
		DisplayName:    "DingTalk",
		Transport:      "stream",
		Description:    "DingTalk stream robot channel using client_id/client_secret and optional AI card settings.",
		Canonical:      true,
		Availability:   "catalog",
		RuntimeStatus:  "reference_only",
		Source:         "Configuration fields and setup flow borrowed from cc-connect platform/dingtalk and docs/dingtalk.md.",
		Notes:          []string{"Nekode exposes DingTalk as a configurable endpoint schema; receive/send runtime is not wired yet."},
		SupportsMedia:  true,
		BindingTargets: defaultBindingTargets(),
		SetupHints: []string{
			"Create a DingTalk stream robot and paste client_id/client_secret.",
			"robot_code defaults to client_id when omitted; agent_id/card settings are optional for richer notifications.",
		},
		Fields: []Field{
			{Name: "client_id", Label: "Client ID", Type: FieldString, Required: true, Description: "DingTalk robot client_id."},
			{Name: "client_secret", Label: "Client secret", Type: FieldString, Required: true, Sensitive: true, Description: "DingTalk robot client_secret."},
			{Name: "robot_code", Label: "Robot code", Type: FieldString, Description: "Optional robot code; falls back to client_id in cc-connect."},
			{Name: "agent_id", Label: "Agent ID", Type: FieldString, Description: "Optional work-notification agent ID."},
			{Name: "allow_from", Label: "Allowed users", Type: FieldString, Description: "Optional comma-separated DingTalk user IDs; empty or * allows all."},
			{Name: "share_session_in_channel", Label: "Share session in group", Type: FieldBoolean, Description: "Use one agent session per conversation."},
			{Name: "card_template_id", Label: "AI card template ID", Type: FieldString, Description: "Optional DingTalk AI card template ID."},
			{Name: "card_template_key", Label: "AI card template key", Type: FieldString, Description: "Template content key; defaults to content in cc-connect."},
			{Name: "card_throttle_ms", Label: "Card throttle ms", Type: FieldString, Description: "Optional card update throttle in milliseconds."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: DingTalkCapabilities(),
	},
	{
		Provider:          ProviderWeCom,
		DisplayName:       "WeChat Work",
		Transport:         "websocket",
		Description:       "WeChat Work channel covering cc-connect's smart bot WebSocket mode and app callback mode settings.",
		Canonical:         true,
		Availability:      "runtime",
		RuntimeStatus:     "callback_app_implemented",
		Source:            "Configuration fields and setup flow borrowed from cc-connect platform/wecom and docs/wecom.md.",
		Notes:             []string{"Callback app mode is wired for encrypted callback receive and app message send.", "Smart-bot WebSocket credentials are still reference-only until the long-connection runtime is adapted."},
		SupportsWebhook:   true,
		SupportsStreaming: true,
		SupportsMedia:     true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"For smart bot mode, fill bot_id and bot_secret.",
			"For app callback mode, fill corp_id, corp_secret, agent_id, callback_token, and callback_aes_key.",
		},
		Fields: []Field{
			{Name: "mode", Label: "Mode", Type: FieldSelect, Options: []string{"callback_app", "websocket_bot"}, Description: "Select app callback runtime or cc-connect's smart bot WebSocket style."},
			{Name: "bot_id", Label: "Bot ID", Type: FieldString, Description: "WeChat Work smart bot ID."},
			{Name: "bot_secret", Label: "Bot secret", Type: FieldString, Sensitive: true, Description: "WeChat Work smart bot secret."},
			{Name: "corp_id", Label: "Corp ID", Type: FieldString, Description: "Enterprise corp_id for app callback mode."},
			{Name: "corp_secret", Label: "Corp secret", Type: FieldString, Sensitive: true, Description: "Enterprise app secret."},
			{Name: "agent_id", Label: "Agent ID", Type: FieldString, Description: "Enterprise app agent ID."},
			{Name: "callback_token", Label: "Callback token", Type: FieldString, Sensitive: true, Description: "Token for callback signature verification."},
			{Name: "callback_aes_key", Label: "Callback AES key", Type: FieldString, Sensitive: true, Description: "EncodingAESKey for callback encryption."},
			{Name: "callback_path", Label: "Callback path", Type: FieldString, Placeholder: "/wecom/callback", Description: "Webhook callback path."},
			{Name: "api_base_url", Label: "API base URL", Type: FieldString, Placeholder: "https://qyapi.weixin.qq.com", Description: "Optional WeChat Work API base URL."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound WeChat Work messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound WeChat Work messages."},
			{Name: "default_user_id", Label: "Default user ID", Type: FieldString, Description: "Optional WeChat Work user ID for outbound notifications without a source message."},
			{Name: "allow_from", Label: "Allowed users", Type: FieldString, Description: "Optional comma-separated user IDs; empty or * allows all."},
			{Name: "enable_markdown", Label: "Enable markdown", Type: FieldBoolean, Description: "Send markdown app messages instead of plain text."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: WeComCapabilities(),
	},
	{
		Provider:          ProviderWeibo,
		DisplayName:       "Weibo Open IM",
		Transport:         "websocket",
		Description:       "Weibo Open IM channel using app_id/app_secret token exchange, based on cc-connect's Weibo runtime.",
		Canonical:         true,
		Availability:      "catalog",
		RuntimeStatus:     "reference_only",
		Source:            "Configuration fields and setup flow borrowed from cc-connect platform/weibo and docs/weibo.md.",
		Notes:             []string{"Nekode exposes Weibo as a configurable endpoint schema; receive/send runtime is not wired yet."},
		SupportsStreaming: true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Create a Weibo Open IM app and paste app_id/app_secret.",
			"Use token_endpoint only for compatible private or test gateways.",
		},
		Fields: []Field{
			{Name: "app_id", Label: "App ID", Type: FieldString, Required: true, Description: "Weibo Open IM app_id."},
			{Name: "app_secret", Label: "App secret", Type: FieldString, Required: true, Sensitive: true, Description: "Weibo Open IM app_secret."},
			{Name: "token_endpoint", Label: "Token endpoint", Type: FieldString, Description: "Optional token endpoint override."},
			{Name: "allow_from", Label: "Allowed users", Type: FieldString, Description: "Optional comma-separated Weibo user IDs; empty or * allows all."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: WeiboCapabilities(),
	},
	{
		Provider:          ProviderQQBot,
		DisplayName:       "QQ Bot Official",
		Transport:         "websocket",
		Description:       "Official QQ Bot API v2 gateway channel, distinct from Nekode's current QQ BotGo schema.",
		Canonical:         false,
		Availability:      "catalog",
		RuntimeStatus:     "reference_only",
		Source:            "Configuration fields and setup flow borrowed from cc-connect platform/qqbot and docs/qqbot.md.",
		Notes:             []string{"Use provider qq for Nekode's current QQ BotGo runtime. qqbot is catalog-only until the official API v2 runtime is wired."},
		SupportsMedia:     true,
		SupportsStreaming: true,
		BindingTargets:    defaultBindingTargets(),
		SetupHints: []string{
			"Create an official QQ bot application and paste AppID/AppSecret.",
			"Enable sandbox only when the bot has not been published.",
		},
		Fields: []Field{
			{Name: "app_id", Label: "App ID", Type: FieldString, Required: true, Description: "Official QQ Bot AppID."},
			{Name: "app_secret", Label: "App secret", Type: FieldString, Required: true, Sensitive: true, Description: "Official QQ Bot AppSecret."},
			{Name: "sandbox", Label: "Sandbox mode", Type: FieldBoolean, Description: "Use the QQ Bot sandbox gateway/API base."},
			{Name: "allow_from", Label: "Allowed users/groups", Type: FieldString, Description: "Optional comma-separated IDs; empty or * allows all."},
			{Name: "share_session_in_channel", Label: "Share session in group", Type: FieldBoolean, Description: "Use one agent session per group."},
			{Name: "markdown_support", Label: "Markdown support", Type: FieldBoolean, Description: "Enable markdown message payloads where supported."},
			{Name: "intents", Label: "Gateway intents", Type: FieldString, Description: "Optional integer gateway intents override."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: QQBotCapabilities(),
	},
	{
		Provider:        ProviderLine,
		DisplayName:     "LINE",
		Transport:       "webhook",
		Description:     "LINE Messaging API channel using channel secret/token and local callback server settings.",
		Canonical:       true,
		Availability:    "catalog",
		RuntimeStatus:   "reference_only",
		Source:          "Configuration fields and setup flow borrowed from cc-connect platform/line.",
		Notes:           []string{"Nekode exposes LINE as a configurable endpoint schema; receive/send runtime is not wired yet."},
		SupportsWebhook: true,
		SupportsMedia:   true,
		BindingTargets:  defaultBindingTargets(),
		SetupHints: []string{
			"Create a LINE Messaging API channel and paste channel_secret/channel_token.",
			"Expose the callback path publicly and configure it in the LINE Developers console.",
		},
		Fields: []Field{
			{Name: "channel_secret", Label: "Channel secret", Type: FieldString, Required: true, Sensitive: true, Description: "LINE channel secret."},
			{Name: "channel_token", Label: "Channel token", Type: FieldString, Required: true, Sensitive: true, Description: "LINE channel access token."},
			{Name: "port", Label: "Callback port", Type: FieldString, Placeholder: "8080", Description: "Local callback server port used by cc-connect."},
			{Name: "callback_path", Label: "Callback path", Type: FieldString, Placeholder: "/callback", Description: "Webhook callback path."},
			{Name: "allow_from", Label: "Allowed users/groups", Type: FieldString, Description: "Optional comma-separated LINE IDs; empty or * allows all."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: LineCapabilities(),
	},
	{
		Provider:        ProviderMax,
		DisplayName:     "MAX",
		Transport:       "polling_or_webhook",
		Description:     "MAX bot channel supporting long-poll and optional webhook settings from cc-connect.",
		Canonical:       true,
		Availability:    "catalog",
		RuntimeStatus:   "reference_only",
		Source:          "Configuration fields and setup flow borrowed from cc-connect platform/max and docs/max-webhook.md.",
		Notes:           []string{"Nekode exposes MAX as a configurable endpoint schema; receive/send runtime is not wired yet.", "cc-connect notes that long-polling is best for personal/low-traffic bots after MAX's 2026-05-11 throttling change."},
		SupportsWebhook: true,
		SupportsPolling: true,
		SupportsMedia:   true,
		BindingTargets:  defaultBindingTargets(),
		SetupHints: []string{
			"Paste the MAX bot token. Omit webhook_url to use long-polling in cc-connect.",
			"If webhook_url is set, also configure webhook_listen/path/secret and a public TLS reverse proxy.",
		},
		Fields: []Field{
			{Name: "token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Description: "MAX bot token."},
			{Name: "allow_from", Label: "Allowed users", Type: FieldString, Placeholder: "*", Description: "Optional comma-separated MAX user IDs; empty or * allows all."},
			{Name: "api_base", Label: "API base URL", Type: FieldString, Description: "Optional MAX-compatible API base URL."},
			{Name: "webhook_url", Label: "Webhook URL", Type: FieldString, Description: "Public HTTPS URL that receives MAX updates."},
			{Name: "webhook_listen", Label: "Webhook listen address", Type: FieldString, Placeholder: "127.0.0.1:8090", Description: "Local address to bind for webhook mode."},
			{Name: "webhook_path", Label: "Webhook path", Type: FieldString, Placeholder: "/webhook", Description: "Webhook path; must match webhook_url."},
			{Name: "webhook_secret", Label: "Webhook secret", Type: FieldString, Sensitive: true, Description: "Shared webhook secret checked on incoming requests."},
			{Name: "webhook_resubscribe_interval", Label: "Webhook resubscribe interval", Type: FieldString, Placeholder: "5m", Description: "How often to refresh the MAX webhook subscription."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint once runtime support exists."},
		},
		InteractionCapabilities: MaxCapabilities(),
	},
}

func SupportsBindingMethod(provider string, method string) bool {
	schema, ok := GetProvider(provider)
	if !ok {
		return false
	}
	method = strings.ToLower(strings.TrimSpace(method))
	for _, bindingMethod := range schema.BindingMethods {
		if bindingMethod.Method == method {
			return true
		}
	}
	return false
}

func ListProviders() []ProviderSchema {
	out := make([]ProviderSchema, len(providerSchemas))
	for i, schema := range providerSchemas {
		out[i] = withDefaultSetupMethods(schema)
	}
	return out
}

func GetProvider(provider string) (ProviderSchema, bool) {
	provider = CanonicalProvider(provider)
	for _, schema := range providerSchemas {
		if schema.Provider == provider {
			return withDefaultSetupMethods(schema), true
		}
	}
	return ProviderSchema{}, false
}

func withDefaultSetupMethods(schema ProviderSchema) ProviderSchema {
	if len(schema.SetupMethods) == 0 {
		schema.SetupMethods = []SetupMethod{
			{
				Method:      SetupMethodManual,
				DisplayName: "Manual input",
				Description: "Enter provider credentials directly.",
				Primary:     true,
			},
		}
	}
	if !schemaHasField(schema, "require_subscription") {
		schema.Fields = append(schema.Fields, requireSubscriptionField())
	}
	return schema
}

func schemaHasField(schema ProviderSchema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func ValidateConfig(provider string, rawConfig string) error {
	schema, ok := GetProvider(provider)
	if !ok {
		return fmt.Errorf("unsupported IM provider %q", provider)
	}
	config, err := decodeConfig(rawConfig)
	if err != nil {
		return err
	}
	if schema.Provider == ProviderWeixin {
		return validateWeixinConfig(config)
	}
	if schema.Provider == ProviderWeCom {
		return validateWeComConfig(config)
	}
	for _, field := range schema.Fields {
		if !field.Required {
			continue
		}
		value, ok := config[field.Name]
		if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return fmt.Errorf("%s: missing required config field %s", schema.Provider, field.Name)
		}
	}
	return nil
}

func validateWeComConfig(config map[string]any) error {
	mode := canonicalWeComMode(configString(config, "mode"))
	if mode == "" {
		mode = "callback_app"
	}
	switch mode {
	case "websocket_bot":
		for _, field := range []string{"bot_id", "bot_secret"} {
			if configString(config, field) == "" {
				return fmt.Errorf("%s: missing required config field %s", ProviderWeCom, field)
			}
		}
	case "callback_app":
		for _, field := range []string{"corp_id", "corp_secret", "agent_id", "callback_token", "callback_aes_key"} {
			if configString(config, field) == "" {
				return fmt.Errorf("%s: missing required config field %s", ProviderWeCom, field)
			}
		}
	default:
		return fmt.Errorf("%s: unsupported mode %q", ProviderWeCom, mode)
	}
	return nil
}

func canonicalWeComMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "websocket":
		return "websocket_bot"
	case "callback", "webhook", "callback_app":
		return "callback_app"
	default:
		return mode
	}
}

func validateWeixinConfig(config map[string]any) error {
	mode := configString(config, "mode")
	if mode == "" {
		mode = "ilink"
	}
	switch mode {
	case "ilink":
		return nil
	case "official_account":
		for _, field := range []string{"app_id", "app_secret", "token"} {
			if configString(config, field) == "" {
				return fmt.Errorf("%s: missing required config field %s", ProviderWeixin, field)
			}
		}
	default:
		return fmt.Errorf("%s: unsupported mode %q", ProviderWeixin, mode)
	}
	return nil
}

func configString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func RedactConfig(provider string, rawConfig string) (string, error) {
	schema, ok := GetProvider(provider)
	if !ok {
		return normalizedObjectJSON(rawConfig)
	}
	config, err := decodeConfig(rawConfig)
	if err != nil {
		return "", err
	}
	sensitive := make(map[string]struct{})
	for _, field := range schema.Fields {
		if field.Sensitive {
			sensitive[field.Name] = struct{}{}
		}
	}
	for key, value := range config {
		if _, ok := sensitive[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			config[key] = "***"
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func CanonicalProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "wechat":
		return ProviderWeixin
	default:
		return provider
	}
}

func normalizeProvider(provider string) string {
	return CanonicalProvider(provider)
}

func groupModeField() Field {
	return Field{
		Name:        "group_mode",
		Label:       "Group mode",
		Type:        FieldSelect,
		Description: "How group chats trigger the agent.",
		Options:     []string{"mention", "always", "disabled"},
	}
}

func requireSubscriptionField() Field {
	return Field{
		Name:        "require_subscription",
		Label:       "Require chat subscription",
		Type:        FieldBoolean,
		Description: "Require /subscribe and operator approval before inbound messages from a chat are accepted.",
	}
}

func defaultBindingTargets() []string {
	return []string{"channel", "thread", "agent", "default_target"}
}

func decodeConfig(rawConfig string) (map[string]any, error) {
	rawConfig = strings.TrimSpace(rawConfig)
	if rawConfig == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, nil
}

func normalizedObjectJSON(rawConfig string) (string, error) {
	config, err := decodeConfig(rawConfig)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
