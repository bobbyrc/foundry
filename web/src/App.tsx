import { useEffect } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useConnected, useUnauthorized } from './auth';
import { eventStream, invalidateFor, subscribe } from './events';
import { Shell } from './components/Shell';
import { Agents } from './pages/Agents';
import { Approvals } from './pages/Approvals';
import { Chat } from './pages/Chat';
import { Config } from './pages/Config';
import { Connect } from './pages/Connect';
import { Deploys } from './pages/Deploys';
import { Friction } from './pages/Friction';
import { Loops } from './pages/Loops';
import { Projects } from './pages/Projects';
import { Overview } from './pages/Overview';
import { RunDetail } from './pages/RunDetail';
import { Runs } from './pages/Runs';
import { Signals } from './pages/Signals';
import { TaskDetail } from './pages/TaskDetail';
import { Tasks } from './pages/Tasks';

export function App() {
  const qc = useQueryClient();
  const unauthorized = useUnauthorized();
  // Connected means a token is stored and the last call was not rejected. Without it, the
  // stream stays closed and the Connect page shows.
  const connected = useConnected();

  useEffect(() => {
    if (!connected) return;
    eventStream.start(qc);
    return () => eventStream.stop();
  }, [connected, qc]);

  // Every event refreshes the query keys it touches (see events.ts keysFor).
  useEffect(() => subscribe((ev) => invalidateFor(ev, qc)), [qc]);

  if (!connected) return <Connect rejected={unauthorized} />;

  return (
    <Shell>
      <Routes>
        <Route path="/" element={<Overview />} />
        <Route path="/projects" element={<Projects />} />
        <Route path="/chat" element={<Chat />} />
        <Route path="/chat/:id" element={<Chat />} />
        <Route path="/tasks" element={<Tasks />} />
        <Route path="/tasks/:id" element={<TaskDetail />} />
        <Route path="/runs" element={<Runs />} />
        <Route path="/runs/:id" element={<RunDetail />} />
        <Route path="/approvals" element={<Approvals />} />
        <Route path="/deploys" element={<Deploys />} />
        <Route path="/signals" element={<Signals />} />
        <Route path="/friction" element={<Friction />} />
        <Route path="/loops" element={<Loops />} />
        <Route path="/agents" element={<Agents />} />
        <Route path="/config" element={<Config />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Shell>
  );
}
