<script lang="ts">
  import { onDestroy } from "svelte";
  import type { PairingCandidate } from "../api/gen/unison/local/v1/local_pb";
  import { t } from "../i18n";
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

  function isRelayCandidate(candidate: PairingCandidate): boolean {
    return candidate.txt.transport === "relay" || candidate.addr.startsWith("relay:");
  }

  const hasLocalCandidates = $derived(candidates.some((candidate) => !isRelayCandidate(candidate)));
</script>

<section class="panel">
  <h3>{t("pairing.openDevicesHeading")}</h3>
  {#if quiet}
    <NetworkIssue summary={quiet.summary} hint={quiet.hint} details={quiet.details} />
  {:else if candidates.length === 0}
    <p class="muted">{t("pairing.noDevices")}</p>
  {:else}
    <ul>
      {#each candidates as candidate (candidate.addr)}
        <li>
          <span class="candidate-info">
            <span class="addr mono">{candidate.addr}</span>
            {#if isRelayCandidate(candidate)}
              <span class="badge">{t("pairing.viaRelay")}</span>
            {/if}
          </span>
          {#if dialing === candidate.addr}
            <span class="badge">{t("pairing.waiting")}</span>
          {:else}
            <button
              class="button small"
              type="button"
              onclick={() => onSelect(candidate.addr)}
              disabled={disabled || dialing !== null}
            >
              {t("pairing.compareCode")}
            </button>
          {/if}
        </li>
      {/each}
    </ul>
    {#if hasLocalCandidates}
      <p class="muted">{t("pairing.severalAddresses")}</p>
    {/if}
  {/if}
</section>

<style>
  ul {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border-radius: 8px;
    background-color: var(--surface-soft);
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 36px;
    padding: 4px 10px;
    box-sizing: border-box;
  }

  li + li {
    border-top: 1px solid var(--separator);
  }

  .candidate-info {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .addr {
    min-width: 0;
    overflow-wrap: anywhere;
    font-size: 0.8125rem;
  }
</style>
