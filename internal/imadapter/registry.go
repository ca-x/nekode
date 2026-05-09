package imadapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProviderTelegram   = "telegram"
	ProviderQQ         = "qq"
	ProviderFeishu     = "feishu"
	ProviderWeixin     = "weixin"
	ProviderTerminal   = "terminal"
	ProviderServerChan = "serverchan"
)

const (
	BindingMethodQRCode = "qr_code"
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

type ProviderSchema struct {
	Provider                 string                   `json:"provider"`
	DisplayName              string                   `json:"displayName"`
	Transport                string                   `json:"transport"`
	Description              string                   `json:"description"`
	Canonical                bool                     `json:"canonical"`
	Availability             string                   `json:"availability"`
	RuntimeStatus            string                   `json:"runtimeStatus"`
	Source                   string                   `json:"source"`
	Notes                    []string                 `json:"notes,omitempty"`
	SupportsWebhook          bool                     `json:"supportsWebhook"`
	SupportsPolling          bool                     `json:"supportsPolling"`
	SupportsStreaming        bool                     `json:"supportsStreaming"`
	SupportsMedia            bool                     `json:"supportsMedia"`
	BindingTargets           []string                 `json:"bindingTargets"`
	BindingMethods           []BindingMethod          `json:"bindingMethods,omitempty"`
	SetupHints               []string                 `json:"setupHints"`
	Fields                   []Field                  `json:"fields"`
	InteractionCapabilities  *InteractionCapabilities `json:"interactionCapabilities,omitempty"`
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
		DisplayName:     "WeChat Official Account",
		Transport:       "webhook",
		Description:     "Official WeChat public account channel using callback signature verification and customer-service message sending.",
		Canonical:       true,
		Availability:    "runtime",
		RuntimeStatus:   "implemented_not_live_smoked",
		Source:          "Official-account runtime plus github.com/lib-x/ilink SDK boundary for iLink bot mode.",
		Notes:           []string{"Provider id is weixin; wechat is accepted as a legacy alias.", "Official-account mode uses WeChat public-account HTTP callbacks/send; iLink mode uses lib-x/ilink for bot messaging and a narrow QR/status compatibility adapter."},
		SupportsWebhook: true,
		SupportsMedia:   true,
		BindingTargets:  defaultBindingTargets(),
		BindingMethods: []BindingMethod{
			{
				Method:      BindingMethodQRCode,
				DisplayName: "QR code",
				Description: "Create a channel binding session that the operator scans in WeChat. The provider adapter supplies the QR ticket and scan status.",
			},
		},
		SetupHints: []string{
			"Use an official WeChat public account test or production account; this is not a generic personal WeChat runtime.",
			"Configure the public account server URL to /api/im/weixin/<endpoint_id>/callback with the same token.",
			"Bind users from the channel management panel by starting a QR code binding session.",
			"Customer-service sends require app_id/app_secret access_token permissions and WeChat's allowed reply window.",
		},
		Fields: []Field{
			{Name: "mode", Label: "Mode", Type: FieldSelect, Required: true, Options: []string{"ilink", "official_account"}, Description: "iLink uses the lib-x iLink bot SDK boundary; official_account uses public-account callbacks."},
			{Name: "bot_token", Label: "iLink bot token", Type: FieldString, Sensitive: true, Description: "Filled after QR binding for lib-x iLink mode."},
			{Name: "bot_id", Label: "iLink bot ID", Type: FieldString, Description: "Filled after QR binding for iLink mode."},
			{Name: "user_id", Label: "iLink user ID", Type: FieldString, Description: "Filled after QR binding for iLink mode."},
			{Name: "base_url", Label: "iLink base URL", Type: FieldString, Description: "Optional iLink API base URL override."},
			{Name: "app_id", Label: "App ID", Type: FieldString, Description: "WeChat public account app_id."},
			{Name: "app_secret", Label: "App secret", Type: FieldString, Sensitive: true, Description: "WeChat public account app_secret for access_token."},
			{Name: "token", Label: "Callback token", Type: FieldString, Sensitive: true, Description: "Token used for WeChat callback SHA1 signature verification."},
			{Name: "default_target", Label: "Default target", Type: FieldString, Description: "Default Nekode target for inbound WeChat messages."},
			{Name: "default_thread_id", Label: "Default thread ID", Type: FieldString, Description: "Optional Nekode thread for inbound WeChat messages."},
			{Name: "api_base_url", Label: "API base URL", Type: FieldString, Description: "Optional API base URL for local tests or compatible gateways."},
			{Name: "access_token", Label: "Access token override", Type: FieldString, Sensitive: true, Description: "Optional test-only access_token override; production should use app_id/app_secret refresh."},
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
	copy(out, providerSchemas)
	return out
}

func GetProvider(provider string) (ProviderSchema, bool) {
	provider = CanonicalProvider(provider)
	for _, schema := range providerSchemas {
		if schema.Provider == provider {
			return schema, true
		}
	}
	return ProviderSchema{}, false
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
