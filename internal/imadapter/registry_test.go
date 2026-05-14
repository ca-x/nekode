package imadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderRegistryCoversRuntimeAndCatalogProviders(t *testing.T) {
	providers := map[string]bool{}
	for _, schema := range ListProviders() {
		providers[schema.Provider] = true
		if schema.DisplayName == "" || len(schema.Fields) == 0 {
			t.Fatalf("schema %q missing display name or fields: %+v", schema.Provider, schema)
		}
		if len(schema.BindingTargets) == 0 {
			t.Fatalf("schema %q missing binding targets", schema.Provider)
		}
	}
	for _, provider := range []string{
		ProviderTelegram,
		ProviderQQ,
		ProviderQQBot,
		ProviderFeishu,
		ProviderWeixin,
		ProviderWeCom,
		ProviderSlack,
		ProviderDiscord,
		ProviderDingTalk,
		ProviderWeibo,
		ProviderLine,
		ProviderMax,
		ProviderTerminal,
		ProviderServerChan,
	} {
		if !providers[provider] {
			t.Fatalf("provider %q not registered; got %#v", provider, providers)
		}
	}
}

func TestValidateConfigAndRedact(t *testing.T) {
	if err := ValidateConfig(ProviderFeishu, `{"app_id":"app","app_secret":"secret","verification_token":"verify"}`); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
	if err := ValidateConfig(ProviderFeishu, `{"app_id":"app"}`); err == nil || !strings.Contains(err.Error(), "app_secret") {
		t.Fatalf("ValidateConfig() error = %v, want missing app_secret", err)
	}
	if err := ValidateConfig(ProviderFeishu, `{"app_id":"app","app_secret":"secret"}`); err == nil || !strings.Contains(err.Error(), "verification_token") {
		t.Fatalf("ValidateConfig() error = %v, want missing verification_token", err)
	}
	redacted, err := RedactConfig(ProviderFeishu, `{"app_id":"app","app_secret":"secret","verification_token":"verify"}`)
	if err != nil {
		t.Fatalf("RedactConfig() error = %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(redacted), &config); err != nil {
		t.Fatalf("redacted JSON invalid: %v", err)
	}
	if config["app_secret"] != "***" || config["verification_token"] != "***" || config["app_id"] != "app" {
		t.Fatalf("redacted config = %#v", config)
	}

	if err := ValidateConfig(ProviderWeixin, `{"mode":"official_account","app_id":"wx","app_secret":"secret","token":"callback"}`); err != nil {
		t.Fatalf("ValidateConfig(weixin official account) error = %v", err)
	}
	if err := ValidateConfig(ProviderWeixin, `{"mode":"official_account","app_id":"wx","token":"callback"}`); err == nil || !strings.Contains(err.Error(), "app_secret") {
		t.Fatalf("ValidateConfig(weixin missing secret) error = %v, want missing app_secret", err)
	}
	if err := ValidateConfig(ProviderWeixin, `{"mode":"ilink"}`); err != nil {
		t.Fatalf("ValidateConfig(weixin ilink pre-bind) error = %v", err)
	}
	if err := ValidateConfig(ProviderServerChan, `{"bot_token":"serverchan-token"}`); err != nil {
		t.Fatalf("ValidateConfig(serverchan) error = %v", err)
	}
	if err := ValidateConfig(ProviderServerChan, `{}`); err == nil || !strings.Contains(err.Error(), "bot_token") {
		t.Fatalf("ValidateConfig(serverchan missing token) error = %v, want missing bot_token", err)
	}
	if err := ValidateConfig(ProviderSlack, `{"bot_token":"xoxb","app_token":"xapp"}`); err != nil {
		t.Fatalf("ValidateConfig(slack) error = %v", err)
	}
	if err := ValidateConfig(ProviderSlack, `{"bot_token":"xoxb"}`); err == nil || !strings.Contains(err.Error(), "app_token") {
		t.Fatalf("ValidateConfig(slack missing app token) error = %v, want missing app_token", err)
	}
	if err := ValidateConfig(ProviderWeCom, `{"mode":"websocket_bot","bot_id":"aib","bot_secret":"secret"}`); err != nil {
		t.Fatalf("ValidateConfig(wecom websocket) error = %v", err)
	}
	if err := ValidateConfig(ProviderWeCom, `{"mode":"websocket","bot_id":"aib","bot_secret":"secret"}`); err != nil {
		t.Fatalf("ValidateConfig(wecom websocket alias) error = %v", err)
	}
	if err := ValidateConfig(ProviderWeCom, `{"mode":"callback_app","corp_id":"ww","corp_secret":"secret","agent_id":"1000002","callback_token":"tok","callback_aes_key":"aes"}`); err != nil {
		t.Fatalf("ValidateConfig(wecom callback) error = %v", err)
	}
	if err := ValidateConfig(ProviderWeCom, `{"corp_id":"ww","corp_secret":"secret","agent_id":"1000002","callback_token":"tok","callback_aes_key":"aes"}`); err != nil {
		t.Fatalf("ValidateConfig(wecom default callback) error = %v", err)
	}
	if err := ValidateConfig(ProviderWeCom, `{"mode":"callback_app","corp_id":"ww"}`); err == nil || !strings.Contains(err.Error(), "corp_secret") {
		t.Fatalf("ValidateConfig(wecom missing callback secret) error = %v, want missing corp_secret", err)
	}
}

func TestProviderSchemaShapeForConfigUI(t *testing.T) {
	bindingMethods := map[string][]BindingMethod{}
	for _, schema := range ListProviders() {
		t.Run(schema.Provider, func(t *testing.T) {
			if _, ok := GetProvider(strings.ToUpper(schema.Provider)); !ok {
				t.Fatalf("GetProvider() should normalize provider case for %q", schema.Provider)
			}
			targets := map[string]bool{}
			for _, target := range schema.BindingTargets {
				targets[target] = true
			}
			for _, target := range []string{"channel", "thread", "agent", "default_target"} {
				if !targets[target] {
					t.Fatalf("schema %q missing binding target %q: %#v", schema.Provider, target, schema.BindingTargets)
				}
			}
			if len(schema.SetupHints) == 0 {
				t.Fatalf("schema %q missing setup hints", schema.Provider)
			}
			if len(schema.SetupMethods) == 0 {
				t.Fatalf("schema %q missing setup methods", schema.Provider)
			}
			bindingMethods[schema.Provider] = schema.BindingMethods
			for _, field := range schema.Fields {
				if field.Name == "group_mode" {
					if field.Type != FieldSelect || strings.Join(field.Options, ",") != "mention,always,disabled" {
						t.Fatalf("schema %q group_mode field = %+v", schema.Provider, field)
					}
				}
			}
		})
	}

	if schema, ok := GetProvider("wechat"); !ok || schema.Provider != ProviderWeixin {
		t.Fatalf("GetProvider(wechat) = %+v, %v; want weixin alias", schema, ok)
	}
	if !SupportsBindingMethod(ProviderWeixin, BindingMethodQRCode) {
		t.Fatalf("weixin should declare QR code binding support")
	}
	weixinSetupMethods := map[string]bool{}
	if schema, ok := GetProvider(ProviderWeixin); ok {
		if schema.DisplayName != "WeChat (iLink)" {
			t.Fatalf("weixin display name = %q, want iLink-specific label", schema.DisplayName)
		}
		for _, method := range schema.SetupMethods {
			weixinSetupMethods[method.Method] = true
		}
	}
	if !weixinSetupMethods[BindingMethodQRCode] || !weixinSetupMethods[SetupMethodManual] {
		t.Fatalf("weixin setup methods = %#v, want QR and manual", weixinSetupMethods)
	}
	if schema, ok := GetProvider(ProviderWeCom); !ok || schema.Provider != ProviderWeCom || !strings.Contains(schema.DisplayName, "WeChat Work") || len(schema.Fields) == 0 || strings.Join(schema.Fields[0].Options, ",") != "callback_app,websocket_bot" {
		t.Fatalf("wecom schema = %+v, %v; want distinct WeChat Work provider", schema, ok)
	}
	for provider, methods := range bindingMethods {
		if provider == ProviderWeixin {
			continue
		}
		if len(methods) != 0 || SupportsBindingMethod(provider, BindingMethodQRCode) {
			t.Fatalf("provider %q binding methods = %+v, want no QR support until implemented", provider, methods)
		}
	}
}

func TestRedactConfigOnlyMasksKnownSensitiveFields(t *testing.T) {
	redacted, err := RedactConfig(ProviderTelegram, `{"token":"secret","channel_id":"chat-1","enable_notify":true}`)
	if err != nil {
		t.Fatalf("RedactConfig(telegram) error = %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(redacted), &config); err != nil {
		t.Fatalf("redacted JSON invalid: %v", err)
	}
	if config["token"] != "***" || config["channel_id"] != "chat-1" || config["enable_notify"] != true {
		t.Fatalf("telegram redacted config = %#v", config)
	}

	redacted, err = RedactConfig(ProviderDiscord, `{"token":"secret","proxy_password":"proxy-secret","guild_id":"guild"}`)
	if err != nil {
		t.Fatalf("RedactConfig(discord) error = %v", err)
	}
	if err := json.Unmarshal([]byte(redacted), &config); err != nil {
		t.Fatalf("discord redacted JSON invalid: %v", err)
	}
	if config["token"] != "***" || config["proxy_password"] != "***" || config["guild_id"] != "guild" {
		t.Fatalf("discord redacted config = %#v", config)
	}

	redacted, err = RedactConfig("unknown-provider", `{"token":"leave-alone","nested":{"ok":true}}`)
	if err != nil {
		t.Fatalf("RedactConfig(unknown) error = %v", err)
	}
	if !strings.Contains(redacted, "leave-alone") || !strings.Contains(redacted, "nested") {
		t.Fatalf("unknown provider redacted config = %s, want normalized unmasked object", redacted)
	}
}

func TestInteractionCapabilitiesExposed(t *testing.T) {
	providers := ListProviders()
	if len(providers) == 0 {
		t.Fatalf("ListProviders() returned empty list")
	}

	// Verify all providers have interaction capabilities
	for _, schema := range providers {
		if schema.InteractionCapabilities == nil {
			t.Fatalf("provider %q missing interaction capabilities", schema.Provider)
		}
		// At minimum, all providers should support text
		if schema.InteractionCapabilities.Text == nil {
			t.Fatalf("provider %q missing text capability", schema.Provider)
		}
		if schema.InteractionCapabilities.Text.Scope == "" {
			t.Fatalf("provider %q text capability missing scope", schema.Provider)
		}
	}

	// Verify Telegram has rich capabilities
	telegram, ok := GetProvider(ProviderTelegram)
	if !ok {
		t.Fatalf("GetProvider(telegram) failed")
	}
	if telegram.InteractionCapabilities.InlineButtons == nil {
		t.Fatalf("telegram should have inline buttons capability")
	}
	if !telegram.InteractionCapabilities.InlineButtons.Callback {
		t.Fatalf("telegram inline buttons should support callbacks")
	}
	if telegram.InteractionCapabilities.Threads == nil {
		t.Fatalf("telegram should have threads capability")
	}

	// Verify Weixin has basic capabilities
	weixin, ok := GetProvider(ProviderWeixin)
	if !ok {
		t.Fatalf("GetProvider(weixin) failed")
	}
	if weixin.InteractionCapabilities.InlineButtons != nil {
		t.Fatalf("weixin should not have inline buttons capability")
	}
	if weixin.InteractionCapabilities.Formatting == nil {
		t.Fatalf("weixin should have formatting capability")
	}

	// Verify capabilities are JSON serializable
	data, err := json.Marshal(providers)
	if err != nil {
		t.Fatalf("failed to marshal providers to JSON: %v", err)
	}
	var unmarshaled []ProviderSchema
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal providers from JSON: %v", err)
	}
	if len(unmarshaled) != len(providers) {
		t.Fatalf("unmarshaled providers count mismatch: got %d, want %d", len(unmarshaled), len(providers))
	}
}
