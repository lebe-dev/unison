<script lang="ts">
  import { client } from "../api/client";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { history } from "../history.svelte";
  import { shortDeviceID } from "../format";
  import { pickFolder } from "../files";
  import { loginItemStatus, setLoginItem, type LoginItem } from "../login";
  import {
    actions,
    bytesOf,
    conditions,
    kinds,
    megabytesOf,
    policyFromSettings,
    policyRequest,
    withCell,
    withRelay,
    type PolicyCell,
    type PolicyCondition,
    type PolicyKind,
    type PolicyTable,
  } from "../policy";
  import { PolicyAction } from "../api/gen/unison/local/v1/local_pb";
  import type { PauseChoice } from "../tray";
  import {
    lifetimeText,
    parseLifetime,
    ttlFromSettings,
    ttlKinds,
    ttlRequest,
    type HistoryTTL,
    type TTLKind,
  } from "../ttl";
  import RelaySection from "./RelaySection.svelte";
  import { theme } from "../theme.svelte";

  const pauses: { choice: PauseChoice; label: string }[] = [
    { choice: "five_minutes", label: "Pause for 5 minutes" },
    { choice: "one_hour", label: "Pause for 1 hour" },
    { choice: "indefinite", label: "Pause until I resume" },
    { choice: "off", label: "Resume" },
  ];

  let name = $state("");
  let downloadsDir = $state("");
  let requireConfirmation = $state(false);
  let requireFileConfirmation = $state(false);
  let policy = $state<PolicyTable | null>(null);
  let ttl = $state<HistoryTTL | null>(null);
  let ttlDrafts = $state<Record<TTLKind, string>>({ text: "", image: "", files: "" });
  let login = $state<LoginItem | null>(null);
  let loginLoaded = $state(false);
  let loaded = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);
  let clock = $state(Date.now());

  const loginEnabled = $derived(
    login?.status === "enabled" || login?.status === "requires_approval",
  );

  const friendsOn = $derived(daemon.status?.friendsDiscovery?.on ?? false);

  const friendsLeft = $derived.by(() => {
    const until = Number(daemon.status?.friendsDiscovery?.untilUnixMs ?? 0n);
    if (!until) {
      return "";
    }
    const left = Math.max(0, until - clock);
    const minutes = Math.floor(left / 60_000);
    const seconds = Math.floor((left % 60_000) / 1000);
    return `${minutes}:${String(seconds).padStart(2, "0")}`;
  });

  const loginHint = $derived.by(() => {
    switch (login?.status) {
      case "enabled":
        return "The background service starts when you log in.";
      case "requires_approval":
        return "Allow Unison in System Settings > General > Login Items.";
      case "not_registered":
        return "The background service starts only while the app asks for it.";
      case "not_found":
        return "The system does not know the background service of this build.";
      default:
        return "Automatic startup is available in the packaged app.";
    }
  });

  $effect(() => {
    if (daemon.connected && !loaded) {
      void load();
    }
  });

  $effect(() => {
    if (!loginLoaded) {
      loginLoaded = true;
      void loadLogin();
    }
  });

  $effect(() => {
    const timer = setInterval(() => {
      clock = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });

  async function loadLogin(): Promise<void> {
    try {
      login = await loginItemStatus();
    } catch {
      login = null;
    }
  }

  async function load(): Promise<void> {
    busy = true;
    try {
      const response = await client.getSettings({});
      name = response.settings?.deviceName ?? "";
      requireConfirmation = response.settings?.requireConfirmation ?? false;
      requireFileConfirmation = response.settings?.requireFileConfirmation ?? false;
      downloadsDir = response.settings?.downloadsDir ?? "";
      policy = policyFromSettings(response.settings);
      showTTL(ttlFromSettings(response.settings));
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
      const response = await client.renameDevice({ deviceId: daemon.deviceID, name: value });
      name = response.device?.name ?? value;
      await daemon.refresh();
      notice = { kind: "info", text: "The name is announced to the paired devices." };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The device name could not be saved.") };
    } finally {
      busy = false;
    }
  }

  /**
   * setConfirmation changes the default flag for devices paired from now on:
   * their clipboard items or file transfers wait for the user.
   */
  async function setConfirmation(scope: "clipboard" | "files", value: boolean): Promise<void> {
    const previous = { clipboard: requireConfirmation, files: requireFileConfirmation };
    if (scope === "clipboard") {
      requireConfirmation = value;
    } else {
      requireFileConfirmation = value;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings(
        scope === "clipboard" ? { requireConfirmation: value } : { requireFileConfirmation: value },
      );
      requireConfirmation = response.settings?.requireConfirmation ?? requireConfirmation;
      requireFileConfirmation =
        response.settings?.requireFileConfirmation ?? requireFileConfirmation;
    } catch (error) {
      requireConfirmation = previous.clipboard;
      requireFileConfirmation = previous.files;
      notice = {
        kind: "error",
        text: errorText(error, "The confirmation setting could not be changed."),
      };
    } finally {
      busy = false;
    }
  }

  /** Stores the folder of the received files; an empty path means the default one. */
  async function saveDownloadsDir(path: string): Promise<void> {
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ downloadsDir: path });
      downloadsDir = response.settings?.downloadsDir ?? path;
      history.downloadsDir = downloadsDir;
      notice = { kind: "info", text: "Files that arrive from now on are saved there." };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The folder could not be saved.") };
    } finally {
      busy = false;
    }
  }

  async function chooseDownloadsDir(): Promise<void> {
    let path: string | null;
    try {
      path = await pickFolder();
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The folder dialog could not be opened.") };
      return;
    }
    if (!path) {
      return;
    }
    await saveDownloadsDir(path);
  }

  async function setPause(choice: PauseChoice): Promise<void> {
    await daemon.setPause(choice);
    if (daemon.error) {
      notice = { kind: "error", text: daemon.error };
    }
  }

  async function savePolicy(next: PolicyTable): Promise<void> {
    const previous = policy;
    policy = next;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings(policyRequest(next));
      policy = policyFromSettings(response.settings) ?? next;
    } catch (error) {
      policy = previous;
      notice = {
        kind: "error",
        text: errorText(error, "The network policy could not be saved."),
      };
    } finally {
      busy = false;
    }
  }

  function setCell(condition: PolicyCondition, kind: PolicyKind, patch: Partial<PolicyCell>): void {
    if (!policy) {
      return;
    }
    const cell = policy[condition][kind];
    void savePolicy(withCell(policy, condition, kind, { ...cell, ...patch }));
  }

  function setSize(condition: PolicyCondition, kind: PolicyKind, megabytes: number): void {
    setCell(condition, kind, { maxSizeBytes: bytesOf(megabytes) });
  }

  function setRelay(condition: PolicyCondition, relay: boolean): void {
    if (!policy) {
      return;
    }
    void savePolicy(withRelay(policy, condition, relay));
  }

  function setRelayLimit(megabytes: number): void {
    if (!policy) {
      return;
    }
    void savePolicy({ ...policy, relayMaxSizeBytes: bytesOf(megabytes) });
  }

  async function resetPolicy(): Promise<void> {
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ resetPolicy: true });
      policy = policyFromSettings(response.settings);
      notice = { kind: "info", text: "The network policy is back to its defaults." };
    } catch (error) {
      notice = {
        kind: "error",
        text: errorText(error, "The network policy could not be reset."),
      };
    } finally {
      busy = false;
    }
  }

  /** Shows the lifetimes and fills the fields with them. */
  function showTTL(next: HistoryTTL | null): void {
    ttl = next;
    if (next) {
      ttlDrafts = {
        text: lifetimeText(next.text),
        image: lifetimeText(next.image),
        files: lifetimeText(next.files),
      };
    }
  }

  async function saveTTL(kind: TTLKind): Promise<void> {
    const seconds = parseLifetime(ttlDrafts[kind]);
    if (seconds === null) {
      notice = { kind: "error", text: "Use a lifetime like 24h or 7d, up to 365d." };
      showTTL(ttl);
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings(ttlRequest(kind, seconds));
      showTTL(ttlFromSettings(response.settings));
      notice = { kind: "info", text: "The lifetime applies to the next item you copy." };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The lifetime could not be saved.") };
      showTTL(ttl);
    } finally {
      busy = false;
    }
  }

  async function resetTTL(): Promise<void> {
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ resetHistoryTtl: true });
      showTTL(ttlFromSettings(response.settings));
      notice = { kind: "info", text: "The lifetimes are back to their defaults." };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The lifetimes could not be reset.") };
    } finally {
      busy = false;
    }
  }

  async function setFriends(on: boolean): Promise<void> {
    busy = true;
    notice = null;
    try {
      await client.setFriendsDiscovery({ on });
      await daemon.refresh();
    } catch (error) {
      notice = {
        kind: "error",
        text: errorText(error, "The search for friends could not be changed."),
      };
    } finally {
      busy = false;
    }
  }

  async function setLogin(enabled: boolean): Promise<void> {
    busy = true;
    notice = null;
    try {
      login = await setLoginItem(enabled);
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The login item could not be changed.") };
    } finally {
      busy = false;
    }
  }

  function changeTheme(event: Event): void {
    const value = (event.currentTarget as HTMLSelectElement).value;
    if (value === "system" || value === "light" || value === "dark") {
      theme.set(value);
    }
  }
</script>

{#snippet appearance()}
  <section class="section">
    <h3 class="section-title">Appearance</h3>
    <div class="list">
      <label class="form-row">
        <span class="label">Theme</span>
        <select aria-label="Theme" value={theme.mode} onchange={changeTheme}>
          <option value="system">Auto</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </label>
    </div>
  </section>
{/snippet}

{#snippet startup()}
  <section class="section">
    <h3 class="section-title">Startup</h3>
    <div class="list">
      <label class="toggle form-row">
        <input
          type="checkbox"
          checked={loginEnabled}
          disabled={busy || login === null || login.status === "unsupported"}
          onchange={(event) => void setLogin(event.currentTarget.checked)}
        />
        <span>
          Start Unison at login
          <small>{loginHint}</small>
        </span>
      </label>
    </div>
  </section>
{/snippet}

<div class="settings">
  {#if !daemon.connected}
    <section class="state">
      {#if daemon.loading}
        <p class="muted">Connecting to the daemon...</p>
      {:else}
        <p class="notice error">The daemon is not running, so the settings are unavailable.</p>
        <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button
        >
      {/if}
    </section>

    {@render appearance()}
    {@render startup()}
  {:else}
    {#if notice}
      <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
        {notice.text}
      </p>
    {/if}

    <section class="section">
      <h3 class="section-title">
        <span class="grow">This Mac</span>
        <span class="mono count">{shortDeviceID(daemon.deviceID)}</span>
      </h3>
      <div class="list">
        <div class="form-row">
          <label class="label" for="device-name">Device name</label>
          <input
            id="device-name"
            class="name-input"
            type="text"
            bind:value={name}
            maxlength="128"
            disabled={busy || !loaded}
            onkeydown={(event) => {
              if (event.key === "Enter") void saveName();
            }}
          />
          <button
            class="button"
            type="button"
            onclick={() => void saveName()}
            disabled={busy || !loaded}
          >
            Save
          </button>
        </div>
        <label class="toggle form-row">
          <input
            type="checkbox"
            checked={requireConfirmation}
            disabled={busy || !loaded}
            onchange={(event) => void setConfirmation("clipboard", event.currentTarget.checked)}
          />
          <span>
            Ask before applying the clipboard of new devices
            <small>
              Items wait in the history and a prompt asks whether to apply them. This is the default
              for devices you pair from now on; change it for a device on the Devices screen.
            </small>
          </span>
        </label>
        <label class="toggle form-row">
          <input
            type="checkbox"
            checked={requireFileConfirmation}
            disabled={busy || !loaded}
            onchange={(event) => void setConfirmation("files", event.currentTarget.checked)}
          />
          <span>
            Ask before accepting files from new devices
            <small>
              Nothing is downloaded until you accept the files in the prompt or on the Requests
              screen.
            </small>
          </span>
        </label>
      </div>
      <p class="section-footer">Peers see this name. They learn it the next time they connect.</p>
    </section>

    {@render appearance()}

    <section class="section">
      <h3 class="section-title">Received Files</h3>
      <div class="list">
        <div class="form-row">
          <span class="label">
            Save to
            <span class="mono path hint">{downloadsDir}</span>
          </span>
          <button
            class="button"
            type="button"
            onclick={() => void saveDownloadsDir("")}
            disabled={busy || !loaded}
          >
            Use default folder
          </button>
          <button
            class="button"
            type="button"
            onclick={() => void chooseDownloadsDir()}
            disabled={busy || !loaded}
          >
            Choose folder
          </button>
        </div>
      </div>
      <p class="section-footer">Files that other devices send are saved in this folder.</p>
    </section>

    <section class="section">
      <h3 class="section-title">Friends</h3>
      <div class="list">
        <label class="toggle form-row">
          <input
            type="checkbox"
            checked={friendsOn}
            disabled={busy || !daemon.connected}
            onchange={(event) => void setFriends(event.currentTarget.checked)}
          />
          <span>
            Find friends
            <small>
              {#if friendsOn}
                Searching for {friendsLeft}. Turn it on on the other device too; the search stops by
                itself.
              {:else}
                Both devices have to turn it on. The search stops by itself after 15 minutes.
              {/if}
            </small>
          </span>
        </label>
      </div>
    </section>

    <section class="section">
      <h3 class="section-title">Clipboard</h3>
      <div class="list">
        <div class="form-row">
          <span class="label">{daemon.pauseText}</span>
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
        </div>
      </div>
    </section>

    {#if ttl}
      <section class="section">
        <h3 class="section-title">
          <span class="grow">History</span>
          <button
            class="button small"
            type="button"
            onclick={() => void resetTTL()}
            disabled={busy}
          >
            Reset lifetimes
          </button>
        </h3>
        <div class="list">
          {#each ttlKinds as kind (kind.key)}
            <label class="form-row">
              <span class="label">{kind.label}</span>
              <input
                class="short"
                type="text"
                value={ttlDrafts[kind.key]}
                aria-label={`${kind.label} lifetime`}
                disabled={busy}
                onchange={(event) => {
                  ttlDrafts = { ...ttlDrafts, [kind.key]: event.currentTarget.value };
                  void saveTTL(kind.key);
                }}
              />
            </label>
          {/each}
        </div>
        <p class="section-footer">
          How long this device keeps the items it copies. The lifetime is set on the item when it is
          copied, so the items that already exist keep their own. Days and hours are accepted, for
          example 7d or 24h, up to 365d.
        </p>
      </section>
    {/if}

    {#if policy}
      {@const table = policy}
      <section class="section">
        <h3 class="section-title">
          <span class="grow">Network policy</span>
          <button
            class="button small"
            type="button"
            onclick={() => void resetPolicy()}
            disabled={busy}
          >
            Reset to defaults
          </button>
        </h3>
        {#each conditions as condition (condition.key)}
          <div class="list">
            <label class="toggle form-row">
              <input
                type="checkbox"
                aria-label={`${condition.label} relay`}
                checked={table[condition.key].relay}
                disabled={busy}
                onchange={(event) => setRelay(condition.key, event.currentTarget.checked)}
              />
              <span class="relay-label">Relay</span>
              <span>
                <strong>{condition.label}</strong>
                <small>{condition.hint}</small>
              </span>
            </label>
            {#each kinds as kind (kind.key)}
              {@const cell = table[condition.key][kind.key]}
              <div class="form-row">
                <span class="label">{kind.label}</span>
                <select
                  value={cell.action}
                  aria-label={`${condition.label} ${kind.label} action`}
                  disabled={busy}
                  onchange={(event) =>
                    setCell(condition.key, kind.key, {
                      action: Number(event.currentTarget.value),
                    })}
                >
                  {#each actions as action (action.value)}
                    <option value={action.value}>{action.label}</option>
                  {/each}
                </select>
                <input
                  class="short"
                  type="number"
                  min="1"
                  max="102400"
                  step="1"
                  aria-label={`${condition.label} ${kind.label} limit`}
                  value={megabytesOf(cell.maxSizeBytes)}
                  disabled={busy || cell.action !== PolicyAction.AUTO}
                  onchange={(event) =>
                    setSize(condition.key, kind.key, event.currentTarget.valueAsNumber)}
                />
                <span class="muted">MiB</span>
              </div>
            {/each}
          </div>
        {/each}
        <div class="list">
          <label class="form-row">
            <span class="label">
              Relay limit
              <small>Items larger than this are not fetched over the relay.</small>
            </span>
            <input
              class="short"
              type="number"
              min="1"
              max="102400"
              step="1"
              aria-label="Relay limit"
              value={megabytesOf(table.relayMaxSizeBytes)}
              disabled={busy}
              onchange={(event) => setRelayLimit(event.currentTarget.valueAsNumber)}
            />
            <span class="muted">MiB</span>
          </label>
        </div>
        <p class="section-footer">
          What arrives by itself on every kind of network. The limit applies to the items fetched
          without asking.
        </p>
      </section>
    {/if}

    {@render startup()}

    <RelaySection />
  {/if}
</div>

<style>
  .settings {
    display: flex;
    flex-direction: column;
    gap: 22px;
    width: 100%;
    max-width: 680px;
    margin: 0 auto;
  }

  .name-input {
    width: 220px;
  }

  .path {
    overflow-wrap: anywhere;
  }

  .short {
    width: 80px;
  }

  .relay-label {
    font-size: 12px;
    color: var(--muted);
  }
</style>
