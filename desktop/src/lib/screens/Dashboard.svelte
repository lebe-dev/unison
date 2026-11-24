<script lang="ts">
  import { untrack } from "svelte";
  import { Kind, type HistoryItem } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { dashboard, type DashboardKind } from "../dashboard.svelte";
  import { canDragOut, crossedDragThreshold, stageItem, startStagedDrag } from "../dragOut";
  import { errorText } from "../errors";
  import {
    formatBytes,
    formatDateTime,
    historyTitle,
    itemSize,
    textContent,
    textSnippet,
  } from "../format";
  import { t, type MessageKey } from "../i18n";
  import Icon, { type IconName } from "../Icon.svelte";
  import ImageCanvas from "../ImageCanvas.svelte";
  import Spinner from "../Spinner.svelte";

  /** Longer side of the pixels of a tile. */
  const THUMB_SIDE = 256;

  /** Longer side of the pixels of the full preview. */
  const PREVIEW_SIDE = 1024;

  const tabs: readonly { kind: DashboardKind; labelKey: MessageKey; icon: IconName }[] = [
    { kind: "images", labelKey: "dashboard.tabImages", icon: "image" },
    { kind: "files", labelKey: "dashboard.tabFiles", icon: "file" },
    { kind: "text", labelKey: "dashboard.tabText", icon: "clipboard" },
  ];

  const EMPTY_KEYS: Record<DashboardKind, MessageKey> = {
    images: "dashboard.emptyImages",
    files: "dashboard.emptyFiles",
    text: "dashboard.emptyText",
  };

  /** Press that may become a drag. */
  interface PendingDrag {
    start: { x: number; y: number };
    item: HistoryItem;
    started: boolean;
    released: boolean;
  }

  let active = $state<DashboardKind>("images");
  let preview = $state<HistoryItem | null>(null);
  let pending: PendingDrag | null = null;
  let suppressClick = false;

  const items = $derived(dashboard.items[active]);

  // The lists are loaded when the daemon answers and again after it comes back.
  // The download directory is read here as well, because the start of the app
  // may come before the daemon.
  $effect(() => {
    if (daemon.connected) {
      untrack(() => {
        void dashboard.loadAll();
        void dashboard.loadSettings();
      });
    }
  });

  /** select shows a tab and refreshes it without hiding the items on screen. */
  function select(kind: DashboardKind): void {
    active = kind;
    void dashboard.load(kind, true);
  }

  /** isDraggable reports the kinds that leave the window as files. */
  function isDraggable(kind: DashboardKind): boolean {
    return kind !== "text";
  }

  /** nextTab returns the tab the key moves to, or -1 for another key. */
  function nextTab(key: string, index: number): number {
    const last = tabs.length - 1;
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

  function handleTabKey(event: KeyboardEvent, index: number): void {
    const next = nextTab(event.key, index);
    if (next < 0) {
      return;
    }
    event.preventDefault();
    active = tabs[next].kind;
    void dashboard.load(tabs[next].kind, true);
    const list = (event.currentTarget as HTMLElement).parentElement;
    list?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  }

  /** handlePointerDown remembers the press. The copies are staged only when the
   * press becomes a drag, so a click does not download the content. */
  function handlePointerDown(event: PointerEvent, item: HistoryItem, kind: DashboardKind): void {
    suppressClick = false;
    pending = null;
    if (!canDragOut() || !isDraggable(kind) || event.button !== 0 || !item.item) {
      return;
    }
    const tile = event.currentTarget as HTMLElement;
    if (typeof tile.setPointerCapture === "function") {
      tile.setPointerCapture(event.pointerId);
    }
    pending = {
      start: { x: event.clientX, y: event.clientY },
      item,
      started: false,
      released: false,
    };
  }

  /** handlePointerMove starts staging once the pointer left the press position,
   * then hands the copies to the native drag. */
  function handlePointerMove(event: PointerEvent): void {
    const drag = pending;
    if (!drag || drag.started || drag.released) {
      return;
    }
    if (!crossedDragThreshold(drag.start, { x: event.clientX, y: event.clientY })) {
      return;
    }
    drag.started = true;
    suppressClick = true;
    void stageItem(drag.item).then(
      (paths) => {
        // A release before the copies are ready cancels the drag.
        if (drag.released || pending !== drag) {
          return;
        }
        pending = null;
        void startStagedDrag(paths).catch((error: unknown) => {
          dashboard.notice = {
            kind: "error",
            text: errorText(error, t("dashboard.dragFailed")),
          };
        });
      },
      (error: unknown) => {
        pending = null;
        dashboard.notice = {
          kind: "error",
          text: errorText(error, t("dashboard.dragFailed")),
        };
      },
    );
  }

  function handlePointerEnd(): void {
    if (!pending) {
      return;
    }
    pending.released = true;
    pending = null;
  }

  function handleClick(event: MouseEvent, item: HistoryItem): void {
    // A click with detail 0 comes from the keyboard, which never drags.
    if (suppressClick && event.detail !== 0) {
      suppressClick = false;
      return;
    }
    suppressClick = false;
    openPreview(item);
  }

  function openPreview(item: HistoryItem): void {
    if (!item.item) {
      return;
    }
    preview = item;
  }

  function closePreview(): void {
    preview = null;
  }

  function fileLabel(name: string): string {
    return name || t("common.item");
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <Spinner />
      <p class="muted">{t("devices.connecting")}</p>
    {:else}
      <p class="notice error">{t("dashboard.daemonNotRunning")}</p>
      <button class="button" type="button" onclick={() => void daemon.retry()}>
        {t("common.tryAgain")}
      </button>
    {/if}
  </section>
{:else}
  <div class="dashboard">
    <div class="tabs" role="tablist" aria-label={t("dashboard.tabsAria")}>
      {#each tabs as tab, index (tab.kind)}
        <button
          id="dashboard-tab-{tab.kind}"
          class="tab"
          class:active={active === tab.kind}
          type="button"
          role="tab"
          aria-selected={active === tab.kind}
          aria-controls="dashboard-panel-{tab.kind}"
          tabindex={active === tab.kind ? 0 : -1}
          onclick={() => select(tab.kind)}
          onkeydown={(event) => handleTabKey(event, index)}
        >
          <Icon name={tab.icon} size={14} />
          <span>{t(tab.labelKey)}</span>
        </button>
      {/each}
    </div>

    {#if dashboard.notice}
      <p
        class="notice"
        class:error={dashboard.notice.kind === "error"}
        class:info={dashboard.notice.kind === "info"}
      >
        {dashboard.notice.text}
      </p>
    {/if}

    {#if dashboard.error}
      <p class="notice error">{dashboard.error}</p>
    {/if}

    <div
      id="dashboard-panel-{active}"
      role="tabpanel"
      aria-labelledby="dashboard-tab-{active}"
      class="panel"
    >
      {#if dashboard.loading.includes(active) && items.length === 0}
        <div class="state">
          <Spinner />
          <p class="muted">{t("dashboard.loading")}</p>
        </div>
      {:else if items.length === 0}
        <p class="empty">{t(EMPTY_KEYS[active])}</p>
      {:else if active === "images"}
        <div class="grid">
          {#each items as item, index (item.item?.id ?? index)}
            {@const info = item.item}
            {#if info}
              <button
                class="tile"
                type="button"
                aria-label={historyTitle(info)}
                onclick={(event) => handleClick(event, item)}
                onpointerdown={(event) => handlePointerDown(event, item, "images")}
                onpointermove={handlePointerMove}
                onpointerup={handlePointerEnd}
                onpointercancel={handlePointerEnd}
              >
                {#if item.complete}
                  <ImageCanvas id={info.id} maxSide={THUMB_SIDE} label={historyTitle(info)} />
                {:else}
                  <span class="placeholder">
                    <Icon name="image" size={22} />
                    <span>{t("history.notDownloaded")}</span>
                  </span>
                {/if}
              </button>
            {/if}
          {/each}
        </div>
      {:else if active === "files"}
        <ul class="cards">
          {#each items as item, index (item.item?.id ?? index)}
            {@const info = item.item}
            {#if info}
              <li>
                <button
                  class="card"
                  type="button"
                  onclick={(event) => handleClick(event, item)}
                  onpointerdown={(event) => handlePointerDown(event, item, "files")}
                  onpointermove={handlePointerMove}
                  onpointerup={handlePointerEnd}
                  onpointercancel={handlePointerEnd}
                >
                  <span class="card-title">{historyTitle(info)}</span>
                  <span class="rows">
                    {#each info.representations as rep, repIndex (repIndex)}
                      <span class="row">
                        <span class="name">{fileLabel(rep.fileName)}</span>
                        <span class="meta">{formatBytes(rep.size)}</span>
                      </span>
                    {/each}
                  </span>
                </button>
              </li>
            {/if}
          {/each}
        </ul>
      {:else}
        <ul class="cards">
          {#each items as item, index (item.item?.id ?? index)}
            {@const info = item.item}
            {#if info}
              <li>
                <button class="card" type="button" onclick={(event) => handleClick(event, item)}>
                  <span class="snippet">{textSnippet(info, 180) || t("format.textItem")}</span>
                  <span class="row meta">
                    <span>{formatDateTime(info.createdAtUnixMs)}</span>
                    <span>{formatBytes(itemSize(info))}</span>
                  </span>
                </button>
              </li>
            {/if}
          {/each}
        </ul>
      {/if}
    </div>

    {#if canDragOut() && active !== "text"}
      <p class="hint muted">{t("dashboard.dragHint")}</p>
    {/if}
  </div>
{/if}

{#if preview}
  {@const info = preview.item}
  {#if info}
    <div class="overlay" role="dialog" aria-modal="true" aria-label={t("dashboard.previewAria")}>
      <button
        class="backdrop"
        type="button"
        aria-label={t("dashboard.closePreviewAria")}
        onclick={closePreview}
      ></button>
      <figure>
        {#if info.kind === Kind.IMAGE}
          {#snippet imageFallback()}
            <p class="muted">{t("history.notDownloaded")}</p>
          {/snippet}
          <ImageCanvas
            id={info.id}
            maxSide={PREVIEW_SIDE}
            label={historyTitle(info)}
            fallback={imageFallback}
          />
        {:else if info.kind === Kind.TEXT}
          <pre class="text">{textContent(info)}</pre>
        {:else}
          <ul class="preview-files">
            {#each info.representations as rep, index (index)}
              <li class="row">
                <span class="name">{fileLabel(rep.fileName)}</span>
                <span class="meta">{formatBytes(rep.size)}</span>
              </li>
            {/each}
          </ul>
        {/if}
        <figcaption>
          <span class="preview-title">{historyTitle(info)}</span>
          <span class="actions">
            {#if info.kind === Kind.TEXT}
              <button
                class="button small"
                type="button"
                onclick={() => void dashboard.copy(info.id)}
              >
                {t("common.copy")}
              </button>
            {:else if info.kind === Kind.FILES}
              <button
                class="button small"
                type="button"
                onclick={() => void dashboard.copy(info.id)}
              >
                {t("common.copy")}
              </button>
              <button
                class="button small"
                type="button"
                onclick={() => void dashboard.download(info.id)}
              >
                {t("common.download")}
              </button>
            {/if}
            <button class="button small" type="button" onclick={closePreview}>
              {t("common.close")}
            </button>
          </span>
        </figcaption>
      </figure>
    </div>
  {/if}
{/if}

<style>
  .dashboard {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 12px;
    min-height: 0;
  }

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

  .panel {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 12px;
  }

  .tile {
    aspect-ratio: 1;
    padding: 0;
    border: 0;
    border-radius: 10px;
    background-color: var(--surface-soft);
    box-shadow: var(--box-shadow);
    overflow: hidden;
    cursor: zoom-in;
    user-select: none;
  }

  .tile :global(canvas) {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    height: 100%;
    padding: 8px;
    box-sizing: border-box;
    color: var(--muted);
    font-size: 11px;
    text-align: center;
  }

  .cards {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border-radius: 10px;
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
    overflow: hidden;
  }

  .cards > li + li {
    border-top: 1px solid var(--separator);
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    padding: 10px 12px;
    border: 0;
    color: var(--foreground);
    background: transparent;
    font: inherit;
    text-align: left;
    cursor: zoom-in;
    user-select: none;
  }

  .card:hover {
    background-color: var(--hover);
  }

  .card:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
    outline-offset: -3px;
  }

  .card-title {
    overflow: hidden;
    font-weight: 500;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    min-width: 0;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    flex: none;
    color: var(--muted);
    font-size: 11px;
  }

  .snippet {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .hint {
    flex: none;
  }

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

  .text {
    max-width: min(80vw, 720px);
    max-height: 70vh;
    margin: 0;
    padding: 8px;
    overflow: auto;
    border-radius: 8px;
    color: var(--foreground);
    background-color: var(--surface-soft);
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .preview-files {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: min(70vw, 480px);
    margin: 0;
    padding: 8px;
    list-style: none;
    border-radius: 8px;
    background-color: var(--surface-soft);
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
