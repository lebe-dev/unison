<script lang="ts">
  import { daemon } from "./daemon.svelte";

  const reason = $derived(daemon.offlineReason);
</script>

<div class="connection">
  <span class="dot" class:online={daemon.connected} class:error={daemon.failed}></span>
  <span>
    {#if daemon.connected}
      Daemon {daemon.status?.version ?? ""}
    {:else if daemon.loading}
      Connecting...
    {:else}
      Daemon offline
    {/if}
  </span>
  {#if reason}
    <span class="reason">{reason}</span>
  {/if}
</div>
