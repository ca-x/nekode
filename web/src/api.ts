import type {
  Attachment,
  AuthResponse,
  AgentStatusSnapshot,
  Channel,
  ChannelMember,
  ChannelMemberRole,
  ChannelVisibility,
  CollaborationEvent,
  CollaborationEventKind,
  DaemonActivityRecord,
  DaemonInfo,
  DaemonRun,
  EventCursor,
  EventOperation,
  EventScope,
  EventScopeType,
  InteractionEndpoint,
  JsonObject,
  Message,
  ProtocolInfo,
  RuntimePreset,
  SetupStatus,
  Task,
  TaskState,
  ThreadInboxItem,
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
    .replace(/^(collaboration_event_kind|event_operation|event_scope_type|task_state)_/, "");
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
  const row = asRecord(raw);
  return {
    id: asString(row.id ?? row.messageId ?? row.message_id),
    target: asString(row.target),
    threadId: asOptionalString(row.threadId ?? row.thread_id),
    role: asString(row.role),
    content: asString(row.content),
    senderUserId: asOptionalString(row.senderUserId ?? row.sender_user_id),
    senderAgentId: asOptionalString(row.senderAgentId ?? row.sender_agent_id),
    senderDisplayName: asOptionalString(row.senderDisplayName ?? row.sender_display_name),
    senderKind: asString(row.senderKind ?? row.sender_kind),
    sourceEndpointId: asOptionalString(row.sourceEndpointId ?? row.source_endpoint_id),
    externalMessageId: asOptionalString(row.externalMessageId ?? row.external_message_id),
    metadataJson: asOptionalString(row.metadataJson ?? row.metadata_json),
    attachments: Array.isArray(row.attachments) ? row.attachments.map(normalizeAttachment) : undefined,
    requestId: asOptionalString(row.requestId ?? row.request_id),
    createdUnix: asNumber(row.createdUnix ?? row.created_time_unix ?? row.createdTimeUnix)
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
    currentUserRole: normalizeChannelMemberRole(row.currentUserRole ?? row.current_user_role)
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
    joinedTimeUnix: asNumber(row.joinedTimeUnix ?? row.joined_time_unix)
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
    grpcAddr: asString(row.grpcAddr ?? row.grpc_addr),
    daemonTransport: asString(row.daemonTransport ?? row.daemon_transport),
    cacheDriver: asString(row.cacheDriver ?? row.cache_driver),
    serverTimeUnix: asNumber(row.serverTimeUnix ?? row.server_time_unix) || undefined,
    health: asOptionalString(row.health),
    agentStatusCount: asNumber(row.agentStatusCount ?? row.agent_status_count),
    runCount: asNumber(row.runCount ?? row.run_count),
    activityCount: asNumber(row.activityCount ?? row.activity_count)
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
    taskId: asOptionalString(row.taskId ?? row.task_id),
    runId: asOptionalString(row.runId ?? row.run_id),
    updatedTimeUnix: asNumber(row.updatedTimeUnix ?? row.updated_time_unix) || undefined,
    expiresTimeUnix: asNumber(row.expiresTimeUnix ?? row.expires_time_unix) || undefined
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
    slockSupported: Boolean(row.slockSupported ?? row.slock_supported),
    multicaSupported: Boolean(row.multicaSupported ?? row.multica_supported),
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

  listInteractionEndpoints(limit = 100) {
    return this.request<ListResponse<InteractionEndpoint>>(
      `/api/interaction-endpoints?limit=${limit}`
    );
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
    return this.request<InteractionEndpoint>("/api/interaction-endpoints", {
      method: "POST",
      body: JSON.stringify(input)
    });
  }

  async listChannels(filters: { joinedOnly?: boolean; limit?: number } = {}) {
    const params = new URLSearchParams();
    if (filters.joinedOnly) params.set("joinedOnly", "true");
    appendIfPresent(params, "limit", filters.limit ?? 100);
    return normalizeList(await this.request<RawListResponse<unknown>>(`/api/channels?${params}`), normalizeChannel);
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

  async createMessage(input: {
    target: string;
    content: string;
    role?: string;
    threadId?: string;
    attachmentIds?: string[];
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

  async listTasks(filters: { state?: TaskState | "all"; target?: string; limit?: number }) {
    const params = new URLSearchParams();
    if (filters.state && filters.state !== "all") params.set("state", filters.state);
    if (filters.target) params.set("target", filters.target);
    params.set("limit", String(filters.limit ?? 100));
    return normalizeList(await this.request<RawListResponse<unknown>>(`/api/tasks?${params}`), normalizeTask);
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
}

export const makeRequestId = (prefix: string) =>
  `${prefix}-${Date.now().toString(36)}-${crypto.randomUUID()}`;
