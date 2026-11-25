<script lang="ts" module>
  /** Message shown to the user after an action or failure. */
  export interface NoticeMessage {
    kind: "info" | "error" | "warn";
    text: string;
  }
</script>

<script lang="ts">
  import type { Snippet } from "svelte";
  import { t } from "./i18n";

  let {
    notice,
    kind = "info",
    text = "",
    role,
    onDismiss,
    children,
  }: {
    /** Structured notice object, skipped when absent. */
    notice?: NoticeMessage | null;
    /** Tone of the notice when passing text directly. */
    kind?: "info" | "error" | "warn";
    /** Plain text when not passing a notice object. */
    text?: string;
    role?: "alert" | "status";
    /** Optional dismiss handler; shows a dismiss button when provided. */
    onDismiss?: () => void;
    children?: Snippet;
  } = $props();

  const resolvedKind = $derived(notice ? notice.kind : kind);
  const resolvedText = $derived(notice ? notice.text : text);
  const visible = $derived(
    notice !== undefined ? notice !== null : resolvedText !== "" || !!children,
  );
</script>

{#if visible}
  {#if onDismiss}
    <div
      class="notice dismissible"
      class:info={resolvedKind === "info"}
      class:error={resolvedKind === "error"}
      class:warn={resolvedKind === "warn"}
      {role}
    >
      <span
        >{resolvedText}{#if children}{@render children()}{/if}</span
      >
      <button class="button ghost small" type="button" onclick={onDismiss}>
        {t("common.dismiss")}
      </button>
    </div>
  {:else}
    <p
      class="notice"
      class:info={resolvedKind === "info"}
      class:error={resolvedKind === "error"}
      class:warn={resolvedKind === "warn"}
      {role}
    >
      {resolvedText}{#if children}{@render children()}{/if}
    </p>
  {/if}
{/if}

<style>
  .dismissible {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
</style>
