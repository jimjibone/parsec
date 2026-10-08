<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from './lib/state.svelte';
  import { ui } from './lib/ui.svelte';
  import Icon from './lib/Icon.svelte';
  import Timeline from './lib/gantt/Timeline.svelte';
  import Tracker from './lib/tracker/Tracker.svelte';
  import TaskDrawer from './lib/TaskDrawer.svelte';
  import GitPanel from './lib/GitPanel.svelte';
  import SignIn from './lib/SignIn.svelte';
  import Settings from './lib/Settings.svelte';

  onMount(() => {
    app.start();
    const onFocus = () => app.loaded && app.refreshGit();
    window.addEventListener('focus', onFocus);
    const timer = setInterval(() => {
      if (app.loaded) app.refreshGit();
      // Waiting for access: there is no event stream, so check now and then.
      else if (app.me?.user && app.role === 'none') app.start();
    }, 60_000);
    return () => {
      window.removeEventListener('focus', onFocus);
      clearInterval(timer);
    };
  });

  $effect(() => {
    ui.persist();
  });

  const dirty = $derived(app.git?.files.length ?? 0);
  const user = $derived(app.me?.user ?? null);
  // In shared mode only admins drive git; everyone else sees nothing of it.
  const showGit = $derived(!app.shared || app.isAdmin);
  let userMenu = $state(false);

  const mac = /Mac|iPhone|iPad/.test(navigator.platform);
  const mod = mac ? 'Cmd+' : 'Ctrl+';

  // Text fields keep the browser's own undo for their contents.
  const TEXT_FIELD = 'textarea, input:not([type=checkbox], [type=radio], [type=button], [type=submit], [type=color], [type=range])';

  function onKey(e: KeyboardEvent) {
    if (!(mac ? e.metaKey : e.ctrlKey) || e.altKey) return;
    const k = e.key.toLowerCase();
    const redo = (k === 'z' && e.shiftKey) || (!mac && k === 'y' && !e.shiftKey);
    const undo = k === 'z' && !e.shiftKey;
    if ((!undo && !redo) || !app.canEdit) return;
    const t = e.target;
    if (t instanceof HTMLElement && (t.isContentEditable || t.closest(TEXT_FIELD))) return;
    e.preventDefault();
    if (redo) app.redo();
    else app.undo();
  }
</script>

<svelte:window
  onkeydown={onKey}
  onpointerdown={() => (app.pointerDown = true)}
  onpointerup={() => app.pointerReleased()}
  onpointercancel={() => app.pointerReleased()}
/>

{#if !app.me}
  <div class="empty muted">
    {#if app.meError}
      <p>Could not reach the server: {app.meError}</p>
      <button onclick={() => app.start()}>Retry</button>
    {:else}
      Loading...
    {/if}
  </div>
{:else if !user}
  <SignIn />
{:else if app.role === 'none'}
  <div class="empty">
    <img src="/favicon.svg" alt="" width="48" height="48" />
    <p>Signed in as <strong>{user.name || user.username}</strong>. An admin has not given this account access yet.</p>
    <div class="row">
      <button onclick={() => app.start()}>Check again</button>
      <button class="ghost" onclick={() => app.logout()}>Sign out</button>
    </div>
  </div>
{:else}
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

      {#if app.canEdit}
        <div class="history">
          <button
            class="icon-btn"
            disabled={!app.history.undo}
            onclick={() => app.undo()}
            title={app.history.undo ? `Undo: ${app.history.undo} (${mod}Z)` : 'Nothing to undo'}
            aria-label="Undo"
          >
            <Icon name="undo" />
          </button>
          <button
            class="icon-btn"
            disabled={!app.history.redo}
            onclick={() => app.redo()}
            title={app.history.redo ? `Redo: ${app.history.redo} (${mod}Shift+Z)` : 'Nothing to redo'}
            aria-label="Redo"
          >
            <Icon name="redo" />
          </button>
        </div>
      {/if}

      {#if showGit}
        <button class="git-btn" class:open={ui.gitOpen} onclick={() => (ui.gitOpen = !ui.gitOpen)} title="Git sync">
          <Icon name="branch" />
          <span>{app.git?.branch ?? 'git'}</span>
          {#if dirty}<span class="badge" title="Uncommitted changes">{dirty}</span>{/if}
          {#if app.git?.ahead}<span class="ab" title="Commits to push"><Icon name="up" size={12} />{app.git.ahead}</span>{/if}
          {#if app.git?.behind}<span class="ab" title="Commits to pull"><Icon name="down" size={12} />{app.git.behind}</span>{/if}
          {#if app.git?.rebasing}<span class="badge warn">rebase</span>{/if}
          {#if app.git?.autoError}<span class="badge warn" title={app.git.autoError}>!</span>{/if}
        </button>
      {/if}

      {#if app.shared}
        <div class="menu-wrap">
          <button class="ghost user-btn" class:open={userMenu} onclick={() => (userMenu = !userMenu)} title="{user.username} ({app.role})">
            <span class="avatar">{(user.name || user.username).slice(0, 1).toUpperCase()}</span>
            <span class="uname">{user.name || user.username}</span>
            {#if app.role === 'viewer'}<span class="chip">view only</span>{/if}
          </button>
          {#if userMenu}
            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
            <div class="menu" onclick={() => (userMenu = false)}>
              <div class="menu-head">
                <strong>{user.name || user.username}</strong>
                <span class="muted">{user.username} - {app.role}</span>
              </div>
              {#if app.isAdmin}
                <button class="ghost" onclick={() => (ui.settingsOpen = true)}>Settings: people and access</button>
              {/if}
              <button class="ghost" onclick={() => app.logout()}>Sign out</button>
            </div>
          {/if}
        </div>
      {/if}
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

    {#if ui.gitOpen && showGit}
      <GitPanel />
    {/if}

    {#if ui.settingsOpen && app.isAdmin}
      <Settings />
    {/if}
  </div>
{/if}

<div class="toasts">
  {#each app.toasts as t (t.id)}
    <div class="toast {t.kind}">
      <span>{t.text}</span>
      <button class="ghost icon-btn" onclick={() => app.dismiss(t.id)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
    </div>
  {/each}
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

  .history {
    display: flex;
    gap: 2px;
  }

  .menu-wrap {
    position: relative;
  }

  .user-btn.open {
    border-color: var(--accent);
  }

  .avatar {
    display: inline-grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-text);
    font-size: 11px;
    font-weight: 700;
  }

  .uname {
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    min-width: 220px;
    display: flex;
    flex-direction: column;
    padding: 4px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow);
    z-index: 50;
  }

  .menu button {
    justify-content: flex-start;
  }

  .menu-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px 8px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 4px;
    font-size: 13px;
  }

  .row {
    display: flex;
    gap: 8px;
  }

  .empty p {
    max-width: 420px;
    text-align: center;
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
