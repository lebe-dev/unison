<script lang="ts">
  import { onDestroy } from "svelte";
  import type { PairingCandidate } from "../api/gen/unison/local/v1/local_pb";
  import NetworkIssue from "../NetworkIssue.svelte";
  import { quietWindowIssue } from "../network";

  let {
    candidates,
    dialing = null,
    disabled = false,
    openedAt = 0,
    listenAddr = "",
    onSelect,
  }: {
    /** Devices that are open for pairing. */
    candidates: PairingCandidate[];
    /** Address of the candidate the exchange was started with. */
    dialing?: string | null;
    disabled?: boolean;
    /** Time the window was opened, unix milliseconds; zero turns the warning off. */
    openedAt?: number;
    /** Address the window listens on, for the technical details. */
    listenAddr?: string;
    onSelect: (addr: string) => void;
  } = $props();

  let now = $state(Date.now());
  const ticker = setInterval(() => (now = Date.now()), 1_000);
  onDestroy(() => clearInterval(ticker));

  const quiet = $derived(
    openedAt > 0 && candidates.length === 0 ? quietWindowIssue(now - openedAt, listenAddr) : null,
  );
</script>

<section class="panel">
  <h3>Devices open for pairing</h3>
  {#if quiet}
    <NetworkIssue summary={quiet.summary} hint={quiet.hint} details={quiet.details} />
  {:else if candidates.length === 0}
    <p class="muted">
      No devices yet. Open the pairing window on the other device and keep both on the same network.
    </p>
  {:else}
    <ul>
      {#each candidates as candidate (candidate.addr)}
        <li>
          <span class="addr mono">{candidate.addr}</span>
          {#if dialing === candidate.addr}
            <span class="badge">Waiting</span>
          {:else}
            <button
              class="button small"
              type="button"
              onclick={() => onSelect(candidate.addr)}
              disabled={disabled || dialing !== null}
            >
              Compare code
            </button>
          {/if}
        </li>
      {/each}
    </ul>
    <p class="muted">
      One device can appear under several addresses for IPv4 and IPv6.
    </p>
  {/if}
</section>

<style>
  ul {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.4rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .addr {
    min-width: 0;
    overflow-wrap: anywhere;
    font-size: 0.8125rem;
  }
</style>
