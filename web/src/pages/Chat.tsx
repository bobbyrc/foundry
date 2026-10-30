import './Chat.css';
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../api';
import { useCan } from '../auth';
import { cx, errorMessage } from '../lib/format';
import { agentLamp, composerState, taskLamp, type ChatLampState } from '../lib/chatState';
import { formatDuration, formatRelative, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import { orchestratorEntries, taskEntries } from '../lib/transcript';
import { readTranscriptView, writeTranscriptView, type TranscriptView } from '../lib/transcriptView';
import { useRunLogsMap } from '../runLogs';
import type { AgentConversation, ChatMessage, LogLine, Task } from '../types';
import {
  useAgentConversations,
  useCancelMessage,
  useCloseTaskConversation,
  useConversation,
  useConversations,
  useCreateConversation,
  useEditMessage,
  useFlushQueued,
  useReopenTaskConversation,
  useRun,
  useSendMessage,
  useSendTaskMessage,
  useStopRun,
  useTaskConversation,
  useTasks,
} from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { Modal } from '../components/Modal';
import { PageHeader } from '../components/PageHeader';
import { Transcript, ViewToggle } from '../components/Transcript';

const EXAMPLES = ["What's running right now?", 'Fix the flaky test in …', 'Triage open issues in …'];

const capitalise = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

/** The Conversation / Terminal choice, remembered per browser. */
function useTranscriptView(): [TranscriptView, (v: TranscriptView) => void] {
  const [view, setView] = useState<TranscriptView>(readTranscriptView);
  return [
    view,
    (next) => {
      setView(next);
      writeTranscriptView(next);
    },
  ];
}

export function Chat() {
  const { id, taskId } = useParams();
  const navigate = useNavigate();
  const conversations = useConversations();
  const agents = useAgentConversations();
  const create = useCreateConversation();
  const canOperate = useCan('operate');
  const [view, setView] = useTranscriptView();
  const hasConversations = (conversations.data?.length ?? 0) > 0 || (agents.data?.length ?? 0) > 0;
  const open = id !== undefined || taskId !== undefined;
  // The list column appears when a conversation is open or there is one to list.
  const showList = open || hasConversations;

  // A new chat started from the composer: create it, then open it with the message to send.
  const startFromComposer = async (text: string) => {
    const conversation = await create.mutateAsync('New chat');
    navigate(`/chat/${conversation.id}`, { state: { message: text } });
  };

  let noneOpen = null;
  if (canOperate) {
    noneOpen = <StartChat starting={create.isPending} onStart={startFromComposer} />;
  } else if (hasConversations) {
    noneOpen = <Empty>Pick a conversation to read it.</Empty>;
  } else if (conversations.isSuccess) {
    noneOpen = <Empty>No conversations yet.</Empty>;
  }

  let thread: ReactNode = noneOpen;
  if (taskId) thread = <TaskThread key={taskId} taskId={taskId} view={view} onView={setView} />;
  else if (id) thread = <OrchestratorThread key={id} conversationId={id} view={view} onView={setView} />;

  return (
    <div className="chat-page">
      <PageHeader title="Chat" />
      <ErrorLine error={create.error} />
      <ErrorLine error={conversations.error} />
      <div className={cx('chat-layout', !showList && 'no-list')}>
        {showList ? (
          <Sidebar
            compact={open}
            canOperate={canOperate}
            activeId={id}
            activeTaskId={taskId}
            orchestrator={(conversations.data ?? []).filter((c) => !c.task_id)}
            agents={agents.data ?? []}
          />
        ) : null}
        <section className="thread">{thread}</section>
      </div>
    </div>
  );
}

function Lamp({ state }: { state: ChatLampState }) {
  return <span className={cx('lamp', 'chat-lamp', `chat-lamp-${state.lamp}`, state.lamp === 'run' && 'pulse')} aria-hidden="true" />;
}

function Sidebar({
  compact,
  canOperate,
  activeId,
  activeTaskId,
  orchestrator,
  agents,
}: {
  compact: boolean;
  canOperate: boolean;
  activeId?: string;
  activeTaskId?: string;
  orchestrator: { id: string; title: string; busy: boolean; updated_at: string }[];
  agents: AgentConversation[];
}) {
  return (
    <aside className={cx('conv-list', compact && 'compact')} aria-label="Conversations">
      {canOperate ? (
        <Link to="/chat" className="btn btn-primary conv-new">
          + New conversation
        </Link>
      ) : null}
      <h2 className="conv-heading">Orchestrator</h2>
      <ul className="conv-group">
        {orchestrator.map((conversation) => (
          <li key={conversation.id}>
            <Link to={`/chat/${conversation.id}`} className={cx('conv-item', conversation.id === activeId && 'active')}>
              <span className="conv-title">{conversation.title || 'Untitled'}</span>
              <span className="conv-meta muted small">
                {conversation.busy ? 'working · ' : ''}
                <ConvTime value={conversation.updated_at} />
              </span>
            </Link>
          </li>
        ))}
      </ul>
      {agents.length > 0 ? (
        <>
          <h2 className="conv-heading conv-heading-quiet">Agents</h2>
          <div className="conv-group">
            {agents.map((agent) => {
              const lamp = agentLamp(agent);
              return (
                <div key={agent.task_id} className="conv-agent-group">
                  <p className="conv-task" title={agent.title}>
                    {agent.title}
                  </p>
                  <Link
                    to={`/chat/task/${agent.task_id}`}
                    className={cx('conv-agent', agent.task_id === activeTaskId && 'active')}
                  >
                    <Lamp state={lamp} />
                    <span className="conv-agent-role">{agent.role}</span>
                    <span className="conv-agent-title">{agent.title}</span>
                    <span className="conv-agent-state muted small">{lamp.label}</span>
                  </Link>
                </div>
              );
            })}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function ConvTime({ value }: { value: string }) {
  const now = useNow();
  const date = parseTime(value);
  return <>{date ? formatRelative(date, now) : ''}</>;
}

/** The start of a new chat: the heading, the composer and the example prompts. */
function StartChat({ starting, onStart }: { starting: boolean; onStart: (text: string) => Promise<void> }) {
  const [draft, setDraft] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const submit = async () => {
    const text = draft.trim();
    if (!text || starting) return;
    try {
      await onStart(text);
    } catch {
      // The error is shown in the line above the page; the draft stays for another try.
    }
  };

  return (
    <div className="chat-start">
      <h2 className="chat-start-title">Talk to the orchestrator</h2>
      <p className="muted">
        It can create tasks, check on work in flight and answer questions about your projects. It acts through the
        same API as you, so it can never change settings or users.
      </p>
      <Composer
        textareaRef={textareaRef}
        draft={draft}
        onDraft={setDraft}
        onSend={() => void submit()}
        disabled={starting}
        sendDisabled={starting || draft.trim() === ''}
        placeholder="Message the orchestrator…"
        label="Message the orchestrator"
        autoFocus
      />
      <div className="examples-row">
        {EXAMPLES.map((example) => (
          <button
            key={example}
            type="button"
            className="chip"
            onClick={() => {
              setDraft(example);
              textareaRef.current?.focus();
            }}
          >
            {example}
          </button>
        ))}
      </div>
    </div>
  );
}

// ---- pieces shared by both kinds of thread ----

function ThreadHead({
  who,
  sub,
  extra,
  view,
  onView,
}: {
  who: string;
  sub: ReactNode;
  extra?: ReactNode;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  return (
    <header className="thread-head">
      <div className="thread-who">
        <h2 className="thread-title">{who}</h2>
        <div className="thread-sub muted small">{sub}</div>
      </div>
      <div className="thread-tools">
        {extra}
        <ViewToggle view={view} onChange={onView} />
      </div>
    </header>
  );
}

/** A second-resolution clock for the working line. */
function useSecond(active: boolean): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

/** "Working · 3m 41s · output 2s ago", the run's link and Stop. */
function WorkingLine({
  runId,
  label,
  lines,
  canStop,
  onStop,
  stopping,
}: {
  runId?: string;
  label: string;
  lines?: LogLine[];
  canStop: boolean;
  onStop: () => void;
  stopping: boolean;
}) {
  const run = useRun(runId ?? '');
  const now = useSecond(true);
  const started = parseTime(run.data?.started_at);
  const lastLine = lines && lines.length > 0 ? lines[lines.length - 1].time : undefined;
  const lastOutput = parseTime(lastLine) ?? parseTime(run.data?.last_output_at);
  return (
    <p className="working small" role="status">
      <span className="lamp lamp-run pulse" aria-hidden="true" />
      <span className="muted">
        {started ? `Working · ${formatDuration(now - started.getTime())}` : `${label} is working`}
        {lastOutput ? ` · output ${formatDuration(now - lastOutput.getTime())} ago` : ''}
        {runId ? (
          <>
            {' · '}
            <Link to={`/runs/${runId}`}>view run</Link>
          </>
        ) : null}
      </span>
      {canStop && runId ? (
        <Button variant="secondary" disabled={stopping} onClick={onStop}>
          Stop
        </Button>
      ) : null}
    </p>
  );
}

/** Edit and Cancel for one queued message, shown in its note. */
function QueuedActions({ message }: { message: ChatMessage | undefined }) {
  const edit = useEditMessage();
  const cancel = useCancelMessage();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  if (!message) return null;
  const save = async () => {
    const next = text.trim();
    if (!next) return;
    try {
      await edit.mutateAsync({ id: message.id, text: next });
      setEditing(false);
    } catch {
      // The error shows in the dialog; the text stays for another try.
    }
  };
  return (
    <span className="tr-qa">
      {' · '}
      <button
        type="button"
        onClick={() => {
          setText(message.text);
          edit.reset();
          setEditing(true);
        }}
      >
        edit
      </button>
      {' · '}
      <button type="button" disabled={cancel.isPending} onClick={() => cancel.mutate(message.id)}>
        cancel
      </button>
      {cancel.error ? <span className="error-line"> {errorMessage(cancel.error)}</span> : null}
      <Modal open={editing} title="Edit queued message" onClose={() => setEditing(false)}>
        <form
          className="chat-modal-form"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <label className="sr-only" htmlFor="edit-queued">
            Message
          </label>
          <textarea id="edit-queued" rows={4} value={text} autoFocus onChange={(event) => setText(event.target.value)} />
          <ErrorLine error={edit.error} />
          <div className="chat-modal-actions">
            <Button variant="secondary" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={edit.isPending || text.trim() === ''}>
              Save
            </Button>
          </div>
        </form>
      </Modal>
    </span>
  );
}

function TaskChip({ task }: { task: Task }) {
  const lamp = taskLamp(task.status);
  return (
    <div className="tr-chip">
      <Lamp state={lamp} />
      <span>{task.title}</span>
      <span className="tr-chip-status">· {lamp.label}</span>
      <Link to={`/chat/task/${task.id}`}>open agent</Link>
      <Link to={`/tasks/${task.id}`}>task</Link>
    </div>
  );
}

interface ComposerProps {
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  draft: string;
  onDraft: (value: string) => void;
  onSend: () => void;
  disabled: boolean;
  sendDisabled: boolean;
  placeholder: string;
  label: string;
  /** Esc in the field; set while a run is live so it can be stopped. */
  onEscape?: () => void;
  hint?: string;
  autoFocus?: boolean;
}

/** The message sheet: an auto-growing field, with the hint and Send inside the same sheet. */
function Composer({
  textareaRef,
  draft,
  onDraft,
  onSend,
  disabled,
  sendDisabled,
  placeholder,
  label,
  onEscape,
  hint = 'Enter to send · Shift+Enter for a new line',
  autoFocus = false,
}: ComposerProps) {
  // Grow the field with its content.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [draft, textareaRef]);

  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        onSend();
      }}
    >
      <label className="sr-only" htmlFor="composer">
        {label}
      </label>
      <textarea
        id="composer"
        ref={textareaRef}
        rows={1}
        autoFocus={autoFocus}
        value={draft}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(event) => onDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            onSend();
          } else if (event.key === 'Escape' && onEscape) {
            event.preventDefault();
            onEscape();
          }
        }}
      />
      <div className="composer-foot">
        <span className="composer-hint muted small">{hint}</span>
        <Button type="submit" variant="primary" disabled={sendDisabled}>
          Send
        </Button>
      </div>
    </form>
  );
}

/** What sits under the transcript: the composer, or why there is none. */
function ComposerArea(props: {
  state: ReturnType<typeof composerState>;
  working: boolean;
  draft: string;
  onDraft: (v: string) => void;
  onSend: () => void;
  sending: boolean;
  onEscape: () => void;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  label: string;
}) {
  const { state } = props;
  if (!state.enabled) return state.note ? <p className="muted small">{state.note}</p> : null;
  return (
    <Composer
      textareaRef={props.textareaRef}
      draft={props.draft}
      onDraft={props.onDraft}
      onSend={props.onSend}
      disabled={false}
      sendDisabled={props.sending || props.draft.trim() === ''}
      placeholder={state.placeholder}
      label={props.label}
      onEscape={props.working ? props.onEscape : undefined}
      hint={
        props.working
          ? 'Enter to send · Shift+Enter for a new line · Esc stops the current turn'
          : 'Enter to send · Shift+Enter for a new line'
      }
    />
  );
}

function SendNow({ onClick, pending }: { onClick: () => void; pending: boolean }) {
  return (
    <div className="send-now">
      <span className="muted small">No run is working, so queued messages are waiting for you.</span>
      <Button variant="secondary" disabled={pending} onClick={onClick}>
        Send now
      </Button>
    </div>
  );
}

const uniq = (ids: (string | undefined)[]): string[] => [...new Set(ids.filter((x): x is string => !!x))];

// ---- the orchestrator's thread ----

function OrchestratorThread({
  conversationId,
  view,
  onView,
}: {
  conversationId: string;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  const conversation = useConversation(conversationId);
  const send = useSendMessage(conversationId);
  const flush = useFlushQueued();
  const stop = useStopRun();
  const tasks = useTasks({ limit: 200 });
  const navigate = useNavigate();
  const location = useLocation();
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const canOperate = useCan('operate');
  const autoSent = useRef(false);

  const conv = conversation.data?.conversation;
  const messages = conversation.data?.messages ?? [];
  const liveRunId = conv?.busy ? conv.active_run_id || undefined : undefined;
  const runIds = uniq([...messages.map((m) => m.run_id), liveRunId]);
  const lastRunId = runIds[runIds.length - 1];
  const logs = useRunLogsMap(runIds, liveRunId);
  const lastRun = useRun(lastRunId ?? '');
  const entries = orchestratorEntries(messages, logs);
  const queued = messages.filter((m) => m.queued && m.role === 'user').length;
  const working = !!conv?.busy;
  const byId = Object.fromEntries(messages.map((m) => [m.id, m]));

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' });
  }, [entries.length, working]);

  const sendText = async (text: string) => {
    setSendError(null);
    try {
      await send.mutateAsync(text);
      setDraft('');
    } catch (err) {
      setSendError(errorMessage(err));
    }
  };

  // A message handed over by StartChat is sent once, when this conversation first opens.
  // The draft stays in the composer if the send fails.
  useEffect(() => {
    const message = (location.state as { message?: string } | null)?.message;
    if (!message || autoSent.current) return;
    autoSent.current = true;
    // Drop the handed-over message from history so a reload does not send it again.
    navigate(location.pathname, { replace: true, state: null });
    setDraft(message);
    void sendText(message);
    // Runs once per mount: sendText changes every render and the ref guards the rest.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.state]);

  if (conversation.isError) {
    // The API answers 404 for an id that was deleted or never existed: say so, and offer a fresh start.
    if (conversation.error instanceof ApiError && conversation.error.status === 404) {
      return (
        <Empty>
          <p>This conversation no longer exists.</p>
          <Link to="/chat" className="btn btn-secondary">
            Start a new conversation
          </Link>
        </Empty>
      );
    }
    return <ErrorLine error={conversation.error} />;
  }
  if (!conv) return <p className="muted">Loading conversation…</p>;

  const state = composerState({ canSend: true, viewer: !canOperate, working, queued });
  const run = lastRun.data;
  const sub = [run?.harness, run?.model, conv.title || 'Untitled'].filter(Boolean).join(' · ');
  const createdBy = (runId: string): ReactNode => {
    const made = (tasks.data ?? []).filter((t) => t.created_by_run === runId);
    return made.length > 0 ? made.map((t) => <TaskChip key={t.id} task={t} />) : null;
  };

  return (
    <>
      <ThreadHead who="Orchestrator" sub={sub} view={view} onView={onView} />
      <div className="messages">
        {entries.length === 0 && canOperate ? (
          <div className="examples">
            <p className="muted">Ask anything, or try one of these:</p>
            {EXAMPLES.map((example) => (
              <button
                key={example}
                type="button"
                className="chip"
                onClick={() => {
                  setDraft(example);
                  textareaRef.current?.focus();
                }}
              >
                {example}
              </button>
            ))}
          </div>
        ) : null}
        <Transcript
          entries={entries}
          view={view}
          agentLabel="Orchestrator"
          afterRun={createdBy}
          queuedActions={canOperate ? (mid) => <QueuedActions message={byId[mid]} /> : undefined}
        />
        {working ? (
          <WorkingLine
            runId={liveRunId}
            label="Orchestrator"
            lines={liveRunId ? logs[liveRunId] : undefined}
            canStop={canOperate}
            stopping={stop.isPending}
            onStop={() => liveRunId && stop.mutate(liveRunId)}
          />
        ) : null}
        {canOperate && queued > 0 && !working ? (
          <SendNow pending={flush.isPending} onClick={() => flush.mutate({ conversationId })} />
        ) : null}
        <div ref={endRef} />
      </div>
      <ComposerArea
        state={state}
        working={working}
        draft={draft}
        onDraft={setDraft}
        onSend={() => draft.trim() && void sendText(draft.trim())}
        sending={send.isPending}
        onEscape={() => liveRunId && stop.mutate(liveRunId)}
        textareaRef={textareaRef}
        label="Message the orchestrator"
      />
      <ErrorLine error={stop.error} />
      <ErrorLine error={flush.error} />
      {sendError ? (
        <p className="error-line" role="alert">
          {sendError}
        </p>
      ) : null}
    </>
  );
}

// ---- a task agent's thread ----

function TaskThread({
  taskId,
  view,
  onView,
}: {
  taskId: string;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  const query = useTaskConversation(taskId);
  const send = useSendTaskMessage(taskId);
  const flush = useFlushQueued();
  const stop = useStopRun();
  const reopen = useReopenTaskConversation(taskId);
  const canOperate = useCan('operate');
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const [closing, setClosing] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);

  const conv = query.data;
  const liveRunId = conv?.live_run_id || undefined;
  const logs = useRunLogsMap(
    (conv?.runs ?? []).map((r) => r.id),
    liveRunId,
  );
  const entries = conv ? taskEntries(conv, logs) : [];
  const messages = conv?.messages ?? [];
  const queued = messages.filter((m) => m.queued && m.role === 'user').length;
  const byId = Object.fromEntries(messages.map((m) => [m.id, m]));

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' });
  }, [entries.length, liveRunId]);

  if (query.isError) {
    if (query.error instanceof ApiError && query.error.status === 404) {
      return (
        <Empty>
          <p>This task has no conversation.</p>
          <Link to="/chat" className="btn btn-secondary">
            Back to chat
          </Link>
        </Empty>
      );
    }
    return <ErrorLine error={query.error} />;
  }
  if (!conv) return <p className="muted">Loading conversation…</p>;

  const { task } = conv;
  const role = task.role || 'agent';
  const closedAt = conv.conversation?.closed_at;
  const closed = !!closedAt;
  const lastRun = conv.runs[conv.runs.length - 1];
  const sub = (
    <>
      {[lastRun?.harness, lastRun?.model].filter(Boolean).join(' · ')}
      {lastRun ? ' · ' : ''}task <Link to={`/tasks/${task.id}`}>{task.title}</Link>
      {task.branch ? <> · branch <span className="mono">{task.branch}</span></> : null}
      {' · '}
      {taskLamp(task.status).label}
    </>
  );
  const state = composerState({
    canSend: conv.can_send,
    reason: conv.reason,
    viewer: !canOperate,
    working: !!liveRunId,
    queued,
    role,
  });

  const sendText = async (text: string) => {
    setSendError(null);
    try {
      await send.mutateAsync(text);
      setDraft('');
    } catch (err) {
      // A 409 carries the reason the agent cannot be messaged.
      setSendError(errorMessage(err));
    }
  };

  const closeButton =
    canOperate && !closed ? (
      <Button variant="secondary" onClick={() => setClosing(true)}>
        Close
      </Button>
    ) : null;

  return (
    <>
      <ThreadHead who={capitalise(role)} sub={sub} extra={closeButton} view={view} onView={onView} />
      <div className="messages">
        <Transcript
          entries={entries}
          view={view}
          agentLabel={capitalise(role)}
          queuedActions={canOperate ? (mid) => <QueuedActions message={byId[mid]} /> : undefined}
        />
        {liveRunId ? (
          <WorkingLine
            runId={liveRunId}
            label={capitalise(role)}
            lines={logs[liveRunId]}
            canStop={canOperate}
            stopping={stop.isPending}
            onStop={() => stop.mutate(liveRunId)}
          />
        ) : null}
        {canOperate && queued > 0 && !liveRunId && !closed ? (
          <SendNow pending={flush.isPending} onClick={() => flush.mutate({ taskId })} />
        ) : null}
        <div ref={endRef} />
      </div>
      {closed ? (
        <div className="closed-note">
          <p>
            Closed by {conv.conversation?.closed_by}: {conv.conversation?.closed_reason}
          </p>
          {canOperate ? (
            <Button variant="secondary" disabled={reopen.isPending} onClick={() => reopen.mutate()}>
              Reopen
            </Button>
          ) : null}
        </div>
      ) : (
        <>
          {state.enabled ? (
            <div className="chat-hint">
              You&apos;re talking directly to this task&apos;s agent. For anything beyond this task,{' '}
              <Link to="/chat">ask the orchestrator</Link>.
            </div>
          ) : null}
          <ComposerArea
            state={state}
            working={!!liveRunId}
            draft={draft}
            onDraft={setDraft}
            onSend={() => draft.trim() && void sendText(draft.trim())}
            sending={send.isPending}
            onEscape={() => liveRunId && stop.mutate(liveRunId)}
            textareaRef={textareaRef}
            label={`Message the ${role}`}
          />
        </>
      )}
      <ErrorLine error={reopen.error} />
      <ErrorLine error={stop.error} />
      <ErrorLine error={flush.error} />
      {sendError ? (
        <p className="error-line" role="alert">
          {sendError}
        </p>
      ) : null}
      <CloseModal open={closing} taskId={taskId} onClose={() => setClosing(false)} />
    </>
  );
}

function CloseModal({ open, taskId, onClose }: { open: boolean; taskId: string; onClose: () => void }) {
  const close = useCloseTaskConversation(taskId);
  const [reason, setReason] = useState('');
  const submit = async () => {
    const text = reason.trim();
    if (!text) return;
    try {
      await close.mutateAsync(text);
      setReason('');
      onClose();
    } catch {
      // The error shows in the dialog; the reason stays for another try.
    }
  };
  return (
    <Modal open={open} title="Close this conversation" onClose={onClose}>
      <form
        className="chat-modal-form"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <p className="muted small">
          The agent can no longer be messaged and queued messages are never sent. The task itself is not changed. You can
          reopen it later.
        </p>
        <label htmlFor="close-reason">Reason</label>
        <textarea id="close-reason" rows={3} value={reason} autoFocus onChange={(event) => setReason(event.target.value)} />
        <ErrorLine error={close.error} />
        <div className="chat-modal-actions">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={close.isPending || reason.trim() === ''}>
            Close conversation
          </Button>
        </div>
      </form>
    </Modal>
  );
}
