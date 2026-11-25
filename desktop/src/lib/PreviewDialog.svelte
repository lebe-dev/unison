<script lang="ts">
  import type { Snippet } from "svelte";
  import { t } from "./i18n";

  let {
    ariaLabel,
    closeAriaLabel,
    title,
    onClose,
    children,
    actions,
  }: {
    ariaLabel: string;
    closeAriaLabel: string;
    title: string;
    onClose: () => void;
    children: Snippet;
    /** Extra action buttons placed before the Close button. */
    actions?: Snippet;
  } = $props();
</script>

<div class="overlay" role="dialog" aria-modal="true" aria-label={ariaLabel}>
  <button class="backdrop" type="button" aria-label={closeAriaLabel} onclick={onClose}></button>
  <figure>
    {@render children()}
    <figcaption>
      <span class="preview-title">{title}</span>
      <span class="actions">
        {#if actions}
          {@render actions()}
        {/if}
        <button class="button small" type="button" onclick={onClose}>
          {t("common.close")}
        </button>
      </span>
    </figcaption>
  </figure>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem;
    background-color: rgba(0, 0, 0, 0.45);
    z-index: 10;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    border: none;
    background: transparent;
    cursor: zoom-out;
  }

  .overlay figure {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 90vw;
    max-height: 90vh;
    margin: 0;
    padding: 0.75rem;
    border-radius: 12px;
    background-color: var(--surface);
  }

  .overlay :global(canvas) {
    max-width: 100%;
    max-height: 75vh;
    object-fit: contain;
  }

  figcaption {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    color: var(--muted);
    font-size: 0.8125rem;
  }

  .preview-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .actions {
    display: flex;
    flex: none;
    gap: 0.35rem;
  }
</style>
