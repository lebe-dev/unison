<script lang="ts">
  import { t } from "./i18n";

  let {
    summary,
    hint = "",
    details = "",
    onDismiss,
  }: {
    /** Short message in plain words. */
    summary: string;
    /** What the user can do about it. */
    hint?: string;
    /** Technical information, shown on request. */
    details?: string;
    onDismiss?: () => void;
  } = $props();
</script>

<div class="notice error issue" role="alert">
  <div class="head">
    <strong>{summary}</strong>
    {#if onDismiss}
      <button class="button ghost small" type="button" onclick={onDismiss}>
        {t("common.dismiss")}
      </button>
    {/if}
  </div>
  {#if hint}
    <p>{hint}</p>
  {/if}
  {#if details}
    <details>
      <summary>{t("common.details")}</summary>
      <pre class="mono">{details}</pre>
    </details>
  {/if}
</div>

<style>
  .issue {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  p {
    margin: 0;
    color: var(--foreground);
  }

  summary {
    cursor: pointer;
    color: var(--muted);
    font-size: 0.8125rem;
  }

  pre {
    margin: 0.35rem 0 0;
    padding: 0.5rem;
    overflow-x: auto;
    border-radius: 6px;
    color: var(--foreground);
    background-color: var(--surface-soft);
    font-size: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
