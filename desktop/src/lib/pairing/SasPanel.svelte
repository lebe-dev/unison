<script lang="ts">
  import Icon from "../Icon.svelte";
  import { PairingMethod } from "../api/gen/unison/local/v1/local_pb";
  import type { PairingRequest } from "../pairing.svelte";
  import { platformLabel, roleLabel } from "../format";
  import { t } from "../i18n";
  import Notice from "../Notice.svelte";
  import SasCode from "./SasCode.svelte";

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

  const scanned = $derived(request.method === PairingMethod.QR);
  const name = $derived(request.name || t("common.aDevice"));
</script>

<section class="sas" aria-live="polite">
  <div class="head">
    <span class="badge-icon"><Icon name="shield" size={28} stroke={1.2} /></span>
    <div class="head-text">
      <h3>
        {scanned ? t("pairing.scannedHeading") : t("pairing.compareCodeHeading")}
      </h3>
      <p class="lead">
        <strong>{name}</strong>
        <span class="muted">({platformLabel(request.platform)})</span>
        {t("pairing.wantsToPairPrefix")} <strong>{roleLabel(request.role)}</strong>.
      </p>
    </div>
  </div>
  {#if scanned}
    <p class="muted">{t("pairing.scannedHint")}</p>
  {:else}
    <p class="muted">{t("pairing.sameCodeHint")}</p>
    {#if request.sas?.digits}
      <SasCode
        layout="hero"
        digits={request.sas.digits}
        emoji={request.sas.emoji}
        ariaLabel={t("pairing.sasAria")}
      />
    {:else}
      <div class="code-wait" aria-label={t("pairing.sasAria")}>
        <p class="muted">{t("pairing.waitingCode")}</p>
      </div>
    {/if}
  {/if}

  {#if confirmed}
    <Notice kind="info" text={t("pairing.confirmedWait")} />
  {/if}

  <div class="toolbar end">
    <button class="button large" type="button" onclick={onReject} disabled={busy}>
      {t("pairing.reject")}
    </button>
    <button
      class="button primary large"
      type="button"
      onclick={onConfirm}
      disabled={busy || confirmed}
    >
      {scanned ? t("common.addDevice") : t("pairing.confirm")}
    </button>
  </div>
</section>

<style>
  .sas {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .badge-icon {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 52px;
    height: 52px;
    border-radius: 12px;
    color: #ffffff;
    background-color: var(--accent);
  }

  .head-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  h3 {
    font-size: 15px;
    font-weight: 700;
  }

  .lead {
    color: var(--secondary);
  }

  .code-wait {
    padding: 20px 16px;
    border-radius: 10px;
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
    text-align: center;
  }
</style>
