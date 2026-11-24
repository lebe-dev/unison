import { describe, expect, it } from "vitest";
import { parseMarkdown, parseInline, safeHref } from "./markdown";

describe("parseMarkdown", () => {
  it("reads headings with their level", () => {
    const blocks = parseMarkdown("## What is new\n\n### Fixes");
    expect(blocks[0]).toEqual({
      type: "heading",
      level: 2,
      children: [{ type: "text", text: "What is new" }],
    });
    expect(blocks[1]).toMatchObject({ type: "heading", level: 3 });
  });

  it("joins the lines of a paragraph", () => {
    const blocks = parseMarkdown("A first line\nsecond line.\n\nAnother one.");
    expect(blocks).toEqual([
      { type: "paragraph", children: [{ type: "text", text: "A first line second line." }] },
      { type: "paragraph", children: [{ type: "text", text: "Another one." }] },
    ]);
  });

  it("reads unordered and ordered lists", () => {
    const [unordered, ordered] = parseMarkdown("- one\n- two\n\n1. first\n2. second");
    expect(unordered).toEqual({
      type: "list",
      ordered: false,
      items: [[{ type: "text", text: "one" }], [{ type: "text", text: "two" }]],
    });
    expect(ordered).toMatchObject({ type: "list", ordered: true });
  });

  it("reads a fenced code block with its language", () => {
    const [block] = parseMarkdown("```bash\nunison --help\n```");
    expect(block).toEqual({ type: "codeBlock", language: "bash", text: "unison --help" });
  });

  it("keeps the text of an unfinished code block", () => {
    const [block] = parseMarkdown("```\nunfinished");
    expect(block).toEqual({ type: "codeBlock", language: "", text: "unfinished" });
  });
});

describe("parseInline", () => {
  it("reads inline code, strong and emphasis", () => {
    expect(parseInline("Run `just test` and **check** it")).toEqual([
      { type: "text", text: "Run " },
      { type: "code", text: "just test" },
      { type: "text", text: " and " },
      { type: "strong", children: [{ type: "text", text: "check" }] },
      { type: "text", text: " it" },
    ]);
    expect(parseInline("*important*")).toEqual([
      { type: "emphasis", children: [{ type: "text", text: "important" }] },
    ]);
  });

  it("reads a link over https", () => {
    expect(parseInline("[the docs](https://example.com/docs)")).toEqual([
      {
        type: "link",
        href: "https://example.com/docs",
        children: [{ type: "text", text: "the docs" }],
      },
    ]);
  });

  it("drops a link scheme that could run code", () => {
    expect(parseInline("[click](javascript:alert(1))")).toEqual([{ type: "text", text: "click" }]);
    expect(parseInline("[data](data:text/html,hello)")).toEqual([{ type: "text", text: "data" }]);
  });

  it("keeps HTML as text instead of markup", () => {
    const nodes = parseInline("<script>alert(1)</script>");
    expect(nodes).toEqual([{ type: "text", text: "<script>alert(1)</script>" }]);
  });
});

describe("safeHref", () => {
  it("accepts only http and https", () => {
    expect(safeHref("https://github.com")).toBe(true);
    expect(safeHref("http://example.com")).toBe(true);
    expect(safeHref("javascript:alert(1)")).toBe(false);
    expect(safeHref("data:text/html,hello")).toBe(false);
    expect(safeHref("file:///etc/passwd")).toBe(false);
    expect(safeHref("not a url")).toBe(false);
  });
});
