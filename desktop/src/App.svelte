<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { daemon } from "./lib/daemon.svelte";
  import { eventKindLabel } from "./lib/events";
  import AddDevice from "./lib/screens/AddDevice.svelte";
  import Devices from "./lib/screens/Devices.svelte";
  import History from "./lib/screens/History.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import Transfers from "./lib/screens/Transfers.svelte";

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
    void daemon.start();
  });

  onDestroy(() => {
    daemon.stop();
  });

  function clock(value: bigint): string {
    return new Date(Number(value)).toLocaleTimeString();
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
        </button>
      {/each}
    </nav>
    <div class="connection">
      <span class="dot" class:online={daemon.connected}></span>
      <span>
        {#if daemon.connected}
          Daemon {daemon.status?.version ?? ""}
        {:else}
          Daemon offline
        {/if}
      </span>
    </div>
  </aside>

  <main>
    <header>
      <h2>{current.label}</h2>
      <div class="summary">
        <span>{daemon.status?.deviceName ?? "Unison"}</span>
        <span>{daemon.onlinePeers} online</span>
        <span class:paused={daemon.paused}>{daemon.pauseText}</span>
      </div>
    </header>

    {#if daemon.error}
      <p class="error">{daemon.error}</p>
    {/if}

    <div class="screen">
      {#if active === "devices"}
        <Devices />
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
