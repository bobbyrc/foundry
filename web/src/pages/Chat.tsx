import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '../api';
import { subscribe } from '../events';
import { errorMessage } from '../lib/format';
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

  const startNew = async () => {
    const conversation = await create.mutateAsync('New chat');
    navigate(`/chat/${conversation.id}`);
  };

  return (
    <>
      <PageHeader
        title="Chat"
        actions={
          <Button variant="primary" disabled={create.isPending} onClick={() => void startNew()}>
            New chat
          </Button>
        }
      />
      <ErrorLine error={create.error} />
      <div className="chat-layout">
        <aside className="conv-list" aria-label="Conversations">
          <ErrorLine error={conversations.error} />
          {conversations.isSuccess && conversations.data.length === 0 ? <Empty>No conversations yet.</Empty> : null}
          <ul>
            {conversations.data?.map((conversation) => (
              <li key={conversation.id}>
                <Link to={`/chat/${conversation.id}`} className={`conv-item${conversation.id === id ? ' active' : ''}`}>
                  <span className="conv-title">{conversation.title || 'Untitled'}</span>
                  <span className="muted small">
                    {conversation.busy ? 'working · ' : ''}
                    <ConvTime value={conversation.updated_at} />
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </aside>
        <section className="thread">
          {id ? (
            <Thread key={id} conversationId={id} />
          ) : (
            <Empty>Pick a conversation, or start a new one to talk to the orchestrator.</Empty>
          )}
        </section>
      </div>
    </>
  );
}

function ConvTime({ value }: { value: string }) {
  const now = useNow();
  const date = parseTime(value);
  return <>{date ? formatRelative(date, now) : ''}</>;
}

/** The provisional assistant text while a reply streams in. */
interface Delta {
  text: string;
  runId?: string;
}

function Thread({ conversationId }: { conversationId: string }) {
  const conversation = useConversation(conversationId);
  const send = useSendMessage(conversationId);
  const [delta, setDelta] = useState<Delta | null>(null);
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(
    () =>
      subscribe((ev) => {
        if (ev.type === 'chat.delta') {
          const data = ev.data as ChatDeltaData | undefined;
          if (data?.conversation_id !== conversationId) return;
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

  const submit = async () => {
    const text = draft.trim();
    if (!text || working) return;
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

  if (conversation.isError) return <ErrorLine error={conversation.error} />;
  if (!conversation.data) return <p className="muted">Loading conversation…</p>;

  const empty = messages.length === 0 && delta === null;

  return (
    <>
      <h2 className="thread-title">{conversation.data.conversation.title || 'Untitled'}</h2>
      <div className="messages">
        {empty ? (
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
            <Markdown text={delta.text} />
            <span className="caret" aria-hidden="true" />
          </div>
        ) : null}
        {working ? (
          <p className="working muted small">
            Orchestrator is working
            {runId ? (
              <>
                {' · '}
                <Link to={`/runs/${runId}`}>view run</Link>
              </>
            ) : null}
          </p>
        ) : null}
        <div ref={endRef} />
      </div>
      <form
        className="composer"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <label className="sr-only" htmlFor="composer">
          Message the orchestrator
        </label>
        <textarea
          id="composer"
          ref={textareaRef}
          rows={1}
          value={draft}
          disabled={working}
          placeholder={working ? 'The orchestrator is replying…' : 'Message the orchestrator'}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              void submit();
            }
          }}
        />
        <Button type="submit" variant="primary" disabled={working || send.isPending || draft.trim() === ''}>
          Send
        </Button>
      </form>
      {sendError ? <p className="error-line" role="alert">{sendError}</p> : null}
    </>
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
