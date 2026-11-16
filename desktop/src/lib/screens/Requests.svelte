<script lang="ts">
  import { onMount } from "svelte";
  import type { ShareRequest } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { formatBytes, formatDateTime, shortDeviceID } from "../format";
  import { requests } from "../requests.svelte";

  let now = $state(Date.now());

  onMount(() => {
    void requests.load();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  function fileSummary(request: ShareRequest): string {
    return request.files.length === 1 ? "1 file" : `${request.files.length} files`;
  }

  function timeLeft(request: ShareRequest): string {
    const left = Number(request.expiresAtUnixMs) - now;
    if (!request.expiresAtUnixMs) {
      return "";
    }
    if (left <= 0) {
      return "The request is expiring.";
    }
    const minutes = Math.ceil(left / 60_000);
    return minutes <= 1 ? "Expires in less than a minute" : `Expires in ${minutes} min`;
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">Connecting to the daemon...</p>
    {:else}
      <p class="notice error">The daemon is not running, so the requests are unavailable.</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button>
    {/if}
  </section>
{:else}
  <div class="requests">
    <div class="toolbar">
      <p class="muted">
        Files that Friends offer, and files of the devices you ask about, wait here until you
        answer. Nothing is written to this device before you accept.
      </p>
      <button
        class="button ghost small"
        type="button"
        onclick={() => void requests.load()}
        disabled={requests.loading}
      >
        Refresh
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
        {requests.loading ? "Loading the requests..." : "No file waits for your answer."}
      </p>
    {:else}
      <ul>
        {#each requests.requests as request (request.id)}
          <li class="request">
            <div class="head">
              <div class="identity">
                <strong>{request.deviceName || "Unnamed device"}</strong>
                <span class="mono muted">{shortDeviceID(request.deviceId)}</span>
                <span class="badge">{fileSummary(request)}</span>
                <span class="badge">{formatBytes(request.totalBytes)}</span>
              </div>
              <span class="muted">{timeLeft(request)}</span>
            </div>

            <ul class="files">
              {#each request.files as file}
                <li>
                  <span class="mono">{file.name}</span>
                  <span class="muted">{formatBytes(file.size)}</span>
                </li>
              {/each}
            </ul>

            <p class="meta muted">Arrived {formatDateTime(request.createdAtUnixMs)}</p>

            <div class="toolbar">
              <button
                class="button primary small"
                type="button"
                onclick={() => void requests.decide(request.id, true)}
                disabled={requests.answering.includes(request.id)}
              >
                {requests.answering.includes(request.id) ? "Answering..." : "Accept"}
              </button>
              <button
                class="button danger small"
                type="button"
                onclick={() => void requests.decide(request.id, false)}
                disabled={requests.answering.includes(request.id)}
              >
                Decline
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
    gap: 0.75rem;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .toolbar {
    align-items: center;
  }

  .toolbar p {
    flex: 1 1 260px;
    margin: 0;
  }

  ul {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .request {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: 10px;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
  }

  .files li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    font-size: 0.8125rem;
    word-break: break-all;
  }

  .meta {
    margin: 0;
  }
</style>
