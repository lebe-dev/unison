<script lang="ts">
  import { formatBytes } from "./format";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import Spinner from "./Spinner.svelte";
  import { updater } from "./updater.svelte";

  const release = $derived(updater.available);

  /** percent returns the share of a download that is done. */
  function percent(downloaded: number, total: number): number {
    if (total <= 0) {
      return 0;
    }
    return Math.min(100, Math.round((downloaded / total) * 100));
  }
</script>

{#if updater.showBanner && release}
  <div class="update-banner" role="status">
    <span class="banner-icon"><Icon name="refresh" /></span>
    <div class="banner-text">
      <p class="banner-title">
        {t("updates.bannerTitle", { version: release.version })}
        {#if release.prerelease}
          <span class="badge">{t("updates.beta")}</span>
        {/if}
      </p>
      {#if updater.status.kind === "downloading"}
        <p class="banner-progress">
          <progress
            max={updater.status.total || 1}
            value={updater.status.downloaded}
            aria-label={t("updates.downloading", {
              percent: percent(updater.status.downloaded, updater.status.total),
            })}
          ></progress>
          <span>
            {t("updates.downloading", {
              percent: percent(updater.status.downloaded, updater.status.total),
            })}
          </span>
          <span class="muted">
            {formatBytes(updater.status.downloaded)} / {formatBytes(updater.status.total)}
          </span>
        </p>
      {:else if updater.busy}
        <p class="banner-progress">
          <Spinner size={14} />
          <span
            >{updater.status.kind === "verifying"
              ? t("updates.verifying")
              : t("updates.installing")}</span
          >
        </p>
      {/if}
    </div>
    <div class="banner-actions">
      {#if updater.busy}
        <button class="button" type="button" onclick={() => updater.openDialog()}>
          {t("updates.showDetails")}
        </button>
      {:else}
        <button class="button primary" type="button" onclick={() => updater.openDialog()}>
          {t("updates.update")}
        </button>
        <button class="button" type="button" onclick={() => void updater.dismiss()}>
          {t("common.later")}
        </button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .update-banner {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--surface);
  }

  .banner-icon {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
  }

  .banner-text {
    flex: 1;
    min-width: 0;
  }

  .banner-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 13px;
    font-weight: 600;
  }

  .banner-progress {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--muted);
  }

  .banner-progress progress {
    width: 160px;
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

  .banner-actions {
    display: flex;
    flex: none;
    gap: 8px;
  }

  .muted {
    color: var(--muted);
  }
</style>
