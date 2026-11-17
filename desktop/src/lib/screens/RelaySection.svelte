<script lang="ts">
  import { client } from "../api/client";
  import type { Relay } from "../api/gen/unison/local/v1/local_pb";
  import { RelayTransport } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { t } from "../i18n";

  const REFRESH_MS = 5000;

  let relay = $state<Relay | null>(null);
  let url = $state("");
  let token = $state("");
  let filesOnRequest = $state(true);
  let loaded = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);

  const stateText = $derived(relayStateText(relay));
  const canSave = $derived(
    url.trim().length > 0 && (token.trim().length > 0 || relay?.tokenSet === true),
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
      apply(response.relay);
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

  function apply(value: Relay | undefined): void {
    relay = value ?? null;
    url = value?.url ?? "";
    filesOnRequest = value?.filesOnRequest ?? true;
    token = "";
  }

  async function save(): Promise<void> {
    const address = url.trim();
    if (!address) {
      notice = { kind: "error", text: t("relay.empty") };
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.setRelay({
        url: address,
        token: token.trim() || undefined,
        filesOnRequest,
      });
      apply(response.relay);
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

  async function turnOff(): Promise<void> {
    busy = true;
    notice = null;
    try {
      const response = await client.relayOff({});
      apply(response.relay);
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
        bind:value={url}
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
        bind:value={token}
        autocomplete="off"
        maxlength="512"
        placeholder={relay?.tokenSet ? t("relay.tokenStoredPlaceholder") : ""}
        disabled={busy || !loaded}
      />
    </label>
    <label class="toggle form-row">
      <input type="checkbox" bind:checked={filesOnRequest} disabled={busy || !loaded} />
      <span>
        {t("relay.filesOnRequest")}
        <small>{t("relay.filesOnRequestHint")}</small>
      </span>
    </label>
  </div>
  <div class="toolbar actions">
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
</style>
