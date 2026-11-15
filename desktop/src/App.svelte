<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { daemon } from "./lib/daemon.svelte";
  import { eventKindLabel } from "./lib/events";
  import { pairing } from "./lib/pairing.svelte";
  import AddDevice from "./lib/screens/AddDevice.svelte";
  import Devices from "./lib/screens/Devices.svelte";
  import History from "./lib/screens/History.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import Transfers from "./lib/screens/Transfers.svelte";
  import { theme } from "./lib/theme.svelte";

  const sections = [
    { id: "devices", label: "Devices" },
    { id: "add", label: "Add device" },
    { id: "history", label: "History" },
    { id: "transfers", label: "Transfers" },
    { id: "settings", label: "Settings" },
  ] as const;

  type SectionID = (typeof sections)[number]["id"];

  let active = $state<SectionID>("devices");
  const current = $derived(sections.find((section) => section.id === active) ?? sections[0]);

  onMount(() => {
    pairing.start();
    void daemon.start();
  });

  onDestroy(() => {
    pairing.stop();
    daemon.stop();
  });

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
      <div class="connection">
        <span class="dot" class:online={daemon.connected}></span>
        <span>
          {#if daemon.connected}
            Daemon {daemon.status?.version ?? ""}
          {:else if daemon.loading}
            Connecting...
          {:else}
            Daemon offline
          {/if}
        </span>
      </div>
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

    {#if daemon.error}
      <p class="error">{daemon.error}</p>
    {/if}

    <div class="screen">
      {#if active === "devices"}
        <Devices onAdd={() => (active = "add")} />
      {:else if active === "add"}
        <AddDevice />
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
