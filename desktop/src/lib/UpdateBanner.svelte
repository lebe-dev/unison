<script lang="ts">
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import UpdateProgress from "./UpdateProgress.svelte";
  import { updater } from "./updater.svelte";

  const release = $derived(updater.available);
</script>

{#if updater.showBanner && release}
  <div class="update-banner" role="status">
    <span class="banner-icon"><Icon name="refresh" /></span>
    <div class="banner-text">
      <p class="banner-title">
        {t("updates.bannerTitle", { version: release.version })}
        {#if release.prerelease}
          <span class="badge beta">{t("updates.beta")}</span>
        {/if}
      </p>
      <UpdateProgress status={updater.status} />
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

  .banner-actions {
    display: flex;
    flex: none;
    gap: 8px;
  }
</style>
