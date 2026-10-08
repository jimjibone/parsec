<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from './api';
  import { app } from './state.svelte';
  import { ui } from './ui.svelte';
  import { TwoClick } from './confirm.svelte';
  import Icon from './Icon.svelte';
  import type { Role, User, UserSettings } from './types';

  const ROLES: { id: Role; label: string; hint: string }[] = [
    { id: 'none', label: 'No access', hint: 'Can sign in but sees nothing' },
    { id: 'viewer', label: 'Viewer', hint: 'Sees everything, changes nothing' },
    { id: 'editor', label: 'Editor', hint: 'Edits projects, tasks and people' },
    { id: 'admin', label: 'Admin', hint: 'Editor, plus git and this panel' },
  ];

  const local = $derived(app.me?.auth.mode === 'local');
  const me = $derived(app.me?.user?.username ?? '');

  let users = $state<User[]>([]);
  let settings = $state<UserSettings>({ defaultRole: 'viewer' });
  let error = $state('');

  let draft = $state({ username: '', name: '', role: 'editor' as Role, password: '' });
  let pwFor = $state<string | null>(null);
  let pwDraft = $state('');
  const confirmRemove = new TwoClick();

  async function refresh() {
    try {
      const r = await api.users();
      users = r.users.sort((a, b) => (a.name || a.username).localeCompare(b.name || b.username));
      settings = r.settings;
    } catch (e) {
      fail(e);
    }
  }

  onMount(refresh);

  function fail(e: unknown) {
    error = e instanceof ApiError ? e.message : String(e);
  }

  async function run(fn: () => Promise<unknown>) {
    error = '';
    try {
      await fn();
    } catch (e) {
      fail(e);
    }
    await refresh();
  }

  function setRole(u: User, role: Role) {
    run(() => api.updateUser(u.username, { role }));
  }

  function setDefault(role: Role) {
    run(() => api.saveSettings({ defaultRole: role }));
  }

  function add(e: SubmitEvent) {
    e.preventDefault();
    const d = { ...draft, username: draft.username.trim() };
    if (!d.username) return;
    run(async () => {
      await api.createUser({ username: d.username, name: d.name.trim(), role: d.role, password: local ? d.password : undefined });
      draft = { username: '', name: '', role: 'editor', password: '' };
    });
  }

  function savePassword(u: User) {
    const pw = pwDraft;
    pwFor = null;
    pwDraft = '';
    if (pw) run(() => api.updateUser(u.username, { password: pw }));
  }

  function remove(u: User) {
    if (confirmRemove.hit(u.username)) run(() => api.deleteUser(u.username));
  }

  function seen(iso: string): string {
    if (!iso) return 'never';
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') ui.settingsOpen = false;
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="backdrop" onclick={() => (ui.settingsOpen = false)} role="presentation"></div>
<aside class="panel" aria-label="Settings">
  <div class="head">
    <h2>People and access</h2>
    <button class="ghost icon-btn" onclick={() => (ui.settingsOpen = false)} aria-label="Close"><Icon name="x" /></button>
  </div>

  <section>
    <label class="field inline">
      <span>{local ? 'Role for new accounts' : 'Role on first sign-in'}</span>
      <select value={settings.defaultRole} onchange={(e) => setDefault(e.currentTarget.value as Role)}>
        {#each ROLES.filter((r) => r.id !== 'admin') as r (r.id)}<option value={r.id}>{r.label}</option>{/each}
      </select>
    </label>
    {#if !local}
      <p class="muted small">
        People appear here after their first sign-in. Add a username below to set someone's role before they sign in.
      </p>
    {/if}
  </section>

  {#if error}<p class="error">{error}</p>{/if}

  <section>
    <table>
      <thead>
        <tr><th>Person</th><th>Role</th><th>Last seen</th><th></th></tr>
      </thead>
      <tbody>
        {#each users as u (u.username)}
          <tr>
            <td>
              <div class="who">
                <b>{u.name || u.username}</b>
                <span class="muted small">{u.username}{u.email ? ` - ${u.email}` : ''}</span>
              </div>
            </td>
            <td>
              <select
                value={u.role}
                disabled={u.fixed || u.username === me}
                title={u.fixed ? 'Admin in the server config file' : u.username === me ? 'Your own role' : ''}
                onchange={(e) => setRole(u, e.currentTarget.value as Role)}
              >
                {#each ROLES as r (r.id)}<option value={r.id} title={r.hint}>{r.label}</option>{/each}
              </select>
            </td>
            <td class="muted small">{seen(u.lastSeen)}</td>
            <td class="acts">
              {#if local}
                {#if pwFor === u.username}
                  <form
                    class="pw"
                    onsubmit={(e) => {
                      e.preventDefault();
                      savePassword(u);
                    }}
                  >
                    <!-- svelte-ignore a11y_autofocus -->
                    <input type="password" bind:value={pwDraft} placeholder="New password" autocomplete="new-password" autofocus />
                    <button type="submit">Set</button>
                    <button type="button" class="ghost" onclick={() => (pwFor = null)}>Cancel</button>
                  </form>
                {:else}
                  <button class="ghost" onclick={() => (pwFor = u.username)}>{u.hasPassword ? 'Reset password' : 'Set password'}</button>
                {/if}
              {/if}
              {#if !u.fixed && u.username !== me}
                <button
                  class="ghost icon-btn"
                  class:danger={confirmRemove.is(u.username)}
                  onclick={() => remove(u)}
                  onblur={() => confirmRemove.reset()}
                  title={confirmRemove.is(u.username) ? 'Click again to remove' : 'Remove'}><Icon name="trash" size={14} /></button
                >
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </section>

  <section>
    <h3>{local ? 'Add an account' : 'Add someone before they sign in'}</h3>
    <form class="add" onsubmit={add}>
      <input type="text" bind:value={draft.username} placeholder={local ? 'Username' : 'GitLab username'} required />
      <input type="text" bind:value={draft.name} placeholder="Display name (optional)" />
      {#if local}
        <input type="password" bind:value={draft.password} placeholder="Password (8+ characters)" autocomplete="new-password" required />
      {/if}
      <select bind:value={draft.role}>
        {#each ROLES as r (r.id)}<option value={r.id}>{r.label}</option>{/each}
      </select>
      <button class="primary" type="submit"><Icon name="plus" /> Add</button>
    </form>
  </section>

  <p class="muted small">
    {#each ROLES as r, i (r.id)}<b>{r.label}</b>: {r.hint.toLowerCase()}{i < ROLES.length - 1 ? '. ' : '.'}{/each}
  </p>
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
    width: min(640px, calc(100vw - 24px));
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

  .field.inline {
    flex-direction: row;
    align-items: center;
    gap: 10px;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  th {
    text-align: left;
    font-weight: 600;
    font-size: 12px;
    color: var(--text-2);
    padding: 4px 6px;
    border-bottom: 1px solid var(--border);
  }

  td {
    padding: 6px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
  }

  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .acts {
    text-align: right;
    white-space: nowrap;
  }

  .pw,
  .add {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .pw input {
    width: 140px;
  }

  .add input {
    flex: 1 1 140px;
  }

  .small {
    font-size: 12px;
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
  }
</style>
