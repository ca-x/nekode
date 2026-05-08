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
