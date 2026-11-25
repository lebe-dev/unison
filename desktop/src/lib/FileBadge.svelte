<script lang="ts">
  import { fileCategory, fileExtension } from "./format";
  import Icon from "./Icon.svelte";

  let {
    fileName = "",
    mimeType = "",
    count = 1,
    size = "normal",
  }: {
    fileName?: string;
    mimeType?: string;
    /** Number of files in a bundle; values above 1 show a bundle badge. */
    count?: number;
    size?: "normal" | "small";
  } = $props();

  const multi = $derived(count > 1);
  const ext = $derived(multi ? "" : fileExtension(fileName));
  const category = $derived(multi ? "bundle" : fileCategory(fileName, mimeType));
</script>

<span class="file-badge {category}" class:small={size === "small"} aria-hidden="true">
  {#if multi}
    <Icon name="file" size={16} />
    <span class="file-badge-count">{count}</span>
  {:else if ext}
    <span class="file-badge-ext">{ext}</span>
  {:else}
    <Icon name="file" size={size === "small" ? 14 : 18} />
  {/if}
</span>

<style>
  .file-badge {
    position: relative;
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    border-radius: 8px;
    color: var(--secondary);
    background-color: var(--surface-soft);
  }

  .file-badge.small {
    width: 28px;
    height: 28px;
    border-radius: 6px;
  }

  .file-badge.image {
    color: var(--accent);
    background-color: var(--accent-soft);
  }

  .file-badge.document {
    color: var(--danger);
    background-color: var(--danger-soft);
  }

  .file-badge.archive {
    color: var(--warn);
    background-color: var(--warn-soft);
  }

  .file-badge.media {
    color: #9b6dff;
    background-color: color-mix(in srgb, #9b6dff 15%, transparent);
  }

  .file-badge.code {
    color: var(--online-text);
    background-color: color-mix(in srgb, var(--online) 14%, transparent);
  }

  .file-badge.bundle {
    color: var(--accent);
    background-color: var(--accent-soft);
  }

  .file-badge-ext {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.03em;
    line-height: 1;
  }

  .file-badge.small .file-badge-ext {
    font-size: 9px;
  }

  .file-badge-count {
    position: absolute;
    right: -4px;
    bottom: -4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    box-sizing: border-box;
    border-radius: 8px;
    color: var(--accent-text);
    background-color: var(--accent);
    font-size: 10px;
    font-weight: 600;
    line-height: 1;
  }
</style>
