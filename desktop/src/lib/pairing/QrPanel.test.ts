import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import QrPanel from "./QrPanel.svelte";

const URI = "unison://pair?v=1&id=aa&fp=bb&t=cc&a=192.0.2.5:5555&role=personal";

describe("QrPanel", () => {
  it("draws the code of the window", async () => {
    render(QrPanel, { props: { uri: URI } });

    await waitFor(() => {
      expect(document.querySelector('[aria-label="Pairing code"] svg')).not.toBeNull();
    });
    expect(screen.getByText(URI)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeTruthy();
  });

  it("asks for an open window without a code", () => {
    render(QrPanel, { props: { uri: "" } });

    expect(screen.getByText(/Open the pairing window/)).toBeTruthy();
    expect(document.querySelector('[aria-label="Pairing code"] svg')).toBeNull();
  });
});
