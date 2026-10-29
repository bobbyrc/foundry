import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import type { TaskKind } from '../types';
import { useAgents, useCreateTask, useProjects } from '../queries';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';
import { Modal } from './Modal';

const KINDS: TaskKind[] = ['feature', 'bugfix', 'issue', 'incident', 'review', 'chore', 'improve', 'question'];

export function NewTaskModal({ open, onClose, defaultProject }: { open: boolean; onClose: () => void; defaultProject: string }) {
  const projects = useProjects();
  const agents = useAgents();
  const create = useCreateTask();
  const navigate = useNavigate();
  const [project, setProject] = useState('');
  const [kind, setKind] = useState<TaskKind>('feature');
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [role, setRole] = useState('');
  const [priority, setPriority] = useState(0);
  const [formError, setFormError] = useState<string | null>(null);

  const projectNames = (projects.data ?? []).map((p) => p.name);
  const selectedProject = project || defaultProject || projectNames[0] || '';

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!selectedProject) return setFormError('Choose a project first.');
    if (!title.trim()) return setFormError('A title is required.');
    setFormError(null);
    create.mutate(
      { project: selectedProject, kind, title: title.trim(), body, role, priority, parent_id: '', source: 'api' },
      {
        onSuccess: (task) => {
          setTitle('');
          setBody('');
          onClose();
          navigate(`/tasks/${task.id}`);
        },
      },
    );
  };

  return (
    <Modal open={open} title="New task" onClose={onClose}>
      <form className="form-stack" onSubmit={submit}>
        <Field label="Project">
          <select value={selectedProject} onChange={(event) => setProject(event.target.value)}>
            {projectNames.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Kind">
          <select value={kind} onChange={(event) => setKind(event.target.value as TaskKind)}>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Title">
          <input type="text" required value={title} onChange={(event) => setTitle(event.target.value)} />
        </Field>
        <Field label="Body" hint="Markdown is supported.">
          <textarea rows={6} value={body} onChange={(event) => setBody(event.target.value)} />
        </Field>
        <Field label="Role" hint="Leave on routing default to let onlet pick the agent.">
          <select value={role} onChange={(event) => setRole(event.target.value)}>
            <option value="">Routing default</option>
            {(agents.data ?? []).filter((agent) => !agent.disabled).map((agent) => (
              <option key={agent.name} value={agent.name}>
                {agent.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Priority" hint="Higher runs first.">
          <input type="number" value={priority} onChange={(event) => setPriority(Number(event.target.value) || 0)} />
        </Field>
        {formError ? <p className="error-line" role="alert">{formError}</p> : null}
        <ErrorLine error={create.error} />
        <div className="row-actions">
          <Button type="submit" variant="primary" disabled={create.isPending}>
            Create task
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </form>
    </Modal>
  );
}
