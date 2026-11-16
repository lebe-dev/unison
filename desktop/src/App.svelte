<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import type { UnlistenFn } from "@tauri-apps/api/event";
  import { daemon } from "./lib/daemon.svelte";
  import DaemonStatus from "./lib/DaemonStatus.svelte";
  import { eventKindLabel } from "./lib/events";
  import { history } from "./lib/history.svelte";
  import { notifications } from "./lib/notifications.svelte";
  import { notify } from "./lib/notify";
  import { pairing } from "./lib/pairing.svelte";
  import { requests } from "./lib/requests.svelte";
  import AddDevice from "./lib/screens/AddDevice.svelte";
  import Devices from "./lib/screens/Devices.svelte";
  import History from "./lib/screens/History.svelte";
  import Requests from "./lib/screens/Requests.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import Transfers from "./lib/screens/Transfers.svelte";
  import { theme } from "./lib/theme.svelte";
  import { onTrayAction, type TrayAction } from "./lib/tray";
  import { transfers } from "./lib/transfers.svelte";

  const sections = [
    { id: "devices", label: "Devices" },
    { id: "add", label: "Add device" },
    { id: "requests", label: "Requests" },
    { id: "history", label: "History" },
    { id: "transfers", label: "Transfers" },
    { id: "settings", label: "Settings" },
  ] as const;

  type SectionID = (typeof sections)[number]["id"];

  let active = $state<SectionID>("devices");
  const current = $derived(sections.find((section) => section.id === active) ?? sections[0]);
  let unlistenTray: UnlistenFn | null = null;

  onMount(() => {
    pairing.start();
    history.start();
    transfers.start();
    requests.start();
    // A notification shows its buttons while the shell listens to them, so the
    // events of the daemon start after the subscription.
    void notifications.start().then(() => daemon.start());
    void onTrayAction(handleTrayAction).then((unlisten) => {
      unlistenTray = unlisten;
    });
  });

  onDestroy(() => {
    pairing.stop();
    history.stop();
    transfers.stop();
    requests.stop();
    notifications.stop();
    daemon.stop();
    void unlistenTray?.();
    unlistenTray = null;
  });

  function handleTrayAction(action: TrayAction): void {
    switch (action.action) {
      case "pause":
        void daemon.setPause(action.mode);
        break;
      case "copy_item":
        void history.copy(action.id).then(() => {
          const notice = history.notice;
          if (notice?.kind === "error") {
            void notify({ title: "Copy failed", body: notice.text });
          }
        });
        break;
      case "send_files":
        void transfers.send(action.paths, [action.deviceId]);
        break;
    }
  }

  function clock(value: bigint): string {
    return new Date(Number(value)).toLocaleTimeString();
  }

  function changeTheme(event: Event): void {
    const value = (event.currentTarget as HTMLSelectElement).value;
    if (value === "system" || value === "light" || value === "dark") {
      theme.set(value);
    }
  }
</script>

<div class="app">
  <aside class="sidebar">
    <h1>Unison</h1>
    <nav>
      {#each sections as section (section.id)}
        <button
          type="button"
          class:active={section.id === active}
          onclick={() => (active = section.id)}
        >
          {section.label}
          {#if section.id === "add" && pairing.request}
            <span class="badge confirm" aria-label="Pairing request waiting">!</span>
          {/if}
          {#if section.id === "requests" && requests.pending > 0}
            <span class="badge confirm" aria-label="Requests waiting">{requests.pending}</span>
          {/if}
        </button>
      {/each}
    </nav>
    <div class="sidebar-footer">
      <label class="theme-picker">
        Theme
        <select aria-label="Theme" value={theme.mode} onchange={changeTheme}>
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </label>
      <DaemonStatus />
    </div>
  </aside>

  <main>
    <header>
      <h2>{current.label}</h2>
      <div class="summary">
        <span>{daemon.deviceName}</span>
        <span>{daemon.onlinePeers} online</span>
        <span class:paused={daemon.paused}>{daemon.pauseText}</span>
      </div>
    </header>

    {#if daemon.connected && daemon.error}
      <p class="error">{daemon.error}</p>
    {/if}

    <div class="screen">
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
    </div>

    <section class="activity">
      <h3>Activity</h3>
      {#if daemon.activity.length === 0}
        <p class="muted">No events yet.</p>
      {:else}
        <ul>
          {#each daemon.activity as event}
            <li>
              <span class="kind">{eventKindLabel(event.kind)}</span>
              <span class="device">{event.name || "This device"}</span>
              <time>{clock(event.atUnixMs)}</time>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </main>
</div>
