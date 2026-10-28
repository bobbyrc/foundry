import './Chat.css';
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../api';
import { useCan } from '../auth';
import { subscribe } from '../events';
import { cx, errorMessage } from '../lib/format';
import { parseTime, formatRelative } from '../lib/time';
import { useNow } from '../lib/clock';
import type { ChatDeltaData, ChatMessage } from '../types';
import { useConversation, useConversations, useCreateConversation, useSendMessage } from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { Markdown } from '../components/Markdown';
import { PageHeader } from '../components/PageHeader';

const EXAMPLES = ["What's running right now?", 'Fix the flaky test in …', 'Triage open issues in …'];

export function Chat() {
  const { id } = useParams();
  const navigate = useNavigate();
  const conversations = useConversations();
  const create = useCreateConversation();
  const canOperate = useCan('operate');
  const hasConversations = (conversations.data?.length ?? 0) > 0;
  // The list column appears when a conversation is open or there is one to list.
  const showList = id !== undefined || hasConversations;

  const startNew = async () => {
    const conversation = await create.mutateAsync('New chat');
    navigate(`/chat/${conversation.id}`);
  };

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

  return (
    <div className="chat-page">
      <PageHeader
        title="Chat"
        actions={
          canOperate ? (
            <Button variant="primary" disabled={create.isPending} onClick={() => void startNew()}>
              New chat
            </Button>
          ) : undefined
        }
      />
      <ErrorLine error={create.error} />
      <ErrorLine error={conversations.error} />
      <div className={cx('chat-layout', !showList && 'no-list')}>
        {showList ? (
          <aside className={id ? 'conv-list compact' : 'conv-list'} aria-label="Conversations">
            <h2 className="conv-heading">Conversations</h2>
            <ul>
              {conversations.data?.map((conversation) => (
                <li key={conversation.id}>
                  <Link to={`/chat/${conversation.id}`} className={`conv-item${conversation.id === id ? ' active' : ''}`}>
                    <span className="conv-title">{conversation.title || 'Untitled'}</span>
                    <span className="conv-meta muted small">
                      {conversation.busy ? 'working · ' : ''}
                      <ConvTime value={conversation.updated_at} />
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </aside>
        ) : null}
        <section className="thread">{id ? <Thread key={id} conversationId={id} /> : noneOpen}</section>
      </div>
    </div>
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
        placeholder="Message the orchestrator"
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

/** The provisional assistant text while a reply streams in. */
interface Delta {
  text: string;
  runId?: string;
}

function Thread({ conversationId }: { conversationId: string }) {
  const conversation = useConversation(conversationId);
  const send = useSendMessage(conversationId);
  const navigate = useNavigate();
  const location = useLocation();
  const [delta, setDelta] = useState<Delta | null>(null);
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const canOperate = useCan('operate');
  const autoSent = useRef(false);

  useEffect(
    () =>
      subscribe((ev) => {
        if (ev.type === 'chat.delta') {
          const data = ev.data as ChatDeltaData | undefined;
          if (!data || data.conversation_id !== conversationId) return;
          setDelta((prev) => ({
            text: (prev?.text ?? '') + (data.text ?? ''),
            runId: data.run_id ?? prev?.runId,
          }));
        } else if (ev.type === 'chat.message') {
          const message = ev.data as ChatMessage | undefined;
          if (message?.conversation_id === conversationId) setDelta(null);
        }
      }),
    [conversationId],
  );

  // A reply that finished without a chat.message event must not leave the provisional text behind.
  const busy = conversation.data?.conversation.busy ?? false;
  useEffect(() => {
    if (conversation.isSuccess && !busy) setDelta(null);
  }, [busy, conversation.isSuccess]);

  const messages = conversation.data?.messages ?? [];
  const working = busy || delta !== null;
  const latestAssistantRun = [...messages].reverse().find((m) => m.role === 'assistant')?.run_id;
  const runId = delta?.runId ?? latestAssistantRun;

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' });
  }, [messages.length, delta?.text]);

  // Grow the composer with its content.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [draft]);

  const sendText = async (text: string) => {
    setSendError(null);
    try {
      await send.mutateAsync(text);
      setDraft('');
      setDelta({ text: '' });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setSendError('The orchestrator is still replying.');
      } else {
        setSendError(errorMessage(err));
      }
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
  }, [location.state]);

  const submit = async () => {
    const text = draft.trim();
    if (!text || working) return;
    await sendText(text);
  };

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
  if (!conversation.data) return <p className="muted">Loading conversation…</p>;

  const empty = messages.length === 0 && delta === null;

  return (
    <>
      <h2 className="thread-title">{conversation.data.conversation.title || 'Untitled'}</h2>
      <div className="messages">
        {empty && canOperate ? (
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
        {messages.map((message) => (
          <MessageView key={message.id} message={message} />
        ))}
        {delta !== null ? (
          <div className="msg assistant provisional">
            <span className="msg-label">orchestrator</span>
            <Markdown text={delta.text} />
            <span className="caret" aria-hidden="true" />
          </div>
        ) : null}
        {working ? (
          <p className="working small">
            <span className="lamp lamp-run pulse" aria-hidden="true" />
            <span className="muted">
              Orchestrator is working
              {runId ? (
                <>
                  {' · '}
                  <Link to={`/runs/${runId}`}>view run</Link>
                </>
              ) : null}
            </span>
          </p>
        ) : null}
        <div ref={endRef} />
      </div>
      {canOperate ? (
        <Composer
          textareaRef={textareaRef}
          draft={draft}
          onDraft={setDraft}
          onSend={() => void submit()}
          disabled={working}
          sendDisabled={working || send.isPending || draft.trim() === ''}
          placeholder={working ? 'The orchestrator is replying…' : 'Message the orchestrator'}
        />
      ) : (
        <p className="muted small">Viewers can read conversations but not send messages.</p>
      )}
      {sendError ? <p className="error-line" role="alert">{sendError}</p> : null}
    </>
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
  autoFocus = false,
}: ComposerProps) {
  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        onSend();
      }}
    >
      <label className="sr-only" htmlFor="composer">
        Message the orchestrator
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
          }
        }}
      />
      <div className="composer-foot">
        <span className="composer-hint muted small">Enter to send · Shift+Enter for a new line</span>
        <Button type="submit" variant="primary" disabled={sendDisabled}>
          Send
        </Button>
      </div>
    </form>
  );
}

function MessageView({ message }: { message: ChatMessage }) {
  if (message.role === 'user') {
    return (
      <div className="msg user">
        <div className="bubble">{message.text}</div>
      </div>
    );
  }
  if (message.role === 'assistant') {
    return (
      <div className="msg assistant">
        <span className="msg-label">orchestrator</span>
        <Markdown text={message.text} />
        {message.run_id ? (
          <p className="small muted">
            <Link to={`/runs/${message.run_id}`}>view run</Link>
          </p>
        ) : null}
      </div>
    );
  }
  return <div className="msg system muted small">{message.text}</div>;
}
