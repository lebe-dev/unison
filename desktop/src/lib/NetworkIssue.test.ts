import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import NetworkIssue from "./NetworkIssue.svelte";

describe("NetworkIssue", () => {
  it("shows the summary and hides the technical details until asked", async () => {
    render(NetworkIssue, {
      props: { summary: "Could not reach the other device.", hint: "Use one network.", details: "1.2.3.4:5: timeout" },
    });

    expect(screen.getByText("Could not reach the other device.")).toBeTruthy();
    expect(screen.getByText("Use one network.")).toBeTruthy();
    const details = document.querySelector("details");
    expect(details?.open).toBe(false);
    await fireEvent.click(screen.getByText("Details"));
    expect(details?.open).toBe(true);
    expect(screen.getByText("1.2.3.4:5: timeout")).toBeTruthy();
  });

  it("offers a dismiss button when it can be dismissed", async () => {
    const onDismiss = vi.fn();
    render(NetworkIssue, { props: { summary: "Gone.", details: "x", onDismiss } });

    await fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("has no details section without details", () => {
    render(NetworkIssue, { props: { summary: "Gone." } });

    expect(document.querySelector("details")).toBeNull();
  });
});
