import {
  Activity,
  AlertTriangle,
  AtSign,
  Bot,
  CheckCircle2,
  Circle,
  CircleX,
  Columns3,
  Eye,
  Hash,
  LayoutGrid,
  List,
  ListFilter,
  Loader2,
  LogOut,
  MessageSquare,
  Monitor,
  OctagonAlert,
  Plus,
  RefreshCw,
  Search,
  Send,
  Server,
  Settings,
  Shield,
  Sparkles,
  UsersRound,
  Wifi
} from "lucide-react";
import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { ApiClient, ApiError, makeRequestId } from "./api";
import brandMarkUrl from "./assets-brand.png";
import type {
  AgentStatusSnapshot,
  AuthResponse,
  CollaborationEvent,
  DaemonActivityRecord,
  DaemonInfo,
  DaemonRun,
  InteractionEndpoint,
  Message,
  ProtocolInfo,
  SetupStatus,
  Task,
  TaskState,
  User
} from "./types";

const TOKEN_KEY = "nekode.console.token";
const EVENT_CURSOR_KEY = "nekode.console.serverEvents.cursorState";
const DEFAULT_TARGET = "#general";

type ViewKey = "overview" | "messages" | "tasks" | "activity" | "skills" | "endpoints" | "daemon";
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
  { key: "messages", label: "Messages", icon: MessageSquare },
  { key: "tasks", label: "Tasks", icon: Columns3 },
  { key: "activity", label: "Activity", icon: Activity },
  { key: "skills", label: "Skills", icon: Sparkles },
  { key: "endpoints", label: "Endpoints", icon: Settings },
  { key: "daemon", label: "Daemon", icon: Bot }
];

const workspaceChannels = ["#general", "#ops", "#release"];

const demoAgents = [
  { id: "qa", name: "QA", role: "Verifier", status: "online", color: "#b46b2b" },
  { id: "architect", name: "Architect", role: "System design", status: "online", color: "#7b4ee6" },
  { id: "developer", name: "Developer", role: "Implementation", status: "busy", color: "#2b79b4" },
  { id: "reviewer", name: "Reviewer", role: "Code review", status: "idle", color: "#b4432b" }
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

function shouldInvalidateForEvent(event: CollaborationEvent) {
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

function App() {
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) ?? "");
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<ViewKey>("overview");
  const [status, setStatus] = useState<LoadState>("idle");
  const [error, setError] = useState("");
  const [protocol, setProtocol] = useState<ProtocolInfo | null>(null);
  const [daemonInfo, setDaemonInfo] = useState<DaemonInfo | null>(null);
  const [realtimeStatus, setRealtimeStatus] = useState<RealtimeStatus>("disabled");
  const [latestEvent, setLatestEvent] = useState<CollaborationEvent | null>(null);
  const [events, setEvents] = useState<CollaborationEvent[]>([]);
  const [agentStatuses, setAgentStatuses] = useState<AgentStatusSnapshot[]>([]);
  const [daemonRuns, setDaemonRuns] = useState<DaemonRun[]>([]);
  const [daemonActivity, setDaemonActivity] = useState<DaemonActivityRecord[]>([]);
  const [endpoints, setEndpoints] = useState<InteractionEndpoint[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [target, setTarget] = useState(DEFAULT_TARGET);

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedTaskId) ?? null,
    [selectedTaskId, tasks]
  );

  const loadData = useCallback(async () => {
    if (!token) return;
    api.setToken(token);
    setStatus("loading");
    setError("");
    try {
      const [
        me,
        protocolInfo,
        daemonBridgeInfo,
        eventList,
        agentStatusList,
        daemonRunList,
        daemonActivityList,
        endpointList,
        messageList,
        taskList
      ] = await Promise.all([
        api.me(),
        api.protocol(),
        api.daemonInfo().catch((err: unknown) => {
          if (isAuthError(err)) throw err;
          return null;
        }),
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
        api.listInteractionEndpoints(),
        api.listMessages(target),
        api.listTasks({ target })
      ]);
      setUser(me);
      setProtocol(protocolInfo);
      setDaemonInfo((current) => (sameDaemonInfo(current, daemonBridgeInfo) ? current : daemonBridgeInfo));
      setEvents(eventList.items);
      setAgentStatuses(agentStatusList.items);
      setDaemonRuns(daemonRunList.items);
      setDaemonActivity(daemonActivityList.items);
      setEndpoints(endpointList.items);
      setMessages(messageList.items);
      setTasks(taskList.items);
      setStatus("ready");
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
  }, [target, token]);

  useEffect(() => {
    if (token) void loadData();
  }, [loadData, token]);

  useEffect(() => {
    if (!token || !daemonInfo) {
      setRealtimeStatus("disabled");
      return undefined;
    }

    const storedCursor = readStoredEventCursor(daemonInfo);
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
        if (shouldInvalidateForEvent(event)) {
          void loadData();
        }
      },
      onError: () => {
        setRealtimeStatus("error");
      }
    });

    return () => unsubscribe.close();
  }, [daemonInfo, loadData, token]);

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
    setSelectedTaskId(null);
    setRealtimeStatus("disabled");
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
          {workspaceChannels.map((channel) => (
            <button
              key={channel}
              className={target === channel ? "side-link is-active" : "side-link"}
              type="button"
              onClick={() => {
                setTarget(channel);
                setView("messages");
              }}
            >
              <Hash size={15} aria-hidden="true" />
              <span>{channel.replace("#", "")}</span>
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
                onChange={(event) => setTarget(event.target.value)}
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
        {view === "messages" ? (
          <MessagesPanel
            target={target}
            messages={messages}
            endpoints={endpoints}
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
        {view === "skills" ? <SkillsPanel /> : null}
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

function MessagesPanel({
  target,
  messages,
  endpoints,
  onCreated
}: {
  target: string;
  messages: Message[];
  endpoints: InteractionEndpoint[];
  onCreated: () => Promise<void>;
}) {
  const [content, setContent] = useState("");
  const [sourceEndpointId, setSourceEndpointId] = useState("");
  const [busy, setBusy] = useState(false);
  const ordered = useMemo(() => [...messages].reverse(), [messages]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!content.trim()) return;
    setBusy(true);
    try {
      await api.createMessage({
        target,
        content,
        sourceEndpointId,
        requestId: makeRequestId("msg")
      });
      setContent("");
      await onCreated();
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="two-column">
      <div className="panel message-panel">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">{target}</p>
            <h2>Messages</h2>
          </div>
        </div>
        <div className="message-list" role="log" aria-label="Messages">
          {ordered.length ? (
            ordered.map((message) => <MessageBubble key={message.id} message={message} />)
          ) : (
            <EmptyState icon={MessageSquare} title="No messages loaded" />
          )}
        </div>
        <form className="composer" onSubmit={submit}>
          <textarea
            aria-label="Message content"
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder="Message this target"
            rows={3}
          />
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
            <button className="primary-button" type="submit" disabled={busy || !content.trim()}>
              <Send size={16} aria-hidden="true" />
              Send
            </button>
          </div>
        </form>
      </div>
      <aside className="panel compact">
        <div className="panel-heading compact-heading">
          <div>
            <p className="eyebrow">Agents</p>
            <h2>Room roster</h2>
          </div>
        </div>
        <div className="agent-roster">
          {demoAgents.map((agent) => (
            <article className="agent-row" key={agent.id}>
              <AvatarBadge label={agent.name} color={agent.color} />
              <div>
                <strong>{agent.name}</strong>
                <span>{agent.role}</span>
              </div>
              <span className={`presence ${agent.status}`} aria-label={agent.status} />
            </article>
          ))}
        </div>
        <div className="mention-chips" aria-label="Mention shortcuts">
          {demoAgents.map((agent) => (
            <button
              key={agent.id}
              type="button"
              onClick={() => setContent((current) => `${current}${current ? " " : ""}@${agent.name} `)}
            >
              <AtSign size={14} aria-hidden="true" />
              {agent.name}
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
    </section>
  );
}

function MessageBubble({ message }: { message: Message }) {
  return (
    <article className="message-bubble">
      <header>
        <strong>{message.senderDisplayName || message.senderKind}</strong>
        <span>{unixTime(message.createdUnix)}</span>
      </header>
      <p>{message.content}</p>
    </article>
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
      await api.createTask({ summary, target, state: newTaskState });
      setSummary("");
      setNewTaskState("todo");
      await onChanged();
    } finally {
      setBusy(false);
    }
  };

  const moveTask = async (task: Task, state: TaskState) => {
    await api.updateTask(task.id, { state });
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
                  <th>Move</th>
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
    </article>
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

function SkillsPanel() {
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
              <option value="codex">Codex</option>
              <option value="claude">Claude</option>
              <option value="opencode">OpenCode</option>
            </select>
          </label>
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
  daemonActivity
}: {
  protocol: ProtocolInfo | null;
  daemonInfo: DaemonInfo | null;
  realtimeStatus: RealtimeStatus;
  latestEvent: CollaborationEvent | null;
  agentStatuses: AgentStatusSnapshot[];
  daemonRuns: DaemonRun[];
  daemonActivity: DaemonActivityRecord[];
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
