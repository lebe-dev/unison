<script lang="ts">
  import { daemon } from "./daemon.svelte";

  const reason = $derived(daemon.offlineReason);
</script>

<div class="connection">
  <span class="dot" class:online={daemon.connected} class:error={daemon.failed}></span>
  <span class="title">
    {#if daemon.connected}
      Daemon {daemon.status?.version ?? ""}
    {:else if daemon.loading}
      Connecting...
    {:else}
      Daemon offline
    {/if}
  </span>
  {#if daemon.connected}
    <span class="detail">{daemon.onlinePeers} of {daemon.devices.length} devices online</span>
  {/if}
  {#if reason}
    <span class="reason">{reason}</span>
  {/if}
</div>

<style>
  .detail {
    grid-column: 2;
    font-size: 11px;
    color: var(--muted);
  }
</style>
