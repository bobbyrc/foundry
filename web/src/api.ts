// One typed function per route in docs/api.md. Every call except health sends the bearer token
// from auth.ts. A 401 dispatches UNAUTHORIZED_EVENT and throws ApiError.
import { getToken, UNAUTHORIZED_EVENT } from './auth';
import type {
  AgentView,
  Approval,
  Config,
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
  Run,
  Signal,
  Stats,
  Task,
  TaskDetail,
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
  const token = auth ? getToken() : null;
  if (token) headers.Authorization = `Bearer ${token}`;

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
