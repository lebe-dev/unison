import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  NetworkStateSchema,
  PolicyAction,
  PolicyCellSchema,
  PolicyRowSchema,
  RelaySchema,
  RelayTransport,
  SettingsSchema,
  type Relay,
  type Settings,
} from "./api/gen/unison/local/v1/local_pb";
import {
  composeExport,
  composeHeader,
  MAX_VISIBLE_RECORDS,
  networkCondition,
  parseRecord,
  recordsFrom,
  relayTransportName,
  relayUrl,
  settingsLines,
  visibleRecords,
  type HeaderInput,
} from "./journal";

const megabyte = 1n << 20n;

/** settings returns the settings of the daemon. */
function settings(): Settings {
  const cell = (action: PolicyAction, megabytes: number) =>
    create(PolicyCellSchema, { action, maxSizeBytes: BigInt(megabytes) * megabyte });
  const row = (action: PolicyAction) =>
    create(PolicyRowSchema, {
      text: cell(action, 10),
      image: cell(PolicyAction.CONFIRM, 5),
      files: cell(PolicyAction.METADATA, 1),
      relay: true,
    });
  return create(SettingsSchema, {
    deviceName: "This Mac",
    requireConfirmation: true,
    requireFileConfirmation: false,
    blobMaxSizeBytes: 64n * megabyte,
    downloadsDir: "/Users/someone/Downloads/Unison",
    unmetered: row(PolicyAction.AUTO),
    metered: row(PolicyAction.CONFIRM),
    roaming: row(PolicyAction.METADATA),
    dataSaver: row(PolicyAction.REJECT),
    relayMaxSizeBytes: 3n * megabyte,
    historyTtlTextSeconds: 7n * 86400n,
    historyTtlImageSeconds: 3n * 86400n,
    historyTtlFilesSeconds: 86400n,
    sentryEnabled: true,
    stripTracking: false,
    journalEnabled: true,
  });
}

/** relay returns the relay settings of the daemon. */
function relay(): Relay {
  return create(RelaySchema, {
    url: "https://relay.example:8443",
    tokenSet: true,
    connected: true,
    transport: RelayTransport.QUIC,
    filesOnRequest: false,
  });
}

/** input returns the header of the example export. */
function input(patch: Partial<HeaderInput> = {}): HeaderInput {
  return {
    generated: new Date("2026-11-17T19:48:02.123Z"),
    appVersion: "1.4.0",
    daemonVersion: "1.4.0",
    os: "macOS 14.5",
    deviceName: "This Mac",
    deviceId: "3f2a",
    network: create(NetworkStateSchema, { online: true, wifi: true }),
    addresses: ["192.168.1.5:4400"],
    settings: settings(),
    relay: relay(),
    journalMarker: false,
    ...patch,
  };
}

describe("records", () => {
  it("parses a line of the journal", () => {
    const record = parseRecord("2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a");
    expect(record).toEqual({
      at: "2026-11-17T19:48:02.123Z",
      level: "DEBUG",
      message: "connected to the peer peer=3f2a",
      text: "2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a",
    });
  });

  it("ignores a line without a record", () => {
    expect(parseRecord("a broken line")).toBeNull();
    expect(parseRecord("2026-11-17T19:48:02Z INFO no milliseconds")).toBeNull();
  });

  it("orders the records by their time", () => {
    const records = recordsFrom([
      "2026-11-17T19:48:03.000Z INFO third",
      "a broken line",
      "2026-11-17T19:48:01.000Z WARN first",
      "2026-11-17T19:48:02.000Z DEBUG second",
    ]);
    expect(records.map((record) => record.message)).toEqual(["first", "second", "third"]);
  });

  it("hides the debug records at the info level", () => {
    const records = recordsFrom([
      "2026-11-17T19:48:01.000Z INFO first",
      "2026-11-17T19:48:02.000Z DEBUG detail",
      "2026-11-17T19:48:03.000Z ERROR third",
    ]);
    expect(visibleRecords(records, "info").map((record) => record.message)).toEqual([
      "third",
      "first",
    ]);
    expect(visibleRecords(records, "debug").map((record) => record.message)).toEqual([
      "third",
      "detail",
      "first",
    ]);
  });
});

describe("limited display", () => {
  it("keeps the newest records only", () => {
    const start = Date.parse("2026-11-17T19:48:00.000Z");
    const lines = Array.from(
      { length: MAX_VISIBLE_RECORDS + 20 },
      (_, index) => `${new Date(start + index).toISOString()} INFO record ${index}`,
    );
    const shown = visibleRecords(recordsFrom(lines), "debug");
    expect(shown).toHaveLength(MAX_VISIBLE_RECORDS);
    expect(shown[0].message).toBe(`record ${MAX_VISIBLE_RECORDS + 19}`);
    expect(shown.at(-1)?.message).toBe("record 20");
  });
});

describe("relay address", () => {
  it("drops the query and the credentials", () => {
    expect(relayUrl("https://user:pass@relay.example:8443?token=secret")).toBe(
      "https://relay.example:8443",
    );
    expect(relayUrl("user:pass@relay.example:8443")).toBe("relay.example:8443");
    expect(relayUrl("https://relay.example:8443/path?a=b")).toBe("https://relay.example:8443/path");
    expect(relayUrl("relay.example:443")).toBe("relay.example:443");
    expect(relayUrl("")).toBe("");
  });

  it("names the transport", () => {
    expect(relayTransportName(RelayTransport.QUIC)).toBe("quic");
    expect(relayTransportName(RelayTransport.WSS)).toBe("wss");
    expect(relayTransportName(RelayTransport.NONE)).toBe("none");
    expect(relayTransportName(RelayTransport.UNSPECIFIED)).toBe("none");
  });
});

describe("network condition", () => {
  it("names the condition of the daemon state", () => {
    expect(networkCondition(undefined)).toBe("offline");
    expect(networkCondition(create(NetworkStateSchema, {}))).toBe("offline");
    expect(networkCondition(create(NetworkStateSchema, { online: true }))).toBe("unmetered");
    expect(networkCondition(create(NetworkStateSchema, { online: true, metered: true }))).toBe(
      "metered",
    );
    expect(networkCondition(create(NetworkStateSchema, { online: true, roaming: true }))).toBe(
      "roaming",
    );
    expect(
      networkCondition(
        create(NetworkStateSchema, { online: true, roaming: true, metered: true, dataSaver: true }),
      ),
    ).toBe("data_saver");
  });
});

describe("header", () => {
  it("describes the app, the daemon and the system", () => {
    expect(composeHeader(input()).split("\n").slice(0, 7)).toEqual([
      "unison application log",
      "generated: 2026-11-17T19:48:02.123Z",
      "app: 1.4.0",
      "daemon: 1.4.0",
      "os: macOS 14.5",
      "device: This Mac (3f2a)",
      "network: online (unmetered)",
    ]);
  });

  it("marks a daemon that is offline and leaves out empty addresses", () => {
    const header = composeHeader(input({ daemonVersion: null, network: undefined, addresses: [] }));
    expect(header).toContain("daemon: offline");
    expect(header).toContain("network: offline");
    expect(header).not.toContain("addresses:");
  });

  it("lists both addresses", () => {
    expect(composeHeader(input({ addresses: ["10.0.0.2:4400", "[::1]:4400"] }))).toContain(
      "addresses: 10.0.0.2:4400, [::1]:4400",
    );
  });
});

describe("settings of the header", () => {
  it("carries every setting without the files of the user", () => {
    const lines = settingsLines(settings(), relay());
    const text = lines.join("\n");
    expect(text).toContain("  device_name: This Mac");
    expect(text).toContain("  require_confirmation: true");
    expect(text).toContain("  require_file_confirmation: false");
    expect(text).toContain("  blob_max_size_bytes: 67108864");
    expect(text).toContain("  downloads_dir: [path]");
    expect(text).toContain("  relay_max_size_bytes: 3145728");
    expect(text).toContain("  history_ttl_text_seconds: 604800");
    expect(text).toContain("  history_ttl_image_seconds: 259200");
    expect(text).toContain("  history_ttl_files_seconds: 86400");
    expect(text).toContain("  sentry_enabled: true");
    expect(text).toContain("  strip_tracking: false");
    expect(text).toContain("  journal_enabled: true");
    expect(text).toContain("  relay.url: https://relay.example:8443");
    expect(text).toContain("  relay.token_set: true");
    expect(text).toContain("  relay.connected: true");
    expect(text).toContain("  relay.transport: quic");
    expect(text).toContain("  relay.files_on_request: false");
    expect(text).not.toContain("/Users/someone");
    expect(text).not.toContain("Downloads");
  });

  it("lists every policy row", () => {
    const text = settingsLines(settings(), relay()).join("\n");
    expect(text).toContain("  policy.unmetered.text.action: auto");
    expect(text).toContain("  policy.unmetered.text.max_size_bytes: 10485760");
    expect(text).toContain("  policy.unmetered.image.action: confirm");
    expect(text).toContain("  policy.unmetered.files.action: metadata");
    expect(text).toContain("  policy.metered.relay: true");
    expect(text).toContain("  policy.roaming.text.action: metadata");
    expect(text).toContain("  policy.data_saver.text.action: reject");
    expect(text).toContain("  policy.data_saver.relay: true");
  });

  it("hides the credentials and the query of the relay address", () => {
    const text = settingsLines(
      settings(),
      create(RelaySchema, {
        url: "https://user:pass@relay.example:8443/path?token=secret",
        transport: RelayTransport.WSS,
      }),
    ).join("\n");
    expect(text).toContain("  relay.url: https://relay.example:8443/path");
    expect(text).not.toContain("user");
    expect(text).not.toContain("pass");
    expect(text).not.toContain("token=secret");
    expect(text).toContain("  relay.transport: wss");
  });

  it("reports a disabled journal", () => {
    const disabled = settings();
    disabled.journalEnabled = false;
    expect(settingsLines(disabled, relay()).join("\n")).toContain("  journal_enabled: false");
  });

  it("reports the marker while the settings do not know the journal", () => {
    const disabled = settings();
    disabled.journalEnabled = false;
    expect(settingsLines(disabled, relay(), true).join("\n")).toContain("  journal_enabled: true");
  });
});

describe("export", () => {
  it("puts the header before every record, oldest first", () => {
    const text = composeExport(input(), [
      "2026-11-17T19:48:03.000Z DEBUG third",
      "2026-11-17T19:48:01.000Z INFO first",
    ]);
    expect(text.split("\n").slice(-4)).toEqual([
      "---",
      "2026-11-17T19:48:01.000Z INFO first",
      "2026-11-17T19:48:03.000Z DEBUG third",
      "",
    ]);
    expect(text.endsWith("\n")).toBe(true);
  });

  it("keeps the header of an empty log", () => {
    expect(composeExport(input(), []).endsWith("\n---\n")).toBe(true);
  });

  it("carries more records than the display shows", () => {
    const start = Date.parse("2026-11-17T19:48:00.000Z");
    const lines = Array.from(
      { length: MAX_VISIBLE_RECORDS + 20 },
      (_, index) => `${new Date(start + index).toISOString()} INFO record ${index}`,
    );
    const text = composeExport(input(), lines);
    const body = text.split("---\n")[1].trimEnd().split("\n");
    expect(body).toHaveLength(MAX_VISIBLE_RECORDS + 20);
  });
});
