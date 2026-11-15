<script lang="ts">
  import { client } from "../api/client";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { shortDeviceID } from "../format";
  import type { PauseChoice } from "../tray";

  const pauses: { choice: PauseChoice; label: string }[] = [
    { choice: "five_minutes", label: "Pause for 5 minutes" },
    { choice: "one_hour", label: "Pause for 1 hour" },
    { choice: "indefinite", label: "Pause until I resume" },
    { choice: "off", label: "Resume" },
  ];

  let name = $state("");
  let requireConfirmation = $state(false);
  let loaded = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);

  $effect(() => {
    if (daemon.connected && !loaded) {
      void load();
    }
  });

  async function load(): Promise<void> {
    busy = true;
    try {
      const response = await client.getSettings({});
      name = response.settings?.deviceName ?? "";
      requireConfirmation = response.settings?.requireConfirmation ?? false;
      loaded = true;
      notice = null;
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The settings could not be loaded.") };
    } finally {
      busy = false;
    }
  }

  async function saveName(): Promise<void> {
    const value = name.trim();
    if (!value) {
      notice = { kind: "error", text: "The device name cannot be empty." };
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ deviceName: value });
      name = response.settings?.deviceName ?? value;
      notice = { kind: "info", text: "The name is announced the next time the daemon starts." };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The device name could not be saved.") };
    } finally {
      busy = false;
    }
  }

  async function setConfirmation(value: boolean): Promise<void> {
    const previous = requireConfirmation;
    requireConfirmation = value;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ requireConfirmation: value });
      requireConfirmation = response.settings?.requireConfirmation ?? value;
    } catch (error) {
      requireConfirmation = previous;
      notice = {
        kind: "error",
        text: errorText(error, "The confirmation flag could not be changed."),
      };
    } finally {
      busy = false;
    }
  }

  async function setPause(choice: PauseChoice): Promise<void> {
    await daemon.setPause(choice);
    if (daemon.error) {
      notice = { kind: "error", text: daemon.error };
    }
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">Connecting to the daemon...</p>
    {:else}
      <p class="notice error">The daemon is not running, so the settings are unavailable.</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button>
    {/if}
  </section>
{:else}
  <div class="settings">
    {#if notice}
      <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
        {notice.text}
      </p>
    {/if}

    <section class="panel">
      <h3>This device</h3>
      <p class="muted mono">{shortDeviceID(daemon.deviceID)}</p>
      <div class="inline">
        <label class="field grow">
          <span>Device name</span>
          <input
            type="text"
            bind:value={name}
            maxlength="128"
            disabled={busy || !loaded}
            onkeydown={(event) => {
              if (event.key === "Enter") void saveName();
            }}
          />
          <small>Peers see this name. It is announced at the next start.</small>
        </label>
        <button class="button primary" type="button" onclick={() => void saveName()} disabled={busy || !loaded}>
          Save
        </button>
      </div>

      <label class="toggle">
        <input
          type="checkbox"
          checked={requireConfirmation}
          disabled={busy || !loaded}
          onchange={(event) => void setConfirmation(event.currentTarget.checked)}
        />
        <span>
          Ask other devices to confirm items sent from this device
          <small>
            The receiving device keeps the item until the user applies it.
          </small>
        </span>
      </label>
    </section>

    <section class="panel">
      <h3>Clipboard</h3>
      <p class="muted">{daemon.pauseText}</p>
      <div class="toolbar">
        {#each pauses as option (option.choice)}
          <button
            class="button small"
            class:primary={option.choice === "off" && daemon.paused}
            type="button"
            onclick={() => void setPause(option.choice)}
          >
            {option.label}
          </button>
        {/each}
      </div>
    </section>

    <section class="panel">
      <h3>Startup</h3>
      <label class="toggle">
        <input type="checkbox" disabled />
        <span>
          Start Unison at login
          <small>The system login item is added by the installer.</small>
        </span>
      </label>
    </section>

    <p class="muted">
      The daemon does not expose the history lifetime, the downloads folder and the relay yet.
    </p>
  </div>
{/if}

<style>
  .settings {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .inline {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 0.5rem;
  }

  .grow {
    flex: 1 1 240px;
  }
</style>
