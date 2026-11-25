<script lang="ts">
  import { formatBytes, formatDateTime } from "./format";
  import { t } from "./i18n";
  import Markdown from "./Markdown.svelte";
  import Notice from "./Notice.svelte";
  import Spinner from "./Spinner.svelte";
  import UpdateProgress from "./UpdateProgress.svelte";
  import { updater } from "./updater.svelte";

  const release = $derived(updater.available);
  const status = $derived(updater.status);
  const locked = $derived(
    status.kind === "verifying" || status.kind === "installing" || status.kind === "restarting",
  );

  /** published returns the publish date of a release, or an empty string. */
  function published(value: string): string {
    const time = Date.parse(value);
    return Number.isFinite(time) ? formatDateTime(time) : "";
  }

  /** retry runs the action that failed. */
  function retry(): void {
    if (!release || !updater.supported) {
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
            <span class="badge beta">{t("updates.beta")}</span>
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
        <Notice
          kind="warn"
          text={t("updates.unsupported", {
            reason: updater.unsupportedReason ?? t("updates.unsupportedUnknown"),
          })}
        />
      {/if}

      {#if status.kind === "error"}
        <div class="notice error retry-row" role="alert">
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

      <UpdateProgress {status} detailed />

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

  .retry-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .dialog-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
