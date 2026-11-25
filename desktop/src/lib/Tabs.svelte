<script lang="ts" module>
  import type { IconName } from "./icons";

  /** Descriptor of a single tab button. */
  export interface TabItem<T extends string = string> {
    id: T;
    label: string;
    icon: IconName;
    /** ID of the controlled tabpanel element. */
    controls: string;
  }
</script>

<script lang="ts" generics="T extends string">
  import Icon from "./Icon.svelte";

  let {
    items,
    active,
    ariaLabel,
    idPrefix,
    onSelect,
  }: {
    items: readonly TabItem<T>[];
    active: T;
    ariaLabel: string;
    idPrefix: string;
    onSelect: (id: T) => void;
  } = $props();

  /** nextTab returns the tab index a navigation key moves to, or -1 otherwise. */
  function nextTab(key: string, index: number): number {
    const last = items.length - 1;
    switch (key) {
      case "ArrowRight":
        return index === last ? 0 : index + 1;
      case "ArrowLeft":
        return index === 0 ? last : index - 1;
      case "Home":
        return 0;
      case "End":
        return last;
      default:
        return -1;
    }
  }

  function handleKey(event: KeyboardEvent, index: number): void {
    const next = nextTab(event.key, index);
    if (next < 0) {
      return;
    }
    event.preventDefault();
    onSelect(items[next].id);
    const list = (event.currentTarget as HTMLElement).parentElement;
    list?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  }
</script>

<div class="tabs" role="tablist" aria-label={ariaLabel}>
  {#each items as tab, index (tab.id)}
    <button
      id="{idPrefix}-tab-{tab.id}"
      class="tab"
      class:active={active === tab.id}
      type="button"
      role="tab"
      aria-selected={active === tab.id}
      aria-controls={tab.controls}
      tabindex={active === tab.id ? 0 : -1}
      onclick={() => onSelect(tab.id)}
      onkeydown={(event) => handleKey(event, index)}
    >
      <Icon name={tab.icon} size={14} />
      <span>{tab.label}</span>
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: 2px;
    width: fit-content;
    padding: 2px;
    border-radius: 8px;
    background-color: var(--fill);
  }

  .tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 12px;
    border: 0;
    border-radius: 6px;
    color: var(--secondary);
    background: transparent;
    font: inherit;
    font-size: 13px;
    cursor: default;
  }

  .tab.active {
    color: var(--foreground);
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
  }

  .tab:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
    outline-offset: 0;
  }
</style>
