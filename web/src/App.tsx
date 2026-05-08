import {
  Activity,
  AlertTriangle,
  AtSign,
  Bot,
  CheckCircle2,
  CheckSquare,
  Circle,
  CircleX,
  Columns3,
  Copy,
  Download,
  Eye,
  File,
  FileText,
  Hash,
  Image,
  Inbox,
  LayoutGrid,
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
  AgentStatusSnapshot,
  Attachment,
  AuthResponse,
  Channel,
  ChannelMember,
  CollaborationEvent,
  DaemonActivityRecord,
  DaemonInfo,
  DaemonRun,
  InteractionEndpoint,
  Message,
  ProtocolInfo,
  RuntimePreset,
  SetupStatus,
  Task,
  TaskState,
  ThreadInboxItem,
  User
} from "./types";

const TOKEN_KEY = "nekode.console.token";
const EVENT_CURSOR_KEY = "nekode.console.serverEvents.cursorState";
const DEFAULT_TARGET = "#general";

type ViewKey = "overview" | "inbox" | "messages" | "tasks" | "activity" | "skills" | "endpoints" | "daemon";
type LoadState = "idle" | "loading" | "ready" | "error";
type RealtimeStatus = "disabled" | "connecting" | "connected" | "error";
type TaskViewMode = "board" | "list";
type TaskStateFilter = TaskState | "all" | "open";
type TaskSortKey = "updated_desc" | "created_desc" | "summary_asc" | "state_asc";
type StoredEventCursor = {
  serverId: string;
  protocolVersion: number;
  sequence: number;
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

const navItems: Array<{ key: ViewKey; label: string; icon: typeof Activity }> = [
  { key: "overview", label: "Overview", icon: Activity },
  { key: "inbox", label: "Inbox", icon: Inbox },
  { key: "messages", label: "Messages", icon: MessageSquare },
  { key: "tasks", label: "Tasks", icon: Columns3 },
  { key: "activity", label: "Activity", icon: Activity },
  { key: "skills", label: "Skills", icon: Sparkles },
  { key: "endpoints", label: "Endpoints", icon: Settings },
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

function attachmentPreviewKind(attachment: Attachment) {
  const mimeType = attachment.mimeType.toLowerCase().split(";")[0].trim();
  if (mimeType.startsWith("image/")) return "image";
  if (mimeType === "text/html" || attachment.filename.toLowerCase().endsWith(".html")) return "html";
  return "file";
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

function healthClass(value?: string) {
  const normalized = (value || "unknown").toLowerCase();
  if (normalized === "ok" || normalized === "online" || normalized === "running") return "is-ok";
  if (normalized === "idle" || normalized === "unspecified" || normalized === "queued") return "is-idle";
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

function App() {
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) ?? "");
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<ViewKey>("overview");
  const [status, setStatus] = useState<LoadState>("idle");
  const [error, setError] = useState("");
  const [protocol, setProtocol] = useState<ProtocolInfo | null>(null);
  const [daemonInfo, setDaemonInfo] = useState<DaemonInfo | null>(null);
  const [realtimeStatus, setRealtimeStatus] = useState<RealtimeStatus>("disabled");
  const [realtimeReconnectAttempt, setRealtimeReconnectAttempt] = useState(0);
  const [latestEvent, setLatestEvent] = useState<CollaborationEvent | null>(null);
  const [events, setEvents] = useState<CollaborationEvent[]>([]);
  const [agentStatuses, setAgentStatuses] = useState<AgentStatusSnapshot[]>([]);
  const [daemonRuns, setDaemonRuns] = useState<DaemonRun[]>([]);
  const [daemonActivity, setDaemonActivity] = useState<DaemonActivityRecord[]>([]);
  const [runtimePresets, setRuntimePresets] = useState<RuntimePreset[]>(fallbackRuntimePresets);
  const [endpoints, setEndpoints] = useState<InteractionEndpoint[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [channelMembers, setChannelMembers] = useState<ChannelMember[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [threadInbox, setThreadInbox] = useState<ThreadInboxItem[]>([]);
  const [activeThread, setActiveThread] = useState<ThreadInboxItem | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [target, setTarget] = useState(DEFAULT_TARGET);

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedTaskId) ?? null,
    [selectedTaskId, tasks]
  );

  const loadData = useCallback(async (options: { background?: boolean } = {}) => {
    if (!token) return;
    api.setToken(token);
    if (!options.background) {
      setStatus((current) => (current === "ready" ? current : "loading"));
    }
    setError("");
    try {
      const messageTarget = activeThread?.target ?? target;
      const messageThreadID = activeThread?.threadId ?? "";
      const coreData = await Promise.all([
        api.me(),
        api.protocol(),
        api.daemonInfo().catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return null;
        }),
        api.listInteractionEndpoints(),
        api.listChannels({ joinedOnly: false }).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listChannelMembers(messageTarget).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listMessages(messageTarget, 50, messageThreadID),
        api.listThreadInbox({ limit: 100 }),
        api.listTasks({ target })
      ]);
      const [
        me,
        protocolInfo,
        daemonBridgeInfo,
        endpointList,
        channelList,
        channelMemberList,
        messageList,
        inboxList,
        taskList
      ] = coreData;
      setUser(me);
      setProtocol(protocolInfo);
      setDaemonInfo((current) => (sameDaemonInfo(current, daemonBridgeInfo) ? current : daemonBridgeInfo));
      setEndpoints(endpointList.items);
      setChannels(channelList.items.length ? channelList.items : fallbackChannels());
      setChannelMembers(channelMemberList.items);
      setMessages(messageList.items);
      setThreadInbox(inboxList.items);
      setTasks(taskList.items);
      setStatus("ready");

      const [
        eventList,
        agentStatusList,
        daemonRunList,
        daemonActivityList,
        runtimePresetList
      ] = await Promise.all([
        api.listDaemonEvents({ target, limit: 80 }).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listAgentStatuses({ target, limit: 100 }).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listDaemonRuns({ target, limit: 100 }).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listDaemonActivity({ target, limit: 100 }).catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return { items: [] };
        }),
        api.listRuntimePresets({ includeExperimental: true })
      ]);
      setEvents(eventList.items);
      setAgentStatuses(agentStatusList.items);
      setDaemonRuns(daemonRunList.items);
      setDaemonActivity(daemonActivityList.items);
      setRuntimePresets(runtimePresetList.items.length ? runtimePresetList.items : fallbackRuntimePresets);
    } catch (err) {
      if (isAuthError(err)) {
        localStorage.removeItem(TOKEN_KEY);
        setToken("");
        setUser(null);
        setStatus("idle");
        return;
      }
      setError(err instanceof Error ? err.message : "Unable to load console data");
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
    setDaemonInfo(null);
    setLatestEvent(null);
    setEvents([]);
    setAgentStatuses([]);
    setDaemonRuns([]);
    setDaemonActivity([]);
    setChannels([]);
    setChannelMembers([]);
    setThreadInbox([]);
    setActiveThread(null);
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
          {demoAgents.map((agent) => (
            <button
              key={agent.id}
              className="agent-link"
              type="button"
              onClick={() => {
                setActiveThread(null);
                setTarget(`dm:@${agent.name.toLowerCase()}`);
                setView("messages");
              }}
            >
              <AvatarBadge label={agent.name} color={agent.color} />
              <span>{agent.name}</span>
              <span className={`presence ${agent.status}`} aria-label={agent.status} />
            </button>
          ))}
        </SidebarSection>
        <SidebarSection title="Machines">
          <button className="side-link" type="button" onClick={() => setView("daemon")}>
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

        {view === "overview" ? (
          <Overview
            protocol={protocol}
            daemonInfo={daemonInfo}
            realtimeStatus={realtimeStatus}
            latestEvent={latestEvent}
            endpoints={endpoints}
            messages={messages}
            tasks={tasks}
            events={events}
          />
        ) : null}
        {view === "inbox" ? (
          <InboxPanel
            items={threadInbox}
            onOpenThread={openThread}
            onMarkRead={markThreadRead}
            onMarkAllRead={markThreadInboxRead}
          />
        ) : null}
        {view === "messages" ? (
          <MessagesPanel
            target={activeThread?.target ?? target}
            thread={activeThread}
            messages={messages}
            endpoints={endpoints}
            onClearThread={() => setActiveThread(null)}
            onMarkThreadRead={activeThread ? () => markThreadRead(activeThread) : undefined}
            channel={channels.find((item) => item.target === (activeThread?.target ?? target)) ?? null}
            channelMembers={channelMembers}
            onCreated={loadData}
          />
        ) : null}
        {view === "tasks" ? (
          <TasksPanel
            target={target}
            tasks={tasks}
            selectedTask={selectedTask}
            onSelectTask={setSelectedTaskId}
            onChanged={loadData}
          />
        ) : null}
        {view === "activity" ? (
          <ActivityPanel
            target={target}
            events={events}
            latestEvent={latestEvent}
            realtimeStatus={realtimeStatus}
            onRefresh={loadData}
          />
        ) : null}
        {view === "skills" ? <SkillsPanel runtimePresets={runtimePresets} /> : null}
        {view === "endpoints" ? (
          <EndpointsPanel endpoints={endpoints} onCreated={loadData} />
        ) : null}
        {view === "daemon" ? (
          <DaemonPanel
            protocol={protocol}
            daemonInfo={daemonInfo}
            realtimeStatus={realtimeStatus}
            latestEvent={latestEvent}
            agentStatuses={agentStatuses}
            daemonRuns={daemonRuns}
            daemonActivity={daemonActivity}
            runtimePresets={runtimePresets}
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

function Overview({
  protocol,
  daemonInfo,
  realtimeStatus,
  latestEvent,
  endpoints,
  messages,
  tasks,
  events
}: {
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  latestEvent: CollaborationEvent | null;
  endpoints: InteractionEndpoint[];
  messages: Message[];
  tasks: Task[];
  events: CollaborationEvent[];
}) {
  const activeTasks = tasks.filter((task) => task.state !== "done" && task.state !== "canceled").length;
  return (
    <section className="content-grid">
      <MetricCard icon={Server} label="Protocol" value={protocol?.name ?? "Unknown"} />
      <MetricCard icon={Wifi} label="Realtime" value={realtimeStatus} />
      <MetricCard icon={Settings} label="Endpoints" value={String(endpoints.length)} />
      <MetricCard icon={Columns3} label="Open Tasks" value={String(activeTasks)} />
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
  onMarkAllRead
}: {
  items: ThreadInboxItem[];
  onOpenThread: (item: ThreadInboxItem) => Promise<void>;
  onMarkRead: (item: ThreadInboxItem) => Promise<void>;
  onMarkAllRead: () => Promise<void>;
}) {
  const [busyThread, setBusyThread] = useState("");
  const unreadCount = items.reduce((sum, item) => sum + item.unreadCount, 0);
  const nextUnread = items.find((item) => item.unreadCount > 0) ?? items[0] ?? null;

  const runItemAction = async (item: ThreadInboxItem, action: (item: ThreadInboxItem) => Promise<void>) => {
    setBusyThread(item.threadId);
    try {
      await action(item);
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
          <button className="secondary-button" type="button" disabled={!unreadCount} onClick={onMarkAllRead}>
            <CheckCircle2 size={16} aria-hidden="true" />
            Mark all read
          </button>
        </div>
      </div>
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
  endpoints,
  onClearThread,
  onMarkThreadRead,
  channel,
  channelMembers,
  onCreated
}: {
  target: string;
  thread: ThreadInboxItem | null;
  messages: Message[];
  endpoints: InteractionEndpoint[];
  onClearThread: () => void;
  onMarkThreadRead?: () => Promise<void>;
  channel: Channel | null;
  channelMembers: ChannelMember[];
  onCreated: () => Promise<void>;
}) {
  const [content, setContent] = useState("");
  const [sourceEndpointId, setSourceEndpointId] = useState("");
  const [draftAttachments, setDraftAttachments] = useState<Attachment[]>([]);
  const [selectedMessageIds, setSelectedMessageIds] = useState<Set<string>>(() => new Set());
  const [lightboxAttachment, setLightboxAttachment] = useState<Attachment | null>(null);
  const [busy, setBusy] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => new Set());
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const ordered = useMemo(() => [...messages].reverse(), [messages]);
  const feed = useMemo(() => buildMessageFeed(ordered), [ordered]);
  const selectedMessages = useMemo(
    () => ordered.filter((message) => selectedMessageIds.has(message.id)),
    [ordered, selectedMessageIds]
  );

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!content.trim() && draftAttachments.length === 0) return;
    setBusy(true);
    try {
      await api.createMessage({
        target,
        threadId: thread?.threadId,
        content: content.trim() || "(attachment)",
        attachmentIds: draftAttachments.map((attachment) => attachment.id),
        sourceEndpointId,
        requestId: makeRequestId("msg")
      });
      setContent("");
      setDraftAttachments([]);
      await onCreated();
    } finally {
      setBusy(false);
    }
  };

  const uploadFiles = async (files: FileList | null) => {
    if (!files?.length) return;
    setUploading(true);
    try {
      const uploaded = await Promise.all(Array.from(files).map((file) => api.uploadAttachment(target, file)));
      setDraftAttachments((current) => [...current, ...uploaded]);
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
    await navigator.clipboard.writeText(text);
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
                <button className="secondary-button" type="button" onClick={() => void onMarkThreadRead()}>
                  <CheckCircle2 size={16} aria-hidden="true" />
                  Mark read
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
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
      />
    </section>
  );
}

function ChannelAccessPanel({
  channel,
  members,
  onMention
}: {
  channel: Channel | null;
  members: ChannelMember[];
  onMention: (name: string) => void;
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
  onPreviewAttachment
}: {
  message: Message;
  compact?: boolean;
  selected?: boolean;
  onToggleSelected?: (messageId: string) => void;
  onPreviewAttachment?: (attachment: Attachment) => void;
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
        <span>{unixTime(message.createdUnix)}</span>
      </header>
      <p>{message.content}</p>
      {message.attachments?.length && onPreviewAttachment ? (
        <div className="attachment-grid">
          {message.attachments.map((attachment) => (
            <AttachmentPreview key={attachment.id} attachment={attachment} onPreview={onPreviewAttachment} />
          ))}
        </div>
      ) : null}
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
  const kind = attachmentPreviewKind(attachment);

  useEffect(() => {
    let disposed = false;
    let url = "";
    if (kind === "image" || kind === "html") {
      api.downloadAttachment(attachment).then((blob) => {
        if (disposed) return;
        url = URL.createObjectURL(blob);
        setObjectUrl(url);
      }).catch(() => undefined);
    }
    return () => {
      disposed = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [attachment, kind]);

  const icon = kind === "image" ? Image : kind === "html" ? FileText : File;
  const Icon = icon;

  return (
    <div className="attachment-preview">
      <button type="button" onClick={() => onPreview(attachment)} aria-label={`Preview ${attachment.filename}`}>
        {kind === "image" && objectUrl ? (
          <img src={objectUrl} alt={attachment.filename} />
        ) : kind === "html" && objectUrl ? (
          <iframe title={attachment.filename} src={objectUrl} sandbox="" />
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

function AttachmentLightbox({ attachment, onClose }: { attachment: Attachment; onClose: () => void }) {
  const [objectUrl, setObjectUrl] = useState("");
  const kind = attachmentPreviewKind(attachment);

  useEffect(() => {
    let disposed = false;
    let url = "";
    api.downloadAttachment(attachment).then((blob) => {
      if (disposed) return;
      url = URL.createObjectURL(blob);
      setObjectUrl(url);
    }).catch(() => undefined);
    return () => {
      disposed = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [attachment]);

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
  selectedTask,
  onSelectTask,
  onChanged
}: {
  target: string;
  tasks: Task[];
  selectedTask: Task | null;
  onSelectTask: (taskId: string | null) => void;
  onChanged: () => Promise<void>;
}) {
  const [summary, setSummary] = useState("");
  const [newTaskState, setNewTaskState] = useState<TaskState>("todo");
  const [viewMode, setViewMode] = useState<TaskViewMode>("board");
  const [stateFilter, setStateFilter] = useState<TaskStateFilter>("all");
  const [sortKey, setSortKey] = useState<TaskSortKey>("updated_desc");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
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
    } finally {
      setBusy(false);
    }
  };

  const moveTask = async (task: Task, state: TaskState) => {
    const updated = await api.updateTask(task.id, { state });
    setTaskReceipt({
      id: updated.id || task.id,
      summary: updated.summary || task.summary,
      state: updated.state || state,
      action: "moved",
      createdUnix: Math.floor(Date.now() / 1000)
    });
    await onChanged();
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
  onClose,
  onMove,
  onChanged
}: {
  task: Task | null;
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
  onRefresh
}: {
  target: string;
  events: CollaborationEvent[];
  latestEvent: CollaborationEvent | null;
  realtimeStatus: RealtimeStatus;
  onRefresh: () => Promise<void>;
}) {
  const ordered = useMemo(() => [...events].sort((a, b) => b.sequence - a.sequence), [events]);

  return (
    <section className="two-column">
      <div className="panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{target}</p>
            <h2>Activity Stream</h2>
          </div>
          <button className="secondary-button" type="button" onClick={() => void onRefresh()}>
            <RefreshCw size={16} aria-hidden="true" />
            Refetch
          </button>
        </div>
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

function EndpointsPanel({
  endpoints,
  onCreated
}: {
  endpoints: InteractionEndpoint[];
  onCreated: () => Promise<void>;
}) {
  const [displayName, setDisplayName] = useState("Web Console");
  const [kind, setKind] = useState("web");
  const [provider, setProvider] = useState("browser");
  const [busy, setBusy] = useState(false);

  const createEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      await api.createInteractionEndpoint({
        kind,
        provider,
        displayName,
        targetPrefix: "#",
        inboundEnabled: true,
        outboundEnabled: true,
        authMode: "bearer",
        configJson: "{}"
      });
      await onCreated();
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="two-column">
      <div className="panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Interaction Layer</p>
            <h2>Endpoints</h2>
          </div>
        </div>
        <div className="endpoint-list">
          {endpoints.length ? (
            endpoints.map((endpoint) => (
              <article className="endpoint-row" key={endpoint.id}>
                <div>
                  <strong>{endpoint.displayName}</strong>
                  <span>
                    {endpoint.kind} / {endpoint.provider}
                  </span>
                </div>
                <div className="endpoint-flags">
                  <StatusDot active={endpoint.inboundEnabled} label="Inbound" />
                  <StatusDot active={endpoint.outboundEnabled} label="Outbound" />
                </div>
              </article>
            ))
          ) : (
            <EmptyState icon={Settings} title="No endpoints configured" />
          )}
        </div>
      </div>
      <form className="panel compact form-stack" onSubmit={createEndpoint}>
        <p className="eyebrow">Create Endpoint</p>
        <label>
          Display name
          <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
        </label>
        <label>
          Kind
          <input value={kind} onChange={(event) => setKind(event.target.value)} />
        </label>
        <label>
          Provider
          <input value={provider} onChange={(event) => setProvider(event.target.value)} />
        </label>
        <button className="primary-button" type="submit" disabled={busy}>
          <Plus size={16} aria-hidden="true" />
          Create
        </button>
      </form>
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
  daemonRuns,
  daemonActivity,
  runtimePresets
}: {
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  latestEvent: CollaborationEvent | null;
  agentStatuses: AgentStatusSnapshot[];
  daemonRuns: DaemonRun[];
  daemonActivity: DaemonActivityRecord[];
  runtimePresets: RuntimePreset[];
}) {
  return (
    <section className="content-grid">
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
        {agentStatuses.length ? (
          <div className="diagnostic-list">
            {agentStatuses.map((status) => (
              <article className="diagnostic-row" key={`${status.agentId}-${status.target ?? "global"}`}>
                <div>
                  <strong>{status.agentId || "unknown agent"}</strong>
                  <span>{status.summary || status.detail || status.target || "No runtime summary"}</span>
                </div>
                <div className="diagnostic-meta">
                  <span className={`diagnostic-badge ${healthClass(status.health)}`}>{status.health}</span>
                  <span>{status.presence || "presence_unknown"}</span>
                  <span>{status.activityState || "activity_unknown"}</span>
                  <span>{status.target || "global"}</span>
                  <span>{formatUnixTime(status.updatedTimeUnix)}</span>
                </div>
              </article>
            ))}
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
        {daemonRuns.length ? (
          <div className="task-list-shell">
            <table className="task-table diagnostic-table">
              <thead>
                <tr>
                  <th>Run</th>
                  <th>State</th>
                  <th>Agent</th>
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
        {daemonActivity.length ? (
          <div className="event-stream" role="log" aria-label="Daemon activity">
            {daemonActivity.map((activity) => (
              <article className="event-row" key={activity.activityId}>
                <div className="event-main">
                  <Activity size={18} aria-hidden="true" />
                  <div>
                    <strong>{activity.kind || "activity"}</strong>
                    <span>{activity.summary || activity.detail || "No detail"}</span>
                  </div>
                </div>
                <span className="event-sequence">
                  {activity.agentId || "system"} · seq {activity.sequence ?? "n/a"}
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
