// The visual config editor: a list of the top-level sections on the left, the active section's
// fields on the right. Edits go into a change list; the draft shown is the file with those
// changes applied. Save sends the list in one request; nothing is written before that.
import { useEffect, useMemo, useState } from 'react';
import { ConfigApiError } from '../../api';
import { useSaveConfigDocument } from '../../queries';
import type { ConfigChange, ConfigDocument, ConfigProblem } from '../../types';
import { addChange, applyChanges, clearChange, humanizeKey, problemsByPath } from '../../lib/configDoc';
import { cx } from '../../lib/format';
import { useParamState } from '../../useParamState';
import { Empty } from '../Empty';
import { ConfigNotes, SaveBar } from './SaveBar';
import { NodeEditor, sectionHasProblems, type EditContext } from './ConfigSection';

const SAVED_MS = 4_000;

export function ConfigEditor({
  doc,
  refetch,
  onDirtyChange,
}: {
  doc: ConfigDocument;
  /** Refetches the document, so the editor starts again from the file on disk after a save. */
  refetch: () => Promise<{ error: unknown }>;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const save = useSaveConfigDocument();
  const [changes, setChanges] = useState<ConfigChange[]>([]);
  const [problems, setProblems] = useState<ConfigProblem[]>([]);
  const [conflict, setConflict] = useState(false);
  const [restart, setRestart] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [sectionKey, setSectionKey] = useParamState('section');

  const dirty = changes.length > 0;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);
  // Leaving the editor (or switching to YAML) takes its unsaved state with it.
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  // Opening the editor again (after the YAML tab) reads the file now, not a copy from earlier.
  useEffect(() => {
    void refetch();
  }, [refetch]);

  useEffect(() => {
    if (!saved) return;
    const timer = window.setTimeout(() => setSaved(false), SAVED_MS);
    return () => window.clearTimeout(timer);
  }, [saved]);

  const draft = useMemo(() => applyChanges(doc.document, changes), [doc.document, changes]);
  const problemMap = useMemo(() => problemsByPath(problems), [problems]);
  const sections = doc.schema.fields ?? [];
  const active = sections.find((section) => section.key === sectionKey) ?? sections[0];

  const ctx: EditContext = {
    draft,
    problems: problemMap,
    setValue: (path, value) => setChanges((prev) => addChange(prev, { op: 'set', path, value })),
    clearValue: (path) => setChanges((prev) => clearChange(doc.document, prev, path)),
  };

  const discard = () => {
    setChanges([]);
    setProblems([]);
    setConflict(false);
    setError(null);
  };

  const onSave = async () => {
    setError(null);
    setConflict(false);
    setProblems([]);
    setSaved(false);
    try {
      const result = await save.mutateAsync({ version: doc.version, changes });
      await refetch();
      setChanges([]);
      setRestart((prev) => [...new Set([...prev, ...(result.restart_required ?? [])])]);
      setSaved(true);
    } catch (err) {
      if (err instanceof ConfigApiError && err.status === 409) setConflict(true);
      else if (err instanceof ConfigApiError && err.status === 422) setProblems(err.problems);
      else setError(err);
    }
  };

  const loadLatest = async () => {
    const res = await refetch();
    setChanges([]);
    setProblems([]);
    setConflict(false);
    setError(res.error ?? null);
  };

  const count = changes.length;

  return (
    <div className="config-editor">
      <div className="config-layout">
        <nav className="config-nav" aria-label="Config sections">
          {sections.map((section) => {
            const on = section.key === active?.key;
            const unsaved = changes.some((change) => change.path[0] === section.key);
            const broken = sectionHasProblems(problemMap.keys(), section.key);
            const notes = [unsaved && 'unsaved changes', broken && 'has problems'].filter(Boolean).join(', ');
            return (
              <button
                key={section.key}
                type="button"
                className={cx('config-nav-item', on && 'active')}
                aria-current={on ? 'true' : undefined}
                onClick={() => setSectionKey(section.key)}
              >
                <span>{humanizeKey(section.key)}</span>
                {unsaved ? <span className="config-lamp lamp-call" aria-hidden="true" /> : null}
                {broken ? <span className="config-lamp lamp-stop" aria-hidden="true" /> : null}
                {notes ? <span className="sr-only"> ({notes})</span> : null}
              </button>
            );
          })}
        </nav>

        <div className="config-body">
          <ConfigNotes
            state={{ saved, restart, problems, conflict, error }}
            onDismissRestart={() => setRestart([])}
            onLoadLatest={() => void loadLatest()}
          />
          {active ? (
            <NodeEditor node={active} path={[active.key]} ctx={ctx} nested={false} />
          ) : (
            <Empty>The schema has no sections.</Empty>
          )}
        </div>
      </div>

      {dirty ? (
        <SaveBar
          label={`${count} unsaved change${count === 1 ? '' : 's'}`}
          saving={save.isPending}
          onSave={() => void onSave()}
          onDiscard={discard}
        />
      ) : null}
    </div>
  );
}
