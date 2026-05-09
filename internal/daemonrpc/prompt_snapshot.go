package daemonrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const launchPromptTemplateVersion = "nekode.launch-prompt.v1"

func (s *Server) GetLaunchPromptSnapshot(ctx context.Context, req *daemonv1.GetLaunchPromptSnapshotRequest) (*daemonv1.GetLaunchPromptSnapshotResponse, error) {
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	s.mu.Lock()
	run := s.runs[runID]
	if run == nil {
		s.mu.Unlock()
		return nil, status.Error(codes.NotFound, "run not found")
	}
	runCopy := proto.Clone(run).(*daemonv1.Run)
	if snapshotID := runCopy.GetLaunchPromptSnapshotId(); snapshotID != "" {
		if snapshot := s.promptSnaps[snapshotID]; snapshot != nil {
			s.mu.Unlock()
			return &daemonv1.GetLaunchPromptSnapshotResponse{Snapshot: proto.Clone(snapshot).(*daemonv1.LaunchPromptSnapshot)}, nil
		}
	}
	agentID := firstNonEmpty(req.GetAgentId(), runCopy.GetAgentId())
	computerID := firstNonEmpty(req.GetComputerId(), runCopy.GetComputerId())
	runtimeProfileID := firstNonEmpty(req.GetRuntimeProfileId(), runCopy.GetRuntimeProfileId())
	agent := s.agentProfileLocked(agentID)
	runtimeProfile := s.runtimeProfileLocked(computerID, runtimeProfileID)
	s.mu.Unlock()

	message, messageFound, err := s.promptMessage(ctx, runCopy)
	if err != nil {
		return nil, err
	}
	task, taskFound, err := s.promptTask(ctx, runCopy)
	if err != nil {
		return nil, err
	}

	snapshot := buildLaunchPromptSnapshot(launchPromptInput{
		serverID:       s.serverID,
		serverName:     s.serverName,
		run:            runCopy,
		agent:          agent,
		runtimeProfile: runtimeProfile,
		message:        message,
		messageFound:   messageFound,
		task:           task,
		taskFound:      taskFound,
	})
	s.mu.Lock()
	if current := s.runs[runID]; current != nil {
		current.LaunchPromptSnapshotId = snapshot.GetSnapshotId()
	}
	s.promptSnaps[snapshot.GetSnapshotId()] = proto.Clone(snapshot).(*daemonv1.LaunchPromptSnapshot)
	s.mu.Unlock()
	return &daemonv1.GetLaunchPromptSnapshotResponse{Snapshot: proto.Clone(snapshot).(*daemonv1.LaunchPromptSnapshot)}, nil
}

func (s *Server) runtimeProfileLocked(computerID string, runtimeProfileID string) *daemonv1.RuntimeProfile {
	for _, computer := range s.computers {
		if computer == nil || computer.inventory == nil {
			continue
		}
		if computerID != "" && computer.info != nil && computer.info.GetComputerId() != "" && computer.info.GetComputerId() != computerID {
			continue
		}
		for _, profile := range computer.inventory.GetRuntimeProfiles() {
			if profile.GetRuntimeProfileId() == runtimeProfileID {
				return proto.Clone(profile).(*daemonv1.RuntimeProfile)
			}
		}
	}
	return nil
}

func (s *Server) promptMessage(ctx context.Context, run *daemonv1.Run) (storage.Message, bool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(run.GetInputMessageId()) == "" {
		return storage.Message{}, false, nil
	}
	msg, err := s.store.GetMessage(ctx, run.GetTarget(), run.GetInputMessageId())
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Message{}, false, nil
	}
	if err != nil {
		return storage.Message{}, false, status.Errorf(codes.Internal, "load prompt message: %v", err)
	}
	return msg, true, nil
}

func (s *Server) promptTask(ctx context.Context, run *daemonv1.Run) (storage.Task, bool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(run.GetTaskId()) == "" {
		return storage.Task{}, false, nil
	}
	task, err := s.store.GetTask(ctx, run.GetTaskId())
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Task{}, false, nil
	}
	if err != nil {
		return storage.Task{}, false, status.Errorf(codes.Internal, "load prompt task: %v", err)
	}
	return task, true, nil
}

type launchPromptInput struct {
	serverID       string
	serverName     string
	run            *daemonv1.Run
	agent          *daemonv1.AgentProfile
	runtimeProfile *daemonv1.RuntimeProfile
	message        storage.Message
	messageFound   bool
	task           storage.Task
	taskFound      bool
}

func buildLaunchPromptSnapshot(input launchPromptInput) *daemonv1.LaunchPromptSnapshot {
	run := input.run
	agent := input.agent
	if agent == nil {
		agent = &daemonv1.AgentProfile{AgentId: run.GetAgentId(), Name: run.GetAgentId(), DisplayName: run.GetAgentId()}
	}
	sections := []*daemonv1.LaunchPromptSnapshotSection{
		promptSection("agent_identity", "agent_profile", agentIdentityPrompt(agent, input.serverName), false),
		promptSection("run_context", "run", runContextPrompt(run), false),
	}
	redactions := []string{}
	if input.runtimeProfile != nil {
		content, redacted := runtimeProfilePrompt(input.runtimeProfile)
		sections = append(sections, promptSection("runtime_profile", "runtime_profile.adapter_config", content, redacted))
		if redacted {
			redactions = append(redactions, "runtime_profile.adapter_config")
		}
	}
	if input.taskFound {
		sections = append(sections, promptSection("task_context", "task", taskPrompt(input.task), false))
	}
	if input.messageFound {
		content, redacted := messagePrompt(input.message)
		sections = append(sections, promptSection("message_context", "message", content, redacted))
		if redacted {
			redactions = append(redactions, "message.metadata")
		}
	}
	sections = append(sections,
		promptSection("communication_protocol", "nekode.prompt_template", communicationPrompt(), false),
		promptSection("execution_verification", "nekode.prompt_template", executionVerificationPrompt(), false),
		promptSection("tools_permissions_skills", "runtime_profile.capabilities", toolsPrompt(agent, input.runtimeProfile), false),
		promptSection("memory_context", "server_memory_summary", memoryPrompt(), false),
		promptSection("safety_audit", "nekode.prompt_template", safetyPrompt(), false),
	)

	content := renderPromptSections(sections)
	hash := sha256.Sum256([]byte(content))
	snapshotID := fmt.Sprintf("prompt_%s_%s", strings.TrimPrefix(run.GetRunId(), "run_"), hex.EncodeToString(hash[:])[:12])
	if run.GetLaunchPromptSnapshotId() != "" {
		snapshotID = run.GetLaunchPromptSnapshotId()
	}
	return &daemonv1.LaunchPromptSnapshot{
		SnapshotId:       snapshotID,
		AgentId:          firstNonEmpty(agent.GetAgentId(), run.GetAgentId()),
		ComputerId:       run.GetComputerId(),
		RunId:            run.GetRunId(),
		RuntimeProfileId: firstNonEmpty(run.GetRuntimeProfileId(), agent.GetRuntimeProfileId()),
		Target:           run.GetTarget(),
		ThreadId:         input.message.ThreadID,
		MessageId:        run.GetInputMessageId(),
		TaskId:           run.GetTaskId(),
		TemplateVersion:  launchPromptTemplateVersion,
		Content:          content,
		ContentHash:      hex.EncodeToString(hash[:]),
		Sections:         sections,
		CreatedTimeUnix:  unixNow(),
		RedactionSummary: redactionSummary(redactions),
		SourceVersion:    input.serverID + ":" + launchPromptTemplateVersion,
	}
}

func promptSection(name string, source string, content string, redacted bool) *daemonv1.LaunchPromptSnapshotSection {
	return &daemonv1.LaunchPromptSnapshotSection{
		Name:     name,
		Source:   source,
		Content:  strings.TrimSpace(content),
		Redacted: redacted,
	}
}

func agentIdentityPrompt(agent *daemonv1.AgentProfile, serverName string) string {
	return strings.Join(nonEmptyLines(
		"You are a Nekode daemon-managed agent.",
		"server: "+firstNonEmpty(serverName, "Nekode"),
		"agent_id: "+agent.GetAgentId(),
		"name: "+firstNonEmpty(agent.GetName(), agent.GetAgentId()),
		"display_name: "+firstNonEmpty(agent.GetDisplayName(), agent.GetName(), agent.GetAgentId()),
		"runtime_kind: "+agent.GetRuntimeKind(),
		"provider: "+agent.GetProvider(),
		"model: "+agent.GetModel(),
	), "\n")
}

func runContextPrompt(run *daemonv1.Run) string {
	return strings.Join(nonEmptyLines(
		"current_run:",
		"- run_id: "+run.GetRunId(),
		"- target: "+run.GetTarget(),
		"- task_id: "+run.GetTaskId(),
		"- input_message_id: "+run.GetInputMessageId(),
		"- attempt: "+fmt.Sprint(run.GetAttempt()),
		"- objective: "+run.GetSummary(),
	), "\n")
}

func runtimeProfilePrompt(profile *daemonv1.RuntimeProfile) (string, bool) {
	lines := nonEmptyLines(
		"runtime_profile:",
		"- runtime_profile_id: "+profile.GetRuntimeProfileId(),
		"- kind: "+profile.GetKind(),
		"- provider: "+profile.GetProvider(),
		"- model: "+profile.GetModel(),
		"- workspace_id: "+profile.GetWorkspaceId(),
	)
	selected, redacted := selectedRuntimeOptions(profile.GetAdapterConfigJson())
	if len(selected) > 0 {
		lines = append(lines, "- selected_options:")
		for _, key := range sortedKeys(selected) {
			value := selected[key]
			if key == "system_message" {
				lines = append(lines, "  - system_message: "+value)
				continue
			}
			lines = append(lines, "  - "+key+": "+value)
		}
	}
	if len(profile.GetEnv()) > 0 {
		lines = append(lines, "- env: "+fmt.Sprintf("%d variables reported; values omitted", len(profile.GetEnv())))
		redacted = true
	}
	return strings.Join(lines, "\n"), redacted
}

func selectedRuntimeOptions(configJSON string) (map[string]string, bool) {
	var payload struct {
		SelectedOptions map[string]string `json:"selectedOptions"`
	}
	if strings.TrimSpace(configJSON) == "" || json.Unmarshal([]byte(configJSON), &payload) != nil {
		return nil, false
	}
	out := make(map[string]string, len(payload.SelectedOptions))
	redacted := false
	for key, value := range payload.SelectedOptions {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" {
			continue
		}
		cleanValue, didRedact := redactPromptValue(cleanKey, value)
		if didRedact {
			redacted = true
		}
		out[cleanKey] = cleanValue
	}
	return out, redacted
}

func taskPrompt(task storage.Task) string {
	return strings.Join(nonEmptyLines(
		"task:",
		"- task_id: "+task.ID,
		"- target: "+task.Target,
		"- state: "+task.State,
		"- assignee_id: "+task.AssigneeID,
		"- summary: "+task.Summary,
		"- description: "+truncatePromptText(task.Description, 1200),
	), "\n")
}

func messagePrompt(msg storage.Message) (string, bool) {
	metadata, redacted := redactJSONLike("metadata_json", msg.MetadataJSON)
	return strings.Join(nonEmptyLines(
		"input_message:",
		"- message_id: "+msg.ID,
		"- target: "+msg.Target,
		"- thread_id: "+msg.ThreadID,
		"- reply_target_hint: "+replyTargetHint(msg.Target, msg.ThreadID),
		"- role: "+firstNonEmpty(msg.Role, "user"),
		"- sender: "+firstNonEmpty(msg.SenderDisplayName, msg.SenderAgentID, msg.SenderUserID),
		"- content: "+truncatePromptText(msg.Content, 1800),
		"- metadata_json: "+metadata,
	), "\n"), redacted
}

func replyTargetHint(target, threadID string) string {
	target = strings.TrimSpace(target)
	threadID = strings.TrimSpace(threadID)
	if target == "" || threadID == "" {
		return target
	}
	return target + ":" + threadID
}

func communicationPrompt() string {
	return strings.Join([]string{
		"communication_protocol:",
		"- Treat the current run objective and input message as the immediate task.",
		"- When message_context includes reply_target_hint, use that exact target for replies instead of reconstructing a channel, DM, or thread target from message text.",
		"- Use Nekode task/message/status APIs when reporting progress.",
		"- Keep task state, direct messages, and channel updates consistent with the server state.",
		"- Do not mention yourself to ask whether you have started or to create a self-reminder; after an assignment, claim the task and report real progress, claim failure, or a concrete blocker.",
		"- Do not send empty coordination/status messages without new execution evidence or actionable handoff information.",
		"- Do not claim provider runtime support unless live receive/auth/send smoke has passed or the task is explicitly feasibility-only.",
	}, "\n")
}

func executionVerificationPrompt() string {
	return strings.Join([]string{
		"execution_verification:",
		"- For long-running work, keep a short execution plan and update it when scope or ownership changes.",
		"- Split independent work into claimable subtasks when it improves throughput; avoid artificial sequential chains.",
		"- Treat acceptance criteria as default-failing until you have opened or observed concrete evidence.",
		"- Persist progress, decisions, blockers, and handoff notes in server-visible task, message, status, or activity surfaces rather than only local context.",
		"- Use a fresh reviewer or narrowly scoped verification pass for substantial claims when available.",
		"- Do not stop at analysis or half-finished implementation while a recoverable path remains.",
		"- Before finishing, run the checks that prove the claim and report the evidence or the exact remaining gap.",
	}, "\n")
}

func toolsPrompt(agent *daemonv1.AgentProfile, profile *daemonv1.RuntimeProfile) string {
	names := []string{}
	for _, capability := range agent.GetCapabilities() {
		if capability.GetEnabled() || capability.GetName() != "" {
			names = append(names, capability.GetName())
		}
	}
	if profile != nil {
		for _, capability := range profile.GetCapabilities() {
			if capability.GetEnabled() || capability.GetName() != "" {
				names = append(names, capability.GetName())
			}
		}
		if len(profile.GetSkills()) > 0 {
			names = append(names, fmt.Sprintf("%d runtime skills indexed", len(profile.GetSkills())))
		}
	}
	sort.Strings(names)
	names = compactStrings(names)
	if len(names) == 0 {
		names = []string{"runtime default tool policy"}
	}
	return "tools_permissions_skills:\n- " + strings.Join(names, "\n- ")
}

func memoryPrompt() string {
	return strings.Join([]string{
		"memory_context:",
		"- Server prompt snapshots include only compact, launch-relevant context.",
		"- Long history and large memory records should stay retrievable by tools rather than being copied into every launch prompt.",
	}, "\n")
}

func safetyPrompt() string {
	return strings.Join([]string{
		"safety_redaction_audit:",
		"- Sensitive values such as tokens, secrets, passwords, cookies, and API keys are redacted before injection.",
		"- The daemon logs only snapshot id, hash, template version, and section names; it must not log full prompt content.",
	}, "\n")
}

func renderPromptSections(sections []*daemonv1.LaunchPromptSnapshotSection) string {
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section.GetContent()) == "" {
			continue
		}
		parts = append(parts, "## "+section.GetName()+"\n"+section.GetContent())
	}
	return strings.Join(parts, "\n\n")
}

func redactPromptValue(key string, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if isSensitiveKey(key) || value == "<redacted>" {
		return "<redacted>", true
	}
	return truncatePromptText(value, 1200), false
}

func redactJSONLike(key string, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	redacted := false
	var payload map[string]any
	if json.Unmarshal([]byte(value), &payload) == nil {
		for k := range payload {
			if isSensitiveKey(k) {
				payload[k] = "<redacted>"
				redacted = true
			}
		}
		out, err := json.Marshal(payload)
		if err == nil {
			return truncatePromptText(string(out), 1200), redacted
		}
	}
	if isSensitiveKey(key) || looksSensitive(value) {
		return "<redacted>", true
	}
	return truncatePromptText(value, 1200), redacted
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "authorization", "cookie", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func looksSensitive(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "token") || strings.Contains(value, "secret") || strings.Contains(value, "password") || strings.Contains(value, "api_key")
}

func truncatePromptText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit < 20 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-15]) + " ...<truncated>"
}

func nonEmptyLines(lines ...string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" && !strings.HasSuffix(strings.TrimSpace(line), ": ") {
			out = append(out, line)
		}
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func redactionSummary(redactions []string) string {
	redactions = compactStrings(redactions)
	if len(redactions) == 0 {
		return "no sensitive values detected"
	}
	sort.Strings(redactions)
	return "redacted: " + strings.Join(redactions, ", ")
}
