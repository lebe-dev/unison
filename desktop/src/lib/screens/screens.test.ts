import { afterEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/svelte";
import { daemon } from "../daemon.svelte";
import AddDevice from "./AddDevice.svelte";
import Devices from "./Devices.svelte";
import Settings from "./Settings.svelte";

describe("screens without a daemon", () => {
  afterEach(() => {
    daemon.status = null;
    daemon.error = null;
  });

  it("shows the starting state of the devices screen", () => {
    render(Devices);

    expect(screen.getByText("Connecting to the daemon...")).toBeTruthy();
  });

  it("shows the starting state of the add device screen", () => {
    render(AddDevice);

    expect(screen.getByText("Connecting to the daemon...")).toBeTruthy();
  });

  it("shows the starting state of the settings screen", () => {
    render(Settings);

    expect(screen.getByText("Connecting to the daemon...")).toBeTruthy();
  });

  it("reports a daemon that does not run", () => {
    daemon.error = "connection refused";

    render(Devices);

    expect(screen.getByText(/The daemon is not running/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });
});
