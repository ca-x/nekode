package imadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderRegistryCoversStellaProviders(t *testing.T) {
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
	for _, provider := range []string{ProviderTelegram, ProviderQQ, ProviderFeishu, ProviderWeixin, ProviderTerminal} {
		if !providers[provider] {
			t.Fatalf("provider %q not registered; got %#v", provider, providers)
		}
	}
}

func TestValidateConfigAndRedact(t *testing.T) {
	if err := ValidateConfig(ProviderFeishu, `{"app_id":"app","app_secret":"secret"}`); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
	if err := ValidateConfig(ProviderFeishu, `{"app_id":"app"}`); err == nil || !strings.Contains(err.Error(), "app_secret") {
		t.Fatalf("ValidateConfig() error = %v, want missing app_secret", err)
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
}

func TestProviderSchemaShapeForConfigUI(t *testing.T) {
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

	redacted, err = RedactConfig("unknown-provider", `{"token":"leave-alone","nested":{"ok":true}}`)
	if err != nil {
		t.Fatalf("RedactConfig(unknown) error = %v", err)
	}
	if !strings.Contains(redacted, "leave-alone") || !strings.Contains(redacted, "nested") {
		t.Fatalf("unknown provider redacted config = %s, want normalized unmasked object", redacted)
	}
}
