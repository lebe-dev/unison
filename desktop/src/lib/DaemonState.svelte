<script lang="ts">
  import { daemon } from "./daemon.svelte";
  import { t } from "./i18n";
  import Spinner from "./Spinner.svelte";

  let { message }: { /** Message shown when the daemon is not running. */ message: string } =
    $props();
</script>

<section class="state">
  {#if daemon.loading}
    <Spinner />
    <p class="muted">{t("devices.connecting")}</p>
  {:else}
    <p class="notice error">{message}</p>
    <button class="button" type="button" onclick={() => void daemon.retry()}>
      {t("common.tryAgain")}
    </button>
  {/if}
</section>
