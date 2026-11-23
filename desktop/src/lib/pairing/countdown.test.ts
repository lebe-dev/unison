import { describe, expect, it } from "vitest";
import { formatRemaining, groupDigits, remainingSeconds, sasEmoji } from "./countdown";

describe("remainingSeconds", () => {
  it("rounds up to the next full second", () => {
    expect(remainingSeconds(10_000, 7_100)).toBe(3);
    expect(remainingSeconds(10_000, 7_000)).toBe(3);
    expect(remainingSeconds(10_000, 9_001)).toBe(1);
  });

  it("stays at zero after the deadline", () => {
    expect(remainingSeconds(10_000, 10_000)).toBe(0);
    expect(remainingSeconds(10_000, 60_000)).toBe(0);
  });
});

describe("formatRemaining", () => {
  it("renders minutes and padded seconds", () => {
    expect(formatRemaining(180)).toBe("3:00");
    expect(formatRemaining(125)).toBe("2:05");
    expect(formatRemaining(9)).toBe("0:09");
    expect(formatRemaining(0)).toBe("0:00");
  });

  it("never renders negative time", () => {
    expect(formatRemaining(-5)).toBe("0:00");
  });
});

describe("groupDigits", () => {
  it("splits six digits into two groups", () => {
    expect(groupDigits("123456")).toBe("123 456");
  });

  it("keeps shorter and already grouped values readable", () => {
    expect(groupDigits("12345")).toBe("123 45");
    expect(groupDigits("123 456")).toBe("123 456");
    expect(groupDigits("")).toBe("");
  });
});

describe("sasEmoji", () => {
  it("keeps at most four symbols", () => {
    expect(sasEmoji(["a", "b", "c", "d", "e"])).toEqual(["a", "b", "c", "d"]);
    expect(sasEmoji([])).toEqual([]);
  });
});
