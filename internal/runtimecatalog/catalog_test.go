package runtimecatalog

import "testing"

func TestDefaultCatalogContainsOnlyProtoCanonicalRecommendedKinds(t *testing.T) {
	got := map[string]bool{}
	for _, preset := range List(false, "", 200) {
		got[preset.GetKind()] = true
	}
	for _, kind := range []string{"codex", "claude", "opencode", "kimi", "gemini", "custom"} {
		if !got[kind] {
			t.Fatalf("default catalog missing proto-canonical kind %q; got=%v", kind, got)
		}
	}
	for _, kind := range []string{"cursor-agent", "copilot", "openclaw", "hermes", "pi", "kiro-cli"} {
		if got[kind] {
			t.Fatalf("default catalog includes non-canonical reference kind %q; got=%v", kind, got)
		}
	}
}

func TestExperimentalCatalogStillExposesReferenceKinds(t *testing.T) {
	got := map[string]bool{}
	for _, preset := range List(true, "", 200) {
		got[preset.GetKind()] = true
	}
	for _, kind := range []string{"cursor-agent", "copilot", "openclaw", "hermes", "pi", "kiro-cli"} {
		if !got[kind] {
			t.Fatalf("experimental catalog missing reference kind %q; got=%v", kind, got)
		}
	}
}
