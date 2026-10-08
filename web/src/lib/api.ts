import type { GitCommit, GitStatus, HistoryState, Person, Project, Snapshot, Task } from './types';

export class ApiError extends Error {
  output?: string;
  constructor(message: string, output?: string) {
    super(message);
    this.output = output;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(data.error ?? `${res.status} ${res.statusText}`, data.output);
  }
  return data as T;
}

interface Changed {
  changedTasks: Task[];
}

interface HistoryStep {
  label: string;
  history: HistoryState;
}

export const api = {
  state: () => req<Snapshot>('GET', '/api/state'),

  createProject: (p: Partial<Project>) => req<Project>('POST', '/api/projects', p),
  updateProject: (p: Project) => req<Project>('PUT', `/api/projects/${p.id}`, p),
  deleteProject: (id: string) => req<Changed>('DELETE', `/api/projects/${id}`),
  reorderProjects: (ids: string[]) => req<Project[]>('PUT', '/api/projects/order', { ids }),

  createTask: (t: Partial<Task>) => req<Task>('POST', '/api/tasks', t),
  updateTasks: (ts: Task[]) => req<Task[]>('PUT', '/api/tasks', ts),
  deleteTask: (id: string) => req<Changed>('DELETE', `/api/tasks/${id}`),

  createPerson: (name: string, color: string) => req<Person>('POST', '/api/people', { name, color }),
  updatePerson: (p: Person) => req<Person>('PUT', `/api/people/${p.id}`, p),
  deletePerson: (id: string) => req<Changed>('DELETE', `/api/people/${id}`),

  gitStatus: () => req<GitStatus>('GET', '/api/git/status'),
  gitLog: () => req<GitCommit[]>('GET', '/api/git/log'),
  gitCommit: (message: string) => req<{ output: string }>('POST', '/api/git/commit', { message }),
  gitPull: () => req<{ output: string }>('POST', '/api/git/pull'),
  gitPush: () => req<{ output: string }>('POST', '/api/git/push'),
  gitSync: () => req<{ output: string }>('POST', '/api/git/sync'),
  gitAbortRebase: () => req<{ output: string }>('POST', '/api/git/abort-rebase'),
  gitSetRemote: (url: string) => req<GitStatus>('PUT', '/api/git/remote', { url }),

  history: () => req<HistoryState>('GET', '/api/history'),
  undo: () => req<HistoryStep>('POST', '/api/history/undo'),
  redo: () => req<HistoryStep>('POST', '/api/history/redo'),
};
