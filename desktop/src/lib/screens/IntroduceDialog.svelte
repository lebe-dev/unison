<script lang="ts">
  import {
    MediatedPairState,
    Role,
    type Device,
    type MediatedPair,
  } from "../api/gen/unison/local/v1/local_pb";
  import { platformLabel, roleLabel } from "../format";
  import { t } from "../i18n";
  import { roleLimit } from "../mediated";
  import { mediated } from "../mediated.svelte";
  import Notice from "../Notice.svelte";
  import SasCode from "../pairing/SasCode.svelte";

  let {
    device,
    partners,
    initialPartnerID,
    initialSessionID,
    onClose,
  }: {
    device: Device;
    partners: Device[];
    initialPartnerID: string;
    initialSessionID: string | null;
    onClose: () => void;
  } = $props();

  let secondID = $state("");
  let introduceRole = $state<Role>(Role.PERSONAL);
  let sessionID = $state<string | null>(null);

  $effect(() => {
    secondID = initialPartnerID;
    sessionID = initialSessionID;
    const partner =
      partners.find((current) => current.deviceId === initialPartnerID) ?? partners[0];
    introduceRole = partner ? roleLimit(device.role, partner.role) : Role.PERSONAL;
  });

  const introducePartner = $derived(
    partners.find((current) => current.deviceId === secondID) ?? partners[0] ?? null,
  );
  const introduceLimit = $derived(
    introducePartner ? roleLimit(device.role, introducePartner.role) : Role.PERSONAL,
  );
  // The device that limits the role, so the hint names the right one.
  const introduceFriends = $derived(
    introducePartner
      ? ([device, introducePartner].find((current) => current.role !== Role.PERSONAL) ?? null)
      : null,
  );
  const exchange = $derived(
    sessionID ? (mediated.pairs.find((pair) => pair.sessionId === sessionID) ?? null) : null,
  );

  function choosePartner(deviceID: string): void {
    secondID = deviceID;
    const partner = partners.find((current) => current.deviceId === deviceID);
    if (partner) {
      introduceRole = roleLimit(device.role, partner.role);
    }
  }

  async function introduce(): Promise<void> {
    if (!introducePartner) {
      return;
    }
    const started = await mediated.startPair(
      device.deviceId,
      introducePartner.deviceId,
      introduceRole,
    );
    if (started) {
      sessionID = started;
    }
  }

  function exchangeNames(pair: MediatedPair): { a: string; b: string } {
    return {
      a: pair.peerA?.name || t("common.theDevice"),
      b: pair.peerB?.name || t("common.theDevice"),
    };
  }

  function exchangeState(pair: MediatedPair): string {
    switch (pair.state) {
      case MediatedPairState.WAITING:
        return t("mediated.waiting");
      case MediatedPairState.READY:
        return t("mediated.ready");
      case MediatedPairState.AWAITING_FINISH:
        return t("mediated.awaitingFinish");
      default:
        return t("mediated.ended");
    }
  }
</script>

<div class="panel introduce" role="dialog" aria-label={t("mediated.dialogAria")}>
  {#if sessionID}
    <h3>{t("mediated.progressAria")}</h3>
    {#if exchange}
      {@const names = exchangeNames(exchange)}
      <p class="muted">{t("mediated.heading", { a: names.a, b: names.b })}</p>
      <p>{exchangeState(exchange)}</p>
      {#if exchange.sas?.digits}
        <SasCode
          layout="panel"
          digits={exchange.sas.digits}
          emoji={exchange.sas.emoji}
          ariaLabel={t("mediated.sasAria")}
        />
      {/if}
      {#if exchange.alreadyPaired}
        <Notice kind="info" text={t("mediated.alreadyPaired")} />
      {/if}
      <div class="toolbar end">
        <button
          class="button"
          type="button"
          onclick={() => void mediated.cancel(exchange.sessionId)}
          disabled={mediated.answering.includes(exchange.sessionId)}
        >
          {t("mediated.cancel")}
        </button>
      </div>
    {:else}
      <p class="muted">{mediated.notice?.text ?? t("mediated.ended")}</p>
      <div class="toolbar end">
        <button class="button" type="button" onclick={onClose}>
          {t("common.close")}
        </button>
      </div>
    {/if}
  {:else}
    <h3>{t("mediated.introduce")}</h3>
    <p class="muted">{t("mediated.introduceHint")}</p>
    <div class="form-row">
      <label class="label" for="mediated-second">{t("mediated.secondDevice")}</label>
      <select
        id="mediated-second"
        value={secondID}
        onchange={(event) => choosePartner(event.currentTarget.value)}
      >
        {#each partners as partner (partner.deviceId)}
          <option value={partner.deviceId}>
            {partner.name || t("common.unnamedDevice")} · {platformLabel(partner.platform)}
          </option>
        {/each}
      </select>
    </div>
    <div class="form-row">
      <span class="label">
        {t("mediated.roleLegend")}
        {#if introduceLimit === Role.FRIENDS && introduceFriends}
          <small>
            {t("mediated.roleLocked", {
              name: introduceFriends.name || t("common.theDevice"),
            })}
          </small>
        {/if}
      </span>
      {#each [Role.PERSONAL, Role.FRIENDS] as value (value)}
        <label class="switch-cell">
          <input
            type="radio"
            name="mediated-role"
            {value}
            bind:group={introduceRole}
            disabled={value === Role.PERSONAL && introduceLimit === Role.FRIENDS}
          />
          {roleLabel(value)}
        </label>
      {/each}
    </div>
    <Notice notice={mediated.notice} />
    <div class="toolbar end">
      <button class="button" type="button" onclick={onClose}>
        {t("common.cancel")}
      </button>
      <button
        class="button primary"
        type="button"
        onclick={() => void introduce()}
        disabled={!introducePartner}
      >
        {t("mediated.start")}
      </button>
    </div>
  {/if}
</div>

<style>
  .introduce .muted {
    margin: 0;
    font-size: 12px;
  }

  .switch-cell {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--secondary);
  }

  .switch-cell + .switch-cell {
    margin-left: 10px;
  }
</style>
