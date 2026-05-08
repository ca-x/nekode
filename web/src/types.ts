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

export type MessageSenderKind = "user" | "agent" | "system" | "endpoint";

export type JsonObject = Record<string, unknown>;

export type Message = {
  id: string;
  target: string;
  threadId?: string;
  role: string;
  content: string;
  senderUserId?: string;
  senderAgentId?: string;
  senderDisplayName?: string;
  senderKind: MessageSenderKind | string;
  sourceEndpointId?: string;
  externalMessageId?: string;
  metadataJson?: string;
  requestId?: string;
  createdUnix: number;
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
  state: TaskState;
  target: string;
  assigneeId?: string;
  createdByUserId?: string;
  version?: number;
  claimLeaseId?: string;
  createdUnix: number;
  updatedUnix: number;
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
  grpcAddr: string;
  cacheDriver: string;
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
