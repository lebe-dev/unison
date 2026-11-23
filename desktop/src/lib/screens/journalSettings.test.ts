import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import {
  GetRelayResponseSchema,
  GetSettingsResponseSchema,
  GetStatusResponseSchema,
  NetworkStateSchema,
  PolicyCellSchema,
  PolicyRowSchema,
  RelaySchema,
  RelayTransport,
  SettingsSchema,
  UpdateSettingsResponseSchema,
  type Settings,
} from "../api/gen/unison/local/v1/local_pb";
import { client } from "../api/client";
import { daemon } from "../daemon.svelte";
import {
  copyText,
  journalEnabled,
  readAllLines,
  readLines,
  shareLog,
  shellInfo,
  writeLog,
} from "../journalIO";
import JournalSettings from "./JournalSettings.svelte";

vi.mock("../api/client", () => ({
  client: {
    getRelay: vi.fn(),
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
  },
}));

vi.mock("../journalIO", () => ({
  copyText: vi.fn(),
  journalEnabled: vi.fn(),
  readAllLines: vi.fn(),
  readLines: vi.fn(),
  recordFailure: vi.fn(),
  saveLog: vi.fn(),
  shareLog: vi.fn(),
  shellInfo: vi.fn(),
  writeLog: vi.fn(),
}));

const getRelay = vi.mocked(client.getRelay);
const getSettings = vi.mocked(client.getSettings);
const updateSettings = vi.mocked(client.updateSettings);
const readAllLinesMock = vi.mocked(readAllLines);
const journalEnabledMock = vi.mocked(journalEnabled);
const readLinesMock = vi.mocked(readLines);
const copyTextMock = vi.mocked(copyText);
const shareLogMock = vi.mocked(shareLog);
const shellInfoMock = vi.mocked(shellInfo);
const writeLogMock = vi.mocked(writeLog);

const DEBUG = "2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a";
const INFO = "2026-11-17T19:48:03.000Z INFO application log enabled";
const ERROR = "2026-11-17T19:48:04.000Z ERROR the daemon did not start";
const BACKUP = "2026-11-17T19:48:01.000Z WARN an older record";

/** settings returns the settings with the journal on. */
function settings(journalEnabled = true): Settings {
  const cell = create(PolicyCellSchema, { maxSizeBytes: 1024n });
  const row = create(PolicyRowSchema, { text: cell, image: cell, files: cell, relay: true });
  return create(SettingsSchema, {
    deviceName: "This Mac",
    downloadsDir: "/Users/someone/Downloads/Unison",
    unmetered: row,
    metered: row,
    roaming: row,
    dataSaver: row,
    journalEnabled,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  daemon.status = create(GetStatusResponseSchema, {
    running: true,
    deviceId: "self",
    deviceName: "This Mac",
    version: "1.4.0",
    network: create(NetworkStateSchema, { online: true }),
    addrs: ["192.168.1.5:4400"],
  });
  daemon.error = null;
  readLinesMock.mockResolvedValue([INFO, DEBUG, ERROR]);
  readAllLinesMock.mockResolvedValue([BACKUP, INFO, DEBUG, ERROR]);
  journalEnabledMock.mockResolvedValue(false);
  shellInfoMock.mockResolvedValue({ appVersion: "1.4.0", os: "macOS 14.5" });
  getRelay.mockResolvedValue(
    create(GetRelayResponseSchema, {
      relay: create(RelaySchema, {
        url: "https://user:pass@relay.example:8443?token=secret",
        tokenSet: true,
        connected: true,
        transport: RelayTransport.QUIC,
        filesOnRequest: false,
      }),
    }),
  );
  getSettings.mockResolvedValue(create(GetSettingsResponseSchema, { settings: settings(true) }));
  updateSettings.mockResolvedValue(
    create(UpdateSettingsResponseSchema, { settings: settings(true) }),
  );
});

afterEach(() => {
  daemon.status = null;
  setUserAgent(originalUserAgent);
});

describe("application log page", () => {
  it("turns the journal on", async () => {
    getSettings.mockResolvedValue(create(GetSettingsResponseSchema, { settings: settings(false) }));
    updateSettings.mockResolvedValue(
      create(UpdateSettingsResponseSchema, { settings: settings(true) }),
    );

    render(JournalSettings, { props: { onback: vi.fn() } });

    const toggle = screen.getByRole("checkbox", { name: /Record the application log/ });
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText(/no new records are written/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy to clipboard" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Share as a file" })).toBeTruthy();
    expect(await screen.findByText(INFO)).toBeTruthy();
    await waitFor(() => expect((toggle as HTMLInputElement).disabled).toBe(false));

    await fireEvent.click(toggle);

    expect(updateSettings).toHaveBeenCalledWith({ journalEnabled: true });
    expect(await screen.findByText(/The application log is on/)).toBeTruthy();
  });

  it("hides the debug records at the info level", async () => {
    render(JournalSettings, { props: { onback: vi.fn() } });

    expect(await screen.findByText(INFO)).toBeTruthy();
    expect(screen.queryByText(DEBUG)).toBeNull();
    expect(screen.getByText(ERROR)).toBeTruthy();

    await fireEvent.change(await screen.findByLabelText("Show"), { target: { value: "debug" } });

    expect(screen.getByText(DEBUG)).toBeTruthy();
  });

  it("copies every record whatever the page shows", async () => {
    copyTextMock.mockResolvedValue();
    render(JournalSettings, { props: { onback: vi.fn() } });

    await fireEvent.change(await screen.findByLabelText("Show"), { target: { value: "info" } });
    await fireEvent.click(screen.getByRole("button", { name: "Copy to clipboard" }));

    await waitFor(() => expect(copyTextMock).toHaveBeenCalled());
    expect(readAllLinesMock).toHaveBeenCalled();
    const text = copyTextMock.mock.calls[0][0];
    expect(text).toContain(`---\n${BACKUP}`);
    expect(text).toContain(DEBUG);
    expect(text).toContain(INFO);
    expect(text).toContain("unison application log");
    expect(text).toContain("downloads_dir: [path]");
    expect(text).toContain("relay.url: https://relay.example:8443");
    expect(text).not.toContain("token=secret");
    expect(text).not.toContain("/Users/someone");
  });

  it("shares a file with every record", async () => {
    writeLogMock.mockResolvedValue({ name: "unison-log-1.log", path: "/tmp/unison-log-1.log" });
    shareLogMock.mockResolvedValue();
    render(JournalSettings, { props: { onback: vi.fn() } });

    await fireEvent.click(await screen.findByRole("button", { name: "Share as a file" }));

    await waitFor(() => expect(shareLogMock).toHaveBeenCalled());
    expect(readAllLinesMock).toHaveBeenCalled();
    const text = writeLogMock.mock.calls[0][0];
    expect(text).toContain(BACKUP);
    expect(text).toContain(DEBUG);
  });

  it("keeps the app records while the daemon is offline", async () => {
    daemon.status = create(GetStatusResponseSchema, { running: false });

    render(JournalSettings, { props: { onback: vi.fn() } });

    const toggle = screen.getByRole("checkbox", { name: /Record the application log/ });
    expect((toggle as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText(/The daemon is offline/)).toBeTruthy();
    expect(screen.getByLabelText("Show")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy to clipboard" })).toBeTruthy();
    expect(await screen.findByText(INFO)).toBeTruthy();
    expect((toggle as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText(/no new records are written/)).toBeTruthy();
  });

  it("takes the toggle from the marker while the daemon is offline", async () => {
    daemon.status = create(GetStatusResponseSchema, { running: false });
    journalEnabledMock.mockResolvedValue(true);

    render(JournalSettings, { props: { onback: vi.fn() } });

    const toggle = screen.getByRole("checkbox", { name: /Record the application log/ });
    await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
    expect((toggle as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByText(/no new records are written/)).toBeNull();
    expect(await screen.findByText(INFO)).toBeTruthy();
  });

  it("copies the records while the daemon is offline", async () => {
    daemon.status = create(GetStatusResponseSchema, { running: false });
    journalEnabledMock.mockResolvedValue(true);
    copyTextMock.mockResolvedValue();

    render(JournalSettings, { props: { onback: vi.fn() } });

    await fireEvent.click(await screen.findByRole("button", { name: "Copy to clipboard" }));

    await waitFor(() => expect(copyTextMock).toHaveBeenCalled());
    const text = copyTextMock.mock.calls[0][0];
    expect(text).toContain(BACKUP);
    expect(text).toContain(INFO);
    expect(text).toContain("daemon: offline");
    expect(text).toContain("network: offline");
    expect(text).toContain("journal_enabled: true");
  });

  it("goes back to the settings", async () => {
    const onback = vi.fn();
    render(JournalSettings, { props: { onback } });

    await fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(onback).toHaveBeenCalled();
  });

  it("names the system of the platform when the shell does not answer", async () => {
    shellInfoMock.mockRejectedValue(new Error("the shell is unavailable"));
    copyTextMock.mockResolvedValue();
    setUserAgent(WINDOWS_UA);

    render(JournalSettings, { props: { onback: vi.fn() } });

    await fireEvent.click(await screen.findByRole("button", { name: "Copy to clipboard" }));

    await waitFor(() => expect(copyTextMock).toHaveBeenCalled());
    const text = copyTextMock.mock.calls[0][0];
    expect(text).toContain("os: Windows");
    expect(text).not.toContain("macOS");
  });
});

const WINDOWS_UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36";
const originalUserAgent = navigator.userAgent;

/** setUserAgent makes the platform helpers read the given user agent. */
function setUserAgent(userAgent: string): void {
  Object.defineProperty(navigator, "userAgent", { value: userAgent, configurable: true });
}
