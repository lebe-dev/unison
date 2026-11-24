<script lang="ts">
  import { formatBytes, formatDateTime } from "./format";
  import { t, type MessageKey } from "./i18n";
  import Markdown from "./Markdown.svelte";
  import Spinner from "./Spinner.svelte";
  import { updater } from "./updater.svelte";
  import type { InstallPhase } from "./updater";

  const release = $derived(updater.available);
  const status = $derived(updater.status);
  const locked = $derived(
    status.kind === "verifying" || status.kind === "installing" || status.kind === "restarting",
  );

  const PHASE_KEYS: Record<InstallPhase, MessageKey> = {
    preparing: "updates.phase.preparing",
    stopping_daemon: "updates.phase.stoppingDaemon",
    applying_update: "updates.phase.applyingUpdate",
    restarting_services: "updates.phase.restartingServices",
    relaunching: "updates.phase.relaunching",
  };

  /** percent returns the share of a download that is done. */
  function percent(downloaded: number, total: number): number {
    if (total <= 0) {
      return 0;
    }
    return Math.min(100, Math.round((downloaded / total) * 100));
  }

  /** published returns the publish date of a release, or an empty string. */
  function published(value: string): string {
    const time = Date.parse(value);
    return Number.isFinite(time) ? formatDateTime(time) : "";
  }

  /** retry runs the action that failed. */
  function retry(): void {
    if (!release) {
      void updater.checkNow();
      return;
    }
    if (!updater.supported) {
      void updater.checkNow();
      return;
    }
    void updater.install();
  }

  function handleKey(event: KeyboardEvent): void {
    if (!updater.dialogOpen || event.key !== "Escape") {
      return;
    }
    event.preventDefault();
    updater.closeDialog();
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if updater.dialogOpen && release}
  <div class="dialog-backdrop">
    <div
      class="dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="update-dialog-title"
      tabindex="-1"
    >
      <header class="dialog-header">
        <h2 id="update-dialog-title">{t("updates.dialogTitle")}</h2>
        <div class="versions">
          <span class="version-pill">{updater.currentVersion || t("updates.unknownVersion")}</span>
          <span aria-hidden="true">-&gt;</span>
          <span class="version-pill new">{release.version}</span>
          {#if release.prerelease}
            <span class="badge">{t("updates.beta")}</span>
          {/if}
        </div>
        <p class="meta">
          {#if published(release.publishedAt)}
            <span>{t("updates.published", { date: published(release.publishedAt) })}</span>
          {/if}
          <span>{formatBytes(release.assetSize)}</span>
        </p>
      </header>

      {#if !updater.supported}
        <p class="notice warn">
          {t("updates.unsupported", {
            reason: updater.unsupportedReason ?? t("updates.unsupportedUnknown"),
          })}
        </p>
      {/if}

      {#if status.kind === "error"}
        <div class="notice error" role="alert">
          <p>{t("updates.failed", { message: status.message })}</p>
          <button class="button small" type="button" onclick={retry}>
            {t("updates.retry")}
          </button>
        </div>
      {/if}

      <div class="changelog">
        {#if release.body.trim() === ""}
          <p class="muted">{t("updates.noNotes")}</p>
        {:else}
          <Markdown text={release.body} />
        {/if}
      </div>

      {#if status.kind === "downloading"}
        <div class="progress" role="status">
          <progress
            max={status.total || 1}
            value={status.downloaded}
            aria-label={t("updates.downloading", {
              percent: percent(status.downloaded, status.total),
            })}
          ></progress>
          <span>
            {t("updates.downloading", {
              percent: percent(status.downloaded, status.total),
            })}
            <span class="muted">
              {formatBytes(status.downloaded)} / {formatBytes(status.total)}
            </span>
          </span>
        </div>
      {:else if status.kind === "verifying"}
        <div class="progress" role="status">
          <Spinner size={14} />
          <span>{t("updates.verifying")}</span>
        </div>
      {:else if status.kind === "installing"}
        <div class="progress" role="status">
          <Spinner size={14} />
          <span>{t(PHASE_KEYS[status.phase])}</span>
        </div>
      {:else if status.kind === "restarting"}
        <div class="progress" role="status">
          <Spinner size={14} />
          <span>{t("updates.restarting")}</span>
        </div>
      {/if}

      <footer class="dialog-footer">
        {#if status.kind === "downloading"}
          <button class="button" type="button" onclick={() => void updater.cancel()}>
            {t("common.cancel")}
          </button>
        {:else if locked}
          <button class="button" type="button" disabled>
            <Spinner size={14} />
            {t("updates.installingButton")}
          </button>
        {:else}
          <button class="button" type="button" onclick={() => updater.closeDialog()}>
            {t("common.later")}
          </button>
          <button
            class="button primary"
            type="button"
            disabled={!updater.supported}
            onclick={() => void updater.install()}
          >
            {t("updates.installUpdate")}
          </button>
        {/if}
      </footer>
    </div>
  </div>
{/if}

<style>
  .dialog-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: rgb(0 0 0 / 35%);
  }

  .dialog {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: min(560px, 100%);
    max-height: min(680px, 100%);
    padding: 20px;
    border: 1px solid var(--border);
    border-radius: 14px;
    background: var(--surface);
    box-shadow: 0 20px 50px rgb(0 0 0 / 25%);
  }

  .dialog-header h2 {
    margin: 0 0 8px;
    font-size: 16px;
  }

  .versions {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  .version-pill {
    padding: 2px 8px;
    border-radius: 6px;
    background: var(--surface-2);
    font-family: var(--mono, ui-monospace, monospace);
    font-size: 12px;
  }

  .version-pill.new {
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
  }

  .badge {
    padding: 1px 6px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .meta {
    display: flex;
    gap: 12px;
    margin: 6px 0 0;
    font-size: 12px;
    color: var(--muted);
  }

  .changelog {
    flex: 1;
    min-height: 80px;
    overflow-y: auto;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--background);
  }

  .notice {
    margin: 0;
    padding: 8px 10px;
    border-radius: 8px;
    font-size: 12px;
  }

  .notice.warn {
    background: color-mix(in srgb, #d97706 12%, transparent);
  }

  .notice.error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    background: color-mix(in srgb, #dc2626 12%, transparent);
  }

  .notice p {
    margin: 0;
  }

  .progress {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    color: var(--muted);
  }

  .progress progress {
    flex: 1;
  }

  .dialog-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .muted {
    color: var(--muted);
  }
</style>
