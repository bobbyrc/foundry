import { riskLabel, riskTone } from '../lib/status';

export function RiskBadge({ risk }: { risk: string | undefined }) {
  return <span className={`badge tone-${riskTone(risk)}`}>{riskLabel(risk)}</span>;
}
