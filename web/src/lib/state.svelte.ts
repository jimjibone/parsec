import { api, ApiError } from './api';
import { place, resolve, type Slot } from './lanes';
import { toDay, toISO, today } from './dates';
import { addWorkDays, snapWorkday } from './schedule';
import { assignColors, nextPersonColor, UNKNOWN_PERSON_COLOR } from './people';
import { PROJECT_COLORS, type GitStatus, type Person, type Project, type Task } from './types';

export interface Toast {
  id: number;
  kind: 'error' | 'info';
  text: string;
}

class AppState {
  projects = $state<Project[]>([]);
  tasks = $state<Task[]>([]);
  people = $state<Person[]>([]);
  loaded = $state(false);
  loadError = $state('');
  git = $state<GitStatus | null>(null);
  toasts = $state<Toast[]>([]);

  taskById = $derived(new Map(this.tasks.map((t) => [t.id, t])));
  projectById = $derived(new Map(this.projects.map((p) => [p.id, p])));
  personById = $derived(new Map(this.people.map((p) => [p.id, p])));
  private personColors = $derived(assignColors(this.people));

  colorOf(personId: string): string {
    return this.personColors.get(personId) ?? UNKNOWN_PERSON_COLOR;
  }

  /** Conflict-free lane per task, computed from stored lanes. */
  displayLane = $derived.by(() => {
    const out = new Map<string, number>();
    for (const p of this.projects) {
      for (const s of resolve(this.slots(p.id))) out.set(s.id, s.lane);
    }
    return out;
  });

  laneCount = $derived.by(() => {
    const out = new Map<string, number>();
    for (const t of this.tasks) {
      const l = (this.displayLane.get(t.id) ?? 0) + 1;
      out.set(t.projectId, Math.max(out.get(t.projectId) ?? 0, l));
    }
    return out;
  });

  private toastSeq = 0;
  private gitTimer: ReturnType<typeof setTimeout> | undefined;

  async load() {
    try {
      const s = await api.state();
      this.projects = s.projects;
      this.tasks = s.tasks;
      this.people = s.people;
      this.loaded = true;
      this.loadError = '';
    } catch (e) {
      this.loadError = String((e as Error).message ?? e);
    }
    this.refreshGit();
  }

  toast(text: string, kind: Toast['kind'] = 'error') {
    const id = ++this.toastSeq;
    this.toasts.push({ id, kind, text });
    setTimeout(() => this.dismiss(id), kind === 'error' ? 8000 : 4000);
  }

  dismiss(id: number) {
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }

  refreshGit() {
    clearTimeout(this.gitTimer);
    this.gitTimer = setTimeout(async () => {
      try {
        this.git = await api.gitStatus();
      } catch {
        this.git = null;
      }
    }, 300);
  }

  /** Run a mutation; on failure show the error and resync from the server. */
  private async run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    try {
      const r = await fn();
      this.refreshGit();
      return r;
    } catch (e) {
      this.toast(e instanceof ApiError ? e.message : String(e));
      await this.load();
      return undefined;
    }
  }

  private mergeTasks(ts: Task[]) {
    if (!ts.length) return;
    const byId = new Map(ts.map((t) => [t.id, t]));
    const existing = new Set(this.tasks.map((t) => t.id));
    this.tasks = [...this.tasks.map((t) => byId.get(t.id) ?? t), ...ts.filter((t) => !existing.has(t.id))];
  }

  private slots(projectId: string): Slot[] {
    return this.tasks.filter((t) => t.projectId === projectId).map((t) => ({ id: t.id, s: toDay(t.start), e: toDay(t.end), lane: t.lane }));
  }

  /**
   * Save a task, re-packing lanes in its project (and the project it left).
   * `targetLane` requests an explicit row move; otherwise the task stays in
   * its row if it fits.
   */
  async saveTask(next: Task, targetLane?: number) {
    const prev = this.taskById.get(next.id);
    const changed = new Map<string, Task>();
    const curLane = (id: string) => this.displayLane.get(id) ?? 0;

    const layoutProject = (pid: string, moved?: Slot, target?: number) => {
      // Start from the conflict-free display layout, not raw stored lanes.
      const base = this.tasks
        .filter((t) => t.projectId === pid && t.id !== next.id)
        .map((t) => ({ id: t.id, s: toDay(t.start), e: toDay(t.end), lane: curLane(t.id) }));
      const slots = moved ? place(base, moved, target) : resolve(base);
      for (const s of slots) {
        const t = s.id === next.id ? next : this.taskById.get(s.id)!;
        if (s.id === next.id || t.lane !== s.lane) changed.set(s.id, { ...t, lane: s.lane });
      }
    };

    const movedSlot: Slot = {
      id: next.id,
      s: toDay(next.start),
      e: toDay(next.end),
      lane: prev && prev.projectId === next.projectId ? curLane(next.id) : 0,
    };
    layoutProject(next.projectId, movedSlot, targetLane);
    if (prev && prev.projectId !== next.projectId) layoutProject(prev.projectId);

    const batch = [...changed.values()];
    this.mergeTasks(batch); // optimistic
    const saved = await this.run(() => api.updateTasks(batch));
    if (saved) this.mergeTasks(saved);
  }

  async createTask(projectId: string, init: Partial<Task> = {}): Promise<Task | undefined> {
    const weekendWork = init.weekendWork ?? false;
    // The requested calendar length becomes a length in working days.
    const s0 = toDay(init.start ?? toISO(today()));
    const len = init.end ? toDay(init.end) - s0 + 1 : 3;
    const s = weekendWork ? s0 : snapWorkday(s0, 1);
    const e = weekendWork ? s + len - 1 : addWorkDays(s, len);
    const t: Partial<Task> = {
      title: 'New task',
      status: 'todo',
      assignees: [],
      dependsOn: [],
      lane: 0,
      ...init,
      start: toISO(s),
      end: toISO(e),
      weekendWork,
      projectId,
    };
    const base = this.tasks
      .filter((x) => x.projectId === projectId)
      .map((x) => ({ id: x.id, s: toDay(x.start), e: toDay(x.end), lane: this.displayLane.get(x.id) ?? 0 }));
    const fresh = { id: '', s: toDay(t.start!), e: toDay(t.end!), lane: t.lane ?? 0 };
    const lane = place(base, fresh).find((x) => x.id === '')!.lane;
    const created = await this.run(() => api.createTask({ ...t, lane }));
    if (created) this.mergeTasks([created]);
    return created;
  }

  async deleteTask(id: string) {
    this.tasks = this.tasks.filter((t) => t.id !== id);
    const r = await this.run(() => api.deleteTask(id));
    if (r) this.mergeTasks(r.changedTasks);
  }

  async addDependency(taskId: string, dependsOn: string) {
    const t = this.taskById.get(taskId);
    if (!t || t.id === dependsOn || t.dependsOn.includes(dependsOn)) return;
    await this.saveTask({ ...t, dependsOn: [...t.dependsOn, dependsOn] });
  }

  async removeDependency(taskId: string, dependsOn: string) {
    const t = this.taskById.get(taskId);
    if (!t) return;
    await this.saveTask({ ...t, dependsOn: t.dependsOn.filter((d) => d !== dependsOn) });
  }

  async createProject(name: string): Promise<Project | undefined> {
    const color = PROJECT_COLORS[this.projects.length % PROJECT_COLORS.length];
    const p = await this.run(() => api.createProject({ name, color, milestones: [] }));
    if (p) this.projects = [...this.projects, p];
    return p;
  }

  async saveProject(p: Project) {
    this.projects = this.projects.map((x) => (x.id === p.id ? p : x));
    const saved = await this.run(() => api.updateProject(p));
    if (saved) this.projects = this.projects.map((x) => (x.id === saved.id ? saved : x));
  }

  async deleteProject(id: string) {
    this.projects = this.projects.filter((p) => p.id !== id);
    this.tasks = this.tasks.filter((t) => t.projectId !== id);
    const r = await this.run(() => api.deleteProject(id));
    if (r) this.mergeTasks(r.changedTasks);
  }

  async createPerson(name: string): Promise<Person | undefined> {
    const p = await this.run(() => api.createPerson(name, nextPersonColor(this.people)));
    if (p) this.people = [...this.people, p];
    return p;
  }

  async savePerson(p: Person) {
    this.people = this.people.map((x) => (x.id === p.id ? p : x));
    await this.run(() => api.updatePerson(p));
  }

  async deletePerson(id: string) {
    this.people = this.people.filter((p) => p.id !== id);
    const r = await this.run(() => api.deletePerson(id));
    if (r) this.mergeTasks(r.changedTasks);
  }
}

export const app = new AppState();
