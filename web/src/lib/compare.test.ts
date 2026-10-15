import { describe, expect, it } from 'vitest';
import { changeLine, compareView } from './compare';
import type { Project, Snapshot, Task } from './types';

const project = (id: string, over: Partial<Project> = {}): Project => ({
  id,
  name: id,
  description: '',
  color: '#4f6bed',
  milestones: [],
  order: 0,
  created: '',
  version: '',
  ...over,
});

const task = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  projectId: 'p1',
  title: id,
  description: '',
  status: 'todo',
  start: '2026-01-05',
  end: '2026-01-06',
  weekendWork: false,
  estimateHours: 0,
  assignees: [],
  dependsOn: [],
  lane: 0,
  created: '',
  version: '',
  ...over,
});

const snap = (projects: Project[], tasks: Task[]): Snapshot => ({
  projects,
  tasks,
  people: [],
  planning: { hoursPerDay: 6, assigneeFactor: 1, version: '' },
});

describe('compareView', () => {
  const ms = { id: 'm1', name: 'Launch', date: '2026-01-10' };
  const head = snap(
    [project('p1', { milestones: [ms] }), project('p2')],
    [task('same'), task('moved'), task('edited'), task('gone'), task('rehomed')],
  );
  const live = snap(
    [
      project('p1', {
        name: 'Renamed',
        milestones: [
          { ...ms, date: '2026-01-12' },
          { id: 'm2', name: 'New', date: '2026-01-20' },
        ],
      }),
      project('p3'),
    ],
    [
      task('same'),
      task('moved', { start: '2026-01-07', end: '2026-01-08', status: 'doing' }),
      task('edited', { status: 'done', assignees: ['alice'] }),
      task('rehomed', { projectId: 'p3' }),
      task('fresh', { projectId: 'p3' }),
    ],
  );
  const v = compareView(head, live);

  it('shows committed tasks plus local additions', () => {
    expect(v.tasks.map((t) => t.id)).toEqual(['same', 'moved', 'edited', 'gone', 'rehomed', 'fresh']);
    expect(v.tasks.find((t) => t.id === 'moved')?.start).toBe('2026-01-05');
  });

  it('classifies task changes', () => {
    expect(v.tasksChanged.has('same')).toBe(false);
    expect(v.tasksChanged.get('moved')).toMatchObject({ kind: 'moved', fields: ['dates', 'status'] });
    expect(v.tasksChanged.get('edited')).toMatchObject({ kind: 'edited', fields: ['status', 'assignees'] });
    expect(v.tasksChanged.get('gone')).toMatchObject({ kind: 'removed' });
    expect(v.tasksChanged.get('rehomed')).toMatchObject({ kind: 'moved', fields: ['project'] });
    expect(v.tasksChanged.get('fresh')).toMatchObject({ kind: 'added' });
  });

  it('keeps removed projects and appends added ones', () => {
    expect(v.projects.map((p) => p.id)).toEqual(['p1', 'p2', 'p3']);
    expect(v.projects[0].name).toBe('p1');
    expect(v.projectsChanged.get('p1')).toMatchObject({ kind: 'edited', fields: ['name'] });
    expect(v.projectsChanged.get('p2')).toMatchObject({ kind: 'removed' });
    expect(v.projectsChanged.get('p3')).toMatchObject({ kind: 'added' });
  });

  it('marks milestones and keeps local ones', () => {
    expect(v.projects[0].milestones.map((m) => [m.id, m.date])).toEqual([
      ['m1', '2026-01-10'],
      ['m2', '2026-01-20'],
    ]);
    expect(v.milestonesChanged.get('m1')).toMatchObject({ kind: 'moved', fields: ['date'] });
    expect(v.milestonesChanged.get('m2')).toMatchObject({ kind: 'added' });
  });

  it('reports nothing for identical data', () => {
    const same = compareView(head, head);
    expect(same.tasksChanged.size + same.projectsChanged.size + same.milestonesChanged.size).toBe(0);
  });
});

describe('changeLine', () => {
  it('names the changed fields', () => {
    expect(changeLine({ kind: 'edited', fields: ['title', 'status'] })).toBe('Since last commit: title, status changed');
    expect(changeLine({ kind: 'removed', fields: [] })).toBe('Since last commit: deleted');
    expect(changeLine(undefined)).toBe('');
  });
});
