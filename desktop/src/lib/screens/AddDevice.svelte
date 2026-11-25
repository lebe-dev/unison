<script lang="ts">
  import { Role } from "../api/gen/unison/local/v1/local_pb";
  import DaemonState from "../DaemonState.svelte";
  import Notice from "../Notice.svelte";
  import CandidateList from "../pairing/CandidateList.svelte";
  import QrPanel from "../pairing/QrPanel.svelte";
  import WindowTimer from "../pairing/WindowTimer.svelte";
  import NetworkIssue from "../NetworkIssue.svelte";
  import { daemon } from "../daemon.svelte";
  import { roleLabel } from "../format";
  import { t } from "../i18n";
  import { pairing } from "../pairing.svelte";

  const roles = [
    {
      role: Role.PERSONAL,
      titleKey: "devices.personal",
      descriptionKey: "add.personalDescription",
    },
    {
      role: Role.FRIENDS,
      titleKey: "devices.friends",
      descriptionKey: "add.friendsDescription",
    },
  ] as const;

  async function openWindow(): Promise<void> {
    await pairing.open();
  }
</script>

{#snippet pairingFeedback(dismissible: boolean)}
  {#if pairing.error}
    <Notice kind="error" text={pairing.error} />
  {/if}
  {#if pairing.result?.details}
    <NetworkIssue
      summary={pairing.result.message}
      hint={pairing.result.hint}
      details={pairing.result.details}
      onDismiss={dismissible ? () => (pairing.result = null) : undefined}
    />
  {:else if pairing.result}
    <Notice
      kind={pairing.result.paired ? "info" : "error"}
      text={pairing.result.message}
      onDismiss={dismissible ? () => (pairing.result = null) : undefined}
    />
  {/if}
{/snippet}

{#if !daemon.connected}
  <DaemonState message={t("add.daemonNotRunning")} />
{:else if pairing.window}
  <div class="window">
    <div class="panel-header">
      <span class="badge personal">{roleLabel(pairing.window.role)}</span>
      <WindowTimer expiresAt={pairing.window.expiresAt} />
    </div>
    <p class="muted">{t("add.windowOpenHint")}</p>
    <div class="toolbar">
      <button
        class="button"
        type="button"
        onclick={() => void pairing.extend()}
        disabled={pairing.busy}
      >
        {t("add.extend")}
      </button>
      <button
        class="button ghost"
        type="button"
        onclick={() => void pairing.close()}
        disabled={pairing.busy}
      >
        {t("add.closeWindow")}
      </button>
    </div>

    {@render pairingFeedback(true)}

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
      <legend>{t("add.roleLegend")}</legend>
      {#each roles as option (option.role)}
        <label class="role" class:selected={pairing.role === option.role}>
          <input type="radio" name="role" value={option.role} bind:group={pairing.role} />
          <span class="role-text">
            <strong>{t(option.titleKey)}</strong>
            <small>{t(option.descriptionKey)}</small>
          </span>
        </label>
      {/each}
    </fieldset>

    <p class="muted">{t("add.startHint")}</p>

    <div class="toolbar">
      <button class="button primary" type="button" onclick={openWindow} disabled={pairing.busy}>
        {t("add.openWindow")}
      </button>
    </div>

    {@render pairingFeedback(false)}

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
