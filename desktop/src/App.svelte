<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import type { UnlistenFn } from "@tauri-apps/api/event";
  import { daemon } from "./lib/daemon.svelte";
  import DaemonStatus from "./lib/DaemonStatus.svelte";
  import { eventKindLabel } from "./lib/events";
  import { history } from "./lib/history.svelte";
  import { openLocalNetworkSettings } from "./lib/localNetwork";
  import { isMac } from "./lib/platform";
  import { notifications } from "./lib/notifications.svelte";
  import { notify } from "./lib/notify";
  import { pairing } from "./lib/pairing.svelte";
  import { PromptHost } from "./lib/prompt/host";
  import { prompts } from "./lib/prompts.svelte";
  import { requests } from "./lib/requests.svelte";
  import { SentrySync } from "./lib/sentry";
  import AddDevice from "./lib/screens/AddDevice.svelte";
  import Devices from "./lib/screens/Devices.svelte";
  import History from "./lib/screens/History.svelte";
  import Requests from "./lib/screens/Requests.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import Transfers from "./lib/screens/Transfers.svelte";
  import Icon, { type IconName } from "./lib/Icon.svelte";
  import { getLocale, t, type MessageKey } from "./lib/i18n";
  import SasPanel from "./lib/pairing/SasPanel.svelte";
  import { onSharedFiles } from "./lib/share";
  import { onTrayAction, type TrayAction } from "./lib/tray";
  import { transfers } from "./lib/transfers.svelte";

  const sections = [
    { id: "devices", labelKey: "nav.devices", icon: "devices" },
    { id: "add", labelKey: "nav.addDevice", icon: "add" },
    { id: "requests", labelKey: "nav.requests", icon: "requests" },
    { id: "transfers", labelKey: "nav.transfers", icon: "transfers" },
    { id: "history", labelKey: "nav.history", icon: "history" },
    { id: "settings", labelKey: "nav.settings", icon: "settings" },
  ] as const satisfies readonly { id: string; labelKey: MessageKey; icon: IconName }[];

  type SectionID = (typeof sections)[number]["id"];

  const navigation = sections.filter((section) => section.id !== "settings");
  const settingsSection = sections[sections.length - 1];

  let active = $state<SectionID>("devices");
  const current = $derived(sections.find((section) => section.id === active) ?? sections[0]);
  let unlistenTray: UnlistenFn | null = null;
  let unlistenShare: UnlistenFn | null = null;
  // The prompt window asks the questions that wait for the user, so a held
  // item or an offer does not stay unnoticed while this window is hidden.
  const promptHost = new PromptHost((answer) => void prompts.answer(answer.id, answer.action));
  const sentry = new SentrySync();

  $effect(() => {
    promptHost.sync($state.snapshot(prompts.queue));
  });

  // The daemon keeps polling its status, so a failed read of the consent is
  // tried again without asking for the settings on every update.
  $effect(() => {
    if (daemon.connected) {
      void sentry.sync();
    }
  });

  onMount(() => {
    pairing.start();
    history.start();
    transfers.start();
    requests.start();
    prompts.start();
    void promptHost.start();
    // A notification shows its buttons while the shell listens to them, so the
    // events of the daemon start after the subscription.
    void notifications.start().then(() => daemon.start());
    void onTrayAction(handleTrayAction).then((unlisten) => {
      unlistenTray = unlisten;
    });
    void onSharedFiles(handleSharedFiles).then((unlisten) => {
      unlistenShare = unlisten;
    });
  });

  onDestroy(() => {
    pairing.stop();
    history.stop();
    transfers.stop();
    requests.stop();
    prompts.stop();
    promptHost.stop();
    notifications.stop();
    daemon.stop();
    void unlistenTray?.();
    unlistenTray = null;
    void unlistenShare?.();
    unlistenShare = null;
  });

  /** handleSharedFiles opens the offer of the files another app shared, so the
   * user picks the devices on the transfers screen. */
  function handleSharedFiles(paths: string[]): void {
    transfers.offer(paths);
    active = "transfers";
  }

  function handleTrayAction(action: TrayAction): void {
    switch (action.action) {
      case "pause":
        void daemon.setPause(action.mode);
        break;
      case "copy_item":
        void history.copy(action.id).then(() => {
          const notice = history.notice;
          if (notice?.kind === "error") {
            void notify({ title: t("app.copyFailedTitle"), body: notice.text });
          }
        });
        break;
      case "send_files":
        void transfers.send(action.paths, [action.deviceId]);
        break;
    }
  }

  function openSettings(): void {
    void openLocalNetworkSettings().catch((error: unknown) => {
      console.warn("cannot open the local network settings", error);
    });
  }

  function clock(value: bigint): string {
    return new Date(Number(value)).toLocaleTimeString(getLocale());
  }

  /** handleShortcut follows the shortcuts of the app: the meta key with a comma
   * opens the settings, and with a digit it opens a section of the sidebar. */
  function handleShortcut(event: KeyboardEvent): void {
    if (!event.metaKey || event.altKey || event.ctrlKey || event.shiftKey) {
      return;
    }
    if (event.key === ",") {
      event.preventDefault();
      active = "settings";
      return;
    }
    const index = Number(event.key) - 1;
    if (Number.isInteger(index) && index >= 0 && index < navigation.length) {
      event.preventDefault();
      active = navigation[index].id;
    }
  }

  function trailing(id: SectionID): number {
    if (id === "requests") {
      return requests.pending;
    }
    if (id === "devices") {
      return daemon.devices.length;
    }
    return 0;
  }
</script>

<svelte:window onkeydown={handleShortcut} />

<div class="app">
  <aside class="sidebar" data-tauri-drag-region>
    <nav aria-label={t("nav.sectionsAria")}>
      {#each navigation as section (section.id)}
        {@render navItem(section)}
      {/each}
    </nav>
    <div class="sidebar-footer">
      {@render navItem(settingsSection)}
      <DaemonStatus />
    </div>
  </aside>

  <main>
    <header class="window-toolbar" data-tauri-drag-region>
      <div class="titles">
        <h1>{t(current.labelKey)}</h1>
        {#if daemon.connected}
          <span class="subtitle">
            {daemon.deviceName} · {t("app.onlinePeers", { count: daemon.onlinePeers })} ·
            <span class:paused={daemon.paused}>{daemon.pauseText}</span>
          </span>
        {/if}
      </div>
    </header>

    <div class="content" class:bleed={active === "devices"}>
      {#if daemon.localNetworkBlocked}
        <div class="banner" role="alert">
          <p>{t("app.localNetworkBanner")}</p>
          {#if isMac()}
            <p>{t("app.localNetworkHint")}</p>
            <button class="button" type="button" onclick={openSettings}
              >{t("app.openSettings")}</button
            >
          {/if}
        </div>
      {/if}

      {#if daemon.manualClipboard}
        <div class="banner" role="status">
          <p>
            {t("app.manualClipboardBanner")}
            {#if daemon.clipboardNote}
              <span class="note">{daemon.clipboardNote}</span>
            {/if}
          </p>
        </div>
      {/if}

      {#if daemon.connected && daemon.error}
        <p class="error">{daemon.error}</p>
      {/if}

      {#if active === "devices"}
        <Devices onAdd={() => (active = "add")} />
      {:else if active === "add"}
        <AddDevice />
      {:else if active === "requests"}
        <Requests />
      {:else if active === "history"}
        <History />
      {:else if active === "transfers"}
        <Transfers />
      {:else}
        <Settings />
      {/if}

      {#if active !== "devices"}
        <details class="activity">
          <summary>{t("app.activity")}</summary>
          {#if daemon.activity.length === 0}
            <p class="muted">{t("app.noEvents")}</p>
          {:else}
            <ul class="list">
              {#each daemon.activity as event (event)}
                <li>
                  <span class="kind">{eventKindLabel(event.kind)}</span>
                  <span class="device">{event.name || t("common.thisDevice")}</span>
                  <time>{clock(event.atUnixMs)}</time>
                </li>
              {/each}
            </ul>
          {/if}
        </details>
      {/if}
    </div>
  </main>
</div>

{#if pairing.request}
  <div class="sheet-backdrop">
    <div class="sheet" role="dialog" aria-modal="true" aria-label={t("app.pairingRequestAria")}>
      <SasPanel
        request={pairing.request}
        busy={pairing.busy}
        confirmed={pairing.confirmed}
        onConfirm={() => void pairing.confirm()}
        onReject={() => void pairing.reject()}
      />
      {#if pairing.error}
        <p class="notice error">{pairing.error}</p>
      {/if}
    </div>
  </div>
{/if}

{#snippet navItem(section: (typeof sections)[number])}
  {@const count = trailing(section.id)}
  <button
    type="button"
    class="nav-item"
    class:active={section.id === active}
    aria-current={section.id === active ? "page" : undefined}
    onclick={() => (active = section.id)}
  >
    <Icon name={section.icon} />
    <span class="label">{t(section.labelKey)}</span>
    {#if section.id === "requests" && count > 0}
      <span class="pill" aria-label={t("nav.requestsWaitingAria")}>{count}</span>
    {:else if section.id === "add" && pairing.request}
      <span class="pill" aria-label={t("nav.pairingWaitingAria")}>1</span>
    {:else if count > 0}
      <span class="trailing">{count}</span>
    {/if}
  </button>
{/snippet}
