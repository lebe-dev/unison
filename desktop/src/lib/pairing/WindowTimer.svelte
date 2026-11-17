<script lang="ts">
  import { onDestroy } from "svelte";
  import { formatRemaining, remainingSeconds } from "./countdown";

  let {
    expiresAt,
    onExpire,
  }: {
    /** Time the pairing window closes, unix milliseconds. */
    expiresAt: number;
    /** Called once when the deadline passes. */
    onExpire?: () => void;
  } = $props();

  let now = $state(Date.now());
  const timer = setInterval(() => {
    now = Date.now();
  }, 1_000);
  onDestroy(() => clearInterval(timer));

  const seconds = $derived(remainingSeconds(expiresAt, now));
  const urgent = $derived(seconds > 0 && seconds <= 30);

  let notified = false;
  $effect(() => {
    if (seconds > 0) {
      notified = false;
      return;
    }
    if (notified) {
      return;
    }
    notified = true;
    onExpire?.();
  });
</script>

<span class="timer" class:urgent>
  {#if seconds > 0}
    Window closes in <strong class="mono">{formatRemaining(seconds)}</strong>
  {:else}
    Closing the window...
  {/if}
</span>

<style>
  .timer {
    font-size: 12px;
    color: var(--muted);
  }

  .timer.urgent {
    color: var(--warn);
  }

  .mono {
    font-variant-numeric: tabular-nums;
  }
</style>
