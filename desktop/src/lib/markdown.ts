/**
 * A small parser for the Markdown of a GitHub release. It keeps the subset a
 * changelog uses and returns typed nodes, so the window renders text and never
 * injects HTML.
 */

/** A piece of text inside a block. */
export type Inline =
  | { type: "text"; text: string }
  | { type: "strong"; children: Inline[] }
  | { type: "emphasis"; children: Inline[] }
  | { type: "code"; text: string }
  | { type: "link"; href: string; children: Inline[] };

/** A block of a changelog. */
export type Block =
  | { type: "heading"; level: number; children: Inline[] }
  | { type: "paragraph"; children: Inline[] }
  | { type: "list"; ordered: boolean; items: Inline[][] }
  | { type: "codeBlock"; language: string; text: string };

/** One item of a list, as far as it matters to the parser. */
interface ListItem {
  ordered: boolean;
  text: string;
}

/** parseMarkdown reads the blocks of a document. */
export function parseMarkdown(text: string): Block[] {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    if (line.trim() === "") {
      index += 1;
      continue;
    }

    const fence = /^```\s*(\S*)\s*$/.exec(line);
    if (fence) {
      const body: string[] = [];
      index += 1;
      while (index < lines.length && !/^```\s*$/.test(lines[index])) {
        body.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) {
        index += 1;
      }
      blocks.push({ type: "codeBlock", language: fence[1] ?? "", text: body.join("\n") });
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      blocks.push({
        type: "heading",
        level: heading[1].length,
        children: parseInline(heading[2].trim()),
      });
      index += 1;
      continue;
    }

    const item = listItem(line);
    if (item) {
      const items: Inline[][] = [];
      while (index < lines.length) {
        const current = listItem(lines[index]);
        if (!current || current.ordered !== item.ordered) {
          break;
        }
        items.push(parseInline(current.text));
        index += 1;
      }
      blocks.push({ type: "list", ordered: item.ordered, items });
      continue;
    }

    const paragraph: string[] = [];
    while (index < lines.length) {
      const current = lines[index];
      if (
        current.trim() === "" ||
        /^```/.test(current) ||
        /^#{1,6}\s/.test(current) ||
        listItem(current)
      ) {
        break;
      }
      paragraph.push(current.trim());
      index += 1;
    }
    blocks.push({ type: "paragraph", children: parseInline(paragraph.join(" ")) });
  }

  return blocks;
}

/** listItem reads one item of a list. */
function listItem(line: string): ListItem | null {
  const unordered = /^\s*[-*+]\s+(.*)$/.exec(line);
  if (unordered) {
    return { ordered: false, text: unordered[1] };
  }
  const ordered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
  if (ordered) {
    return { ordered: true, text: ordered[1] };
  }
  return null;
}

/** parseInline reads the inline nodes of one line. */
export function parseInline(text: string): Inline[] {
  const nodes: Inline[] = [];
  let plain = "";
  let index = 0;

  /** flush stores the text collected so far. */
  function flush(): void {
    if (plain !== "") {
      nodes.push({ type: "text", text: plain });
      plain = "";
    }
  }

  while (index < text.length) {
    const rest = text.slice(index);

    if (rest.startsWith("`")) {
      const end = rest.indexOf("`", 1);
      if (end > 1) {
        flush();
        nodes.push({ type: "code", text: rest.slice(1, end) });
        index += end + 1;
        continue;
      }
    }

    if (rest.startsWith("**")) {
      const end = rest.indexOf("**", 2);
      if (end > 2) {
        flush();
        nodes.push({ type: "strong", children: parseInline(rest.slice(2, end)) });
        index += end + 2;
        continue;
      }
    }

    if (rest.startsWith("*")) {
      const end = rest.indexOf("*", 1);
      if (end > 1) {
        flush();
        nodes.push({ type: "emphasis", children: parseInline(rest.slice(1, end)) });
        index += end + 1;
        continue;
      }
    }

    if (rest.startsWith("[")) {
      const labelEnd = rest.indexOf("](");
      if (labelEnd > 0) {
        const hrefEnd = closingParen(rest, labelEnd + 1);
        if (hrefEnd > labelEnd + 2) {
          const label = rest.slice(1, labelEnd);
          const href = rest.slice(labelEnd + 2, hrefEnd).trim();
          flush();
          if (safeHref(href)) {
            nodes.push({ type: "link", href, children: parseInline(label) });
          } else {
            // A link with another scheme is shown as the text it labels.
            nodes.push({ type: "text", text: label });
          }
          index += hrefEnd + 1;
          continue;
        }
      }
    }

    plain += text[index];
    index += 1;
  }

  flush();
  return nodes;
}

/** closingParen returns the parenthesis that closes the one at the open index,
 * so a target with parentheses stays whole. */
function closingParen(text: string, open: number): number {
  let depth = 0;
  for (let index = open; index < text.length; index += 1) {
    if (text[index] === "(") {
      depth += 1;
    } else if (text[index] === ")") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

/** safeHref reports whether a link may be offered as a link. */
export function safeHref(href: string): boolean {
  try {
    const url = new URL(href);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}
