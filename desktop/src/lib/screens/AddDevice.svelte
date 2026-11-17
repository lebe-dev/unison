<script lang="ts">
  import { Role } from "../api/gen/unison/local/v1/local_pb";
  import CandidateList from "../pairing/CandidateList.svelte";
  import QrPanel from "../pairing/QrPanel.svelte";
  import WindowTimer from "../pairing/WindowTimer.svelte";
  import NetworkIssue from "../NetworkIssue.svelte";
  import { daemon } from "../daemon.svelte";
  import { roleLabel } from "../format";
  import { pairing } from "../pairing.svelte";

  const roles = [
    {
      role: Role.PERSONAL,
      title: "Personal",
      description: "Shares the clipboard and the history with this device.",
    },
    {
      role: Role.FRIENDS,
      title: "Friends",
      description: "Sends and receives items you share, with a confirmation on arrival.",
    },
  ];

  async function openWindow(): Promise<void> {
    await pairing.open();
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">Connecting to the daemon...</p>
    {:else}
      <p class="notice error">The daemon is not running, so pairing is unavailable.</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button>
    {/if}
  </section>
{:else if pairing.window}
  <div class="window">
    <div class="panel-header">
      <span class="badge personal">{roleLabel(pairing.window.role)}</span>
      <WindowTimer expiresAt={pairing.window.expiresAt} />
    </div>
    <p class="muted">
      Pairing window is open. The other device sees this one and compares the same code before both
      devices accept.
    </p>
    <div class="toolbar">
      <button
        class="button"
        type="button"
        onclick={() => void pairing.extend()}
        disabled={pairing.busy}
      >
        Extend for 3 minutes
      </button>
      <button
        class="button ghost"
        type="button"
        onclick={() => void pairing.close()}
        disabled={pairing.busy}
      >
        Close window
      </button>
    </div>

    {#if pairing.result?.details}
      <NetworkIssue
        summary={pairing.result.message}
        hint={pairing.result.hint}
        details={pairing.result.details}
        onDismiss={() => (pairing.result = null)}
      />
    {:else if pairing.result}
      <p class="notice" class:error={!pairing.result.paired} class:info={pairing.result.paired}>
        {pairing.result.message}
        <button class="button ghost small" type="button" onclick={() => (pairing.result = null)}>
          Dismiss
        </button>
      </p>
    {/if}

    {#if pairing.error}
      <p class="notice error">{pairing.error}</p>
    {/if}

    <CandidateList
      candidates={pairing.candidates}
      dialing={pairing.dialing}
      disabled={pairing.busy}
      openedAt={pairing.window.openedAt}
      listenAddr={pairing.window.addr}
      onSelect={(addr: string) => void pairing.pairWith(addr)}
    />

    {#if pairing.qr}
      <QrPanel uri={pairing.qr.uri} />
    {/if}
  </div>
{:else}
  <div class="start">
    <fieldset class="roles">
      <legend>Role of the device you add</legend>
      {#each roles as option (option.role)}
        <label class="role" class:selected={pairing.role === option.role}>
          <input type="radio" name="role" value={option.role} bind:group={pairing.role} />
          <span class="role-text">
            <strong>{option.title}</strong>
            <small>{option.description}</small>
          </span>
        </label>
      {/each}
    </fieldset>

    <p class="muted">
      Open the pairing window on both devices and compare the code. The window closes after three
      minutes and can be extended.
    </p>

    <div class="toolbar">
      <button class="button primary" type="button" onclick={openWindow} disabled={pairing.busy}>
        Open pairing window
      </button>
    </div>

    {#if pairing.error}
      <p class="notice error">{pairing.error}</p>
    {/if}

    {#if pairing.result?.details}
      <NetworkIssue
        summary={pairing.result.message}
        hint={pairing.result.hint}
        details={pairing.result.details}
      />
    {:else if pairing.result}
      <p class="notice" class:error={!pairing.result.paired} class:info={pairing.result.paired}>
        {pairing.result.message}
      </p>
    {/if}

    <QrPanel />
  </div>
{/if}

<style>
  .window,
  .start {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 100%;
    max-width: 640px;
    margin: 0 auto;
  }

  .roles {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin: 0;
    padding: 0;
    border: none;
  }

  legend {
    margin-bottom: 0.5rem;
    padding: 0;
    font-size: 13px;
    font-weight: 600;
  }

  .role {
    display: flex;
    flex: 1 1 220px;
    align-items: flex-start;
    gap: 0.6rem;
    padding: 12px;
    border-radius: 10px;
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
    cursor: default;
  }

  .role.selected {
    box-shadow:
      0 0 0 2px var(--accent),
      var(--box-shadow);
  }

  .role input {
    margin-top: 0.2rem;
    accent-color: var(--accent);
  }

  .role-text {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .role-text small {
    color: var(--muted);
    font-size: 0.8125rem;
  }
</style>
