import { useProjects } from '../queries';
import { Field } from './Field';

/** A project picker. An empty value means "all projects" when includeAll is set. */
export function ProjectSelect({
  value,
  onChange,
  includeAll = true,
  label = 'Project',
}: {
  value: string;
  onChange: (value: string) => void;
  includeAll?: boolean;
  label?: string;
}) {
  const projects = useProjects();
  const names = (projects.data ?? []).map((p) => p.name);
  return (
    <Field label={label}>
      <select value={value} onChange={(event) => onChange(event.target.value)}>
        {includeAll ? <option value="">All projects</option> : null}
        {names.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
        {value && !names.includes(value) ? <option value={value}>{value}</option> : null}
      </select>
    </Field>
  );
}
