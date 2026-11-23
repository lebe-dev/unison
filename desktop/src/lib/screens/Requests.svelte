<script lang="ts">
  import { onMount } from "svelte";
  import type { ShareRequest } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { filesCountText, formatBytes, formatDateTime, shortDeviceID } from "../format";
  import { t } from "../i18n";
  import { requests } from "../requests.svelte";
  import Spinner from "../Spinner.svelte";

  let now = $state(Date.now());

  onMount(() => {
    void requests.load();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  function fileSummary(request: ShareRequest): string {
    return filesCountText(request.files.length);
  }

  function timeLeft(request: ShareRequest): string {
    const left = Number(request.expiresAtUnixMs) - now;
    if (!request.expiresAtUnixMs) {
      return "";
    }
    if (left <= 0) {
      return t("requests.expiring");
    }
    const minutes = Math.ceil(left / 60_000);
    return minutes <= 1
      ? t("requests.expiresInLessThanMinute")
      : t("requests.expiresInMinutes", { count: minutes });
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <Spinner />
      <p class="muted">{t("devices.connecting")}</p>
    {:else}
      <p class="notice error">{t("requests.daemonNotRunning")}</p>
      <button class="button" type="button" onclick={() => void daemon.retry()}>
        {t("common.tryAgain")}
      </button>
    {/if}
  </section>
{:else}
  <div class="requests">
    <div class="toolbar intro">
      <p class="muted">{t("requests.intro")}</p>
      <button
        class="button ghost small"
        type="button"
        onclick={() => void requests.load()}
        disabled={requests.loading}
      >
        {t("common.refresh")}
      </button>
    </div>

    {#if requests.notice}
      <p
        class="notice"
        class:error={requests.notice.kind === "error"}
        class:info={requests.notice.kind === "info"}
      >
        {requests.notice.text}
      </p>
    {/if}

    {#if requests.error}
      <p class="notice error">{requests.error}</p>
    {/if}

    {#if requests.requests.length === 0}
      <p class="empty">
        {requests.loading ? t("requests.loading") : t("requests.empty")}
      </p>
    {:else}
      <ul class="list">
        {#each requests.requests as request (request.id)}
          <li class="request">
            <div class="head">
              <div class="identity">
                <strong>{request.deviceName || t("common.unnamedDevice")}</strong>
                <span class="mono muted">{shortDeviceID(request.deviceId)}</span>
                <span class="badge">{fileSummary(request)}</span>
                <span class="badge">{formatBytes(request.totalBytes)}</span>
              </div>
              <span class="muted">{timeLeft(request)}</span>
            </div>

            <ul class="files">
              {#each request.files as file (file.name)}
                <li>
                  <span class="mono">{file.name}</span>
                  <span class="muted">{formatBytes(file.size)}</span>
                </li>
              {/each}
            </ul>

            <p class="meta muted">
              {t("requests.arrived", { time: formatDateTime(request.createdAtUnixMs) })}
            </p>

            <div class="toolbar answer">
              <button
                class="button primary small"
                type="button"
                onclick={() => void requests.decide(request.id, true)}
                disabled={requests.answering.includes(request.id)}
              >
                {requests.answering.includes(request.id)
                  ? t("requests.answering")
                  : t("common.accept")}
              </button>
              <button
                class="button danger small"
                type="button"
                onclick={() => void requests.decide(request.id, false)}
                disabled={requests.answering.includes(request.id)}
              >
                {t("common.decline")}
              </button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

<style>
  .requests {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .intro {
    align-items: center;
  }

  .intro p {
    flex: 1 1 260px;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .identity strong {
    font-weight: 600;
  }

  .files {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 8px 10px;
    border-radius: 8px;
    list-style: none;
    background-color: var(--surface-soft);
  }

  .files li {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 8px;
    word-break: break-all;
  }

  .toolbar.answer {
    justify-content: flex-end;
  }
</style>
