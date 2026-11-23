<script lang="ts">
  import { client } from "../api/client";
  import type { Relay, RelayCheck, TestRelayResponse } from "../api/gen/unison/local/v1/local_pb";
  import { RelayCheckFailure, RelayTransport } from "../api/gen/unison/local/v1/local_pb";
  import type { MessageKey } from "../i18n";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { t } from "../i18n";
  import { relayDraft } from "../relayDraft.svelte";

  const REFRESH_MS = 5000;

  let relay = $state<Relay | null>(null);
  let loaded = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);
  let checking = $state(false);
  let check = $state<TestRelayResponse | null>(null);

  const stateText = $derived(relayStateText(relay));
  const canSave = $derived(
    relayDraft.url.trim().length > 0 &&
      (relayDraft.token.trim().length > 0 || relay?.tokenSet === true),
  );
  const checkRows = $derived(
    check
      ? [
          { label: t("relay.checkQuic"), result: check.quic },
          { label: t("relay.checkWss"), result: check.websocket },
        ]
      : [],
  );
  const tokenHint = $derived(relay?.tokenSet ? t("relay.tokenStored") : t("relay.tokenHint"));

  $effect(() => {
    if (daemon.connected && !loaded) {
      void load();
    }
  });

  $effect(() => {
    if (!daemon.connected) {
      return;
    }
    const timer = setInterval(() => {
      void load(true);
    }, REFRESH_MS);
    return () => clearInterval(timer);
  });

  function relayStateText(value: Relay | null): string {
    if (!value?.url) {
      return t("relay.stateOff");
    }
    if (!value.connected) {
      return t("relay.stateWaiting");
    }
    switch (value.transport) {
      case RelayTransport.QUIC:
        return t("relay.stateQuic");
      case RelayTransport.WSS:
        return t("relay.stateWss");
      default:
        return t("relay.stateConnected");
    }
  }

  async function load(quiet = false): Promise<void> {
    if (!quiet) {
      busy = true;
      notice = null;
    }
    try {
      const response = await client.getRelay({});
      applyStatus(response.relay);
      relayDraft.fill(response.relay);
      loaded = true;
    } catch (error) {
      if (!quiet) {
        notice = {
          kind: "error",
          text: errorText(error, t("relay.loadFailed")),
        };
      }
    } finally {
      if (!quiet) {
        busy = false;
      }
    }
  }

  /** applyStatus takes the state of the relay without touching the form. */
  function applyStatus(value: Relay | undefined): void {
    relay = value ?? null;
  }

  async function save(): Promise<void> {
    const address = relayDraft.url.trim();
    if (!address) {
      notice = { kind: "error", text: t("relay.empty") };
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.setRelay({
        url: address,
        token: relayDraft.token.trim() || undefined,
        filesOnRequest: relayDraft.filesOnRequest,
      });
      applyStatus(response.relay);
      relayDraft.fill(response.relay, true);
      notice = {
        kind: "info",
        text: t("relay.saved"),
      };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("relay.saveFailed")) };
    } finally {
      busy = false;
    }
  }

  async function runCheck(): Promise<void> {
    checking = true;
    check = null;
    notice = null;
    try {
      check = await client.testRelay({
        url: relayDraft.url.trim(),
        token: relayDraft.token.trim() || undefined,
      });
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("relay.checkFailed")) };
    } finally {
      checking = false;
    }
  }

  function checkSummary(value: TestRelayResponse): string {
    const quic = value.quic?.ok === true;
    const wss = value.websocket?.ok === true;
    if (quic && wss) {
      return t("relay.checkBoth");
    }
    if (wss) {
      return t("relay.checkWssOnly");
    }
    if (quic) {
      return t("relay.checkQuicOnly");
    }
    return t("relay.checkNone");
  }

  const failureKeys: Partial<Record<RelayCheckFailure, MessageKey>> = {
    [RelayCheckFailure.ADDRESS]: "relay.failAddress",
    [RelayCheckFailure.TIMEOUT]: "relay.failTimeout",
    [RelayCheckFailure.REFUSED]: "relay.failRefused",
    [RelayCheckFailure.CERTIFICATE]: "relay.failCertificate",
    [RelayCheckFailure.NOT_RELAY]: "relay.failNotRelay",
    [RelayCheckFailure.UNAVAILABLE]: "relay.failUnavailable",
    [RelayCheckFailure.UNAUTHORIZED]: "relay.failUnauthorized",
    [RelayCheckFailure.QUOTA_EXCEEDED]: "relay.failQuota",
    [RelayCheckFailure.DATAGRAMS_TOO_SMALL]: "relay.failDatagrams",
  };

  function checkText(result: RelayCheck | undefined): string {
    if (result?.ok) {
      return t("relay.checkOk", { ms: String(result.latencyMs) });
    }
    const key = result ? failureKeys[result.failure] : undefined;
    return t(key ?? "relay.failOther");
  }

  async function turnOff(): Promise<void> {
    busy = true;
    notice = null;
    try {
      const response = await client.relayOff({});
      applyStatus(response.relay);
      relayDraft.fill(response.relay, true);
      notice = { kind: "info", text: t("relay.off") };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("relay.offFailed")) };
    } finally {
      busy = false;
    }
  }
</script>

<section class="section">
  <h3 class="section-title">{t("relay.heading")}</h3>
  {#if notice}
    <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
      {notice.text}
    </p>
  {/if}
  {#if relay?.protocolNewer}
    <p class="notice warn" role="alert">{t("relay.protocolNewer")}</p>
  {/if}
  <div class="list">
    <div class="form-row">
      <span class="label">{stateText}</span>
    </div>
    <label class="form-row">
      <span class="label">
        {t("relay.address")}
        <small>{t("relay.addressHint")}</small>
      </span>
      <input
        class="wide"
        type="text"
        bind:value={relayDraft.url}
        placeholder="relay.example.com:443"
        maxlength="2048"
        disabled={busy || !loaded}
      />
    </label>
    <label class="form-row">
      <span class="label">
        {t("relay.token")}
        <small>{tokenHint}</small>
      </span>
      <input
        class="wide"
        type="password"
        bind:value={relayDraft.token}
        autocomplete="off"
        maxlength="512"
        placeholder={relay?.tokenSet ? t("relay.tokenStoredPlaceholder") : ""}
        disabled={busy || !loaded}
      />
    </label>
    <label class="toggle form-row">
      <input type="checkbox" bind:checked={relayDraft.filesOnRequest} disabled={busy || !loaded} />
      <span>
        {t("relay.filesOnRequest")}
        <small>{t("relay.filesOnRequestHint")}</small>
      </span>
    </label>
  </div>
  {#if checking}
    <p class="notice info" role="status">{t("relay.checking")}</p>
  {:else if check}
    <div class="check" role="status">
      <p class="check-summary">{checkSummary(check)}</p>
      {#each checkRows as row (row.label)}
        <div class="check-row" class:ok={row.result?.ok} class:failed={!row.result?.ok}>
          <span class="check-transport">{row.label}</span>
          <span>
            {checkText(row.result)}
            {#if !row.result?.ok && row.result?.detail}
              <small class="check-detail">{row.result.detail}</small>
            {/if}
          </span>
        </div>
      {/each}
    </div>
  {/if}
  <div class="toolbar actions">
    <button
      class="button"
      type="button"
      onclick={() => void runCheck()}
      disabled={busy || checking || !loaded || !canSave}
    >
      {t("relay.check")}
    </button>
    <button
      class="button"
      type="button"
      onclick={() => void turnOff()}
      disabled={busy || !loaded || !relay?.url}
    >
      {t("relay.turnOff")}
    </button>
    <button
      class="button primary"
      type="button"
      onclick={() => void save()}
      disabled={busy || !loaded || !canSave}
    >
      {t("common.save")}
    </button>
  </div>
</section>

<style>
  .wide {
    width: 240px;
  }

  .actions {
    justify-content: flex-end;
  }

  .check {
    display: grid;
    gap: 6px;
    margin: 8px 0;
  }

  .check-summary {
    margin: 0;
  }

  .check-row {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: 8px;
  }

  .check-transport {
    color: var(--muted);
  }

  .check-row.ok span:last-child {
    color: var(--online-text);
  }

  .check-row.failed span:last-child {
    color: var(--danger);
  }

  .check-detail {
    display: block;
    color: var(--muted);
    word-break: break-word;
  }
</style>
