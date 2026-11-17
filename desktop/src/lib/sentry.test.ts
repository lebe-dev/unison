import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  GetSettingsResponseSchema,
  SettingsSchema,
  type GetSettingsResponse,
} from "./api/gen/unison/local/v1/local_pb";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  getSettings: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("./api/client", () => ({ client: { getSettings: mocks.getSettings } }));

import { SentrySync } from "./sentry";

/** settings returns the answer of the daemon with the given consent. */
function settings(sentryEnabled: boolean): GetSettingsResponse {
  return create(GetSettingsResponseSchema, {
    settings: create(SettingsSchema, { sentryEnabled }),
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.invoke.mockResolvedValue(undefined);
});

describe("sentry consent sync", () => {
  it("passes the stored consent to the shell once", async () => {
    mocks.getSettings.mockResolvedValue(settings(true));
    const sync = new SentrySync();

    await sync.sync();
    await sync.sync();

    expect(mocks.getSettings).toHaveBeenCalledTimes(1);
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
    expect(mocks.invoke).toHaveBeenCalledWith("set_sentry_enabled", { enabled: true });
  });

  it("passes a consent that is off to the shell", async () => {
    mocks.getSettings.mockResolvedValue(settings(false));
    const sync = new SentrySync();

    await sync.sync();

    expect(mocks.invoke).toHaveBeenCalledWith("set_sentry_enabled", { enabled: false });
  });

  it("asks again after a failed read", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    mocks.getSettings.mockRejectedValueOnce(new Error("the daemon is loading"));
    mocks.getSettings.mockResolvedValue(settings(true));
    const sync = new SentrySync();

    await sync.sync();
    await sync.sync();
    warn.mockRestore();

    expect(mocks.getSettings).toHaveBeenCalledTimes(2);
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
    expect(mocks.invoke).toHaveBeenCalledWith("set_sentry_enabled", { enabled: true });
  });

  it("keeps one read in flight", async () => {
    let answer: (value: GetSettingsResponse) => void = () => {};
    mocks.getSettings.mockReturnValue(
      new Promise<GetSettingsResponse>((resolve) => {
        answer = resolve;
      }),
    );
    const sync = new SentrySync();

    const first = sync.sync();
    const second = sync.sync();
    answer(settings(false));
    await Promise.all([first, second]);

    expect(mocks.getSettings).toHaveBeenCalledTimes(1);
  });
});
