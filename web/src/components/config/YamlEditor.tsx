// The YAML tab: the raw text of onett.yaml in a plain textarea. It is read from the server each
// time the tab opens, so it never shows a copy older than the file on disk at that moment.
import { useEffect, useState, type KeyboardEvent } from 'react';
import { useConfigFile, useSaveConfigFile } from '../../queries';
import type { ConfigProblem, ConfigText } from '../../types';
import { Empty } from '../Empty';
import { ErrorLine } from '../ErrorLine';
import { ConfigNotes, SaveBar } from './SaveBar';
import { classifySaveFailure } from './saveFailure';

const SAVED_MS = 4_000;

export function YamlEditor({ onDirtyChange }: { onDirtyChange: (dirty: boolean) => void }) {
  const file = useConfigFile();
  const save = useSaveConfigFile();
  // `base` is the file as the server last gave it; `text` is what the owner has typed so far.
  const [base, setBase] = useState<ConfigText | null>(null);
  const [text, setText] = useState('');
  const [problems, setProblems] = useState<ConfigProblem[]>([]);
  const [conflict, setConflict] = useState(false);
  const [restart, setRestart] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [reloadFailed, setReloadFailed] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);

  const dirty = base !== null && text !== base.text;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  // Take the first fetch made after this tab opened, not a copy cached from earlier.
  useEffect(() => {
    if (base === null && file.isFetchedAfterMount && file.data) {
      setBase(file.data);
      setText(file.data.text);
    }
  }, [base, file.data, file.isFetchedAfterMount]);

  useEffect(() => {
    if (!saved) return;
    const timer = window.setTimeout(() => setSaved(false), SAVED_MS);
    return () => window.clearTimeout(timer);
  }, [saved]);

  const onSave = async () => {
    if (!base) return;
    setError(null);
    setConflict(false);
    setProblems([]);
    setSaved(false);
    setReloadFailed(null);
    try {
      const result = await save.mutateAsync({ version: base.version, text });
      const res = await file.refetch();
      const latest = res.data ?? { ...base, version: result.version, text };
      setBase(latest);
      setText(latest.text);
      setRestart((prev) => [...new Set([...prev, ...(result.restart_required ?? [])])]);
      setSaved(true);
    } catch (err) {
      const failure = classifySaveFailure(err);
      switch (failure.kind) {
        case 'conflict':
          setConflict(true);
          break;
        case 'problems':
          setProblems(failure.problems);
          break;
        case 'written-not-reloaded': {
          // The file may hold the new text now, so show it as it is rather than the typed copy.
          const res = await file.refetch();
          if (res.data) {
            setBase(res.data);
            setText(res.data.text);
          } else {
            setError(res.error);
          }
          setReloadFailed(failure.message);
          break;
        }
        case 'error':
          setError(failure.error);
          break;
      }
    }
  };

  const discard = () => {
    if (!base) return;
    setText(base.text);
    setProblems([]);
    setConflict(false);
    setError(null);
  };

  const loadLatest = async () => {
    const res = await file.refetch();
    if (res.data) {
      setBase(res.data);
      setText(res.data.text);
    }
    setProblems([]);
    setConflict(false);
    setError(res.error ?? null);
  };

  if (!base) {
    if (file.error) return <ErrorLine error={file.error} />;
    return <Empty>Loading config…</Empty>;
  }

  return (
    <div className="config-yaml">
      <ConfigNotes
        state={{ saved, restart, problems, conflict, reloadFailed, error }}
        onDismissRestart={() => setRestart([])}
        onLoadLatest={() => void loadLatest()}
      />
      <textarea
        className="config-yaml-text"
        aria-label="onett.yaml"
        value={text}
        spellCheck={false}
        wrap="off"
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event: KeyboardEvent<HTMLTextAreaElement>) => {
          // Tab inserts two spaces, as YAML wants; Shift+Tab still moves focus.
          if (event.key !== 'Tab' || event.shiftKey) return;
          event.preventDefault();
          const el = event.currentTarget;
          const { selectionStart: start, selectionEnd: end, value } = el;
          setText(`${value.slice(0, start)}  ${value.slice(end)}`);
          requestAnimationFrame(() => {
            el.selectionStart = start + 2;
            el.selectionEnd = start + 2;
          });
        }}
      />
      {dirty ? (
        <SaveBar
          label="Unsaved edits"
          saving={save.isPending}
          onSave={() => void onSave()}
          onDiscard={discard}
        />
      ) : null}
    </div>
  );
}
