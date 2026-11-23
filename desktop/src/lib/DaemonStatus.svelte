<script lang="ts">
  import { daemon } from "./daemon.svelte";
  import { t } from "./i18n";

  const reason = $derived(daemon.offlineReason);

  async function install(): Promise<void> {
    await daemon.install();
  }
</script>

<div class="connection">
  <span class="dot" class:online={daemon.connected} class:error={daemon.failed}></span>
  <span class="title">
    {#if daemon.connected}
      {t("daemon.status", { version: daemon.status?.version ?? "" })}
    {:else if daemon.loading}
      {t("daemon.connecting")}
    {:else}
      {t("daemon.offline")}
    {/if}
  </span>
  {#if daemon.connected}
    <span class="detail">
      {t("daemon.devicesOnline", {
        online: daemon.onlinePeers,
        total: daemon.devices.length,
      })}
    </span>
  {/if}
  {#if daemon.installMissing}
    <span class="reason">{t("daemon.installMissing")}</span>
  {:else if reason}
    <span class="reason">{reason}</span>
  {/if}
  {#if daemon.installable}
    <button
      class="button small action"
      type="button"
      disabled={daemon.installing}
      onclick={install}
    >
      {daemon.installing ? t("daemon.installing") : t("daemon.install")}
    </button>
  {/if}
  {#if daemon.installError}
    <span class="reason">{daemon.installError}</span>
  {/if}
</div>

<style>
  .detail {
    grid-column: 2;
    font-size: 11px;
    color: var(--muted);
  }

  .action {
    grid-column: 2;
    justify-self: start;
    margin-top: 2px;
  }
</style>
