// One typed function per route in docs/api.md. Browser calls authenticate with the session
// cookie that the server set at sign-in, which the browser sends by itself, so no call carries
// a credential here. A 401 on a normal call dispatches UNAUTHORIZED_EVENT (the session ended).
// Sign-in, setup and the state check pass auth:false: they run before there is a session, so a
// 401 from them is a wrong password, not an expired session. Sign-out and password change run
// inside a session, so a 401 from them means the session is gone and the app must sign in again.
import { UNAUTHORIZED_EVENT } from './auth';
import type {
  AgentView,
  Approval,
  AuthStateResponse,
  Config,
  ConfigChange,
  ConfigDocument,
  ConfigProblem,
  ConfigSaved,
  ConfigText,
  Conversation,
  ConversationDetail,
  Deployment,
  FoundryEvent,
  Friction,
  LogLine,
  LoopView,
  NewTask,
  ChatMessage,
  PausedBy,
  ProjectView,
  Role,
  Run,
  Signal,
  Stats,
  Task,
  TaskDetail,
  User,
} from './types';

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

type QueryValue = string | number | boolean | undefined | null;

function withQuery(path: string, query: Record<string, QueryValue>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== '') params.set(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

async function request<T>(
  method: string,
  path: string,
  options: { body?: unknown; auth?: boolean } = {},
): Promise<T> {
  const auth = options.auth ?? true;
  const headers: Record<string, string> = {};
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });

  if (!res.ok) {
    if (res.status === 401 && auth) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    let message = res.statusText || `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // Not JSON: keep the status text.
    }
    throw new ApiError(res.status, message);
  }

  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

// ---- accounts and sessions ----

export const getAuthState = () => request<AuthStateResponse>('GET', '/auth/state', { auth: false });

export const setupOwner = (username: string, password: string) =>
  request<User>('POST', '/auth/setup', { body: { username, password }, auth: false });

export const login = (username: string, password: string) =>
  request<User>('POST', '/auth/login', { body: { username, password }, auth: false });

export const logout = () => request<void>('POST', '/auth/logout');

/** A wrong current password is 403 (not 401), so a 401 here still means the session ended. */
export const changePassword = (current: string, next: string) =>
  request<void>('POST', '/auth/password', { body: { current, new: next } });

// ---- users (owner only) ----

export const listUsers = () => request<User[]>('GET', '/users');

export const createUser = (username: string, password: string, role: Role) =>
  request<User>('POST', '/users', { body: { username, password, role } });

export const setUserRole = (name: string, role: Role) =>
  request<User>('POST', `/users/${encodeURIComponent(name)}/role`, { body: { role } });

export const setUserPassword = (name: string, password: string) =>
  request<void>('POST', `/users/${encodeURIComponent(name)}/password`, { body: { password } });

export const deleteUser = (name: string) => request<void>('DELETE', `/users/${encodeURIComponent(name)}`);

// ---- health and events ----

export const getHealth = () => request<{ ok: boolean; version: string }>('GET', '/health', { auth: false });

export const getEventsHistory = (q: { after?: number; project?: string; task_id?: string; limit?: number } = {}) =>
  request<FoundryEvent[]>('GET', withQuery('/events/history', q));

// ---- stats and control ----

export const getStats = () => request<Stats>('GET', '/stats');

export const getControl = () => request<{ paused: boolean }>('GET', '/control');

export const pauseFactory = () => request<{ paused: boolean }>('POST', '/control/pause');

export const resumeFactory = () => request<{ paused: boolean }>('POST', '/control/resume');

// ---- projects and tasks ----

export const listProjects = () => request<ProjectView[]>('GET', '/projects');

export interface ProjectPauseState {
  paused: boolean;
  paused_by: PausedBy;
}

/** Pauses a project. The server answers 409 when it is paused in foundry.yaml and cannot be resumed here. */
export const pauseProject = (name: string) =>
  request<ProjectPauseState>('POST', `/projects/${encodeURIComponent(name)}/pause`);

export const resumeProject = (name: string) =>
  request<ProjectPauseState>('POST', `/projects/${encodeURIComponent(name)}/resume`);

export interface TaskQuery {
  project?: string;
  /** Comma-separated statuses. */
  status?: string;
  kind?: string;
  limit?: number;
}

export const listTasks = (q: TaskQuery = {}) => request<Task[]>('GET', withQuery('/tasks', { ...q }));

export const createTask = (task: NewTask) => request<Task>('POST', '/tasks', { body: task });

export const getTask = (id: string) => request<TaskDetail>('GET', `/tasks/${encodeURIComponent(id)}`);

export const cancelTask = (id: string) => request<void>('POST', `/tasks/${encodeURIComponent(id)}/cancel`);

export const retryTask = (id: string, note = '') =>
  request<void>('POST', `/tasks/${encodeURIComponent(id)}/retry`, { body: { note } });

// ---- runs ----

export interface RunQuery {
  task_id?: string;
  project?: string;
  /** Comma-separated statuses. */
  status?: string;
  limit?: number;
}

export const listRuns = (q: RunQuery = {}) => request<Run[]>('GET', withQuery('/runs', { ...q }));

export const getRun = (id: string) => request<Run>('GET', `/runs/${encodeURIComponent(id)}`);

export const getRunLogs = (id: string, q: { after?: number; limit?: number } = {}) =>
  request<LogLine[]>('GET', withQuery(`/runs/${encodeURIComponent(id)}/logs`, { ...q }));

export const cancelRun = (id: string) => request<void>('POST', `/runs/${encodeURIComponent(id)}/cancel`);

// ---- approvals ----

export const listApprovals = (status = 'pending') =>
  request<Approval[]>('GET', withQuery('/approvals', { status }));

export const decideApproval = (id: string, approve: boolean, note = '') =>
  request<Approval>('POST', `/approvals/${encodeURIComponent(id)}/decide`, { body: { approve, note } });

// ---- deployments, signals, friction ----

export const listDeployments = (q: { project?: string; limit?: number } = {}) =>
  request<Deployment[]>('GET', withQuery('/deployments', { ...q }));

export const listSignals = (q: { project?: string; limit?: number } = {}) =>
  request<Signal[]>('GET', withQuery('/signals', { ...q }));

export const listFriction = (q: { project?: string; limit?: number } = {}) =>
  request<Friction[]>('GET', withQuery('/friction', { ...q }));

// ---- loops, agents, config ----

export const listLoops = () => request<LoopView[]>('GET', '/loops');

export const runLoop = (name: string, project = '') =>
  request<void>('POST', withQuery(`/loops/${encodeURIComponent(name)}/run`, { project }));

export const listAgents = () => request<AgentView[]>('GET', '/agents');

export const getConfig = () => request<Config>('GET', '/config');

export const reloadConfig = () => request<void>('POST', '/config/reload');

// ---- chat ----

export const listConversations = () => request<Conversation[]>('GET', '/conversations');

export const createConversation = (title: string, project = '') =>
  request<Conversation>('POST', '/conversations', { body: { title, project } });

export const getConversation = (id: string) =>
  request<ConversationDetail>('GET', `/conversations/${encodeURIComponent(id)}`);

export const sendMessage = (id: string, text: string) =>
  request<ChatMessage>('POST', `/conversations/${encodeURIComponent(id)}/messages`, { body: { text } });

// ---- config editing (owner only) ----
// These calls need the problems list from a 422 and the status of a 409, which the shared
// request() helper does not keep, so they have their own fetch. Its session handling is the same.

/** A refused save. `problems` is set for 422 (the edited file is invalid). */
export class ConfigApiError extends ApiError {
  readonly problems: ConfigProblem[];

  constructor(status: number, message: string, problems: ConfigProblem[]) {
    super(status, message);
    this.name = 'ConfigApiError';
    this.problems = problems;
  }
}

async function configRequest<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    let message = res.statusText || `HTTP ${res.status}`;
    let problems: ConfigProblem[] = [];
    try {
      const data = (await res.json()) as { error?: string; problems?: ConfigProblem[] };
      if (data.error) message = data.error;
      if (Array.isArray(data.problems)) problems = data.problems;
    } catch {
      // Not JSON: keep the status text.
    }
    throw new ConfigApiError(res.status, message, problems);
  }
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const getConfigDocument = () => configRequest<ConfigDocument>('GET', '/config/document');

export const patchConfigDocument = (version: string, changes: ConfigChange[]) =>
  configRequest<ConfigSaved>('PATCH', '/config/document', { version, changes });

export const getConfigFile = () => configRequest<ConfigText>('GET', '/config/file');

export const putConfigFile = (version: string, text: string) =>
  configRequest<ConfigSaved>('PUT', '/config/file', { version, text });
