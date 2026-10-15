<script lang="ts">
  import type { Status } from './types';

  // Shape carries the status so it reads without colour: empty ring (to do),
  // half-filled ring (in progress), ring with a bar (blocked), ring with a
  // check (done). Size is mirrored in gantt/barlayout.ts (GLYPH_W).
  let { status, size = 12 }: { status: Status; size?: number } = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  class="status-icon"
>
  <circle cx="12" cy="12" r="9.5" />
  {#if status === 'doing'}
    <path d="M12 5.5a6.5 6.5 0 010 13z" fill="currentColor" stroke="none" />
  {:else if status === 'blocked'}
    <path d="M7.5 12h9" />
  {:else if status === 'done'}
    <path d="M7.5 12.5l3 3 6-6.5" />
  {/if}
</svg>

<style>
  .status-icon {
    flex: none;
    display: block;
  }
</style>
