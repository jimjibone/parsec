<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from './lib/state.svelte';
  import { ui } from './lib/ui.svelte';
  import Icon from './lib/Icon.svelte';
  import Timeline from './lib/gantt/Timeline.svelte';
  import Tracker from './lib/tracker/Tracker.svelte';
  import TaskDrawer from './lib/TaskDrawer.svelte';
  import GitPanel from './lib/GitPanel.svelte';

  onMount(() => {
    app.load();
    const onFocus = () => app.refreshGit();
    window.addEventListener('focus', onFocus);
    const timer = setInterval(() => app.refreshGit(), 60_000);
    return () => {
      window.removeEventListener('focus', onFocus);
      clearInterval(timer);
    };
  });

  $effect(() => {
    ui.persist();
  });

  const dirty = $derived(app.git?.files.length ?? 0);
</script>

<div class="shell">
  <header>
    <div class="brand" title="A unit of time, obviously.">
      <img src="/favicon.svg" alt="" width="22" height="22" />
      <span>parsec</span>
    </div>

    <nav class="views">
      <button class:active={ui.view === 'timeline'} onclick={() => (ui.view = 'timeline')}>
        <Icon name="timeline" /> Timeline
      </button>
      <button class:active={ui.view === 'tracker'} onclick={() => (ui.view = 'tracker')}>
        <Icon name="list" /> Projects &amp; tasks
      </button>
    </nav>

    <div class="spacer"></div>

    <button class="git-btn" class:open={ui.gitOpen} onclick={() => (ui.gitOpen = !ui.gitOpen)} title="Git sync">
      <Icon name="branch" />
      <span>{app.git?.branch ?? 'git'}</span>
      {#if dirty}<span class="badge" title="Uncommitted changes">{dirty}</span>{/if}
      {#if app.git?.ahead}<span class="ab" title="Commits to push"><Icon name="up" size={12} />{app.git.ahead}</span>{/if}
      {#if app.git?.behind}<span class="ab" title="Commits to pull"><Icon name="down" size={12} />{app.git.behind}</span>{/if}
      {#if app.git?.rebasing}<span class="badge warn">rebase</span>{/if}
    </button>
  </header>

  <main>
    {#if app.loadError}
      <div class="empty">
        <p>Could not load data: {app.loadError}</p>
        <button onclick={() => app.load()}>Retry</button>
      </div>
    {:else if !app.loaded}
      <div class="empty muted">Loading...</div>
    {:else if ui.view === 'timeline'}
      <Timeline />
    {:else}
      <Tracker />
    {/if}
  </main>

  {#if ui.selectedTask && app.taskById.get(ui.selectedTask)}
    <TaskDrawer taskId={ui.selectedTask} />
  {/if}

  {#if ui.gitOpen}
    <GitPanel />
  {/if}

  <div class="toasts">
    {#each app.toasts as t (t.id)}
      <div class="toast {t.kind}">
        <span>{t.text}</span>
        <button class="ghost icon-btn" onclick={() => app.dismiss(t.id)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
      </div>
    {/each}
  </div>
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 8px 14px;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex: none;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 700;
    font-size: 17px;
    letter-spacing: 0.02em;
    cursor: default;
  }

  .views {
    display: flex;
    gap: 2px;
    padding: 2px;
    background: var(--surface-2);
    border-radius: 8px;
  }

  .views button {
    border: 0;
    background: transparent;
    color: var(--text-2);
  }

  .views button.active {
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
  }

  .spacer {
    flex: 1;
  }

  .git-btn.open {
    border-color: var(--accent);
  }

  .badge {
    padding: 0 6px;
    border-radius: 999px;
    background: var(--accent);
    color: var(--accent-text);
    font-size: 11px;
    font-weight: 600;
  }

  .badge.warn {
    background: var(--warn);
  }

  .ab {
    display: inline-flex;
    align-items: center;
    font-size: 12px;
    color: var(--text-2);
  }

  main {
    flex: 1;
    min-height: 0;
    position: relative;
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    height: 100%;
  }

  .toasts {
    position: fixed;
    left: 50%;
    bottom: 20px;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 100;
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 10px;
    max-width: min(560px, 90vw);
    padding: 8px 8px 8px 14px;
    border-radius: var(--radius);
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--shadow);
  }

  .toast.error {
    border-color: var(--danger);
    color: var(--danger);
  }
</style>
