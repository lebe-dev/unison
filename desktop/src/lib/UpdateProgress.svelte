<script lang="ts">
  import { downloadPercent, formatBytes } from "./format";
  import { t, type MessageKey } from "./i18n";
  import Spinner from "./Spinner.svelte";
  import type { InstallPhase, UpdateStatus } from "./updater";

  let {
    status,
    detailed = false,
  }: {
    status: UpdateStatus;
    /** Shows detailed install phases and a full-width progress bar. */
    detailed?: boolean;
  } = $props();

  const PHASE_KEYS: Record<InstallPhase, MessageKey> = {
    preparing: "updates.phase.preparing",
    stopping_daemon: "updates.phase.stoppingDaemon",
    applying_update: "updates.phase.applyingUpdate",
    restarting_services: "updates.phase.restartingServices",
    relaunching: "updates.phase.relaunching",
  };
</script>

{#if status.kind === "downloading"}
  {@const pct = downloadPercent(status.downloaded, status.total)}
  <div class="update-progress" class:detailed role={detailed ? "status" : undefined}>
    <progress
      max={status.total || 1}
      value={status.downloaded}
      aria-label={t("updates.downloading", { percent: pct })}
    ></progress>
    <span>
      {t("updates.downloading", { percent: pct })}
      <span class="muted">
        {formatBytes(status.downloaded)} / {formatBytes(status.total)}
      </span>
    </span>
  </div>
{:else if status.kind === "verifying"}
  <div class="update-progress" class:detailed role={detailed ? "status" : undefined}>
    <Spinner size={14} />
    <span>{t("updates.verifying")}</span>
  </div>
{:else if status.kind === "installing"}
  <div class="update-progress" class:detailed role={detailed ? "status" : undefined}>
    <Spinner size={14} />
    <span>{detailed ? t(PHASE_KEYS[status.phase]) : t("updates.installing")}</span>
  </div>
{:else if status.kind === "restarting"}
  <div class="update-progress" class:detailed role={detailed ? "status" : undefined}>
    <Spinner size={14} />
    <span>{detailed ? t("updates.restarting") : t("updates.installing")}</span>
  </div>
{/if}

<style>
  .update-progress {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--muted);
  }

  .update-progress progress {
    width: 160px;
  }

  .update-progress.detailed {
    gap: 10px;
    margin: 0;
  }

  .update-progress.detailed progress {
    flex: 1;
    width: auto;
  }
</style>
