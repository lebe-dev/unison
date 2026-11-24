<script lang="ts">
  import { parseMarkdown, type Inline } from "./markdown";

  let { text }: { /** Markdown of a release changelog. */ text: string } = $props();

  const blocks = $derived(parseMarkdown(text));
</script>

<div class="markdown">
  {#each blocks as block, index (index)}
    {#if block.type === "heading"}
      <svelte:element this={`h${Math.min(block.level + 2, 6)}`}>
        {@render inlines(block.children)}
      </svelte:element>
    {:else if block.type === "paragraph"}
      <p>{@render inlines(block.children)}</p>
    {:else if block.type === "list"}
      {#if block.ordered}
        <ol>
          {#each block.items as item, itemIndex (itemIndex)}
            <li>{@render inlines(item)}</li>
          {/each}
        </ol>
      {:else}
        <ul>
          {#each block.items as item, itemIndex (itemIndex)}
            <li>{@render inlines(item)}</li>
          {/each}
        </ul>
      {/if}
    {:else}
      <pre><code class:language={block.language !== ""}>{block.text}</code></pre>
    {/if}
  {/each}
</div>

{#snippet inlines(nodes: Inline[])}
  {#each nodes as node, index (index)}
    {#if node.type === "text"}{node.text}
    {:else if node.type === "code"}<code>{node.text}</code>
    {:else if node.type === "strong"}<strong>{@render inlines(node.children)}</strong>
    {:else if node.type === "emphasis"}<em>{@render inlines(node.children)}</em>
    {:else}
      <a href={node.href} target="_blank" rel="noreferrer noopener"
        >{@render inlines(node.children)}</a
      >
    {/if}
  {/each}
{/snippet}

<style>
  .markdown {
    font-size: 13px;
    line-height: 1.5;
  }

  .markdown :global(h3),
  .markdown :global(h4),
  .markdown :global(h5),
  .markdown :global(h6) {
    margin: 16px 0 6px;
    font-size: 13px;
    font-weight: 600;
  }

  .markdown :global(h3:first-child),
  .markdown :global(h4:first-child),
  .markdown :global(h5:first-child),
  .markdown :global(h6:first-child) {
    margin-top: 0;
  }

  .markdown p,
  .markdown ul,
  .markdown ol,
  .markdown pre {
    margin: 0 0 10px;
  }

  .markdown ul,
  .markdown ol {
    padding-left: 20px;
  }

  .markdown pre {
    overflow-x: auto;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--surface-2);
  }

  .markdown code {
    font-family: var(--mono, ui-monospace, monospace);
    font-size: 12px;
  }

  .markdown a {
    color: var(--accent);
  }
</style>
