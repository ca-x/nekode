package imadapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProviderTelegram = "telegram"
	ProviderQQ       = "qq"
	ProviderFeishu   = "feishu"
	ProviderWeixin   = "weixin"
	ProviderTerminal = "terminal"
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

type ProviderSchema struct {
	Provider          string   `json:"provider"`
	DisplayName       string   `json:"displayName"`
	Transport         string   `json:"transport"`
	Description       string   `json:"description"`
	SupportsWebhook   bool     `json:"supportsWebhook"`
	SupportsPolling   bool     `json:"supportsPolling"`
	SupportsStreaming bool     `json:"supportsStreaming"`
	SupportsMedia     bool     `json:"supportsMedia"`
	BindingTargets    []string `json:"bindingTargets"`
	SetupHints        []string `json:"setupHints"`
	Fields            []Field  `json:"fields"`
}

var providerSchemas = []ProviderSchema{
	{
		Provider:          ProviderTelegram,
		DisplayName:       "Telegram",
		Transport:         "webhook",
		Description:       "Telegram Bot API channel. Nekode uses webhook updates with Telegram secret-token validation plus Bot API sendMessage delivery.",
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
	},
	{
		Provider:          ProviderQQ,
		DisplayName:       "QQ",
		Transport:         "websocket",
		Description:       "QQ Bot channel based on Stella's BotGo adapter shape.",
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
			groupModeField(),
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
	},
	{
		Provider:          ProviderFeishu,
		DisplayName:       "Feishu",
		Transport:         "webhook",
		Description:       "Feishu/Lark bot channel. Nekode uses plain event callbacks with verification-token challenge handling plus OpenAPI Message.Create delivery.",
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
	},
	{
		Provider:        ProviderWeixin,
		DisplayName:     "WeChat",
		Transport:       "polling",
		Description:     "WeChat iLink Bot channel based on Stella's Weixin adapter shape.",
		SupportsPolling: true,
		SupportsMedia:   true,
		BindingTargets:  defaultBindingTargets(),
		SetupHints: []string{
			"Configure the iLink bot token. Optional base_url can point to a private iLink gateway.",
			"Bind the endpoint to a default Nekode target after the first user or group is discovered.",
		},
		Fields: []Field{
			{Name: "bot_token", Label: "Bot token", Type: FieldString, Required: true, Sensitive: true, Description: "WeChat iLink bot token."},
			{Name: "base_url", Label: "Base URL", Type: FieldString, Description: "Optional iLink base URL override."},
			{Name: "bot_id", Label: "Bot ID", Type: FieldString, Description: "Optional bot identity."},
			{Name: "user_id", Label: "User ID", Type: FieldString, Description: "Optional user identity."},
			{Name: "enable_notify", Label: "Enable notifications", Type: FieldBoolean, Description: "Allow Nekode notifications to use this endpoint."},
		},
	},
	{
		Provider:          ProviderTerminal,
		DisplayName:       "Terminal",
		Transport:         "local",
		Description:       "Local terminal channel for development smoke tests and manual operator input.",
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
	},
}

func ListProviders() []ProviderSchema {
	out := make([]ProviderSchema, len(providerSchemas))
	copy(out, providerSchemas)
	return out
}

func GetProvider(provider string) (ProviderSchema, bool) {
	provider = normalizeProvider(provider)
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

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "wechat":
		return ProviderWeixin
	default:
		return provider
	}
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
