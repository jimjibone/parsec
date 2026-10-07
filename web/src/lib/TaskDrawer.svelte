<script lang="ts">
  import { app } from './state.svelte';
  import { ui } from './ui.svelte';
  import Icon from './Icon.svelte';
  import { spanDays, toDay, toISO } from './dates';
  import { STATUSES, type Task } from './types';

  let { taskId }: { taskId: string } = $props();

  const task = $derived(app.taskById.get(taskId)!);
  const project = $derived(app.projectById.get(task.projectId));
  const lane = $derived(app.displayLane.get(task.id) ?? 0);
  const successors = $derived(app.tasks.filter((t) => t.dependsOn.includes(task.id)));

  let newPerson = $state<string | null>(null);

  function save(patch: Partial<Task>) {
    app.saveTask({ ...task, ...patch });
  }

  function setStart(v: string) {
    if (!v) return;
    // Keep duration when moving the start.
    const len = toDay(task.end) - toDay(task.start);
    save({ start: v, end: toISO(toDay(v) + len) });
  }

  function setEnd(v: string) {
    if (!v) return;
    if (toDay(v) < toDay(task.start)) save({ start: v, end: v });
    else save({ end: v });
  }

  function addAssignee(id: string) {
    if (id === '__new') {
      newPerson = '';
      return;
    }
    if (id && !task.assignees.includes(id)) save({ assignees: [...task.assignees, id] });
  }

  async function createPerson() {
    const name = newPerson?.trim();
    newPerson = null;
    if (!name) return;
    const p = await app.createPerson(name);
    if (p) save({ assignees: [...task.assignees, p.id] });
  }

  function addDep(id: string) {
    if (id) app.addDependency(task.id, id);
  }

  async function remove() {
    if (!confirm(`Delete task "${task.title}"?`)) return;
    ui.selectedTask = null;
    await app.deleteTask(task.id);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && !(e.target as HTMLElement).closest('input, textarea, select')) ui.selectedTask = null;
  }

  // Tasks that (transitively) depend on this one; picking any would form a cycle.
  const downstream = $derived.by(() => {
    const out = new Set<string>([task.id]);
    let grew = true;
    while (grew) {
      grew = false;
      for (const t of app.tasks) {
        if (!out.has(t.id) && t.dependsOn.some((d) => out.has(d))) {
          out.add(t.id);
          grew = true;
        }
      }
    }
    return out;
  });

  // Candidate predecessors grouped by project.
  const depOptions = $derived(
    app.projects.map((p) => ({
      project: p,
      tasks: app.tasks.filter((t) => t.projectId === p.id && !downstream.has(t.id) && !task.dependsOn.includes(t.id)),
    })),
  );

  function focusSelect(el: HTMLInputElement) {
    el.focus();
    el.select();
  }

  function focusSelectIfNew(el: HTMLInputElement, isNew: boolean) {
    if (isNew) focusSelect(el);
  }
</script>

<svelte:window onkeydown={onKey} />

<aside class="drawer" aria-label="Task details">
  <div class="top">
    <span class="dot" style:background={project?.color}></span>
    <span class="muted proj">{project?.name}</span>
    <div class="spacer"></div>
    <button class="ghost icon-btn" title="Move up a row" disabled={lane === 0} onclick={() => app.saveTask(task, lane - 1)}
      ><Icon name="up" /></button
    >
    <button class="ghost icon-btn" title="Move down a row" onclick={() => app.saveTask(task, lane + 1)}><Icon name="down" /></button>
    <button class="ghost icon-btn danger" title="Delete task" onclick={remove}><Icon name="trash" /></button>
    <button class="ghost icon-btn" title="Close" onclick={() => (ui.selectedTask = null)}><Icon name="x" /></button>
  </div>

  {#key task.id}
    <input
      class="title"
      type="text"
      value={task.title}
      onchange={(e) => save({ title: e.currentTarget.value })}
      use:focusSelectIfNew={task.title === 'New task'}
    />
  {/key}

  <div class="cols">
    <label class="field">
      <span>Status</span>
      <select value={task.status} onchange={(e) => save({ status: e.currentTarget.value as Task['status'] })}>
        {#each STATUSES as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
      </select>
    </label>
    <label class="field">
      <span>Project</span>
      <select value={task.projectId} onchange={(e) => save({ projectId: e.currentTarget.value })}>
        {#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
      </select>
    </label>
    <label class="field">
      <span>Start</span>
      <input type="date" value={task.start} onchange={(e) => setStart(e.currentTarget.value)} />
    </label>
    <label class="field">
      <span>End</span>
      <input type="date" value={task.end} min={task.start} onchange={(e) => setEnd(e.currentTarget.value)} />
    </label>
    <label class="field">
      <span>Estimate (hours)</span>
      <input
        type="number"
        min="0"
        step="0.5"
        value={task.estimateHours || ''}
        placeholder="0"
        onchange={(e) => save({ estimateHours: Math.max(0, Number(e.currentTarget.value) || 0) })}
      />
    </label>
    <div class="field">
      <span>Duration</span>
      <div class="static">{spanDays(task.start, task.end)} day{spanDays(task.start, task.end) === 1 ? '' : 's'}</div>
    </div>
  </div>

  <div class="field">
    <span>Assignees</span>
    <div class="chips">
      {#each task.assignees as a (a)}
        <span class="chip">
          {app.personById.get(a)?.name ?? a}
          <button onclick={() => save({ assignees: task.assignees.filter((x) => x !== a) })} aria-label="Unassign"
            ><Icon name="x" size={12} /></button
          >
        </span>
      {/each}
      {#if newPerson !== null}
        <input
          type="text"
          placeholder="Name"
          bind:value={newPerson}
          use:focusSelect
          onkeydown={(e) => {
            if (e.key === 'Enter') createPerson();
            if (e.key === 'Escape') newPerson = null;
          }}
          onblur={createPerson}
        />
      {:else}
        <select
          value=""
          onchange={(e) => {
            addAssignee(e.currentTarget.value);
            e.currentTarget.value = '';
          }}
        >
          <option value="">Add...</option>
          {#each app.people.filter((p) => !task.assignees.includes(p.id)) as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
          <option value="__new">New person...</option>
        </select>
      {/if}
    </div>
  </div>

  <div class="field">
    <span>Depends on (must finish first)</span>
    <ul class="deps">
      {#each task.dependsOn as d (d)}
        {@const dt = app.taskById.get(d)}
        <li>
          <span class="dot" style:background={app.projectById.get(dt?.projectId ?? '')?.color}></span>
          <button class="ghost linkish" onclick={() => (ui.selectedTask = d)}>{dt?.title ?? d}</button>
          {#if dt && toDay(dt.end) >= toDay(task.start)}<span class="bad" title="Ends on or after this task starts">overlaps</span>{/if}
          <button class="ghost icon-btn" onclick={() => app.removeDependency(task.id, d)} aria-label="Remove dependency"
            ><Icon name="x" size={14} /></button
          >
        </li>
      {/each}
    </ul>
    <select
      value=""
      onchange={(e) => {
        addDep(e.currentTarget.value);
        e.currentTarget.value = '';
      }}
    >
      <option value="">Add dependency...</option>
      {#each depOptions as g (g.project.id)}
        {#if g.tasks.length}
          <optgroup label={g.project.name}>
            {#each g.tasks as t (t.id)}<option value={t.id}>{t.title}</option>{/each}
          </optgroup>
        {/if}
      {/each}
    </select>
  </div>

  {#if successors.length}
    <div class="field">
      <span>Blocks</span>
      <ul class="deps">
        {#each successors as s (s.id)}
          <li>
            <span class="dot" style:background={app.projectById.get(s.projectId)?.color}></span>
            <button class="ghost linkish" onclick={() => (ui.selectedTask = s.id)}>{s.title}</button>
            <button class="ghost icon-btn" onclick={() => app.removeDependency(s.id, task.id)} aria-label="Remove dependency"
              ><Icon name="x" size={14} /></button
            >
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <label class="field grow">
    <span>Description</span>
    {#key task.id}
      <textarea
        value={task.description}
        rows="8"
        placeholder="Notes, acceptance criteria, links..."
        onchange={(e) => save({ description: e.currentTarget.value })}></textarea>
    {/key}
  </label>
</aside>

<style>
  .drawer {
    position: fixed;
    top: 52px;
    right: 0;
    bottom: 0;
    width: min(420px, 100vw);
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 12px 16px 16px;
    overflow: auto;
    background: var(--surface);
    border-left: 1px solid var(--border);
    box-shadow: var(--shadow);
  }

  .top {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .proj {
    font-size: 13px;
    margin-left: 4px;
  }

  .spacer {
    flex: 1;
  }

  .title {
    font-size: 18px;
    font-weight: 600;
    border-color: transparent !important;
    background: transparent !important;
    padding: 4px 6px;
    margin: 0 -6px;
  }

  .title:hover {
    background: var(--surface-2) !important;
  }

  .cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: var(--text-2);
  }

  .field :global(select),
  .field :global(input),
  .field :global(textarea),
  .static,
  .deps {
    color: var(--text);
    font-size: 14px;
  }

  .static {
    padding: 5px 0;
  }

  .grow {
    flex: 1;
  }

  .grow textarea {
    flex: 1;
    min-height: 120px;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }

  .chips select,
  .chips input {
    width: 140px;
    padding: 2px 6px;
  }

  .deps {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .deps li {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .linkish {
    padding: 2px 4px;
    flex: 1;
    justify-content: flex-start;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .bad {
    font-size: 11px;
    color: var(--danger);
  }
</style>
