export type Status = 'todo' | 'doing' | 'blocked' | 'done';

export const STATUSES: { id: Status; label: string }[] = [
  { id: 'todo', label: 'To do' },
  { id: 'doing', label: 'In progress' },
  { id: 'blocked', label: 'Blocked' },
  { id: 'done', label: 'Done' },
];

export interface Milestone {
  id: string;
  name: string;
  date: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  color: string;
  milestones: Milestone[];
  /** Display position, ascending. Changed only via the reorder endpoint. */
  order: number;
  created: string;
  /** Server content hash; sent back on save to detect conflicting edits. */
  version: string;
}

export interface Task {
  id: string;
  projectId: string;
  title: string;
  description: string;
  status: Status;
  start: string;
  end: string;
  /** When false, weekends are non-working days for this task. */
  weekendWork: boolean;
  estimateHours: number;
  assignees: string[];
  dependsOn: string[];
  lane: number;
  created: string;
  version: string;
}

export interface Person {
  id: string;
  name: string;
  /** '#rrggbb', or '' to use the default derived from the id. */
  color: string;
  /** Focused hours per working day; 0 uses the team default (Planning). */
  hoursPerDay: number;
  version: string;
}

/** Team-wide capacity settings for task pressure, stored in planning.yaml. */
export interface Planning {
  /** Focused hours one person gives a task per working day. */
  hoursPerDay: number;
  /** How much each assignee after the first adds (0..1); 1 = even split. */
  assigneeFactor: number;
  version: string;
}

export type Role = 'none' | 'viewer' | 'editor' | 'admin';

export interface User {
  username: string;
  name: string;
  email: string;
  role: Role;
  lastSeen: string;
  /** Admin listed in the server config; role cannot change in the app. */
  fixed: boolean;
  hasPassword: boolean;
}

export interface Me {
  auth: { mode: 'none' | 'oidc' | 'local'; label?: string };
  autoCommit: boolean;
  user: User | null;
}

export interface UserSettings {
  defaultRole: Role;
}

export interface Snapshot {
  projects: Project[];
  tasks: Task[];
  people: Person[];
  planning: Planning;
}

export interface GitFile {
  path: string;
  code: string;
}

export interface GitStatus {
  branch: string;
  upstream: string;
  ahead: number;
  behind: number;
  remote: string;
  files: GitFile[];
  rebasing: boolean;
  noCommits: boolean;
  /** Last automatic commit/sync failure, if any. */
  autoError: string;
}

/** Labels of the next undo and redo steps; empty when there is none. */
export interface HistoryState {
  undo: string;
  redo: string;
}

export interface GitCommit {
  hash: string;
  author: string;
  when: string;
  subject: string;
}

export const PROJECT_COLORS = ['#4f6bed', '#0f9d8a', '#d9822b', '#c2457a', '#7a5af8', '#3a8fd9', '#5e9c2f', '#b8562c'];
