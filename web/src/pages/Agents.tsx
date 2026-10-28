import { useAgents } from '../queries';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';

export function Agents() {
  const agents = useAgents();
  return (
    <>
      <PageHeader title="Agents" />
      <ErrorLine error={agents.error} />
      {agents.isSuccess && agents.data.length === 0 ? (
        <Empty>No agents are configured. Roles are set under agents in onlet.yaml.</Empty>
      ) : null}
      <div className="card-grid">
        {agents.data?.map((agent) => (
          <article key={agent.name} className="card agent-card">
            <h3 className="mono">{agent.name}</h3>
            <p className="muted">{agent.description || 'No description.'}</p>
            <dl className="kv">
              <dt>Harness</dt>
              <dd>
                {agent.harness} <span className="muted">({agent.harness_type})</span>
              </dd>
              <dt>Model</dt>
              <dd>{agent.model || 'harness default'}</dd>
              <dt>Prompt</dt>
              <dd className="mono">{agent.prompt}</dd>
              <dt>Task kinds</dt>
              <dd>
                {(agent.kinds ?? []).length > 0
                  ? (agent.kinds ?? []).map((kind) => (
                      <span key={kind} className="tag">
                        {kind}
                      </span>
                    ))
                  : 'none routed'}
              </dd>
            </dl>
          </article>
        ))}
      </div>
    </>
  );
}
