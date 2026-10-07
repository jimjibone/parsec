<script lang="ts">
  import { app } from '../state.svelte';
  import { ui } from '../ui.svelte';
  import Icon from '../Icon.svelte';
  import { TwoClick } from '../confirm.svelte';
  import { formatLong, toDay, toISO, today } from '../dates';
  import { PROJECT_COLORS, type Milestone, type Project } from '../types';

  let { project }: { project: Project } = $props();

  function save(patch: Partial<Project>) {
    app.saveProject({ ...project, ...patch });
  }

  function setMilestone(i: number, patch: Partial<Milestone>) {
    const ms = project.milestones.map((m, j) => (j === i ? { ...m, ...patch } : m));
    if (ms[i].name.trim() && ms[i].date) save({ milestones: ms });
  }

  function addMilestone() {
    save({ milestones: [...project.milestones, { id: '', name: 'Milestone', date: toISO(today() + 14) }] });
  }

  function removeMilestone(i: number) {
    save({ milestones: project.milestones.filter((_, j) => j !== i) });
  }

  const confirmDelete = new TwoClick();
  const taskCount = $derived(app.tasks.filter((t) => t.projectId === project.id).length);

  $effect(() => {
    void project.id;
    confirmDelete.reset();
  });

  async function remove() {
    if (!confirmDelete.hit(project.id)) return;
    // Read the id first: clearing the selection unbinds the `project` prop.
    const id = project.id;
    ui.trackerProject = '';
    await app.deleteProject(id);
  }
</script>

<section class="editor">
  <div class="row">
    {#key project.id}
      <input
        class="name"
        type="text"
        value={project.name}
        onchange={(e) => e.currentTarget.value.trim() && save({ name: e.currentTarget.value })}
      />
    {/key}
    <div class="colors">
      {#each PROJECT_COLORS as c (c)}
        <button
          class="swatch"
          class:active={project.color === c}
          style:background={c}
          onclick={() => save({ color: c })}
          aria-label="Colour {c}"
        ></button>
      {/each}
      <input type="color" value={project.color} onchange={(e) => save({ color: e.currentTarget.value })} title="Custom colour" />
    </div>
    <div class="spacer"></div>
    <button class="ghost danger" class:armed={confirmDelete.is(project.id)} onclick={remove} onblur={() => confirmDelete.reset()}>
      <Icon name="trash" />
      {#if confirmDelete.is(project.id)}
        Delete project and {taskCount} task{taskCount === 1 ? '' : 's'}?
      {:else}
        Delete project
      {/if}
    </button>
  </div>

  {#key project.id}
    <textarea
      rows="2"
      placeholder="Project description"
      value={project.description}
      onchange={(e) => save({ description: e.currentTarget.value })}></textarea>
  {/key}

  <div class="ms">
    <div class="ms-head">
      <h3>Milestones</h3>
      <button class="ghost" onclick={addMilestone}><Icon name="plus" /> Add</button>
    </div>
    {#if project.milestones.length}
      <ul>
        {#each project.milestones as m, i (m.id || i)}
          <li>
            <span class="diamond" style:background={project.color}></span>
            <input type="text" value={m.name} onchange={(e) => setMilestone(i, { name: e.currentTarget.value })} />
            <input type="date" value={m.date} onchange={(e) => setMilestone(i, { date: e.currentTarget.value })} />
            <span class="muted when">{formatLong(toDay(m.date))}</span>
            <button class="ghost icon-btn" onclick={() => removeMilestone(i)} aria-label="Remove milestone"
              ><Icon name="x" size={14} /></button
            >
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">No milestones.</p>
    {/if}
  </div>
</section>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--surface);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }

  .name {
    font-size: 18px;
    font-weight: 600;
    min-width: 200px;
  }

  .colors {
    display: flex;
    gap: 4px;
    align-items: center;
  }

  .swatch {
    width: 20px;
    height: 20px;
    padding: 0;
    border-radius: 50%;
    border: 2px solid transparent;
  }

  .swatch.active {
    border-color: var(--text);
  }

  input[type='color'] {
    width: 28px;
    height: 24px;
    padding: 0;
    border: 0;
    background: none;
  }

  .spacer {
    flex: 1;
  }

  .armed {
    background: var(--danger-soft);
  }

  .ms-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  h3 {
    margin: 0;
    font-size: 13px;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  li {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  li input[type='text'] {
    flex: 1;
    max-width: 280px;
  }

  .diamond {
    width: 10px;
    height: 10px;
    transform: rotate(45deg);
    flex: none;
  }

  .when {
    font-size: 12px;
  }

  p {
    margin: 0;
    font-size: 13px;
  }
</style>
