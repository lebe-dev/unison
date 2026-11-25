<script lang="ts">
  import { groupDigits, sasEmoji } from "./countdown";

  let {
    digits,
    emoji,
    ariaLabel,
    layout = "hero",
    grouped = false,
  }: {
    digits: string;
    emoji: readonly string[];
    ariaLabel: string;
    /** Visual density and orientation of the verification code. */
    layout?: "hero" | "panel" | "compact" | "inline";
    /** Set when digits are already formatted for display. */
    grouped?: boolean;
  } = $props();

  const formattedDigits = $derived(grouped ? digits : groupDigits(digits));
  const symbols = $derived(sasEmoji(emoji));
</script>

{#if layout === "inline"}
  <span class="sas-inline" aria-label={ariaLabel}>
    <span class="digits mono">{formattedDigits}</span>
    <span class="emoji">{symbols.join(" ")}</span>
  </span>
{:else if layout === "compact"}
  <div class="sas-compact" aria-label={ariaLabel}>
    <span class="digits mono">{formattedDigits}</span>
    <span class="emoji">{symbols.join(" ")}</span>
  </div>
{:else}
  <div class="sas-box" class:panel={layout === "panel"} aria-label={ariaLabel}>
    <div class="digits mono">{formattedDigits}</div>
    <div class="emoji">
      {#each symbols as symbol, index (index)}
        <span>{symbol}</span>
      {/each}
    </div>
  </div>
{/if}

<style>
  .sas-box {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 20px 16px;
    border-radius: 10px;
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
    text-align: center;
  }

  .sas-box .digits {
    font-size: 36px;
    font-weight: 600;
    letter-spacing: 0.3rem;
  }

  .sas-box .emoji {
    display: flex;
    justify-content: center;
    gap: 12px;
    font-size: 32px;
    line-height: 1.2;
  }

  .sas-box.panel {
    gap: 6px;
    padding: 12px;
    background-color: var(--surface-soft);
    box-shadow: none;
  }

  .sas-box.panel .digits {
    font-size: 24px;
    letter-spacing: 0.2rem;
  }

  .sas-box.panel .emoji {
    gap: 10px;
    font-size: 22px;
  }

  .sas-compact {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 8px;
    background-color: var(--surface-soft);
  }

  .sas-compact .digits {
    font-size: 18px;
    font-weight: 600;
    letter-spacing: 0.12rem;
  }

  .sas-compact .emoji {
    font-size: 18px;
    line-height: 1.2;
  }

  .sas-inline {
    display: flex;
    align-items: baseline;
    gap: 8px;
    color: var(--foreground);
  }

  .sas-inline .digits {
    font-size: 16px;
    font-weight: 600;
    letter-spacing: 0.08rem;
  }

  .sas-inline .emoji {
    font-size: 15px;
  }
</style>
