import {
  Activity,
  AlertTriangle,
  AtSign,
  Bell,
  Bookmark,
  Bot,
  CheckCircle2,
  CheckSquare,
  Circle,
  CircleX,
  Columns3,
  CornerUpLeft,
  Copy,
  Download,
  Eye,
  File,
  FileText,
  Hash,
  History,
  Image,
  Inbox,
  LayoutGrid,
  Link2,
  List,
  ListFilter,
  Loader2,
  LogOut,
  MessageSquare,
  Monitor,
  OctagonAlert,
  Paperclip,
  Plus,
  RefreshCw,
  Search,
  Send,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Sparkles,
  Square,
  X,
  UsersRound,
  Wifi
} from "lucide-react";
import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiClient, ApiError, makeRequestId } from "./api";
import brandMarkUrl from "./assets-brand.png";
import type {
  AgentControlAction,
  AgentControlResult,
  AgentDirectMessageResult,
  AgentStatusSnapshot,
  Attachment,
  AuthResponse,
  Channel,
  ChannelMember,
  CollaborationEvent,
  CreateDaemonAgentResult,
  DaemonActivityRecord,
  DaemonEnrollment,
  DaemonInfo,
  DaemonInventoryComputer,
  DaemonRun,
  EventCursor,
  IMBindingSession,
  IMProviderField,
  IMProviderSchema,
  InteractionEndpoint,
  InteractionEndpointTestResult,
  Message,
  NotificationRoute,
  ProtocolInfo,
  Reminder,
  ReminderEvent,
  RuntimeInstanceTemplate,
  RuntimeOptionSchema,
  RuntimePreset,
  SavedMessage,
  SetupStatus,
  Task,
  TaskState,
  ThreadInboxItem,
  User
} from "./types";

const TOKEN_KEY = "nekode.console.token";
const EVENT_CURSOR_KEY = "nekode.console.serverEvents.cursorState";
const DEFAULT_TARGET = "#general";
const TEXT_ATTACHMENT_PREVIEW_LIMIT = 2000;
const TEXT_ATTACHMENT_LIGHTBOX_LIMIT = 200000;

type ViewKey =
  | "overview"
  | "inbox"
  | "messages"
  | "tasks"
  | "reminders"
  | "activity"
  | "skills"
  | "settings"
  | "endpoints"
  | "daemon";
type LoadState = "idle" | "loading" | "ready" | "error";
type RealtimeStatus = "disabled" | "connecting" | "connected" | "error";
type TaskViewMode = "board" | "list";
type TaskStateFilter = TaskState | "all" | "open";
type TaskSortKey = "updated_desc" | "created_desc" | "summary_asc" | "state_asc";
type AgentActionBusy = { agentId: string; kind: "control" | "message" } | null;
type SectionIssueKey =
  | "setup"
  | "protocol"
  | "daemon"
  | "endpoints"
  | "notificationRoutes"
  | "imProviders"
  | "channels"
  | "channelMembers"
  | "messages"
  | "savedMessages"
  | "inbox"
  | "tasks"
  | "reminders"
  | "activity"
  | "agentStatuses"
  | "daemonInventory"
  | "daemonRuns"
  | "daemonActivity"
  | "runtimePresets";
type StoredEventCursor = {
  serverId: string;
  protocolVersion: number;
  sequence: number;
};
type SectionIssue = {
  section: SectionIssueKey;
  label: string;
  message: string;
};
type MessageFeedItem =
  | { kind: "message"; message: Message }
  | { kind: "system_group"; id: string; messages: Message[] };
type TaskReceipt = {
  id: string;
  summary: string;
  state: TaskState;
  action: "created" | "moved";
  createdUnix: number;
};
type EnrollmentPlatform = "linux" | "macos" | "windows";

const api = new ApiClient();

const taskColumns: Array<{ state: TaskState; label: string; icon: typeof Circle }> = [
  { state: "todo", label: "Todo", icon: Circle },
  { state: "in_progress", label: "In Progress", icon: Loader2 },
  { state: "blocked", label: "Blocked", icon: OctagonAlert },
  { state: "in_review", label: "In Review", icon: Eye },
  { state: "done", label: "Done", icon: CheckCircle2 },
  { state: "canceled", label: "Canceled", icon: CircleX }
];

const taskStateRank = new Map<TaskState, number>(
  taskColumns.map((column, index) => [column.state, index])
);

const taskStateFilters: Array<{ value: TaskStateFilter; label: string }> = [
  { value: "all", label: "All states" },
  { value: "open", label: "Open" },
  ...taskColumns.map((column) => ({ value: column.state, label: column.label }))
];

const taskSortOptions: Array<{ value: TaskSortKey; label: string }> = [
  { value: "updated_desc", label: "Recently updated" },
  { value: "created_desc", label: "Newest first" },
  { value: "summary_asc", label: "Summary A-Z" },
  { value: "state_asc", label: "Board order" }
];

const agentControlActions: Array<{ value: AgentControlAction; label: string }> = [
  { value: "terminate", label: "Terminate" },
  { value: "restart", label: "Restart" },
  { value: "restart_reset_session", label: "Reset session" },
  { value: "restart_full_reset", label: "Full reset" }
];

const navItems: Array<{ key: ViewKey; label: string; icon: typeof Activity }> = [
  { key: "overview", label: "Overview", icon: Activity },
  { key: "inbox", label: "Inbox", icon: Inbox },
  { key: "messages", label: "Messages", icon: MessageSquare },
  { key: "tasks", label: "Tasks", icon: Columns3 },
  { key: "reminders", label: "Reminders", icon: Bell },
  { key: "activity", label: "Activity", icon: Activity },
  { key: "skills", label: "Skills", icon: Sparkles },
  { key: "settings", label: "Settings", icon: Settings },
  { key: "endpoints", label: "Endpoints", icon: Server },
  { key: "daemon", label: "Daemon", icon: Bot }
];

const demoAgents = [
  { id: "qa", name: "QA", role: "Verifier", status: "online", color: "#b46b2b" },
  { id: "architect", name: "Architect", role: "System design", status: "online", color: "#7b4ee6" },
  { id: "developer", name: "Developer", role: "Implementation", status: "busy", color: "#2b79b4" },
  { id: "reviewer", name: "Reviewer", role: "Code review", status: "idle", color: "#b4432b" }
];

const fallbackRuntimePresets: RuntimePreset[] = [
  {
    kind: "codex",
    displayName: "Codex CLI",
    provider: "openai",
    command: "codex",
    aliases: [],
    defaultArgs: [],
    envVarNames: [],
    installHint: [],
    capabilities: ["code_execution", "file_write", "shell"],
    slockSupported: true,
    multicaSupported: true,
    recommended: true
  },
  {
    kind: "claude",
    displayName: "Claude Code",
    provider: "anthropic",
    command: "claude",
    aliases: [],
    defaultArgs: [],
    envVarNames: [],
    installHint: [],
    capabilities: ["code_execution", "file_write", "shell"],
    slockSupported: true,
    multicaSupported: true,
    recommended: true
  },
  {
    kind: "opencode",
    displayName: "OpenCode",
    provider: "opencode",
    command: "opencode",
    aliases: [],
    defaultArgs: [],
    envVarNames: [],
    installHint: [],
    capabilities: ["code_execution", "file_write", "shell"],
    slockSupported: true,
    multicaSupported: true,
    recommended: true
  }
];

const skillItems = [
  {
    name: "Review discipline",
    tag: "quality",
    usedBy: "Reviewer",
    detail: "Findings first, concrete evidence, residual risk."
  },
  {
    name: "Bridge integration",
    tag: "daemon",
    usedBy: "Developer",
    detail: "HTTP/SSE bridge contracts, cursor replay, typed client boundaries."
  },
  {
    name: "Frontend polish",
    tag: "ux",
    usedBy: "Designer",
    detail: "Responsive layouts, accessible controls, production density."
  }
];

function unixTime(value?: number) {
  if (!value) return "Never";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(value * 1000);
}

function datetimeLocalValue(value?: number) {
  if (!value) return "";
  const date = new Date(value * 1000);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

function attachmentPreviewKind(attachment: Attachment) {
  const mimeType = attachment.mimeType.toLowerCase().split(";")[0].trim();
  const filename = attachment.filename.toLowerCase();
  if (mimeType.startsWith("image/")) return "image";
  if (mimeType === "text/html" || filename.endsWith(".html")) return "html";
  if (mimeType === "text/plain" || filename.endsWith(".txt")) return "text";
  return "file";
}

function truncateAttachmentText(value: string, limit: number) {
  if (value.length <= limit) return { text: value, truncated: false };
  return { text: value.slice(0, limit), truncated: true };
}

function formatBytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function wrapCanvasText(
  ctx: CanvasRenderingContext2D,
  text: string,
  x: number,
  y: number,
  maxWidth: number,
  lineHeight: number,
  maxLines: number
) {
  const words = text.split(/\s+/);
  let line = "";
  let lines = 0;
  for (const word of words) {
    const nextLine = line ? `${line} ${word}` : word;
    if (ctx.measureText(nextLine).width > maxWidth && line) {
      ctx.fillText(line, x, y + lines * lineHeight);
      line = word;
      lines += 1;
      if (lines >= maxLines) return;
    } else {
      line = nextLine;
    }
  }
  if (line && lines < maxLines) {
    ctx.fillText(line, x, y + lines * lineHeight);
  }
}

function isAuthError(error: unknown) {
  return error instanceof ApiError && error.status === 401;
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

function sectionIssue(section: SectionIssueKey, error: unknown): SectionIssue {
  return {
    section,
    label: sectionIssueLabels[section],
    message: errorMessage(error, "Unable to load this section")
  };
}

function readStoredEventCursor(info: DaemonInfo | null): StoredEventCursor | null {
  if (!info) return null;
  try {
    const parsed = JSON.parse(localStorage.getItem(EVENT_CURSOR_KEY) ?? "null") as Partial<StoredEventCursor> | null;
    if (
      parsed?.serverId === info.serverId &&
      parsed.protocolVersion === info.protocolVersion &&
      typeof parsed.sequence === "number"
    ) {
      return {
        serverId: parsed.serverId,
        protocolVersion: parsed.protocolVersion,
        sequence: parsed.sequence
      };
    }
  } catch {
    // Invalid persisted cursors are discarded below.
  }
  localStorage.removeItem(EVENT_CURSOR_KEY);
  return null;
}

function writeStoredEventCursor(info: DaemonInfo, sequence: number) {
  localStorage.setItem(
    EVENT_CURSOR_KEY,
    JSON.stringify({
      serverId: info.serverId,
      protocolVersion: info.protocolVersion,
      sequence
    } satisfies StoredEventCursor)
  );
}

function taskColumnFor(state: TaskState) {
  return taskColumns.find((column) => column.state === state) ?? taskColumns[0];
}

function isOpenTask(task: Task) {
  return task.state !== "done" && task.state !== "canceled";
}

function compareTasks(sortKey: TaskSortKey) {
  return (left: Task, right: Task) => {
    switch (sortKey) {
      case "created_desc":
        return right.createdUnix - left.createdUnix || right.updatedUnix - left.updatedUnix;
      case "summary_asc":
        return left.summary.localeCompare(right.summary) || right.updatedUnix - left.updatedUnix;
      case "state_asc":
        return (
          (taskStateRank.get(left.state) ?? 0) - (taskStateRank.get(right.state) ?? 0) ||
          right.updatedUnix - left.updatedUnix
        );
      case "updated_desc":
      default:
        return right.updatedUnix - left.updatedUnix || right.createdUnix - left.createdUnix;
    }
  };
}

function sameDaemonInfo(left: DaemonInfo | null, right: DaemonInfo | null) {
  return (
    left?.serverId === right?.serverId &&
    left?.protocolVersion === right?.protocolVersion &&
    left?.cacheDriver === right?.cacheDriver &&
    left?.grpcAddr === right?.grpcAddr
  );
}

function shouldInvalidateForEvent(event: CollaborationEvent, activeTarget: string) {
  if (event.target && event.target !== activeTarget) {
    return false;
  }
  const scopeType = event.scope?.scopeType;
  if (event.kind === "message" && event.operation === "appended" && scopeType === "target") {
    return true;
  }
  if (event.kind === "activity" && event.operation === "created" && scopeType === "target") {
    return true;
  }
  if (
    event.kind === "task" &&
    scopeType === "task" &&
    ["created", "state_changed", "updated", "claimed"].includes(event.operation)
  ) {
    return true;
  }
  if (event.kind === "reminder" && ["created", "updated", "canceled"].includes(event.operation)) {
    return true;
  }
  return false;
}

function eventScopeLabel(event: CollaborationEvent) {
  const scope = event.scope;
  if (!scope) return event.target || event.aggregateId || "server";
  return [scope.scopeType, scope.scopeId || scope.target || scope.customType].filter(Boolean).join(":");
}

function eventSummary(event: CollaborationEvent) {
  return `${event.kind}/${event.operation}/${event.scope?.scopeType ?? "unspecified"}`;
}

function isSystemReceipt(message: Message) {
  const senderKind = message.senderKind.toLowerCase();
  const role = message.role.toLowerCase();
  return senderKind === "system" || role === "system";
}

function buildMessageFeed(messages: Message[]): MessageFeedItem[] {
  const feed: MessageFeedItem[] = [];
  let pendingSystemMessages: Message[] = [];

  const flushSystemMessages = () => {
    if (!pendingSystemMessages.length) return;
    if (pendingSystemMessages.length === 1) {
      feed.push({ kind: "message", message: pendingSystemMessages[0] });
    } else {
      feed.push({
        kind: "system_group",
        id: pendingSystemMessages.map((message) => message.id).join(":"),
        messages: pendingSystemMessages
      });
    }
    pendingSystemMessages = [];
  };

  messages.forEach((message) => {
    if (isSystemReceipt(message)) {
      pendingSystemMessages.push(message);
      return;
    }
    flushSystemMessages();
    feed.push({ kind: "message", message });
  });
  flushSystemMessages();
  return feed;
}

function formatUnixTime(value?: number) {
  if (!value) return "unknown";
  return new Date(value * 1000).toLocaleString();
}

function compactId(value?: string) {
  if (!value) return "n/a";
  return value.length > 18 ? `${value.slice(0, 10)}...${value.slice(-6)}` : value;
}

function runtimeTemplateCount(inventory: DaemonInventoryComputer[]) {
  return inventory.reduce(
    (total, computer) => total + computer.runtimes.reduce((sum, runtime) => sum + runtime.templates.length, 0),
    0
  );
}

function defaultOptionValues(template: RuntimeInstanceTemplate | null) {
  const values: Record<string, string> = {};
  for (const option of template?.options ?? []) {
    if (option.default !== undefined) values[option.name] = option.default;
  }
  return values;
}

function optionInputValue(values: Record<string, string>, option: RuntimeOptionSchema) {
  return values[option.name] ?? option.default ?? "";
}

function messagePermalink(message: Message) {
  return `#${encodeURIComponent(message.target)}:${encodeURIComponent(message.id)}`;
}

function healthClass(value?: string) {
  const normalized = (value || "unknown").toLowerCase();
  if (normalized === "ok" || normalized === "online" || normalized === "running" || normalized === "connected") {
    return "is-ok";
  }
  if (normalized === "idle" || normalized === "pending" || normalized === "unspecified" || normalized === "queued") {
    return "is-idle";
  }
  return "is-warn";
}

function presenceClass(value?: string) {
  const normalized = (value || "idle").toLowerCase();
  if (normalized === "online") return "online";
  if (normalized === "busy") return "busy";
  if (["stale", "offline", "degraded"].includes(normalized)) return "offline";
  return "idle";
}

function enrollmentStatus(enrollment: DaemonEnrollment | null) {
  if (!enrollment) return "idle";
  if (enrollment.status === "pending" && enrollment.expiresUnix && enrollment.expiresUnix <= Math.floor(Date.now() / 1000)) {
    return "expired";
  }
  return enrollment.status || "pending";
}

function mergeEnrollmentStatus(current: DaemonEnrollment | null, next: DaemonEnrollment) {
  return {
    ...next,
    installCommand: current?.installCommand,
    installScriptUrl: current?.installScriptUrl,
    token: current?.token
  };
}

function platformInstallCommands(enrollment: DaemonEnrollment | null) {
  if (!enrollment) return [];
  const installScriptUrl = enrollment.installScriptUrl || "";
  const macScriptUrl = installScriptUrl.replace("platform=linux", "platform=darwin");
  const windowsScriptUrl = macScriptUrl.replace("/install.sh", "/install.ps1").replace("platform=darwin", "platform=windows");
  return [
    {
      platform: "linux" as EnrollmentPlatform,
      label: "Linux",
      detail: "Bash entry for systemd hosts",
      command: enrollment.installCommand || "",
      ready: Boolean(enrollment.installCommand)
    },
    {
      platform: "macos" as EnrollmentPlatform,
      label: "macOS",
      detail: "Bash entry for launchd hosts",
      command: macScriptUrl ? `sudo bash -c "$(curl -fsSL ${macScriptUrl})"` : "",
      ready: Boolean(macScriptUrl)
    },
    {
      platform: "windows" as EnrollmentPlatform,
      label: "Windows",
      detail: "PowerShell entry for Windows Service",
      command: windowsScriptUrl ? `powershell -ExecutionPolicy Bypass -Command "iwr ${windowsScriptUrl} | iex"` : "",
      ready: Boolean(windowsScriptUrl)
    }
  ];
}

function roleClass(value?: string) {
  const normalized = (value || "member").toLowerCase();
  if (["owner", "admin"].includes(normalized)) return "is-ok";
  if (normalized === "viewer") return "is-idle";
  return "is-warn";
}

function fallbackChannels(): Channel[] {
  return ["#general", "#ops", "#release"].map((target) => ({
    target,
    displayName: target.replace("#", ""),
    channelType: "channel",
    visibility: "public",
    joined: true,
    memberCount: 1,
    currentUserRole: "member"
  }));
}

function realAgentSidebarItems(inventory: DaemonInventoryComputer[], statuses: AgentStatusSnapshot[]) {
  const byID = new Map<string, {
    agentId: string;
    label: string;
    detail: string;
    presence: string;
  }>();
  for (const computer of inventory) {
    for (const agent of computer.agents) {
      if (!agent.agentId) continue;
      byID.set(agent.agentId, {
        agentId: agent.agentId,
        label: agent.displayName || agent.name || agent.agentId,
        detail: agent.runtimeKind || computer.displayName || computer.hostname || computer.computerId,
        presence: agent.status || "idle"
      });
    }
  }
  for (const status of statuses) {
    if (!status.agentId) continue;
    const current = byID.get(status.agentId);
    byID.set(status.agentId, {
      agentId: status.agentId,
      label: current?.label || status.agentId,
      detail: status.summary || status.target || current?.detail || status.computerId || "runtime status",
      presence: status.presence || current?.presence || "idle"
    });
  }
  return Array.from(byID.values()).sort((left, right) => left.label.localeCompare(right.label));
}

function App() {
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) ?? "");
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<ViewKey>("overview");
  const [status, setStatus] = useState<LoadState>("idle");
  const [error, setError] = useState("");
  const [sectionIssues, setSectionIssues] = useState<SectionIssue[]>([]);
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null);
  const [protocol, setProtocol] = useState<ProtocolInfo | null>(null);
  const [daemonInfo, setDaemonInfo] = useState<DaemonInfo | null>(null);
  const [realtimeStatus, setRealtimeStatus] = useState<RealtimeStatus>("disabled");
  const [realtimeReconnectAttempt, setRealtimeReconnectAttempt] = useState(0);
  const [latestEvent, setLatestEvent] = useState<CollaborationEvent | null>(null);
  const [events, setEvents] = useState<CollaborationEvent[]>([]);
  const [agentStatuses, setAgentStatuses] = useState<AgentStatusSnapshot[]>([]);
  const [daemonInventory, setDaemonInventory] = useState<DaemonInventoryComputer[]>([]);
  const [daemonRuns, setDaemonRuns] = useState<DaemonRun[]>([]);
  const [daemonActivity, setDaemonActivity] = useState<DaemonActivityRecord[]>([]);
  const [runtimePresets, setRuntimePresets] = useState<RuntimePreset[]>(fallbackRuntimePresets);
  const [endpoints, setEndpoints] = useState<InteractionEndpoint[]>([]);
  const [notificationRoutes, setNotificationRoutes] = useState<NotificationRoute[]>([]);
  const [imProviders, setIMProviders] = useState<IMProviderSchema[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [channelMembers, setChannelMembers] = useState<ChannelMember[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [threadInbox, setThreadInbox] = useState<ThreadInboxItem[]>([]);
  const [activeThread, setActiveThread] = useState<ThreadInboxItem | null>(null);
  const [savedMessages, setSavedMessages] = useState<SavedMessage[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [reminders, setReminders] = useState<Reminder[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [target, setTarget] = useState(DEFAULT_TARGET);

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedTaskId) ?? null,
    [selectedTaskId, tasks]
  );
  const sidebarAgents = useMemo(
    () => realAgentSidebarItems(daemonInventory, agentStatuses),
    [daemonInventory, agentStatuses]
  );
  const sectionIssueGroups = useMemo(() => {
    const pick = (sections: SectionIssueKey[]) =>
      sectionIssues.filter((issue) => sections.includes(issue.section));
    return {
      overview: sectionIssues,
      inbox: pick(["inbox"]),
      messages: pick(["messages", "savedMessages", "channelMembers"]),
      tasks: pick(["tasks"]),
      reminders: pick(["reminders"]),
      activity: pick(["activity", "agentStatuses", "daemonRuns", "daemonActivity"]),
      settings: pick(["setup", "protocol", "daemon", "channels", "channelMembers", "runtimePresets"]),
      endpoints: pick(["endpoints", "notificationRoutes", "imProviders"]),
      daemon: pick(["daemon", "agentStatuses", "daemonInventory", "daemonRuns", "daemonActivity", "runtimePresets"])
    };
  }, [sectionIssues]);

  const loadData = useCallback(async (options: { background?: boolean } = {}) => {
    if (!token) return;
    api.setToken(token);
    if (!options.background) {
      setStatus((current) => (current === "ready" ? current : "loading"));
    }
    setError("");
    if (!options.background) {
      setSectionIssues([]);
    }
    const issues: SectionIssue[] = [];
    const capture = async <T,>(section: SectionIssueKey, request: Promise<T>, fallback: T): Promise<T> => {
      try {
        return await request;
      } catch (err) {
        if (isAuthError(err)) throw err;
        issues.push(sectionIssue(section, err));
        return fallback;
      }
    };
    try {
      const messageTarget = activeThread?.target ?? target;
      const messageThreadID = activeThread?.threadId ?? "";
      const coreData = await Promise.all([
        api.me(),
        capture<SetupStatus | null>("setup", api.setupStatus(), null),
        capture<ProtocolInfo | null>("protocol", api.protocol(), null),
        capture<DaemonInfo | null>("daemon", api.daemonInfo(), null),
        capture("endpoints", api.listInteractionEndpoints(), { items: [] as InteractionEndpoint[] }),
        capture("notificationRoutes", api.listNotificationRoutes(), { items: [] as NotificationRoute[] }),
        capture("imProviders", api.listIMProviders(), { items: [] as IMProviderSchema[] }),
        capture("channels", api.listChannels({ joinedOnly: false }), { items: [] as Channel[] }),
        capture("channelMembers", api.listChannelMembers(messageTarget), { items: [] as ChannelMember[] }),
        capture("messages", api.listMessages(messageTarget, 50, messageThreadID), { items: [] as Message[] }),
        capture("savedMessages", api.listSavedMessages(messageTarget), { items: [] as SavedMessage[] }),
        capture("inbox", api.listThreadInbox({ limit: 100 }), { items: [] as ThreadInboxItem[] }),
        capture("tasks", api.listTasks({ target }), { items: [] as Task[] }),
        capture("reminders", api.listReminders({ target, includeCanceled: true, limit: 100 }), { items: [] as Reminder[] })
      ]);
      const [
        me,
        setupInfo,
        protocolInfo,
        daemonBridgeInfo,
        endpointList,
        notificationRouteList,
        imProviderList,
        channelList,
        channelMemberList,
        messageList,
        savedMessageList,
        inboxList,
        taskList,
        reminderList
      ] = coreData;
      setUser(me);
      setSetupStatus(setupInfo);
      setProtocol(protocolInfo);
      setDaemonInfo((current) => (sameDaemonInfo(current, daemonBridgeInfo) ? current : daemonBridgeInfo));
      setEndpoints(endpointList.items);
      setNotificationRoutes(notificationRouteList.items);
      setIMProviders(imProviderList.items);
      setChannels(channelList.items.length ? channelList.items : fallbackChannels());
      setChannelMembers(channelMemberList.items);
      setMessages(messageList.items);
      setSavedMessages(savedMessageList.items);
      setThreadInbox(inboxList.items);
      setTasks(taskList.items);
      setReminders(reminderList.items);
      setStatus("ready");

      const [
        eventList,
        agentStatusList,
        daemonInventoryList,
        daemonRunList,
        daemonActivityList,
        runtimePresetList
      ] = await Promise.all([
        capture("activity", api.listDaemonEvents({ target, limit: 80 }), { items: [] as CollaborationEvent[], nextCursor: undefined as EventCursor | undefined }),
        capture("agentStatuses", api.listAgentStatuses({ limit: 100 }), { items: [] as AgentStatusSnapshot[] }),
        capture("daemonInventory", api.listDaemonInventory(100), { items: [] as DaemonInventoryComputer[] }),
        capture("daemonRuns", api.listDaemonRuns({ limit: 100 }), { items: [] as DaemonRun[] }),
        capture("daemonActivity", api.listDaemonActivity({ limit: 100 }), { items: [] as DaemonActivityRecord[] }),
        capture("runtimePresets", api.listRuntimePresets({ includeExperimental: true }), { items: [] as RuntimePreset[] })
      ]);
      setEvents(eventList.items);
      setAgentStatuses(agentStatusList.items);
      setDaemonInventory(daemonInventoryList.items);
      setDaemonRuns(daemonRunList.items);
      setDaemonActivity(daemonActivityList.items);
      setRuntimePresets(runtimePresetList.items.length ? runtimePresetList.items : fallbackRuntimePresets);
      setSectionIssues(issues);
    } catch (err) {
      if (isAuthError(err)) {
        localStorage.removeItem(TOKEN_KEY);
        setToken("");
        setUser(null);
        setStatus("idle");
        setSectionIssues([]);
        return;
      }
      setError(errorMessage(err, "Unable to load console data"));
      setStatus("error");
    }
  }, [activeThread, target, token]);

  useEffect(() => {
    if (token) void loadData();
  }, [loadData, token]);

  useEffect(() => {
    if (!token || !daemonInfo) {
      setRealtimeStatus("disabled");
      return undefined;
    }

    const storedCursor = readStoredEventCursor(daemonInfo);
    let reconnectTimer: number | undefined;
    setRealtimeStatus("connecting");
    const unsubscribe = api.subscribeServerEvents({
      token,
      sequence: storedCursor?.sequence,
      onEvent: (message) => {
        const event = message.data;
        if (event.sequence > 0) {
          writeStoredEventCursor(daemonInfo, event.sequence);
        }
        setLatestEvent(event);
        setRealtimeStatus("connected");
        if (realtimeReconnectAttempt > 0) {
          setRealtimeReconnectAttempt(0);
        }
        if (shouldInvalidateForEvent(event, target)) {
          void loadData({ background: true });
        }
      },
      onError: () => {
        setRealtimeStatus("error");
        if (reconnectTimer === undefined) {
          const delay = Math.min(30000, 1000 * 2 ** Math.min(realtimeReconnectAttempt, 5));
          reconnectTimer = window.setTimeout(() => {
            setRealtimeReconnectAttempt((attempt) => attempt + 1);
          }, delay);
        }
      }
    });

    return () => {
      unsubscribe.close();
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer);
      }
    };
  }, [daemonInfo, loadData, realtimeReconnectAttempt, target, token]);

  const handleAuth = (auth: AuthResponse) => {
    api.setToken(auth.token);
    localStorage.setItem(TOKEN_KEY, auth.token);
    setToken(auth.token);
    setUser(auth.user);
  };

  const handleLogout = async () => {
    try {
      if (token) await api.logout();
    } catch {
      // Logout should clear the local session even if the server session expired.
    }
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(EVENT_CURSOR_KEY);
    setToken("");
    setUser(null);
    setSetupStatus(null);
    setDaemonInfo(null);
    setLatestEvent(null);
    setEvents([]);
    setAgentStatuses([]);
    setDaemonRuns([]);
    setDaemonActivity([]);
    setChannels([]);
    setChannelMembers([]);
    setMessages([]);
    setSavedMessages([]);
    setThreadInbox([]);
    setActiveThread(null);
    setReminders([]);
    setSelectedTaskId(null);
    setRealtimeStatus("disabled");
  };

  const openThread = async (item: ThreadInboxItem) => {
    setTarget(item.target);
    setActiveThread(item);
    setView("messages");
    if (item.unreadCount > 0) {
      await api.markThreadRead({ target: item.target, threadId: item.threadId });
    }
  };

  const markThreadRead = async (item: ThreadInboxItem) => {
    await api.markThreadRead({ target: item.target, threadId: item.threadId });
    await loadData();
  };

  const markThreadInboxRead = async () => {
    await api.markThreadInboxRead();
    await loadData();
  };

  if (!token) {
    return <AuthScreen onAuth={handleAuth} />;
  }

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand">
          <img src={brandMarkUrl} alt="" className="brand-mark" />
          <div>
            <strong>Nekode</strong>
            <span>Control Console</span>
          </div>
        </div>
        <nav className="nav-list">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <button
                key={item.key}
                className={view === item.key ? "nav-item is-active" : "nav-item"}
                type="button"
                aria-label={item.label}
                aria-current={view === item.key ? "page" : undefined}
                title={item.label}
                onClick={() => setView(item.key)}
              >
                <Icon size={18} aria-hidden="true" />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>
        <SidebarSection title="Channels" actionLabel="Create channel">
          {(channels.length ? channels : fallbackChannels()).map((channel) => (
            <button
              key={channel.target}
              className={target === channel.target ? "side-link is-active" : "side-link"}
              type="button"
              aria-label={`Open ${channel.displayName || channel.target} messages`}
              onClick={() => {
                setActiveThread(null);
                setTarget(channel.target);
                setView("messages");
              }}
            >
              <Hash size={15} aria-hidden="true" />
              <span>{channel.displayName || channel.target.replace("#", "")}</span>
              {channel.visibility === "private" ? <Shield size={14} aria-label="Private channel" /> : null}
            </button>
          ))}
        </SidebarSection>
        <SidebarSection title="Agents" actionLabel="Create agent">
          {sidebarAgents.length ? sidebarAgents.map((agent) => (
            <button
              key={agent.agentId}
              className="agent-link"
              type="button"
              aria-label={`Open ${agent.label} direct messages`}
              onClick={() => {
                setActiveThread(null);
                setTarget(`dm:${agent.agentId}`);
                setView("messages");
              }}
            >
              <AvatarBadge label={agent.label} color="#2b79b4" />
              <span>{agent.label}</span>
              <span className={`presence ${presenceClass(agent.presence)}`} aria-label={agent.presence} />
            </button>
          )) : demoAgents.map((agent) => (
            <button
              key={agent.id}
              className="agent-link"
              type="button"
              aria-label={`Open ${agent.name} daemon details`}
              onClick={() => setView("daemon")}
            >
              <AvatarBadge label={agent.name} color={agent.color} />
              <span>{agent.name}</span>
              <span className="machine-state">demo</span>
            </button>
          ))}
        </SidebarSection>
        <SidebarSection title="Machines">
          <button className="side-link" type="button" aria-label="Open daemon bridge details" onClick={() => setView("daemon")}>
            <Monitor size={15} aria-hidden="true" />
            <span>Local bridge</span>
            <span className="machine-state">pending</span>
          </button>
        </SidebarSection>
        <div className="user-panel">
          <div>
            <strong>{user?.displayName || user?.username || "Signed in"}</strong>
            <span>{user?.role ?? "member"}</span>
          </div>
          <button className="icon-button" type="button" aria-label="Sign out" onClick={handleLogout}>
            <LogOut size={18} aria-hidden="true" />
          </button>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <p className="eyebrow">Target</p>
            <div className="target-row">
              <input
                aria-label="Current target"
                value={target}
                onChange={(event) => {
                  setActiveThread(null);
                  setTarget(event.target.value);
                }}
                onBlur={() => void loadData()}
              />
              <button className="secondary-button" type="button" onClick={() => void loadData()}>
                <RefreshCw size={16} aria-hidden="true" />
                Refresh
              </button>
            </div>
          </div>
          <StatusPill status={status} />
        </header>

        {error ? (
          <div className="notice error" role="alert">
            {error}
          </div>
        ) : null}
        {status === "loading" ? <SectionStatusNotice loading message="Loading console sections" /> : null}
        {sectionIssues.length ? (
          <SectionIssuesNotice issues={sectionIssues} onRetry={loadData} />
        ) : null}

        {view === "overview" ? (
          <Overview
            protocol={protocol}
            daemonInfo={daemonInfo}
            realtimeStatus={realtimeStatus}
            latestEvent={latestEvent}
            endpoints={endpoints}
            messages={messages}
            tasks={tasks}
            reminders={reminders}
            events={events}
            issues={sectionIssueGroups.overview}
            loading={status === "loading"}
            onRetry={loadData}
          />
        ) : null}
        {view === "inbox" ? (
          <InboxPanel
            items={threadInbox}
            onOpenThread={openThread}
            onMarkRead={markThreadRead}
            onMarkAllRead={markThreadInboxRead}
            issues={sectionIssueGroups.inbox}
            loading={status === "loading" && threadInbox.length === 0}
            onRetry={loadData}
          />
        ) : null}
        {view === "messages" ? (
          <MessagesPanel
            target={activeThread?.target ?? target}
            thread={activeThread}
            messages={messages}
            savedMessages={savedMessages}
            endpoints={endpoints}
            onClearThread={() => setActiveThread(null)}
            onMarkThreadRead={activeThread ? () => markThreadRead(activeThread) : undefined}
            channel={channels.find((item) => item.target === (activeThread?.target ?? target)) ?? null}
            channelMembers={channelMembers}
            onCreated={loadData}
            issues={sectionIssueGroups.messages}
            loading={status === "loading" && messages.length === 0}
            onRetry={loadData}
          />
        ) : null}
        {view === "tasks" ? (
          <TasksPanel
            target={target}
            tasks={tasks}
            daemonRuns={daemonRuns}
            selectedTask={selectedTask}
            onSelectTask={setSelectedTaskId}
            onChanged={loadData}
            issues={sectionIssueGroups.tasks}
            loading={status === "loading" && tasks.length === 0}
            onRetry={loadData}
          />
        ) : null}
        {view === "reminders" ? (
          <RemindersPanel
            target={target}
            reminders={reminders}
            onChanged={loadData}
            issues={sectionIssueGroups.reminders}
            loading={status === "loading" && reminders.length === 0}
            onRetry={loadData}
          />
        ) : null}
        {view === "activity" ? (
          <ActivityPanel
            target={target}
            events={events}
            latestEvent={latestEvent}
            realtimeStatus={realtimeStatus}
            onRefresh={loadData}
            issues={sectionIssueGroups.activity}
            loading={status === "loading" && events.length === 0}
          />
        ) : null}
        {view === "skills" ? <SkillsPanel runtimePresets={runtimePresets} /> : null}
        {view === "settings" ? (
          <SettingsPanel
            user={user}
            setupStatus={setupStatus}
            protocol={protocol}
            daemonInfo={daemonInfo}
            realtimeStatus={realtimeStatus}
            target={target}
            channel={channels.find((item) => item.target === target) ?? null}
            channelMembers={channelMembers}
            endpoints={endpoints}
            runtimePresets={runtimePresets}
            issues={sectionIssueGroups.settings}
            loading={status === "loading"}
            onRetry={loadData}
            onChanged={loadData}
          />
        ) : null}
        {view === "endpoints" ? (
          <EndpointsPanel
            endpoints={endpoints}
            routes={notificationRoutes}
            imProviders={imProviders}
            onCreated={loadData}
            issues={sectionIssueGroups.endpoints}
            loading={status === "loading" && endpoints.length === 0}
            onRetry={loadData}
          />
        ) : null}
        {view === "daemon" ? (
          <DaemonPanel
            protocol={protocol}
            daemonInfo={daemonInfo}
            realtimeStatus={realtimeStatus}
            latestEvent={latestEvent}
            agentStatuses={agentStatuses}
            daemonInventory={daemonInventory}
            daemonRuns={daemonRuns}
            daemonActivity={daemonActivity}
            runtimePresets={runtimePresets}
            onAgentCreated={() => void loadData({ background: true })}
            issues={sectionIssueGroups.daemon}
            loading={status === "loading" && !daemonInfo}
            onRetry={loadData}
          />
        ) : null}
      </main>
    </div>
  );
}

function SidebarSection({
  title,
  actionLabel,
  children
}: {
  title: string;
  actionLabel?: string;
  children: ReactNode;
}) {
  return (
    <section className="sidebar-section">
      <div className="sidebar-section-heading">
        <span>{title}</span>
        {actionLabel ? (
          <button className="mini-button" type="button" aria-label={actionLabel} disabled>
            <Plus size={14} aria-hidden="true" />
          </button>
        ) : null}
      </div>
      <div className="sidebar-section-body">{children}</div>
    </section>
  );
}

function AvatarBadge({ label, color }: { label: string; color: string }) {
  return (
    <span className="avatar-badge" style={{ backgroundColor: color }}>
      {label.slice(0, 1).toUpperCase()}
    </span>
  );
}

function AuthScreen({ onAuth }: { onAuth: (auth: AuthResponse) => void }) {
  const [mode, setMode] = useState<"checking" | "login" | "setup" | "disabled">("checking");
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null);
  const [username, setUsername] = useState("admin");
  const [displayName, setDisplayName] = useState("Admin");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const loadSetupStatus = useCallback(async () => {
    setError("");
    setMode("checking");
    try {
      const status = await api.setupStatus();
      setSetupStatus(status);
      if (status.initialized) {
        setMode("login");
      } else {
        setMode(status.webSetupEnabled ? "setup" : "disabled");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load setup status");
      setMode("login");
    }
  }, []);

  useEffect(() => {
    void loadSetupStatus();
  }, [loadSetupStatus]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (mode === "checking" || mode === "disabled") return;
    if (mode === "setup" && password !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const auth =
        mode === "setup"
          ? await api.init(username, password, displayName)
          : await api.login(username, password);
      onAuth(auth);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Authentication failed");
    } finally {
      setBusy(false);
    }
  };

  const isSetupMode = mode === "setup";

  return (
    <main className="auth-screen">
      <section className="auth-panel">
        <div className="auth-copy">
          <img src={brandMarkUrl} alt="" className="auth-mark" />
          <p className="eyebrow">Nekode Console</p>
          <h1>
            {isSetupMode
              ? "Create the first administrator."
              : "Run a self-hosted Slock-style team surface."}
          </h1>
          <p>
            {isSetupMode
              ? "This server is not initialized yet. The setup wizard creates exactly one admin account, then signs you in."
              : "Manage collaboration targets, messages, tasks, interaction endpoints, and the daemon bridge from one operator-focused console."}
          </p>
          {setupStatus ? (
            <div className="setup-context">
              <span>server_id: {setupStatus.serverId || "pending"}</span>
              <span>data_dir: {setupStatus.dataDir || "unknown"}</span>
            </div>
          ) : null}
        </div>
        <form className="auth-form" onSubmit={submit}>
          <div className="setup-mode-banner">
            <Shield size={18} aria-hidden="true" />
            {mode === "checking"
              ? "Checking server setup"
              : mode === "setup"
                ? "First-run setup"
                : mode === "disabled"
                  ? "Web setup disabled"
                  : "Login"}
          </div>
          {mode === "disabled" ? (
            <>
              <div className="notice" role="status">
                Browser setup is disabled for this server. Ask the operator to provide
                NEKODE_BOOTSTRAP_ADMIN_USERNAME and NEKODE_BOOTSTRAP_ADMIN_PASSWORD through the
                environment, then restart Nekode.
              </div>
              <button
                className="secondary-button"
                type="button"
                onClick={() => void loadSetupStatus()}
              >
                <RefreshCw size={16} aria-hidden="true" />
                Recheck
              </button>
            </>
          ) : null}
          {mode !== "disabled" ? (
            <>
              <label>
                Username
                <input value={username} onChange={(event) => setUsername(event.target.value)} />
              </label>
              {isSetupMode ? (
                <label>
                  Display name
                  <input
                    value={displayName}
                    onChange={(event) => setDisplayName(event.target.value)}
                  />
                </label>
              ) : null}
              <label>
                Password
                <input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  minLength={8}
                />
              </label>
              {isSetupMode ? (
                <label>
                  Confirm password
                  <input
                    type="password"
                    value={confirmPassword}
                    onChange={(event) => setConfirmPassword(event.target.value)}
                    minLength={8}
                  />
                </label>
              ) : null}
              {error ? (
                <div className="notice error" role="alert">
                  {error}
                </div>
              ) : null}
              <button className="primary-button" type="submit" disabled={busy || mode === "checking"}>
                <Shield size={18} aria-hidden="true" />
                {busy ? "Working..." : isSetupMode ? "Create Admin" : "Sign In"}
              </button>
            </>
          ) : null}
        </form>
      </section>
    </main>
  );
}

function StatusPill({ status }: { status: LoadState }) {
  const label = status === "loading" ? "Syncing" : status === "error" ? "Needs Attention" : "Ready";
  return (
    <div className={`status-pill ${status}`}>
      <Wifi size={16} aria-hidden="true" />
      {label}
    </div>
  );
}

function SectionStatusNotice({ loading, message }: { loading: boolean; message: string }) {
  if (!loading) return null;
  return (
    <div className="section-status-notice" role="status">
      <Loader2 size={16} aria-hidden="true" />
      <span>{message}</span>
    </div>
  );
}

function SectionIssuesNotice({
  issues,
  onRetry
}: {
  issues: SectionIssue[];
  onRetry: () => Promise<void>;
}) {
  if (!issues.length) return null;
  const summary = issues.length === 1
    ? `${issues[0].label}: ${issues[0].message}`
    : issues.map((issue) => issue.label).join(", ");
  return (
    <div className="section-issues-notice" role="alert">
      <AlertTriangle size={16} aria-hidden="true" />
      <div>
        <strong>{issues.length === 1 ? "Section needs refresh" : `${issues.length} sections need refresh`}</strong>
        <span>{summary}</span>
      </div>
      <button className="secondary-button" type="button" onClick={() => void onRetry()}>
        <RefreshCw size={16} aria-hidden="true" />
        Retry
      </button>
    </div>
  );
}

function Overview({
  protocol,
  daemonInfo,
  realtimeStatus,
  latestEvent,
  endpoints,
  messages,
  tasks,
  reminders,
  events,
  issues,
  loading,
  onRetry
}: {
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  latestEvent: CollaborationEvent | null;
  endpoints: InteractionEndpoint[];
  messages: Message[];
  tasks: Task[];
  reminders: Reminder[];
  events: CollaborationEvent[];
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const activeTasks = tasks.filter((task) => task.state !== "done" && task.state !== "canceled").length;
  const activeReminders = reminders.filter((reminder) => reminder.status === "active").length;
  return (
    <section className="content-grid">
      <div className="wide section-notice-row">
        <SectionStatusNotice loading={loading} message="Loading overview data" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
      </div>
      <MetricCard icon={Server} label="Protocol" value={protocol?.name ?? "Unknown"} />
      <MetricCard icon={Wifi} label="Realtime" value={realtimeStatus} />
      <MetricCard icon={Settings} label="Endpoints" value={String(endpoints.length)} />
      <MetricCard icon={Columns3} label="Open Tasks" value={String(activeTasks)} />
      <MetricCard icon={Bell} label="Active Reminders" value={String(activeReminders)} />
      <MetricCard icon={Activity} label="Loaded Events" value={String(events.length)} />
      <MetricCard icon={MessageSquare} label="Loaded Messages" value={String(messages.length)} />
      <section className="panel wide">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Bridge Status</p>
            <h2>{daemonInfo ? daemonInfo.serverName || "Daemon bridge connected" : "Daemon bridge unavailable"}</h2>
          </div>
        </div>
        <div className="readiness-list">
          <div className="readiness-item">
            <Server size={18} aria-hidden="true" />
            <span>server_id: {daemonInfo?.serverId || "not loaded"}</span>
          </div>
          <div className="readiness-item">
            <Shield size={18} aria-hidden="true" />
            <span>protocol: {daemonInfo?.protocolVersion ?? "unknown"}</span>
          </div>
          <div className="readiness-item">
            <Wifi size={18} aria-hidden="true" />
            <span>event stream: {realtimeStatus}</span>
          </div>
          <div className="readiness-item">
            <Activity size={18} aria-hidden="true" />
            <span>
              latest event:{" "}
              {latestEvent ? `${latestEvent.kind}/${latestEvent.operation} #${latestEvent.sequence}` : "none"}
            </span>
          </div>
        </div>
        <p className="boundary-note">
          Realtime follows task #107 producer coverage: message/appended/target,
          task created/state_changed/updated/claimed on task scope, and
          activity/created/target trigger refetch. Server DTOs remain authoritative.
        </p>
      </section>
      <section className="panel wide">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Workspace Shape</p>
            <h2>Reference-inspired collaboration layout</h2>
          </div>
        </div>
        <div className="reference-grid">
          <ReferenceItem
            title="Channels, DMs, machines"
            detail="Grouped sidebar structure from Zano keeps navigation predictable as agents and endpoints grow."
          />
          <ReferenceItem
            title="Shared agent roster"
            detail="Open Agent Room's human-agent roster makes runtime status visible before a message is sent."
          />
          <ReferenceItem
            title="Task chat affordance"
            detail="Task cards reserve room for owner/review metadata and quick jump back to conversation."
          />
          <ReferenceItem
            title="Reusable skill shelf"
            detail="Skill Center stays separate from settings so agent setup can attach proven instructions."
          />
        </div>
      </section>
    </section>
  );
}

function ReferenceItem({ title, detail }: { title: string; detail: string }) {
  return (
    <article className="reference-item">
      <CheckCircle2 size={18} aria-hidden="true" />
      <div>
        <strong>{title}</strong>
        <span>{detail}</span>
      </div>
    </article>
  );
}

function MetricCard({
  icon: Icon,
  label,
  value
}: {
  icon: typeof Activity;
  label: string;
  value: string;
}) {
  return (
    <section className="metric-card">
      <Icon size={20} aria-hidden="true" />
      <span>{label}</span>
      <strong>{value}</strong>
    </section>
  );
}

function InboxPanel({
  items,
  onOpenThread,
  onMarkRead,
  onMarkAllRead,
  issues,
  loading,
  onRetry
}: {
  items: ThreadInboxItem[];
  onOpenThread: (item: ThreadInboxItem) => Promise<void>;
  onMarkRead: (item: ThreadInboxItem) => Promise<void>;
  onMarkAllRead: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [busyThread, setBusyThread] = useState("");
  const [actionError, setActionError] = useState("");
  const unreadCount = items.reduce((sum, item) => sum + item.unreadCount, 0);
  const nextUnread = items.find((item) => item.unreadCount > 0) ?? items[0] ?? null;

  const runItemAction = async (item: ThreadInboxItem, action: (item: ThreadInboxItem) => Promise<void>) => {
    setBusyThread(item.threadId);
    setActionError("");
    try {
      await action(item);
    } catch (err) {
      setActionError(errorMessage(err, "Unable to update this thread"));
    } finally {
      setBusyThread("");
    }
  };

  const runBulkAction = async () => {
    setBusyThread("__all__");
    setActionError("");
    try {
      await onMarkAllRead();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to update the inbox"));
    } finally {
      setBusyThread("");
    }
  };

  return (
    <section className="panel inbox-panel">
      <div className="panel-heading">
        <div>
          <p className="eyebrow">Threads</p>
          <h2>Inbox</h2>
        </div>
        <div className="inbox-actions">
          <button
            className="secondary-button"
            type="button"
            disabled={!nextUnread}
            onClick={() => nextUnread && runItemAction(nextUnread, onOpenThread)}
          >
            <Inbox size={16} aria-hidden="true" />
            Next unread
          </button>
          <button className="secondary-button" type="button" disabled={!unreadCount || busyThread === "__all__"} onClick={runBulkAction}>
            <CheckCircle2 size={16} aria-hidden="true" />
            Mark all read
          </button>
        </div>
      </div>
      <SectionStatusNotice loading={loading} message="Loading inbox threads" />
      <SectionIssuesNotice issues={issues} onRetry={onRetry} />
      {actionError ? <p className="inline-error" role="alert">{actionError}</p> : null}
      <div className="inbox-summary" role="status">
        <strong>{items.length}</strong> threads · <strong>{unreadCount}</strong> unread replies
      </div>
      <div className="inbox-list">
        {items.length ? (
          items.map((item) => (
            <article
              key={`${item.target}:${item.threadId}`}
              className={item.unreadCount > 0 ? "inbox-row is-unread" : "inbox-row"}
              onDoubleClick={() => runItemAction(item, onOpenThread)}
            >
              <button className="inbox-row-main" type="button" onClick={() => runItemAction(item, onOpenThread)}>
                <div className="inbox-row-header">
                  <strong>{item.topic || "Untitled thread"}</strong>
                  <span>{unixTime(item.updatedUnix)}</span>
                </div>
                <p>{item.latestMessage.content}</p>
                <div className="inbox-row-meta">
                  <span>{item.target}</span>
                  <span>{item.messageCount} replies</span>
                  <span>{compactId(item.threadId)}</span>
                </div>
              </button>
              <div className="inbox-row-actions">
                {item.unreadCount > 0 ? <span className="unread-badge">{item.unreadCount}</span> : null}
                <button
                  className="icon-button"
                  type="button"
                  aria-label="Mark thread read"
                  disabled={busyThread === item.threadId || item.unreadCount === 0}
                  onClick={(event) => {
                    event.stopPropagation();
                    void runItemAction(item, onMarkRead);
                  }}
                >
                  <CheckCircle2 size={16} aria-hidden="true" />
                </button>
              </div>
            </article>
          ))
        ) : (
          <EmptyState icon={Inbox} title="No active threads" />
        )}
      </div>
    </section>
  );
}

function MessagesPanel({
  target,
  thread,
  messages,
  savedMessages,
  endpoints,
  onClearThread,
  onMarkThreadRead,
  channel,
  channelMembers,
  onCreated,
  issues,
  loading,
  onRetry
}: {
  target: string;
  thread: ThreadInboxItem | null;
  messages: Message[];
  savedMessages: SavedMessage[];
  endpoints: InteractionEndpoint[];
  onClearThread: () => void;
  onMarkThreadRead?: () => Promise<void>;
  channel: Channel | null;
  channelMembers: ChannelMember[];
  onCreated: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [content, setContent] = useState("");
  const [sourceEndpointId, setSourceEndpointId] = useState("");
  const [draftAttachments, setDraftAttachments] = useState<Attachment[]>([]);
  const [selectedMessageIds, setSelectedMessageIds] = useState<Set<string>>(() => new Set());
  const [lightboxAttachment, setLightboxAttachment] = useState<Attachment | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [senderFilter, setSenderFilter] = useState("");
  const [attachmentOnlySearch, setAttachmentOnlySearch] = useState(false);
  const [searchSort, setSearchSort] = useState<"recent" | "relevance">("recent");
  const [searchResults, setSearchResults] = useState<Message[]>([]);
  const [savedQuery, setSavedQuery] = useState("");
  const [savedAttachmentOnly, setSavedAttachmentOnly] = useState(false);
  const [savedDiscovery, setSavedDiscovery] = useState<SavedMessage[]>([]);
  const [replyTo, setReplyTo] = useState<Message | null>(null);
  const [busy, setBusy] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [actionError, setActionError] = useState("");
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => new Set());
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const ordered = useMemo(() => [...messages].reverse(), [messages]);
  const feed = useMemo(() => buildMessageFeed(ordered), [ordered]);
  const selectedMessages = useMemo(
    () => ordered.filter((message) => selectedMessageIds.has(message.id)),
    [ordered, selectedMessageIds]
  );
  const messageById = useMemo(() => new Map(messages.map((message) => [message.id, message])), [messages]);
  const savedIds = useMemo(() => new Set(savedMessages.map((item) => item.messageId)), [savedMessages]);
  const mentionOptions = useMemo(() => {
    const names = new Set<string>();
    for (const message of messages) {
      const name = message.senderDisplayName || message.senderAgentId || message.senderUserId;
      if (name) names.add(name.replace(/^@/, ""));
    }
    for (const agent of demoAgents) names.add(agent.name);
    return [...names].sort((left, right) => left.localeCompare(right));
  }, [messages]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!content.trim() && draftAttachments.length === 0) return;
    setBusy(true);
    setActionError("");
    try {
      await api.createMessage({
        target,
        threadId: thread?.threadId,
        content: content.trim() || "(attachment)",
        attachmentIds: draftAttachments.map((attachment) => attachment.id),
        replyToMessageId: replyTo?.id,
        sourceEndpointId,
        requestId: makeRequestId("msg")
      });
      setContent("");
      setDraftAttachments([]);
      setReplyTo(null);
      await onCreated();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to send message"));
    } finally {
      setBusy(false);
    }
  };

  const uploadFiles = async (files: FileList | null) => {
    if (!files?.length) return;
    setUploading(true);
    setActionError("");
    try {
      const uploaded = await Promise.all(Array.from(files).map((file) => api.uploadAttachment(target, file)));
      setDraftAttachments((current) => [...current, ...uploaded]);
    } catch (err) {
      setActionError(errorMessage(err, "Unable to upload attachment"));
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const toggleSelected = (messageId: string) => {
    setSelectedMessageIds((current) => {
      const next = new Set(current);
      if (next.has(messageId)) {
        next.delete(messageId);
      } else {
        next.add(messageId);
      }
      return next;
    });
  };

  const copySelectedMessages = async () => {
    const text = selectedMessages
      .map((message) => {
        const sender = message.senderDisplayName || message.senderKind;
        const attachments = message.attachments?.length
          ? `\nAttachments: ${message.attachments.map((attachment) => attachment.filename).join(", ")}`
          : "";
        return `[${unixTime(message.createdUnix)}] ${sender}: ${message.content}${attachments}`;
      })
      .join("\n\n");
    setActionError("");
    try {
      await navigator.clipboard.writeText(text);
    } catch (err) {
      setActionError(errorMessage(err, "Unable to copy selected messages"));
    }
  };

  const saveSelectedAsImage = () => {
    if (!selectedMessages.length) return;
    const width = 960;
    const rowHeight = 112;
    const height = Math.max(180, 52 + selectedMessages.length * rowHeight);
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = "#17201d";
    ctx.font = "700 22px Inter, sans-serif";
    ctx.fillText(`${target} selected messages`, 32, 38);
    selectedMessages.forEach((message, index) => {
      const y = 66 + index * rowHeight;
      ctx.fillStyle = "#eef3f1";
      ctx.fillRect(24, y, width - 48, rowHeight - 14);
      ctx.fillStyle = "#17201d";
      ctx.font = "700 16px Inter, sans-serif";
      ctx.fillText(message.senderDisplayName || message.senderKind, 44, y + 30);
      ctx.fillStyle = "#63736d";
      ctx.font = "13px Inter, sans-serif";
      ctx.fillText(unixTime(message.createdUnix), width - 210, y + 30);
      ctx.fillStyle = "#17201d";
      ctx.font = "15px Inter, sans-serif";
      wrapCanvasText(ctx, message.content, 44, y + 58, width - 96, 20, 2);
      if (message.attachments?.length) {
        ctx.fillStyle = "#3c5a7d";
        ctx.fillText(`${message.attachments.length} attachment(s)`, 44, y + 92);
      }
    });
    const link = document.createElement("a");
    link.download = `nekode-${target.replace(/[^a-z0-9]+/gi, "-")}-messages.png`;
    link.href = canvas.toDataURL("image/png");
    link.click();
  };

  const runSearch = async (event: FormEvent) => {
    event.preventDefault();
    const query = searchQuery.trim();
    const sender = senderFilter.trim();
    if (!query && !sender && !attachmentOnlySearch) {
      setSearchResults([]);
      return;
    }
    setBusy(true);
    setActionError("");
    try {
      const results = await api.searchMessages({ query, sender, hasAttachment: attachmentOnlySearch, sort: searchSort, target, limit: 25 });
      setSearchResults(results.items);
    } catch (err) {
      setActionError(errorMessage(err, "Unable to search messages"));
    } finally {
      setBusy(false);
    }
  };

  const discoverSavedMessages = async (event: FormEvent) => {
    event.preventDefault();
    const query = savedQuery.trim();
    if (!query && !savedAttachmentOnly) {
      setSavedDiscovery([]);
      return;
    }
    setBusy(true);
    setActionError("");
    try {
      const results = await api.listSavedMessages(target, 25, { query, hasAttachment: savedAttachmentOnly });
      setSavedDiscovery(results.items);
    } catch (err) {
      setActionError(errorMessage(err, "Unable to search saved messages"));
    } finally {
      setBusy(false);
    }
  };

  const toggleSaved = async (message: Message) => {
    setActionError("");
    try {
      if (savedIds.has(message.id)) {
        await api.unsaveMessage(message.id, target);
      } else {
        await api.saveMessage(message.id, target);
      }
      await onCreated();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to update saved message"));
    }
  };

  const addMention = (name: string) => {
    setContent((current) => `${current}${current && !current.endsWith(" ") ? " " : ""}@${name} `);
  };

  const markCurrentThreadRead = async () => {
    if (!onMarkThreadRead) return;
    setActionError("");
    try {
      await onMarkThreadRead();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to mark thread read"));
    }
  };

  return (
    <section className="two-column">
      <div className="panel message-panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{thread ? `${target} thread` : target}</p>
            <h2>{thread ? thread.topic || "Thread" : "Messages"}</h2>
          </div>
          {thread ? (
            <div className="thread-actions">
              <button className="secondary-button" type="button" onClick={onClearThread}>
                Back to channel
              </button>
              {onMarkThreadRead ? (
                <button className="secondary-button" type="button" onClick={() => void markCurrentThreadRead()}>
                  <CheckCircle2 size={16} aria-hidden="true" />
                  Mark read
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
        <SectionStatusNotice loading={loading} message="Loading messages" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
        {actionError ? <p className="inline-error" role="alert">{actionError}</p> : null}
        <form className="message-search" onSubmit={runSearch}>
          <label className="search-field">
            <Search size={16} aria-hidden="true" />
            <input
              aria-label="Search messages"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              placeholder="Search messages"
            />
          </label>
          <input
            aria-label="Sender filter"
            value={senderFilter}
            onChange={(event) => setSenderFilter(event.target.value)}
            placeholder="@sender"
          />
          <label className="checkbox-line compact-checkbox">
            <input
              type="checkbox"
              aria-label="Only messages with attachments"
              checked={attachmentOnlySearch}
              onChange={(event) => setAttachmentOnlySearch(event.target.checked)}
            />
            Attachments
          </label>
          <select
            aria-label="Search sort"
            value={searchSort}
            onChange={(event) => setSearchSort(event.target.value as "recent" | "relevance")}
          >
            <option value="recent">Recent</option>
            <option value="relevance">Relevance</option>
          </select>
          <button className="secondary-button" type="submit" disabled={busy}>
            <Search size={16} aria-hidden="true" />
            Search
          </button>
        </form>
        {searchResults.length ? (
          <div className="search-results" aria-label="Message search results">
            {searchResults.map((message) => (
              <button key={message.id} type="button" onClick={() => setReplyTo(message)}>
                <strong>{message.senderDisplayName || message.senderKind}</strong>
                <span>{messageSearchSummary(message)}</span>
              </button>
            ))}
          </div>
        ) : null}
        <form className="saved-discovery" onSubmit={discoverSavedMessages}>
          <label className="search-field">
            <Bookmark size={16} aria-hidden="true" />
            <input
              aria-label="Search saved messages"
              value={savedQuery}
              onChange={(event) => setSavedQuery(event.target.value)}
              placeholder="Find saved messages"
            />
          </label>
          <label className="checkbox-line compact-checkbox">
            <input
              type="checkbox"
              aria-label="Only saved messages with attachments"
              checked={savedAttachmentOnly}
              onChange={(event) => setSavedAttachmentOnly(event.target.checked)}
            />
            Attachments
          </label>
          <button className="secondary-button" type="submit" disabled={busy}>
            <Search size={16} aria-hidden="true" />
            Find
          </button>
        </form>
        {savedDiscovery.length ? (
          <div className="saved-message-list discovery-results" aria-label="Saved message search results">
            {savedDiscovery.map((item) => (
              <button key={item.id} type="button" onClick={() => setReplyTo(item.message)}>
                <Bookmark size={14} aria-hidden="true" />
                <span>{messageSearchSummary(item.message)}</span>
              </button>
            ))}
          </div>
        ) : null}
        <div className="message-list" role="log" aria-label="Messages">
          {feed.length ? (
            feed.map((item) => {
              if (item.kind === "system_group") {
                return (
                  <SystemReceiptGroup
                    key={item.id}
                    group={item}
                    expanded={expandedGroups.has(item.id)}
                    onToggle={() =>
                      setExpandedGroups((current) => {
                        const next = new Set(current);
                        if (next.has(item.id)) {
                          next.delete(item.id);
                        } else {
                          next.add(item.id);
                        }
                        return next;
                      })
                    }
                  />
                );
              }
              return (
                <MessageBubble
                  key={item.message.id}
                  message={item.message}
                  selected={selectedMessageIds.has(item.message.id)}
                  onToggleSelected={toggleSelected}
                  onPreviewAttachment={setLightboxAttachment}
                  replyMessage={item.message.replyToMessageId ? messageById.get(item.message.replyToMessageId) : undefined}
                  saved={savedIds.has(item.message.id)}
                  onReply={() => setReplyTo(item.message)}
                  onToggleSaved={() => void toggleSaved(item.message)}
                />
              );
            })
          ) : (
            <EmptyState icon={MessageSquare} title="No messages loaded" />
          )}
        </div>
        {selectedMessages.length > 0 ? (
          <div className="selection-toolbar" aria-label="Selected message actions">
            <span>{selectedMessages.length} selected</span>
            <button className="secondary-button" type="button" onClick={copySelectedMessages}>
              <Copy size={16} aria-hidden="true" />
              Copy
            </button>
            <button className="secondary-button" type="button" onClick={saveSelectedAsImage}>
              <Image size={16} aria-hidden="true" />
              Save image
            </button>
            <button className="icon-button" type="button" aria-label="Clear selection" onClick={() => setSelectedMessageIds(new Set())}>
              <X size={16} aria-hidden="true" />
            </button>
          </div>
        ) : null}
        <form className="composer" onSubmit={submit}>
          {replyTo ? (
            <div className="reply-chip">
              <CornerUpLeft size={14} aria-hidden="true" />
              <span>{replyTo.content}</span>
              <button type="button" aria-label="Clear reply reference" onClick={() => setReplyTo(null)}>
                <CircleX size={14} aria-hidden="true" />
              </button>
            </div>
          ) : null}
          <textarea
            aria-label="Message content"
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder="Message this target"
            rows={3}
          />
          {draftAttachments.length ? (
            <div className="draft-attachments" aria-label="Draft attachments">
              {draftAttachments.map((attachment) => (
                <span key={attachment.id}>
                  <Paperclip size={14} aria-hidden="true" />
                  {attachment.filename}
                  <button
                    type="button"
                    aria-label={`Remove ${attachment.filename}`}
                    onClick={() => setDraftAttachments((current) => current.filter((item) => item.id !== attachment.id))}
                  >
                    <X size={14} aria-hidden="true" />
                  </button>
                </span>
              ))}
            </div>
          ) : null}
          <div className="composer-actions">
            <select
              aria-label="Source endpoint"
              value={sourceEndpointId}
              onChange={(event) => setSourceEndpointId(event.target.value)}
            >
              <option value="">Default source</option>
              {endpoints.map((endpoint) => (
                <option key={endpoint.id} value={endpoint.id}>
                  {endpoint.displayName}
                </option>
              ))}
            </select>
            <div className="composer-actions-right">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                hidden
                onChange={(event) => uploadFiles(event.currentTarget.files)}
              />
              <button className="secondary-button" type="button" onClick={() => fileInputRef.current?.click()} disabled={uploading}>
                <Paperclip size={16} aria-hidden="true" />
                {uploading ? "Uploading" : "Attach"}
              </button>
              <button className="primary-button" type="submit" disabled={busy || (!content.trim() && draftAttachments.length === 0)}>
                <Send size={16} aria-hidden="true" />
                Send
              </button>
            </div>
          </div>
        </form>
      </div>
      {lightboxAttachment ? (
        <AttachmentLightbox attachment={lightboxAttachment} onClose={() => setLightboxAttachment(null)} />
      ) : null}
      <ChannelAccessPanel
        channel={channel}
        members={channelMembers}
        onMention={(name) => setContent((current) => `${current}${current ? " " : ""}@${name} `)}
        savedMessages={savedMessages}
        onSavedSelect={(message) => setReplyTo(message)}
      />
    </section>
  );
}

function ChannelAccessPanel({
  channel,
  members,
  onMention,
  savedMessages,
  onSavedSelect
}: {
  channel: Channel | null;
  members: ChannelMember[];
  onMention: (name: string) => void;
  savedMessages: SavedMessage[];
  onSavedSelect: (message: Message) => void;
}) {
  const visibility = channel?.visibility ?? "public";
  const role = channel?.currentUserRole ?? "member";
  return (
    <aside className="panel compact">
      <div className="panel-heading compact-heading">
        <div>
          <p className="eyebrow">Access</p>
          <h2>Channel permissions</h2>
        </div>
        {visibility === "private" ? <Shield size={18} aria-label="Private channel" /> : <ShieldCheck size={18} aria-label="Public channel" />}
      </div>
      <div className="access-summary">
        <span>{visibility}</span>
        <strong>{role}</strong>
        <small>{channel?.memberCount ?? members.length} member(s)</small>
      </div>
      <div className="agent-roster" aria-label="Channel members">
        {members.length ? (
          members.map((member) => (
            <article className="agent-row" key={`${member.kind}:${member.memberId}`}>
              <AvatarBadge label={member.displayName} color={member.kind === "agent" ? "#2b79b4" : "#146b5a"} />
              <div>
                <strong>{member.displayName}</strong>
                <span>
                  {member.kind} · {member.role}
                </span>
              </div>
            </article>
          ))
        ) : (
          <div className="empty-list">No visible members</div>
        )}
      </div>
      <div className="mention-chips" aria-label="Mention shortcuts">
        {members.map((member) => (
          <button key={member.memberId} type="button" onClick={() => onMention(member.username || member.displayName)}>
            <AtSign size={14} aria-hidden="true" />
            {member.displayName}
          </button>
        ))}
      </div>
      <p className="eyebrow grammar-heading">Saved</p>
      <div className="saved-message-list">
        {savedMessages.length ? (
          savedMessages.map((item) => (
            <button key={item.id} type="button" onClick={() => onSavedSelect(item.message)}>
              <Bookmark size={14} aria-hidden="true" />
              <span>{item.message.content}</span>
            </button>
          ))
        ) : (
          <span className="muted-line">No saved messages</span>
        )}
      </div>
      <p className="eyebrow grammar-heading">Target Grammar</p>
      <dl className="definition-list">
        <div>
          <dt>Channel</dt>
          <dd>#general</dd>
        </div>
        <div>
          <dt>Thread</dt>
          <dd>#general:msgid</dd>
        </div>
        <div>
          <dt>DM</dt>
          <dd>dm:@agent</dd>
        </div>
      </dl>
    </aside>
  );
}

function SystemReceiptGroup({
  group,
  expanded,
  onToggle
}: {
  group: Extract<MessageFeedItem, { kind: "system_group" }>;
  expanded: boolean;
  onToggle: () => void;
}) {
  const first = group.messages[0];
  const last = group.messages[group.messages.length - 1];
  return (
    <section className="system-receipt-group">
      <button type="button" onClick={onToggle} aria-expanded={expanded}>
        <Activity size={16} aria-hidden="true" />
        <span>{group.messages.length} status receipts collapsed</span>
        <small>
          {unixTime(first.createdUnix)} - {unixTime(last.createdUnix)}
        </small>
      </button>
      {expanded ? (
        <div className="system-receipt-items">
          {group.messages.map((message) => (
            <MessageBubble key={message.id} message={message} compact />
          ))}
        </div>
      ) : null}
    </section>
  );
}

function MessageBubble({
  message,
  compact,
  selected,
  onToggleSelected,
  onPreviewAttachment,
  replyMessage,
  saved,
  onReply,
  onToggleSaved
}: {
  message: Message;
  compact?: boolean;
  selected?: boolean;
  onToggleSelected?: (messageId: string) => void;
  onPreviewAttachment?: (attachment: Attachment) => void;
  replyMessage?: Message;
  saved?: boolean;
  onReply?: () => void;
  onToggleSaved?: () => void;
}) {
  const classes = [
    "message-bubble",
    selected ? "is-selected" : "",
    compact ? "is-compact" : "",
    isSystemReceipt(message) ? "is-system" : ""
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <article className={classes}>
      <header>
        {onToggleSelected && !compact ? (
          <button
            className="message-select"
            type="button"
            aria-label={selected ? "Deselect message" : "Select message"}
            onClick={() => onToggleSelected(message.id)}
          >
            {selected ? <CheckSquare size={16} aria-hidden="true" /> : <Square size={16} aria-hidden="true" />}
          </button>
        ) : null}
        <strong>{message.senderDisplayName || message.senderKind}</strong>
        <span>
          <a href={messagePermalink(message)} title="Message permalink">
            <Link2 size={13} aria-hidden="true" />
          </a>
          {unixTime(message.createdUnix)}
        </span>
      </header>
      {replyMessage ? (
        <button className="reference-chip" type="button" onClick={onReply}>
          <CornerUpLeft size={14} aria-hidden="true" />
          <span>{replyMessage.content}</span>
        </button>
      ) : null}
      <p>{message.content}</p>
      {message.attachments?.length && onPreviewAttachment ? (
        <div className="attachment-grid">
          {message.attachments.map((attachment) => (
            <AttachmentPreview key={attachment.id} attachment={attachment} onPreview={onPreviewAttachment} />
          ))}
        </div>
      ) : null}
      <footer className="message-actions">
        <button type="button" onClick={onReply}>
          <CornerUpLeft size={14} aria-hidden="true" />
          Reply
        </button>
        <button type="button" className={saved ? "is-saved" : ""} onClick={onToggleSaved}>
          <Bookmark size={14} aria-hidden="true" />
          {saved ? "Saved" : "Save"}
        </button>
      </footer>
    </article>
  );
}

function AttachmentPreview({
  attachment,
  onPreview
}: {
  attachment: Attachment;
  onPreview: (attachment: Attachment) => void;
}) {
  const [objectUrl, setObjectUrl] = useState("");
  const [textPreview, setTextPreview] = useState("");
  const kind = attachmentPreviewKind(attachment);

  useEffect(() => {
    let disposed = false;
    let url = "";
    setObjectUrl("");
    setTextPreview("");
    if (kind === "image" || kind === "html") {
      api.downloadAttachment(attachment).then((blob) => {
        if (disposed) return;
        url = URL.createObjectURL(blob);
        setObjectUrl(url);
      }).catch(() => undefined);
    } else if (kind === "text") {
      api.downloadAttachment(attachment).then((blob) => blob.text()).then((text) => {
        if (disposed) return;
        const preview = truncateAttachmentText(text, TEXT_ATTACHMENT_PREVIEW_LIMIT);
        setTextPreview(preview.text);
      }).catch(() => undefined);
    }
    return () => {
      disposed = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [attachment, kind]);

  const icon = kind === "image" ? Image : kind === "html" || kind === "text" ? FileText : File;
  const Icon = icon;

  return (
    <div className="attachment-preview">
      <button type="button" onClick={() => onPreview(attachment)} aria-label={`Preview ${attachment.filename}`}>
        {kind === "image" && objectUrl ? (
          <img src={objectUrl} alt={attachment.filename} />
        ) : kind === "html" && objectUrl ? (
          <iframe title={attachment.filename} src={objectUrl} sandbox="" />
        ) : kind === "text" && textPreview ? (
          <pre className="attachment-text-preview">{textPreview}</pre>
        ) : (
          <span className="attachment-file-icon">
            <Icon size={22} aria-hidden="true" />
          </span>
        )}
      </button>
      <div>
        <strong>{attachment.filename}</strong>
        <span>{attachment.mimeType} · {formatBytes(attachment.sizeBytes)}</span>
      </div>
    </div>
  );
}

function messageSearchSummary(message: Message) {
  const attachmentText = message.attachments?.length
    ? ` [${message.attachments.map((attachment) => attachment.filename).join(", ")}]`
    : "";
  return `${message.content}${attachmentText}`;
}

function AttachmentLightbox({ attachment, onClose }: { attachment: Attachment; onClose: () => void }) {
  const [objectUrl, setObjectUrl] = useState("");
  const [textPreview, setTextPreview] = useState("");
  const [textTruncated, setTextTruncated] = useState(false);
  const kind = attachmentPreviewKind(attachment);

  useEffect(() => {
    let disposed = false;
    let url = "";
    setObjectUrl("");
    setTextPreview("");
    setTextTruncated(false);
    api.downloadAttachment(attachment).then((blob) => {
      if (disposed) return;
      if (kind === "text") {
        return blob.text().then((text) => {
          if (disposed) return;
          const preview = truncateAttachmentText(text, TEXT_ATTACHMENT_LIGHTBOX_LIMIT);
          setTextPreview(preview.text);
          setTextTruncated(preview.truncated);
        });
      }
      url = URL.createObjectURL(blob);
      setObjectUrl(url);
    }).catch(() => undefined);
    return () => {
      disposed = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [attachment, kind]);

  const download = async () => {
    const blob = await api.downloadAttachment(attachment);
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = attachment.filename;
    link.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="lightbox-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="lightbox-surface" onClick={(event) => event.stopPropagation()}>
        <div className="lightbox-header">
          <strong>{attachment.filename}</strong>
          <div>
            <button className="icon-button" type="button" aria-label="Download attachment" onClick={download}>
              <Download size={16} aria-hidden="true" />
            </button>
            <button className="icon-button" type="button" aria-label="Close preview" onClick={onClose}>
              <X size={16} aria-hidden="true" />
            </button>
          </div>
        </div>
        {kind === "image" && objectUrl ? (
          <img className="lightbox-image" src={objectUrl} alt={attachment.filename} />
        ) : kind === "html" && objectUrl ? (
          <iframe className="lightbox-frame" title={attachment.filename} src={objectUrl} sandbox="" />
        ) : kind === "text" ? (
          <div className="lightbox-text-shell">
            <pre className="lightbox-text">{textPreview}</pre>
            {textTruncated ? <span className="lightbox-text-note">Preview truncated. Download to view the full file.</span> : null}
          </div>
        ) : (
          <div className="lightbox-file">
            <File size={42} aria-hidden="true" />
            <span>{attachment.mimeType}</span>
          </div>
        )}
      </div>
    </div>
  );
}

function TasksPanel({
  target,
  tasks,
  daemonRuns,
  selectedTask,
  onSelectTask,
  onChanged,
  issues,
  loading,
  onRetry
}: {
  target: string;
  tasks: Task[];
  daemonRuns: DaemonRun[];
  selectedTask: Task | null;
  onSelectTask: (taskId: string | null) => void;
  onChanged: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [summary, setSummary] = useState("");
  const [newTaskState, setNewTaskState] = useState<TaskState>("todo");
  const [viewMode, setViewMode] = useState<TaskViewMode>("board");
  const [stateFilter, setStateFilter] = useState<TaskStateFilter>("all");
  const [sortKey, setSortKey] = useState<TaskSortKey>("updated_desc");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [taskReceipt, setTaskReceipt] = useState<TaskReceipt | null>(null);

  const taskStats = useMemo(
    () => ({
      total: tasks.length,
      open: tasks.filter(isOpenTask).length,
      blocked: tasks.filter((task) => task.state === "blocked").length,
      inReview: tasks.filter((task) => task.state === "in_review").length
    }),
    [tasks]
  );

  const visibleTasks = useMemo(() => {
    const search = query.trim().toLowerCase();
    return tasks
      .filter((task) => {
        if (stateFilter === "open" && !isOpenTask(task)) return false;
        if (stateFilter !== "all" && stateFilter !== "open" && task.state !== stateFilter) {
          return false;
        }
        if (!search) return true;
        return [task.summary, task.id, task.target, task.assigneeId ?? ""]
          .join(" ")
          .toLowerCase()
          .includes(search);
      })
      .sort(compareTasks(sortKey));
  }, [query, sortKey, stateFilter, tasks]);

  const grouped = useMemo(
    () =>
      taskColumns.map((column) => ({
        ...column,
        tasks: visibleTasks.filter((task) => task.state === column.state)
      })),
    [visibleTasks]
  );

  useEffect(() => {
    if (selectedTask && !visibleTasks.some((task) => task.id === selectedTask.id)) {
      onSelectTask(null);
    }
  }, [onSelectTask, selectedTask, visibleTasks]);

  const createTask = async (event: FormEvent) => {
    event.preventDefault();
    if (!summary.trim()) return;
    setBusy(true);
    setActionError("");
    try {
      const createdSummary = summary.trim();
      const createdState = newTaskState;
      const created = await api.createTask({ summary: createdSummary, target, state: createdState });
      setSummary("");
      setNewTaskState("todo");
      setTaskReceipt({
        id: created.id,
        summary: created.summary || createdSummary,
        state: created.state || createdState,
        action: "created",
        createdUnix: Math.floor(Date.now() / 1000)
      });
      await onChanged();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to create task"));
    } finally {
      setBusy(false);
    }
  };

  const moveTask = async (task: Task, state: TaskState) => {
    setBusy(true);
    setActionError("");
    try {
      const updated = await api.updateTask(task.id, { state });
      setTaskReceipt({
        id: updated.id || task.id,
        summary: updated.summary || task.summary,
        state: updated.state || state,
        action: "moved",
        createdUnix: Math.floor(Date.now() / 1000)
      });
      await onChanged();
    } catch (err) {
      setActionError(errorMessage(err, "Unable to move task"));
    } finally {
      setBusy(false);
    }
  };

  const filtersActive = query.trim() !== "" || stateFilter !== "all" || sortKey !== "updated_desc";

  return (
    <section className="task-workspace">
      <div className="panel task-board-panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{target}</p>
            <h2>Task Workspace</h2>
          </div>
          <form className="quick-task-form" onSubmit={createTask}>
            <input
              aria-label="New task summary"
              value={summary}
              onChange={(event) => setSummary(event.target.value)}
              placeholder="New task summary"
            />
            <select
              aria-label="New task state"
              value={newTaskState}
              onChange={(event) => setNewTaskState(event.target.value as TaskState)}
            >
              {taskColumns.map((option) => (
                <option key={option.state} value={option.state}>
                  {option.label}
                </option>
              ))}
            </select>
            <button className="primary-button" type="submit" disabled={busy || !summary.trim()}>
              <Plus size={16} aria-hidden="true" />
              Add
            </button>
          </form>
        </div>
        <SectionStatusNotice loading={loading} message="Loading task board" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
        {actionError ? <p className="inline-error" role="alert">{actionError}</p> : null}
        <div className="task-toolbar" aria-label="Task filters">
          <label className="search-field">
            <Search size={16} aria-hidden="true" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search tasks"
            />
          </label>
          <label className="toolbar-field">
            <ListFilter size={16} aria-hidden="true" />
            <select
              value={stateFilter}
              onChange={(event) => setStateFilter(event.target.value as TaskStateFilter)}
            >
              {taskStateFilters.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label className="toolbar-field">
            Sort
            <select value={sortKey} onChange={(event) => setSortKey(event.target.value as TaskSortKey)}>
              {taskSortOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <div className="segmented view-switch" role="tablist" aria-label="Task view">
            <button
              type="button"
              className={viewMode === "board" ? "is-active" : ""}
              onClick={() => setViewMode("board")}
            >
              <LayoutGrid size={16} aria-hidden="true" />
              Board
            </button>
            <button
              type="button"
              className={viewMode === "list" ? "is-active" : ""}
              onClick={() => setViewMode("list")}
            >
              <List size={16} aria-hidden="true" />
              List
            </button>
          </div>
          <button
            className="secondary-button"
            type="button"
            disabled={!filtersActive}
            onClick={() => {
              setQuery("");
              setStateFilter("all");
              setSortKey("updated_desc");
            }}
          >
            Reset
          </button>
        </div>
        <div className="task-summary-strip" aria-label="Task counts">
          <span>
            Total <strong>{taskStats.total}</strong>
          </span>
          <span>
            Open <strong>{taskStats.open}</strong>
          </span>
          <span>
            Blocked <strong>{taskStats.blocked}</strong>
          </span>
          <span>
            Review <strong>{taskStats.inReview}</strong>
          </span>
          <span>
            Showing <strong>{visibleTasks.length}</strong>
          </span>
        </div>
        <div className="board-note" role="note">
          <AlertTriangle size={16} aria-hidden="true" />
          Column membership comes from server state. Status changes submit to the API and wait for the
          returned DTO/refetch before the board becomes authoritative.
        </div>
        {taskReceipt ? <TaskStatusReceipt receipt={taskReceipt} onDismiss={() => setTaskReceipt(null)} /> : null}
        {viewMode === "board" ? (
          <div className="task-board">
            {grouped.map((column) => (
              <section
                className={`task-column state-${column.state}`}
                key={column.state}
                aria-labelledby={`${column.state}-title`}
              >
                <h3 id={`${column.state}-title`}>
                  <TaskStatusBadge state={column.state} label={column.label} />
                  <span>{column.tasks.length}</span>
                </h3>
                <div className="task-stack">
                  {column.tasks.length ? (
                    column.tasks.map((task) => (
                      <TaskCard
                        key={task.id}
                        task={task}
                        selected={selectedTask?.id === task.id}
                        onSelect={() => onSelectTask(task.id)}
                        onMove={moveTask}
                      />
                    ))
                  ) : (
                    <div className="empty-column">No {column.label.toLowerCase()} tasks</div>
                  )}
                </div>
              </section>
            ))}
          </div>
        ) : (
          <div className="task-list-shell">
            <table className="task-table">
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Status</th>
                  <th>Assignee</th>
                  <th>Updated</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {visibleTasks.map((task) => (
                  <TaskListRow
                    key={task.id}
                    task={task}
                    selected={selectedTask?.id === task.id}
                    onSelect={() => onSelectTask(task.id)}
                    onMove={moveTask}
                  />
                ))}
              </tbody>
            </table>
            {!visibleTasks.length ? <div className="empty-list">No matching tasks</div> : null}
          </div>
        )}
      </div>
      <TaskInspector
        task={selectedTask}
        daemonRuns={daemonRuns}
        onClose={() => onSelectTask(null)}
        onMove={moveTask}
        onChanged={onChanged}
      />
    </section>
  );
}

function TaskStatusReceipt({
  receipt,
  onDismiss
}: {
  receipt: TaskReceipt;
  onDismiss: () => void;
}) {
  const status = taskColumnFor(receipt.state);
  return (
    <div className="task-receipt" role="status">
      <CheckCircle2 size={16} aria-hidden="true" />
      <span>
        Task {receipt.action === "created" ? "created" : "moved"}: <strong>{receipt.summary}</strong>
      </span>
      <TaskStatusBadge state={receipt.state} label={status.label} compact />
      <small>{unixTime(receipt.createdUnix)}</small>
      <button className="icon-button" type="button" aria-label="Dismiss status receipt" onClick={onDismiss}>
        <X size={14} aria-hidden="true" />
      </button>
    </div>
  );
}

function RemindersPanel({
  target,
  reminders,
  onChanged,
  issues,
  loading,
  onRetry
}: {
  target: string;
  reminders: Reminder[];
  onChanged: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [title, setTitle] = useState("");
  const [delayMinutes, setDelayMinutes] = useState(15);
  const [selectedReminderId, setSelectedReminderId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editFireAt, setEditFireAt] = useState("");
  const [logEvents, setLogEvents] = useState<ReminderEvent[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const ordered = useMemo(
    () =>
      [...reminders].sort((left, right) => {
        const leftActive = left.status === "active" ? 0 : 1;
        const rightActive = right.status === "active" ? 0 : 1;
        return leftActive - rightActive || right.updatedUnix - left.updatedUnix;
      }),
    [reminders]
  );
  const selectedReminder = ordered.find((reminder) => reminder.id === selectedReminderId) ?? ordered[0] ?? null;

  const loadLog = useCallback(async (reminder: Reminder | null) => {
    if (!reminder) {
      setLogEvents([]);
      return;
    }
    try {
      const response = await api.listReminderLog(reminder.id);
      setLogEvents(response.items);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load reminder log");
    }
  }, []);

  useEffect(() => {
    if (!selectedReminder) {
      setSelectedReminderId(null);
      setEditTitle("");
      setEditFireAt("");
      setLogEvents([]);
      return;
    }
    if (selectedReminderId !== selectedReminder.id) {
      setSelectedReminderId(selectedReminder.id);
    }
    setEditTitle(selectedReminder.title);
    setEditFireAt(datetimeLocalValue(selectedReminder.nextRunUnix));
    void loadLog(selectedReminder);
  }, [loadLog, selectedReminder, selectedReminderId]);

  const createReminder = async (event: FormEvent) => {
    event.preventDefault();
    if (!title.trim()) return;
    setBusy(true);
    setError("");
    try {
      const created = await api.createReminder({
        target,
        title: title.trim(),
        prompt: title.trim(),
        delaySeconds: Math.max(1, delayMinutes) * 60
      });
      setTitle("");
      setSelectedReminderId(created.id);
      await onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to create reminder");
    } finally {
      setBusy(false);
    }
  };

  const refreshReminders = async () => {
    setBusy(true);
    setError("");
    try {
      await onChanged();
    } catch (err) {
      setError(errorMessage(err, "Unable to refresh reminders"));
    } finally {
      setBusy(false);
    }
  };

  const snoozeReminder = async (reminder: Reminder, minutes: number) => {
    setBusy(true);
    setError("");
    try {
      await api.snoozeReminder(reminder.id, minutes * 60);
      await onChanged();
      await loadLog(reminder);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to snooze reminder");
    } finally {
      setBusy(false);
    }
  };

  const cancelReminder = async (reminder: Reminder) => {
    setBusy(true);
    setError("");
    try {
      await api.cancelReminder(reminder.id, reminder.cancelToken);
      await onChanged();
      await loadLog(reminder);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to cancel reminder");
    } finally {
      setBusy(false);
    }
  };

  const updateReminder = async (event: FormEvent) => {
    event.preventDefault();
    if (!selectedReminder || !editTitle.trim()) return;
    setBusy(true);
    setError("");
    try {
      await api.updateReminder(selectedReminder.id, {
        title: editTitle.trim(),
        fireAt: editFireAt || undefined
      });
      await onChanged();
      await loadLog(selectedReminder);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to update reminder");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="two-column">
      <div className="panel reminder-panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{target}</p>
            <h2>Reminder Lifecycle</h2>
          </div>
          <button className="secondary-button" type="button" onClick={() => void refreshReminders()} disabled={busy}>
            <RefreshCw size={16} aria-hidden="true" />
            Refetch
          </button>
        </div>
        <SectionStatusNotice loading={loading} message="Loading reminders" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
        {error ? (
          <div className="notice error" role="alert">
            {error}
          </div>
        ) : null}
        <form className="reminder-create-form" onSubmit={createReminder}>
          <input
            aria-label="Reminder title"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="Reminder title"
          />
          <label>
            Minutes
            <input
              type="number"
              min={1}
              value={delayMinutes}
              onChange={(event) => setDelayMinutes(Number(event.target.value) || 1)}
            />
          </label>
          <button className="primary-button" type="submit" disabled={busy || !title.trim()}>
            <Bell size={16} aria-hidden="true" />
            Schedule
          </button>
        </form>
        <div className="reminder-list">
          {ordered.length ? (
            ordered.map((reminder) => (
              <article
                className={selectedReminder?.id === reminder.id ? "reminder-row is-selected" : "reminder-row"}
                key={reminder.id}
              >
                <button type="button" onClick={() => setSelectedReminderId(reminder.id)}>
                  <div>
                    <strong>{reminder.title}</strong>
                    <span>{reminder.schedule || reminder.scheduleKind}</span>
                  </div>
                  <span className={`reminder-status status-${reminder.status}`}>{reminder.status}</span>
                  <small>{unixTime(reminder.nextRunUnix)}</small>
                </button>
                <div className="reminder-row-actions">
                  <button
                    className="mini-action-button"
                    type="button"
                    disabled={busy || reminder.status === "canceled"}
                    onClick={() => void snoozeReminder(reminder, 15)}
                  >
                    +15m
                  </button>
                  <button
                    className="mini-action-button"
                    type="button"
                    disabled={busy || reminder.status === "canceled"}
                    onClick={() => void snoozeReminder(reminder, 60)}
                  >
                    +1h
                  </button>
                  <button
                    className="mini-action-button"
                    type="button"
                    disabled={busy || reminder.status === "canceled"}
                    onClick={() => void cancelReminder(reminder)}
                  >
                    Cancel
                  </button>
                </div>
              </article>
            ))
          ) : (
            <EmptyState icon={Bell} title="No reminders scheduled" />
          )}
        </div>
      </div>
      <aside className="panel compact reminder-inspector">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Reminder Detail</p>
            <h2>{selectedReminder?.title ?? "No reminder"}</h2>
          </div>
        </div>
        {selectedReminder ? (
          <>
            <form className="form-stack" onSubmit={updateReminder}>
              <label>
                Title
                <input value={editTitle} onChange={(event) => setEditTitle(event.target.value)} />
              </label>
              <label>
                Next fire
                <input
                  type="datetime-local"
                  value={editFireAt}
                  onChange={(event) => setEditFireAt(event.target.value)}
                />
              </label>
              <button className="primary-button" type="submit" disabled={busy || !editTitle.trim()}>
                <CheckCircle2 size={16} aria-hidden="true" />
                Save
              </button>
            </form>
            <dl className="definition-list">
              <div>
                <dt>ID</dt>
                <dd>{selectedReminder.id}</dd>
              </div>
              <div>
                <dt>Status</dt>
                <dd>{selectedReminder.status}</dd>
              </div>
              <div>
                <dt>Target</dt>
                <dd>{selectedReminder.target}</dd>
              </div>
              <div>
                <dt>Runs</dt>
                <dd>{selectedReminder.runCount}</dd>
              </div>
            </dl>
            <section className="inspector-subsection" aria-label="Reminder log">
              <div className="inspector-subheading">
                <div>
                  <p className="eyebrow">Log</p>
                  <h3>{logEvents.length}</h3>
                </div>
                <button className="icon-button" type="button" aria-label="Refresh reminder log" onClick={() => void loadLog(selectedReminder)}>
                  <RefreshCw size={16} aria-hidden="true" />
                </button>
              </div>
              <div className="timeline-list">
                {logEvents.length ? (
                  logEvents.map((event) => (
                    <article className="timeline-row" key={event.id}>
                      <strong>{event.eventType}</strong>
                      <span>{event.actorType}{event.actorId ? `:${event.actorId}` : ""}</span>
                      <span>{unixTime(event.occurredTimeUnix)} · next {unixTime(event.nextFireTimeUnix)}</span>
                    </article>
                  ))
                ) : (
                  <EmptyState icon={Activity} title="No reminder log" />
                )}
              </div>
            </section>
          </>
        ) : (
          <EmptyState icon={Bell} title="Select a reminder" />
        )}
      </aside>
    </section>
  );
}

function TaskListRow({
  task,
  selected,
  onSelect,
  onMove
}: {
  task: Task;
  selected: boolean;
  onSelect: () => void;
  onMove: (task: Task, state: TaskState) => Promise<void>;
}) {
  return (
    <tr className={selected ? "is-selected" : ""}>
      <td>
        <button className="table-link" type="button" onClick={onSelect}>
          <strong>{task.summary}</strong>
          <span>
            {task.id} · {task.target}
          </span>
        </button>
      </td>
      <td>
        <TaskStatusBadge state={task.state} />
      </td>
      <td>{task.assigneeId || "unassigned"}</td>
      <td>{unixTime(task.updatedUnix)}</td>
      <td>
        <TaskMoveActions task={task} onMove={onMove} />
      </td>
    </tr>
  );
}

function TaskCard({
  task,
  selected,
  onSelect,
  onMove
}: {
  task: Task;
  selected: boolean;
  onSelect: () => void;
  onMove: (task: Task, state: TaskState) => Promise<void>;
}) {
  return (
    <article className={`task-card state-${task.state}${selected ? " is-selected" : ""}`} key={task.id}>
      <button className="task-card-open" type="button" onClick={onSelect} aria-label={`Open ${task.summary}`}>
        <div className="task-card-title">
          <TaskStatusBadge state={task.state} compact />
          <p>{task.summary}</p>
        </div>
        <span>
          {task.id} · {unixTime(task.updatedUnix)}
        </span>
        {task.assigneeId ? <span>Assignee: {task.assigneeId}</span> : null}
      </button>
      <TaskMoveActions task={task} onMove={onMove} />
    </article>
  );
}

function TaskMoveActions({
  task,
  onMove
}: {
  task: Task;
  onMove: (task: Task, state: TaskState) => Promise<void>;
}) {
  const canMarkDone = task.state !== "done";
  const canReopen = task.state === "done" || task.state === "canceled";
  return (
    <div className="task-actions">
      <select
        aria-label={`Move ${task.summary}`}
        value={task.state}
        onChange={(event) => void onMove(task, event.target.value as TaskState)}
      >
        {taskColumns.map((option) => (
          <option key={option.state} value={option.state}>
            {option.label}
          </option>
        ))}
      </select>
      <button
        className="mini-action-button"
        type="button"
        disabled={!canMarkDone}
        onClick={() => void onMove(task, "done")}
      >
        Done
      </button>
      <button
        className="mini-action-button"
        type="button"
        disabled={!canReopen}
        onClick={() => void onMove(task, "todo")}
      >
        Reopen
      </button>
    </div>
  );
}

function TaskInspector({
  task,
  daemonRuns,
  onClose,
  onMove,
  onChanged
}: {
  task: Task | null;
  daemonRuns: DaemonRun[];
  onClose: () => void;
  onMove: (task: Task, state: TaskState) => Promise<void>;
  onChanged: () => Promise<void>;
}) {
  const [summaryDraft, setSummaryDraft] = useState("");
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const [blockedReasonDraft, setBlockedReasonDraft] = useState("");
  const [commentDraft, setCommentDraft] = useState("");
  const [comments, setComments] = useState<Message[]>([]);
  const [timeline, setTimeline] = useState<CollaborationEvent[]>([]);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [savingDetail, setSavingDetail] = useState(false);
  const [detailError, setDetailError] = useState("");

  const loadTaskDetail = useCallback(async () => {
    if (!task) {
      setComments([]);
      setTimeline([]);
      return;
    }
    setLoadingDetail(true);
    setDetailError("");
    try {
      const [commentList, timelineList] = await Promise.all([
        api.listTaskComments(task.id),
        api.listTaskTimeline(task.id)
      ]);
      setComments(commentList.items);
      setTimeline(timelineList.items);
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : "Unable to load task detail");
    } finally {
      setLoadingDetail(false);
    }
  }, [task]);

  useEffect(() => {
    setSummaryDraft(task?.summary ?? "");
    setDescriptionDraft(task?.description ?? "");
    setBlockedReasonDraft(task?.blockedReason ?? "");
    setCommentDraft("");
    void loadTaskDetail();
  }, [loadTaskDetail, task?.blockedReason, task?.description, task?.summary]);

  if (!task) {
    return (
      <aside className="panel task-inspector">
        <EmptyState icon={Columns3} title="Select a task to inspect" />
      </aside>
    );
  }

  const taskRuns = daemonRuns
    .filter((run) => run.taskId === task.id)
    .sort((a, b) => (b.updatedTimeUnix ?? b.startedTimeUnix ?? 0) - (a.updatedTimeUnix ?? a.startedTimeUnix ?? 0));
  const latestTaskRun = taskRuns[0];

  const saveDetail = async (event: FormEvent) => {
    event.preventDefault();
    if (!summaryDraft.trim()) return;
    setSavingDetail(true);
    setDetailError("");
    try {
      await api.updateTask(task.id, {
        summary: summaryDraft,
        description: descriptionDraft,
        blockedReason: blockedReasonDraft
      });
      await onChanged();
      await loadTaskDetail();
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : "Unable to save task detail");
    } finally {
      setSavingDetail(false);
    }
  };

  const addComment = async (event: FormEvent) => {
    event.preventDefault();
    if (!commentDraft.trim()) return;
    setSavingDetail(true);
    setDetailError("");
    try {
      await api.createTaskComment(task.id, {
        content: commentDraft,
        requestId: makeRequestId("task-comment")
      });
      setCommentDraft("");
      await loadTaskDetail();
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : "Unable to add task comment");
    } finally {
      setSavingDetail(false);
    }
  };

  return (
    <aside className="panel task-inspector">
      <div className="panel-heading compact-heading">
        <div>
          <p className="eyebrow">Task Detail</p>
          <h2>{task.summary}</h2>
        </div>
        <button className="icon-button" type="button" aria-label="Close task detail" onClick={onClose}>
          <CircleX size={18} aria-hidden="true" />
        </button>
      </div>
      <div className="inspector-section">
        <TaskStatusBadge state={task.state} />
        <select
          aria-label={`Change state for ${task.summary}`}
          value={task.state}
          onChange={(event) => void onMove(task, event.target.value as TaskState)}
        >
          {taskColumns.map((option) => (
            <option key={option.state} value={option.state}>
              {option.label}
            </option>
          ))}
        </select>
      </div>
      {task.state === "blocked" && task.blockedReason ? (
        <div className="blocked-reason" role="note">
          <OctagonAlert size={16} aria-hidden="true" />
          <span>{task.blockedReason}</span>
        </div>
      ) : null}
      {detailError ? (
        <div className="notice error" role="alert">
          {detailError}
        </div>
      ) : null}
      <form className="task-detail-form" onSubmit={saveDetail}>
        <label>
          Summary
          <input
            value={summaryDraft}
            onChange={(event) => setSummaryDraft(event.target.value)}
            placeholder="Task summary"
          />
        </label>
        <label>
          Description
          <textarea
            value={descriptionDraft}
            onChange={(event) => setDescriptionDraft(event.target.value)}
            placeholder="Capture scope, acceptance notes, and links"
            rows={5}
          />
        </label>
        <label>
          Blocked reason
          <textarea
            value={blockedReasonDraft}
            onChange={(event) => setBlockedReasonDraft(event.target.value)}
            placeholder="Visible when the task is blocked"
            rows={3}
          />
        </label>
        <button className="primary-button" type="submit" disabled={savingDetail || !summaryDraft.trim()}>
          <CheckCircle2 size={16} aria-hidden="true" />
          Save Detail
        </button>
      </form>
      <dl className="definition-list">
        <div>
          <dt>ID</dt>
          <dd>{task.id}</dd>
        </div>
        <div>
          <dt>Target</dt>
          <dd>{task.target}</dd>
        </div>
        <div>
          <dt>Assignee</dt>
          <dd>{task.assigneeId || "unassigned"}</dd>
        </div>
        <div>
          <dt>Claim lease</dt>
          <dd>{task.claimLeaseId || "none"}</dd>
        </div>
        <div>
          <dt>Version</dt>
          <dd>{task.version ?? "unknown"}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{unixTime(task.createdUnix)}</dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>{unixTime(task.updatedUnix)}</dd>
        </div>
      </dl>
      <section className="inspector-subsection" aria-label="Task execution">
        <div className="inspector-subheading">
          <div>
            <p className="eyebrow">Execution</p>
            <h3>{taskRuns.length ? `${taskRuns.length} run${taskRuns.length === 1 ? "" : "s"}` : "No run"}</h3>
          </div>
        </div>
        {latestTaskRun ? (
          <dl className="definition-list">
            <div>
              <dt>Run</dt>
              <dd>{latestTaskRun.runId}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>
                <span className={`diagnostic-badge ${healthClass(latestTaskRun.state)}`}>{latestTaskRun.state}</span>
              </dd>
            </div>
            <div>
              <dt>Agent</dt>
              <dd>{latestTaskRun.agentId || task.assigneeId || "unassigned"}</dd>
            </div>
            <div>
              <dt>Runtime</dt>
              <dd>{latestTaskRun.runtimeProfileId || "unknown"}</dd>
            </div>
            <div>
              <dt>Updated</dt>
              <dd>{formatUnixTime(latestTaskRun.updatedTimeUnix || latestTaskRun.completedTimeUnix || latestTaskRun.startedTimeUnix)}</dd>
            </div>
            <div>
              <dt>Feedback</dt>
              <dd>{latestTaskRun.error || latestTaskRun.summary || "No daemon feedback yet"}</dd>
            </div>
          </dl>
        ) : (
          <EmptyState icon={Activity} title="No execution run linked to this task" />
        )}
      </section>
      <section className="inspector-subsection" aria-label="Task comments">
        <div className="inspector-subheading">
          <div>
            <p className="eyebrow">Comments</p>
            <h3>{comments.length}</h3>
          </div>
          <button className="icon-button" type="button" aria-label="Refresh task detail" onClick={() => void loadTaskDetail()}>
            <RefreshCw size={16} aria-hidden="true" />
          </button>
        </div>
        <form className="comment-form" onSubmit={addComment}>
          <textarea
            value={commentDraft}
            onChange={(event) => setCommentDraft(event.target.value)}
            placeholder="Add a task comment"
            rows={3}
          />
          <button className="secondary-button" type="submit" disabled={savingDetail || !commentDraft.trim()}>
            <Send size={16} aria-hidden="true" />
            Send
          </button>
        </form>
        <div className="comment-list">
          {comments.length ? (
            comments.map((comment) => <MessageBubble key={comment.id} message={comment} />)
          ) : (
            <EmptyState icon={MessageSquare} title={loadingDetail ? "Loading comments" : "No comments yet"} />
          )}
        </div>
      </section>
      <section className="inspector-subsection" aria-label="Task timeline">
        <div className="inspector-subheading">
          <div>
            <p className="eyebrow">Timeline</p>
            <h3>{timeline.length}</h3>
          </div>
        </div>
        <div className="timeline-list">
          {timeline.length ? (
            timeline.map((event) => (
              <article className="timeline-row" key={event.eventId || event.sequence}>
                <strong>{eventSummary(event)}</strong>
                <span>{eventScopeLabel(event)}</span>
                <span>
                  seq {event.sequence || "n/a"} · {unixTime(event.createdTimeUnix)}
                </span>
              </article>
            ))
          ) : (
            <EmptyState icon={Activity} title={loadingDetail ? "Loading timeline" : "No timeline events"} />
          )}
        </div>
      </section>
      <p className="boundary-note">
        This panel submits mutations through the API and waits for server DTO/refetch. It does not
        treat local state changes or SSE payloads as authoritative facts.
      </p>
    </aside>
  );
}

function TaskStatusBadge({
  state,
  label,
  compact = false
}: {
  state: TaskState;
  label?: string;
  compact?: boolean;
}) {
  const column = taskColumnFor(state);
  const Icon = column.icon;
  return (
    <span className={`task-status state-${state}`} title={column.label}>
      <Icon size={compact ? 14 : 16} aria-hidden="true" />
      {compact ? null : <span>{label ?? column.label}</span>}
    </span>
  );
}

function ActivityPanel({
  target,
  events,
  latestEvent,
  realtimeStatus,
  onRefresh,
  issues,
  loading
}: {
  target: string;
  events: CollaborationEvent[];
  latestEvent: CollaborationEvent | null;
  realtimeStatus: RealtimeStatus;
  onRefresh: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
}) {
  const ordered = useMemo(() => [...events].sort((a, b) => b.sequence - a.sequence), [events]);
  const [refreshError, setRefreshError] = useState("");

  const refreshActivity = async () => {
    setRefreshError("");
    try {
      await onRefresh();
    } catch (err) {
      setRefreshError(errorMessage(err, "Unable to refresh activity"));
    }
  };

  return (
    <section className="two-column">
      <div className="panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{target}</p>
            <h2>Activity Stream</h2>
          </div>
          <button className="secondary-button" type="button" onClick={() => void refreshActivity()}>
            <RefreshCw size={16} aria-hidden="true" />
            Refetch
          </button>
        </div>
        <SectionStatusNotice loading={loading} message="Loading activity stream" />
        <SectionIssuesNotice issues={issues} onRetry={refreshActivity} />
        {refreshError ? <p className="inline-error" role="alert">{refreshError}</p> : null}
        <div className="board-note" role="note">
          <Activity size={16} aria-hidden="true" />
          Events are routing signals. The UI refetches authoritative server DTOs instead of moving
          tasks or messages directly from event payloads.
        </div>
        <div className="event-stream" role="log" aria-label="Collaboration events">
          {ordered.length ? (
            ordered.map((event) => <EventRow event={event} key={event.eventId || event.sequence} />)
          ) : (
            <EmptyState icon={Activity} title="No durable events loaded" />
          )}
        </div>
      </div>
      <aside className="panel compact">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Realtime</p>
            <h2>SSE Signal</h2>
          </div>
        </div>
        <dl className="definition-list">
          <div>
            <dt>Status</dt>
            <dd>{realtimeStatus}</dd>
          </div>
          <div>
            <dt>Latest signal</dt>
            <dd>{latestEvent ? eventSummary(latestEvent) : "none"}</dd>
          </div>
          <div>
            <dt>Sequence</dt>
            <dd>{latestEvent?.sequence ?? "none"}</dd>
          </div>
          <div>
            <dt>Scope</dt>
            <dd>{latestEvent ? eventScopeLabel(latestEvent) : "none"}</dd>
          </div>
        </dl>
        <p className="boundary-note">
          Stable invalidation coverage: message/appended/target,
          activity/created/target, and task created/state_changed/updated/claimed on task scope.
        </p>
      </aside>
    </section>
  );
}

function EventRow({ event }: { event: CollaborationEvent }) {
  return (
    <article className="event-row">
      <div className="event-main">
        <span className={`event-kind event-${event.kind}`}>{event.kind}</span>
        <div>
          <strong>{event.operation}</strong>
          <span>{eventScopeLabel(event)}</span>
        </div>
      </div>
      <div className="event-meta">
        <span>seq {event.sequence || "n/a"}</span>
        {event.aggregateId ? <span>{event.aggregateId}</span> : null}
        {event.protocolVersion ? <span>proto {event.protocolVersion}</span> : null}
      </div>
    </article>
  );
}

function SettingsPanel({
  user,
  setupStatus,
  protocol,
  daemonInfo,
  realtimeStatus,
  target,
  channel,
  channelMembers,
  endpoints,
  runtimePresets,
  issues,
  loading,
  onRetry,
  onChanged
}: {
  user: User | null;
  setupStatus: SetupStatus | null;
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  target: string;
  channel: Channel | null;
  channelMembers: ChannelMember[];
  endpoints: InteractionEndpoint[];
  runtimePresets: RuntimePreset[];
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
  onChanged: () => Promise<void>;
}) {
  const userRole = (user?.role || "member").toLowerCase();
  const channelRole = channel?.currentUserRole || "member";
  const visibility = channel?.visibility || "public";
  const recommendedRuntimes = runtimePresets.filter((preset) => preset.recommended).length;
  const writableEndpoints = endpoints.filter((endpoint) => endpoint.outboundEnabled).length;
  const readableEndpoints = endpoints.filter((endpoint) => endpoint.inboundEnabled).length;
  const canManageChannel = channelRole === "admin";
  const canCreateChannels = userRole === "admin" || userRole === "owner";
  const [channelTargetDraft, setChannelTargetDraft] = useState("");
  const [newChannelNameDraft, setNewChannelNameDraft] = useState("");
  const [newChannelVisibilityDraft, setNewChannelVisibilityDraft] = useState("public");
  const [currentChannelNameDraft, setCurrentChannelNameDraft] = useState("");
  const [currentChannelVisibilityDraft, setCurrentChannelVisibilityDraft] = useState("public");
  const [memberKindDraft, setMemberKindDraft] = useState("human");
  const [memberIdDraft, setMemberIdDraft] = useState("");
  const [memberDisplayNameDraft, setMemberDisplayNameDraft] = useState("");
  const [memberUsernameDraft, setMemberUsernameDraft] = useState("");
  const [memberRoleDraft, setMemberRoleDraft] = useState("member");
  const [memberRoleEdits, setMemberRoleEdits] = useState<Record<string, string>>({});
  const [channelBusy, setChannelBusy] = useState(false);
  const [channelActionError, setChannelActionError] = useState("");
  const [channelActionReceipt, setChannelActionReceipt] = useState("");

  useEffect(() => {
    setCurrentChannelNameDraft(channel?.displayName || "");
    setCurrentChannelVisibilityDraft(channel?.visibility || "public");
    setMemberRoleEdits({});
    setChannelActionError("");
    setChannelActionReceipt("");
  }, [channel?.target, channel?.displayName, channel?.visibility]);

  const runChannelMutation = async (fallback: string, action: () => Promise<string | void>) => {
    setChannelBusy(true);
    setChannelActionError("");
    setChannelActionReceipt("");
    try {
      const receipt = await action();
      if (receipt) setChannelActionReceipt(receipt);
      await onChanged();
    } catch (err) {
      setChannelActionError(errorMessage(err, fallback));
    } finally {
      setChannelBusy(false);
    }
  };

  const createManagedChannel = async (event: FormEvent) => {
    event.preventDefault();
    await runChannelMutation("Unable to create channel", async () => {
      const created = await api.createChannel({
        target: channelTargetDraft,
        displayName: newChannelNameDraft,
        visibility: newChannelVisibilityDraft
      });
      setChannelTargetDraft("");
      setNewChannelNameDraft("");
      setNewChannelVisibilityDraft("public");
      return `Created ${created.target}`;
    });
  };

  const updateManagedChannel = async (event: FormEvent) => {
    event.preventDefault();
    if (!channel) return;
    await runChannelMutation("Unable to update channel", async () => {
      const updated = await api.updateChannel(channel.target, {
        displayName: currentChannelNameDraft,
        visibility: currentChannelVisibilityDraft
      });
      return `Updated ${updated.target}`;
    });
  };

  const deleteManagedChannel = async () => {
    if (!channel) return;
    await runChannelMutation("Unable to delete channel", async () => {
      await api.deleteChannel(channel.target);
      return `Deleted ${channel.target}`;
    });
  };

  const addManagedMember = async (event: FormEvent) => {
    event.preventDefault();
    if (!channel) return;
    await runChannelMutation("Unable to add channel member", async () => {
      const member = await api.upsertChannelMember(channel.target, {
        memberId: memberIdDraft,
        username: memberUsernameDraft,
        displayName: memberDisplayNameDraft,
        kind: memberKindDraft,
        role: memberRoleDraft
      });
      setMemberIdDraft("");
      setMemberUsernameDraft("");
      setMemberDisplayNameDraft("");
      setMemberKindDraft("human");
      setMemberRoleDraft("member");
      return `Saved ${member.displayName}`;
    });
  };

  const updateManagedMemberRole = async (member: ChannelMember) => {
    if (!channel) return;
    const role = memberRoleEdits[`${member.kind}:${member.memberId}`] || String(member.role);
    await runChannelMutation("Unable to update member role", async () => {
      const updated = await api.updateChannelMember(channel.target, member.kind, member.memberId, { role });
      return `Updated ${updated.displayName}`;
    });
  };

  const removeManagedMember = async (member: ChannelMember) => {
    if (!channel) return;
    await runChannelMutation("Unable to remove member", async () => {
      await api.deleteChannelMember(channel.target, member.kind, member.memberId);
      return `Removed ${member.displayName}`;
    });
  };

  return (
    <section className="content-grid settings-grid">
      <div className="wide section-notice-row">
        <SectionStatusNotice loading={loading} message="Loading settings posture" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
      </div>
      <MetricCard icon={ShieldCheck} label="Account Role" value={userRole} />
      <MetricCard icon={Hash} label="Active Target" value={target} />
      <MetricCard icon={UsersRound} label="Visible Members" value={String(channelMembers.length)} />
      <MetricCard icon={Bot} label="Runtime Presets" value={String(runtimePresets.length)} />

      <section className="panel wide">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Setup</p>
            <h2>Server readiness</h2>
          </div>
          <StatusDot active={Boolean(setupStatus?.initialized)} label={setupStatus?.initialized ? "Initialized" : "Pending"} />
        </div>
        <div className="settings-posture-grid">
          <article className="settings-posture-item">
            <Server size={18} aria-hidden="true" />
            <div>
              <strong>{setupStatus?.serverId || daemonInfo?.serverId || "server pending"}</strong>
              <span>server_id</span>
            </div>
          </article>
          <article className="settings-posture-item">
            <Shield size={18} aria-hidden="true" />
            <div>
              <strong>
                {setupStatus ? (setupStatus.webSetupEnabled ? "enabled" : "disabled") : "unknown"}
              </strong>
              <span>web setup</span>
            </div>
          </article>
          <article className="settings-posture-item">
            <FileText size={18} aria-hidden="true" />
            <div>
              <strong>{setupStatus?.bootstrapMethods?.join(", ") || "password"}</strong>
              <span>bootstrap methods</span>
            </div>
          </article>
          <article className="settings-posture-item">
            <Monitor size={18} aria-hidden="true" />
            <div>
              <strong>{setupStatus?.dataDir || "not reported"}</strong>
              <span>data dir</span>
            </div>
          </article>
        </div>
      </section>

      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Permissions</p>
            <h2>Current access posture</h2>
          </div>
        </div>
        <div className="settings-access-grid">
          <article>
            <span className={`diagnostic-badge ${roleClass(userRole)}`}>{userRole}</span>
            <strong>{user?.displayName || user?.username || "Signed in user"}</strong>
            <small>server account</small>
          </article>
          <article>
            <span className={`diagnostic-badge ${visibility === "private" ? "is-warn" : "is-ok"}`}>{visibility}</span>
            <strong>{channel?.displayName || target}</strong>
            <small>{channel?.memberCount ?? channelMembers.length} channel member(s)</small>
          </article>
          <article>
            <span className={`diagnostic-badge ${roleClass(String(channelRole))}`}>{channelRole}</span>
            <strong>target role</strong>
            <small>{visibility === "private" ? "membership gated" : "public target"}</small>
          </article>
        </div>
        <div className="settings-member-list">
          {channelMembers.length ? (
            channelMembers.slice(0, 12).map((member) => (
              <article className="agent-row" key={`${member.kind}:${member.memberId}`}>
                <AvatarBadge label={member.displayName} color={member.kind === "agent" ? "#2b79b4" : "#146b5a"} />
                <div>
                  <strong>{member.displayName}</strong>
                  <span>
                    {member.kind} · {member.role}
                  </span>
                </div>
              </article>
            ))
          ) : (
            <EmptyState icon={UsersRound} title="No visible members for this target" />
          )}
        </div>
      </section>

      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Channel Admin</p>
            <h2>Management</h2>
          </div>
          <span className={`diagnostic-badge ${canManageChannel ? "is-ok" : "is-idle"}`}>
            {canManageChannel ? "editable" : "read only"}
          </span>
        </div>
        {channelActionError ? <p className="inline-error" role="alert">{channelActionError}</p> : null}
        {channelActionReceipt ? <p className="inline-success" role="status">{channelActionReceipt}</p> : null}
        <div className="channel-admin-grid">
          <form className="form-stack" onSubmit={createManagedChannel}>
            <p className="eyebrow">Create</p>
            <label>
              Target
              <input
                value={channelTargetDraft}
                onChange={(event) => setChannelTargetDraft(event.target.value)}
                placeholder="#team-ops"
                disabled={!canCreateChannels}
              />
            </label>
            <label>
              Display name
              <input
                value={newChannelNameDraft}
                onChange={(event) => setNewChannelNameDraft(event.target.value)}
                placeholder="Team Ops"
                disabled={!canCreateChannels}
              />
            </label>
            <label>
              Visibility
              <select
                value={newChannelVisibilityDraft}
                onChange={(event) => setNewChannelVisibilityDraft(event.target.value)}
                disabled={!canCreateChannels}
              >
                <option value="public">Public</option>
                <option value="private">Private</option>
              </select>
            </label>
            <button className="secondary-button" type="submit" disabled={channelBusy || !canCreateChannels || !channelTargetDraft.trim()}>
              <Plus size={16} aria-hidden="true" />
              Create
            </button>
          </form>
          <form className="form-stack" onSubmit={updateManagedChannel}>
            <p className="eyebrow">Current Channel</p>
            <label>
              Display name
              <input
                value={currentChannelNameDraft}
                onChange={(event) => setCurrentChannelNameDraft(event.target.value)}
                disabled={!channel || !canManageChannel}
              />
            </label>
            <label>
              Visibility
              <select
                value={currentChannelVisibilityDraft}
                onChange={(event) => setCurrentChannelVisibilityDraft(event.target.value)}
                disabled={!channel || !canManageChannel}
              >
                <option value="public">Public</option>
                <option value="private">Private</option>
              </select>
            </label>
            <div className="enrollment-actions">
              <button className="primary-button" type="submit" disabled={channelBusy || !channel || !canManageChannel}>
                <CheckCircle2 size={16} aria-hidden="true" />
                Save
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={() => void deleteManagedChannel()}
                disabled={channelBusy || !channel || !canManageChannel}
              >
                <X size={16} aria-hidden="true" />
                Delete
              </button>
            </div>
          </form>
          <form className="form-stack" onSubmit={addManagedMember}>
            <p className="eyebrow">Members</p>
            <div className="channel-member-form-grid">
              <label>
                Kind
                <select
                  value={memberKindDraft}
                  onChange={(event) => setMemberKindDraft(event.target.value)}
                  disabled={!channel || !canManageChannel}
                >
                  <option value="human">Human</option>
                  <option value="agent">Agent</option>
                </select>
              </label>
              <label>
                Role
                <select
                  value={memberRoleDraft}
                  onChange={(event) => setMemberRoleDraft(event.target.value)}
                  disabled={!channel || !canManageChannel}
                >
                  <option value="admin">Admin</option>
                  <option value="member">Member</option>
                  <option value="viewer">Viewer</option>
                </select>
              </label>
            </div>
            <label>
              Member ID
              <input
                value={memberIdDraft}
                onChange={(event) => setMemberIdDraft(event.target.value)}
                placeholder={memberKindDraft === "agent" ? "agent:reviewer" : "usr_..."}
                disabled={!channel || !canManageChannel}
              />
            </label>
            <label>
              Display name
              <input
                value={memberDisplayNameDraft}
                onChange={(event) => setMemberDisplayNameDraft(event.target.value)}
                placeholder="Review Agent"
                disabled={!channel || !canManageChannel}
              />
            </label>
            <label>
              Username
              <input
                value={memberUsernameDraft}
                onChange={(event) => setMemberUsernameDraft(event.target.value)}
                placeholder="optional for humans"
                disabled={!channel || !canManageChannel}
              />
            </label>
            <button className="secondary-button" type="submit" disabled={channelBusy || !channel || !canManageChannel || !memberIdDraft.trim()}>
              <Plus size={16} aria-hidden="true" />
              Add member
            </button>
          </form>
        </div>
        <div className="channel-member-management-list">
          {channelMembers.length ? channelMembers.map((member) => {
            const key = `${member.kind}:${member.memberId}`;
            return (
              <article className="channel-member-management-row" key={key}>
                <div>
                  <strong>{member.displayName}</strong>
                  <span>{member.kind} · {member.memberId}</span>
                </div>
                <select
                  value={memberRoleEdits[key] || String(member.role)}
                  onChange={(event) => setMemberRoleEdits((current) => ({ ...current, [key]: event.target.value }))}
                  disabled={!canManageChannel || channelBusy}
                  aria-label={`Role for ${member.displayName}`}
                >
                  <option value="admin">Admin</option>
                  <option value="member">Member</option>
                  <option value="viewer">Viewer</option>
                </select>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => void updateManagedMemberRole(member)}
                  disabled={!canManageChannel || channelBusy}
                >
                  Save role
                </button>
                <button
                  className="icon-button"
                  type="button"
                  aria-label={`Remove ${member.displayName}`}
                  onClick={() => void removeManagedMember(member)}
                  disabled={!canManageChannel || channelBusy}
                >
                  <X size={16} aria-hidden="true" />
                </button>
              </article>
            );
          }) : <EmptyState icon={UsersRound} title="No members to manage" />}
        </div>
      </section>

      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Configuration</p>
            <h2>Runtime and bridge visibility</h2>
          </div>
        </div>
        <dl className="definition-list settings-definition-list">
          <div>
            <dt>Protocol</dt>
            <dd>{protocol ? `${protocol.name} ${protocol.compatibility}` : "unknown"}</dd>
          </div>
          <div>
            <dt>Daemon</dt>
            <dd>{daemonInfo?.health || "unknown"} / {daemonInfo?.daemonTransport || "transport unknown"}</dd>
          </div>
          <div>
            <dt>gRPC</dt>
            <dd>{daemonInfo?.grpcAddr || "not reported"}</dd>
          </div>
          <div>
            <dt>Cache</dt>
            <dd>{daemonInfo?.cacheDriver || "unknown"}</dd>
          </div>
          <div>
            <dt>Realtime</dt>
            <dd>{realtimeStatus}</dd>
          </div>
          <div>
            <dt>Endpoints</dt>
            <dd>{readableEndpoints} inbound / {writableEndpoints} outbound</dd>
          </div>
          <div>
            <dt>Recommended runtimes</dt>
            <dd>{recommendedRuntimes} of {runtimePresets.length}</dd>
          </div>
          <div>
            <dt>Server time</dt>
            <dd>{formatUnixTime(daemonInfo?.serverTimeUnix)}</dd>
          </div>
        </dl>
      </section>
    </section>
  );
}

type EndpointCreateMode = "im" | "generic";
type EndpointConfigValue = string | boolean;

const DEFAULT_BINDING_TARGETS = ["channel", "thread", "agent", "default_target"];
const DEFAULT_GROUP_MODES = ["mention", "always", "disabled"];
const NOTIFICATION_EVENT_KINDS = ["message", "mention", "task", "reminder", "run", "activity", "delivery_status", "all"];
const NOTIFICATION_PREFERENCES = ["all", "mentions", "muted"];
const REDACTED_VALUE = "***";

const sectionIssueLabels: Record<SectionIssueKey, string> = {
  setup: "Setup",
  protocol: "Protocol",
  daemon: "Daemon bridge",
  endpoints: "Endpoints",
  notificationRoutes: "Notification routes",
  imProviders: "IM providers",
  channels: "Channels",
  channelMembers: "Members",
  messages: "Messages",
  savedMessages: "Saved messages",
  inbox: "Inbox",
  tasks: "Tasks",
  reminders: "Reminders",
  activity: "Activity",
  agentStatuses: "Agent status",
  daemonInventory: "Daemon inventory",
  daemonRuns: "Daemon runs",
  daemonActivity: "Daemon activity",
  runtimePresets: "Runtime presets"
};

function parseEndpointConfig(configJson?: string): Record<string, unknown> {
  if (!configJson?.trim()) return {};
  try {
    const parsed = JSON.parse(configJson) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function formatProviderLabel(provider: string, schemas: IMProviderSchema[]) {
  return schemas.find((schema) => schema.provider === provider)?.displayName || provider;
}

function displayConfigValue(value: unknown) {
  if (value === REDACTED_VALUE) return "redacted";
  if (typeof value === "boolean") return value ? "enabled" : "disabled";
  if (typeof value === "string") return value || "empty";
  if (value === null || value === undefined) return "empty";
  return JSON.stringify(value);
}

function hasConfigField(schema: IMProviderSchema | null, name: string) {
  return Boolean(schema?.fields.some((field) => field.name === name));
}

function defaultIMValues(schema: IMProviderSchema | null): Record<string, EndpointConfigValue> {
  const values: Record<string, EndpointConfigValue> = {};
  for (const field of schema?.fields ?? []) {
    if (field.type === "boolean") {
      values[field.name] = false;
    } else if (field.type === "select") {
      values[field.name] = field.options?.[0] ?? "";
    } else if (field.type === "json") {
      values[field.name] = "{}";
    } else {
      values[field.name] = "";
    }
  }
  return values;
}

function editableConfigJSON(endpoint: InteractionEndpoint) {
  return endpoint.configJson.includes(`"${REDACTED_VALUE}"`) ? "" : endpoint.configJson || "{}";
}

function endpointConfig(endpoint: InteractionEndpoint | null): Record<string, unknown> {
  if (!endpoint?.configJson.trim()) return {};
  try {
    const parsed = JSON.parse(endpoint.configJson);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function endpointSupportsBindingMethod(endpoint: InteractionEndpoint | null, schema: IMProviderSchema | null, method: string) {
  if (!endpoint || !schema?.bindingMethods?.some((bindingMethod) => bindingMethod.method === method)) return false;
  if ((endpoint.provider === "weixin" || endpoint.provider === "wechat") && method === "qr_code") {
    const mode = String(endpointConfig(endpoint).mode ?? "official_account").trim().toLowerCase();
    return mode === "ilink";
  }
  return true;
}

function targetLabel(target: string) {
  return target
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function EndpointsPanel({
  endpoints,
  routes,
  imProviders,
  onCreated,
  issues,
  loading,
  onRetry
}: {
  endpoints: InteractionEndpoint[];
  routes: NotificationRoute[];
  imProviders: IMProviderSchema[];
  onCreated: () => Promise<void>;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [mode, setMode] = useState<EndpointCreateMode>("im");
  const [displayName, setDisplayName] = useState("");
  const [kind, setKind] = useState("web");
  const [provider, setProvider] = useState("");
  const [targetPrefix, setTargetPrefix] = useState("#");
  const [authMode, setAuthMode] = useState("bearer");
  const [inboundEnabled, setInboundEnabled] = useState(true);
  const [outboundEnabled, setOutboundEnabled] = useState(true);
  const [imConfigValues, setIMConfigValues] = useState<Record<string, EndpointConfigValue>>({});
  const [bindingTargetType, setBindingTargetType] = useState("channel");
  const [defaultTarget, setDefaultTarget] = useState("#general");
  const [defaultAgent, setDefaultAgent] = useState("");
  const [groupMode, setGroupMode] = useState("mention");
  const [formError, setFormError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectedEndpointId, setSelectedEndpointId] = useState("");
  const [endpointEditName, setEndpointEditName] = useState("");
  const [endpointEditTargetPrefix, setEndpointEditTargetPrefix] = useState("#");
  const [endpointEditAuthMode, setEndpointEditAuthMode] = useState("bearer");
  const [endpointEditInbound, setEndpointEditInbound] = useState(true);
  const [endpointEditOutbound, setEndpointEditOutbound] = useState(true);
  const [endpointEditConfig, setEndpointEditConfig] = useState("");
  const [endpointActionError, setEndpointActionError] = useState("");
  const [endpointActionBusy, setEndpointActionBusy] = useState("");
  const [endpointTestResult, setEndpointTestResult] = useState<InteractionEndpointTestResult | null>(null);
  const [bindingSession, setBindingSession] = useState<IMBindingSession | null>(null);
  const [bindingError, setBindingError] = useState("");
  const [bindingBusy, setBindingBusy] = useState("");
  const [routeTarget, setRouteTarget] = useState("#general");
  const [routeThreadId, setRouteThreadId] = useState("");
  const [routeEndpointId, setRouteEndpointId] = useState("");
  const [routeEventKind, setRouteEventKind] = useState("message");
  const [routePreference, setRoutePreference] = useState("all");
  const [routeEnabled, setRouteEnabled] = useState(true);
  const [routeConfigJson, setRouteConfigJson] = useState("{}");
  const [routeResolveTarget, setRouteResolveTarget] = useState("#general");
  const [routeResolveThreadId, setRouteResolveThreadId] = useState("");
  const [routeResolveEventKind, setRouteResolveEventKind] = useState("message");
  const [routeResolveResult, setRouteResolveResult] = useState<NotificationRoute[] | null>(null);
  const [routeActionError, setRouteActionError] = useState("");
  const [routeActionBusy, setRouteActionBusy] = useState("");
  const autoNameRef = useRef("");

  const selectedEndpoint = useMemo(
    () => endpoints.find((endpoint) => endpoint.id === selectedEndpointId) ?? endpoints[0] ?? null,
    [endpoints, selectedEndpointId]
  );
  const selectedProvider = useMemo(
    () => imProviders.find((schema) => schema.provider === provider) ?? imProviders[0] ?? null,
    [imProviders, provider]
  );
  const endpointById = useMemo(
    () => new Map(endpoints.map((endpoint) => [endpoint.id, endpoint])),
    [endpoints]
  );
  const routesByEndpoint = useMemo(() => {
    const counts = new Map<string, number>();
    for (const route of routes) counts.set(route.endpointId, (counts.get(route.endpointId) ?? 0) + 1);
    return counts;
  }, [routes]);
  const bindingTargets = selectedProvider?.bindingTargets.length
    ? selectedProvider.bindingTargets
    : DEFAULT_BINDING_TARGETS;
  const providerHasGroupMode = hasConfigField(selectedProvider, "group_mode");
  const selectedEndpointProvider = useMemo(
    () => imProviders.find((schema) => schema.provider === selectedEndpoint?.provider) ?? null,
    [imProviders, selectedEndpoint?.provider]
  );
  const qrBindingMethod = endpointSupportsBindingMethod(selectedEndpoint, selectedEndpointProvider, "qr_code")
    ? selectedEndpointProvider?.bindingMethods?.find((method) => method.method === "qr_code") ?? null
    : null;

  useEffect(() => {
    if (mode !== "im") return;
    if (!provider && imProviders[0]) setProvider(imProviders[0].provider);
  }, [imProviders, mode, provider]);

  useEffect(() => {
    if (mode !== "im" || !selectedProvider) return;
    setIMConfigValues(defaultIMValues(selectedProvider));
    setBindingTargetType(selectedProvider.bindingTargets[0] || "channel");
    setAuthMode(selectedProvider.supportsWebhook ? "webhook_signature" : "bearer");
    const nextName = `${selectedProvider.displayName} endpoint`;
    if (!displayName || displayName === autoNameRef.current) {
      setDisplayName(nextName);
      autoNameRef.current = nextName;
    }
  }, [mode, selectedProvider]);

  useEffect(() => {
    if (!selectedEndpoint) return;
    setSelectedEndpointId(selectedEndpoint.id);
    setEndpointEditName(selectedEndpoint.displayName);
    setEndpointEditTargetPrefix(selectedEndpoint.targetPrefix || "#");
    setEndpointEditAuthMode(selectedEndpoint.authMode || "bearer");
    setEndpointEditInbound(selectedEndpoint.inboundEnabled);
    setEndpointEditOutbound(selectedEndpoint.outboundEnabled);
    setEndpointEditConfig(editableConfigJSON(selectedEndpoint));
    setEndpointActionError("");
    setEndpointTestResult(null);
    setBindingSession(null);
    setBindingError("");
  }, [selectedEndpoint]);

  useEffect(() => {
    if (!routeEndpointId && endpoints[0]) setRouteEndpointId(endpoints[0].id);
  }, [endpoints, routeEndpointId]);

  const updateIMValue = (field: IMProviderField, value: EndpointConfigValue) => {
    setIMConfigValues((current) => ({ ...current, [field.name]: value }));
  };

  const endpointLabel = (endpointId: string) => {
    const endpoint = endpointById.get(endpointId);
    return endpoint ? `${endpoint.displayName} (${formatProviderLabel(endpoint.provider, imProviders)})` : endpointId;
  };

  const createEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setFormError("");
    try {
      if (mode === "im") {
        if (!selectedProvider) {
          setFormError("IM provider schema is not loaded yet.");
          return;
        }
        const config: Record<string, unknown> = {};
        for (const field of selectedProvider.fields) {
          const value = imConfigValues[field.name];
          if (field.type === "json") {
            const raw = String(value ?? "").trim();
            if (!raw) continue;
            try {
              config[field.name] = JSON.parse(raw) as unknown;
            } catch {
              setFormError(`${field.label} must be valid JSON.`);
              return;
            }
          } else if (field.type === "boolean") {
            config[field.name] = Boolean(value);
          } else {
            const stringValue = String(value ?? "").trim();
            if (field.required && !stringValue) {
              setFormError(`${field.label} is required.`);
              return;
            }
            if (stringValue) config[field.name] = stringValue;
          }
        }
        config.binding_target_type = bindingTargetType;
        config.default_target = defaultTarget.trim();
        if (defaultAgent.trim()) config.default_agent = defaultAgent.trim();
        if (!providerHasGroupMode) config.group_mode = groupMode;

        await api.createInteractionEndpoint({
          kind: "im",
          provider: selectedProvider.provider,
          displayName: displayName.trim() || `${selectedProvider.displayName} endpoint`,
          targetPrefix: "#",
          inboundEnabled,
          outboundEnabled,
          authMode,
          configJson: JSON.stringify(config)
        });
      } else {
        await api.createInteractionEndpoint({
          kind: kind.trim() || "web",
          provider: provider.trim() || "browser",
          displayName: displayName.trim() || "Web Console",
          targetPrefix: targetPrefix.trim() || "#",
          inboundEnabled,
          outboundEnabled,
          authMode: authMode.trim() || "bearer",
          configJson: "{}"
        });
      }
      await onCreated();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Endpoint creation failed.");
    } finally {
      setBusy(false);
    }
  };

  const updateEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    if (!selectedEndpoint) return;
    setEndpointActionBusy("update-endpoint");
    setEndpointActionError("");
    try {
      const patch: {
        displayName: string;
        targetPrefix: string;
        inboundEnabled: boolean;
        outboundEnabled: boolean;
        authMode: string;
        configJson?: string;
      } = {
        displayName: endpointEditName.trim(),
        targetPrefix: endpointEditTargetPrefix.trim(),
        inboundEnabled: endpointEditInbound,
        outboundEnabled: endpointEditOutbound,
        authMode: endpointEditAuthMode.trim()
      };
      if (endpointEditConfig.trim()) {
        JSON.parse(endpointEditConfig);
        patch.configJson = endpointEditConfig.trim();
      }
      await api.updateInteractionEndpoint(selectedEndpoint.id, patch);
      await onCreated();
    } catch (err) {
      setEndpointActionError(err instanceof Error ? err.message : "Endpoint update failed.");
    } finally {
      setEndpointActionBusy("");
    }
  };

  const testEndpoint = async (endpointId: string) => {
    setSelectedEndpointId(endpointId);
    setEndpointActionBusy(`test:${endpointId}`);
    setEndpointActionError("");
    setEndpointTestResult(null);
    try {
      setEndpointTestResult(await api.testInteractionEndpoint(endpointId));
    } catch (err) {
      setEndpointActionError(err instanceof Error ? err.message : "Endpoint test failed.");
    } finally {
      setEndpointActionBusy("");
    }
  };

  useEffect(() => {
    if (!bindingSession || bindingSession.status !== "pending") return undefined;
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const next = await api.getIMBindingSession(bindingSession.endpointId, bindingSession.id);
          setBindingSession(next);
          setBindingError("");
        } catch (err) {
          setBindingError(err instanceof Error ? err.message : "Binding session refresh failed.");
        }
      })();
    }, 3000);
    return () => window.clearInterval(timer);
  }, [bindingSession]);

  const startBindingSession = async () => {
    if (!selectedEndpoint || !qrBindingMethod) return;
    setBindingBusy("create");
    setBindingError("");
    try {
      setBindingSession(await api.createIMBindingSession(selectedEndpoint.id, qrBindingMethod.method));
    } catch (err) {
      setBindingError(err instanceof Error ? err.message : "Binding session creation failed.");
    } finally {
      setBindingBusy("");
    }
  };

  const refreshBindingSession = async () => {
    if (!bindingSession) return;
    setBindingBusy("refresh");
    setBindingError("");
    try {
      setBindingSession(await api.getIMBindingSession(bindingSession.endpointId, bindingSession.id));
    } catch (err) {
      setBindingError(err instanceof Error ? err.message : "Binding session refresh failed.");
    } finally {
      setBindingBusy("");
    }
  };

  const cancelBindingSession = async () => {
    if (!bindingSession) return;
    setBindingBusy("cancel");
    setBindingError("");
    try {
      setBindingSession(await api.cancelIMBindingSession(bindingSession.endpointId, bindingSession.id));
    } catch (err) {
      setBindingError(err instanceof Error ? err.message : "Binding session cancel failed.");
    } finally {
      setBindingBusy("");
    }
  };

  const deleteEndpoint = async (endpoint: InteractionEndpoint) => {
    if (!window.confirm(`Delete endpoint "${endpoint.displayName}"?`)) return;
    setEndpointActionBusy(`delete:${endpoint.id}`);
    setEndpointActionError("");
    try {
      await api.deleteInteractionEndpoint(endpoint.id);
      await onCreated();
    } catch (err) {
      setEndpointActionError(err instanceof Error ? err.message : "Endpoint delete failed.");
    } finally {
      setEndpointActionBusy("");
    }
  };

  const createRoute = async (event: FormEvent) => {
    event.preventDefault();
    setRouteActionBusy("create-route");
    setRouteActionError("");
    try {
      JSON.parse(routeConfigJson || "{}");
      await api.createNotificationRoute({
        target: routeTarget.trim(),
        threadId: routeThreadId.trim() || undefined,
        endpointId: routeEndpointId,
        eventKind: routeEventKind,
        preference: routePreference,
        enabled: routeEnabled,
        configJson: routeConfigJson.trim() || "{}"
      });
      await onCreated();
    } catch (err) {
      setRouteActionError(err instanceof Error ? err.message : "Notification route creation failed.");
    } finally {
      setRouteActionBusy("");
    }
  };

  const toggleRoute = async (route: NotificationRoute) => {
    setRouteActionBusy(`toggle:${route.id}`);
    setRouteActionError("");
    try {
      await api.updateNotificationRoute(route.id, { enabled: !route.enabled });
      await onCreated();
    } catch (err) {
      setRouteActionError(err instanceof Error ? err.message : "Notification route update failed.");
    } finally {
      setRouteActionBusy("");
    }
  };

  const rebindRoute = async (route: NotificationRoute) => {
    setRouteActionBusy(`rebind:${route.id}`);
    setRouteActionError("");
    try {
      await api.updateNotificationRoute(route.id, { endpointId: routeEndpointId });
      await onCreated();
    } catch (err) {
      setRouteActionError(err instanceof Error ? err.message : "Notification route rebind failed.");
    } finally {
      setRouteActionBusy("");
    }
  };

  const deleteRoute = async (route: NotificationRoute) => {
    setRouteActionBusy(`delete-route:${route.id}`);
    setRouteActionError("");
    try {
      await api.deleteNotificationRoute(route.id);
      await onCreated();
    } catch (err) {
      setRouteActionError(err instanceof Error ? err.message : "Notification route delete failed.");
    } finally {
      setRouteActionBusy("");
    }
  };

  const resolveRoutes = async (event: FormEvent) => {
    event.preventDefault();
    setRouteActionBusy("resolve-routes");
    setRouteActionError("");
    setRouteResolveResult(null);
    try {
      const result = await api.resolveNotificationRoutes({
        target: routeResolveTarget.trim(),
        threadId: routeResolveThreadId.trim() || undefined,
        eventKind: routeResolveEventKind
      });
      setRouteResolveResult(result.items);
    } catch (err) {
      setRouteActionError(err instanceof Error ? err.message : "Notification route resolve failed.");
    } finally {
      setRouteActionBusy("");
    }
  };

  const renderProviderField = (field: IMProviderField) => {
    const fieldID = `im-field-${field.name}`;
    const value = imConfigValues[field.name];
    const hint = field.sensitive
      ? `${field.description || ""} Stored endpoint responses are redacted.`
      : field.description;

    if (field.type === "boolean") {
      return (
        <label className="checkbox-row" key={field.name} htmlFor={fieldID}>
          <input
            id={fieldID}
            type="checkbox"
            checked={Boolean(value)}
            onChange={(event) => updateIMValue(field, event.target.checked)}
          />
          <span>
            {field.label}
            {hint ? <small>{hint}</small> : null}
          </span>
        </label>
      );
    }

    if (field.type === "select") {
      return (
        <label key={field.name} htmlFor={fieldID}>
          {field.label}
          <select
            id={fieldID}
            required={field.required}
            value={String(value ?? "")}
            onChange={(event) => updateIMValue(field, event.target.value)}
          >
            {(field.options || []).map((option) => (
              <option key={option} value={option}>
                {targetLabel(option)}
              </option>
            ))}
          </select>
          {hint ? <small>{hint}</small> : null}
        </label>
      );
    }

    if (field.type === "json") {
      return (
        <label key={field.name} htmlFor={fieldID}>
          {field.label}
          <textarea
            id={fieldID}
            required={field.required}
            value={String(value ?? "")}
            placeholder={field.placeholder || "{}"}
            onChange={(event) => updateIMValue(field, event.target.value)}
          />
          {hint ? <small>{hint}</small> : null}
        </label>
      );
    }

    return (
      <label key={field.name} htmlFor={fieldID}>
        {field.label}
        <input
          id={fieldID}
          required={field.required}
          type={field.sensitive ? "password" : "text"}
          value={String(value ?? "")}
          placeholder={field.placeholder || (field.sensitive ? "Stored redacted after create" : "")}
          onChange={(event) => updateIMValue(field, event.target.value)}
        />
        {hint ? <small>{hint}</small> : null}
      </label>
    );
  };

  return (
    <section className="endpoint-workspace">
      <SectionStatusNotice loading={loading} message="Loading endpoint configuration" />
      <SectionIssuesNotice issues={issues} onRetry={onRetry} />
      <div className="endpoint-grid">
      <div className="panel endpoint-list-panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Interaction Layer</p>
            <h2>Endpoints</h2>
          </div>
        </div>
        <div className="endpoint-list">
          {endpoints.length ? (
            endpoints.map((endpoint) => {
              const config = parseEndpointConfig(endpoint.configJson);
              const schema = imProviders.find((item) => item.provider === endpoint.provider) ?? null;
              const previewKeys = [
                "binding_target_type",
                "default_target",
                "group_mode",
                "enable_notify",
                ...(schema?.fields.map((field) => field.name) ?? [])
              ];
              const previewEntries = Array.from(new Set(previewKeys))
                .filter((key) => config[key] !== undefined && config[key] !== "")
                .slice(0, 5);
              return (
                <article
                  className={
                    endpoint.id === selectedEndpoint?.id
                      ? "endpoint-row endpoint-row-detailed is-selected"
                      : "endpoint-row endpoint-row-detailed"
                  }
                  key={endpoint.id}
                >
                  <div className="endpoint-main">
                    <div>
                      <strong>{endpoint.displayName}</strong>
                      <span>
                        {endpoint.kind} / {formatProviderLabel(endpoint.provider, imProviders)}
                      </span>
                    </div>
                    {endpoint.kind === "im" ? (
                      <div className="endpoint-config-chips" aria-label="Endpoint config summary">
                        {previewEntries.length ? (
                          previewEntries.map((key) => (
                            <span key={key}>
                              {targetLabel(key)}: {displayConfigValue(config[key])}
                            </span>
                          ))
                        ) : (
                          <span>Config redacted</span>
                        )}
                      </div>
                    ) : null}
                    {endpoint.kind === "im" && endpoint.configJson ? (
                      <details className="endpoint-config-details">
                        <summary>Redacted config JSON</summary>
                        <code>{endpoint.configJson}</code>
                      </details>
                    ) : null}
                  </div>
                  <div className="endpoint-flags">
                    <StatusDot active={endpoint.inboundEnabled} label="Inbound" />
                    <StatusDot active={endpoint.outboundEnabled} label="Outbound" />
                    <span className="route-count">{routesByEndpoint.get(endpoint.id) ?? 0} routes</span>
                  </div>
                  <div className="endpoint-actions">
                    <button className="mini-action-button" type="button" onClick={() => setSelectedEndpointId(endpoint.id)}>
                      Edit
                    </button>
                    <button
                      className="mini-action-button"
                      type="button"
                      disabled={endpointActionBusy === `test:${endpoint.id}`}
                      onClick={() => void testEndpoint(endpoint.id)}
                    >
                      Test
                    </button>
                    <button
                      className="mini-action-button"
                      type="button"
                      disabled={endpointActionBusy === `delete:${endpoint.id}`}
                      onClick={() => void deleteEndpoint(endpoint)}
                    >
                      Delete
                    </button>
                  </div>
                </article>
              );
            })
          ) : (
            <EmptyState icon={Settings} title="No endpoints configured" />
          )}
        </div>
        {endpointActionError ? <p className="inline-error" role="alert">{endpointActionError}</p> : null}
      </div>
      <form className="panel compact form-stack endpoint-create-panel" onSubmit={createEndpoint}>
        <p className="eyebrow">Create Endpoint</p>
        <div className="segmented endpoint-mode-toggle" role="group" aria-label="Endpoint type">
          <button
            className={mode === "im" ? "is-active" : ""}
            type="button"
            onClick={() => setMode("im")}
          >
            IM
          </button>
          <button
            className={mode === "generic" ? "is-active" : ""}
            type="button"
            onClick={() => setMode("generic")}
          >
            Generic
          </button>
        </div>

        {mode === "im" ? (
          <>
            <label htmlFor="im-provider">
              Provider
              <select
                id="im-provider"
                value={selectedProvider?.provider || ""}
                disabled={!imProviders.length}
                onChange={(event) => setProvider(event.target.value)}
              >
                {imProviders.map((schema) => (
                  <option key={schema.provider} value={schema.provider}>
                    {schema.displayName}
                  </option>
                ))}
              </select>
            </label>
            {selectedProvider ? (
              <div className="provider-schema-summary">
                <div>
                  <strong>{selectedProvider.displayName}</strong>
                  <span>{selectedProvider.description}</span>
                </div>
                <div className="endpoint-config-chips">
                  <span>{selectedProvider.transport}</span>
                  {selectedProvider.supportsWebhook ? <span>Webhook</span> : null}
                  {selectedProvider.supportsPolling ? <span>Polling</span> : null}
                  {selectedProvider.supportsStreaming ? <span>Streaming</span> : null}
                  {selectedProvider.supportsMedia ? <span>Media</span> : null}
                </div>
              </div>
            ) : (
              <p className="notice error">IM provider schema is unavailable.</p>
            )}
            <label htmlFor="endpoint-display-name">
              Display name
              <input
                id="endpoint-display-name"
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
              />
            </label>

            {selectedProvider ? (
              <>
                <div className="endpoint-form-section">
                  <strong>Provider config</strong>
                  <div className="form-stack">
                    {selectedProvider.fields.map((field) => renderProviderField(field))}
                  </div>
                </div>

                <div className="endpoint-form-section">
                  <strong>Binding</strong>
                  <div className="endpoint-binding-grid">
                    <label htmlFor="binding-target-type">
                      Target type
                      <select
                        id="binding-target-type"
                        value={bindingTargetType}
                        onChange={(event) => setBindingTargetType(event.target.value)}
                      >
                        {bindingTargets.map((target) => (
                          <option key={target} value={target}>
                            {targetLabel(target)}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label htmlFor="default-target">
                      Default target
                      <input
                        id="default-target"
                        value={defaultTarget}
                        placeholder="#general or inbox:im/provider"
                        onChange={(event) => setDefaultTarget(event.target.value)}
                      />
                    </label>
                    <label htmlFor="default-agent">
                      Default agent
                      <input
                        id="default-agent"
                        value={defaultAgent}
                        placeholder="@agent handle or agent id"
                        onChange={(event) => setDefaultAgent(event.target.value)}
                      />
                    </label>
                    {!providerHasGroupMode ? (
                      <label htmlFor="group-mode">
                        Group mode
                        <select
                          id="group-mode"
                          value={groupMode}
                          onChange={(event) => setGroupMode(event.target.value)}
                        >
                          {DEFAULT_GROUP_MODES.map((modeOption) => (
                            <option key={modeOption} value={modeOption}>
                              {targetLabel(modeOption)}
                            </option>
                          ))}
                        </select>
                      </label>
                    ) : null}
                  </div>
                </div>

                <div className="endpoint-form-section">
                  <strong>Credential handling</strong>
                  <p>
                    Sensitive fields are sent once and the endpoint list reads back redacted values.
                  </p>
                </div>

                {selectedProvider.bindingMethods?.length ? (
                  <div className="endpoint-form-section">
                    <strong>Binding capabilities</strong>
                    <div className="endpoint-config-chips">
                      {selectedProvider.bindingMethods.map((method) => (
                        <span key={method.method}>{method.displayName}</span>
                      ))}
                    </div>
                    <p>Binding sessions are started after the endpoint is created.</p>
                  </div>
                ) : null}

                {selectedProvider.setupHints.length ? (
                  <div className="setup-hints">
                    {selectedProvider.setupHints.map((hint) => (
                      <span key={hint}>{hint}</span>
                    ))}
                    {selectedProvider.supportsWebhook ? (
                      <span>Callback setup is provider-runtime specific; use webhook signature auth for signed callbacks.</span>
                    ) : null}
                  </div>
                ) : null}
              </>
            ) : null}
          </>
        ) : (
          <>
            <label htmlFor="generic-display-name">
              Display name
              <input
                id="generic-display-name"
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
              />
            </label>
            <label htmlFor="generic-kind">
              Kind
              <input id="generic-kind" value={kind} onChange={(event) => setKind(event.target.value)} />
            </label>
            <label htmlFor="generic-provider">
              Provider
              <input
                id="generic-provider"
                value={provider}
                onChange={(event) => setProvider(event.target.value)}
              />
            </label>
            <label htmlFor="generic-prefix">
              Target prefix
              <input
                id="generic-prefix"
                value={targetPrefix}
                onChange={(event) => setTargetPrefix(event.target.value)}
              />
            </label>
          </>
        )}

        <div className="endpoint-toggle-grid">
          <label className="checkbox-row" htmlFor="endpoint-inbound-enabled">
            <input
              id="endpoint-inbound-enabled"
              type="checkbox"
              checked={inboundEnabled}
              onChange={(event) => setInboundEnabled(event.target.checked)}
            />
            <span>Inbound</span>
          </label>
          <label className="checkbox-row" htmlFor="endpoint-outbound-enabled">
            <input
              id="endpoint-outbound-enabled"
              type="checkbox"
              checked={outboundEnabled}
              onChange={(event) => setOutboundEnabled(event.target.checked)}
            />
            <span>Outbound</span>
          </label>
        </div>

        <label htmlFor="endpoint-auth-mode">
          Auth mode
          <input
            id="endpoint-auth-mode"
            value={authMode}
            onChange={(event) => setAuthMode(event.target.value)}
          />
        </label>

        {formError ? <p className="inline-error">{formError}</p> : null}
        <button className="primary-button" type="submit" disabled={busy || (mode === "im" && !selectedProvider)}>
          <Plus size={16} aria-hidden="true" />
          Create
        </button>
      </form>

      <form className="panel compact form-stack endpoint-edit-panel" onSubmit={updateEndpoint}>
        <p className="eyebrow">Manage Endpoint</p>
        {selectedEndpoint ? (
          <>
            <label htmlFor="endpoint-edit-name">
              Display name
              <input
                id="endpoint-edit-name"
                value={endpointEditName}
                onChange={(event) => setEndpointEditName(event.target.value)}
              />
            </label>
            <div className="endpoint-toggle-grid">
              <label className="checkbox-row" htmlFor="endpoint-edit-inbound">
                <input
                  id="endpoint-edit-inbound"
                  type="checkbox"
                  checked={endpointEditInbound}
                  onChange={(event) => setEndpointEditInbound(event.target.checked)}
                />
                <span>Inbound</span>
              </label>
              <label className="checkbox-row" htmlFor="endpoint-edit-outbound">
                <input
                  id="endpoint-edit-outbound"
                  type="checkbox"
                  checked={endpointEditOutbound}
                  onChange={(event) => setEndpointEditOutbound(event.target.checked)}
                />
                <span>Outbound</span>
              </label>
            </div>
            <label htmlFor="endpoint-edit-prefix">
              Target prefix
              <input
                id="endpoint-edit-prefix"
                value={endpointEditTargetPrefix}
                onChange={(event) => setEndpointEditTargetPrefix(event.target.value)}
              />
            </label>
            <label htmlFor="endpoint-edit-auth-mode">
              Auth mode
              <input
                id="endpoint-edit-auth-mode"
                value={endpointEditAuthMode}
                onChange={(event) => setEndpointEditAuthMode(event.target.value)}
              />
            </label>
            <label htmlFor="endpoint-edit-config">
              Config JSON
              <textarea
                id="endpoint-edit-config"
                value={endpointEditConfig}
                placeholder={selectedEndpoint.configJson.includes(REDACTED_VALUE) ? "Leave blank to preserve stored redacted secrets" : "{}"}
                onChange={(event) => setEndpointEditConfig(event.target.value)}
              />
              <small>Blank keeps the existing provider config when sensitive values are redacted.</small>
            </label>
            {endpointTestResult ? (
              <div className="endpoint-test-result" role="status">
                <div>
                  <strong>{endpointTestResult.ready ? "Config ready" : "Needs attention"}</strong>
                  <span>{endpointTestResult.summary}</span>
                </div>
                <div className="endpoint-config-chips">
                  <span>{endpointTestResult.runtimeLive ? "Runtime smoke passed" : "Runtime not exercised"}</span>
                  {endpointTestResult.checks.map((check) => (
                    <span key={check.name}>
                      {targetLabel(check.name)}: {check.ok ? "ok" : "not ready"}
                    </span>
                  ))}
                </div>
              </div>
            ) : null}
            {selectedEndpoint.kind === "im" ? (
              <div className="endpoint-form-section">
                <strong>Channel binding</strong>
                {qrBindingMethod ? (
                  <>
                    <p>{qrBindingMethod.description}</p>
                    <div className="endpoint-actions">
                      <button
                        className="secondary-button"
                        type="button"
                        disabled={bindingBusy === "create"}
                        onClick={() => void startBindingSession()}
                      >
                        Start QR binding
                      </button>
                      <button
                        className="mini-action-button"
                        type="button"
                        disabled={!bindingSession || bindingBusy === "refresh"}
                        onClick={() => void refreshBindingSession()}
                      >
                        Refresh
                      </button>
                      <button
                        className="mini-action-button"
                        type="button"
                        disabled={!bindingSession || bindingBusy === "cancel"}
                        onClick={() => void cancelBindingSession()}
                      >
                        Cancel
                      </button>
                    </div>
                    {bindingSession ? (
                      <div className="binding-session-panel" role="status">
                        <div className="binding-qr-frame" aria-label="QR binding payload">
                          {bindingSession.qrImageUrl ? (
                            <img src={bindingSession.qrImageUrl} alt="Channel binding QR code" />
                          ) : (
                            <code>{bindingSession.qrPayload || "QR payload pending provider adapter"}</code>
                          )}
                        </div>
                        <div className="binding-session-detail">
                          <strong>{targetLabel(bindingSession.status)}</strong>
                          <span>{bindingSession.detail || "Waiting for scan status from provider adapter."}</span>
                          <span>Session {bindingSession.id}</span>
                          <span>Expires {formatUnixTime(bindingSession.expiresUnix)}</span>
                        </div>
                      </div>
                    ) : null}
                    {bindingError ? <p className="inline-error" role="alert">{bindingError}</p> : null}
                  </>
                ) : (
                  <p>No QR binding capability is available for this provider.</p>
                )}
              </div>
            ) : null}
            <div className="endpoint-actions">
              <button className="primary-button" type="submit" disabled={endpointActionBusy === "update-endpoint"}>
                Save endpoint
              </button>
              <button
                className="secondary-button"
                type="button"
                disabled={endpointActionBusy === `test:${selectedEndpoint.id}`}
                onClick={() => void testEndpoint(selectedEndpoint.id)}
              >
                Test readiness
              </button>
            </div>
          </>
        ) : (
          <EmptyState icon={Settings} title="Select an endpoint" />
        )}
      </form>
      </div>

      <div className="endpoint-route-grid">
        <section className="panel">
          <div className="panel-heading">
            <div>
              <p className="eyebrow">Notification Routes</p>
              <h2>Delivery routing</h2>
            </div>
          </div>
          <div className="endpoint-list">
            {routes.length ? (
              routes.map((route) => (
                <article className="endpoint-row endpoint-row-detailed" key={route.id}>
                  <div className="endpoint-main">
                    <div>
                      <strong>{route.target}{route.threadId ? ` / ${route.threadId}` : ""}</strong>
                      <span>{route.eventKind} · {route.preference} · {endpointLabel(route.endpointId)}</span>
                    </div>
                    <div className="endpoint-config-chips">
                      <span>{route.enabled ? "enabled" : "disabled"}</span>
                      <span>{route.configJson || "{}"}</span>
                    </div>
                  </div>
                  <div className="endpoint-actions">
                    <button
                      className="mini-action-button"
                      type="button"
                      disabled={routeActionBusy === `toggle:${route.id}`}
                      onClick={() => void toggleRoute(route)}
                    >
                      {route.enabled ? "Disable" : "Enable"}
                    </button>
                    <button
                      className="mini-action-button"
                      type="button"
                      disabled={!routeEndpointId || route.endpointId === routeEndpointId || routeActionBusy === `rebind:${route.id}`}
                      onClick={() => void rebindRoute(route)}
                    >
                      Rebind
                    </button>
                    <button
                      className="mini-action-button"
                      type="button"
                      disabled={routeActionBusy === `delete-route:${route.id}`}
                      onClick={() => void deleteRoute(route)}
                    >
                      Delete
                    </button>
                  </div>
                </article>
              ))
            ) : (
              <EmptyState icon={Bell} title="No notification routes configured" />
            )}
          </div>
        </section>

        <form className="panel compact form-stack" onSubmit={createRoute}>
          <p className="eyebrow">Create / Rebind</p>
          <label htmlFor="route-target">
            Target
            <input id="route-target" value={routeTarget} onChange={(event) => setRouteTarget(event.target.value)} />
          </label>
          <label htmlFor="route-thread">
            Thread id
            <input id="route-thread" value={routeThreadId} onChange={(event) => setRouteThreadId(event.target.value)} />
          </label>
          <label htmlFor="route-endpoint">
            Endpoint
            <select id="route-endpoint" value={routeEndpointId} onChange={(event) => setRouteEndpointId(event.target.value)}>
              {endpoints.map((endpoint) => (
                <option key={endpoint.id} value={endpoint.id}>
                  {endpointLabel(endpoint.id)}
                </option>
              ))}
            </select>
          </label>
          <div className="endpoint-binding-grid">
            <label htmlFor="route-event-kind">
              Event
              <select id="route-event-kind" value={routeEventKind} onChange={(event) => setRouteEventKind(event.target.value)}>
                {NOTIFICATION_EVENT_KINDS.map((kind) => (
                  <option key={kind} value={kind}>{targetLabel(kind)}</option>
                ))}
              </select>
            </label>
            <label htmlFor="route-preference">
              Preference
              <select id="route-preference" value={routePreference} onChange={(event) => setRoutePreference(event.target.value)}>
                {NOTIFICATION_PREFERENCES.map((preference) => (
                  <option key={preference} value={preference}>{targetLabel(preference)}</option>
                ))}
              </select>
            </label>
          </div>
          <label htmlFor="route-config">
            Config JSON
            <textarea id="route-config" value={routeConfigJson} onChange={(event) => setRouteConfigJson(event.target.value)} />
          </label>
          <label className="checkbox-row" htmlFor="route-enabled">
            <input
              id="route-enabled"
              type="checkbox"
              checked={routeEnabled}
              onChange={(event) => setRouteEnabled(event.target.checked)}
            />
            <span>Enabled</span>
          </label>
          {routeActionError ? <p className="inline-error" role="alert">{routeActionError}</p> : null}
          <button className="primary-button" type="submit" disabled={!routeEndpointId || routeActionBusy === "create-route"}>
            <Plus size={16} aria-hidden="true" />
            Create route
          </button>
        </form>

        <form className="panel compact form-stack" onSubmit={resolveRoutes}>
          <p className="eyebrow">Resolve Test</p>
          <label htmlFor="route-resolve-target">
            Target
            <input id="route-resolve-target" value={routeResolveTarget} onChange={(event) => setRouteResolveTarget(event.target.value)} />
          </label>
          <label htmlFor="route-resolve-thread">
            Thread id
            <input id="route-resolve-thread" value={routeResolveThreadId} onChange={(event) => setRouteResolveThreadId(event.target.value)} />
          </label>
          <label htmlFor="route-resolve-event">
            Event
            <select id="route-resolve-event" value={routeResolveEventKind} onChange={(event) => setRouteResolveEventKind(event.target.value)}>
              {NOTIFICATION_EVENT_KINDS.filter((kind) => kind !== "all").map((kind) => (
                <option key={kind} value={kind}>{targetLabel(kind)}</option>
              ))}
            </select>
          </label>
          <button className="secondary-button" type="submit" disabled={routeActionBusy === "resolve-routes"}>
            Resolve delivery
          </button>
          {routeResolveResult ? (
            <div className="endpoint-config-chips" role="status">
              {routeResolveResult.length ? routeResolveResult.map((route) => (
                <span key={route.id}>{endpointLabel(route.endpointId)}</span>
              )) : <span>No enabled route</span>}
            </div>
          ) : null}
        </form>
      </div>
    </section>
  );
}

function SkillsPanel({ runtimePresets }: { runtimePresets: RuntimePreset[] }) {
  return (
    <section className="two-column">
      <div className="panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Skill Center</p>
            <h2>Reusable workspace instructions</h2>
          </div>
          <button className="primary-button" type="button">
            <Plus size={16} aria-hidden="true" />
            Create Skill
          </button>
        </div>
        <div className="skill-tools">
          <input aria-label="Search skills" placeholder="Search skill name, tag, source, content" />
          <select aria-label="Filter skills by tag" defaultValue="all">
            <option value="all">All tags</option>
            <option value="quality">quality</option>
            <option value="daemon">daemon</option>
            <option value="ux">ux</option>
          </select>
        </div>
        <div className="skill-list">
          {skillItems.map((skill) => (
            <article className="skill-row" key={skill.name}>
              <Sparkles size={18} aria-hidden="true" />
              <div>
                <strong>{skill.name}</strong>
                <span>{skill.detail}</span>
              </div>
              <span className="skill-tag">#{skill.tag}</span>
              <span className="skill-used">Used by {skill.usedBy}</span>
            </article>
          ))}
        </div>
      </div>
      <aside className="panel compact">
        <p className="eyebrow">Agent Setup</p>
        <div className="setup-preview">
          <label>
            Runtime
            <select defaultValue="codex">
              {runtimePresets.map((preset) => (
                <option key={preset.kind} value={preset.kind}>
                  {preset.displayName || preset.kind}
                </option>
              ))}
            </select>
          </label>
          <div className="runtime-preset-list">
            {runtimePresets.slice(0, 6).map((preset) => (
              <article className="runtime-preset" key={preset.kind}>
                <div>
                  <strong>{preset.displayName || preset.kind}</strong>
                  <span>{preset.description || `${preset.provider} / ${preset.command || preset.kind}`}</span>
                </div>
                <div className="runtime-flags">
                  {preset.slockSupported ? <span>Slock</span> : null}
                  {preset.multicaSupported ? <span>Multica</span> : null}
                </div>
              </article>
            ))}
          </div>
          <label>
            Model
            <input defaultValue="CLI default" />
          </label>
          <label>
            Initial instructions
            <textarea rows={5} defaultValue={"Review discipline\nPrefer small verified changes."} />
          </label>
        </div>
      </aside>
    </section>
  );
}

function DaemonPanel({
  protocol,
  daemonInfo,
  realtimeStatus,
  latestEvent,
  agentStatuses,
  daemonInventory,
  daemonRuns,
  daemonActivity,
  runtimePresets,
  onAgentCreated,
  issues,
  loading,
  onRetry
}: {
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  latestEvent: CollaborationEvent | null;
  agentStatuses: AgentStatusSnapshot[];
  daemonInventory: DaemonInventoryComputer[];
  daemonRuns: DaemonRun[];
  daemonActivity: DaemonActivityRecord[];
  runtimePresets: RuntimePreset[];
  onAgentCreated: () => void;
  issues: SectionIssue[];
  loading: boolean;
  onRetry: () => Promise<void>;
}) {
  const [displayName, setDisplayName] = useState("");
  const [computerId, setComputerId] = useState("");
  const [hostname, setHostname] = useState("");
  const [enrollment, setEnrollment] = useState<DaemonEnrollment | null>(null);
  const [selectedPlatform, setSelectedPlatform] = useState<EnrollmentPlatform>("linux");
  const [wizardState, setWizardState] = useState<"idle" | "creating" | "polling" | "error">("idle");
  const [wizardError, setWizardError] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "error">("idle");
  const currentEnrollmentStatus = enrollmentStatus(enrollment);
  const canFinishEnrollment = currentEnrollmentStatus === "connected";
  const canRegenerateEnrollment = ["expired", "revoked", "failed"].includes(currentEnrollmentStatus);
  const canCreateEnrollment = wizardState !== "creating" && !["pending", "connected"].includes(currentEnrollmentStatus);
  const platformCommands = platformInstallCommands(enrollment);
  const selectedCommand =
    platformCommands.find((entry) => entry.platform === selectedPlatform) ?? platformCommands[0] ?? null;
  const [agentComputerId, setAgentComputerId] = useState("");
  const [agentRuntimeId, setAgentRuntimeId] = useState("");
  const [agentTemplateId, setAgentTemplateId] = useState("");
  const [agentDisplayName, setAgentDisplayName] = useState("");
  const [agentName, setAgentName] = useState("");
  const [agentTarget, setAgentTarget] = useState(DEFAULT_TARGET);
  const [agentOptions, setAgentOptions] = useState<Record<string, string>>({});
  const [agentCreateState, setAgentCreateState] = useState<"idle" | "creating" | "created" | "error">("idle");
  const [agentError, setAgentError] = useState("");
  const [createdAgent, setCreatedAgent] = useState<CreateDaemonAgentResult | null>(null);
  const [agentControlActionsById, setAgentControlActionsById] = useState<Record<string, AgentControlAction>>({});
  const [agentControlReasons, setAgentControlReasons] = useState<Record<string, string>>({});
  const [agentMessageDrafts, setAgentMessageDrafts] = useState<Record<string, string>>({});
  const [agentActionBusy, setAgentActionBusy] = useState<AgentActionBusy>(null);
  const [agentActionErrors, setAgentActionErrors] = useState<Record<string, string>>({});
  const [agentActionReceipts, setAgentActionReceipts] = useState<Record<string, string>>({});
  const agentComputers = useMemo(
    () => daemonInventory.filter((computer) =>
      computer.runtimes.some((runtime) =>
        runtime.installed && runtime.healthy && runtime.templates.some((template) => template.multiInstance)
      )
    ),
    [daemonInventory]
  );
  const selectedAgentComputer = agentComputers.find((computer) => computer.computerId === agentComputerId) ?? agentComputers[0] ?? null;
  const agentRuntimes = selectedAgentComputer?.runtimes.filter((runtime) =>
    runtime.installed && runtime.healthy && runtime.templates.some((template) => template.multiInstance)
  ) ?? [];
  const selectedAgentRuntime = agentRuntimes.find((runtime) => runtime.runtimeId === agentRuntimeId) ?? agentRuntimes[0] ?? null;
  const agentTemplates = selectedAgentRuntime?.templates.filter((template) => template.multiInstance) ?? [];
  const selectedAgentTemplate = agentTemplates.find((template) => template.templateId === agentTemplateId) ?? agentTemplates[0] ?? null;
  const canCreateAgent = Boolean(
    selectedAgentComputer?.computerId &&
    selectedAgentRuntime?.runtimeId &&
    selectedAgentTemplate?.templateId &&
    agentDisplayName.trim() &&
    agentCreateState !== "creating"
  );
  const manageableAgents = useMemo(() => {
    const byID = new Map<string, AgentStatusSnapshot>();
    for (const computer of daemonInventory) {
      for (const agent of computer.agents) {
        if (!agent.agentId) continue;
        byID.set(agent.agentId, {
          agentId: agent.agentId,
          computerId: agent.computerId || computer.computerId,
          runtimeProfileId: agent.runtimeProfileId,
          presence: agent.status || "idle",
          activityState: "unspecified",
          health: "unspecified",
          severity: "info",
          summary: agent.displayName || agent.name || agent.description,
          target: undefined,
          updatedTimeUnix: computer.lastHeartbeatUnix
        });
      }
    }
    for (const status of agentStatuses) {
      if (status.agentId) byID.set(status.agentId, { ...byID.get(status.agentId), ...status });
    }
    return Array.from(byID.values()).sort((left, right) => left.agentId.localeCompare(right.agentId));
  }, [agentStatuses, daemonInventory]);
  const runtimeKindByProfile = useMemo(() => {
    const byProfile = new Map<string, string>();
    for (const computer of daemonInventory) {
      for (const agent of computer.agents) {
        if (agent.runtimeProfileId && agent.runtimeKind) {
          byProfile.set(agent.runtimeProfileId, agent.runtimeKind);
        }
      }
      for (const runtime of computer.runtimes) {
        for (const template of runtime.templates) {
          if (template.templateId) {
            byProfile.set(template.templateId, runtime.kind);
          }
        }
      }
    }
    return byProfile;
  }, [daemonInventory]);

  useEffect(() => {
    if (!enrollment?.id || canFinishEnrollment || currentEnrollmentStatus !== "pending") {
      return undefined;
    }
    setWizardState("polling");
    const timer = window.setInterval(() => {
      void api.getDaemonEnrollment(enrollment.id).then((next) => {
        setEnrollment((current) => mergeEnrollmentStatus(current, next));
        setWizardError("");
        if (enrollmentStatus(next) === "connected") {
          setWizardState("idle");
        }
      }).catch((err: unknown) => {
        setWizardState("error");
        setWizardError(err instanceof Error ? err.message : "Unable to refresh enrollment status");
      });
    }, 3000);
    return () => window.clearInterval(timer);
  }, [canFinishEnrollment, currentEnrollmentStatus, enrollment?.id]);

  useEffect(() => {
    if (selectedAgentComputer && selectedAgentComputer.computerId !== agentComputerId) {
      setAgentComputerId(selectedAgentComputer.computerId);
    }
  }, [agentComputerId, selectedAgentComputer]);

  useEffect(() => {
    if (selectedAgentRuntime && selectedAgentRuntime.runtimeId !== agentRuntimeId) {
      setAgentRuntimeId(selectedAgentRuntime.runtimeId);
    }
  }, [agentRuntimeId, selectedAgentRuntime]);

  useEffect(() => {
    if (selectedAgentTemplate && selectedAgentTemplate.templateId !== agentTemplateId) {
      setAgentTemplateId(selectedAgentTemplate.templateId);
    }
  }, [agentTemplateId, selectedAgentTemplate]);

  useEffect(() => {
    setAgentOptions(defaultOptionValues(selectedAgentTemplate));
    setCreatedAgent(null);
    setAgentError("");
    setAgentCreateState("idle");
    if (!agentDisplayName.trim() && selectedAgentTemplate?.displayName) {
      setAgentDisplayName(selectedAgentTemplate.displayName);
    }
  }, [selectedAgentTemplate?.templateId]);

  const createEnrollment = async () => {
    setWizardState("creating");
    setWizardError("");
    setCopyState("idle");
    try {
      const next = await api.createDaemonEnrollment({
        displayName,
        computerId,
        hostname
      });
      setEnrollment(next);
      setSelectedPlatform("linux");
      setWizardState("polling");
    } catch (err) {
      setWizardState("error");
      setWizardError(err instanceof Error ? err.message : "Unable to create device enrollment");
    }
  };

  const copyInstallCommand = async () => {
    if (!selectedCommand?.ready || !selectedCommand.command) return;
    try {
      await navigator.clipboard.writeText(selectedCommand.command);
      setCopyState("copied");
      window.setTimeout(() => setCopyState("idle"), 1800);
    } catch {
      setCopyState("error");
    }
  };

  const refreshEnrollment = async () => {
    if (!enrollment?.id) return;
    setWizardError("");
    try {
      const next = await api.getDaemonEnrollment(enrollment.id);
      setEnrollment((current) => mergeEnrollmentStatus(current, next));
    } catch (err) {
      setWizardError(err instanceof Error ? err.message : "Unable to refresh enrollment status");
    }
  };

  const cancelEnrollment = async () => {
    if (!enrollment?.id) return;
    setWizardError("");
    try {
      await api.revokeDaemonEnrollment(enrollment.id);
      setEnrollment(null);
      setWizardState("idle");
      setCopyState("idle");
    } catch (err) {
      setWizardError(err instanceof Error ? err.message : "Unable to revoke enrollment");
    }
  };

  const setAgentOption = (name: string, value: string) => {
    setAgentOptions((current) => ({ ...current, [name]: value }));
  };

  const createAgent = async () => {
    if (!selectedAgentComputer || !selectedAgentRuntime || !selectedAgentTemplate) return;
    setAgentCreateState("creating");
    setAgentError("");
    setCreatedAgent(null);
    try {
      const result = await api.createDaemonAgent({
        computerId: selectedAgentComputer.computerId,
        runtimeId: selectedAgentRuntime.runtimeId,
        templateId: selectedAgentTemplate.templateId,
        displayName: agentDisplayName.trim(),
        name: agentName.trim() || undefined,
        target: agentTarget.trim() || undefined,
        options: {
          ...agentOptions,
          display_name: agentDisplayName.trim()
        }
      });
      setCreatedAgent(result);
      setAgentCreateState("created");
      onAgentCreated();
    } catch (err) {
      setAgentCreateState("error");
      setAgentError(err instanceof Error ? err.message : "Unable to create agent");
    }
  };

  const setAgentControlAction = (agentId: string, action: AgentControlAction) => {
    setAgentControlActionsById((current) => ({ ...current, [agentId]: action }));
  };

  const setAgentControlReason = (agentId: string, reason: string) => {
    setAgentControlReasons((current) => ({ ...current, [agentId]: reason }));
  };

  const setAgentMessageDraft = (agentId: string, content: string) => {
    setAgentMessageDrafts((current) => ({ ...current, [agentId]: content }));
  };

  const setAgentActionError = (agentId: string, error: string) => {
    setAgentActionErrors((current) => ({ ...current, [agentId]: error }));
  };

  const setAgentActionReceipt = (agentId: string, receipt: string) => {
    setAgentActionReceipts((current) => ({ ...current, [agentId]: receipt }));
  };

  const controlAgent = async (status: AgentStatusSnapshot) => {
    const action = agentControlActionsById[status.agentId] ?? "restart";
    setAgentActionBusy({ agentId: status.agentId, kind: "control" });
    setAgentActionError(status.agentId, "");
    setAgentActionReceipt(status.agentId, "");
    try {
      const result: AgentControlResult = await api.controlDaemonAgent(status.agentId, {
        action,
        reason: agentControlReasons[status.agentId],
        computerId: status.computerId,
        runtimeProfileId: status.runtimeProfileId,
        requestId: makeRequestId("agent-control")
      });
      setAgentActionReceipt(
        status.agentId,
        `${targetLabel(result.action)} ${result.state}${result.operationId ? ` · ${compactId(result.operationId)}` : ""}`
      );
      onAgentCreated();
    } catch (err) {
      setAgentActionError(status.agentId, err instanceof Error ? err.message : "Unable to control agent");
    } finally {
      setAgentActionBusy(null);
    }
  };

  const sendAgentDirectMessage = async (status: AgentStatusSnapshot) => {
    const content = (agentMessageDrafts[status.agentId] ?? "").trim();
    if (!content) {
      setAgentActionError(status.agentId, "Message content is required");
      return;
    }
    setAgentActionBusy({ agentId: status.agentId, kind: "message" });
    setAgentActionError(status.agentId, "");
    setAgentActionReceipt(status.agentId, "");
    try {
      const result: AgentDirectMessageResult = await api.sendDaemonAgentDirectMessage(status.agentId, {
        content,
        requestId: makeRequestId("agent-dm")
      });
      setAgentMessageDraft(status.agentId, "");
      setAgentActionReceipt(status.agentId, `Direct message queued · ${compactId(result.message.id)}`);
      onAgentCreated();
    } catch (err) {
      setAgentActionError(status.agentId, err instanceof Error ? err.message : "Unable to send direct message");
    } finally {
      setAgentActionBusy(null);
    }
  };

  return (
    <section className="content-grid">
      <div className="wide section-notice-row">
        <SectionStatusNotice loading={loading} message="Loading daemon state" />
        <SectionIssuesNotice issues={issues} onRetry={onRetry} />
      </div>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Add Computer</p>
            <h2>Install a daemon</h2>
          </div>
          <span className={`diagnostic-badge ${healthClass(currentEnrollmentStatus)}`}>
            {currentEnrollmentStatus}
          </span>
        </div>
        <div className="enrollment-layout">
          <form className="enrollment-form" onSubmit={(event) => {
            event.preventDefault();
            void createEnrollment();
          }}>
            <label>
              Display name
              <input
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
                placeholder="Office Mac mini"
              />
            </label>
            <label>
              Computer ID
              <input
                value={computerId}
                onChange={(event) => setComputerId(event.target.value)}
                placeholder="computer-office"
              />
            </label>
            <label>
              Hostname
              <input
                value={hostname}
                onChange={(event) => setHostname(event.target.value)}
                placeholder="office-mini"
              />
            </label>
            <div className="enrollment-actions">
              <button className="primary-button" type="submit" disabled={!canCreateEnrollment}>
                <Plus size={16} aria-hidden="true" />
                {canRegenerateEnrollment ? "Regenerate" : enrollment ? "Waiting" : "Create enrollment"}
              </button>
              {enrollment ? (
                <button className="secondary-button" type="button" onClick={refreshEnrollment}>
                  <RefreshCw size={16} aria-hidden="true" />
                  Refresh
                </button>
              ) : null}
              {enrollment && !canFinishEnrollment ? (
                <button className="secondary-button" type="button" onClick={() => void cancelEnrollment()}>
                  <X size={16} aria-hidden="true" />
                  Cancel
                </button>
              ) : null}
            </div>
          </form>
          <div className="enrollment-command">
            <div className="enrollment-status-grid">
              <div>
                <span>Enrollment</span>
                <strong>{enrollment?.id || "not created"}</strong>
              </div>
              <div>
                <span>Computer</span>
                <strong>{enrollment?.computerId || computerId || "pending"}</strong>
              </div>
              <div>
                <span>Connected</span>
                <strong>{formatUnixTime(enrollment?.connectedUnix)}</strong>
              </div>
              <div>
                <span>Heartbeat</span>
                <strong>{formatUnixTime(enrollment?.lastHeartbeatUnix)}</strong>
              </div>
            </div>
            {enrollment ? (
              <div className="platform-install-shell">
                <div className="platform-tabs" role="tablist" aria-label="Daemon install platform">
                  {platformCommands.map((entry) => (
                    <button
                      key={entry.platform}
                      type="button"
                      role="tab"
                      aria-selected={selectedPlatform === entry.platform}
                      className={selectedPlatform === entry.platform ? "is-active" : ""}
                      onClick={() => {
                        setSelectedPlatform(entry.platform);
                        setCopyState("idle");
                      }}
                    >
                      {entry.label}
                    </button>
                  ))}
                </div>
                {selectedCommand ? (
                  <div className={selectedCommand.ready ? "command-box" : "command-box is-pending"}>
                    <div>
                      <span>{selectedCommand.detail}</span>
                      <code>{selectedCommand.command}</code>
                    </div>
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={copyInstallCommand}
                      disabled={!selectedCommand.ready}
                    >
                      <Copy size={16} aria-hidden="true" />
                      {selectedCommand.ready ? (copyState === "copied" ? "Copied" : "Copy") : "Script pending"}
                    </button>
                  </div>
                ) : null}
              </div>
            ) : (
              <EmptyState icon={Monitor} title="Create an enrollment to get an install command" />
            )}
            {wizardError ? <p className="inline-error" role="alert">{wizardError}</p> : null}
            {copyState === "error" ? <p className="inline-error" role="alert">Clipboard access failed</p> : null}
            {canFinishEnrollment ? (
              <div className="enrollment-ready">
                <CheckCircle2 size={18} aria-hidden="true" />
                <span>{enrollment?.displayName || enrollment?.computerId || "Computer"} is connected.</span>
                <button className="primary-button" type="button" onClick={() => setEnrollment(null)}>
                  Finish
                </button>
              </div>
            ) : null}
          </div>
        </div>
      </section>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Add Agent</p>
            <h2>Create an agent instance</h2>
          </div>
          <span className={`diagnostic-badge ${healthClass(agentCreateState)}`}>
            {agentCreateState}
          </span>
        </div>
        {agentComputers.length ? (
          <form className="agent-create-form" onSubmit={(event) => {
            event.preventDefault();
            void createAgent();
          }}>
            <div className="agent-create-grid">
              <label>
                Computer
                <select
                  value={selectedAgentComputer?.computerId ?? ""}
                  onChange={(event) => {
                    setAgentComputerId(event.target.value);
                    setAgentRuntimeId("");
                    setAgentTemplateId("");
                  }}
                >
                  {agentComputers.map((computer) => (
                    <option key={computer.computerId} value={computer.computerId}>
                      {computer.displayName || computer.hostname || computer.computerId}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Runtime
                <select
                  value={selectedAgentRuntime?.runtimeId ?? ""}
                  onChange={(event) => {
                    setAgentRuntimeId(event.target.value);
                    setAgentTemplateId("");
                  }}
                >
                  {agentRuntimes.map((runtime) => (
                    <option key={runtime.runtimeId} value={runtime.runtimeId}>
                      {runtime.displayName || runtime.kind}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Template
                <select
                  value={selectedAgentTemplate?.templateId ?? ""}
                  onChange={(event) => setAgentTemplateId(event.target.value)}
                >
                  {agentTemplates.map((template) => (
                    <option key={template.templateId} value={template.templateId}>
                      {template.displayName || template.templateId}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Display name
                <input
                  value={agentDisplayName}
                  onChange={(event) => setAgentDisplayName(event.target.value)}
                  placeholder="Review Agent"
                  required
                />
              </label>
              <label>
                Agent name
                <input
                  value={agentName}
                  onChange={(event) => setAgentName(event.target.value)}
                  placeholder="review-agent"
                />
              </label>
              <label>
                Target
                <input
                  value={agentTarget}
                  onChange={(event) => setAgentTarget(event.target.value)}
                  placeholder={DEFAULT_TARGET}
                />
              </label>
            </div>
            {selectedAgentTemplate?.options.length ? (
              <div className="agent-options-grid">
                {selectedAgentTemplate.options.map((option) => (
                  <label key={option.name} className={option.type === "free_text" ? "span-2" : undefined}>
                    {option.label || option.name}{option.required ? " *" : ""}
                    {option.type === "enum" ? (
                      <select
                        value={optionInputValue(agentOptions, option)}
                        onChange={(event) => setAgentOption(option.name, event.target.value)}
                        required={option.required}
                      >
                        {!option.required ? <option value="">Default</option> : null}
                        {option.enum.map((item) => (
                          <option key={item} value={item}>{item}</option>
                        ))}
                      </select>
                    ) : option.type === "boolean" ? (
                      <select
                        value={optionInputValue(agentOptions, option) || "false"}
                        onChange={(event) => setAgentOption(option.name, event.target.value)}
                      >
                        <option value="true">true</option>
                        <option value="false">false</option>
                      </select>
                    ) : option.type === "free_text" ? (
                      <textarea
                        value={optionInputValue(agentOptions, option)}
                        onChange={(event) => setAgentOption(option.name, event.target.value)}
                        rows={3}
                        required={option.required}
                      />
                    ) : (
                      <input
                        type={option.sensitive ? "password" : option.type === "number" ? "number" : "text"}
                        value={optionInputValue(agentOptions, option)}
                        onChange={(event) => setAgentOption(option.name, event.target.value)}
                        required={option.required}
                      />
                    )}
                  </label>
                ))}
              </div>
            ) : null}
            <div className="enrollment-actions">
              <button className="primary-button" type="submit" disabled={!canCreateAgent}>
                <Plus size={16} aria-hidden="true" />
                {agentCreateState === "creating" ? "Creating" : "Create agent"}
              </button>
              {createdAgent ? (
                <span className="inline-success">
                  Created {createdAgent.agent.displayName || createdAgent.agent.agentId}
                </span>
              ) : null}
              {agentError ? <p className="inline-error" role="alert">{agentError}</p> : null}
            </div>
          </form>
        ) : (
          <EmptyState icon={Bot} title="No available runtime templates" />
        )}
      </section>
      <section className="panel wide">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Bridge Contract</p>
            <h2>HTTP bridge + SSE event stream</h2>
          </div>
        </div>
        <div className="contract-grid">
          {[
            ["Transport boundary", "Web uses SSE now; WebSocket can be added behind the API client later"],
            ["Cursor boundary", "Resume persists data.sequence and clears on server_id/protocol change"],
            ["DTO boundary", "Components receive camelCase frontend DTOs, not proto snake_case or enum numbers"],
            ["Realtime boundary", "Events invalidate/refetch server facts; append-only patching stays in client layer"]
          ].map(([title, detail]) => (
            <article className="contract-item" key={title}>
              <Circle size={16} aria-hidden="true" />
              <div>
                <strong>{title}</strong>
                <span>{detail}</span>
              </div>
            </article>
          ))}
        </div>
      </section>
      <MetricCard icon={Server} label="Protocol Path" value={protocol?.protoPath ?? "Loading"} />
      <MetricCard icon={CheckCircle2} label="Bridge Health" value={daemonInfo?.health ?? "unknown"} />
      <MetricCard icon={UsersRound} label="Agent Statuses" value={String(daemonInfo?.agentStatusCount ?? agentStatuses.length)} />
      <MetricCard icon={Wifi} label="SSE" value={realtimeStatus} />
      <MetricCard icon={Monitor} label="Computers" value={String(daemonInventory.length)} />
      <MetricCard icon={Bot} label="Runtime Templates" value={String(runtimeTemplateCount(daemonInventory))} />
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Runtime Health</p>
            <h2>Daemon bridge diagnostics</h2>
          </div>
        </div>
        <dl className="definition-list">
          <div>
            <dt>Server ID</dt>
            <dd>{daemonInfo?.serverId || "unavailable"}</dd>
          </div>
          <div>
            <dt>Protocol version</dt>
            <dd>{daemonInfo?.protocolVersion ?? "unknown"}</dd>
          </div>
          <div>
            <dt>gRPC address</dt>
            <dd>{daemonInfo?.grpcAddr || "unknown"}</dd>
          </div>
          <div>
            <dt>Daemon transport</dt>
            <dd>{daemonInfo?.daemonTransport || "unknown"}</dd>
          </div>
          <div>
            <dt>Cache driver</dt>
            <dd>{daemonInfo?.cacheDriver || "unknown"}</dd>
          </div>
          <div>
            <dt>Server time</dt>
            <dd>{formatUnixTime(daemonInfo?.serverTimeUnix)}</dd>
          </div>
          <div>
            <dt>Latest event</dt>
            <dd>
              {latestEvent
                ? `${latestEvent.kind}/${latestEvent.operation} sequence=${latestEvent.sequence}`
                : "none"}
            </dd>
          </div>
        </dl>
      </section>
      <MetricCard icon={Activity} label="Runs" value={String(daemonInfo?.runCount ?? daemonRuns.length)} />
      <MetricCard icon={AlertTriangle} label="Activity Logs" value={String(daemonInfo?.activityCount ?? daemonActivity.length)} />
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Inventory</p>
            <h2>Runtime types and agent templates</h2>
          </div>
        </div>
        {daemonInventory.length ? (
          <div className="runtime-preset-grid">
            {daemonInventory.flatMap((computer) =>
              computer.runtimes.map((runtime) => (
                <article className="runtime-preset" key={`${computer.computerId}-${runtime.runtimeId}`}>
                  <div>
                    <strong>{runtime.displayName || runtime.kind}</strong>
                    <span>
                      {computer.displayName || computer.hostname || computer.computerId} · {runtime.templates.length} template(s)
                      {runtime.runtimeType?.availabilityReason ? ` · ${runtime.runtimeType.availabilityReason}` : ""}
                    </span>
                  </div>
                  <div className="runtime-flags">
                    {runtime.runtimeType?.canonical === false ? <span>non-canonical</span> : <span>canonical</span>}
                    <span>{runtime.installed ? "installed" : "missing"}</span>
                    <span>{runtime.healthy ? "smoke passed" : runtime.runtimeType?.availability || "unavailable"}</span>
                    {runtime.runtimeType?.smoke?.category ? <span>{runtime.runtimeType.smoke.category}</span> : null}
                    {runtime.templates.some((template) => template.multiInstance) ? <span>multi-instance</span> : null}
                  </div>
                </article>
              ))
            )}
          </div>
        ) : (
          <EmptyState icon={Monitor} title="No daemon inventory reported" />
        )}
      </section>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Runtime Catalog</p>
            <h2>Slock.ai and Multica presets</h2>
          </div>
        </div>
        <div className="runtime-preset-grid">
          {runtimePresets.map((preset) => (
            <article className="runtime-preset" key={preset.kind}>
              <div>
                <strong>{preset.displayName || preset.kind}</strong>
                <span>{preset.description || `${preset.provider} / ${preset.command || preset.kind}`}</span>
              </div>
              <div className="runtime-flags">
                {preset.slockSupported ? <span>Slock</span> : null}
                {preset.multicaSupported ? <span>Multica</span> : null}
                {preset.recommended ? <span>preset</span> : null}
              </div>
            </article>
          ))}
        </div>
      </section>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Agents</p>
            <h2>Agent runtime health</h2>
          </div>
        </div>
        {manageableAgents.length ? (
          <div className="diagnostic-list">
            {manageableAgents.map((status) => {
              const controlBusy = agentActionBusy?.agentId === status.agentId && agentActionBusy.kind === "control";
              const messageBusy = agentActionBusy?.agentId === status.agentId && agentActionBusy.kind === "message";
              const action = agentControlActionsById[status.agentId] ?? "restart";
              const messageDraft = agentMessageDrafts[status.agentId] ?? "";
              const actionError = agentActionErrors[status.agentId];
              const actionReceipt = agentActionReceipts[status.agentId];
              return (
                <article className="diagnostic-row agent-management-row" key={`${status.agentId}-${status.target ?? "global"}`}>
                  <div className="agent-management-main">
                    <div className="agent-management-title">
                      <span className={`presence ${presenceClass(status.presence)}`} aria-label={status.presence || "idle"} />
                      <strong>{status.summary || status.agentId || "unknown agent"}</strong>
                    </div>
                    <span>{status.detail || status.agentId || "No runtime summary"}</span>
                    <div className="diagnostic-meta">
                      <span className={`diagnostic-badge ${healthClass(status.health)}`}>{status.health}</span>
                      <span>{status.presence || "presence_unknown"}</span>
                      <span>{status.activityState || "activity_unknown"}</span>
                      <span>{status.target || "global"}</span>
                      <span>{status.computerId || "computer_unknown"}</span>
                      <span>{formatUnixTime(status.updatedTimeUnix)}</span>
                    </div>
                    {actionError ? <p className="agent-action-feedback is-error" role="alert">{actionError}</p> : null}
                    {actionReceipt ? <p className="agent-action-feedback is-success" role="status">{actionReceipt}</p> : null}
                  </div>
                  <div className="agent-management-controls">
                    <div className="agent-control-strip">
                      <label>
                        Control
                        <select
                          value={action}
                          onChange={(event) => setAgentControlAction(status.agentId, event.target.value as AgentControlAction)}
                          disabled={controlBusy || messageBusy}
                        >
                          {agentControlActions.map((item) => (
                            <option key={item.value} value={item.value}>{item.label}</option>
                          ))}
                        </select>
                      </label>
                      <label>
                        Reason
                        <input
                          value={agentControlReasons[status.agentId] ?? ""}
                          onChange={(event) => setAgentControlReason(status.agentId, event.target.value)}
                          placeholder="Operator action"
                          disabled={controlBusy || messageBusy}
                        />
                      </label>
                      <button
                        className="secondary-button"
                        type="button"
                        onClick={() => void controlAgent(status)}
                        disabled={controlBusy || messageBusy}
                      >
                        <RefreshCw size={16} aria-hidden="true" />
                        {controlBusy ? "Queuing" : "Run control"}
                      </button>
                    </div>
                    <div className="agent-message-form">
                      <label>
                        Direct message
                        <textarea
                          value={messageDraft}
                          onChange={(event) => setAgentMessageDraft(status.agentId, event.target.value)}
                          placeholder="Send a direct instruction to this agent"
                          rows={2}
                          disabled={controlBusy || messageBusy}
                        />
                      </label>
                      <button
                        className="primary-button"
                        type="button"
                        onClick={() => void sendAgentDirectMessage(status)}
                        disabled={controlBusy || messageBusy || !messageDraft.trim()}
                      >
                        <Send size={16} aria-hidden="true" />
                        {messageBusy ? "Sending" : "Send"}
                      </button>
                    </div>
                  </div>
                </article>
              );
            })}
          </div>
        ) : (
          <EmptyState icon={Bot} title="No agent runtime status yet" />
        )}
      </section>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Runs</p>
            <h2>Runtime run queue</h2>
          </div>
        </div>
        <div className="board-note" role="note">
          <Activity size={16} aria-hidden="true" />
          Showing daemon runs across all targets, including direct-message and task-linked runs.
        </div>
        {daemonRuns.length ? (
          <div className="task-list-shell">
            <table className="task-table diagnostic-table">
              <thead>
                <tr>
                  <th>Run</th>
                  <th>State</th>
                  <th>Agent</th>
                  <th>Runtime</th>
                  <th>Target</th>
                  <th>Updated</th>
                  <th>Summary</th>
                </tr>
              </thead>
              <tbody>
                {daemonRuns.map((run) => (
                  <tr key={run.runId}>
                    <td>{compactId(run.runId)}</td>
                    <td><span className={`diagnostic-badge ${healthClass(run.state)}`}>{run.state}</span></td>
                    <td>{run.agentId || "n/a"}</td>
                    <td>{runtimeKindByProfile.get(run.runtimeProfileId || "") || run.runtimeProfileId || "unknown"}</td>
                    <td>{run.target || "global"}</td>
                    <td>{formatUnixTime(run.updatedTimeUnix || run.lastHeartbeatTimeUnix)}</td>
                    <td>{run.error || run.summary || "No summary"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState icon={Activity} title="No daemon runs recorded" />
        )}
      </section>
      <section className="panel wide">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Activity</p>
            <h2>Daemon activity timeline</h2>
          </div>
        </div>
        <div className="board-note" role="note">
          <History size={16} aria-hidden="true" />
          Showing persisted daemon activity across all scopes. Control requests remain visible after refresh.
        </div>
        {daemonActivity.length ? (
          <div className="event-stream" role="log" aria-label="Daemon activity">
            {daemonActivity.map((activity) => (
              <article className="event-row" key={activity.activityId}>
                <div className="event-main">
                  <Activity size={18} aria-hidden="true" />
                  <div>
                    <strong>{activity.kind || "activity"}</strong>
                    <span>{activity.summary || activity.detail || "No detail"}</span>
                    {activity.detail && activity.summary ? <span>{activity.detail}</span> : null}
                  </div>
                </div>
                <span className="event-sequence">
                  {activity.agentId || "system"} · {activity.target || "global"} · seq {activity.sequence ?? "n/a"}
                </span>
              </article>
            ))}
          </div>
        ) : (
          <EmptyState icon={Activity} title="No daemon activity recorded" />
        )}
      </section>
    </section>
  );
}

function StatusDot({ active, label }: { active: boolean; label: string }) {
  return (
    <span className={active ? "status-dot active" : "status-dot"}>
      <span aria-hidden="true" />
      {label}
    </span>
  );
}

function EmptyState({ icon: Icon, title }: { icon: typeof Activity; title: string }) {
  return (
    <div className="empty-state">
      <Icon size={22} aria-hidden="true" />
      <span>{title}</span>
    </div>
  );
}

export { App };
