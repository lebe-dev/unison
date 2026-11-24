<script lang="ts">
  import { client } from "../api/client";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { history } from "../history.svelte";
  import { shortDeviceID } from "../format";
  import { t, type MessageKey } from "../i18n";
  import { pickFolder } from "../files";
  import { loginItemStatus, setLoginItem, type LoginItem } from "../login";
  import { deviceNameKey } from "../platform";
  import { setSentryEnabled } from "../sentry";
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
  import Spinner from "../Spinner.svelte";
  import {
    lifetimeText,
    parseLifetime,
    ttlFromSettings,
    ttlKinds,
    ttlRequest,
    type HistoryTTL,
    type TTLKind,
  } from "../ttl";
  import JournalSettings from "./JournalSettings.svelte";
  import RelaySection from "./RelaySection.svelte";
  import { dashboard, DASHBOARD_LIMIT_OPTIONS, isDashboardLimit } from "../dashboard.svelte";
  import { theme } from "../theme.svelte";
  import Icon, { type IconName } from "../Icon.svelte";

  type TabID = "general" | "clipboard" | "files" | "network" | "privacy";

  const tabs: readonly { id: TabID; labelKey: MessageKey; icon: IconName }[] = [
    { id: "general", labelKey: "settings.tabGeneral", icon: "settings" },
    { id: "clipboard", labelKey: "settings.tabClipboard", icon: "clipboard" },
    { id: "files", labelKey: "settings.tabFiles", icon: "file" },
    { id: "network", labelKey: "settings.tabNetwork", icon: "devices" },
    { id: "privacy", labelKey: "settings.tabPrivacy", icon: "shield" },
  ];

  const pauses: { choice: PauseChoice; labelKey: MessageKey }[] = [
    { choice: "five_minutes", labelKey: "settings.pause5" },
    { choice: "one_hour", labelKey: "settings.pause1h" },
    { choice: "indefinite", labelKey: "settings.pauseIndefinite" },
    { choice: "off", labelKey: "common.resume" },
  ];

  let name = $state("");
  let downloadsDir = $state("");
  let requireConfirmation = $state(false);
  let requireFileConfirmation = $state(false);
  let allowRemoteSettings = $state(true);
  let sentryEnabled = $state(false);
  let stripTracking = $state(true);
  let policy = $state<PolicyTable | null>(null);
  let ttl = $state<HistoryTTL | null>(null);
  let ttlDrafts = $state<Record<TTLKind, string>>({ text: "", image: "", files: "" });
  let login = $state<LoginItem | null>(null);
  let loginLoaded = $state(false);
  let loaded = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);
  let clock = $state(Date.now());
  let page = $state<"index" | "journal">("index");
  let activeTab = $state<TabID>("general");

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

  const loginHint = $derived.by((): MessageKey => {
    switch (login?.status) {
      case "enabled":
        return "settings.loginHint.enabled";
      case "requires_approval":
        return "settings.loginHint.requiresApproval";
      case "not_registered":
        return "settings.loginHint.notRegistered";
      case "not_found":
        return "settings.loginHint.notFound";
      case "unsupported":
        return "settings.loginHint.unsupported";
      default:
        return "settings.loginHint.default";
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
      allowRemoteSettings = response.settings?.allowRemoteSettings ?? true;
      downloadsDir = response.settings?.downloadsDir ?? "";
      dashboard.downloadsDir = downloadsDir;
      sentryEnabled = response.settings?.sentryEnabled ?? false;
      stripTracking = response.settings?.stripTracking ?? true;
      policy = policyFromSettings(response.settings);
      showTTL(ttlFromSettings(response.settings));
      loaded = true;
      notice = null;
      void syncSentry(sentryEnabled);
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.loadFailed")) };
    } finally {
      busy = false;
    }
  }

  async function saveName(): Promise<void> {
    const value = name.trim();
    if (!value) {
      notice = { kind: "error", text: t("settings.nameEmpty") };
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.renameDevice({ deviceId: daemon.deviceID, name: value });
      name = response.device?.name ?? value;
      await daemon.refresh();
      notice = { kind: "info", text: t("settings.nameSaved") };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.nameFailed")) };
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
        text: errorText(error, t("settings.confirmationFailed")),
      };
    } finally {
      busy = false;
    }
  }

  /**
   * setRemoteSettings lets the paired devices change the confirmation flags of
   * this device. Refusing it here blocks them for every device.
   */
  async function setRemoteSettings(value: boolean): Promise<void> {
    const previous = allowRemoteSettings;
    allowRemoteSettings = value;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ allowRemoteSettings: value });
      allowRemoteSettings = response.settings?.allowRemoteSettings ?? value;
    } catch (error) {
      allowRemoteSettings = previous;
      notice = {
        kind: "error",
        text: errorText(error, t("settings.confirmationFailed")),
      };
    } finally {
      busy = false;
    }
  }

  /** syncSentry tells the shell of the app whether it may report its errors. */
  async function syncSentry(enabled: boolean): Promise<void> {
    try {
      await setSentryEnabled(enabled);
    } catch {
      // A shell that cannot take the setting keeps its own default.
    }
  }

  /** setSentryReporting stores the consent and passes it to the shell. */
  async function setSentryReporting(value: boolean): Promise<void> {
    const previous = sentryEnabled;
    sentryEnabled = value;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ sentryEnabled: value });
      sentryEnabled = response.settings?.sentryEnabled ?? value;
      await syncSentry(sentryEnabled);
    } catch (error) {
      sentryEnabled = previous;
      notice = {
        kind: "error",
        text: errorText(error, t("settings.diagnosticsFailed")),
      };
    } finally {
      busy = false;
    }
  }

  /** setStripTracking stores whether the links in the clipboard lose their tracking parameters. */
  async function setStripTracking(value: boolean): Promise<void> {
    const previous = stripTracking;
    stripTracking = value;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ stripTracking: value });
      stripTracking = response.settings?.stripTracking ?? value;
    } catch (error) {
      stripTracking = previous;
      notice = {
        kind: "error",
        text: errorText(error, t("settings.stripTrackingFailed")),
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
      dashboard.downloadsDir = downloadsDir;
      notice = { kind: "info", text: t("settings.folderSaved") };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.folderFailed")) };
    } finally {
      busy = false;
    }
  }

  async function chooseDownloadsDir(): Promise<void> {
    let path: string | null;
    try {
      path = await pickFolder();
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.dialogFailed")) };
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
        text: errorText(error, t("settings.policyFailed")),
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
      notice = { kind: "info", text: t("settings.policyReset") };
    } catch (error) {
      notice = {
        kind: "error",
        text: errorText(error, t("settings.policyResetFailed")),
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
      notice = { kind: "error", text: t("settings.badLifetime") };
      showTTL(ttl);
      return;
    }
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings(ttlRequest(kind, seconds));
      showTTL(ttlFromSettings(response.settings));
      notice = { kind: "info", text: t("settings.lifetimeSaved") };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.lifetimeFailed")) };
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
      notice = { kind: "info", text: t("settings.lifetimesReset") };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("settings.lifetimesResetFailed")) };
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
        text: errorText(error, t("settings.friendsFailed")),
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
      notice = { kind: "error", text: errorText(error, t("settings.loginFailed")) };
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

  function changeDashboardLimit(event: Event): void {
    const value = Number((event.currentTarget as HTMLSelectElement).value);
    if (isDashboardLimit(value)) {
      dashboard.setLimit(value);
    }
  }

  function selectTab(id: TabID): void {
    activeTab = id;
  }

  /** nextTab returns the tab the key moves to, or -1 for another key. */
  function nextTab(key: string, index: number): number {
    const last = tabs.length - 1;
    switch (key) {
      case "ArrowRight":
        return index === last ? 0 : index + 1;
      case "ArrowLeft":
        return index === 0 ? last : index - 1;
      case "Home":
        return 0;
      case "End":
        return last;
      default:
        return -1;
    }
  }

  function handleTabKey(event: KeyboardEvent, index: number): void {
    const next = nextTab(event.key, index);
    if (next < 0) {
      return;
    }
    event.preventDefault();
    activeTab = tabs[next].id;
    const list = (event.currentTarget as HTMLElement).parentElement;
    list?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  }
</script>

{#snippet appearance()}
  <section class="section">
    <h3 class="section-title">{t("settings.appearance")}</h3>
    <div class="list">
      <label class="form-row">
        <span class="label">{t("settings.theme")}</span>
        <select aria-label={t("settings.theme")} value={theme.mode} onchange={changeTheme}>
          <option value="system">{t("settings.themeAuto")}</option>
          <option value="light">{t("settings.themeLight")}</option>
          <option value="dark">{t("settings.themeDark")}</option>
        </select>
      </label>
    </div>
  </section>
{/snippet}

{#snippet dashboardSection()}
  <section class="section">
    <h3 class="section-title">{t("settings.dashboard")}</h3>
    <div class="list">
      <label class="form-row">
        <span class="label">
          {t("settings.dashboardItems")}
          <small>{t("settings.dashboardItemsHint")}</small>
        </span>
        <select
          aria-label={t("settings.dashboardItems")}
          value={dashboard.limit}
          onchange={changeDashboardLimit}
        >
          {#each DASHBOARD_LIMIT_OPTIONS as option (option)}
            <option value={option}>{option}</option>
          {/each}
        </select>
      </label>
    </div>
  </section>
{/snippet}

{#snippet startup()}
  <section class="section">
    <h3 class="section-title">{t("settings.startup")}</h3>
    <div class="list">
      <label class="toggle form-row">
        <input
          type="checkbox"
          checked={loginEnabled}
          disabled={busy || login === null || login.status === "unsupported"}
          onchange={(event) => void setLogin(event.currentTarget.checked)}
        />
        <span>
          {t("settings.startAtLogin")}
          <small>{t(loginHint)}</small>
        </span>
      </label>
    </div>
  </section>
{/snippet}

{#snippet applicationLog()}
  <section class="section">
    <div class="list">
      <button class="nav-row" type="button" onclick={() => (page = "journal")}>
        <span class="label">
          {t("settings.applicationLog")}
          <small>{t("settings.applicationLogHint")}</small>
        </span>
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <path
            d="m6 3 5 5-5 5"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>
  </section>
{/snippet}

<div class="settings">
  {#if page === "journal"}
    <JournalSettings onback={() => (page = "index")} />
  {:else}
    <div class="tabs" role="tablist" aria-label={t("settings.tabsAria")}>
      {#each tabs as tab, index (tab.id)}
        <button
          id="settings-tab-{tab.id}"
          class="tab"
          class:active={activeTab === tab.id}
          type="button"
          role="tab"
          aria-selected={activeTab === tab.id}
          aria-controls="settings-panel"
          tabindex={activeTab === tab.id ? 0 : -1}
          onclick={() => selectTab(tab.id)}
          onkeydown={(event) => handleTabKey(event, index)}
        >
          <Icon name={tab.icon} size={14} />
          <span>{t(tab.labelKey)}</span>
        </button>
      {/each}
    </div>

    {#if notice}
      <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
        {notice.text}
      </p>
    {/if}

    {#if !daemon.connected}
      <section class="state">
        {#if daemon.loading}
          <Spinner />
          <p class="muted">{t("devices.connecting")}</p>
        {:else}
          <p class="notice error">{t("settings.daemonNotRunning")}</p>
          <button class="button" type="button" onclick={() => void daemon.retry()}>
            {t("common.tryAgain")}
          </button>
        {/if}
      </section>
    {/if}

    <div
      id="settings-panel"
      class="panel"
      role="tabpanel"
      aria-labelledby="settings-tab-{activeTab}"
      tabindex="0"
    >
      {#if activeTab === "general"}
        {#if daemon.connected}
          <section class="section">
            <h3 class="section-title">
              <span class="grow">{t(deviceNameKey())}</span>
              <span class="mono count">{shortDeviceID(daemon.deviceID)}</span>
            </h3>
            <div class="list">
              <div class="form-row">
                <label class="label" for="device-name">{t("settings.deviceName")}</label>
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
                  {t("common.save")}
                </button>
              </div>
            </div>
            <p class="section-footer">{t("settings.nameFooter")}</p>
          </section>
        {/if}

        {@render appearance()}

        {@render dashboardSection()}

        {@render startup()}

        {@render applicationLog()}
      {:else if activeTab === "clipboard"}
        {#if daemon.connected}
          <section class="section">
            <h3 class="section-title">{t("common.clipboard")}</h3>
            {#if daemon.manualClipboard}
              <p class="notice warn">
                {t("settings.clipboardManual")}
                {#if daemon.clipboardNote}
                  <span class="note">{daemon.clipboardNote}</span>
                {/if}
              </p>
            {/if}
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
                      {t(option.labelKey)}
                    </button>
                  {/each}
                </div>
              </div>
              <label class="toggle form-row">
                <input
                  type="checkbox"
                  checked={requireConfirmation}
                  disabled={busy || !loaded}
                  onchange={(event) =>
                    void setConfirmation("clipboard", event.currentTarget.checked)}
                />
                <span>
                  {t("settings.askClipboard")}
                  <small>{t("settings.askClipboardHint")}</small>
                </span>
              </label>
              <label class="toggle form-row">
                <input
                  type="checkbox"
                  checked={stripTracking}
                  disabled={busy || !loaded}
                  onchange={(event) => void setStripTracking(event.currentTarget.checked)}
                />
                <span>
                  {t("settings.stripTracking")}
                  <small>{t("settings.stripTrackingHint")}</small>
                </span>
              </label>
            </div>
            {#if daemon.manualClipboard}
              <p class="section-footer">{t("settings.clipboardManualHint")}</p>
            {/if}
          </section>

          {#if ttl}
            <section class="section">
              <h3 class="section-title">
                <span class="grow">{t("nav.history")}</span>
                <button
                  class="button small"
                  type="button"
                  onclick={() => void resetTTL()}
                  disabled={busy}
                >
                  {t("settings.resetLifetimes")}
                </button>
              </h3>
              <div class="list">
                {#each ttlKinds as kind (kind.key)}
                  <label class="form-row">
                    <span class="label">{t(kind.label)}</span>
                    <input
                      class="short"
                      type="text"
                      value={ttlDrafts[kind.key]}
                      aria-label={t("settings.lifetimeAria", { kind: t(kind.label) })}
                      disabled={busy}
                      onchange={(event) => {
                        ttlDrafts = { ...ttlDrafts, [kind.key]: event.currentTarget.value };
                        void saveTTL(kind.key);
                      }}
                    />
                  </label>
                {/each}
              </div>
              <p class="section-footer">{t("settings.lifetimeFooter")}</p>
            </section>
          {/if}
        {/if}
      {:else if activeTab === "files"}
        {#if daemon.connected}
          <section class="section">
            <h3 class="section-title">{t("settings.receivedFiles")}</h3>
            <div class="list">
              <div class="form-row">
                <span class="label">
                  {t("settings.saveTo")}
                  <span class="mono path hint">{downloadsDir}</span>
                </span>
                <button
                  class="button"
                  type="button"
                  onclick={() => void saveDownloadsDir("")}
                  disabled={busy || !loaded}
                >
                  {t("settings.useDefaultFolder")}
                </button>
                <button
                  class="button"
                  type="button"
                  onclick={() => void chooseDownloadsDir()}
                  disabled={busy || !loaded}
                >
                  {t("settings.chooseFolder")}
                </button>
              </div>
              <label class="toggle form-row">
                <input
                  type="checkbox"
                  checked={requireFileConfirmation}
                  disabled={busy || !loaded}
                  onchange={(event) => void setConfirmation("files", event.currentTarget.checked)}
                />
                <span>
                  {t("settings.askFiles")}
                  <small>{t("settings.askFilesHint")}</small>
                </span>
              </label>
            </div>
            <p class="section-footer">{t("settings.receivedFooter")}</p>
          </section>

          {#if policy}
            {@const table = policy}
            <section class="section">
              <h3 class="section-title">
                <span class="grow">{t("settings.networkPolicy")}</span>
                <button
                  class="button small"
                  type="button"
                  onclick={() => void resetPolicy()}
                  disabled={busy}
                >
                  {t("settings.resetDefaults")}
                </button>
              </h3>
              {#each conditions as condition (condition.key)}
                {@const conditionLabel = t(condition.label)}
                <div class="list">
                  <label class="toggle form-row">
                    <input
                      type="checkbox"
                      aria-label={t("settings.conditionRelayAria", { condition: conditionLabel })}
                      checked={table[condition.key].relay}
                      disabled={busy}
                      onchange={(event) => setRelay(condition.key, event.currentTarget.checked)}
                    />
                    <span class="relay-label">{t("settings.relay")}</span>
                    <span>
                      <strong>{conditionLabel}</strong>
                      <small>{t(condition.hint)}</small>
                    </span>
                  </label>
                  {#each kinds as kind (kind.key)}
                    {@const cell = table[condition.key][kind.key]}
                    <div class="form-row">
                      <span class="label">{t(kind.label)}</span>
                      <select
                        value={cell.action}
                        aria-label={t("settings.kindActionAria", {
                          condition: conditionLabel,
                          kind: t(kind.label),
                        })}
                        disabled={busy}
                        onchange={(event) =>
                          setCell(condition.key, kind.key, {
                            action: Number(event.currentTarget.value),
                          })}
                      >
                        {#each actions as action (action.value)}
                          <option value={action.value}>{t(action.label)}</option>
                        {/each}
                      </select>
                      <input
                        class="short"
                        type="number"
                        min="1"
                        max="102400"
                        step="1"
                        aria-label={t("settings.kindLimitAria", {
                          condition: conditionLabel,
                          kind: t(kind.label),
                        })}
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
                    {t("settings.relayLimit")}
                    <small>{t("settings.relayLimitHint")}</small>
                  </span>
                  <input
                    class="short"
                    type="number"
                    min="1"
                    max="102400"
                    step="1"
                    aria-label={t("settings.relayLimitAria")}
                    value={megabytesOf(table.relayMaxSizeBytes)}
                    disabled={busy}
                    onchange={(event) => setRelayLimit(event.currentTarget.valueAsNumber)}
                  />
                  <span class="muted">MiB</span>
                </label>
              </div>
              <p class="section-footer">{t("settings.policyFooter")}</p>
            </section>
          {/if}
        {/if}
      {:else if activeTab === "network"}
        {#if daemon.connected}
          <section class="section">
            <h3 class="section-title">{t("devices.friends")}</h3>
            <div class="list">
              <label class="toggle form-row">
                <input
                  type="checkbox"
                  checked={friendsOn}
                  disabled={busy || !daemon.connected}
                  onchange={(event) => void setFriends(event.currentTarget.checked)}
                />
                <span>
                  {t("settings.findFriends")}
                  <small>
                    {#if friendsOn}
                      {t("settings.searchingFor", { time: friendsLeft })}
                    {:else}
                      {t("settings.searchingOff")}
                    {/if}
                  </small>
                </span>
              </label>
            </div>
          </section>

          <section class="section">
            <h3 class="section-title">{t("settings.pairedDevices")}</h3>
            <div class="list">
              <label class="toggle form-row">
                <input
                  type="checkbox"
                  aria-label={t("settings.allowRemote")}
                  checked={allowRemoteSettings}
                  disabled={busy || !loaded}
                  onchange={(event) => void setRemoteSettings(event.currentTarget.checked)}
                />
                <span>
                  {t("settings.allowRemote")}
                  <small>{t("settings.allowRemoteHint")}</small>
                </span>
              </label>
            </div>
          </section>

          <RelaySection />
        {/if}
      {:else if daemon.connected}
        <section class="section">
          <h3 class="section-title">{t("settings.diagnosticsSection")}</h3>
          {#if daemon.keystoreInFile}
            <p class="notice warn">{t("settings.keystoreFile")}</p>
          {/if}
          <div class="list">
            <label class="toggle form-row">
              <input
                type="checkbox"
                checked={sentryEnabled}
                disabled={busy || !loaded}
                onchange={(event) => void setSentryReporting(event.currentTarget.checked)}
              />
              <span>
                {t("settings.diagnostics")}
                <small>{t("settings.diagnosticsHint")}</small>
              </span>
            </label>
          </div>
        </section>
      {/if}
    </div>
  {/if}
</div>

<style>
  .settings {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 100%;
    max-width: 680px;
    margin: 0 auto;
  }

  .tabs {
    display: flex;
    gap: 2px;
    width: fit-content;
    padding: 2px;
    border-radius: 8px;
    background-color: var(--fill);
  }

  .tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 12px;
    border: 0;
    border-radius: 6px;
    color: var(--secondary);
    background: transparent;
    font: inherit;
    font-size: 13px;
    cursor: default;
  }

  .tab.active {
    color: var(--foreground);
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
  }

  .tab:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
    outline-offset: 0;
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 22px;
  }

  .name-input {
    width: 220px;
  }

  .path {
    overflow-wrap: anywhere;
  }

  .note {
    display: block;
    margin-top: 2px;
    font-size: 11px;
    color: var(--secondary);
  }

  .short {
    width: 80px;
  }

  .relay-label {
    font-size: 12px;
    color: var(--muted);
  }

  .nav-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 40px;
    padding: 10px 12px;
    border: 0;
    border-radius: 0;
    color: var(--text);
    background: transparent;
    font: inherit;
    text-align: left;
  }

  .nav-row:hover {
    background-color: var(--hover);
  }

  .nav-row svg {
    flex: none;
    color: var(--muted);
  }
</style>
