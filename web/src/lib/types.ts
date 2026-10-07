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
  created: string;
}

export interface Task {
  id: string;
  projectId: string;
  title: string;
  description: string;
  status: Status;
  start: string;
  end: string;
  estimateHours: number;
  assignees: string[];
  dependsOn: string[];
  lane: number;
  created: string;
}

export interface Person {
  id: string;
  name: string;
}

export interface Snapshot {
  projects: Project[];
  tasks: Task[];
  people: Person[];
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
}

export interface GitCommit {
  hash: string;
  author: string;
  when: string;
  subject: string;
}

export const PROJECT_COLORS = ['#4f6bed', '#0f9d8a', '#d9822b', '#c2457a', '#7a5af8', '#3a8fd9', '#5e9c2f', '#b8562c'];
