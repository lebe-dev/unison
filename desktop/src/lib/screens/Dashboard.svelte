<script lang="ts">
  import { tick, untrack } from "svelte";
  import { client } from "../api/client";
  import { Kind, type Device, type HistoryItem } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { dashboard, type DashboardKind } from "../dashboard.svelte";
  import { canDragOut, crossedDragThreshold, stageItem, startStagedDrag } from "../dragOut";
  import { errorText } from "../errors";
  import {
    formatBytes,
    formatDateTime,
    historyTitle,
    itemSize,
    shortDeviceID,
    textContent,
    textSnippet,
  } from "../format";
  import { t, type MessageKey } from "../i18n";
  import Icon, { type IconName } from "../Icon.svelte";
  import ImageCanvas from "../ImageCanvas.svelte";
  import Spinner from "../Spinner.svelte";
  import { transfers } from "../transfers.svelte";
  import UpdateBanner from "../UpdateBanner.svelte";

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

  /** Step of the context menu of an item. */
  type MenuStep = "actions" | "devices" | "delete";

  /** Context menu of one item, at the point of the right click. */
  interface ItemMenu {
    item: HistoryItem;
    x: number;
    y: number;
    step: MenuStep;
  }

  /** Width of the menu, used to keep it inside the window. */
  const MENU_WIDTH = 260;

  /** Approximate height of the menu, used to keep it inside the window. */
  const MENU_HEIGHT = 220;

  let active = $state<DashboardKind>("images");
  let previewId = $state<string | null>(null);
  let pending: PendingDrag | null = null;
  let suppressClick = false;
  let menu = $state<ItemMenu | null>(null);
  let menuBusy = $state(false);
  let menuElement: HTMLDivElement | null = $state(null);

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
  const menuDevices = $derived(daemon.devices.filter((device) => device.online && !device.revoked));

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
    previewId = item.item.id;
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
      step: "actions",
    };
    menuBusy = false;
    void tick().then(focusMenu);
  }

  /** focusMenu keeps the menu inside the window and focuses its first entry. */
  function focusMenu(): void {
    if (!menuElement) {
      return;
    }
    const rect = menuElement.getBoundingClientRect();
    if (rect.bottom > window.innerHeight - 8) {
      menuElement.style.top = `${Math.max(8, window.innerHeight - rect.height - 8)}px`;
    }
    if (rect.right > window.innerWidth - 8) {
      menuElement.style.left = `${Math.max(8, window.innerWidth - rect.width - 8)}px`;
    }
    menuElement.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
  }

  function closeMenu(): void {
    menu = null;
    menuBusy = false;
  }

  function showActions(): void {
    if (menu) {
      menu.step = "actions";
    }
  }

  function showDevices(): void {
    if (menu) {
      menu.step = "devices";
      void tick().then(focusMenu);
    }
  }

  function showDelete(): void {
    if (menu) {
      menu.step = "delete";
      void tick().then(focusMenu);
    }
  }

  /** handleMenuKey moves the focus of the menu and closes it with Escape. */
  function handleMenuKey(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeMenu();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      return;
    }
    const entries = [
      ...(menuElement?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ??
        []),
    ];
    if (entries.length === 0) {
      return;
    }
    event.preventDefault();
    const current = entries.indexOf(document.activeElement as HTMLButtonElement);
    let next = 0;
    if (event.key === "ArrowDown") {
      next = current < 0 ? 0 : (current + 1) % entries.length;
    } else if (event.key === "ArrowUp") {
      next = current <= 0 ? entries.length - 1 : current - 1;
    } else if (event.key === "End") {
      next = entries.length - 1;
    }
    entries[next].focus();
  }

  function deviceName(device: Device): string {
    return device.name || shortDeviceID(device.deviceId);
  }

  /** copyItem puts the item on the clipboard without sending it anywhere. The
   * daemon writes it with the echo marker of its own writes, so the copy does
   * not come back as a new item and is not announced to the other devices. */
  async function copyItem(item: HistoryItem): Promise<void> {
    const info = item.item;
    if (!info || menuBusy) {
      return;
    }
    menuBusy = true;
    try {
      await dashboard.copy(info.id);
    } finally {
      menuBusy = false;
      closeMenu();
    }
  }

  /** sendItem offers the content of the item to one device as a file. */
  async function sendItem(item: HistoryItem, device: Device): Promise<void> {
    if (!item.item || menuBusy) {
      return;
    }
    menuBusy = true;
    try {
      const paths = await stageItem(item);
      if (await transfers.send(paths, [device.deviceId])) {
        dashboard.notice = {
          kind: "info",
          text: t("dashboard.sentTo", { name: deviceName(device) }),
        };
      } else {
        dashboard.notice = {
          kind: "error",
          text: transfers.notice?.text ?? t("dashboard.sendFailed"),
        };
      }
    } catch (error) {
      dashboard.notice = {
        kind: "error",
        text: errorText(error, t("dashboard.sendFailed")),
      };
    } finally {
      menuBusy = false;
      closeMenu();
    }
  }

  /** deleteItem removes the item from the history of every Personal device. */
  async function deleteItem(item: HistoryItem): Promise<void> {
    const id = item.item?.id;
    if (!id || menuBusy) {
      return;
    }
    menuBusy = true;
    try {
      await client.deleteHistoryItem({ id });
      dashboard.notice = { kind: "info", text: t("dashboard.deleted") };
      void dashboard.loadAll(true);
    } catch (error) {
      dashboard.notice = {
        kind: "error",
        text: errorText(error, t("dashboard.deleteFailed")),
      };
    } finally {
      menuBusy = false;
      closeMenu();
    }
  }

  function fileLabel(name: string): string {
    return name || t("common.item");
  }
</script>

<UpdateBanner />

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
                oncontextmenu={(event) => openMenu(event, item)}
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
                  oncontextmenu={(event) => openMenu(event, item)}
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
  {@const target = menu.item}
  <button
    class="menu-backdrop"
    type="button"
    aria-label={t("dashboard.closeMenuAria")}
    onclick={closeMenu}
  ></button>
  <div
    class="menu"
    role="menu"
    aria-label={t("dashboard.menuAria")}
    tabindex="-1"
    bind:this={menuElement}
    style:left={`${menu.x}px`}
    style:top={`${menu.y}px`}
    onkeydown={handleMenuKey}
  >
    {#if menu.step === "actions"}
      <button
        class="menu-item"
        type="button"
        role="menuitem"
        disabled={menuBusy}
        onclick={showDevices}
      >
        <span>{t("dashboard.menuSend")}</span>
        <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
          <path
            d="m6 3 5 5-5 5"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        class="menu-item"
        type="button"
        role="menuitem"
        disabled={menuBusy}
        onclick={() => void copyItem(target)}
      >
        {t("dashboard.menuCopy")}
      </button>
      <div class="menu-separator"></div>
      <button
        class="menu-item danger"
        type="button"
        role="menuitem"
        disabled={menuBusy}
        onclick={showDelete}
      >
        {t("history.delete")}
      </button>
    {:else if menu.step === "devices"}
      <p class="menu-title">{t("dashboard.sendTo")}</p>
      {#if menuDevices.length === 0}
        <p class="menu-empty">{t("transfers.noDeviceOnline")}</p>
      {:else}
        {#each menuDevices as device (device.deviceId)}
          <button
            class="menu-item"
            type="button"
            role="menuitem"
            disabled={menuBusy}
            onclick={() => void sendItem(target, device)}
          >
            {deviceName(device)}
          </button>
        {/each}
      {/if}
      <div class="menu-separator"></div>
      <button class="menu-item" type="button" role="menuitem" onclick={showActions}>
        {t("common.back")}
      </button>
    {:else}
      <p class="menu-title">{t("dashboard.deleteTitle")}</p>
      <p class="menu-empty">{t("dashboard.deleteConfirm")}</p>
      <button
        class="menu-item danger"
        type="button"
        role="menuitem"
        disabled={menuBusy}
        onclick={() => void deleteItem(target)}
      >
        {t("history.delete")}
      </button>
      <button class="menu-item" type="button" role="menuitem" onclick={showActions}>
        {t("common.cancel")}
      </button>
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
          {#if preview.complete}
            <ImageCanvas
              id={info.id}
              maxSide={PREVIEW_SIDE}
              label={historyTitle(info)}
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
            {#if info.kind === Kind.IMAGE}
              <button
                class="button small"
                type="button"
                onclick={() => void dashboard.copy(info.id)}
              >
                {t("common.copy")}
              </button>
              {#if !preview.complete}
                <button
                  class="button primary small"
                  type="button"
                  disabled={dashboard.downloading.includes(info.id)}
                  onclick={() => void dashboard.download(info.id, Kind.IMAGE)}
                >
                  {t("common.download")}
                </button>
              {/if}
            {:else if info.kind === Kind.TEXT}
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
                disabled={dashboard.downloading.includes(info.id)}
                onclick={() => void dashboard.download(info.id, Kind.FILES)}
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

  .menu-backdrop {
    position: fixed;
    inset: 0;
    border: none;
    background: transparent;
    z-index: 20;
  }

  .menu {
    position: fixed;
    display: flex;
    flex-direction: column;
    width: 260px;
    max-height: calc(100vh - 16px);
    padding: 4px;
    border: 1px solid var(--separator);
    border-radius: 8px;
    background-color: var(--surface);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.28);
    overflow-y: auto;
    z-index: 21;
  }

  .menu-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    width: 100%;
    height: 28px;
    padding: 0 8px;
    border: 0;
    border-radius: 5px;
    color: var(--foreground);
    background: transparent;
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: default;
  }

  .menu-item:hover:not(:disabled),
  .menu-item:focus-visible {
    background-color: var(--accent);
    color: var(--accent-text);
    outline: none;
  }

  .menu-item:disabled {
    color: var(--muted);
  }

  .menu-item.danger {
    color: var(--danger);
  }

  .menu-item.danger:hover:not(:disabled),
  .menu-item.danger:focus-visible {
    background-color: var(--danger);
    color: #fff;
  }

  .menu-separator {
    height: 1px;
    margin: 4px 6px;
    background-color: var(--separator);
  }

  .menu-title {
    margin: 4px 8px 6px;
    color: var(--muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
  }

  .menu-empty {
    margin: 0;
    padding: 4px 8px 8px;
    color: var(--muted);
    font-size: 12px;
    line-height: 1.4;
  }
</style>
