import type { GitCommit, GitStatus, HistoryState, Me, Person, Project, Snapshot, Task, User, UserSettings } from './types';

export class ApiError extends Error {
  status: number;
  output?: string;
  constructor(message: string, status: number, output?: string) {
    super(message);
    this.status = status;
    this.output = output;
  }
}

/** Identifies this browser tab, so the server can tell its own quick saves
 * apart from other people's edits, and so live updates skip the sender. */
export const CLIENT_ID = Math.random().toString(36).slice(2, 12);

/** Called when the server says the session has ended. */
let onUnauthorized = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

/** Called when a request cannot reach the server (or the proxy says the
 * server is down). */
let onNetworkError = () => {};
export function setNetworkErrorHandler(fn: () => void) {
  onNetworkError = fn;
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Parsec-Client': CLIENT_ID };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    onNetworkError();
    // status 0: no response at all
    const msg = method === 'GET' ? 'Cannot reach the parsec server.' : 'Cannot reach the parsec server; the change was not saved.';
    throw new ApiError(msg, 0);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    if (res.status === 401) onUnauthorized();
    if (res.status === 502 || res.status === 503 || res.status === 504) onNetworkError();
    throw new ApiError(data.error ?? `${res.status} ${res.statusText}`, res.status, data.output);
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
  me: () => req<Me>('GET', '/api/me'),
  localLogin: (username: string, password: string) => req<void>('POST', '/auth/login', { username, password }),
  logout: () => req<void>('POST', '/auth/logout'),

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

  users: () => req<{ users: User[]; settings: UserSettings }>('GET', '/api/users'),
  createUser: (u: { username: string; name?: string; email?: string; role: string; password?: string }) =>
    req<User>('POST', '/api/users', u),
  updateUser: (username: string, u: { name?: string; email?: string; role?: string; password?: string }) =>
    req<User>('PUT', `/api/users/${encodeURIComponent(username)}`, u),
  deleteUser: (username: string) => req<{ ok: boolean }>('DELETE', `/api/users/${encodeURIComponent(username)}`),
  saveSettings: (s: UserSettings) => req<UserSettings>('PUT', '/api/settings', s),
};
