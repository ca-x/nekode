package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/runtimeadapter"
	"github.com/ca-x/nekode/internal/storage"
)

func TestHealth(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestProtocolEndpoint(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/protocol", nil)
	s.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["protoPath"] != ProtocolPath {
		t.Fatalf("protoPath = %q, want %q", body["protoPath"], ProtocolPath)
	}
}

func TestWebConsoleStaticServing(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<!doctype html><title>Nekode</title><div id="root"></div>`), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	assets := filepath.Join(dist, "assets")
	if err := os.Mkdir(assets, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assets, "app.js"), []byte(`console.log("nekode")`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	cfg := testConfig()
	cfg.WebDistDir = dist
	s := New(cfg, slog.New(slog.DiscardHandler), newTestStore(t))

	index := doGET(t, s, "/", "")
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "Nekode") {
		t.Fatalf("index response = %d body=%s", index.Code, index.Body.String())
	}
	asset := doGET(t, s, "/assets/app.js", "")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "nekode") {
		t.Fatalf("asset response = %d body=%s", asset.Code, asset.Body.String())
	}
	spa := doGET(t, s, "/tasks/123", "")
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), "Nekode") {
		t.Fatalf("spa response = %d body=%s", spa.Code, spa.Body.String())
	}
	missingAsset := doGET(t, s, "/assets/missing.js", "")
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missingAsset.Code)
	}
	missingAPI := doGET(t, s, "/api/not-found", "")
	if missingAPI.Code != http.StatusNotFound {
		t.Fatalf("missing api status = %d, want 404", missingAPI.Code)
	}
}

func TestSetupStatusAndWebInit(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	status := doGET(t, s, "/api/auth/setup-status", "")
	if status.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", status.Code, status.Body.String())
	}
	var setupStatus struct {
		Initialized      bool     `json:"initialized"`
		WebSetupEnabled  bool     `json:"webSetupEnabled"`
		BootstrapMethods []string `json:"bootstrapMethods"`
		ServerID         string   `json:"serverId"`
		DataDir          string   `json:"dataDir"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &setupStatus); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if setupStatus.Initialized || !setupStatus.WebSetupEnabled || setupStatus.ServerID == "" || setupStatus.DataDir == "" {
		t.Fatalf("setup status body = %+v", setupStatus)
	}
	if len(setupStatus.BootstrapMethods) != 2 || setupStatus.BootstrapMethods[0] != "env" || setupStatus.BootstrapMethods[1] != "web" {
		t.Fatalf("bootstrap methods = %+v, want env,web", setupStatus.BootstrapMethods)
	}

	initResp := doJSON(t, s, http.MethodPost, "/api/auth/init", "", map[string]any{
		"username":    "admin",
		"password":    "secret123",
		"displayName": "Admin",
	})
	if initResp.Code != http.StatusCreated {
		t.Fatalf("init status = %d body=%s", initResp.Code, initResp.Body.String())
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(initResp.Body.Bytes(), &tokenBody); err != nil {
		t.Fatalf("decode init response: %v", err)
	}
	if tokenBody.Token == "" {
		t.Fatal("init token is empty")
	}
	again := doJSON(t, s, http.MethodPost, "/api/auth/init", "", map[string]any{
		"username": "other",
		"password": "secret123",
	})
	if again.Code != http.StatusConflict {
		t.Fatalf("second init status = %d body=%s, want 409", again.Code, again.Body.String())
	}
	status = doGET(t, s, "/api/auth/init-status", "")
	if status.Code != http.StatusOK {
		t.Fatalf("init-status = %d body=%s", status.Code, status.Body.String())
	}
	if err := json.Unmarshal(status.Body.Bytes(), &setupStatus); err != nil {
		t.Fatalf("decode init status: %v", err)
	}
	if !setupStatus.Initialized {
		t.Fatalf("initialized = false after init")
	}
}

func TestWebSetupCanBeDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.BootstrapDisableWeb = true
	s := New(cfg, slog.New(slog.DiscardHandler), newTestStore(t))

	status := doGET(t, s, "/api/auth/setup-status", "")
	if status.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", status.Code, status.Body.String())
	}
	var setupStatus struct {
		Initialized      bool     `json:"initialized"`
		WebSetupEnabled  bool     `json:"webSetupEnabled"`
		BootstrapMethods []string `json:"bootstrapMethods"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &setupStatus); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if setupStatus.Initialized || setupStatus.WebSetupEnabled {
		t.Fatalf("setup status body = %+v", setupStatus)
	}
	if len(setupStatus.BootstrapMethods) != 1 || setupStatus.BootstrapMethods[0] != "env" {
		t.Fatalf("bootstrap methods = %+v, want env only", setupStatus.BootstrapMethods)
	}

	initResp := doJSON(t, s, http.MethodPost, "/api/auth/init", "", map[string]any{
		"username": "admin",
		"password": "secret123",
	})
	if initResp.Code != http.StatusForbidden {
		t.Fatalf("init disabled status = %d body=%s, want 403", initResp.Code, initResp.Body.String())
	}
	bootstrapResp := doJSON(t, s, http.MethodPost, "/api/auth/bootstrap", "", map[string]any{
		"username": "admin",
		"password": "secret123",
	})
	if bootstrapResp.Code != http.StatusForbidden {
		t.Fatalf("bootstrap disabled status = %d body=%s, want 403", bootstrapResp.Code, bootstrapResp.Body.String())
	}
}

func TestEnvironmentBootstrap(t *testing.T) {
	cfg := testConfig()
	cfg.BootstrapAdminUsername = "env-admin"
	cfg.BootstrapAdminPassword = "secret123"
	cfg.BootstrapAdminName = "Env Admin"
	s := New(cfg, slog.New(slog.DiscardHandler), newTestStore(t))

	if err := s.BootstrapFromEnvironment(context.Background()); err != nil {
		t.Fatalf("BootstrapFromEnvironment() error = %v", err)
	}
	login := doJSON(t, s, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "env-admin",
		"password": "secret123",
	})
	if login.Code != http.StatusOK {
		t.Fatalf("login env admin status = %d body=%s", login.Code, login.Body.String())
	}
	if err := s.BootstrapFromEnvironment(context.Background()); err != nil {
		t.Fatalf("BootstrapFromEnvironment(already initialized) error = %v", err)
	}
	count, err := s.store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("CountUsers() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("users = %d, want 1", count)
	}
}

func TestEnvironmentBootstrapIncompleteDoesNotCreateUser(t *testing.T) {
	cfg := testConfig()
	cfg.BootstrapAdminUsername = "env-admin"
	s := New(cfg, slog.New(slog.DiscardHandler), newTestStore(t))

	if err := s.BootstrapFromEnvironment(context.Background()); err != nil {
		t.Fatalf("BootstrapFromEnvironment() error = %v", err)
	}
	initialized, err := s.auth.Initialized(context.Background())
	if err != nil {
		t.Fatalf("Initialized() error = %v", err)
	}
	if initialized {
		t.Fatal("Initialized() = true, want false")
	}
}

func TestAuthAndCoreAPIs(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	token := bootstrapToken(t, s)
	providers := doGET(t, s, "/api/im/providers", token)
	if providers.Code != http.StatusOK {
		t.Fatalf("list IM providers status = %d body=%s", providers.Code, providers.Body.String())
	}
	var providerBody struct {
		Items []struct {
			Provider string `json:"provider"`
			Fields   []struct {
				Name      string `json:"name"`
				Sensitive bool   `json:"sensitive"`
			} `json:"fields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(providers.Body.Bytes(), &providerBody); err != nil {
		t.Fatalf("decode IM providers: %v", err)
	}
	seenProviders := map[string]bool{}
	for _, provider := range providerBody.Items {
		seenProviders[provider.Provider] = true
	}
	for _, provider := range []string{"telegram", "qq", "feishu", "weixin", "terminal"} {
		if !seenProviders[provider] {
			t.Fatalf("IM providers missing %q: %+v", provider, providerBody.Items)
		}
	}

	endpoint := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints", token, map[string]any{
		"kind":            "web",
		"provider":        "browser",
		"displayName":     "Web Console",
		"inboundEnabled":  true,
		"outboundEnabled": true,
	})
	if endpoint.Code != http.StatusCreated {
		t.Fatalf("create endpoint status = %d body=%s", endpoint.Code, endpoint.Body.String())
	}
	missingIMConfig := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints", token, map[string]any{
		"kind":        "im",
		"provider":    "feishu",
		"displayName": "Feishu",
		"configJson":  `{"app_id":"app"}`,
	})
	if missingIMConfig.Code != http.StatusBadRequest {
		t.Fatalf("create IM endpoint without secret status = %d body=%s", missingIMConfig.Code, missingIMConfig.Body.String())
	}
	imEndpoint := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints", token, map[string]any{
		"kind":            "im",
		"provider":        "feishu",
		"displayName":     "Feishu Ops",
		"inboundEnabled":  true,
		"outboundEnabled": true,
		"authMode":        "webhook_signature",
		"configJson": `{
			"app_id":"app",
			"app_secret":"secret",
			"verification_token":"verify",
			"group_mode":"mention",
			"default_target":"#general",
			"agent_profile_id":"agent-default",
			"system_prompt_id":"prompt-default",
			"allowed_tools":["search","shell"],
			"groups":{
				"oc_123":{
					"target":"#ops",
					"thread_id":"thread-ops",
					"group_mode":"always",
					"agent_profile_id":"agent-ops",
					"system_prompt":"You are helping the ops channel.",
					"tool_policy":{"allow":["search"]},
					"disabled_tools":["shell"]
				}
			}
		}`,
	})
	if imEndpoint.Code != http.StatusCreated {
		t.Fatalf("create IM endpoint status = %d body=%s", imEndpoint.Code, imEndpoint.Body.String())
	}
	var imEndpointBody storage.InteractionEndpoint
	if err := json.Unmarshal(imEndpoint.Body.Bytes(), &imEndpointBody); err != nil {
		t.Fatalf("decode IM endpoint: %v", err)
	}
	if !strings.Contains(imEndpointBody.ConfigJSON, `"app_secret":"***"`) ||
		!strings.Contains(imEndpointBody.ConfigJSON, `"verification_token":"***"`) {
		t.Fatalf("IM endpoint config was not redacted: %s", imEndpointBody.ConfigJSON)
	}
	testEndpoint := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints/"+imEndpointBody.ID+"/test", token, map[string]any{})
	if testEndpoint.Code != http.StatusOK {
		t.Fatalf("test endpoint status = %d body=%s", testEndpoint.Code, testEndpoint.Body.String())
	}
	var testEndpointBody struct {
		Ready       bool   `json:"ready"`
		RuntimeLive bool   `json:"runtimeLive"`
		Summary     string `json:"summary"`
	}
	if err := json.Unmarshal(testEndpoint.Body.Bytes(), &testEndpointBody); err != nil {
		t.Fatalf("decode endpoint test: %v", err)
	}
	if !testEndpointBody.Ready || testEndpointBody.RuntimeLive || !strings.Contains(testEndpointBody.Summary, "Provider receive/send runtime") {
		t.Fatalf("endpoint test result = %+v, want config-ready non-live runtime", testEndpointBody)
	}
	updateEndpoint := doJSON(t, s, http.MethodPatch, "/api/interaction-endpoints/"+imEndpointBody.ID, token, map[string]any{
		"displayName":     "Feishu Ops Primary",
		"inboundEnabled":  false,
		"outboundEnabled": true,
		"authMode":        "webhook_signature",
	})
	if updateEndpoint.Code != http.StatusOK {
		t.Fatalf("update endpoint status = %d body=%s", updateEndpoint.Code, updateEndpoint.Body.String())
	}
	var updatedEndpoint storage.InteractionEndpoint
	if err := json.Unmarshal(updateEndpoint.Body.Bytes(), &updatedEndpoint); err != nil {
		t.Fatalf("decode updated endpoint: %v", err)
	}
	if updatedEndpoint.DisplayName != "Feishu Ops Primary" || updatedEndpoint.InboundEnabled {
		t.Fatalf("updated endpoint = %+v, want renamed inbound-disabled endpoint", updatedEndpoint)
	}
	policyResp := doGET(t, s, "/api/im/policies/effective?endpointId="+imEndpointBody.ID+"&conversationId=oc_123", token)
	if policyResp.Code != http.StatusOK {
		t.Fatalf("effective IM policy status = %d body=%s", policyResp.Code, policyResp.Body.String())
	}
	var policyBody struct {
		EndpointID      string         `json:"endpointId"`
		Provider        string         `json:"provider"`
		ConversationID  string         `json:"conversationId"`
		Matched         bool           `json:"matched"`
		GroupMode       string         `json:"groupMode"`
		Target          string         `json:"target"`
		ThreadID        string         `json:"threadId"`
		AgentProfileID  string         `json:"agentProfileId"`
		SystemPromptID  string         `json:"systemPromptId"`
		SystemPrompt    string         `json:"systemPrompt"`
		ToolPolicy      map[string]any `json:"toolPolicy"`
		AllowedTools    []string       `json:"allowedTools"`
		DisabledTools   []string       `json:"disabledTools"`
		AppSecret       string         `json:"app_secret"`
		VerificationTok string         `json:"verification_token"`
	}
	if err := json.Unmarshal(policyResp.Body.Bytes(), &policyBody); err != nil {
		t.Fatalf("decode IM policy: %v", err)
	}
	if policyBody.EndpointID != imEndpointBody.ID || policyBody.Provider != "feishu" || !policyBody.Matched {
		t.Fatalf("effective IM policy source = %+v", policyBody)
	}
	if policyBody.GroupMode != "always" || policyBody.Target != "#ops" || policyBody.ThreadID != "thread-ops" {
		t.Fatalf("effective IM policy routing = %+v", policyBody)
	}
	if policyBody.AgentProfileID != "agent-ops" || policyBody.SystemPromptID != "prompt-default" || policyBody.SystemPrompt == "" {
		t.Fatalf("effective IM policy overrides = %+v", policyBody)
	}
	if policyBody.AppSecret != "" || policyBody.VerificationTok != "" {
		t.Fatalf("effective IM policy leaked credential fields: %+v", policyBody)
	}
	fallbackPolicy := doGET(t, s, "/api/im/policies/effective?endpointId="+imEndpointBody.ID+"&conversationId=oc_unknown", token)
	if fallbackPolicy.Code != http.StatusOK {
		t.Fatalf("fallback IM policy status = %d body=%s", fallbackPolicy.Code, fallbackPolicy.Body.String())
	}
	var fallbackBody struct {
		Matched        bool     `json:"matched"`
		GroupMode      string   `json:"groupMode"`
		Target         string   `json:"target"`
		AgentProfileID string   `json:"agentProfileId"`
		AllowedTools   []string `json:"allowedTools"`
	}
	if err := json.Unmarshal(fallbackPolicy.Body.Bytes(), &fallbackBody); err != nil {
		t.Fatalf("decode fallback IM policy: %v", err)
	}
	if fallbackBody.Matched || fallbackBody.GroupMode != "mention" || fallbackBody.Target != "#general" || fallbackBody.AgentProfileID != "agent-default" || len(fallbackBody.AllowedTools) != 2 {
		t.Fatalf("fallback IM policy = %+v", fallbackBody)
	}
	nonIMPolicy := doGET(t, s, "/api/im/policies/effective?endpointId="+endpointBodyID(t, endpoint)+"&conversationId=web", token)
	if nonIMPolicy.Code != http.StatusBadRequest {
		t.Fatalf("non-IM policy status = %d body=%s", nonIMPolicy.Code, nonIMPolicy.Body.String())
	}
	routeResp := doJSON(t, s, http.MethodPost, "/api/notification-routes", token, map[string]any{
		"target":     "#general",
		"threadId":   "thread-im-1",
		"endpointId": imEndpointBody.ID,
		"eventKind":  "messages",
		"preference": "all",
		"enabled":    true,
		"configJson": `{"purpose":"im-notifications"}`,
	})
	if routeResp.Code != http.StatusCreated {
		t.Fatalf("create notification route status = %d body=%s", routeResp.Code, routeResp.Body.String())
	}
	var routeBody storage.NotificationRoute
	if err := json.Unmarshal(routeResp.Body.Bytes(), &routeBody); err != nil {
		t.Fatalf("decode notification route: %v", err)
	}
	if routeBody.EventKind != "message" || routeBody.EndpointID != imEndpointBody.ID {
		t.Fatalf("notification route = %+v, want normalized message route for %s", routeBody, imEndpointBody.ID)
	}
	deleteActiveEndpoint := doJSON(t, s, http.MethodDelete, "/api/interaction-endpoints/"+imEndpointBody.ID, token, map[string]any{})
	if deleteActiveEndpoint.Code != http.StatusConflict {
		t.Fatalf("delete endpoint with route status = %d body=%s, want conflict", deleteActiveEndpoint.Code, deleteActiveEndpoint.Body.String())
	}
	resolvedRoutes := doGET(t, s, "/api/notification-routes/resolve?target=%23general&threadId=thread-im-1&eventKind=message", token)
	if resolvedRoutes.Code != http.StatusOK {
		t.Fatalf("resolve notification routes status = %d body=%s", resolvedRoutes.Code, resolvedRoutes.Body.String())
	}
	var resolvedBody struct {
		Items []storage.NotificationRoute `json:"items"`
	}
	if err := json.Unmarshal(resolvedRoutes.Body.Bytes(), &resolvedBody); err != nil {
		t.Fatalf("decode resolved routes: %v", err)
	}
	if len(resolvedBody.Items) != 1 || resolvedBody.Items[0].ID != routeBody.ID {
		t.Fatalf("resolved routes = %+v, want %s", resolvedBody.Items, routeBody.ID)
	}
	targetRouteResp := doJSON(t, s, http.MethodPost, "/api/notification-routes", token, map[string]any{
		"target":     "#general",
		"endpointId": imEndpointBody.ID,
		"eventKind":  "task",
		"preference": "mentions",
		"enabled":    true,
		"configJson": `{}`,
	})
	if targetRouteResp.Code != http.StatusCreated {
		t.Fatalf("create target route status = %d body=%s", targetRouteResp.Code, targetRouteResp.Body.String())
	}
	var targetRouteBody storage.NotificationRoute
	if err := json.Unmarshal(targetRouteResp.Body.Bytes(), &targetRouteBody); err != nil {
		t.Fatalf("decode target route: %v", err)
	}
	listRoutes := doGET(t, s, "/api/notification-routes?endpointId="+imEndpointBody.ID, token)
	if listRoutes.Code != http.StatusOK {
		t.Fatalf("list notification routes status = %d body=%s", listRoutes.Code, listRoutes.Body.String())
	}
	var listRoutesBody struct {
		Items []storage.NotificationRoute `json:"items"`
	}
	if err := json.Unmarshal(listRoutes.Body.Bytes(), &listRoutesBody); err != nil {
		t.Fatalf("decode route list: %v", err)
	}
	if len(listRoutesBody.Items) != 2 {
		t.Fatalf("route list = %+v, want thread and target routes", listRoutesBody.Items)
	}
	updateRoute := doJSON(t, s, http.MethodPatch, "/api/notification-routes/"+routeBody.ID, token, map[string]any{
		"threadId":   "",
		"endpointId": imEndpointBody.ID,
		"eventKind":  "mention",
		"preference": "mentions",
		"enabled":    false,
	})
	if updateRoute.Code != http.StatusOK {
		t.Fatalf("update notification route status = %d body=%s", updateRoute.Code, updateRoute.Body.String())
	}
	var updatedRoute storage.NotificationRoute
	if err := json.Unmarshal(updateRoute.Body.Bytes(), &updatedRoute); err != nil {
		t.Fatalf("decode updated route: %v", err)
	}
	if updatedRoute.ThreadID != "" || updatedRoute.EventKind != "mention" || updatedRoute.Preference != "mentions" || updatedRoute.Enabled {
		t.Fatalf("updated route = %+v, want disabled target mention route", updatedRoute)
	}
	deleteRoute := doJSON(t, s, http.MethodDelete, "/api/notification-routes/"+targetRouteBody.ID, token, map[string]any{})
	if deleteRoute.Code != http.StatusOK {
		t.Fatalf("delete notification route status = %d body=%s", deleteRoute.Code, deleteRoute.Body.String())
	}
	deleteMissingRoute := doJSON(t, s, http.MethodDelete, "/api/notification-routes/"+targetRouteBody.ID, token, map[string]any{})
	if deleteMissingRoute.Code != http.StatusNotFound {
		t.Fatalf("delete missing notification route status = %d body=%s, want 404", deleteMissingRoute.Code, deleteMissingRoute.Body.String())
	}

	message := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":  "#general",
		"content": "hello",
	})
	if message.Code != http.StatusCreated {
		t.Fatalf("create message status = %d body=%s", message.Code, message.Body.String())
	}
	var messageBody storage.Message
	if err := json.Unmarshal(message.Body.Bytes(), &messageBody); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	reply := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":           "#general",
		"content":          "reply with reference",
		"replyToMessageId": messageBody.ID,
	})
	if reply.Code != http.StatusCreated {
		t.Fatalf("create reply status = %d body=%s", reply.Code, reply.Body.String())
	}
	var replyBody storage.Message
	if err := json.Unmarshal(reply.Body.Bytes(), &replyBody); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if replyBody.ReplyToMessageID != messageBody.ID {
		t.Fatalf("replyToMessageId = %q, want %q", replyBody.ReplyToMessageID, messageBody.ID)
	}

	messages := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/messages?target=%23general", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(messages, req)
	if messages.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body=%s", messages.Code, messages.Body.String())
	}
	search := doGET(t, s, "/api/messages/search?target=%23general&q=reference&sort=recent", token)
	if search.Code != http.StatusOK {
		t.Fatalf("search messages status = %d body=%s", search.Code, search.Body.String())
	}
	assertJSONItems(t, search.Body.Bytes(), 1)
	save := doJSON(t, s, http.MethodPost, "/api/messages/"+messageBody.ID+"/save?target=%23general", token, map[string]any{})
	if save.Code != http.StatusOK {
		t.Fatalf("save message status = %d body=%s", save.Code, save.Body.String())
	}
	saved := doGET(t, s, "/api/messages/saved?target=%23general", token)
	if saved.Code != http.StatusOK {
		t.Fatalf("list saved messages status = %d body=%s", saved.Code, saved.Body.String())
	}
	assertJSONItems(t, saved.Body.Bytes(), 1)
	unsave := doJSON(t, s, http.MethodDelete, "/api/messages/"+messageBody.ID+"/save?target=%23general", token, map[string]any{})
	if unsave.Code != http.StatusOK {
		t.Fatalf("unsave message status = %d body=%s", unsave.Code, unsave.Body.String())
	}

	attachmentResp := doMultipartAttachment(t, s, token, "#general", "preview.html", "text/html", "<strong>safe</strong>")
	if attachmentResp.Code != http.StatusCreated {
		t.Fatalf("upload attachment status = %d body=%s", attachmentResp.Code, attachmentResp.Body.String())
	}
	var attachment storage.Attachment
	if err := json.Unmarshal(attachmentResp.Body.Bytes(), &attachment); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if attachment.ID == "" || attachment.MimeType != "text/html" || attachment.DownloadURL == "" {
		t.Fatalf("attachment body = %+v, want html attachment with download url", attachment)
	}
	attachmentMessage := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":        "#general",
		"content":       "with attachment",
		"attachmentIds": []string{attachment.ID},
	})
	if attachmentMessage.Code != http.StatusCreated {
		t.Fatalf("create attachment message status = %d body=%s", attachmentMessage.Code, attachmentMessage.Body.String())
	}
	var attachmentMessageBody storage.Message
	if err := json.Unmarshal(attachmentMessage.Body.Bytes(), &attachmentMessageBody); err != nil {
		t.Fatalf("decode attachment message: %v", err)
	}
	if len(attachmentMessageBody.Attachments) != 1 || attachmentMessageBody.Attachments[0].Filename != "preview.html" {
		t.Fatalf("message attachments = %+v, want uploaded attachment", attachmentMessageBody.Attachments)
	}
	download := doGET(t, s, "/api/attachments/"+attachment.ID+"/content", token)
	if download.Code != http.StatusOK || !strings.Contains(download.Body.String(), "<strong>safe</strong>") {
		t.Fatalf("download attachment status = %d body=%s", download.Code, download.Body.String())
	}

	task := doJSON(t, s, http.MethodPost, "/api/tasks", token, map[string]any{
		"summary":       "wire backend",
		"description":   "connect the daemon bridge",
		"target":        "#general",
		"blockedReason": "waiting for test credentials",
	})
	if task.Code != http.StatusCreated {
		t.Fatalf("create task status = %d body=%s", task.Code, task.Body.String())
	}
	var taskBody storage.Task
	if err := json.Unmarshal(task.Body.Bytes(), &taskBody); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if taskBody.Description != "connect the daemon bridge" || taskBody.BlockedReason != "waiting for test credentials" {
		t.Fatalf("task detail fields = %+v, want description and blocked reason", taskBody)
	}
	updated := doJSON(t, s, http.MethodPatch, "/api/tasks/"+taskBody.ID, token, map[string]any{
		"state":       "in_progress",
		"description": "daemon bridge is connected",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update task status = %d body=%s", updated.Code, updated.Body.String())
	}
	blocked := doJSON(t, s, http.MethodPatch, "/api/tasks/"+taskBody.ID, token, map[string]any{
		"state":         "blocked",
		"blockedReason": "waiting on API review",
	})
	if blocked.Code != http.StatusOK {
		t.Fatalf("block task status = %d body=%s", blocked.Code, blocked.Body.String())
	}
	var blockedBody storage.Task
	if err := json.Unmarshal(blocked.Body.Bytes(), &blockedBody); err != nil {
		t.Fatalf("decode blocked task: %v", err)
	}
	if blockedBody.Description != "daemon bridge is connected" || blockedBody.BlockedReason != "waiting on API review" {
		t.Fatalf("blocked task detail fields = %+v, want patched detail fields", blockedBody)
	}
	comment := doJSON(t, s, http.MethodPost, "/api/tasks/"+taskBody.ID+"/comments", token, map[string]any{
		"content":   "Reviewer asked for timeline evidence.",
		"requestId": "task-comment-test",
	})
	if comment.Code != http.StatusCreated {
		t.Fatalf("create task comment status = %d body=%s", comment.Code, comment.Body.String())
	}
	var commentBody storage.Message
	if err := json.Unmarshal(comment.Body.Bytes(), &commentBody); err != nil {
		t.Fatalf("decode task comment: %v", err)
	}
	if commentBody.ThreadID != taskBody.ID || commentBody.Target != "#general" {
		t.Fatalf("task comment routing = %+v, want thread task id and task target", commentBody)
	}
	comments := doGET(t, s, "/api/tasks/"+taskBody.ID+"/comments", token)
	if comments.Code != http.StatusOK {
		t.Fatalf("list task comments status = %d body=%s", comments.Code, comments.Body.String())
	}
	assertJSONItems(t, comments.Body.Bytes(), 1)
	timeline := doGET(t, s, "/api/tasks/"+taskBody.ID+"/timeline", token)
	if timeline.Code != http.StatusOK {
		t.Fatalf("task timeline status = %d body=%s", timeline.Code, timeline.Body.String())
	}
	assertJSONItemsAtLeast(t, timeline.Body.Bytes(), 4)
	listBlocked := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks?state=blocked&target=%23general", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(listBlocked, req)
	if listBlocked.Code != http.StatusOK {
		t.Fatalf("list blocked tasks status = %d body=%s", listBlocked.Code, listBlocked.Body.String())
	}
	cancelledTask := doJSON(t, s, http.MethodPost, "/api/tasks", token, map[string]any{
		"summary": "cancel stale work",
		"target":  "#general",
		"state":   "cancelled",
	})
	if cancelledTask.Code != http.StatusCreated {
		t.Fatalf("create cancelled task status = %d body=%s", cancelledTask.Code, cancelledTask.Body.String())
	}
	var cancelledBody storage.Task
	if err := json.Unmarshal(cancelledTask.Body.Bytes(), &cancelledBody); err != nil {
		t.Fatalf("decode cancelled task: %v", err)
	}
	if cancelledBody.State != "canceled" {
		t.Fatalf("cancelled alias stored as %q, want canceled", cancelledBody.State)
	}
	invalidList := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks?state=reviewing", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(invalidList, req)
	if invalidList.Code != http.StatusBadRequest {
		t.Fatalf("list invalid state status = %d body=%s, want 400", invalidList.Code, invalidList.Body.String())
	}
	events, err := s.daemon.ListEventsSince(context.Background(), &daemonv1.ListEventsSinceRequest{
		Cursor: &daemonv1.EventCursor{Target: "#general"},
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListEventsSince() error = %v", err)
	}
	if findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_MESSAGE, daemonv1.EventOperation_EVENT_OPERATION_APPENDED) == nil {
		t.Fatalf("HTTP mutation events = %+v, want appended message event", events.GetEvents())
	}
	if findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_CREATED) == nil {
		t.Fatalf("HTTP mutation events = %+v, want created task event", events.GetEvents())
	}
	changed := findHTTPMutationEvent(events.GetEvents(), daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK, daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED)
	if changed == nil || changed.GetScope().GetScopeType() != daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK {
		t.Fatalf("HTTP mutation events = %+v, want task state_changed event with task scope", events.GetEvents())
	}
}

func TestCoreAPIsRejectMissingAuthAndInvalidInputs(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))

	unauthenticated := []struct {
		name   string
		method string
		target string
		body   any
	}{
		{name: "auth me", method: http.MethodGet, target: "/api/auth/me"},
		{name: "list messages", method: http.MethodGet, target: "/api/messages?target=%23general"},
		{name: "create message", method: http.MethodPost, target: "/api/messages", body: map[string]any{"target": "#general", "content": "hello"}},
		{name: "list tasks", method: http.MethodGet, target: "/api/tasks"},
		{name: "create task", method: http.MethodPost, target: "/api/tasks", body: map[string]any{"target": "#general", "summary": "ship"}},
		{name: "list reminders", method: http.MethodGet, target: "/api/reminders"},
		{name: "create reminder", method: http.MethodPost, target: "/api/reminders", body: map[string]any{"target": "#general", "title": "standup", "delaySeconds": 60}},
		{name: "daemon info", method: http.MethodGet, target: "/api/daemon/info"},
		{name: "create daemon agent", method: http.MethodPost, target: "/api/daemon/agents", body: map[string]any{"profileId": "agent"}},
		{name: "list interaction endpoints", method: http.MethodGet, target: "/api/interaction-endpoints"},
		{name: "list IM providers", method: http.MethodGet, target: "/api/im/providers"},
		{name: "list notification routes", method: http.MethodGet, target: "/api/notification-routes"},
	}
	for _, tt := range unauthenticated {
		t.Run("missing auth "+tt.name, func(t *testing.T) {
			resp := doJSONOrEmpty(t, s, tt.method, tt.target, "", tt.body)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s status = %d body=%s, want 401", tt.method, tt.target, resp.Code, resp.Body.String())
			}
		})
	}

	invalidToken := doGETWithToken(t, s, "/api/messages?target=%23general", "not-a-valid-token")
	if invalidToken.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d body=%s, want 401", invalidToken.Code, invalidToken.Body.String())
	}

	token := bootstrapToken(t, s)
	endpointResp := doJSON(t, s, http.MethodPost, "/api/interaction-endpoints", token, map[string]any{
		"kind":        "web",
		"provider":    "browser",
		"displayName": "Web Console",
	})
	if endpointResp.Code != http.StatusCreated {
		t.Fatalf("create endpoint status = %d body=%s", endpointResp.Code, endpointResp.Body.String())
	}
	endpointID := endpointBodyID(t, endpointResp)

	cases := []struct {
		name     string
		method   string
		target   string
		body     any
		wantCode int
	}{
		{name: "message missing content", method: http.MethodPost, target: "/api/messages", body: map[string]any{"target": "#general"}, wantCode: http.StatusBadRequest},
		{name: "message unknown attachment", method: http.MethodPost, target: "/api/messages", body: map[string]any{"target": "#general", "content": "hello", "attachmentIds": []string{"att-missing"}}, wantCode: http.StatusNotFound},
		{name: "task missing summary", method: http.MethodPost, target: "/api/tasks", body: map[string]any{"target": "#general"}, wantCode: http.StatusBadRequest},
		{name: "task invalid state", method: http.MethodPost, target: "/api/tasks", body: map[string]any{"target": "#general", "summary": "ship", "state": "reviewing"}, wantCode: http.StatusBadRequest},
		{name: "update missing task", method: http.MethodPatch, target: "/api/tasks/task-missing", body: map[string]any{"state": "done"}, wantCode: http.StatusNotFound},
		{name: "task comment missing task", method: http.MethodPost, target: "/api/tasks/task-missing/comments", body: map[string]any{"content": "review"}, wantCode: http.StatusNotFound},
		{name: "task comment empty content", method: http.MethodPost, target: "/api/tasks/task-missing/comments", body: map[string]any{"content": " "}, wantCode: http.StatusBadRequest},
		{name: "task timeline missing task", method: http.MethodGet, target: "/api/tasks/task-missing/timeline", wantCode: http.StatusNotFound},
		{name: "reminder missing target", method: http.MethodPost, target: "/api/reminders", body: map[string]any{"title": "standup", "delaySeconds": 60}, wantCode: http.StatusBadRequest},
		{name: "reminder missing title and prompt", method: http.MethodPost, target: "/api/reminders", body: map[string]any{"target": "#general", "delaySeconds": 60}, wantCode: http.StatusBadRequest},
		{name: "reminder invalid schedule", method: http.MethodPost, target: "/api/reminders", body: map[string]any{"target": "#general", "title": "standup", "scheduleKind": "never"}, wantCode: http.StatusBadRequest},
		{name: "snooze reminder missing delay", method: http.MethodPost, target: "/api/reminders/rem-missing/snooze", body: map[string]any{}, wantCode: http.StatusBadRequest},
		{name: "update missing reminder", method: http.MethodPatch, target: "/api/reminders/rem-missing", body: map[string]any{"title": "later"}, wantCode: http.StatusNotFound},
		{name: "interaction endpoint missing display name", method: http.MethodPost, target: "/api/interaction-endpoints", body: map[string]any{"kind": "web", "provider": "browser"}, wantCode: http.StatusBadRequest},
		{name: "notification route missing endpoint", method: http.MethodPost, target: "/api/notification-routes", body: map[string]any{"target": "#general"}, wantCode: http.StatusBadRequest},
		{name: "notification route unknown endpoint", method: http.MethodPost, target: "/api/notification-routes", body: map[string]any{"target": "#general", "endpointId": "endpoint-missing", "eventKind": "message"}, wantCode: http.StatusBadRequest},
		{name: "notification route invalid event kind", method: http.MethodPost, target: "/api/notification-routes", body: map[string]any{"target": "#general", "endpointId": endpointID, "eventKind": "nonsense"}, wantCode: http.StatusBadRequest},
		{name: "resolve notification route missing target", method: http.MethodGet, target: "/api/notification-routes/resolve?eventKind=message", wantCode: http.StatusBadRequest},
		{name: "resolve notification route invalid kind", method: http.MethodGet, target: "/api/notification-routes/resolve?target=%23general&eventKind=nonsense", wantCode: http.StatusBadRequest},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp := doJSONOrEmpty(t, s, tt.method, tt.target, token, tt.body)
			if resp.Code != tt.wantCode {
				t.Fatalf("%s %s status = %d body=%s, want %d", tt.method, tt.target, resp.Code, resp.Body.String(), tt.wantCode)
			}
		})
	}

	nonMultipart := doJSON(t, s, http.MethodPost, "/api/attachments", token, map[string]any{"target": "#general"})
	if nonMultipart.Code != http.StatusBadRequest {
		t.Fatalf("non-multipart attachment status = %d body=%s, want 400", nonMultipart.Code, nonMultipart.Body.String())
	}

}

func TestThreadInboxReadWorkflow(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)

	parentResp := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":  "#general",
		"content": "Thread root",
	})
	if parentResp.Code != http.StatusCreated {
		t.Fatalf("create parent message status = %d body=%s", parentResp.Code, parentResp.Body.String())
	}
	var parent storage.Message
	if err := json.Unmarshal(parentResp.Body.Bytes(), &parent); err != nil {
		t.Fatalf("decode parent message: %v", err)
	}
	replyResp := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":   "#general",
		"threadId": parent.ID,
		"content":  "First thread reply",
	})
	if replyResp.Code != http.StatusCreated {
		t.Fatalf("create reply status = %d body=%s", replyResp.Code, replyResp.Body.String())
	}

	channelMessages := doGET(t, s, "/api/messages?target=%23general", token)
	if channelMessages.Code != http.StatusOK {
		t.Fatalf("list parent channel messages status = %d body=%s", channelMessages.Code, channelMessages.Body.String())
	}
	assertJSONItems(t, channelMessages.Body.Bytes(), 1)
	threadMessages := doGET(t, s, "/api/messages?target=%23general&threadId="+parent.ID, token)
	if threadMessages.Code != http.StatusOK {
		t.Fatalf("list thread messages status = %d body=%s", threadMessages.Code, threadMessages.Body.String())
	}
	assertJSONItems(t, threadMessages.Body.Bytes(), 1)

	inbox := readThreadInbox(t, s, token)
	if len(inbox) != 1 {
		t.Fatalf("inbox items = %+v, want one thread", inbox)
	}
	if inbox[0].ThreadID != parent.ID || inbox[0].Topic != "Thread root" || inbox[0].UnreadCount != 1 {
		t.Fatalf("inbox item = %+v, want parent topic and one unread reply", inbox[0])
	}

	markRead := doJSON(t, s, http.MethodPost, "/api/inbox/threads/"+parent.ID+"/read", token, map[string]any{
		"target": "#general",
	})
	if markRead.Code != http.StatusOK {
		t.Fatalf("mark thread read status = %d body=%s", markRead.Code, markRead.Body.String())
	}
	inbox = readThreadInbox(t, s, token)
	if inbox[0].UnreadCount != 0 {
		t.Fatalf("unread after mark read = %d, want 0", inbox[0].UnreadCount)
	}

	time.Sleep(time.Second)
	secondReply := doJSON(t, s, http.MethodPost, "/api/messages", token, map[string]any{
		"target":   "#general",
		"threadId": parent.ID,
		"content":  "Second thread reply",
	})
	if secondReply.Code != http.StatusCreated {
		t.Fatalf("create second reply status = %d body=%s", secondReply.Code, secondReply.Body.String())
	}
	inbox = readThreadInbox(t, s, token)
	if inbox[0].UnreadCount != 1 || inbox[0].LatestMessage.Content != "Second thread reply" {
		t.Fatalf("inbox after second reply = %+v, want one unread latest reply", inbox[0])
	}

	markAll := doJSON(t, s, http.MethodPost, "/api/inbox/threads/read-all", token, map[string]any{})
	if markAll.Code != http.StatusOK {
		t.Fatalf("mark all read status = %d body=%s", markAll.Code, markAll.Body.String())
	}
	inbox = readThreadInbox(t, s, token)
	if inbox[0].UnreadCount != 0 {
		t.Fatalf("unread after mark all = %d, want 0", inbox[0].UnreadCount)
	}
}

func readThreadInbox(t *testing.T, s *Server, token string) []storage.ThreadInboxItem {
	t.Helper()
	resp := doGET(t, s, "/api/inbox/threads", token)
	if resp.Code != http.StatusOK {
		t.Fatalf("thread inbox status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Items []storage.ThreadInboxItem `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode thread inbox: %v", err)
	}
	return body.Items
}

func TestReminderLifecycleWorkflow(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)

	createdResp := doJSON(t, s, http.MethodPost, "/api/reminders", token, map[string]any{
		"target":       "#general",
		"title":        "Review release notes",
		"prompt":       "Review release notes",
		"delaySeconds": 300,
	})
	if createdResp.Code != http.StatusCreated {
		t.Fatalf("create reminder status = %d body=%s", createdResp.Code, createdResp.Body.String())
	}
	var reminderBody storage.Reminder
	if err := json.Unmarshal(createdResp.Body.Bytes(), &reminderBody); err != nil {
		t.Fatalf("decode reminder: %v", err)
	}
	if reminderBody.ID == "" || reminderBody.Status != "active" || reminderBody.NextRunUnix == 0 {
		t.Fatalf("created reminder = %+v, want active scheduled reminder", reminderBody)
	}

	snoozed := doJSON(t, s, http.MethodPost, "/api/reminders/"+reminderBody.ID+"/snooze", token, map[string]any{
		"delaySeconds": 900,
	})
	if snoozed.Code != http.StatusOK {
		t.Fatalf("snooze reminder status = %d body=%s", snoozed.Code, snoozed.Body.String())
	}
	fireAt := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	updated := doJSON(t, s, http.MethodPatch, "/api/reminders/"+reminderBody.ID, token, map[string]any{
		"title":  "Review release gate",
		"fireAt": fireAt,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update reminder status = %d body=%s", updated.Code, updated.Body.String())
	}
	var updatedBody storage.Reminder
	if err := json.Unmarshal(updated.Body.Bytes(), &updatedBody); err != nil {
		t.Fatalf("decode updated reminder: %v", err)
	}
	if updatedBody.Title != "Review release gate" || updatedBody.Schedule != fireAt {
		t.Fatalf("updated reminder = %+v, want patched title and fireAt schedule", updatedBody)
	}

	logResp := doGET(t, s, "/api/reminders/"+reminderBody.ID+"/log", token)
	if logResp.Code != http.StatusOK {
		t.Fatalf("reminder log status = %d body=%s", logResp.Code, logResp.Body.String())
	}
	assertJSONItemsAtLeast(t, logResp.Body.Bytes(), 3)

	canceled := doJSON(t, s, http.MethodPost, "/api/reminders/"+reminderBody.ID+"/cancel", token, map[string]any{
		"cancelToken": updatedBody.CancelToken,
	})
	if canceled.Code != http.StatusOK {
		t.Fatalf("cancel reminder status = %d body=%s", canceled.Code, canceled.Body.String())
	}
	var canceledBody storage.Reminder
	if err := json.Unmarshal(canceled.Body.Bytes(), &canceledBody); err != nil {
		t.Fatalf("decode canceled reminder: %v", err)
	}
	if canceledBody.Status != "canceled" || canceledBody.Enabled {
		t.Fatalf("canceled reminder = %+v, want disabled canceled reminder", canceledBody)
	}

	list := doGET(t, s, "/api/reminders?target=%23general&includeCanceled=true", token)
	if list.Code != http.StatusOK {
		t.Fatalf("list reminders status = %d body=%s", list.Code, list.Body.String())
	}
	assertJSONItems(t, list.Body.Bytes(), 1)

	daemonCreated, err := s.daemon.ScheduleReminder(context.Background(), &daemonv1.ScheduleReminderRequest{
		Target: "#general",
		Title:  "Daemon reminder",
		ScheduleSpec: &daemonv1.ScheduleReminderRequest_DelaySeconds{
			DelaySeconds: 60,
		},
		Context: &daemonv1.RequestContext{
			Actor: &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_AGENT, AgentId: "agent-1"},
		},
	})
	if err != nil {
		t.Fatalf("ScheduleReminder() error = %v", err)
	}
	if daemonCreated.GetReminder().GetReminderId() == "" || daemonCreated.GetReminder().GetStatus() != daemonv1.ReminderStatus_REMINDER_STATUS_ACTIVE {
		t.Fatalf("daemon reminder = %+v, want active reminder", daemonCreated.GetReminder())
	}
	if _, err := s.daemon.SnoozeReminder(context.Background(), &daemonv1.SnoozeReminderRequest{
		ReminderId:   daemonCreated.GetReminder().GetReminderId(),
		DelaySeconds: 120,
	}); err != nil {
		t.Fatalf("SnoozeReminder() error = %v", err)
	}
	daemonLog, err := s.daemon.GetReminderLog(context.Background(), &daemonv1.GetReminderLogRequest{
		ReminderId: daemonCreated.GetReminder().GetReminderId(),
	})
	if err != nil {
		t.Fatalf("GetReminderLog() error = %v", err)
	}
	if len(daemonLog.GetEvents()) < 2 {
		t.Fatalf("daemon reminder log = %+v, want create and snooze events", daemonLog.GetEvents())
	}
}

func findHTTPMutationEvent(events []*daemonv1.CollaborationEvent, kind daemonv1.CollaborationEventKind, operation daemonv1.EventOperation) *daemonv1.CollaborationEvent {
	for _, event := range events {
		if event.GetKind() == kind && event.GetOperation() == operation {
			return event
		}
	}
	return nil
}

func TestDaemonBridgeEndpoints(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)

	unauthorized := httptest.NewRecorder()
	s.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/daemon/info", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("daemon info without auth status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	info := doGET(t, s, "/api/daemon/info", token)
	if info.Code != http.StatusOK {
		t.Fatalf("daemon info status = %d body=%s", info.Code, info.Body.String())
	}
	var infoBody map[string]any
	if err := json.Unmarshal(info.Body.Bytes(), &infoBody); err != nil {
		t.Fatalf("decode daemon info: %v", err)
	}
	if infoBody["serverId"] == "" || infoBody["protocolVersion"] == float64(0) {
		t.Fatalf("daemon info body = %+v, want server identity and protocol version", infoBody)
	}
	if infoBody["health"] != "idle" || infoBody["agentStatusCount"] != float64(0) {
		t.Fatalf("daemon info initial diagnostics = %+v, want idle with zero agents", infoBody)
	}

	presets := doGET(t, s, "/api/runtime-presets?includeExperimental=true", token)
	if presets.Code != http.StatusOK {
		t.Fatalf("runtime presets status = %d body=%s", presets.Code, presets.Body.String())
	}
	var presetBody struct {
		Items []struct {
			Kind             string `json:"kind"`
			SlockSupported   bool   `json:"slockSupported"`
			MulticaSupported bool   `json:"multicaSupported"`
		} `json:"items"`
	}
	if err := json.Unmarshal(presets.Body.Bytes(), &presetBody); err != nil {
		t.Fatalf("decode runtime presets: %v", err)
	}
	gotKinds := map[string]bool{}
	for _, item := range presetBody.Items {
		gotKinds[item.Kind] = true
		if item.Kind == "codex" && (!item.SlockSupported || !item.MulticaSupported) {
			t.Fatalf("codex preset = %+v, want slock and multica support", item)
		}
	}
	for _, kind := range []string{"codex", "claude", "opencode", "kimi", "gemini", "cursor-agent", "copilot", "openclaw", "hermes", "pi", "kiro-cli"} {
		if !gotKinds[kind] {
			t.Fatalf("runtime presets missing %q; got=%v", kind, gotKinds)
		}
	}

	inventory := runtimeadapter.ComputerInventory(runtimeadapter.InventoryConfig{
		ComputerID:           "computer-1",
		PreferredRuntimeKind: "codex",
		AgentID:              "agent-1",
		LookupPath: func(command string) (string, error) {
			return "/usr/bin/" + command, nil
		},
	})
	if _, err := s.daemon.RegisterComputer(context.Background(), &daemonv1.RegisterComputerRequest{
		Info: &daemonv1.ComputerInfo{
			ComputerId:  "computer-1",
			DaemonId:    "daemon-1",
			DisplayName: "Test Computer",
			Hostname:    "test-host",
		},
		Inventory: inventory,
	}); err != nil {
		t.Fatalf("RegisterComputer() error = %v", err)
	}
	inventoryResp := doGET(t, s, "/api/daemon/inventory", token)
	if inventoryResp.Code != http.StatusOK {
		t.Fatalf("daemon inventory status = %d body=%s", inventoryResp.Code, inventoryResp.Body.String())
	}
	var inventoryBody struct {
		Items []struct {
			Inventory map[string]any `json:"inventory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(inventoryResp.Body.Bytes(), &inventoryBody); err != nil {
		t.Fatalf("decode daemon inventory: %v", err)
	}
	gotInventory := inventoryBody.Items[0].Inventory
	runtimes, _ := gotInventory["runtimes"].([]any)
	runtimeProfiles, _ := gotInventory["runtime_profiles"].([]any)
	agents, _ := gotInventory["agents"].([]any)
	if len(inventoryBody.Items) != 1 ||
		len(runtimes) < 2 ||
		len(runtimeProfiles) < 2 ||
		len(agents) != 1 {
		t.Fatalf("daemon inventory body = %+v, want runtime types, templates, and bootstrap agent", inventoryBody)
	}
	var codexRuntimeID, codexTemplateID string
	for _, runtimeEntry := range inventory.GetRuntimes() {
		if runtimeEntry.GetKind() == "codex" {
			codexRuntimeID = runtimeEntry.GetRuntimeId()
			break
		}
	}
	for _, profile := range inventory.GetRuntimeProfiles() {
		if profile.GetKind() == "codex" {
			codexTemplateID = profile.GetRuntimeProfileId()
			break
		}
	}
	if codexRuntimeID == "" || codexTemplateID == "" {
		t.Fatalf("codex runtime/template missing in inventory")
	}
	createAgent := func(displayName string) struct {
		Agent struct {
			AgentID          string `json:"agent_id"`
			DisplayName      string `json:"display_name"`
			ComputerID       string `json:"computer_id"`
			RuntimeProfileID string `json:"runtime_profile_id"`
			RuntimeKind      string `json:"runtime_kind"`
		} `json:"agent"`
		RuntimeProfile struct {
			RuntimeProfileID string `json:"runtime_profile_id"`
			AdapterConfig    string `json:"adapter_config_json"`
		} `json:"runtimeProfile"`
	} {
		resp := doJSON(t, s, http.MethodPost, "/api/daemon/agents", token, map[string]any{
			"computerId":  "computer-1",
			"runtimeId":   codexRuntimeID,
			"templateId":  codexTemplateID,
			"displayName": displayName,
			"target":      "#general",
			"options": map[string]string{
				"model":            "gpt-test",
				"reasoning_effort": "medium",
				"allow_file_write": "true",
				"api_token":        "secret-token",
			},
		})
		if resp.Code != http.StatusCreated {
			t.Fatalf("create daemon agent status = %d body=%s", resp.Code, resp.Body.String())
		}
		var body struct {
			Agent struct {
				AgentID          string `json:"agent_id"`
				DisplayName      string `json:"display_name"`
				ComputerID       string `json:"computer_id"`
				RuntimeProfileID string `json:"runtime_profile_id"`
				RuntimeKind      string `json:"runtime_kind"`
			} `json:"agent"`
			RuntimeProfile struct {
				RuntimeProfileID string `json:"runtime_profile_id"`
				AdapterConfig    string `json:"adapter_config_json"`
			} `json:"runtimeProfile"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode create daemon agent: %v", err)
		}
		if body.Agent.AgentID == "" || body.Agent.ComputerID != "computer-1" || body.Agent.RuntimeKind != "codex" {
			t.Fatalf("created daemon agent = %+v", body.Agent)
		}
		if body.Agent.RuntimeProfileID == codexTemplateID || body.RuntimeProfile.RuntimeProfileID == codexTemplateID {
			t.Fatalf("created runtime profile reused template id: %+v", body.RuntimeProfile)
		}
		if !strings.Contains(body.RuntimeProfile.AdapterConfig, `"inventoryRole":"agent_instance"`) ||
			!strings.Contains(body.RuntimeProfile.AdapterConfig, `"wrapCommand"`) {
			t.Fatalf("created runtime profile adapter config = %s, want instance config with wrap command", body.RuntimeProfile.AdapterConfig)
		}
		if strings.Contains(body.RuntimeProfile.AdapterConfig, "secret-token") ||
			!strings.Contains(body.RuntimeProfile.AdapterConfig, `"api_token":"\u003credacted\u003e"`) {
			t.Fatalf("created runtime profile adapter config = %s, want redacted sensitive option", body.RuntimeProfile.AdapterConfig)
		}
		return body
	}
	firstAgent := createAgent("Review Agent")
	secondAgent := createAgent("Review Agent")
	if firstAgent.Agent.AgentID == secondAgent.Agent.AgentID {
		t.Fatalf("multi-instance create reused agent id %q", firstAgent.Agent.AgentID)
	}
	invalidAgent := doJSON(t, s, http.MethodPost, "/api/daemon/agents", token, map[string]any{
		"computerId":  "computer-1",
		"runtimeId":   codexRuntimeID,
		"templateId":  codexTemplateID,
		"displayName": "Bad Agent",
		"options":     map[string]string{"reasoning_effort": "warp"},
	})
	if invalidAgent.Code != http.StatusBadRequest {
		t.Fatalf("invalid daemon agent status = %d body=%s, want 400", invalidAgent.Code, invalidAgent.Body.String())
	}
	updatedInventory := s.daemon.ListComputerInventories(1)
	if got := len(updatedInventory[0].Inventory.GetAgents()); got != 3 {
		t.Fatalf("inventory agents = %d, want bootstrap + two created agents", got)
	}

	if _, err := s.daemon.UpdateAgentStatus(context.Background(), &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:       "agent-1",
			ComputerId:    "computer-1",
			Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
			ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_CODING,
			Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
		},
	}); err != nil {
		t.Fatalf("UpdateAgentStatus() error = %v", err)
	}
	statuses := doGET(t, s, "/api/daemon/agent-statuses?agentId=agent-1", token)
	if statuses.Code != http.StatusOK {
		t.Fatalf("agent statuses status = %d body=%s", statuses.Code, statuses.Body.String())
	}
	assertJSONItems(t, statuses.Body.Bytes(), 1)

	if _, err := s.daemon.UpdateRunStatus(context.Background(), &daemonv1.UpdateRunStatusRequest{
		RunId:   "run-1",
		AgentId: "agent-1",
		State:   daemonv1.RunState_RUN_STATE_RUNNING,
		Summary: "runtime smoke",
	}); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	runs := doGET(t, s, "/api/daemon/runs?agentId=agent-1", token)
	if runs.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", runs.Code, runs.Body.String())
	}
	assertJSONItems(t, runs.Body.Bytes(), 1)

	if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
		Target:  "#general",
		AgentId: "agent-1",
		Kind:    "test_run",
		Summary: "bridge test",
	}); err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}
	activity := doGET(t, s, "/api/daemon/activity?target=%23general", token)
	if activity.Code != http.StatusOK {
		t.Fatalf("activity status = %d body=%s", activity.Code, activity.Body.String())
	}
	assertJSONItems(t, activity.Body.Bytes(), 1)

	info = doGET(t, s, "/api/daemon/info", token)
	if info.Code != http.StatusOK {
		t.Fatalf("daemon info after diagnostics status = %d body=%s", info.Code, info.Body.String())
	}
	if err := json.Unmarshal(info.Body.Bytes(), &infoBody); err != nil {
		t.Fatalf("decode daemon info after diagnostics: %v", err)
	}
	if infoBody["health"] != "ok" || infoBody["agentStatusCount"] != float64(1) ||
		infoBody["runCount"] != float64(1) || infoBody["activityCount"] != float64(1) {
		t.Fatalf("daemon info diagnostics = %+v, want health/count rollup", infoBody)
	}

	if _, err := s.daemon.UpdateAgentStatus(context.Background(), &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:       "agent-1",
			ComputerId:    "computer-1",
			Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_STALE,
			ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_THINKING,
			Health:        daemonv1.AgentHealth_AGENT_HEALTH_OK,
		},
	}); err != nil {
		t.Fatalf("UpdateAgentStatus(stale) error = %v", err)
	}
	info = doGET(t, s, "/api/daemon/info", token)
	if err := json.Unmarshal(info.Body.Bytes(), &infoBody); err != nil {
		t.Fatalf("decode daemon info stale: %v", err)
	}
	if infoBody["health"] != "degraded" {
		t.Fatalf("daemon stale health = %+v, want degraded", infoBody)
	}

	if _, err := s.daemon.UpdateAgentStatus(context.Background(), &daemonv1.UpdateAgentStatusRequest{
		Status: &daemonv1.AgentStatusSnapshot{
			AgentId:       "agent-1",
			ComputerId:    "computer-1",
			Presence:      daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE,
			ActivityState: daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_CODING,
			Health:        daemonv1.AgentHealth_AGENT_HEALTH_TEST_FAILED,
		},
	}); err != nil {
		t.Fatalf("UpdateAgentStatus(degraded) error = %v", err)
	}
	info = doGET(t, s, "/api/daemon/info", token)
	if err := json.Unmarshal(info.Body.Bytes(), &infoBody); err != nil {
		t.Fatalf("decode daemon info degraded: %v", err)
	}
	if infoBody["health"] != "degraded" {
		t.Fatalf("daemon degraded health = %+v, want degraded", infoBody)
	}

	events := doGET(t, s, "/api/daemon/events?target=%23general", token)
	if events.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s", events.Code, events.Body.String())
	}
	assertJSONItems(t, events.Body.Bytes(), 1)
}

func TestServerEventsSSEBridge(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)
	if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
		Target:  "#general",
		AgentId: "agent-1",
		Kind:    "test_run",
		Summary: "stream test",
	}); err != nil {
		t.Fatalf("LogActivity() error = %v", err)
	}

	testServer := httptest.NewServer(s.Handler())
	t.Cleanup(testServer.Close)
	resp, err := testServer.Client().Get(testServer.URL + "/api/server-events?target=%23general&access_token=" + token)
	if err != nil {
		t.Fatalf("GET server events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("server events status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("server events stream closed before message event")
			}
			if line == "event: message" {
				return
			}
		case <-deadline:
			t.Fatal("server events stream did not emit a message event")
		}
	}
}

func TestServerEventsSSEGlobalStreamKeepsGlobalCursor(t *testing.T) {
	s := New(testConfig(), slog.New(slog.DiscardHandler), newTestStore(t))
	token := bootstrapToken(t, s)
	for _, target := range []string{"#general", "dm:agent-2"} {
		if _, err := s.daemon.LogActivity(context.Background(), &daemonv1.LogActivityRequest{
			Target:  target,
			AgentId: "agent-1",
			Kind:    "test_run",
			Summary: "global stream test",
		}); err != nil {
			t.Fatalf("LogActivity(%s) error = %v", target, err)
		}
	}

	testServer := httptest.NewServer(s.Handler())
	t.Cleanup(testServer.Close)
	resp, err := testServer.Client().Get(testServer.URL + "/api/server-events?limit=1&access_token=" + token)
	if err != nil {
		t.Fatalf("GET server events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("server events status = %d", resp.StatusCode)
	}
	events := readSSEMessages(t, resp, 2)
	got := []string{events[0].Target, events[1].Target}
	want := []string{"#general", "dm:agent-2"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("global SSE targets = %v, want %v", got, want)
	}
	if events[0].Sequence == 0 || events[1].Sequence <= events[0].Sequence {
		t.Fatalf("global SSE sequences = %+v, want increasing non-zero sequences", events)
	}
}

func testConfig() config.Config {
	return config.Config{
		Addr:     "127.0.0.1:0",
		GRPCAddr: "127.0.0.1:0",
		BaseURL:  "http://127.0.0.1",
		DataDir:  "/tmp/nekode-test",
		DBType:   "sqlite",
		DBDSN:    ":memory:",
	}
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), "file:"+storage.NewID("server_test")+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func bootstrapToken(t *testing.T, s *Server) string {
	t.Helper()
	resp := doJSON(t, s, http.MethodPost, "/api/auth/bootstrap", "", map[string]any{
		"username":    "admin",
		"password":    "secret123",
		"displayName": "Admin",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if body.Token == "" {
		t.Fatal("bootstrap token is empty")
	}
	return body.Token
}

func endpointBodyID(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var endpoint storage.InteractionEndpoint
	if err := json.Unmarshal(resp.Body.Bytes(), &endpoint); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	return endpoint.ID
}

func doJSON(t *testing.T, s *Server, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(resp, req)
	return resp
}

func doGET(t *testing.T, s *Server, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doGETWithToken(t, s, target, authorizationHeader(token))
}

func authorizationHeader(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func doGETWithToken(t *testing.T, s *Server, target, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	s.Handler().ServeHTTP(resp, req)
	return resp
}

func doJSONOrEmpty(t *testing.T, s *Server, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	if body == nil {
		return doGETWithToken(t, s, target, authorizationHeader(token))
	}
	return doJSON(t, s, method, target, token, body)
}

func doMultipartAttachment(
	t *testing.T,
	s *Server,
	token string,
	target string,
	filename string,
	contentType string,
	content string,
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("target", target); err != nil {
		t.Fatalf("write target field: %v", err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(resp, req)
	return resp
}

func assertJSONItems(t *testing.T, data []byte, want int) {
	t.Helper()
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(body.Items) != want {
		t.Fatalf("items = %d, want %d; body=%s", len(body.Items), want, string(data))
	}
}

func assertJSONItemsAtLeast(t *testing.T, data []byte, wantMin int) {
	t.Helper()
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(body.Items) < wantMin {
		t.Fatalf("items = %d, want at least %d; body=%s", len(body.Items), wantMin, string(data))
	}
}

type sseEvent struct {
	Target   string `json:"target"`
	Sequence int64  `json:"sequence"`
}

func readSSEMessages(t *testing.T, resp *http.Response, count int) []sseEvent {
	t.Helper()
	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.After(2 * time.Second)
	events := make([]sseEvent, 0, count)
	messageEvent := false
	for len(events) < count {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("server events stream closed after %d message events", len(events))
			}
			if line == "event: message" {
				messageEvent = true
				continue
			}
			if messageEvent && strings.HasPrefix(line, "data: ") {
				var event sseEvent
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
					t.Fatalf("decode SSE data %q: %v", line, err)
				}
				events = append(events, event)
				messageEvent = false
			}
		case <-deadline:
			t.Fatalf("server events stream emitted %d message events, want %d", len(events), count)
		}
	}
	return events
}
