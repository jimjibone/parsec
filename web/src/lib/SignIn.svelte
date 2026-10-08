<script lang="ts">
  import { api, ApiError } from './api';
  import { app } from './state.svelte';

  const mode = $derived(app.me?.auth.mode);
  const label = $derived(app.me?.auth.label || 'single sign-on');

  // The oidc callback reports failures in the query string.
  const initialError = new URLSearchParams(location.search).get('signin_error') ?? '';
  if (initialError) history.replaceState(null, '', location.pathname);
  let error = $state(initialError);

  let username = $state('');
  let password = $state('');
  let busy = $state(false);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.localLogin(username.trim(), password);
      password = '';
      await app.start();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="signin">
  <div class="card">
    <img src="/favicon.svg" alt="" width="48" height="48" />
    <h1>parsec</h1>

    {#if mode === 'oidc'}
      <a class="button primary" href="/auth/login">Sign in with {label}</a>
    {:else if mode === 'local'}
      <form onsubmit={submit}>
        <label class="field">
          <span>Username</span>
          <!-- svelte-ignore a11y_autofocus -->
          <input type="text" bind:value={username} autocomplete="username" autofocus required />
        </label>
        <label class="field">
          <span>Password</span>
          <input type="password" bind:value={password} autocomplete="current-password" required />
        </label>
        <button class="primary" type="submit" disabled={busy}>{busy ? 'Signing in...' : 'Sign in'}</button>
      </form>
    {/if}

    {#if error}
      <p class="error">{error}</p>
    {/if}
  </div>
</div>

<style>
  .signin {
    display: grid;
    place-items: center;
    height: 100%;
    padding: 16px;
  }

  .card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    width: min(340px, 100%);
    padding: 28px 24px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }

  h1 {
    margin: 0;
    font-size: 22px;
    letter-spacing: 0.02em;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 100%;
  }

  .button {
    display: inline-flex;
    justify-content: center;
    width: 100%;
    padding: 8px 12px;
    border-radius: var(--radius);
    text-decoration: none;
    font-weight: 600;
  }

  .button.primary {
    background: var(--accent);
    color: var(--accent-text);
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
    text-align: center;
  }
</style>
