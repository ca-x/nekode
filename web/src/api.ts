import type {
  AgentRun,
  AgentRunEvent,
  AgentRunPhase,
  AgentRunSearchHit,
  Attachment,
  AuthResponse,
  AgentControlAction,
  AgentControlResult,
  AgentDirectMessageResult,
  AgentStatusSnapshot,
  Channel,
  ChannelDecision,
  ChannelDecisionVote,
  ChannelMember,
  ChannelMemberRole,
  ChannelVisibility,
  CollaborationEvent,
  CollaborationEventKind,
  CreateDaemonAgentInput,
  CreateDaemonAgentResult,
  DaemonAgentInstance,
  DaemonEnrollment,
  DaemonActivityRecord,
  DaemonInfo,
  DaemonInventoryComputer,
  DaemonInventoryRuntime,
  DaemonRun,
  DecisionActorKind,
  DecisionStatus,
  DecisionVote,
  EventCursor,
  EventOperation,
  EventScope,
  EventScopeType,
  InteractionEndpoint,
  IMChatAuthDecisionResult,
  IMChatAuthRequest,
  IMChatSubscription,
  IMProviderSchema,
  IMBindingSession,
  JsonObject,
  Message,
  MessageKind,
  NotificationRoute,
  InteractionEndpointTestResult,
  ProtocolInfo,
  Reminder,
  ReminderEvent,
  ReminderScheduleKind,
  ReminderStatus,
  RuntimePreset,
  RuntimeInstanceTemplate,
  RuntimeOptionSchema,
  RuntimeTypeInventory,
  SavedMessage,
  SetupStatus,
  Task,
  TaskState,
  ThreadInboxItem,
  TunnelAccessPolicy,
  TunnelRecord,
  TunnelState,
  User
} from "./types";

type ListResponse<T> = {
  items: T[];
};

export type ServerEventMessage<T = unknown> = {
  id?: string;
  event?: string;
  data: T;
};

type RawRecord = Record<string, unknown>;

type RawListResponse<T> = {
  items?: T[];
};

type RawEventCursor = {
  cursor?: unknown;
  target?: unknown;
  protocolVersion?: unknown;
  protocol_version?: unknown;
  snapshotId?: unknown;
  snapshot_id?: unknown;
  sequence?: unknown;
  aggregateId?: unknown;
  aggregate_id?: unknown;
  serverId?: unknown;
  server_id?: unknown;
};

type RawEventScope = {
  scopeType?: unknown;
  scope_type?: unknown;
  scopeId?: unknown;
  scope_id?: unknown;
  target?: unknown;
  customType?: unknown;
  custom_type?: unknown;
};

type RawCollaborationEvent = {
  eventId?: unknown;
  event_id?: unknown;
  target?: unknown;
  kind?: unknown;
  operation?: unknown;
  scope?: RawEventScope | unknown;
  workspaceId?: unknown;
  workspace_id?: unknown;
  sequence?: unknown;
  aggregateId?: unknown;
  aggregate_id?: unknown;
  protocolVersion?: unknown;
  protocol_version?: unknown;
  messageId?: unknown;
  message_id?: unknown;
  activityId?: unknown;
  activity_id?: unknown;
  taskId?: unknown;
  task_id?: unknown;
  runId?: unknown;
  run_id?: unknown;
  sourceEndpointId?: unknown;
  source_endpoint_id?: unknown;
  createdTimeUnix?: unknown;
  created_time_unix?: unknown;
  payload?: unknown;
  message?: unknown;
  activity?: unknown;
  task?: unknown;
  run?: unknown;
  runStep?: unknown;
  run_step?: unknown;
};

type RawMessage = {
  id?: unknown;
  messageId?: unknown;
  message_id?: unknown;
  target?: unknown;
  threadId?: unknown;
  thread_id?: unknown;
  role?: unknown;
  content?: unknown;
  replyToMessageId?: unknown;
  reply_to_message_id?: unknown;
  senderUserId?: unknown;
  sender_user_id?: unknown;
  senderAgentId?: unknown;
  sender_agent_id?: unknown;
  senderDisplayName?: unknown;
  sender_display_name?: unknown;
  senderKind?: unknown;
  sender_kind?: unknown;
  sourceEndpointId?: unknown;
  source_endpoint_id?: unknown;
  externalMessageId?: unknown;
  external_message_id?: unknown;
  metadataJson?: unknown;
  metadata_json?: unknown;
  attachments?: unknown;
  requestId?: unknown;
  request_id?: unknown;
  createdUnix?: unknown;
  created_time_unix?: unknown;
  createdTimeUnix?: unknown;
  kind?: unknown;
};

type RawDaemonEnrollment = {
  id?: unknown;
  tokenPrefix?: unknown;
  token_prefix?: unknown;
  token?: unknown;
  installCommand?: unknown;
  install_command?: unknown;
  installScriptUrl?: unknown;
  install_script_url?: unknown;
  statusUrl?: unknown;
  status_url?: unknown;
  displayName?: unknown;
  display_name?: unknown;
  computerId?: unknown;
  computer_id?: unknown;
  daemonId?: unknown;
  daemon_id?: unknown;
  hostname?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  createdTimeUnix?: unknown;
  created_time_unix?: unknown;
  expiresUnix?: unknown;
  expires_unix?: unknown;
  expiresTimeUnix?: unknown;
  expires_time_unix?: unknown;
  connectedUnix?: unknown;
  connected_unix?: unknown;
  connectedTimeUnix?: unknown;
  connected_time_unix?: unknown;
  lastHeartbeatUnix?: unknown;
  last_heartbeat_unix?: unknown;
  lastHeartbeatTimeUnix?: unknown;
  last_heartbeat_time_unix?: unknown;
  revokedUnix?: unknown;
  revoked_unix?: unknown;
  revokedTimeUnix?: unknown;
  revoked_time_unix?: unknown;
  status?: unknown;
};

type RawSavedMessage = {
  id?: unknown;
  savedMessageId?: unknown;
  saved_message_id?: unknown;
  target?: unknown;
  messageId?: unknown;
  message_id?: unknown;
  savedByUserId?: unknown;
  saved_by_user_id?: unknown;
  savedByAgentId?: unknown;
  saved_by_agent_id?: unknown;
  createdUnix?: unknown;
  savedTimeUnix?: unknown;
  saved_time_unix?: unknown;
  message?: unknown;
};

type RawInteractionEndpoint = {
  id?: unknown;
  kind?: unknown;
  provider?: unknown;
  displayName?: unknown;
  display_name?: unknown;
  targetPrefix?: unknown;
  target_prefix?: unknown;
  inboundEnabled?: unknown;
  inbound_enabled?: unknown;
  outboundEnabled?: unknown;
  outbound_enabled?: unknown;
  authMode?: unknown;
  auth_mode?: unknown;
  configJson?: unknown;
  config_json?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  updatedUnix?: unknown;
  updated_unix?: unknown;
};

type RawIMBindingSession = {
  id?: unknown;
  endpointId?: unknown;
  endpoint_id?: unknown;
  provider?: unknown;
  method?: unknown;
  status?: unknown;
  qrPayload?: unknown;
  qr_payload?: unknown;
  qrImageUrl?: unknown;
  qr_image_url?: unknown;
  expiresUnix?: unknown;
  expires_unix?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  updatedUnix?: unknown;
  updated_unix?: unknown;
  detail?: unknown;
};

type RawIMChatAuthRequest = {
  id?: unknown;
  endpointId?: unknown;
  endpoint_id?: unknown;
  provider?: unknown;
  conversationId?: unknown;
  conversation_id?: unknown;
  externalThreadId?: unknown;
  external_thread_id?: unknown;
  chatTitle?: unknown;
  chat_title?: unknown;
  senderExternalId?: unknown;
  sender_external_id?: unknown;
  tokenPrefix?: unknown;
  token_prefix?: unknown;
  status?: unknown;
  requestedTarget?: unknown;
  requested_target?: unknown;
  requestedThreadId?: unknown;
  requested_thread_id?: unknown;
  expiresUnix?: unknown;
  expires_unix?: unknown;
  resolvedByUserId?: unknown;
  resolved_by_user_id?: unknown;
  resolvedUnix?: unknown;
  resolved_unix?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  updatedUnix?: unknown;
  updated_unix?: unknown;
};

type RawIMChatSubscription = {
  id?: unknown;
  endpointId?: unknown;
  endpoint_id?: unknown;
  provider?: unknown;
  conversationId?: unknown;
  conversation_id?: unknown;
  externalThreadId?: unknown;
  external_thread_id?: unknown;
  chatTitle?: unknown;
  chat_title?: unknown;
  target?: unknown;
  threadId?: unknown;
  thread_id?: unknown;
  senderExternalId?: unknown;
  sender_external_id?: unknown;
  authorizedByRequestId?: unknown;
  authorized_by_request_id?: unknown;
  subscribed?: unknown;
  verbose?: unknown;
  authorizedUnix?: unknown;
  authorized_unix?: unknown;
  subscribedUnix?: unknown;
  subscribed_unix?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  updatedUnix?: unknown;
  updated_unix?: unknown;
};

type RawNotificationRoute = {
  id?: unknown;
  target?: unknown;
  threadId?: unknown;
  thread_id?: unknown;
  endpointId?: unknown;
  endpoint_id?: unknown;
  eventKind?: unknown;
  event_kind?: unknown;
  preference?: unknown;
  enabled?: unknown;
  configJson?: unknown;
  config_json?: unknown;
  createdUnix?: unknown;
  created_unix?: unknown;
  updatedUnix?: unknown;
  updated_unix?: unknown;
};

type EventTransport = {
  close: () => void;
};

export type SubscribeServerEventsOptions = {
  token?: string;
  cursor?: string;
  sequence?: number;
  target?: string;
  aggregateId?: string;
  limit?: number;
  onEvent: (message: ServerEventMessage<CollaborationEvent>) => void;
  onError?: (error: Event) => void;
};

const taskStateValues = new Set<TaskState>([
  "todo",
  "in_progress",
  "blocked",
  "in_review",
  "done",
  "canceled"
]);
const channelVisibilityValues = new Set<ChannelVisibility>(["public", "private", "unspecified"]);
const channelMemberRoleValues = new Set<ChannelMemberRole>(["admin", "member", "viewer", "unspecified"]);
const reminderStatusValues = new Set<ReminderStatus>(["active", "done", "canceled", "paused", "failed", "unspecified"]);
const reminderScheduleKindValues = new Set<ReminderScheduleKind>(["cron", "every", "at", "rrule", "natural", "unspecified"]);

const collaborationEventKindByNumber: Record<number, CollaborationEventKind> = {
  0: "unspecified",
  1: "message",
  2: "activity",
  3: "task",
  4: "reminder",
  5: "coordination",
  6: "memory",
  7: "run",
  8: "run_step",
  9: "attachment",
  10: "ping"
};

const eventOperationByNumber: Record<number, EventOperation> = {
  0: "unspecified",
  1: "created",
  2: "updated",
  3: "deleted",
  4: "state_changed",
  5: "appended",
  6: "claimed",
  7: "released",
  8: "failed",
  9: "canceled",
  10: "heartbeat",
  11: "invalidated",
  12: "snapshot"
};

const eventScopeTypeByNumber: Record<number, EventScopeType> = {
  0: "unspecified",
  1: "server",
  2: "workspace",
  3: "target",
  4: "thread",
  5: "task",
  6: "run",
  7: "agent",
  8: "computer",
  9: "user",
  10: "endpoint",
  11: "daemon",
  12: "custom"
};

const agentPresenceByNumber: Record<number, string> = {
  0: "unspecified",
  1: "online",
  2: "idle",
  3: "busy",
  4: "sleeping",
  5: "stale",
  6: "offline",
  7: "degraded"
};

const agentActivityStateByNumber: Record<number, string> = {
  0: "unspecified",
  1: "receiving_message",
  2: "reading_context",
  3: "compacting_context",
  4: "thinking",
  5: "coding",
  6: "running_command",
  7: "running_test",
  8: "reviewing",
  9: "waiting",
  10: "blocked",
  11: "restarting",
  12: "restoring_memory"
};

const agentHealthByNumber: Record<number, string> = {
  0: "unspecified",
  1: "ok",
  2: "provider_quota",
  3: "command_failed",
  4: "test_failed",
  5: "auth_required",
  6: "runtime_error",
  7: "offline"
};

const agentSeverityByNumber: Record<number, string> = {
  0: "unspecified",
  1: "info",
  2: "warning",
  3: "error"
};

const agentControlActionByNumber: Record<number, string> = {
  0: "unspecified",
  1: "terminate",
  2: "restart",
  3: "restart_reset_session",
  4: "restart_full_reset",
  5: "custom"
};

const agentControlStateByNumber: Record<number, string> = {
  0: "unspecified",
  1: "requested",
  2: "queued",
  3: "running",
  4: "completed",
  5: "failed",
  6: "unsupported"
};

const runStateByNumber: Record<number, string> = {
  0: "unspecified",
  1: "queued",
  2: "running",
  3: "blocked",
  4: "completed",
  5: "failed",
  6: "canceled"
};

function asRecord(value: unknown): RawRecord {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as RawRecord) : {};
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asOptionalString(value: unknown): string | undefined {
  const stringValue = asString(value).trim();
  return stringValue || undefined;
}

function asNumber(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function enumLabel(value: unknown): string {
  return asString(value)
    .toLowerCase()
    .replace(/^(collaboration_event_kind|event_operation|event_scope_type|task_state|reminder_status|reminder_schedule_kind|reminder_event_type|reminder_actor_type)_/, "");
}

function knownEnumLabel(value: unknown, byNumber: Record<number, string>, prefix: string): string {
  if (typeof value === "number") return byNumber[value] ?? "unspecified";
  return enumLabel(value).replace(new RegExp(`^${prefix}_`), "") || "unspecified";
}

export function normalizeTaskState(value: unknown): TaskState {
  if (typeof value === "number") {
    return (
      ({
        1: "todo",
        2: "in_progress",
        3: "in_review",
        4: "done",
        5: "blocked",
        6: "canceled"
      } as Record<number, TaskState>)[value] ?? "todo"
    );
  }
  const label = enumLabel(value);
  const normalized = label === "cancelled" ? "canceled" : label;
  return taskStateValues.has(normalized as TaskState) ? (normalized as TaskState) : "todo";
}

function normalizeTask(raw: unknown): Task {
  const row = asRecord(raw);
  return {
    id: asString(row.id ?? row.taskId ?? row.task_id),
    summary: asString(row.summary),
    description: asOptionalString(row.description),
    state: normalizeTaskState(row.state ?? row.boardColumn ?? row.board_column),
    target: asString(row.target),
    assigneeId: asOptionalString(row.assigneeId ?? row.assignee_id),
    createdByUserId: asOptionalString(row.createdByUserId ?? row.created_by_user_id),
    blockedReason: asOptionalString(row.blockedReason ?? row.blocked_reason),
    version: asNumber(row.version) || undefined,
    claimLeaseId: asOptionalString(row.claimLeaseId ?? row.claim_lease_id),
    createdUnix: asNumber(row.createdUnix ?? row.created_time_unix ?? row.createdTimeUnix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_time_unix ?? row.updatedTimeUnix)
  };
}

function normalizeAttachment(raw: unknown): Attachment {
  const row = asRecord(raw);
  const id = asString(row.id ?? row.attachmentId ?? row.attachment_id);
  return {
    id,
    target: asString(row.target),
    ownerId: asOptionalString(row.ownerId ?? row.owner_id),
    filename: asString(row.filename) || "attachment",
    mimeType: asString(row.mimeType ?? row.mime_type) || "application/octet-stream",
    sizeBytes: asNumber(row.sizeBytes ?? row.size_bytes),
    storageRef: asOptionalString(row.storageRef ?? row.storage_ref),
    downloadUrl: asString(row.downloadUrl ?? row.download_url) || `/api/attachments/${encodeURIComponent(id)}/content`,
    uploadUrl: asOptionalString(row.uploadUrl ?? row.upload_url),
    expiresTimeUnix: asNumber(row.expiresTimeUnix ?? row.expires_time_unix) || undefined,
    createdUnix: asNumber(row.createdUnix ?? row.created_time_unix ?? row.createdTimeUnix)
  };
}

function normalizeMessage(raw: unknown): Message {
  const row = asRecord(raw) as RawMessage;
  return {
    id: asString(row.id ?? row.messageId ?? row.message_id),
    target: asString(row.target),
    threadId: asOptionalString(row.threadId ?? row.thread_id),
    role: asString(row.role) || "user",
    content: asString(row.content),
    replyToMessageId: asOptionalString(row.replyToMessageId ?? row.reply_to_message_id),
    senderUserId: asOptionalString(row.senderUserId ?? row.sender_user_id),
    senderAgentId: asOptionalString(row.senderAgentId ?? row.sender_agent_id),
    senderDisplayName: asOptionalString(row.senderDisplayName ?? row.sender_display_name),
    senderKind: asString(row.senderKind ?? row.sender_kind) || "user",
    sourceEndpointId: asOptionalString(row.sourceEndpointId ?? row.source_endpoint_id),
    externalMessageId: asOptionalString(row.externalMessageId ?? row.external_message_id),
    metadataJson: asOptionalString(row.metadataJson ?? row.metadata_json),
    attachments: Array.isArray(row.attachments) ? row.attachments.map(normalizeAttachment) : undefined,
    requestId: asOptionalString(row.requestId ?? row.request_id),
    createdUnix: asNumber(row.createdUnix ?? row.created_time_unix ?? row.createdTimeUnix),
    kind: (asString(row.kind) || "") as MessageKind
  };
}

function normalizeChannelVisibility(value: unknown): ChannelVisibility | string {
  const label = enumLabel(value).replace(/^channel_visibility_/, "") || "unspecified";
  return channelVisibilityValues.has(label as ChannelVisibility) ? (label as ChannelVisibility) : label;
}

function normalizeChannelMemberRole(value: unknown): ChannelMemberRole | string {
  const label = enumLabel(value).replace(/^channel_member_role_/, "") || "unspecified";
  return channelMemberRoleValues.has(label as ChannelMemberRole) ? (label as ChannelMemberRole) : label;
}

function normalizeChannel(raw: unknown): Channel {
  const row = asRecord(raw);
  return {
    target: asString(row.target),
    displayName: asString(row.displayName ?? row.display_name) || asString(row.target),
    channelType: asString(row.channelType ?? row.channel_type) || "channel",
    visibility: normalizeChannelVisibility(row.visibility),
    joined: Boolean(row.joined),
    memberCount: asNumber(row.memberCount ?? row.member_count),
    currentUserRole: normalizeChannelMemberRole(row.currentUserRole ?? row.current_user_role),
    createdByUserId: asOptionalString(row.createdByUserId ?? row.created_by_user_id),
    createdUnix: asNumber(row.createdUnix ?? row.created_unix) || undefined,
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix) || undefined
  };
}

function normalizeChannelMember(raw: unknown): ChannelMember {
  const row = asRecord(raw);
  const member = asRecord(row.member);
  return {
    target: asString(row.target),
    memberId: asString(row.memberId ?? row.member_id ?? member.id ?? member.actorId ?? member.actor_id),
    username: asOptionalString(row.username ?? member.username ?? member.handle),
    displayName:
      asString(row.displayName ?? row.display_name ?? member.displayName ?? member.display_name ?? member.name) ||
      "Member",
    kind: asString(row.kind ?? member.kind ?? member.actorKind ?? member.actor_kind) || "human",
    role: normalizeChannelMemberRole(row.role),
    joinedTimeUnix: asNumber(row.joinedTimeUnix ?? row.joined_time_unix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix) || undefined
  };
}

function normalizeReminderStatus(value: unknown): ReminderStatus | string {
  if (typeof value === "number") {
    return (
      ({
        1: "active",
        2: "done",
        3: "canceled",
        4: "paused",
        5: "failed"
      } as Record<number, ReminderStatus>)[value] ?? "unspecified"
    );
  }
  const label = enumLabel(value).replace(/^reminder_status_/, "") || "unspecified";
  const normalized = label === "cancelled" ? "canceled" : label;
  return reminderStatusValues.has(normalized as ReminderStatus) ? (normalized as ReminderStatus) : normalized;
}

function normalizeReminderScheduleKind(value: unknown): ReminderScheduleKind | string {
  if (typeof value === "number") {
    return (
      ({
        1: "cron",
        2: "every",
        3: "at",
        4: "rrule",
        5: "natural"
      } as Record<number, ReminderScheduleKind>)[value] ?? "unspecified"
    );
  }
  const label = enumLabel(value).replace(/^reminder_schedule_kind_/, "") || "unspecified";
  return reminderScheduleKindValues.has(label as ReminderScheduleKind) ? (label as ReminderScheduleKind) : label;
}

function normalizeDecision(raw: unknown): ChannelDecision {
  const row = asRecord(raw);
  return {
    id: asString(row.id ?? row.decisionId),
    target: asString(row.target),
    title: asString(row.title),
    body: asString(row.body),
    status: (asString(row.status) || "proposed") as DecisionStatus,
    proposerId: asString(row.proposerId ?? row.proposer_id),
    proposerKind: (asString(row.proposerKind ?? row.proposer_kind) || "human") as DecisionActorKind,
    createdUnix: asNumber(row.createdUnix ?? row.created_unix),
    ratifiedUnix: asNumber(row.ratifiedUnix ?? row.ratified_unix),
    retiredUnix: asNumber(row.retiredUnix ?? row.retired_unix),
    retiredBy: asOptionalString(row.retiredBy ?? row.retired_by),
    retireReason: asOptionalString(row.retireReason ?? row.retire_reason),
    supersedesDecisionId: asOptionalString(row.supersedesDecisionId ?? row.supersedes_decision_id),
    approveCount: asNumber(row.approveCount ?? row.approve_count),
    rejectCount: asNumber(row.rejectCount ?? row.reject_count),
    abstainCount: asNumber(row.abstainCount ?? row.abstain_count)
  };
}

function normalizeDecisionVote(raw: unknown): ChannelDecisionVote {
  const row = asRecord(raw);
  return {
    id: asString(row.id),
    decisionId: asString(row.decisionId ?? row.decision_id),
    voterId: asString(row.voterId ?? row.voter_id),
    voterKind: (asString(row.voterKind ?? row.voter_kind) || "human") as DecisionActorKind,
    decision: (asString(row.decision) || "abstain") as DecisionVote,
    votedUnix: asNumber(row.votedUnix ?? row.voted_unix),
    reason: asOptionalString(row.reason)
  };
}

function normalizeAgentRun(raw: unknown): AgentRun {
  const row = asRecord(raw);
  return {
    id: asString(row.id ?? row.runId ?? row.run_id),
    agentId: asString(row.agentId ?? row.agent_id),
    computerId: asString(row.computerId ?? row.computer_id),
    startedUnix: asNumber(row.startedUnix ?? row.started_unix),
    endedUnix: asNumber(row.endedUnix ?? row.ended_unix),
    exitCode: asNumber(row.exitCode ?? row.exit_code),
    summary: asOptionalString(row.summary) ?? "",
    error: asOptionalString(row.error) ?? "",
    eventCount: asNumber(row.eventCount ?? row.event_count)
  };
}

function normalizeAgentRunEvent(raw: unknown): AgentRunEvent {
  const row = asRecord(raw);
  return {
    id: asString(row.id),
    runId: asString(row.runId ?? row.run_id),
    atUnixNano: asNumber(row.atUnixNano ?? row.at_unix_nano),
    phase: (asString(row.phase) || "output") as AgentRunPhase,
    summary: asOptionalString(row.summary) ?? "",
    payloadJson: asOptionalString(row.payloadJson ?? row.payload_json) ?? "",
    exitCode: asNumber(row.exitCode ?? row.exit_code),
    errorMessage: asOptionalString(row.errorMessage ?? row.error_message) ?? ""
  };
}

function normalizeAgentRunHit(raw: unknown): AgentRunSearchHit {
  const row = asRecord(raw);
  return {
    run: normalizeAgentRun(row.run),
    event: normalizeAgentRunEvent(row.event),
    highlight: asOptionalString(row.highlight) ?? ""
  };
}

function normalizeTunnel(raw: unknown): TunnelRecord {
  const row = asRecord(raw);
  return {
    id: asString(row.id),
    token: asOptionalString(row.token) ?? "",
    publicUrl: asOptionalString(row.publicUrl ?? row.public_url) ?? "",
    computerId: asString(row.computerId ?? row.computer_id),
    daemonId: asOptionalString(row.daemonId ?? row.daemon_id) ?? "",
    localPort: asNumber(row.localPort ?? row.local_port),
    label: asOptionalString(row.label) ?? "",
    state: (asString(row.state) || "pending_approval") as TunnelState,
    accessPolicy: (asOptionalString(row.accessPolicy ?? row.access_policy) ?? "members") as TunnelAccessPolicy,
    creatorId: asOptionalString(row.creatorId ?? row.creator_id) ?? "",
    creatorKind: asOptionalString(row.creatorKind ?? row.creator_kind) ?? "",
    createdUnix: asNumber(row.createdUnix ?? row.created_unix),
    expiresUnix: asNumber(row.expiresUnix ?? row.expires_unix),
    approvedUnix: asNumber(row.approvedUnix ?? row.approved_unix),
    approvedBy: asOptionalString(row.approvedBy ?? row.approved_by) ?? "",
    closedUnix: asNumber(row.closedUnix ?? row.closed_unix),
    closeReason: asOptionalString(row.closeReason ?? row.close_reason) ?? ""
  };
}

function normalizeReminder(raw: unknown): Reminder {
  const row = asRecord(raw);
  const id = asString(row.id ?? row.reminderId ?? row.reminder_id);
  return {
    id,
    target: asString(row.target),
    scheduleKind: normalizeReminderScheduleKind(row.scheduleKind ?? row.schedule_kind),
    schedule: asString(row.schedule),
    prompt: asOptionalString(row.prompt),
    enabled: Boolean(row.enabled),
    nextRunUnix: asNumber(row.nextRunUnix ?? row.next_run_unix),
    lastRunUnix: asNumber(row.lastRunUnix ?? row.last_run_unix) || undefined,
    runCount: asNumber(row.runCount ?? row.run_count),
    lastError: asOptionalString(row.lastError ?? row.last_error),
    title: asString(row.title) || asString(row.prompt) || id,
    status: normalizeReminderStatus(row.status),
    msgRef: asOptionalString(row.msgRef ?? row.msg_ref),
    recurrenceRule: asOptionalString(row.recurrenceRule ?? row.recurrence_rule ?? asRecord(row.recurrence).rule),
    recurrenceDescription: asOptionalString(
      row.recurrenceDescription ?? row.recurrence_description ?? asRecord(row.recurrence).description
    ),
    recurrenceTimezone: asOptionalString(
      row.recurrenceTimezone ?? row.recurrence_timezone ?? asRecord(row.recurrence).timezone
    ),
    cancelToken: asOptionalString(row.cancelToken ?? row.cancel_token),
    createdUnix: asNumber(row.createdUnix ?? row.created_time_unix ?? row.createdTimeUnix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_time_unix ?? row.updatedTimeUnix)
  };
}

function normalizeReminderEvent(raw: unknown): ReminderEvent {
  const row = asRecord(raw);
  return {
    id: asString(row.id ?? row.eventId ?? row.event_id),
    reminderId: asString(row.reminderId ?? row.reminder_id),
    eventType: enumLabel(row.eventType ?? row.event_type) || asString(row.eventType ?? row.event_type),
    actorType: enumLabel(row.actorType ?? row.actor_type) || asString(row.actorType ?? row.actor_type),
    actorId: asOptionalString(row.actorId ?? row.actor_id),
    occurredTimeUnix: asNumber(row.occurredTimeUnix ?? row.occurred_time_unix),
    nextFireTimeUnix: asNumber(row.nextFireTimeUnix ?? row.next_fire_time_unix) || undefined,
    detail: asOptionalString(row.detail)
  };
}

function normalizeSavedMessage(raw: unknown): SavedMessage {
	const row = asRecord(raw) as RawSavedMessage;
	return {
    id: asString(row.id ?? row.savedMessageId ?? row.saved_message_id),
    target: asString(row.target),
    messageId: asString(row.messageId ?? row.message_id),
    savedByUserId: asOptionalString(row.savedByUserId ?? row.saved_by_user_id),
    savedByAgentId: asOptionalString(row.savedByAgentId ?? row.saved_by_agent_id),
    createdUnix: asNumber(row.createdUnix ?? row.savedTimeUnix ?? row.saved_time_unix),
    message: normalizeMessage(row.message)
	};
}

function normalizeInteractionEndpoint(raw: unknown): InteractionEndpoint {
	const row = asRecord(raw) as RawInteractionEndpoint;
	return {
		id: asString(row.id),
		kind: asString(row.kind),
		provider: asString(row.provider),
		displayName: asString(row.displayName ?? row.display_name),
		targetPrefix: asString(row.targetPrefix ?? row.target_prefix),
		inboundEnabled: Boolean(row.inboundEnabled ?? row.inbound_enabled),
		outboundEnabled: Boolean(row.outboundEnabled ?? row.outbound_enabled),
		authMode: asString(row.authMode ?? row.auth_mode),
		configJson: asString(row.configJson ?? row.config_json),
		createdUnix: asNumber(row.createdUnix ?? row.created_unix),
		updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix)
	};
}

function normalizeIMBindingSession(raw: unknown): IMBindingSession {
  const row = asRecord(raw) as RawIMBindingSession;
  return {
    id: asString(row.id),
    endpointId: asString(row.endpointId ?? row.endpoint_id),
    provider: asString(row.provider),
    method: asString(row.method),
    status: asString(row.status),
    qrPayload: asOptionalString(row.qrPayload ?? row.qr_payload),
    qrImageUrl: asOptionalString(row.qrImageUrl ?? row.qr_image_url),
    expiresUnix: asNumber(row.expiresUnix ?? row.expires_unix),
    createdUnix: asNumber(row.createdUnix ?? row.created_unix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix),
    detail: asOptionalString(row.detail)
  };
}

function normalizeIMChatAuthRequest(raw: unknown): IMChatAuthRequest {
  const row = asRecord(raw) as RawIMChatAuthRequest;
  return {
    id: asString(row.id),
    endpointId: asString(row.endpointId ?? row.endpoint_id),
    provider: asString(row.provider),
    conversationId: asString(row.conversationId ?? row.conversation_id),
    externalThreadId: asOptionalString(row.externalThreadId ?? row.external_thread_id),
    chatTitle: asOptionalString(row.chatTitle ?? row.chat_title),
    senderExternalId: asOptionalString(row.senderExternalId ?? row.sender_external_id),
    tokenPrefix: asOptionalString(row.tokenPrefix ?? row.token_prefix),
    status: asString(row.status),
    requestedTarget: asOptionalString(row.requestedTarget ?? row.requested_target),
    requestedThreadId: asOptionalString(row.requestedThreadId ?? row.requested_thread_id),
    expiresUnix: asNumber(row.expiresUnix ?? row.expires_unix) || undefined,
    resolvedByUserId: asOptionalString(row.resolvedByUserId ?? row.resolved_by_user_id),
    resolvedUnix: asNumber(row.resolvedUnix ?? row.resolved_unix) || undefined,
    createdUnix: asNumber(row.createdUnix ?? row.created_unix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix)
  };
}

function normalizeIMChatSubscription(raw: unknown): IMChatSubscription {
  const row = asRecord(raw) as RawIMChatSubscription;
  return {
    id: asString(row.id),
    endpointId: asString(row.endpointId ?? row.endpoint_id),
    provider: asString(row.provider),
    conversationId: asString(row.conversationId ?? row.conversation_id),
    externalThreadId: asOptionalString(row.externalThreadId ?? row.external_thread_id),
    chatTitle: asOptionalString(row.chatTitle ?? row.chat_title),
    target: asOptionalString(row.target),
    threadId: asOptionalString(row.threadId ?? row.thread_id),
    senderExternalId: asOptionalString(row.senderExternalId ?? row.sender_external_id),
    authorizedByRequestId: asOptionalString(row.authorizedByRequestId ?? row.authorized_by_request_id),
    subscribed: Boolean(row.subscribed),
    verbose: Boolean(row.verbose),
    authorizedUnix: asNumber(row.authorizedUnix ?? row.authorized_unix) || undefined,
    subscribedUnix: asNumber(row.subscribedUnix ?? row.subscribed_unix) || undefined,
    createdUnix: asNumber(row.createdUnix ?? row.created_unix),
    updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix)
  };
}

function normalizeIMChatAuthDecisionResult(raw: unknown): IMChatAuthDecisionResult {
  const row = asRecord(raw);
  return {
    request: normalizeIMChatAuthRequest(row.request),
    subscription: row.subscription ? normalizeIMChatSubscription(row.subscription) : undefined
  };
}

function normalizeNotificationRoute(raw: unknown): NotificationRoute {
	const row = asRecord(raw) as RawNotificationRoute;
	return {
		id: asString(row.id),
		target: asString(row.target),
		threadId: asOptionalString(row.threadId ?? row.thread_id),
		endpointId: asString(row.endpointId ?? row.endpoint_id),
		eventKind: asString(row.eventKind ?? row.event_kind),
		preference: asString(row.preference),
		enabled: Boolean(row.enabled),
		configJson: asString(row.configJson ?? row.config_json),
		createdUnix: asNumber(row.createdUnix ?? row.created_unix),
		updatedUnix: asNumber(row.updatedUnix ?? row.updated_unix)
	};
}

function normalizeList<T>(response: RawListResponse<unknown>, normalize: (raw: unknown) => T): ListResponse<T> {
	return { items: (response.items ?? []).map(normalize) };
}

function normalizeCursor(raw: RawEventCursor | undefined): EventCursor | undefined {
  if (!raw) return undefined;
  return {
    cursor: asOptionalString(raw.cursor),
    target: asOptionalString(raw.target),
    protocolVersion: asNumber(raw.protocolVersion ?? raw.protocol_version) || undefined,
    snapshotId: asOptionalString(raw.snapshotId ?? raw.snapshot_id),
    sequence: asNumber(raw.sequence),
    aggregateId: asOptionalString(raw.aggregateId ?? raw.aggregate_id),
    serverId: asOptionalString(raw.serverId ?? raw.server_id)
  };
}

function normalizeEventKind(value: unknown): CollaborationEventKind {
  if (typeof value === "number") return collaborationEventKindByNumber[value] ?? "unspecified";
  const label = enumLabel(value);
  return (
    ({
      message: "message",
      activity: "activity",
      task: "task",
      reminder: "reminder",
      coordination: "coordination",
      memory: "memory",
      run: "run",
      run_step: "run_step",
      attachment: "attachment",
      ping: "ping"
    } as Record<string, CollaborationEventKind>)[label] ?? "unspecified"
  );
}

function normalizeOperation(value: unknown): EventOperation {
  if (typeof value === "number") return eventOperationByNumber[value] ?? "unspecified";
  const label = enumLabel(value);
  return (
    ({
      created: "created",
      updated: "updated",
      deleted: "deleted",
      state_changed: "state_changed",
      appended: "appended",
      claimed: "claimed",
      released: "released",
      failed: "failed",
      canceled: "canceled",
      cancelled: "canceled",
      heartbeat: "heartbeat",
      invalidated: "invalidated",
      snapshot: "snapshot"
    } as Record<string, EventOperation>)[label] ?? "unspecified"
  );
}

function normalizeScopeType(value: unknown): EventScopeType {
  if (typeof value === "number") return eventScopeTypeByNumber[value] ?? "unspecified";
  const label = enumLabel(value);
  return (
    ({
      server: "server",
      workspace: "workspace",
      target: "target",
      thread: "thread",
      task: "task",
      run: "run",
      agent: "agent",
      computer: "computer",
      user: "user",
      endpoint: "endpoint",
      daemon: "daemon",
      custom: "custom"
    } as Record<string, EventScopeType>)[label] ?? "unspecified"
  );
}

function normalizeScope(raw: unknown): EventScope | undefined {
  const row = asRecord(raw) as RawEventScope;
  if (!Object.keys(row).length) return undefined;
  return {
    scopeType: normalizeScopeType(row.scopeType ?? row.scope_type),
    scopeId: asOptionalString(row.scopeId ?? row.scope_id),
    target: asOptionalString(row.target),
    customType: asOptionalString(row.customType ?? row.custom_type)
  };
}

function normalizePayload(row: RawCollaborationEvent): JsonObject | undefined {
  const payload =
    row.payload ?? row.message ?? row.activity ?? row.task ?? row.run ?? row.runStep ?? row.run_step;
  const record = asRecord(payload);
  return Object.keys(record).length ? record : undefined;
}

function normalizeCollaborationEvent(raw: unknown): CollaborationEvent {
  const row = asRecord(raw) as RawCollaborationEvent;
  return {
    eventId: asOptionalString(row.eventId ?? row.event_id),
    target: asOptionalString(row.target),
    kind: normalizeEventKind(row.kind),
    operation: normalizeOperation(row.operation),
    scope: normalizeScope(row.scope),
    workspaceId: asOptionalString(row.workspaceId ?? row.workspace_id),
    sequence: asNumber(row.sequence),
    aggregateId: asOptionalString(row.aggregateId ?? row.aggregate_id),
    protocolVersion: asNumber(row.protocolVersion ?? row.protocol_version) || undefined,
    messageId: asOptionalString(row.messageId ?? row.message_id),
    activityId: asOptionalString(row.activityId ?? row.activity_id),
    taskId: asOptionalString(row.taskId ?? row.task_id),
    runId: asOptionalString(row.runId ?? row.run_id),
    sourceEndpointId: asOptionalString(row.sourceEndpointId ?? row.source_endpoint_id),
    createdTimeUnix: asNumber(row.createdTimeUnix ?? row.created_time_unix) || undefined,
    payload: normalizePayload(row)
  };
}

function normalizeDaemonInfo(raw: unknown): DaemonInfo {
  const row = asRecord(raw);
  return {
    serverId: asString(row.serverId ?? row.server_id),
    serverName: asString(row.serverName ?? row.server_name),
    protocolVersion: asNumber(row.protocolVersion ?? row.protocol_version),
    minProtocolVersion: asNumber(row.minProtocolVersion ?? row.min_protocol_version),
    maxProtocolVersion: asNumber(row.maxProtocolVersion ?? row.max_protocol_version),
    daemonRpcUrl: asString(row.daemonRpcUrl ?? row.daemon_rpc_url),
    daemonTransport: asString(row.daemonTransport ?? row.daemon_transport),
    cacheDriver: asString(row.cacheDriver ?? row.cache_driver),
    serverTimeUnix: asNumber(row.serverTimeUnix ?? row.server_time_unix) || undefined,
    health: asOptionalString(row.health),
    agentStatusCount: asNumber(row.agentStatusCount ?? row.agent_status_count),
    runCount: asNumber(row.runCount ?? row.run_count),
    activityCount: asNumber(row.activityCount ?? row.activity_count)
  };
}

function normalizeDaemonEnrollment(raw: unknown): DaemonEnrollment {
  const row = asRecord(raw) as RawDaemonEnrollment;
  return {
    id: asString(row.id),
    tokenPrefix: asString(row.tokenPrefix ?? row.token_prefix),
    token: asOptionalString(row.token),
    installCommand: asOptionalString(row.installCommand ?? row.install_command),
    installScriptUrl: asOptionalString(row.installScriptUrl ?? row.install_script_url),
    statusUrl: asString(row.statusUrl ?? row.status_url),
    displayName: asOptionalString(row.displayName ?? row.display_name),
    computerId: asOptionalString(row.computerId ?? row.computer_id),
    daemonId: asOptionalString(row.daemonId ?? row.daemon_id),
    hostname: asOptionalString(row.hostname),
    createdUnix: asNumber(row.createdUnix ?? row.created_unix ?? row.createdTimeUnix ?? row.created_time_unix),
    expiresUnix: asNumber(row.expiresUnix ?? row.expires_unix ?? row.expiresTimeUnix ?? row.expires_time_unix) || undefined,
    connectedUnix: asNumber(row.connectedUnix ?? row.connected_unix ?? row.connectedTimeUnix ?? row.connected_time_unix) || undefined,
    lastHeartbeatUnix:
      asNumber(row.lastHeartbeatUnix ?? row.last_heartbeat_unix ?? row.lastHeartbeatTimeUnix ?? row.last_heartbeat_time_unix) ||
      undefined,
    revokedUnix: asNumber(row.revokedUnix ?? row.revoked_unix ?? row.revokedTimeUnix ?? row.revoked_time_unix) || undefined,
    status: asString(row.status) || "pending"
  };
}

function normalizeAgentStatus(raw: unknown): AgentStatusSnapshot {
  const row = asRecord(raw);
  return {
    agentId: asString(row.agentId ?? row.agent_id),
    computerId: asOptionalString(row.computerId ?? row.computer_id),
    runtimeProfileId: asOptionalString(row.runtimeProfileId ?? row.runtime_profile_id),
    presence: knownEnumLabel(row.presence, agentPresenceByNumber, "agent_presence"),
    activityState: knownEnumLabel(
      row.activityState ?? row.activity_state,
      agentActivityStateByNumber,
      "agent_activity_state"
    ),
    health: knownEnumLabel(row.health, agentHealthByNumber, "agent_health"),
    severity: knownEnumLabel(row.severity, agentSeverityByNumber, "agent_status_severity"),
    summary: asOptionalString(row.summary),
    detail: asOptionalString(row.detail),
    target: asOptionalString(row.target),
    threadId: asOptionalString(row.threadId ?? row.thread_id),
    messageId: asOptionalString(row.messageId ?? row.message_id),
    taskId: asOptionalString(row.taskId ?? row.task_id),
    runId: asOptionalString(row.runId ?? row.run_id),
    operationId: asOptionalString(row.operationId ?? row.operation_id),
    updatedTimeUnix: asNumber(row.updatedTimeUnix ?? row.updated_time_unix) || undefined,
    expiresTimeUnix: asNumber(row.expiresTimeUnix ?? row.expires_time_unix) || undefined
  };
}

function normalizeAgentControlResult(raw: unknown): AgentControlResult {
  const row = asRecord(raw);
  const operation = asRecord(row.operation);
  return {
    accepted: Boolean(row.accepted),
    operationId: asString(operation.operationId ?? operation.operation_id),
    agentId: asString(operation.agentId ?? operation.agent_id),
    computerId: asOptionalString(operation.computerId ?? operation.computer_id),
    runtimeProfileId: asOptionalString(operation.runtimeProfileId ?? operation.runtime_profile_id),
    action: knownEnumLabel(operation.action, agentControlActionByNumber, "agent_control_action"),
    state: knownEnumLabel(operation.state, agentControlStateByNumber, "agent_control_state"),
    reason: asOptionalString(operation.reason),
    createdTimeUnix: asNumber(operation.createdTimeUnix ?? operation.created_time_unix) || undefined,
    updatedTimeUnix: asNumber(operation.updatedTimeUnix ?? operation.updated_time_unix) || undefined
  };
}

function normalizeAgentDirectMessageResult(raw: unknown): AgentDirectMessageResult {
  const row = asRecord(raw);
  return {
    accepted: Boolean(row.accepted),
    message: normalizeMessage(row.message)
  };
}

function normalizeDaemonRun(raw: unknown): DaemonRun {
  const row = asRecord(raw);
  return {
    runId: asString(row.runId ?? row.run_id),
    taskId: asOptionalString(row.taskId ?? row.task_id),
    target: asOptionalString(row.target),
    agentId: asOptionalString(row.agentId ?? row.agent_id),
    computerId: asOptionalString(row.computerId ?? row.computer_id),
    runtimeProfileId: asOptionalString(row.runtimeProfileId ?? row.runtime_profile_id),
    state: knownEnumLabel(row.state, runStateByNumber, "run_state"),
    summary: asOptionalString(row.summary),
    error: asOptionalString(row.error),
    startedTimeUnix: asNumber(row.startedTimeUnix ?? row.started_time_unix) || undefined,
    updatedTimeUnix: asNumber(row.updatedTimeUnix ?? row.updated_time_unix) || undefined,
    completedTimeUnix: asNumber(row.completedTimeUnix ?? row.completed_time_unix) || undefined,
    lastHeartbeatTimeUnix: asNumber(row.lastHeartbeatTimeUnix ?? row.last_heartbeat_time_unix) || undefined
  };
}

function normalizeDaemonActivity(raw: unknown): DaemonActivityRecord {
  const row = asRecord(raw);
  return {
    activityId: asString(row.activityId ?? row.activity_id),
    target: asOptionalString(row.target),
    agentId: asOptionalString(row.agentId ?? row.agent_id),
    kind: asString(row.kind),
    summary: asOptionalString(row.summary),
    detail: asOptionalString(row.detail),
    runId: asOptionalString(row.runId ?? row.run_id),
    stepId: asOptionalString(row.stepId ?? row.step_id),
    sequence: asNumber(row.sequence) || undefined,
    createdTimeUnix: asNumber(row.createdTimeUnix ?? row.created_time_unix) || undefined
  };
}

function normalizeDaemonAgentInstance(raw: unknown): DaemonAgentInstance {
  const row = asRecord(raw);
  return {
    agentId: asString(row.agentId ?? row.agent_id),
    name: asOptionalString(row.name),
    displayName: asOptionalString(row.displayName ?? row.display_name),
    description: asOptionalString(row.description),
    provider: asOptionalString(row.provider),
    model: asOptionalString(row.model),
    computerId: asOptionalString(row.computerId ?? row.computer_id),
    runtimeProfileId: asOptionalString(row.runtimeProfileId ?? row.runtime_profile_id),
    runtimeKind: asOptionalString(row.runtimeKind ?? row.runtime_kind),
    reasoningEffort: asOptionalString(row.reasoningEffort ?? row.reasoning_effort),
    status: knownEnumLabel(row.status, agentPresenceByNumber, "agent_presence")
  };
}

function normalizeCreateDaemonAgentResult(raw: unknown): CreateDaemonAgentResult {
  const row = asRecord(raw);
  const runtimeProfile = asRecord(row.runtimeProfile ?? row.runtime_profile);
  return {
    agent: normalizeDaemonAgentInstance(row.agent),
    runtimeProfileId: asString(runtimeProfile.runtimeProfileId ?? runtimeProfile.runtime_profile_id)
  };
}

function normalizeDaemonInventoryComputer(raw: unknown): DaemonInventoryComputer {
  const row = asRecord(raw);
  const info = asRecord(row.info);
  const inventory = asRecord(row.inventory);
  const profiles = Array.isArray(inventory.runtime_profiles)
    ? inventory.runtime_profiles
    : Array.isArray(inventory.runtimeProfiles)
      ? inventory.runtimeProfiles
      : [];
  const templatesByKind = new Map<string, RuntimeInstanceTemplate[]>();
  const runtimeTypesByKind = new Map<string, RuntimeTypeInventory>();
  for (const profile of profiles) {
    const adapter = parseAdapterConfig(asOptionalString(asRecord(profile).adapter_config_json ?? asRecord(profile).adapterConfigJson));
    const template = normalizeRuntimeInstanceTemplate(profile);
    if (!template.runtimeKind || template.inventoryRole !== "agent_instance_template") continue;
    const current = templatesByKind.get(template.runtimeKind) ?? [];
    current.push(template);
    templatesByKind.set(template.runtimeKind, current);
    const runtimeType = normalizeRuntimeTypeInventory(asRecord(adapter.runtimeType));
    if (runtimeType.kind) {
      runtimeTypesByKind.set(runtimeType.kind, runtimeType);
    }
  }
  const runtimes = (
    Array.isArray(inventory.runtimes) ? inventory.runtimes : []
  ).map((runtime) => normalizeDaemonInventoryRuntime(runtime, templatesByKind, runtimeTypesByKind));
  const agents = (Array.isArray(inventory.agents) ? inventory.agents : [])
    .map(normalizeDaemonAgentInstance)
    .filter((agent) => agent.agentId);
  return {
    computerId: asString(info.computer_id ?? info.computerId),
    displayName: asOptionalString(info.display_name ?? info.displayName),
    hostname: asOptionalString(info.hostname),
    inventoryVersion: asOptionalString(row.inventoryVersion ?? row.inventory_version),
    lastHeartbeatUnix: asNumber(row.lastHeartbeatUnix ?? row.last_heartbeat_unix) || undefined,
    runtimes,
    agents
  };
}

function normalizeDaemonInventoryRuntime(
  raw: unknown,
  templatesByKind: Map<string, RuntimeInstanceTemplate[]>,
  runtimeTypesByKind: Map<string, RuntimeTypeInventory>
): DaemonInventoryRuntime {
  const row = asRecord(raw);
  const kind = asString(row.kind);
  const runtimeType = runtimeTypesByKind.get(kind);
  return {
    runtimeId: asString(row.runtime_id ?? row.runtimeId),
    computerId: asOptionalString(row.computer_id ?? row.computerId),
    kind,
    displayName: asString(row.display_name ?? row.displayName) || kind,
    command: asOptionalString(row.command),
    installed: Boolean(row.installed),
    healthy: Boolean(row.healthy),
    canonical: typeof row.canonical === "boolean" ? row.canonical : undefined,
    capabilities: normalizeCapabilityNames(row.capabilities),
    runtimeType,
    templates: templatesByKind.get(kind) ?? []
  };
}

function normalizeRuntimeInstanceTemplate(raw: unknown): RuntimeInstanceTemplate {
  const row = asRecord(raw);
  const adapter = parseAdapterConfig(asOptionalString(row.adapter_config_json ?? row.adapterConfigJson));
  const template = asRecord(adapter.template);
  return {
    templateId: asString(template.templateId) || asString(row.runtime_profile_id ?? row.runtimeProfileId),
    runtimeKind: asString(template.runtimeKind) || asString(row.kind),
    displayName: asString(template.displayName) || asString(row.kind),
    description: asOptionalString(template.description),
    capabilities: asStringArray(template.capabilities),
    options: Array.isArray(template.options) ? template.options.map(normalizeRuntimeOptionSchema) : [],
    multiInstance: Boolean(template.multiInstance),
    inventoryRole: asString(template.inventoryRole),
    agentIdPattern: asOptionalString(template.agentIdPattern)
  };
}

function normalizeRuntimeTypeInventory(row: Record<string, unknown>): RuntimeTypeInventory {
  const smoke = asRecord(row.smoke);
  return {
    kind: asString(row.kind),
    displayName: asString(row.displayName),
    provider: asString(row.provider),
    command: asOptionalString(row.command),
    aliases: asStringArray(row.aliases),
    installed: Boolean(row.installed),
    healthy: Boolean(row.healthy),
    canonical: typeof row.canonical === "boolean" ? row.canonical : undefined,
    resolvedPath: asOptionalString(row.resolvedPath),
    availability: asOptionalString(row.availability),
    availabilityReason: asOptionalString(row.availabilityReason),
    smoke: Object.keys(smoke).length ? {
      ok: Boolean(smoke.ok),
      status: asString(smoke.status),
      category: asOptionalString(smoke.category),
      detail: asOptionalString(smoke.detail)
    } : undefined,
    capabilities: asStringArray(row.capabilities),
    templates: asStringArray(row.templates)
  };
}

function normalizeRuntimeOptionSchema(raw: unknown): RuntimeOptionSchema {
  const row = asRecord(raw);
  const type = asString(row.type) as RuntimeOptionSchema["type"];
  return {
    name: asString(row.name),
    label: asString(row.label),
    type: ["string", "free_text", "number", "boolean", "path", "enum"].includes(type) ? type : "string",
    required: Boolean(row.required),
    default: asOptionalString(row.default),
    sensitive: Boolean(row.sensitive),
    enum: asStringArray(row.enum),
    description: asOptionalString(row.description)
  };
}

function parseAdapterConfig(value: string | undefined): Record<string, unknown> {
  if (!value) return {};
  try {
    const parsed = JSON.parse(value);
    return asRecord(parsed);
  } catch {
    return {};
  }
}

function normalizeCapabilityNames(value: unknown): string[] {
  return Array.isArray(value)
    ? value.map((capability) => asString(asRecord(capability).name ?? capability)).filter(Boolean)
    : [];
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.map(asString).filter(Boolean) : [];
}

function normalizeRuntimePreset(raw: unknown): RuntimePreset {
  const row = asRecord(raw);
  const capabilities = Array.isArray(row.capabilities)
    ? row.capabilities.map((capability) => asRecord(capability).name).map(asString).filter(Boolean)
    : [];
  return {
    kind: asString(row.kind),
    displayName: asString(row.displayName ?? row.display_name),
    provider: asString(row.provider),
    defaultModel: asOptionalString(row.defaultModel ?? row.default_model),
    command: asOptionalString(row.command),
    aliases: asStringArray(row.aliases),
    defaultArgs: asStringArray(row.defaultArgs ?? row.default_args),
    envVarNames: asStringArray(row.envVarNames ?? row.env_var_names),
    installHint: asStringArray(row.installHint ?? row.install_hint),
    capabilities,
    recommended: Boolean(row.recommended),
    description: asOptionalString(row.description)
  };
}

function appendIfPresent(params: URLSearchParams, name: string, value: string | number | undefined) {
  if (value === undefined) return;
  const stringValue = String(value);
  if (stringValue) params.set(name, stringValue);
}

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export class ApiClient {
  private token = "";

  constructor(private readonly baseURL = "") {}

  setToken(token: string) {
    this.token = token;
  }

  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (this.token) {
      headers.set("Authorization", `Bearer ${this.token}`);
    }

    const response = await fetch(`${this.baseURL}${path}`, {
      ...init,
      headers
    });
    const text = await response.text();
    const body = text ? JSON.parse(text) : null;
    if (!response.ok) {
      throw new ApiError(response.status, body?.error ?? response.statusText);
    }
    return body as T;
  }

  bootstrap(username: string, password: string, displayName: string) {
    return this.request<AuthResponse>("/api/auth/bootstrap", {
      method: "POST",
      body: JSON.stringify({ username, password, displayName })
    });
  }

  init(username: string, password: string, displayName: string) {
    return this.request<AuthResponse>("/api/auth/init", {
      method: "POST",
      body: JSON.stringify({ username, password, displayName })
    });
  }

  setupStatus() {
    return this.request<SetupStatus>("/api/auth/setup-status");
  }

  login(username: string, password: string) {
    return this.request<AuthResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password })
    });
  }

  logout() {
    return this.request<{ ok: boolean }>("/api/auth/logout", { method: "POST" });
  }

  me() {
    return this.request<User>("/api/auth/me");
  }

  protocol() {
    return this.request<ProtocolInfo>("/api/protocol");
  }

  async daemonInfo() {
    return normalizeDaemonInfo(await this.request<unknown>("/api/daemon/info"));
  }

  async createDaemonEnrollment(input: {
    displayName?: string;
    computerId?: string;
    hostname?: string;
    expiresUnix?: number;
  }) {
    return normalizeDaemonEnrollment(await this.request<unknown>("/api/daemon/enrollments", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async getDaemonEnrollment(id: string) {
    return normalizeDaemonEnrollment(
      await this.request<unknown>(`/api/daemon/enrollments/${encodeURIComponent(id)}`)
    );
  }

  async revokeDaemonEnrollment(id: string) {
    return normalizeDaemonEnrollment(
      await this.request<unknown>(`/api/daemon/enrollments/${encodeURIComponent(id)}/revoke`, {
        method: "POST"
      })
    );
  }

  async listDaemonInventory(limit = 100) {
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/daemon/inventory?limit=${limit}`),
      normalizeDaemonInventoryComputer
    );
  }

  async createDaemonAgent(input: CreateDaemonAgentInput) {
    return normalizeCreateDaemonAgentResult(await this.request<unknown>("/api/daemon/agents", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async controlDaemonAgent(agentId: string, input: {
    action: AgentControlAction;
    reason?: string;
    computerId?: string;
    runtimeProfileId?: string;
    requestId?: string;
  }) {
    return normalizeAgentControlResult(
      await this.request<unknown>(`/api/daemon/agents/${encodeURIComponent(agentId)}/control`, {
        method: "POST",
        body: JSON.stringify(input)
      })
    );
  }

  async sendDaemonAgentDirectMessage(agentId: string, input: {
    content: string;
    replyToMessageId?: string;
    attachmentIds?: string[];
    requestId?: string;
  }) {
    return normalizeAgentDirectMessageResult(
      await this.request<unknown>(`/api/daemon/agents/${encodeURIComponent(agentId)}/messages`, {
        method: "POST",
        body: JSON.stringify(input)
      })
    );
  }

  async listAgentStatuses(filters: { target?: string; agentId?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "agentId", filters.agentId);
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/daemon/agent-statuses?${params}`),
      normalizeAgentStatus
    );
  }

  async listDaemonRuns(filters: { target?: string; taskId?: string; agentId?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "taskId", filters.taskId);
    appendIfPresent(params, "agentId", filters.agentId);
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/daemon/runs?${params}`),
      normalizeDaemonRun
    );
  }

  async listDaemonActivity(filters: { target?: string; agentId?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "agentId", filters.agentId);
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/daemon/activity?${params}`),
      normalizeDaemonActivity
    );
  }

  async listRuntimePresets(filters: { includeExperimental?: boolean; kindPrefix?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    if (filters.includeExperimental) params.set("includeExperimental", "true");
    appendIfPresent(params, "kindPrefix", filters.kindPrefix);
    appendIfPresent(params, "limit", filters.limit ?? 200);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/runtime-presets?${params}`),
      normalizeRuntimePreset
    );
  }

  listIMProviders() {
    return this.request<ListResponse<IMProviderSchema>>("/api/im/providers");
  }

  listInteractionEndpoints(limit = 100) {
    return this.request<RawListResponse<unknown>>(
      `/api/interaction-endpoints?limit=${limit}`
    ).then((response) => normalizeList(response, normalizeInteractionEndpoint));
  }

  createInteractionEndpoint(input: {
    kind: string;
    provider: string;
    displayName: string;
    targetPrefix: string;
    inboundEnabled: boolean;
    outboundEnabled: boolean;
    authMode: string;
    configJson: string;
  }) {
    return this.request<unknown>("/api/interaction-endpoints", {
      method: "POST",
      body: JSON.stringify(input)
    }).then(normalizeInteractionEndpoint);
  }

  updateInteractionEndpoint(id: string, patch: {
    displayName?: string;
    targetPrefix?: string;
    inboundEnabled?: boolean;
    outboundEnabled?: boolean;
    authMode?: string;
    configJson?: string;
  }) {
    return this.request<unknown>(`/api/interaction-endpoints/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    }).then(normalizeInteractionEndpoint);
  }

  deleteInteractionEndpoint(id: string) {
    return this.request<{ ok: boolean }>(`/api/interaction-endpoints/${encodeURIComponent(id)}`, {
      method: "DELETE"
    });
  }

  testInteractionEndpoint(id: string) {
    return this.request<InteractionEndpointTestResult>(
      `/api/interaction-endpoints/${encodeURIComponent(id)}/test`,
      { method: "POST", body: JSON.stringify({}) }
    );
  }

  createIMBindingSession(endpointId: string, method = "qr_code") {
    return this.request<unknown>(
      `/api/interaction-endpoints/${encodeURIComponent(endpointId)}/binding-sessions`,
      { method: "POST", body: JSON.stringify({ method }) }
    ).then(normalizeIMBindingSession);
  }

  getIMBindingSession(endpointId: string, sessionId: string) {
    return this.request<unknown>(
      `/api/interaction-endpoints/${encodeURIComponent(endpointId)}/binding-sessions/${encodeURIComponent(sessionId)}`
    ).then(normalizeIMBindingSession);
  }

  cancelIMBindingSession(endpointId: string, sessionId: string) {
    return this.request<unknown>(
      `/api/interaction-endpoints/${encodeURIComponent(endpointId)}/binding-sessions/${encodeURIComponent(sessionId)}/cancel`,
      { method: "POST", body: JSON.stringify({}) }
    ).then(normalizeIMBindingSession);
  }

  listIMChatAuthRequests(filters: {
    endpointId?: string;
    status?: string;
    includeExpired?: boolean;
    limit?: number;
  } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "endpointId", filters.endpointId);
    appendIfPresent(params, "status", filters.status);
    if (filters.includeExpired) params.set("includeExpired", "true");
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return this.request<RawListResponse<unknown>>(`/api/im/chat-auth-requests?${params}`).then((response) =>
      normalizeList(response, normalizeIMChatAuthRequest)
    );
  }

  approveIMChatAuthRequest(id: string, input: { target?: string; threadId?: string } = {}) {
    return this.request<unknown>(`/api/im/chat-auth-requests/${encodeURIComponent(id)}/approve`, {
      method: "POST",
      body: JSON.stringify(input)
    }).then(normalizeIMChatAuthDecisionResult);
  }

  rejectIMChatAuthRequest(id: string) {
    return this.request<{ request?: unknown }>(`/api/im/chat-auth-requests/${encodeURIComponent(id)}/reject`, {
      method: "POST",
      body: JSON.stringify({})
    }).then((response) => normalizeIMChatAuthRequest(response.request));
  }

  bindIMChatAuthRequest(input: { key: string; target?: string; threadId?: string }) {
    return this.request<unknown>("/api/im/chat-auth-requests/bind", {
      method: "POST",
      body: JSON.stringify(input)
    }).then(normalizeIMChatAuthDecisionResult);
  }

  listIMChatSubscriptions(filters: {
    endpointId?: string;
    provider?: string;
    subscribed?: boolean;
    limit?: number;
  } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "endpointId", filters.endpointId);
    appendIfPresent(params, "provider", filters.provider);
    if (filters.subscribed !== undefined) params.set("subscribed", String(filters.subscribed));
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return this.request<RawListResponse<unknown>>(`/api/im/chat-subscriptions?${params}`).then((response) =>
      normalizeList(response, normalizeIMChatSubscription)
    );
  }

  updateIMChatSubscription(id: string, patch: {
    chatTitle?: string;
    target?: string;
    threadId?: string;
    subscribed?: boolean;
    verbose?: boolean;
  }) {
    return this.request<unknown>(`/api/im/chat-subscriptions/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    }).then(normalizeIMChatSubscription);
  }

  listNotificationRoutes(filters: {
    target?: string;
    threadId?: string;
    endpointId?: string;
    eventKind?: string;
    enabled?: boolean;
    limit?: number;
  } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "threadId", filters.threadId);
    appendIfPresent(params, "endpointId", filters.endpointId);
    appendIfPresent(params, "eventKind", filters.eventKind);
    if (filters.enabled !== undefined) params.set("enabled", String(filters.enabled));
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return this.request<RawListResponse<unknown>>(`/api/notification-routes?${params}`).then((response) =>
      normalizeList(response, normalizeNotificationRoute)
    );
  }

  createNotificationRoute(input: {
    target: string;
    threadId?: string;
    endpointId: string;
    eventKind: string;
    preference: string;
    enabled: boolean;
    configJson: string;
  }) {
    return this.request<unknown>("/api/notification-routes", {
      method: "POST",
      body: JSON.stringify(input)
    }).then(normalizeNotificationRoute);
  }

  updateNotificationRoute(id: string, patch: {
    target?: string;
    threadId?: string;
    endpointId?: string;
    eventKind?: string;
    preference?: string;
    enabled?: boolean;
    configJson?: string;
  }) {
    return this.request<unknown>(`/api/notification-routes/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    }).then(normalizeNotificationRoute);
  }

  deleteNotificationRoute(id: string) {
    return this.request<{ ok: boolean }>(`/api/notification-routes/${encodeURIComponent(id)}`, {
      method: "DELETE"
    });
  }

  resolveNotificationRoutes(input: { target: string; threadId?: string; eventKind: string; limit?: number }) {
    const params = new URLSearchParams();
    params.set("target", input.target);
    appendIfPresent(params, "threadId", input.threadId);
    appendIfPresent(params, "eventKind", input.eventKind);
    appendIfPresent(params, "limit", input.limit ?? 100);
    return this.request<RawListResponse<unknown>>(`/api/notification-routes/resolve?${params}`).then((response) =>
      normalizeList(response, normalizeNotificationRoute)
    );
  }

  async listChannels(filters: { joinedOnly?: boolean; limit?: number } = {}) {
    const params = new URLSearchParams();
    if (filters.joinedOnly) params.set("joinedOnly", "true");
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(await this.request<RawListResponse<unknown>>(`/api/channels?${params}`), normalizeChannel);
  }

  async createChannel(input: {
    target: string;
    displayName?: string;
    visibility?: ChannelVisibility | string;
  }) {
    return normalizeChannel(await this.request<unknown>("/api/channels", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async updateChannel(target: string, input: {
    displayName?: string;
    visibility?: ChannelVisibility | string;
  }) {
    const channel = target.replace(/^#/, "");
    return normalizeChannel(await this.request<unknown>(`/api/channels/${encodeURIComponent(channel)}`, {
      method: "PATCH",
      body: JSON.stringify(input)
    }));
  }

  async deleteChannel(target: string) {
    const channel = target.replace(/^#/, "");
    return this.request<{ ok: boolean }>(`/api/channels/${encodeURIComponent(channel)}`, {
      method: "DELETE"
    });
  }

  async listChannelMembers(target: string, limit = 100) {
    const channel = target.replace(/^#/, "");
    return normalizeList(
      await this.request<RawListResponse<unknown>>(
        `/api/channels/${encodeURIComponent(channel)}/members?limit=${limit}`
      ),
      normalizeChannelMember
    );
  }

  async upsertChannelMember(target: string, input: {
    memberId: string;
    username?: string;
    displayName?: string;
    kind?: string;
    role?: ChannelMemberRole | string;
  }) {
    const channel = target.replace(/^#/, "");
    return normalizeChannelMember(await this.request<unknown>(`/api/channels/${encodeURIComponent(channel)}/members`, {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async updateChannelMember(target: string, kind: string, memberId: string, input: {
    username?: string;
    displayName?: string;
    role?: ChannelMemberRole | string;
  }) {
    const channel = target.replace(/^#/, "");
    return normalizeChannelMember(
      await this.request<unknown>(
        `/api/channels/${encodeURIComponent(channel)}/members/${encodeURIComponent(kind)}/${encodeURIComponent(memberId)}`,
        {
          method: "PATCH",
          body: JSON.stringify(input)
        }
      )
    );
  }

  async deleteChannelMember(target: string, kind: string, memberId: string) {
    const channel = target.replace(/^#/, "");
    return this.request<{ ok: boolean }>(
      `/api/channels/${encodeURIComponent(channel)}/members/${encodeURIComponent(kind)}/${encodeURIComponent(memberId)}`,
      { method: "DELETE" }
    );
  }

  async uploadAttachment(target: string, file: File) {
    const form = new FormData();
    form.set("target", target);
    form.set("file", file);
    return normalizeAttachment(await this.request<unknown>("/api/attachments", {
      method: "POST",
      body: form
    }));
  }

  async downloadAttachment(attachment: Attachment) {
    const response = await fetch(`${this.baseURL}${attachment.downloadUrl}`, {
      headers: this.token ? { Authorization: `Bearer ${this.token}` } : undefined
    });
    if (!response.ok) {
      throw new ApiError(response.status, response.statusText);
    }
    return response.blob();
  }

  async listMessages(target: string, limit = 50, threadId = "") {
    const params = new URLSearchParams();
    params.set("target", target);
    params.set("limit", String(limit));
    if (threadId) params.set("threadId", threadId);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/messages?${params}`),
      normalizeMessage
    );
  }

  searchMessages(filters: { query?: string; target?: string; sender?: string; hasAttachment?: boolean; sort?: "recent" | "relevance"; limit?: number }) {
    const params = new URLSearchParams();
    appendIfPresent(params, "q", filters.query);
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "sender", filters.sender);
    if (filters.hasAttachment) params.set("hasAttachment", "true");
    appendIfPresent(params, "sort", filters.sort ?? "recent");
    appendIfPresent(params, "limit", filters.limit ?? 50);
    return this.request<RawListResponse<unknown>>(`/api/messages/search?${params}`).then((response) =>
      normalizeList(response, normalizeMessage)
    );
  }

  async createMessage(input: {
    target: string;
    content: string;
    role?: string;
    kind?: MessageKind | string;
    threadId?: string;
    attachmentIds?: string[];
    replyToMessageId?: string;
    sourceEndpointId?: string;
    requestId?: string;
  }) {
    return normalizeMessage(await this.request<unknown>("/api/messages", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  listThreadInbox(filters: { targetPrefix?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    if (filters.targetPrefix) params.set("targetPrefix", filters.targetPrefix);
    params.set("limit", String(filters.limit ?? 100));
    return this.request<ListResponse<ThreadInboxItem>>(`/api/inbox/threads?${params}`);
  }

  markThreadRead(input: { target: string; threadId: string }) {
    return this.request<{ ok: boolean }>(
      `/api/inbox/threads/${encodeURIComponent(input.threadId)}/read`,
      {
        method: "POST",
        body: JSON.stringify({ target: input.target })
      }
    );
  }

  markThreadInboxRead(input: { targetPrefix?: string } = {}) {
    return this.request<{ ok: boolean }>("/api/inbox/threads/read-all", {
      method: "POST",
      body: JSON.stringify({ targetPrefix: input.targetPrefix ?? "" })
    });
  }

  listSavedMessages(target: string, limit = 50, filters: { query?: string; hasAttachment?: boolean } = {}) {
    const params = new URLSearchParams();
    params.set("target", target);
    params.set("limit", String(limit));
    appendIfPresent(params, "q", filters.query);
    if (filters.hasAttachment) params.set("hasAttachment", "true");
    return this.request<RawListResponse<unknown>>(`/api/messages/saved?${params}`).then((response) =>
      normalizeList(response, normalizeSavedMessage)
    );
  }

  saveMessage(messageId: string, target: string) {
    return this.request<unknown>(
      `/api/messages/${encodeURIComponent(messageId)}/save?target=${encodeURIComponent(target)}`,
      { method: "POST", body: JSON.stringify({}) }
    ).then(normalizeSavedMessage);
  }

  unsaveMessage(messageId: string, target: string) {
    return this.request<unknown>(
      `/api/messages/${encodeURIComponent(messageId)}/save?target=${encodeURIComponent(target)}`,
      { method: "DELETE", body: JSON.stringify({}) }
    );
  }

  async listTasks(filters: { state?: TaskState | "all"; target?: string; limit?: number }) {
    const params = new URLSearchParams();
    if (filters.state && filters.state !== "all") params.set("state", filters.state);
    if (filters.target) params.set("target", filters.target);
    params.set("limit", String(filters.limit ?? 100));
    return normalizeList(await this.request<RawListResponse<unknown>>(`/api/tasks?${params}`), normalizeTask);
  }

  async listReminders(filters: { target?: string; status?: ReminderStatus; includeCanceled?: boolean; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "status", filters.status);
    if (filters.includeCanceled) params.set("includeCanceled", "true");
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/reminders?${params}`),
      normalizeReminder
    );
  }

  async createReminder(input: {
    target: string;
    title: string;
    prompt?: string;
    delaySeconds?: number;
    fireAt?: string;
    scheduleKind?: ReminderScheduleKind | string;
    schedule?: string;
    timezone?: string;
    msgRef?: string;
  }) {
    return normalizeReminder(await this.request<unknown>("/api/reminders", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async snoozeReminder(id: string, delaySeconds: number) {
    return normalizeReminder(await this.request<unknown>(`/api/reminders/${encodeURIComponent(id)}/snooze`, {
      method: "POST",
      body: JSON.stringify({ delaySeconds })
    }));
  }

  async updateReminder(
    id: string,
    patch: {
      title?: string;
      delaySeconds?: number;
      fireAt?: string;
      scheduleKind?: ReminderScheduleKind | string;
      schedule?: string;
      timezone?: string;
    }
  ) {
    return normalizeReminder(await this.request<unknown>(`/api/reminders/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    }));
  }

  async cancelReminder(id: string, cancelToken?: string) {
    return normalizeReminder(await this.request<unknown>(`/api/reminders/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      body: JSON.stringify({ cancelToken: cancelToken ?? "" })
    }));
  }

  async listReminderLog(id: string, limit = 100) {
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/reminders/${encodeURIComponent(id)}/log?limit=${limit}`),
      normalizeReminderEvent
    );
  }

  async createTask(input: {
    summary: string;
    target: string;
    state?: TaskState;
    assigneeId?: string;
    description?: string;
    blockedReason?: string;
  }) {
    return normalizeTask(await this.request<unknown>("/api/tasks", {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  async updateTask(
    id: string,
    patch: Partial<Pick<Task, "summary" | "description" | "state" | "assigneeId" | "blockedReason">>
  ) {
    return normalizeTask(await this.request<unknown>(`/api/tasks/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    }));
  }

  listTaskComments(taskId: string, limit = 100) {
    return this.request<RawListResponse<unknown>>(
      `/api/tasks/${encodeURIComponent(taskId)}/comments?limit=${limit}`
    ).then((response) =>
      normalizeList(response, normalizeMessage)
    );
  }

  async createTaskComment(taskId: string, input: { content: string; requestId?: string }) {
    return normalizeMessage(await this.request<unknown>(`/api/tasks/${encodeURIComponent(taskId)}/comments`, {
      method: "POST",
      body: JSON.stringify(input)
    }));
  }

  listTaskTimeline(taskId: string, filters: { sequence?: number; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "sequence", filters.sequence);
    appendIfPresent(params, "limit", filters.limit ?? 100);
    const query = params.toString();
    return this.request<{ items?: unknown[]; nextCursor?: RawEventCursor; next_cursor?: RawEventCursor }>(
      `/api/tasks/${encodeURIComponent(taskId)}/timeline${query ? `?${query}` : ""}`
    ).then((response) => ({
      items: (response.items ?? []).map(normalizeCollaborationEvent),
      nextCursor: normalizeCursor(response.nextCursor ?? response.next_cursor)
    }));
  }

  listDaemonEvents(filters: { target?: string; aggregateId?: string; sequence?: number; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "target", filters.target);
    appendIfPresent(params, "aggregateId", filters.aggregateId);
    appendIfPresent(params, "sequence", filters.sequence);
    appendIfPresent(params, "limit", filters.limit ?? 100);
    const query = params.toString();
    return this.request<{ items?: unknown[]; nextCursor?: RawEventCursor; next_cursor?: RawEventCursor }>(
      `/api/daemon/events${query ? `?${query}` : ""}`
    ).then((response) => ({
      items: (response.items ?? []).map(normalizeCollaborationEvent),
      nextCursor: normalizeCursor(response.nextCursor ?? response.next_cursor)
    }));
  }

  subscribeServerEvents({
    token,
    cursor,
    sequence,
    target,
    aggregateId,
    limit,
    onEvent,
    onError
  }: SubscribeServerEventsOptions): EventTransport {
    const params = new URLSearchParams();
    appendIfPresent(params, "access_token", token ?? this.token);
    appendIfPresent(params, "cursor", cursor);
    if (!cursor) appendIfPresent(params, "sequence", sequence);
    appendIfPresent(params, "target", target);
    appendIfPresent(params, "aggregateId", aggregateId);
    appendIfPresent(params, "limit", limit);
    const query = params.toString();
    const source = new EventSource(`${this.baseURL}/api/server-events${query ? `?${query}` : ""}`);

    source.onmessage = (event) => {
      onEvent({
        id: event.lastEventId || undefined,
        event: event.type || undefined,
        data: normalizeCollaborationEvent(event.data ? JSON.parse(event.data) : null)
      });
    };
    source.onerror = (event) => {
      onError?.(event);
    };

    return source;
  }

  // --- Channel decisions (governance records with voting) ---------------

  async listChannelDecisions(target: string, filters: { status?: DecisionStatus[]; limit?: number } = {}) {
    const params = new URLSearchParams();
    if (filters.status && filters.status.length > 0) params.set("status", filters.status.join(","));
    params.set("limit", String(filters.limit ?? 100));
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/channels/${encodeURIComponent(target)}/decisions?${params}`),
      normalizeDecision
    );
  }

  async proposeChannelDecision(target: string, body: { title: string; body: string; supersedesDecisionId?: string }) {
    return normalizeDecision(
      await this.request<unknown>(`/api/channels/${encodeURIComponent(target)}/decisions`, {
        method: "POST",
        body: JSON.stringify(body)
      })
    );
  }

  async voteChannelDecision(id: string, vote: { decision: DecisionVote; reason?: string }) {
    const raw = (await this.request<{ decision: unknown; vote: unknown }>(`/api/decisions/${encodeURIComponent(id)}/vote`, {
      method: "POST",
      body: JSON.stringify(vote)
    })) ?? { decision: {}, vote: {} };
    return {
      decision: normalizeDecision(raw.decision),
      vote: normalizeDecisionVote(raw.vote)
    };
  }

  async ratifyChannelDecision(id: string, opts: { force?: boolean } = {}) {
    return normalizeDecision(
      await this.request<unknown>(`/api/decisions/${encodeURIComponent(id)}/ratify`, {
        method: "POST",
        body: JSON.stringify(opts)
      })
    );
  }

  async retireChannelDecision(id: string, opts: { reason?: string } = {}) {
    return normalizeDecision(
      await this.request<unknown>(`/api/decisions/${encodeURIComponent(id)}/retire`, {
        method: "POST",
        body: JSON.stringify(opts)
      })
    );
  }

  async listDecisionVotes(id: string) {
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/decisions/${encodeURIComponent(id)}/votes`),
      normalizeDecisionVote
    );
  }

  // --- Agent run archive ------------------------------------------------

  async listAgentRuns(filters: { agentId?: string; computerId?: string; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "agentId", filters.agentId);
    appendIfPresent(params, "computerId", filters.computerId);
    params.set("limit", String(filters.limit ?? 50));
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/agent-runs?${params}`),
      normalizeAgentRun
    );
  }

  async getAgentRun(id: string, includeEvents = false) {
    const params = new URLSearchParams();
    if (includeEvents) params.set("events", "1");
    const raw = (await this.request<{ run: unknown; events: unknown[] }>(
      `/api/agent-runs/${encodeURIComponent(id)}?${params}`
    )) ?? { run: {}, events: [] };
    return {
      run: normalizeAgentRun(raw.run),
      events: (Array.isArray(raw.events) ? raw.events : []).map(normalizeAgentRunEvent)
    };
  }

  async searchAgentRuns(filters: { q: string; agentId?: string; computerId?: string; limit?: number }) {
    const params = new URLSearchParams();
    params.set("q", filters.q);
    appendIfPresent(params, "agentId", filters.agentId);
    appendIfPresent(params, "computerId", filters.computerId);
    params.set("limit", String(filters.limit ?? 50));
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/agent-runs/search?${params}`),
      normalizeAgentRunHit
    );
  }

  // --- Preview tunnels --------------------------------------------------

  async listTunnels(filters: { computerId?: string; state?: TunnelState; limit?: number } = {}) {
    const params = new URLSearchParams();
    appendIfPresent(params, "computerId", filters.computerId);
    appendIfPresent(params, "state", filters.state);
    params.set("limit", String(filters.limit ?? 50));
    return normalizeList(
      await this.request<RawListResponse<unknown>>(`/api/tunnels?${params}`),
      normalizeTunnel
    );
  }

  async createTunnel(body: { computerId: string; localPort: number; label?: string; accessPolicy?: TunnelAccessPolicy; ttlSeconds?: number }) {
    return normalizeTunnel(
      await this.request<unknown>(`/api/tunnels`, {
        method: "POST",
        body: JSON.stringify(body)
      })
    );
  }

  async approveTunnel(id: string) {
    return normalizeTunnel(
      await this.request<unknown>(`/api/tunnels/${encodeURIComponent(id)}/approve`, { method: "POST" })
    );
  }

  async rejectTunnel(id: string, reason?: string) {
    return normalizeTunnel(
      await this.request<unknown>(`/api/tunnels/${encodeURIComponent(id)}/reject`, {
        method: "POST",
        body: JSON.stringify({ reason: reason ?? "" })
      })
    );
  }

  async closeTunnel(id: string, reason?: string) {
    return normalizeTunnel(
      await this.request<unknown>(`/api/tunnels/${encodeURIComponent(id)}/close`, {
        method: "POST",
        body: JSON.stringify({ reason: reason ?? "" })
      })
    );
  }
}

export const makeRequestId = (prefix: string) =>
  `${prefix}-${Date.now().toString(36)}-${crypto.randomUUID()}`;
