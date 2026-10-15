// Compare mode: the data as of the last commit, with marks for what changed
// locally since. Items only in the local data are kept (as "added") so they
// can be drawn as ghosts next to the committed state.
import type { Milestone, Person, Planning, Project, Snapshot, Task } from './types';

/** What happened to an item locally since the last commit. */
export type ChangeKind = 'added' | 'removed' | 'moved' | 'edited';

export interface Change<T> {
  kind: ChangeKind;
  /** Labels of the fields that differ (moved/edited only). */
  fields: string[];
  /** The local version (absent when removed). */
  live?: T;
}

export interface CompareView {
  projects: Project[];
  tasks: Task[];
  people: Person[];
  planning: Planning;
  tasksChanged: Map<string, Change<Task>>;
  projectsChanged: Map<string, Change<Project>>;
  milestonesChanged: Map<string, Change<Milestone>>;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
const sameSet = (a: string[], b: string[]) => same([...a].sort(), [...b].sort());

const TASK_FIELDS: [label: string, differs: (a: Task, b: Task) => boolean][] = [
  ['project', (a, b) => a.projectId !== b.projectId],
  ['dates', (a, b) => a.start !== b.start || a.end !== b.end],
  ['title', (a, b) => a.title !== b.title],
  ['status', (a, b) => a.status !== b.status],
  ['assignees', (a, b) => !sameSet(a.assignees, b.assignees)],
  ['dependencies', (a, b) => !sameSet(a.dependsOn, b.dependsOn)],
  ['estimate', (a, b) => a.estimateHours !== b.estimateHours],
  ['weekends', (a, b) => a.weekendWork !== b.weekendWork],
  ['description', (a, b) => a.description !== b.description],
  ['row', (a, b) => a.lane !== b.lane],
];

const PROJECT_FIELDS: [label: string, differs: (a: Project, b: Project) => boolean][] = [
  ['name', (a, b) => a.name !== b.name],
  ['colour', (a, b) => a.color !== b.color],
  ['description', (a, b) => a.description !== b.description],
  ['order', (a, b) => a.order !== b.order],
];

function diff<T>(head: T, live: T, fields: [string, (a: T, b: T) => boolean][], moved: string[]): Change<T> | null {
  const changed = fields.filter(([, d]) => d(head, live)).map(([label]) => label);
  if (!changed.length) return null;
  return { kind: changed.some((f) => moved.includes(f)) ? 'moved' : 'edited', fields: changed, live };
}

export function compareView(head: Snapshot, live: Snapshot): CompareView {
  const tasksChanged = new Map<string, Change<Task>>();
  const projectsChanged = new Map<string, Change<Project>>();
  const milestonesChanged = new Map<string, Change<Milestone>>();

  const liveTasks = new Map(live.tasks.map((t) => [t.id, t]));
  const headTaskIds = new Set(head.tasks.map((t) => t.id));
  for (const t of head.tasks) {
    const l = liveTasks.get(t.id);
    const c = l ? diff(t, l, TASK_FIELDS, ['project', 'dates']) : { kind: 'removed' as const, fields: [] };
    if (c) tasksChanged.set(t.id, c);
  }
  const addedTasks = live.tasks.filter((t) => !headTaskIds.has(t.id));
  for (const t of addedTasks) tasksChanged.set(t.id, { kind: 'added', fields: [], live: t });

  const liveProjects = new Map(live.projects.map((p) => [p.id, p]));
  const headProjectIds = new Set(head.projects.map((p) => p.id));
  const projects = head.projects.map((p) => {
    const l = liveProjects.get(p.id);
    if (!l) {
      projectsChanged.set(p.id, { kind: 'removed', fields: [] });
      for (const m of p.milestones) milestonesChanged.set(m.id, { kind: 'removed', fields: [] });
      return p;
    }
    const c = diff(p, l, PROJECT_FIELDS, []);
    if (c) projectsChanged.set(p.id, c);
    // Milestones added locally are kept so they can show as ghosts.
    const liveMs = new Map(l.milestones.map((m) => [m.id, m]));
    const headMsIds = new Set(p.milestones.map((m) => m.id));
    for (const m of p.milestones) {
      const lm = liveMs.get(m.id);
      if (!lm) milestonesChanged.set(m.id, { kind: 'removed', fields: [] });
      else if (lm.date !== m.date || lm.name !== m.name) {
        const fields = [...(lm.date !== m.date ? ['date'] : []), ...(lm.name !== m.name ? ['name'] : [])];
        milestonesChanged.set(m.id, { kind: lm.date !== m.date ? 'moved' : 'edited', fields, live: lm });
      }
    }
    const addedMs = l.milestones.filter((m) => !headMsIds.has(m.id));
    for (const m of addedMs) milestonesChanged.set(m.id, { kind: 'added', fields: [], live: m });
    return addedMs.length ? { ...p, milestones: [...p.milestones, ...addedMs] } : p;
  });
  for (const p of live.projects) {
    if (headProjectIds.has(p.id)) continue;
    projects.push(p);
    projectsChanged.set(p.id, { kind: 'added', fields: [], live: p });
    for (const m of p.milestones) milestonesChanged.set(m.id, { kind: 'added', fields: [], live: m });
  }

  // People added locally keep their names and colours on added tasks.
  const headPeople = new Set(head.people.map((p) => p.id));
  const people = [...head.people, ...live.people.filter((p) => !headPeople.has(p.id))];

  return {
    projects,
    tasks: [...head.tasks, ...addedTasks],
    people,
    planning: head.planning,
    tasksChanged,
    projectsChanged,
    milestonesChanged,
  };
}

/** Tooltip line for a change, e.g. "Since last commit: dates, status changed". */
export function changeLine(c: Change<unknown> | undefined): string {
  if (!c) return '';
  if (c.kind === 'added') return 'Since last commit: added';
  if (c.kind === 'removed') return 'Since last commit: deleted';
  return `Since last commit: ${c.fields.join(', ')} changed`;
}
