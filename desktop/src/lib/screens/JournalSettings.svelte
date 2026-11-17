<script lang="ts">
  import { onMount } from "svelte";
  import { create } from "@bufbuild/protobuf";
  import { client } from "../api/client";
  import {
    RelaySchema,
    SettingsSchema,
    type Relay,
    type Settings,
  } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { t } from "../i18n";
  import { composeExport, recordsFrom, visibleRecords, type DisplayLevel } from "../journal";
  import {
    copyText,
    journalEnabled,
    readAllLines,
    readLines,
    recordFailure,
    shareLog,
    shellInfo,
    writeLog,
    type ShellInfo,
  } from "../journalIO";

  let { onback }: { /** Returns to the list of the settings. */ onback: () => void } = $props();

  const REFRESH_MS = 2000;

  let enabled = $state(false);
  let settings = $state<Settings | undefined>(undefined);
  let relay = $state<Relay>(create(RelaySchema, { filesOnRequest: true }));
  let markerEnabled = $state(false);
  let info = $state<ShellInfo>({ appVersion: "", os: "macOS" });
  let level = $state<DisplayLevel>("info");
  let lines = $state<string[]>([]);
  let loaded = $state(false);
  let readFailed = $state(false);
  let busy = $state(false);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);

  const toggleDisabled = $derived(busy || !daemon.connected || !loaded);
  const records = $derived(recordsFrom(lines));
  const shown = $derived(visibleRecords(records, level));

  onMount(() => {
    void load();
    const timer = setInterval(() => void refreshLines(), REFRESH_MS);
    return () => clearInterval(timer);
  });

  $effect(() => {
    if (daemon.connected && !loaded) {
      void loadSettings();
      void loadRelay();
      void loadMarker();
    }
  });

  async function load(): Promise<void> {
    await Promise.allSettled([
      loadShellInfo(),
      loadSettings(),
      loadRelay(),
      loadMarker(),
      refreshLines(),
    ]);
  }

  async function loadShellInfo(): Promise<void> {
    try {
      info = await shellInfo();
    } catch {
      info = { appVersion: "", os: "macOS" };
    }
  }

  async function loadSettings(): Promise<void> {
    if (!daemon.connected) {
      return;
    }
    try {
      const response = await client.getSettings({});
      settings = response.settings;
      enabled = response.settings?.journalEnabled ?? false;
      loaded = settings !== undefined;
    } catch (error) {
      if (daemon.connected) {
        notice = { kind: "error", text: errorText(error, t("journal.loadFailed")) };
      }
    }
  }

  async function loadRelay(): Promise<void> {
    if (!daemon.connected) {
      return;
    }
    try {
      const response = await client.getRelay({});
      relay = response.relay ?? relay;
    } catch {
      // The header shows the defaults when the relay settings do not load.
    }
  }

  async function loadMarker(): Promise<void> {
    try {
      markerEnabled = await journalEnabled();
      if (!loaded) {
        enabled = markerEnabled;
      }
    } catch {
      // The marker check is an extra source of truth; the daemon settings win.
    }
  }

  async function refreshLines(): Promise<void> {
    try {
      lines = await readLines();
      readFailed = false;
    } catch {
      readFailed = true;
    }
  }

  /** setEnabled turns the journal on or off for the daemon and the shell. */
  async function setEnabled(value: boolean): Promise<void> {
    const previous = enabled;
    enabled = value;
    busy = true;
    notice = null;
    try {
      const response = await client.updateSettings({ journalEnabled: value });
      settings = response.settings ?? settings;
      enabled = response.settings?.journalEnabled ?? value;
      loaded = settings !== undefined;
      await refreshLines();
      await loadMarker();
      notice = { kind: "info", text: value ? t("journal.nowEnabled") : t("journal.nowDisabled") };
    } catch (error) {
      enabled = previous;
      notice = { kind: "error", text: errorText(error, t("journal.toggleFailed")) };
    } finally {
      busy = false;
    }
  }

  /** exportText returns the shared file, which always carries every record.
   * The header takes the defaults while the daemon does not answer. */
  function exportText(records: string[]): string {
    const current = settings ?? create(SettingsSchema, {});
    return composeExport(
      {
        generated: new Date(),
        appVersion: info.appVersion || "unknown",
        daemonVersion: daemon.connected ? (daemon.status?.version ?? null) : null,
        os: info.os || "macOS",
        deviceName: daemon.status?.deviceName || current.deviceName || "Unison",
        deviceId: daemon.status?.deviceId ?? "",
        network: daemon.status?.network,
        addresses: daemon.status?.addrs ?? [],
        settings: current,
        relay,
        journalMarker: markerEnabled,
      },
      records,
    );
  }

  async function copy(): Promise<void> {
    busy = true;
    notice = null;
    try {
      await copyText(exportText(await readAllLines()));
      notice = { kind: "info", text: t("journal.copied") };
    } catch (error) {
      recordFailure("the clipboard copy failed");
      notice = { kind: "error", text: errorText(error, t("journal.copyFailed")) };
    } finally {
      busy = false;
    }
  }

  async function share(): Promise<void> {
    busy = true;
    notice = null;
    try {
      const log = await writeLog(exportText(await readAllLines()));
      await shareLog(log);
    } catch (error) {
      recordFailure("the share of the log failed");
      notice = { kind: "error", text: errorText(error, t("journal.shareFailed")) };
    } finally {
      busy = false;
    }
  }
</script>

<div class="journal">
  <header class="page-header">
    <button class="back" type="button" aria-label={t("common.back")} onclick={onback}>
      <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
        <path
          d="M10 3 5 8l5 5"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
    </button>
    <h2>{t("settings.applicationLog")}</h2>
  </header>

  {#if !daemon.connected}
    <p class="notice info">{t("journal.daemonOffline")}</p>
  {/if}

  {#if notice}
    <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
      {notice.text}
    </p>
  {/if}

  {#if readFailed}
    <p class="notice error">{t("journal.readFailed")}</p>
  {/if}

  {#if !enabled}
    <p class="notice info">{t("journal.disabledInfo")}</p>
  {/if}

  <section class="section">
    <div class="list">
      <label class="toggle form-row">
        <input
          type="checkbox"
          checked={enabled}
          disabled={toggleDisabled}
          onchange={(event) => void setEnabled(event.currentTarget.checked)}
        />
        <span>
          {t("journal.enable")}
          <small>{t("journal.enableHint")}</small>
        </span>
      </label>

      <label class="form-row">
        <span class="label">{t("journal.displayLevel")}</span>
        <select
          aria-label={t("journal.displayLevel")}
          value={level}
          disabled={busy}
          onchange={(event) => {
            level = event.currentTarget.value as DisplayLevel;
          }}
        >
          <option value="info">{t("journal.levelInfo")}</option>
          <option value="debug">{t("journal.levelDebug")}</option>
        </select>
      </label>
      <div class="form-row">
        <div class="toolbar">
          <button class="button" type="button" onclick={() => void copy()} disabled={busy}>
            {t("journal.copy")}
          </button>
          <button class="button" type="button" onclick={() => void share()} disabled={busy}>
            {t("journal.share")}
          </button>
        </div>
      </div>
    </div>
    <p class="section-footer">{t("journal.footer")}</p>
  </section>

  <section class="section">
    <h3 class="section-title">{t("journal.records")}</h3>
    {#if shown.length === 0}
      <p class="muted">{t("journal.empty")}</p>
    {:else}
      <ul class="list records mono" aria-label={t("journal.records")}>
        {#each shown as record, index (index)}
          <li class:debug={record.level === "DEBUG"}>{record.text}</li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .journal {
    display: flex;
    flex-direction: column;
    gap: 18px;
    width: 100%;
  }

  .page-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 6px;
  }

  .page-header h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 600;
  }

  .back {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    color: var(--secondary);
    background: transparent;
  }

  .back:hover {
    background-color: var(--hover);
  }

  .records {
    max-height: 340px;
    overflow: auto;
    font-size: 11px;
  }

  .records li {
    display: block;
    padding: 6px 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .records li.debug {
    color: var(--muted);
  }
</style>
