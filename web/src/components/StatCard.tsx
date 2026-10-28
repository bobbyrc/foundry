import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';

export function StatCard({ label, value, href }: { label: string; value: ReactNode; href: string }) {
  return (
    <Link to={href} className="stat-card">
      <span className="stat-label">{label}</span>
      <span className="stat-value">{value ?? '—'}</span>
    </Link>
  );
}
