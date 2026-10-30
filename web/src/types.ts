// Mirrors internal/domain, internal/config and internal/api. Keep in step with the Go structs;
// field names are their JSON tags. Optional fields are the ones Go marks omitempty.

export type TaskKind =
  | 'feature'
  | 'bugfix'
  | 'issue'
  | 'incident'
  | 'review'
  | 'chore'
  | 'improve'
  | 'question';

export type TaskStatus =
  | 'queued'
  | 'running'
  | 'review'
  | 'awaiting_approval'
  | 'merged'
  | 'deploying'
  | 'done'
  | 'blocked'
  | 'failed'
  | 'cancelled';

/** Empty string means unrated; policy treats it as high. */
export type Risk = '' | 'low' | 'medium' | 'high';

export type RunStatus =
  | 'starting'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'stalled'
  | 'timed_out'
  | 'denied'
  | 'cancelled';

export type LogKind = 'text' | 'tool_use' | 'tool_result' | 'denial' | 'system' | 'stderr' | 'error';

export type ApprovalStatus = 'pending' | 'approved' | 'rejected';

export type DeployState = 'pending' | 'deploying' | 'verifying' | 'healthy' | 'failed' | 'rolled_back';

export interface Task {
  id: string;
  project: string;
  parent_id?: string;
  kind: TaskKind;
  title: string;
  body: string;
  role: string;
  status: TaskStatus;
  priority: number;
  risk?: Risk;
  risk_reasons?: string[];
  source: string;
  source_ref?: string;
  branch?: string;
  pr_number?: number;
  pr_url?: string;
  merge_sha?: string;
  approved_sha?: string;
  pushed_at?: string;
  summary?: string;
  blocked_reason?: string;
  attempts: number;
  max_attempts: number;
  created_at: string;
  updated_at: string;
}

export interface Run {
  id: string;
  task_id: string;
  project: string;
  role: string;
  harness: string;
  model?: string;
  status: RunStatus;
  session_id?: string;
  workdir?: string;
  branch?: string;
  started_at: string;
  ended_at?: string;
  last_output_at: string;
  exit_code: number;
  cost_usd: number;
  cost_estimated?: boolean;
  input_tokens: number;
  output_tokens: number;
  summary?: string;
  error?: string;
}

export interface LogLine {
  run_id: string;
  seq: number;
  time: string;
  kind: LogKind;
  text: string;
}

/**
 * A live event from the server (WebSocket for the UI, SSE for the CLI). Live-only events
 * (run.log, chat.delta) may carry id 0.
 */
export interface OnettEvent<T = unknown> {
  id: number;
  time: string;
  type: string;
  project?: string;
  task_id?: string;
  run_id?: string;
  message: string;
  data?: T;
}

/** A frame the server sends on /api/v1/ws. Each is one JSON text message. */
export type WsServerMessage =
  | { kind: 'event'; event: OnettEvent }
  | { kind: 'subscribed' | 'unsubscribed'; run_id?: string; conversation_id?: string }
  | { kind: 'error'; message: string };

/** A frame the client sends on /api/v1/ws. It names exactly one ID. */
export interface WsClientMessage {
  kind: 'subscribe' | 'unsubscribe';
  run_id?: string;
  conversation_id?: string;
}

export interface ChatDeltaData {
  conversation_id: string;
  text: string;
  run_id?: string;
}

export interface Approval {
  id: string;
  task_id: string;
  project: string;
  kind: string;
  title: string;
  reason: string;
  status: ApprovalStatus;
  head_sha?: string;
  note?: string;
  created_at: string;
  decided_at?: string;
}

export interface Deployment {
  id: string;
  project: string;
  task_id?: string;
  ref: string;
  provider_id: string;
  url?: string;
  state: DeployState;
  notes?: string;
  log?: string;
  verifying_since?: string;
  started_at: string;
  updated_at: string;
}

export interface Signal {
  id: string;
  project: string;
  source: string;
  fingerprint: string;
  title: string;
  level: string;
  count: number;
  url?: string;
  culprit?: string;
  release?: string;
  first_seen: string;
  last_seen: string;
  task_id?: string;
  created_at: string;
  updated_at: string;
}

export interface Friction {
  id: string;
  project: string;
  run_id?: string;
  title: string;
  body: string;
  fingerprint: string;
  count: number;
  issue_url?: string;
  issue_state?: 'open' | 'closed';
  created_at: string;
  updated_at: string;
}

export interface Conversation {
  id: string;
  title: string;
  project?: string;
  session_id?: string;
  busy: boolean;
  created_at: string;
  updated_at: string;
}

export interface ChatMessage {
  id: string;
  conversation_id: string;
  role: 'user' | 'assistant' | 'system';
  text: string;
  run_id?: string;
  created_at: string;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: ChatMessage[];
}

export interface LoopView {
  name: string;
  project: string;
  enabled: boolean;
  every: string;
  last_run_at: string;
  last_error?: string;
}

/** Where an agent's settings come from: the built-in definition, a built-in with changes, or a custom agent. */
export type AgentOrigin = 'builtin' | 'edited' | 'custom';

export interface AgentView {
  name: string;
  origin: AgentOrigin;
  disabled: boolean;
  harness: string;
  harness_type: string;
  model: string;
  prompt: string;
  prompt_text: string;
  prompt_error?: string;
  prompt_updates: boolean;
  builtin_prompt_text?: string;
  description: string;
  args: string[];
  kinds: string[];
  uses: AgentUse[];
}

export interface AgentUse {
  kind?: string;
  project?: string;
  description: string;
}

export interface Stats {
  tasks_by_status: Partial<Record<TaskStatus, number>>;
  active_runs: number;
  approvals_pending: number;
  cost_today_usd: number;
  paused: boolean;
}

export interface TaskDetail {
  task: Task;
  runs: Run[];
  approvals: Approval[];
  children: Task[];
}

export interface NewTask {
  project: string;
  kind: TaskKind;
  title: string;
  body: string;
  role: string;
  priority: number;
  parent_id: string;
  source: string;
}

export interface ProjectCounts {
  queued: number;
  running: number;
  review: number;
  awaiting_approval: number;
  blocked: number;
}

export interface Project {
  name: string;
  repo: string;
  path: string;
  default_branch: string;
  policy: string;
  instructions?: string;
  env?: Record<string, string>;
  agents?: Record<string, Agent>;
  routing?: Record<string, string>;
  deploy?: Deploy | null;
  observe?: Observer[];
  loops?: Loops;
  paused: boolean;
}

/** Who holds the project paused: onett.yaml ("config"), the UI or API ("owner"), or nobody. */
export type PausedBy = '' | 'config' | 'owner';

/** A project as GET /api/v1/projects returns it: config fields, effective pause state and counts. */
export interface ProjectView extends Omit<Project, 'deploy' | 'observe'> {
  deploy: Deploy | null;
  observe: Observer[];
  paused_by: PausedBy;
  counts: ProjectCounts;
}

// ---- Config (internal/config) ----

export interface ServerConfig {
  addr: string;
  data_dir: string;
  public_url: string;
  /** Go duration string, e.g. "720h". */
  retain_events?: string;
  retain_run_logs?: string;
}

export interface LimitsConfig {
  max_concurrent_runs: number;
  max_runs_per_project: number;
  /** Go duration string, e.g. "15m". */
  stall_timeout: string;
  run_timeout: string;
  max_attempts: number;
}

export interface Harness {
  type: string;
  command?: string[];
  args?: string[];
  env?: Record<string, string>;
  inherit_env?: string[];
}

export interface Agent {
  harness: string;
  model?: string;
  prompt: string;
  prompt_text?: string;
  args?: string[];
  description?: string;
  disabled?: boolean;
}

export interface Rule {
  paths?: string[];
  labels?: string[];
  kinds?: string[];
  risk: Risk;
  reason: string;
}

export interface Gate {
  auto_merge: boolean;
  require_review: boolean;
  require_ci: boolean;
}

export interface Policy {
  max_lines: number;
  max_files: number;
  /** Go nil slices and maps marshal as null, so these may be null. */
  rules: Rule[] | null;
  gates: Record<string, Gate> | null;
}

export interface Deploy {
  type: string;
  options?: Record<string, unknown>;
  health_url?: string;
  verify_for: string;
  auto_rollback?: boolean;
}

export interface Observer {
  name: string;
  type: string;
  options?: Record<string, unknown>;
}

export interface Notifier {
  type: string;
  options?: Record<string, unknown>;
  min_priority?: string;
}

export interface LoopSettings {
  enabled?: boolean;
  every: string;
}

export interface Loops {
  issue_intake: LoopSettings & { label: string };
  exec_intake: LoopSettings;
  pr_shepherd: LoopSettings;
  deploy_watch: LoopSettings;
  prod_watch: LoopSettings & { min_level: string; min_count: number };
  friction: LoopSettings & {
    auto_file?: boolean;
    auto_queue: boolean;
    label: string;
    min_count: number;
  };
  retro: LoopSettings & { lookback: string };
}

export interface Config {
  server: ServerConfig;
  limits: LimitsConfig;
  harnesses: Record<string, Harness>;
  agents: Record<string, Agent>;
  routing: Record<string, string>;
  policies: Record<string, Policy>;
  projects: Record<string, Project | null>;
  loops: Loops;
  notify: Notifier[] | null;
  self_project: string;
  path: string;
}

// ---- accounts ----

export type Role = 'viewer' | 'operator' | 'owner';

export interface User {
  username: string;
  role: Role;
  created_at: string;
  updated_at: string;
}

/** The answer to GET /auth/state. `user` is the signed-in account, or null. */
export interface AuthStateResponse {
  setup_required: boolean;
  user: User | null;
}

// ---- Config editing (owner only; GET/PATCH /config/document and GET/PUT /config/file) ----

/** The kinds of node in the config schema. "section" has fields; "map" and "list" have an item. */
export type SchemaType = 'section' | 'map' | 'list' | 'string' | 'int' | 'float' | 'bool' | 'duration' | 'any';

/** One node of the schema the server sends with the document. The root is a section with key "". */
export interface SchemaNode {
  key: string;
  type: SchemaType;
  help?: string;
  /** Sections: their fields, in display order. */
  fields?: SchemaNode[];
  /** Maps: the schema of each value. Lists: the schema of each element. */
  item?: SchemaNode;
}

/** One edit to the file. `path` is the mapping keys; decimal strings index lists. */
export interface ConfigChange {
  op: 'set' | 'delete';
  path: string[];
  value?: unknown;
}

/** A problem the server found in the edited file. `path` is dotted, and "" means the whole file. */
export interface ConfigProblem {
  path: string;
  message: string;
}

/** GET /config/document: the file as JSON, with ${NAME} references left unexpanded. */
export interface ConfigDocument {
  path: string;
  version: string;
  document: unknown;
  schema: SchemaNode;
}

/** GET /config/file: the raw text of the file. */
export interface ConfigText {
  path: string;
  version: string;
  text: string;
}

/** The answer to a successful save. `restart_required` lists the settings that need a restart. */
export interface ConfigSaved {
  version: string;
  restart_required: string[];
}
