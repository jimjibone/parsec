<script lang="ts">
  import { app } from '../state.svelte';
  import { ui } from '../ui.svelte';
  import Icon from '../Icon.svelte';
  import ProjectEditor from './ProjectEditor.svelte';
  import { TwoClick } from '../confirm.svelte';
  import { formatShort, toDay, toISO, today } from '../dates';
  import { fmtRatio, pressureLabel } from '../pressure';
  import { STATUSES, type Person, type Task } from '../types';

  type SortKey = 'title' | 'project' | 'status' | 'start' | 'end' | 'estimate' | 'pressure' | 'created';

  let search = $state('');
  let statusFilter = $state('');
  let assigneeFilter = $state('');
  let sortKey = $state<SortKey>('start');
  let sortAsc = $state(true);
  let newTitle = $state('');
  let newProject = $state<string | null>(null);
  let renamingPerson = $state<string | null>(null);
  let newPerson = $state<string | null>(null);

  const selected = $derived(ui.trackerProject ? app.projectById.get(ui.trackerProject) : undefined);

  const statusOrder = Object.fromEntries(STATUSES.map((s, i) => [s.id, i]));

  const pressure = $derived(new Map(app.tasks.map((t) => [t.id, app.pressureOf(t)])));

  const rows = $derived.by(() => {
    const q = search.trim().toLowerCase();
    const list = app.tasks.filter((t) => {
      if (selected && t.projectId !== selected.id) return false;
      if (statusFilter === 'open' ? t.status === 'done' : statusFilter && t.status !== statusFilter) return false;
      if (assigneeFilter === '__none' ? t.assignees.length : assigneeFilter && !t.assignees.includes(assigneeFilter)) return false;
      if (q && !t.title.toLowerCase().includes(q) && !t.description.toLowerCase().includes(q)) return false;
      return true;
    });
    const key = (t: Task): string | number => {
      switch (sortKey) {
        case 'title':
          return t.title.toLowerCase();
        case 'project':
          return app.projectById.get(t.projectId)?.name.toLowerCase() ?? '';
        case 'status':
          return statusOrder[t.status];
        case 'start':
          return t.start;
        case 'end':
          return t.end;
        case 'estimate':
          return t.estimateHours;
        case 'pressure':
          return pressure.get(t.id)?.ratio ?? -1;
        case 'created':
          return t.created;
      }
    };
    return list.sort((a, b) => {
      // Tasks with no pressure sort last in both directions.
      if (sortKey === 'pressure') {
        const na = !pressure.get(a.id);
        const nb = !pressure.get(b.id);
        if (na !== nb) return na ? 1 : -1;
      }
      const ka = key(a);
      const kb = key(b);
      const c = ka < kb ? -1 : ka > kb ? 1 : a.title.localeCompare(b.title);
      return sortAsc ? c : -c;
    });
  });

  const totals = $derived({
    count: rows.length,
    hours: rows.reduce((n, t) => n + (t.estimateHours || 0), 0),
    done: rows.filter((t) => t.status === 'done').length,
  });

  function sortBy(k: SortKey) {
    if (sortKey === k) sortAsc = !sortAsc;
    else {
      sortKey = k;
      sortAsc = true;
    }
  }

  async function addTask() {
    const title = newTitle.trim();
    const pid = selected?.id ?? app.projects[0]?.id;
    if (!title || !pid) return;
    newTitle = '';
    const s = today();
    // Lane 0 = first free row from the top.
    await app.createTask(pid, { title, start: toISO(s), end: toISO(s + 2), lane: 0 });
  }

  async function createProject() {
    const name = newProject?.trim();
    newProject = null;
    if (!name) return;
    const p = await app.createProject(name);
    if (p) ui.trackerProject = p.id;
  }

  async function createPerson() {
    const name = newPerson?.trim();
    newPerson = null;
    if (name) await app.createPerson(name);
  }

  function renamePerson(id: string, name: string) {
    renamingPerson = null;
    name = name.trim();
    const p = app.personById.get(id);
    if (p && name && name !== p.name) app.savePerson({ ...p, name });
  }

  const confirmRemove = new TwoClick();

  function setPersonColor(p: Person, color: string) {
    app.savePerson({ ...p, color });
  }

  // Empty saves 0, which means "use the team default". Invalid input reverts
  // to the saved value rather than being sent.
  function setPersonHours(p: Person, el: HTMLInputElement) {
    const h = Number(el.value || 0);
    if (h >= 0 && h <= 24) app.savePerson({ ...p, hoursPerDay: h });
    else el.value = p.hoursPerDay ? String(p.hoursPerDay) : '';
  }

  function setPlanningHours(el: HTMLInputElement) {
    const h = Number(el.value);
    if (el.value !== '' && h > 0 && h <= 24) app.savePlanning({ ...app.planning, hoursPerDay: h });
    else el.value = String(app.planning.hoursPerDay);
  }

  function setAssigneeFactor(el: HTMLInputElement) {
    const pct = Number(el.value);
    if (el.value !== '' && pct >= 0 && pct <= 100) app.savePlanning({ ...app.planning, assigneeFactor: pct / 100 });
    else el.value = String(Math.round(app.planning.assigneeFactor * 100));
  }

  async function deletePerson(id: string) {
    if (!confirmRemove.hit(id)) return;
    if (assigneeFilter === id) assigneeFilter = '';
    await app.deletePerson(id);
  }

  function setStatus(t: Task, status: Task['status']) {
    app.saveTask({ ...t, status });
  }

  function focusSelect(el: HTMLInputElement) {
    el.focus();
    el.select();
  }

  const overdue = (t: Task) => t.status !== 'done' && toDay(t.end) < today();
</script>

<div class="tracker">
  <nav class="side">
    <div class="side-head">
      <h3>Projects</h3>
      {#if app.canEdit}
        <button class="ghost icon-btn" onclick={() => (newProject = '')} title="New project"><Icon name="plus" /></button>
      {/if}
    </div>
    <button class="item" class:active={!selected} onclick={() => (ui.trackerProject = '')}>
      <span class="dot all"></span> All tasks <span class="n">{app.tasks.length}</span>
    </button>
    {#each app.projects as p, i (p.id)}
      <div class="item-row">
        <button class="item" class:active={selected?.id === p.id} onclick={() => (ui.trackerProject = p.id)}>
          <span class="dot" style:background={p.color}></span>
          <span class="label">{p.name}</span>
          <span class="n">{app.tasks.filter((t) => t.projectId === p.id).length}</span>
        </button>
        {#if app.canEdit}
          <div class="move">
            <button class="ghost icon-btn" disabled={i === 0} onclick={() => app.moveProject(p.id, -1)} title="Move project up"
              ><Icon name="up" size={12} /></button
            >
            <button
              class="ghost icon-btn"
              disabled={i === app.projects.length - 1}
              onclick={() => app.moveProject(p.id, 1)}
              title="Move project down"><Icon name="down" size={12} /></button
            >
          </div>
        {/if}
      </div>
    {/each}
    {#if newProject !== null}
      <input
        type="text"
        placeholder="Project name"
        bind:value={newProject}
        use:focusSelect
        onkeydown={(e) => {
          if (e.key === 'Enter') createProject();
          if (e.key === 'Escape') newProject = null;
        }}
        onblur={createProject}
      />
    {/if}

    <div class="side-head people-head">
      <h3>People</h3>
      {#if app.canEdit}
        <button class="ghost icon-btn" onclick={() => (newPerson = '')} title="Add person"><Icon name="plus" /></button>
      {/if}
    </div>
    {#each app.people as p (p.id)}
      <div class="person">
        {#if renamingPerson === p.id}
          <input
            type="text"
            value={p.name}
            use:focusSelect
            onkeydown={(e) => {
              if (e.key === 'Enter') renamePerson(p.id, e.currentTarget.value);
              if (e.key === 'Escape') renamingPerson = null;
            }}
            onblur={(e) => renamePerson(p.id, e.currentTarget.value)}
          />
        {:else}
          <label class="swatch" style:background={app.colorOf(p.id)} title={app.canEdit ? 'Change colour' : ''}>
            <input
              type="color"
              value={app.colorOf(p.id)}
              disabled={!app.canEdit}
              onchange={(e) => setPersonColor(p, e.currentTarget.value)}
            />
          </label>
          <button
            class="ghost pname"
            ondblclick={() => app.canEdit && (renamingPerson = p.id)}
            onclick={() => (assigneeFilter = assigneeFilter === p.id ? '' : p.id)}
            class:active={assigneeFilter === p.id}
            title={app.canEdit ? 'Click to filter, double-click to rename' : 'Click to filter'}
          >
            {p.name}
            <span class="n">{app.tasks.filter((t) => t.assignees.includes(p.id) && t.status !== 'done').length}</span>
          </button>
          <input
            class="hpd"
            type="number"
            min="0"
            max="24"
            step="0.5"
            value={p.hoursPerDay || ''}
            placeholder={String(app.planning.hoursPerDay)}
            disabled={!app.canEdit}
            title="Hours per working day; empty uses the team default"
            aria-label="Hours per day for {p.name}"
            onchange={(e) => setPersonHours(p, e.currentTarget)}
          />
          {#if app.canEdit}
            <button
              class="ghost rm"
              class:icon-btn={!confirmRemove.is(p.id)}
              class:armed={confirmRemove.is(p.id)}
              onclick={() => deletePerson(p.id)}
              onblur={() => confirmRemove.reset()}
              aria-label="Remove person"
              title={confirmRemove.is(p.id) ? 'Click again to remove and unassign from all tasks' : 'Remove person'}
              ><Icon name="x" size={14} />{#if confirmRemove.is(p.id)}Remove?{/if}</button
            >
          {/if}
        {/if}
      </div>
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
    {/if}

    <div class="side-head people-head">
      <h3>Capacity</h3>
    </div>
    <label class="cap">
      <span>Hours per day</span>
      <input
        type="number"
        min="0.5"
        max="24"
        step="0.5"
        value={app.planning.hoursPerDay}
        disabled={!app.canEdit}
        onchange={(e) => setPlanningHours(e.currentTarget)}
      />
    </label>
    <label class="cap">
      <span>Each extra assignee adds</span>
      <span class="pct">
        <input
          type="number"
          min="0"
          max="100"
          step="5"
          value={Math.round(app.planning.assigneeFactor * 100)}
          disabled={!app.canEdit}
          onchange={(e) => setAssigneeFactor(e.currentTarget)}
        />%
      </span>
    </label>
    <p class="hint muted">Used for task pressure. 100% = work splits evenly between assignees.</p>
  </nav>

  <div class="main">
    {#if selected}
      <ProjectEditor project={selected} />
    {/if}

    {#if !app.projects.length}
      <div class="empty">
        <p class="muted">No projects yet.</p>
        {#if app.canEdit}
          <button class="primary" onclick={() => (newProject = '')}><Icon name="plus" /> Create a project</button>
        {/if}
      </div>
    {:else}
      <div class="filters">
        <input type="search" placeholder="Search tasks" bind:value={search} />
        <select bind:value={statusFilter}>
          <option value="">Any status</option>
          <option value="open">Not done</option>
          {#each STATUSES as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
        </select>
        <select bind:value={assigneeFilter}>
          <option value="">Anyone</option>
          <option value="__none">Unassigned</option>
          {#each app.people as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <div class="spacer"></div>
        <span class="muted totals"
          >{totals.count} tasks, {totals.done} done{#if totals.hours}, {totals.hours}h estimated{/if}</span
        >
      </div>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              {#snippet th(k: SortKey, label: string, cls = '')}
                <th class={cls}>
                  <button class="ghost sort" class:on={sortKey === k} onclick={() => sortBy(k)}>
                    {label}{#if sortKey === k}<Icon name={sortAsc ? 'up' : 'down'} size={12} />{/if}
                  </button>
                </th>
              {/snippet}
              {@render th('title', 'Task', 'w-title')}
              {#if !selected}{@render th('project', 'Project')}{/if}
              {@render th('status', 'Status')}
              <th>Assignees</th>
              {@render th('start', 'Start')}
              {@render th('end', 'End')}
              {@render th('estimate', 'Est.', 'num')}
              {@render th('pressure', 'Pressure', 'num')}
              <th class="num">Deps</th>
            </tr>
          </thead>
          <tbody>
            {#each rows as t (t.id)}
              {@const p = app.projectById.get(t.projectId)}
              {@const pr = pressure.get(t.id)}
              <tr class:sel={ui.selectedTask === t.id} class:done={t.status === 'done'} onclick={() => (ui.selectedTask = t.id)}>
                <td class="w-title"><span class="dot" style:background={p?.color}></span>{t.title}</td>
                {#if !selected}<td class="muted">{p?.name}</td>{/if}
                <td>
                  <select
                    class="status-sel {t.status}"
                    value={t.status}
                    disabled={!app.canEdit}
                    onclick={(e) => e.stopPropagation()}
                    onchange={(e) => setStatus(t, e.currentTarget.value as Task['status'])}
                  >
                    {#each STATUSES as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
                  </select>
                </td>
                <td>
                  {#each t.assignees as a (a)}<span class="chip plain"
                      ><span class="dot" style:background={app.colorOf(a)}></span>{app.personById.get(a)?.name ?? a}</span
                    >{/each}
                </td>
                <td class="date">{formatShort(t.start)}</td>
                <td class="date" class:overdue={overdue(t)}>{formatShort(t.end)}</td>
                <td class="num">{t.estimateHours ? `${t.estimateHours}h` : ''}</td>
                <td class="num level-{pr?.level}" title={pr ? pressureLabel(pr) : ''}>{pr ? fmtRatio(pr) : ''}</td>
                <td class="num muted">{t.dependsOn.length || ''}</td>
              </tr>
            {:else}
              <tr><td colspan="9" class="muted none">No tasks match.</td></tr>
            {/each}
          </tbody>
        </table>
        {#if app.canEdit}
          <form
            class="add"
            onsubmit={(e) => {
              e.preventDefault();
              addTask();
            }}
          >
            <Icon name="plus" />
            <input type="text" placeholder={`Add a task to ${selected?.name ?? app.projects[0]?.name}...`} bind:value={newTitle} />
          </form>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .tracker {
    display: flex;
    height: 100%;
  }

  .side {
    width: 240px;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 12px 8px;
    border-right: 1px solid var(--border);
    background: var(--surface);
    overflow: auto;
  }

  .side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 4px 4px 8px;
  }

  .people-head {
    margin-top: 18px;
  }

  h3 {
    margin: 0;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
  }

  .item {
    justify-content: flex-start;
    border: 0;
    background: transparent;
    gap: 8px;
  }

  .item-row {
    position: relative;
    display: flex;
    align-items: center;
  }

  .item-row .item {
    flex: 1;
    min-width: 0;
  }

  /* Overlays the task count on hover instead of reserving width. */
  .move {
    position: absolute;
    top: 0;
    bottom: 0;
    right: 4px;
    display: flex;
    align-items: center;
    padding-left: 12px;
    background: linear-gradient(to right, transparent, var(--surface) 12px);
    opacity: 0;
    pointer-events: none;
  }

  .item-row:hover .move,
  .move:focus-within {
    opacity: 1;
    pointer-events: auto;
  }

  .move button {
    padding: 2px;
  }

  .item.active,
  .pname.active {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .item .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .n {
    margin-left: auto;
    font-size: 12px;
    color: var(--text-3);
  }

  .dot.all {
    background: var(--text-3);
  }

  .person {
    display: flex;
    align-items: center;
  }

  .swatch {
    position: relative;
    width: 12px;
    height: 12px;
    margin: 0 2px 0 8px;
    border-radius: 50%;
    flex: none;
    cursor: pointer;
  }

  .swatch input {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
  }

  /* Hours per day per person; compact so names keep their room. */
  .hpd {
    width: 44px;
    flex: none;
    padding: 2px 4px;
    font-size: 12px;
    text-align: right;
  }

  .cap {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 2px 4px 2px 8px;
    font-size: 13px;
  }

  .cap input {
    width: 56px;
    padding: 2px 4px;
    font-size: 12px;
    text-align: right;
  }

  .pct {
    display: flex;
    align-items: center;
    gap: 2px;
    font-size: 12px;
    color: var(--text-2);
  }

  .hint {
    margin: 4px 4px 0 8px;
    font-size: 12px;
  }

  .level-tight {
    color: var(--warn);
  }

  .level-over {
    color: var(--danger);
  }

  .chip.plain .dot {
    width: 8px;
    height: 8px;
  }

  .pname {
    flex: 1;
    justify-content: flex-start;
    min-width: 0;
  }

  .person .rm {
    opacity: 0;
  }

  .person:hover .rm,
  .person .rm.armed {
    opacity: 1;
  }

  .rm.armed {
    padding: 2px 6px;
    gap: 2px;
    color: var(--danger);
    background: var(--danger-soft);
  }

  .side input {
    margin: 2px 4px;
  }

  .main {
    flex: 1;
    min-width: 0;
    overflow: auto;
    padding: 16px 20px 40px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .filters {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  .filters input {
    width: 240px;
  }

  .spacer {
    flex: 1;
  }

  .totals {
    font-size: 13px;
  }

  .table-wrap {
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--surface);
    overflow: hidden;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  th {
    text-align: left;
    padding: 4px 6px;
    background: var(--surface-2);
    border-bottom: 1px solid var(--border);
    font-weight: 500;
    color: var(--text-2);
    white-space: nowrap;
  }

  th.num,
  td.num {
    text-align: right;
  }

  .sort {
    padding: 2px 4px;
    gap: 2px;
    color: inherit;
  }

  .sort.on {
    color: var(--text);
  }

  td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
  }

  tbody tr {
    cursor: pointer;
  }

  tbody tr:hover {
    background: var(--surface-2);
  }

  tr.sel {
    background: var(--accent-soft) !important;
  }

  tr.done .w-title {
    color: var(--text-3);
    text-decoration: line-through;
  }

  .w-title {
    width: 40%;
  }

  td.w-title {
    font-weight: 500;
  }

  td.w-title .dot {
    margin-right: 8px;
    width: 8px;
    height: 8px;
  }

  .date {
    white-space: nowrap;
  }

  .overdue {
    color: var(--danger);
  }

  .status-sel {
    padding: 1px 6px;
    font-size: 12px;
    border-radius: 999px;
    border-color: transparent;
    background: var(--surface-3);
  }

  .status-sel.doing {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .status-sel.blocked {
    background: var(--danger-soft);
    color: var(--danger);
  }

  .status-sel.done {
    color: var(--ok);
  }

  .chip.plain {
    padding: 1px 8px;
    margin-right: 4px;
  }

  .none {
    text-align: center;
    padding: 20px;
  }

  .add {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    color: var(--text-3);
  }

  .add input {
    flex: 1;
    border-color: transparent;
    background: transparent;
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 60px;
  }
</style>
