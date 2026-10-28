// React Query hooks for every route. Lists refetch every 30 seconds as a fallback to events.
import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import * as api from './api';
import { ApiError } from './api';
import type { ConfigChange, NewTask, Role, Stats, TaskDetail } from './types';
import { keys } from './keys';
import { normaliseProject } from './lib/projects';

export const LIST_REFRESH_MS = 30_000;

/** Retries transient failures twice, but never a 4xx such as an expired session. */
export function retryPolicy(failureCount: number, error: Error): boolean {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false;
  return failureCount < 2;
}

function invalidate(qc: QueryClient, ...queryKeys: readonly (readonly unknown[])[]): void {
  for (const queryKey of queryKeys) void qc.invalidateQueries({ queryKey });
}

export interface TaskFilters {
  project?: string;
  status?: string;
  kind?: string;
  limit?: number;
}

export interface RunFilters {
  project?: string;
  status?: string;
  task_id?: string;
  limit?: number;
}

// ---- queries ----

export function useStats() {
  return useQuery({
    queryKey: keys.stats,
    queryFn: () => api.getStats(),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useControl() {
  return useQuery({ queryKey: keys.control, queryFn: () => api.getControl() });
}

export function useProjects() {
  return useQuery({
    queryKey: keys.projectsAll,
    queryFn: async () => (await api.listProjects()).map(normaliseProject),
    refetchInterval: LIST_REFRESH_MS,
  });
}

/** Names of the projects paused in onlet.yaml. Their queued tasks do not start. */
export function usePausedProjects(): string[] {
  const projects = useProjects();
  return useMemo(() => (projects.data ?? []).filter((p) => p.paused).map((p) => p.name), [projects.data]);
}

export function useTasks(filters: TaskFilters) {
  return useQuery({
    queryKey: keys.taskList(filters),
    queryFn: () => api.listTasks(filters),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useTask(id: string) {
  return useQuery<TaskDetail>({
    queryKey: keys.task(id),
    queryFn: () => api.getTask(id),
    enabled: id !== '',
  });
}

export function useRuns(filters: RunFilters) {
  return useQuery({
    queryKey: keys.runList(filters),
    queryFn: () => api.listRuns(filters),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useRun(id: string) {
  return useQuery({
    queryKey: keys.run(id),
    queryFn: () => api.getRun(id),
    enabled: id !== '',
  });
}

export function useApprovals(status: 'pending' | 'all') {
  return useQuery({
    queryKey: keys.approvalList(status),
    queryFn: () => api.listApprovals(status),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useDeployments(project: string) {
  return useQuery({
    queryKey: keys.deploymentList({ project }),
    queryFn: () => api.listDeployments({ project, limit: 200 }),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useSignals(project: string) {
  return useQuery({
    queryKey: keys.signalList({ project }),
    queryFn: () => api.listSignals({ project, limit: 200 }),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useFriction(project: string) {
  return useQuery({
    queryKey: keys.frictionList({ project }),
    queryFn: () => api.listFriction({ project, limit: 200 }),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useLoops() {
  return useQuery({
    queryKey: keys.loopsAll,
    queryFn: () => api.listLoops(),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useAgents() {
  return useQuery({
    queryKey: keys.agentsAll,
    queryFn: () => api.listAgents(),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useConfig() {
  return useQuery({ queryKey: keys.configAll, queryFn: () => api.getConfig() });
}

export function useConversations() {
  return useQuery({
    queryKey: keys.conversationsAll,
    queryFn: () => api.listConversations(),
    refetchInterval: LIST_REFRESH_MS,
  });
}

export function useConversation(id: string) {
  return useQuery({
    queryKey: keys.conversation(id),
    queryFn: () => api.getConversation(id),
    refetchInterval: LIST_REFRESH_MS,
  });
}

// ---- mutations: each invalidates the keys its change can touch ----

export function useCreateTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (task: NewTask) => api.createTask(task),
    onSuccess: () => invalidate(qc, keys.tasksAll, keys.stats),
  });
}

export function useCancelTask(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.cancelTask(id),
    onSuccess: () => invalidate(qc, keys.tasksAll, keys.task(id), keys.runsAll, keys.stats),
  });
}

export function useRetryTask(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (note: string) => api.retryTask(id, note),
    onSuccess: () => invalidate(qc, keys.tasksAll, keys.task(id), keys.stats),
  });
}

export function useCancelRun(runId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.cancelRun(runId),
    onSuccess: () => invalidate(qc, keys.runsAll, keys.run(runId), keys.tasksAll, keys.stats),
  });
}

export function useDecideApproval(approvalId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { approve: boolean; note: string }) =>
      api.decideApproval(approvalId, vars.approve, vars.note),
    onSuccess: (approval) => {
      invalidate(qc, keys.approvalsAll, keys.stats, keys.tasksAll);
      invalidate(qc, keys.task(approval.task_id));
    },
  });
}

export function useRunLoop() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { name: string; project: string }) => api.runLoop(vars.name, vars.project),
    onSuccess: () => invalidate(qc, keys.loopsAll),
  });
}

export function useReloadConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.reloadConfig(),
    onSuccess: () => invalidate(qc, keys.configAll, keys.loopsAll, keys.agentsAll, keys.projectsAll),
  });
}

export function useTogglePause() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (pause: boolean) => (pause ? api.pauseFactory() : api.resumeFactory()),
    onSuccess: (state) => {
      qc.setQueryData(keys.control, state);
      qc.setQueryData<Stats | undefined>(keys.stats, (prev) => (prev ? { ...prev, paused: state.paused } : prev));
      invalidate(qc, keys.stats, keys.projectsAll);
    },
  });
}

/** Pauses or resumes one project. Its card shows the pending state and error from the variables. */
export function useSetProjectPaused() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { name: string; pause: boolean }) =>
      vars.pause ? api.pauseProject(vars.name) : api.resumeProject(vars.name),
    onSuccess: () => invalidate(qc, keys.projectsAll, keys.stats),
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (title: string) => api.createConversation(title),
    onSuccess: () => invalidate(qc, keys.conversationsAll),
  });
}

export function useSendMessage(conversationId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (text: string) => api.sendMessage(conversationId, text),
    onSuccess: () => invalidate(qc, keys.conversation(conversationId), keys.conversationsAll),
  });
}

// ---- users (owner only) ----

export function useUsers() {
  return useQuery({ queryKey: keys.usersAll, queryFn: () => api.listUsers() });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { username: string; password: string; role: Role }) =>
      api.createUser(vars.username, vars.password, vars.role),
    onSuccess: () => invalidate(qc, keys.usersAll),
  });
}

export function useSetUserRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { name: string; role: Role }) => api.setUserRole(vars.name, vars.role),
    onSuccess: () => invalidate(qc, keys.usersAll),
  });
}

export function useSetUserPassword() {
  return useMutation({
    mutationFn: (vars: { name: string; password: string }) => api.setUserPassword(vars.name, vars.password),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.deleteUser(name),
    onSuccess: () => invalidate(qc, keys.usersAll),
  });
}

// ---- config editing (owner only). The document and the raw file are read fresh on demand, and
// saves never refetch them by themselves: the editor refetches after it has handled the result. ----

/** The file as JSON with its schema, for the visual editor. No refetch on focus: edits are kept. */
export function useConfigDocument() {
  return useQuery({
    queryKey: keys.configDocument,
    queryFn: () => api.getConfigDocument(),
    refetchOnWindowFocus: false,
  });
}

/** The raw text of the file, for the YAML tab. */
export function useConfigFile() {
  return useQuery({
    queryKey: keys.configFile,
    queryFn: () => api.getConfigFile(),
    refetchOnWindowFocus: false,
  });
}

export function useSaveConfigDocument() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { version: string; changes: ConfigChange[] }) =>
      api.patchConfigDocument(vars.version, vars.changes),
    onSuccess: () => invalidate(qc, keys.configAll, keys.loopsAll, keys.agentsAll, keys.projectsAll),
  });
}

export function useSaveConfigFile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { version: string; text: string }) => api.putConfigFile(vars.version, vars.text),
    onSuccess: () => invalidate(qc, keys.configAll, keys.loopsAll, keys.agentsAll, keys.projectsAll),
  });
}
