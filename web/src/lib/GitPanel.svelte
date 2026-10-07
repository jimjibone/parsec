<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from './api';
  import { app } from './state.svelte';
  import { ui } from './ui.svelte';
  import Icon from './Icon.svelte';
  import type { GitCommit } from './types';

  let message = $state('');
  let busy = $state('');
  let output = $state('');
  let failed = $state(false);
  let log = $state<GitCommit[]>([]);
  let remoteDraft = $state('');
  let editingRemote = $state(false);

  const git = $derived(app.git);

  async function refreshLog() {
    try {
      log = await api.gitLog();
    } catch {
      log = [];
    }
  }

  onMount(() => {
    app.refreshGit();
    refreshLog();
  });

  async function op(name: string, fn: () => Promise<{ output: string }>, reload = false) {
    busy = name;
    output = '';
    failed = false;
    try {
      const r = await fn();
      output = r.output.trim() || `${name}: done`;
    } catch (e) {
      failed = true;
      output = e instanceof ApiError ? [e.message, e.output].filter(Boolean).join('\n\n') : String(e);
    } finally {
      busy = '';
      if (reload) await app.load();
      else app.refreshGit();
      refreshLog();
    }
  }

  async function commit() {
    await op('Commit', () => api.gitCommit(message));
    if (!failed) message = '';
  }

  async function saveRemote() {
    try {
      app.git = await api.gitSetRemote(remoteDraft);
      editingRemote = false;
    } catch (e) {
      failed = true;
      output = String((e as Error).message);
    }
  }

  function codeLabel(code: string): string {
    const c = code.trim();
    if (c === '??' || c.includes('A')) return 'added';
    if (c.includes('D')) return 'deleted';
    if (c.includes('R')) return 'renamed';
    if (c.includes('U')) return 'conflict';
    return 'modified';
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') ui.gitOpen = false;
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="backdrop" onclick={() => (ui.gitOpen = false)} role="presentation"></div>
<aside class="panel">
  <div class="head">
    <h2><Icon name="branch" /> Data repository</h2>
    <button class="ghost icon-btn" onclick={() => (ui.gitOpen = false)} aria-label="Close"><Icon name="x" /></button>
  </div>

  {#if !git}
    <p class="muted">Git status unavailable.</p>
  {:else}
    <section class="meta">
      <div>
        <span class="muted">Branch</span>
        <b>{git.branch || '(detached)'}</b>
        {#if git.upstream}<span class="muted">tracking {git.upstream}</span>{/if}
      </div>
      <div class="remote">
        <span class="muted">Remote</span>
        {#if editingRemote}
          <input type="text" bind:value={remoteDraft} placeholder="git@example.com:team/plans.git" />
          <button onclick={saveRemote}>Save</button>
          <button class="ghost" onclick={() => (editingRemote = false)}>Cancel</button>
        {:else}
          <code>{git.remote || 'none'}</code>
          <button
            class="ghost"
            onclick={() => {
              remoteDraft = git.remote;
              editingRemote = true;
            }}>Edit</button
          >
        {/if}
      </div>
      {#if git.ahead || git.behind}
        <div class="muted">{git.ahead} to push, {git.behind} to pull (as of last fetch)</div>
      {/if}
    </section>

    {#if git.rebasing}
      <section class="warnbox">
        A rebase stopped on conflicts. Resolve them in the data repository with git, or abort to return to the state before the pull.
        <button class="danger" disabled={!!busy} onclick={() => op('Abort rebase', api.gitAbortRebase, true)}>Abort rebase</button>
      </section>
    {/if}

    <section>
      <h3>Changes <span class="muted">({git.files.length})</span></h3>
      {#if git.files.length}
        <ul class="files">
          {#each git.files as f (f.path)}
            <li><span class="code {codeLabel(f.code)}">{codeLabel(f.code)}</span> <code>{f.path}</code></li>
          {/each}
        </ul>
      {:else}
        <p class="muted">Working tree clean.</p>
      {/if}
      <form
        class="commit"
        onsubmit={(e) => {
          e.preventDefault();
          commit();
        }}
      >
        <input type="text" bind:value={message} placeholder="Commit message (default: Update via parsec)" />
        <button class="primary" type="submit" disabled={!!busy || !git.files.length}>Commit</button>
      </form>
    </section>

    <section class="actions">
      <button disabled={!!busy || !git.remote} onclick={() => op('Pull', api.gitPull, true)}><Icon name="down" /> Pull</button>
      <button disabled={!!busy || !git.remote} onclick={() => op('Push', api.gitPush)}><Icon name="up" /> Push</button>
      <button class="primary" disabled={!!busy || !git.remote} onclick={() => op('Sync', api.gitSync, true)}
        ><Icon name="sync" /> Sync</button
      >
      {#if busy}<span class="muted">{busy}...</span>{/if}
    </section>
    {#if !git.remote}
      <p class="muted small">Set a remote to enable pull, push and sync.</p>
    {/if}

    {#if output}
      <pre class="output" class:failed>{output}</pre>
    {/if}

    <section>
      <h3>History</h3>
      {#if log.length}
        <ul class="log">
          {#each log as c (c.hash)}
            <li>
              <code>{c.hash}</code>
              <span class="subj">{c.subject}</span>
              <span class="muted small">{c.author}, {c.when}</span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted">No commits yet.</p>
      {/if}
    </section>
  {/if}
</aside>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }

  .panel {
    position: fixed;
    top: 52px;
    right: 12px;
    width: min(460px, calc(100vw - 24px));
    max-height: calc(100vh - 64px);
    overflow: auto;
    z-index: 41;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 0;
    font-size: 15px;
  }

  h3 {
    margin: 0 0 6px;
    font-size: 13px;
  }

  section {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .meta {
    font-size: 13px;
  }

  .remote {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .remote input {
    flex: 1;
  }

  code {
    font-family: var(--mono);
    font-size: 12px;
    word-break: break-all;
  }

  .files,
  .log {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 180px;
    overflow: auto;
    font-size: 13px;
  }

  .files li {
    display: flex;
    gap: 8px;
    align-items: baseline;
    padding: 2px 0;
  }

  .code {
    font-size: 11px;
    width: 58px;
    flex: none;
    color: var(--warn);
  }

  .code.added {
    color: var(--ok);
  }

  .code.deleted,
  .code.conflict {
    color: var(--danger);
  }

  .commit {
    display: flex;
    gap: 6px;
  }

  .commit input {
    flex: 1;
  }

  .actions {
    flex-direction: row;
    align-items: center;
  }

  .output {
    margin: 0;
    padding: 8px 10px;
    background: var(--surface-2);
    border-radius: var(--radius);
    font: 12px/1.45 var(--mono);
    white-space: pre-wrap;
    max-height: 200px;
    overflow: auto;
  }

  .output.failed {
    color: var(--danger);
  }

  .warnbox {
    padding: 10px;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    font-size: 13px;
    align-items: flex-start;
  }

  .log li {
    display: grid;
    grid-template-columns: auto 1fr;
    column-gap: 8px;
    padding: 3px 0;
    border-bottom: 1px solid var(--border);
  }

  .log .small {
    grid-column: 2;
  }

  .small {
    font-size: 12px;
  }
</style>
