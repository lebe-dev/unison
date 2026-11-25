<script lang="ts">
  import { untrack } from "svelte";
  import { Kind, type HistoryItem } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import DaemonState from "../DaemonState.svelte";
  import { dashboard, type DashboardKind } from "../dashboard.svelte";
  import { canDragOut, crossedDragThreshold, stageItem, startStagedDrag } from "../dragOut";
  import { errorText } from "../errors";
  import FileBadge from "../FileBadge.svelte";
  import {
    fileNames,
    filesCountText,
    formatBytes,
    formatDateTime,
    historyTitle,
    itemSize,
    originLabel,
    textContent,
    textSnippet,
  } from "../format";
  import { t, type MessageKey } from "../i18n";
  import Icon from "../Icon.svelte";
  import type { IconName } from "../icons";
  import ImageCanvas from "../ImageCanvas.svelte";
  import Notice from "../Notice.svelte";
  import PreviewDialog from "../PreviewDialog.svelte";
  import Spinner from "../Spinner.svelte";
  import Tabs from "../Tabs.svelte";
  import UpdateBanner from "../UpdateBanner.svelte";
  import DashboardMenu, { MENU_HEIGHT, MENU_WIDTH } from "./DashboardMenu.svelte";

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

  /** Context menu of one item, at the point of the right click. */
  interface ItemMenu {
    item: HistoryItem;
    x: number;
    y: number;
  }

  let active = $state<DashboardKind>("images");
  let previewId = $state<string | null>(null);
  let pending: PendingDrag | null = null;
  let suppressClick = false;
  let menu = $state<ItemMenu | null>(null);

  const tabItems = $derived(
    tabs.map((tab) => ({
      id: tab.kind,
      label: t(tab.labelKey),
      icon: tab.icon,
      controls: `dashboard-panel-${tab.kind}`,
    })),
  );
  const items = $derived(dashboard.items[active]);
  const preview = $derived.by(() => {
    if (!previewId) {
      return null;
    }
    for (const kind of ["images", "files", "text"] as const) {
      const found = dashboard.items[kind].find((entry) => entry.item?.id === previewId);
      if (found) {
        return found;
      }
    }
    return null;
  });

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
    previewId = item.item.id;
    if (item.item.kind === Kind.IMAGE && !item.complete) {
      void dashboard.download(item.item.id, Kind.IMAGE);
    }
  }

  function closePreview(): void {
    previewId = null;
  }

  /** openMenu shows the actions of an item at the point of the right click. */
  function openMenu(event: MouseEvent, item: HistoryItem): void {
    event.preventDefault();
    if (!item.item) {
      return;
    }
    const area = document.documentElement;
    menu = {
      item,
      x: Math.max(8, Math.min(event.clientX, area.clientWidth - MENU_WIDTH - 8)),
      y: Math.max(8, Math.min(event.clientY, area.clientHeight - MENU_HEIGHT - 8)),
    };
  }

  function closeMenu(): void {
    menu = null;
  }

  function fileLabel(name: string): string {
    return name || t("common.item");
  }

  function originName(origin: string): string {
    return originLabel(origin, daemon.deviceID, daemon.devices);
  }

  /** fileMetaParts builds the concise metadata segments of a file item. */
  function fileMetaParts(item: HistoryItem): string[] {
    const info = item.item;
    if (!info) {
      return [];
    }
    const parts: string[] = [];
    if (info.representations.length > 1) {
      parts.push(filesCountText(info.representations.length));
    }
    const size = itemSize(info);
    if (size > 0) {
      parts.push(formatBytes(size));
    }
    const origin = originName(info.origin);
    if (origin) {
      parts.push(origin);
    }
    const when = formatDateTime(info.createdAtUnixMs);
    if (when) {
      parts.push(when);
    }
    return parts;
  }

  /** openMenuAtButton anchors the item context menu below a trigger button. */
  function openMenuAtButton(event: MouseEvent, item: HistoryItem): void {
    event.stopPropagation();
    if (!item.item) {
      return;
    }
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    const area = document.documentElement;
    menu = {
      item,
      x: Math.max(8, Math.min(rect.right - MENU_WIDTH, area.clientWidth - MENU_WIDTH - 8)),
      y: Math.max(8, Math.min(rect.bottom + 4, area.clientHeight - MENU_HEIGHT - 8)),
    };
  }
</script>

<UpdateBanner />

{#if !daemon.connected}
  <DaemonState message={t("dashboard.daemonNotRunning")} />
{:else}
  <div class="dashboard">
    <Tabs
      items={tabItems}
      {active}
      ariaLabel={t("dashboard.tabsAria")}
      idPrefix="dashboard"
      onSelect={select}
    />

    <Notice notice={dashboard.notice} />

    {#if dashboard.error}
      <Notice kind="error" text={dashboard.error} />
    {/if}

    <div
      id="dashboard-panel-{active}"
      role="tabpanel"
      aria-labelledby="dashboard-tab-{active}"
      class="tab-panel"
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
                oncontextmenu={(event) => openMenu(event, item)}
                onpointerdown={(event) => handlePointerDown(event, item, "images")}
                onpointermove={handlePointerMove}
                onpointerup={handlePointerEnd}
                onpointercancel={handlePointerEnd}
              >
                {#if item.complete}
                  {#snippet tileFallback()}
                    <span class="placeholder">
                      <Icon name="image" size={22} />
                      <span>{t("dashboard.previewUnavailable")}</span>
                    </span>
                  {/snippet}
                  <ImageCanvas
                    id={info.id}
                    maxSide={THUMB_SIDE}
                    label={historyTitle(info)}
                    fallback={tileFallback}
                  />
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
        <ul class="file-list">
          {#each items as item, index (item.item?.id ?? index)}
            {@const info = item.item}
            {#if info}
              {@const firstRep = info.representations[0]}
              {@const metaParts = fileMetaParts(item)}
              {@const isDownloading = dashboard.downloading.includes(info.id)}
              <li
                class="file-item"
                class:pending={!item.complete}
                oncontextmenu={(event) => openMenu(event, item)}
              >
                <button
                  class="file-main"
                  type="button"
                  onclick={(event) => handleClick(event, item)}
                  onpointerdown={(event) => handlePointerDown(event, item, "files")}
                  onpointermove={handlePointerMove}
                  onpointerup={handlePointerEnd}
                  onpointercancel={handlePointerEnd}
                >
                  <FileBadge
                    fileName={firstRep?.fileName ?? ""}
                    mimeType={firstRep?.mimeType ?? ""}
                    count={info.representations.length}
                  />
                  <span class="file-info">
                    <span class="file-heading">
                      <span class="file-name">{historyTitle(info)}</span>
                      {#if !item.complete}
                        <span class="badge confirm">{t("history.notDownloaded")}</span>
                      {/if}
                    </span>
                    {#if metaParts.length > 0}
                      <span class="file-meta">
                        {#each metaParts as part, partIndex (partIndex)}
                          {#if partIndex > 0}
                            <span class="file-meta-sep" aria-hidden="true">·</span>
                          {/if}
                          <span>{part}</span>
                        {/each}
                      </span>
                    {/if}
                    {#if info.representations.length > 1}
                      <span class="file-sublist">{fileNames(info).join(", ")}</span>
                    {/if}
                  </span>
                </button>
                <div class="file-actions">
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
                    disabled={isDownloading}
                    onclick={() => void dashboard.download(info.id, Kind.FILES)}
                  >
                    {isDownloading ? t("dashboard.downloading") : t("common.download")}
                  </button>
                  <button
                    class="icon-button file-more"
                    type="button"
                    aria-label={t("dashboard.menuAria")}
                    onclick={(event) => openMenuAtButton(event, item)}
                  >
                    <Icon name="more" size={15} />
                  </button>
                </div>
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
                <button
                  class="card"
                  type="button"
                  onclick={(event) => handleClick(event, item)}
                  oncontextmenu={(event) => openMenu(event, item)}
                >
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

{#if menu}
  <DashboardMenu item={menu.item} x={menu.x} y={menu.y} onClose={closeMenu} />
{/if}

{#if preview}
  {@const info = preview.item}
  {#if info}
    {#snippet previewActions()}
      <button class="button small" type="button" onclick={() => void dashboard.copy(info.id)}>
        {t("common.copy")}
      </button>
      {#if info.kind === Kind.IMAGE || info.kind === Kind.FILES}
        <button
          class={info.kind === Kind.IMAGE && !preview.complete
            ? "button primary small"
            : "button small"}
          type="button"
          disabled={dashboard.downloading.includes(info.id)}
          onclick={() =>
            void dashboard.download(
              info.id,
              info.kind === Kind.IMAGE && !preview.complete ? Kind.IMAGE : Kind.FILES,
            )}
        >
          {t("common.download")}
        </button>
      {/if}
    {/snippet}

    <PreviewDialog
      ariaLabel={t("dashboard.previewAria")}
      closeAriaLabel={t("dashboard.closePreviewAria")}
      title={historyTitle(info)}
      onClose={closePreview}
      actions={previewActions}
    >
      {#if info.kind === Kind.IMAGE}
        {#snippet imageLoading()}
          <div class="preview-state">
            <Spinner />
          </div>
        {/snippet}
        {#snippet imageFallback()}
          <p class="muted">{t("dashboard.previewUnavailable")}</p>
        {/snippet}
        {#if preview.complete}
          <ImageCanvas
            id={info.id}
            maxSide={PREVIEW_SIDE}
            label={historyTitle(info)}
            loading={imageLoading}
            fallback={imageFallback}
          />
        {:else if dashboard.downloading.includes(info.id)}
          <div class="preview-state">
            <Spinner />
          </div>
        {:else}
          <p class="muted">{t("history.notDownloaded")}</p>
        {/if}
      {:else if info.kind === Kind.TEXT}
        <pre class="text">{textContent(info)}</pre>
      {:else}
        {@const previewMeta = fileMetaParts(preview)}
        <div class="preview-files-wrap">
          {#if previewMeta.length > 0 || !preview.complete}
            <div class="preview-meta">
              {#each previewMeta as part, partIndex (partIndex)}
                {#if partIndex > 0}
                  <span class="file-meta-sep" aria-hidden="true">·</span>
                {/if}
                <span>{part}</span>
              {/each}
              {#if !preview.complete}
                <span class="badge confirm">{t("history.notDownloaded")}</span>
              {/if}
            </div>
          {/if}
          <ul class="preview-files">
            {#each info.representations as rep, index (index)}
              <li class="preview-file-row">
                <FileBadge fileName={rep.fileName} mimeType={rep.mimeType} size="small" />
                <span class="preview-file-name">{fileLabel(rep.fileName)}</span>
                <span class="meta tabular">{formatBytes(rep.size)}</span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    </PreviewDialog>
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

  .tab-panel {
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

  .file-list {
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

  .file-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding-right: 12px;
  }

  .file-item + .file-item {
    border-top: 1px solid var(--separator);
  }

  .file-item:hover {
    background-color: var(--hover);
  }

  .file-item.pending .file-name {
    color: var(--secondary);
  }

  .file-main {
    display: flex;
    flex: 1;
    align-items: center;
    gap: 12px;
    min-width: 0;
    padding: 10px 0 10px 12px;
    border: 0;
    color: var(--foreground);
    background: transparent;
    font: inherit;
    text-align: left;
    cursor: default;
    user-select: none;
  }

  .file-main:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
    outline-offset: -3px;
  }

  .file-info {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .file-heading {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .file-name {
    min-width: 0;
    overflow: hidden;
    font-weight: 500;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-meta {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    gap: 5px;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    color: var(--muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }

  .file-meta-sep {
    opacity: 0.6;
  }

  .file-sublist {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--secondary);
    font-size: 11px;
    opacity: 0.85;
  }

  .file-actions {
    display: flex;
    flex: none;
    align-items: center;
    gap: 6px;
  }

  .file-more {
    width: 24px;
    height: 22px;
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

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    min-width: 0;
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

  .preview-state {
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 160px;
    min-height: 96px;
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

  .preview-files-wrap {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(70vw, 500px);
  }

  .preview-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }

  .preview-files {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 55vh;
    margin: 0;
    padding: 6px;
    overflow-y: auto;
    list-style: none;
    border-radius: 8px;
    background-color: var(--surface-soft);
  }

  .preview-file-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 8px;
    border-radius: 6px;
  }

  .preview-file-name {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .tabular {
    font-variant-numeric: tabular-nums;
  }
</style>
