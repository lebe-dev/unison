<script lang="ts">
  import type { PairingRequest } from "../pairing.svelte";
  import { groupDigits, sasEmoji } from "./countdown";
  import { platformLabel, roleLabel } from "../format";

  let {
    request,
    busy = false,
    confirmed = false,
    onConfirm,
    onReject,
  }: {
    /** Device that waits for the comparison of the code. */
    request: PairingRequest;
    /** A call of the local API is in flight. */
    busy?: boolean;
    /** The code was accepted on this device. */
    confirmed?: boolean;
    onConfirm: () => void;
    onReject: () => void;
  } = $props();

  const digits = $derived(request.sas ? groupDigits(request.sas.digits) : "");
  const emoji = $derived(sasEmoji(request.sas?.emoji ?? []));
  const name = $derived(request.name || "A device");
</script>

<section class="sas" aria-live="polite">
  <h3>Compare the code</h3>
  <p class="lead">
    <strong>{name}</strong>
    <span class="muted">({platformLabel(request.platform)})</span>
    wants to pair as <strong>{roleLabel(request.role)}</strong>.
  </p>
  <p class="muted">The same code must appear on both devices. Accept only when the code matches.</p>

  <div class="code" aria-label="Short authentication string">
    {#if digits}
      <div class="digits mono">{digits}</div>
      <div class="emoji">
        {#each emoji as symbol}
          <span>{symbol}</span>
        {/each}
      </div>
    {:else}
      <p class="muted">Waiting for the code...</p>
    {/if}
  </div>

  {#if confirmed}
    <p class="notice info">Confirmed. Waiting for the other device to confirm as well.</p>
  {/if}

  <div class="toolbar">
    <button class="button primary" type="button" onclick={onConfirm} disabled={busy || confirmed}>
      Confirm
    </button>
    <button class="button danger" type="button" onclick={onReject} disabled={busy}>
      Reject
    </button>
  </div>
</section>

<style>
  .sas {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  h3 {
    margin: 0;
    font-size: 0.9375rem;
    font-weight: 600;
  }

  .lead {
    margin: 0;
  }

  .code {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 1.25rem;
    border: 1px solid var(--border);
    border-radius: 12px;
    background-color: var(--surface-soft);
    text-align: center;
  }

  .digits {
    font-size: clamp(2.5rem, 8vw, 3.5rem);
    font-weight: 600;
    letter-spacing: 0.35rem;
  }

  .emoji {
    display: flex;
    justify-content: center;
    gap: 0.75rem;
    font-size: 2.5rem;
    line-height: 1.2;
  }
</style>
