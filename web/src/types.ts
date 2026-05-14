export type User = {
  id: string;
  username: string;
  displayName: string;
  role: string;
  createdUnix?: number;
  updatedUnix?: number;
};

export type AuthResponse = {
  token: string;
  expiresUnix: number;
  user: User;
};

export type SetupStatus = {
  initialized: boolean;
  webSetupEnabled: boolean;
  bootstrapMethods: string[];
  serverId: string;
  dataDir: string;
};

export type InteractionEndpoint = {
  id: string;
  kind: string;
  provider: string;
  displayName: string;
  targetPrefix: string;
  inboundEnabled: boolean;
  outboundEnabled: boolean;
  authMode: string;
  configJson: string;
  createdUnix: number;
  updatedUnix: number;
};

export type InteractionEndpointTestResult = {
  ready: boolean;
  runtimeLive: boolean;
  summary: string;
  checks: Array<{
    name: string;
    ok: boolean;
    detail?: string;
  }>;
  endpoint?: InteractionEndpoint;
};

export type NotificationRoute = {
  id: string;
  target: string;
  threadId?: string;
  endpointId: string;
  eventKind: string;
  preference: string;
  enabled: boolean;
  configJson: string;
  createdUnix: number;
  updatedUnix: number;
};

export type IMProviderFieldType = "string" | "boolean" | "select" | "json";

export type IMProviderField = {
  name: string;
  label: string;
  type: IMProviderFieldType | string;
  required?: boolean;
  sensitive?: boolean;
  description?: string;
  placeholder?: string;
  options?: string[];
};

export type IMBindingMethod = {
  method: string;
  displayName: string;
  description: string;
};

export type IMSetupMethod = {
  method: string;
  displayName: string;
  description: string;
  primary?: boolean;
};

export type IMProviderSchema = {
  provider: string;
  displayName: string;
  transport: string;
  description: string;
  canonical?: boolean;
  availability?: string;
  runtimeStatus?: string;
  source?: string;
  notes?: string[];
  supportsWebhook: boolean;
  supportsPolling: boolean;
  supportsStreaming: boolean;
  supportsMedia: boolean;
  bindingTargets: string[];
  bindingMethods?: IMBindingMethod[];
  setupMethods?: IMSetupMethod[];
  setupHints: string[];
  fields: IMProviderField[];
};

export type IMBindingSession = {
  id: string;
  endpointId: string;
  provider: string;
  method: string;
  status: string;
  qrPayload?: string;
  qrImageUrl?: string;
  expiresUnix: number;
  createdUnix: number;
  updatedUnix: number;
  detail?: string;
};

export type IMChatAuthRequestStatus = "pending" | "approved" | "rejected" | "expired" | string;

export type IMChatAuthRequest = {
  id: string;
  endpointId: string;
  provider: string;
  conversationId: string;
  externalThreadId?: string;
  chatTitle?: string;
  senderExternalId?: string;
  tokenPrefix?: string;
  status: IMChatAuthRequestStatus;
  requestedTarget?: string;
  requestedThreadId?: string;
  expiresUnix?: number;
  resolvedByUserId?: string;
  resolvedUnix?: number;
  createdUnix: number;
  updatedUnix: number;
};

export type IMChatSubscription = {
  id: string;
  endpointId: string;
  provider: string;
  conversationId: string;
  externalThreadId?: string;
  chatTitle?: string;
  target?: string;
  threadId?: string;
  senderExternalId?: string;
  authorizedByRequestId?: string;
  subscribed: boolean;
  verbose: boolean;
  authorizedUnix?: number;
  subscribedUnix?: number;
  createdUnix: number;
  updatedUnix: number;
};

export type IMChatAuthDecisionResult = {
  request: IMChatAuthRequest;
  subscription?: IMChatSubscription;
};

export type ChannelVisibility = "public" | "private" | "unspecified";
export type ChannelMemberRole = "admin" | "member" | "viewer" | "unspecified";

export type Channel = {
  target: string;
  displayName: string;
  channelType: string;
  visibility: ChannelVisibility | string;
  joined: boolean;
  memberCount: number;
  currentUserRole?: ChannelMemberRole | string;
  createdByUserId?: string;
  createdUnix?: number;
  updatedUnix?: number;
};

export type ChannelMember = {
  target: string;
  memberId: string;
  username?: string;
  displayName: string;
  kind: string;
  role: ChannelMemberRole | string;
  joinedTimeUnix: number;
  updatedUnix?: number;
};

export type MessageSenderKind = "user" | "agent" | "system" | "endpoint";

export type Attachment = {
  id: string;
  target: string;
  ownerId?: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  storageRef?: string;
  downloadUrl: string;
  uploadUrl?: string;
  expiresTimeUnix?: number;
  createdUnix: number;
};

export type JsonObject = Record<string, unknown>;

export type Message = {
  id: string;
  target: string;
  threadId?: string;
  role: string;
  content: string;
  replyToMessageId?: string;
  senderUserId?: string;
  senderAgentId?: string;
  senderDisplayName?: string;
  senderKind: MessageSenderKind | string;
  sourceEndpointId?: string;
  externalMessageId?: string;
  metadataJson?: string;
  attachments?: Attachment[];
  requestId?: string;
  createdUnix: number;
  kind?: MessageKind;
};

export type ThreadInboxItem = {
  target: string;
  threadId: string;
  topic: string;
  messageCount: number;
  unreadCount: number;
  lastReadUnix: number;
  lastReadMessageId?: string;
  firstMessage: Message;
  latestMessage: Message;
  updatedUnix: number;
};

export type SavedMessage = {
  id: string;
  target: string;
  messageId: string;
  savedByUserId?: string;
  savedByAgentId?: string;
  createdUnix: number;
  message: Message;
};

export type TaskState =
  | "todo"
  | "in_progress"
  | "blocked"
  | "in_review"
  | "done"
  | "canceled";

export type Task = {
  id: string;
  summary: string;
  description?: string;
  state: TaskState;
  target: string;
  assigneeId?: string;
  createdByUserId?: string;
  blockedReason?: string;
  version?: number;
  claimLeaseId?: string;
  createdUnix: number;
  updatedUnix: number;
};

export type ReminderStatus = "active" | "done" | "canceled" | "paused" | "failed" | "unspecified";

export type ReminderScheduleKind = "cron" | "every" | "at" | "rrule" | "natural" | "unspecified";

export type Reminder = {
  id: string;
  target: string;
  scheduleKind: ReminderScheduleKind | string;
  schedule: string;
  prompt?: string;
  enabled: boolean;
  nextRunUnix: number;
  lastRunUnix?: number;
  runCount: number;
  lastError?: string;
  title: string;
  status: ReminderStatus | string;
  msgRef?: string;
  recurrenceRule?: string;
  recurrenceDescription?: string;
  recurrenceTimezone?: string;
  cancelToken?: string;
  createdUnix: number;
  updatedUnix: number;
};

export type ReminderEvent = {
  id: string;
  reminderId: string;
  eventType: string;
  actorType: string;
  actorId?: string;
  occurredTimeUnix: number;
  nextFireTimeUnix?: number;
  detail?: string;
};

export type ProtocolInfo = {
  name: string;
  protoPath: string;
  documentation: string;
  compatibility: string;
};

export type DaemonInfo = {
  serverId: string;
  serverName: string;
  protocolVersion: number;
  minProtocolVersion: number;
  maxProtocolVersion: number;
  daemonRpcUrl: string;
  daemonTransport: string;
  cacheDriver: string;
  serverTimeUnix?: number;
  health?: string;
  agentStatusCount?: number;
  runCount?: number;
  activityCount?: number;
};

export type DaemonEnrollmentStatus = "pending" | "connected" | "expired" | "revoked" | "failed" | string;

export type DaemonEnrollment = {
  id: string;
  tokenPrefix: string;
  token?: string;
  installCommand?: string;
  installScriptUrl?: string;
  statusUrl: string;
  displayName?: string;
  computerId?: string;
  daemonId?: string;
  hostname?: string;
  createdUnix: number;
  expiresUnix?: number;
  connectedUnix?: number;
  lastHeartbeatUnix?: number;
  revokedUnix?: number;
  status: DaemonEnrollmentStatus;
};

export type AgentStatusSnapshot = {
  agentId: string;
  computerId?: string;
  runtimeProfileId?: string;
  presence: string;
  activityState: string;
  health: string;
  severity: string;
  summary?: string;
  detail?: string;
  target?: string;
  threadId?: string;
  messageId?: string;
  taskId?: string;
  runId?: string;
  operationId?: string;
  updatedTimeUnix?: number;
  expiresTimeUnix?: number;
};

export type AgentControlAction =
  | "terminate"
  | "restart"
  | "restart_reset_session"
  | "restart_full_reset";

export type AgentControlResult = {
  accepted: boolean;
  operationId: string;
  agentId: string;
  computerId?: string;
  runtimeProfileId?: string;
  action: AgentControlAction | string;
  state: string;
  reason?: string;
  createdTimeUnix?: number;
  updatedTimeUnix?: number;
};

export type AgentDirectMessageResult = {
  accepted: boolean;
  message: Message;
};

export type DaemonRun = {
  runId: string;
  taskId?: string;
  target?: string;
  agentId?: string;
  computerId?: string;
  runtimeProfileId?: string;
  state: string;
  summary?: string;
  error?: string;
  startedTimeUnix?: number;
  updatedTimeUnix?: number;
  completedTimeUnix?: number;
  lastHeartbeatTimeUnix?: number;
};

export type DaemonActivityRecord = {
  activityId: string;
  target?: string;
  agentId?: string;
  kind: string;
  summary?: string;
  detail?: string;
  runId?: string;
  stepId?: string;
  sequence?: number;
  createdTimeUnix?: number;
};

export type RuntimeOptionSchema = {
  name: string;
  label: string;
  type: "string" | "free_text" | "number" | "boolean" | "path" | "enum";
  required: boolean;
  default?: string;
  sensitive?: boolean;
  enum: string[];
  description?: string;
};

export type RuntimeTypeInventory = {
  kind: string;
  displayName: string;
  provider: string;
  command?: string;
  aliases: string[];
  installed: boolean;
  healthy: boolean;
  canonical?: boolean;
  resolvedPath?: string;
  availability?: string;
  availabilityReason?: string;
  smoke?: {
    ok: boolean;
    status: string;
    category?: string;
    detail?: string;
  };
  capabilities: string[];
  templates: string[];
};

export type RuntimeInstanceTemplate = {
  templateId: string;
  runtimeKind: string;
  displayName: string;
  description?: string;
  capabilities: string[];
  options: RuntimeOptionSchema[];
  multiInstance: boolean;
  inventoryRole: string;
  agentIdPattern?: string;
};

export type DaemonInventoryRuntime = {
  runtimeId: string;
  computerId?: string;
  kind: string;
  displayName: string;
  command?: string;
  installed: boolean;
  healthy: boolean;
  canonical?: boolean;
  capabilities: string[];
  runtimeType?: RuntimeTypeInventory;
  templates: RuntimeInstanceTemplate[];
};

export type DaemonInventoryComputer = {
  computerId: string;
  displayName?: string;
  hostname?: string;
  inventoryVersion?: string;
  lastHeartbeatUnix?: number;
  runtimes: DaemonInventoryRuntime[];
  agents: DaemonAgentInstance[];
};

export type DaemonAgentInstance = {
  agentId: string;
  name?: string;
  displayName?: string;
  description?: string;
  provider?: string;
  model?: string;
  computerId?: string;
  runtimeProfileId?: string;
  runtimeKind?: string;
  reasoningEffort?: string;
  status?: string;
};

export type CreateDaemonAgentInput = {
  computerId: string;
  runtimeId: string;
  templateId: string;
  displayName: string;
  name?: string;
  target?: string;
  options: Record<string, string>;
};

export type CreateDaemonAgentResult = {
  agent: DaemonAgentInstance;
  runtimeProfileId: string;
};

export type RuntimePreset = {
  kind: string;
  displayName: string;
  provider: string;
  defaultModel?: string;
  command?: string;
  aliases: string[];
  defaultArgs: string[];
  envVarNames: string[];
  installHint: string[];
  capabilities: string[];
  recommended: boolean;
  description?: string;
};

export type EventCursor = {
  cursor?: string;
  target?: string;
  protocolVersion?: number;
  snapshotId?: string;
  sequence: number;
  aggregateId?: string;
  serverId?: string;
};

export type EventScopeType =
  | "unspecified"
  | "server"
  | "workspace"
  | "target"
  | "thread"
  | "task"
  | "run"
  | "agent"
  | "computer"
  | "user"
  | "endpoint"
  | "daemon"
  | "custom";

export type EventScope = {
  scopeType: EventScopeType;
  scopeId?: string;
  target?: string;
  customType?: string;
};

export type CollaborationEventKind =
  | "unspecified"
  | "message"
  | "activity"
  | "task"
  | "reminder"
  | "coordination"
  | "memory"
  | "run"
  | "run_step"
  | "attachment"
  | "ping";

export type EventOperation =
  | "unspecified"
  | "created"
  | "updated"
  | "deleted"
  | "state_changed"
  | "appended"
  | "claimed"
  | "released"
  | "failed"
  | "canceled"
  | "heartbeat"
  | "invalidated"
  | "snapshot";

export type CollaborationEvent = {
  eventId?: string;
  target?: string;
  kind: CollaborationEventKind;
  operation: EventOperation;
  scope?: EventScope;
  workspaceId?: string;
  sequence: number;
  aggregateId?: string;
  protocolVersion?: number;
  messageId?: string;
  activityId?: string;
  taskId?: string;
  runId?: string;
  sourceEndpointId?: string;
  createdTimeUnix?: number;
  payload?: JsonObject;
};

// --- B-UI: channel decisions + agent runs --------------------------------

export type DecisionStatus = "proposed" | "ratified" | "rejected" | "retired";
export type DecisionVote = "approve" | "reject" | "abstain";
export type DecisionActorKind = "human" | "agent";

export type MessageKind = "" | "note" | "decision" | "blocker" | "status";

export type ChannelDecision = {
  id: string;
  target: string;
  title: string;
  body: string;
  status: DecisionStatus;
  proposerId: string;
  proposerKind: DecisionActorKind;
  createdUnix: number;
  ratifiedUnix: number;
  retiredUnix: number;
  retiredBy?: string;
  retireReason?: string;
  supersedesDecisionId?: string;
  approveCount: number;
  rejectCount: number;
  abstainCount: number;
};

export type ChannelDecisionVote = {
  id: string;
  decisionId: string;
  voterId: string;
  voterKind: DecisionActorKind;
  decision: DecisionVote;
  votedUnix: number;
  reason?: string;
};

export type AgentRunPhase =
  | "start"
  | "tool_call"
  | "tool_result"
  | "error"
  | "output"
  | "end";

export type AgentRun = {
  id: string;
  agentId: string;
  computerId: string;
  startedUnix: number;
  endedUnix: number;
  exitCode: number;
  summary: string;
  error: string;
  eventCount: number;
};

export type AgentRunEvent = {
  id: string;
  runId: string;
  atUnixNano: number;
  phase: AgentRunPhase;
  summary: string;
  payloadJson: string;
  exitCode: number;
  errorMessage: string;
};

export type AgentRunSearchHit = {
  run: AgentRun;
  event: AgentRunEvent;
  highlight: string;
};

export type TunnelState = "pending_approval" | "active" | "rejected" | "closed";
export type TunnelAccessPolicy = "private" | "members" | "public";

export type TunnelRecord = {
  id: string;
  token: string;
  publicUrl: string;
  computerId: string;
  daemonId: string;
  localPort: number;
  label: string;
  state: TunnelState;
  accessPolicy: TunnelAccessPolicy;
  creatorId: string;
  creatorKind: string;
  createdUnix: number;
  expiresUnix: number;
  approvedUnix: number;
  approvedBy: string;
  closedUnix: number;
  closeReason: string;
};
