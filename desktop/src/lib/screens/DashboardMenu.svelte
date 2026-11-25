<script lang="ts" module>
  /** Width of the menu, used to keep it inside the window. */
  export const MENU_WIDTH = 260;

  /** Approximate height of the menu, used to keep it inside the window. */
  export const MENU_HEIGHT = 220;
</script>

<script lang="ts">
  import { tick } from "svelte";
  import { client } from "../api/client";
  import type { Device, HistoryItem } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { dashboard } from "../dashboard.svelte";
  import { stageItem } from "../dragOut";
  import { errorText } from "../errors";
  import { deviceLabel } from "../format";
  import { t } from "../i18n";
  import Icon from "../Icon.svelte";
  import { transfers } from "../transfers.svelte";

  /** Step of the context menu of an item. */
  type MenuStep = "actions" | "devices" | "delete";

  let {
    item,
    x,
    y,
    onClose,
  }: {
    item: HistoryItem;
    x: number;
    y: number;
    onClose: () => void;
  } = $props();

  let step = $state<MenuStep>("actions");
  let busy = $state(false);
  let menuElement: HTMLDivElement | null = $state(null);

  const onlineDevices = $derived(
    daemon.devices.filter((device) => device.online && !device.revoked),
  );

  $effect(() => {
    void item;
    void x;
    void y;
    step = "actions";
    busy = false;
    void tick().then(focusMenu);
  });

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

  function showStep(next: MenuStep): void {
    step = next;
    if (next !== "actions") {
      void tick().then(focusMenu);
    }
  }

  /** handleMenuKey moves the focus of the menu and closes it with Escape. */
  function handleMenuKey(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
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

  async function runBusy(fn: () => Promise<void>): Promise<void> {
    if (busy) {
      return;
    }
    busy = true;
    try {
      await fn();
    } finally {
      busy = false;
      onClose();
    }
  }

  function copyItem(): Promise<void> {
    const info = item.item;
    if (!info) {
      return Promise.resolve();
    }
    return runBusy(() => dashboard.copy(info.id));
  }

  function sendItem(device: Device): Promise<void> {
    if (!item.item) {
      return Promise.resolve();
    }
    return runBusy(async () => {
      try {
        const paths = await stageItem(item);
        if (await transfers.send(paths, [device.deviceId])) {
          dashboard.notice = {
            kind: "info",
            text: t("dashboard.sentTo", { name: deviceLabel(device) }),
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
      }
    });
  }

  function deleteItem(): Promise<void> {
    const id = item.item?.id;
    if (!id) {
      return Promise.resolve();
    }
    return runBusy(async () => {
      try {
        await client.deleteHistoryItem({ id });
        dashboard.notice = { kind: "info", text: t("dashboard.deleted") };
        void dashboard.loadAll(true);
      } catch (error) {
        dashboard.notice = {
          kind: "error",
          text: errorText(error, t("dashboard.deleteFailed")),
        };
      }
    });
  }
</script>

<button
  class="menu-backdrop"
  type="button"
  aria-label={t("dashboard.closeMenuAria")}
  onclick={onClose}
></button>
<div
  class="menu"
  role="menu"
  aria-label={t("dashboard.menuAria")}
  tabindex="-1"
  bind:this={menuElement}
  style:left={`${x}px`}
  style:top={`${y}px`}
  onkeydown={handleMenuKey}
>
  {#if step === "actions"}
    <button
      class="menu-item"
      type="button"
      role="menuitem"
      disabled={busy}
      onclick={() => showStep("devices")}
    >
      <span>{t("dashboard.menuSend")}</span>
      <Icon name="chevronRight" size={12} stroke={1.6} />
    </button>
    <button
      class="menu-item"
      type="button"
      role="menuitem"
      disabled={busy}
      onclick={() => void copyItem()}
    >
      {t("dashboard.menuCopy")}
    </button>
    <div class="menu-separator"></div>
    <button
      class="menu-item danger"
      type="button"
      role="menuitem"
      disabled={busy}
      onclick={() => showStep("delete")}
    >
      {t("history.delete")}
    </button>
  {:else if step === "devices"}
    <p class="menu-title">{t("dashboard.sendTo")}</p>
    {#if onlineDevices.length === 0}
      <p class="menu-empty">{t("transfers.noDeviceOnline")}</p>
    {:else}
      {#each onlineDevices as device (device.deviceId)}
        <button
          class="menu-item"
          type="button"
          role="menuitem"
          disabled={busy}
          onclick={() => void sendItem(device)}
        >
          {deviceLabel(device)}
        </button>
      {/each}
    {/if}
    <div class="menu-separator"></div>
    <button class="menu-item" type="button" role="menuitem" onclick={() => showStep("actions")}>
      {t("common.back")}
    </button>
  {:else}
    <p class="menu-title">{t("dashboard.deleteTitle")}</p>
    <p class="menu-empty">{t("dashboard.deleteConfirm")}</p>
    <button
      class="menu-item danger"
      type="button"
      role="menuitem"
      disabled={busy}
      onclick={() => void deleteItem()}
    >
      {t("history.delete")}
    </button>
    <button class="menu-item" type="button" role="menuitem" onclick={() => showStep("actions")}>
      {t("common.cancel")}
    </button>
  {/if}
</div>

<style>
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
