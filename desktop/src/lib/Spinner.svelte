<script lang="ts">
  let { size = 18 }: { /** Diameter of the spinner in pixels. */ size?: number } = $props();
</script>

<span class="spinner" aria-hidden="true" style:width="{size}px" style:height="{size}px"></span>

<style>
  .spinner {
    display: inline-block;
    flex: none;
    box-sizing: border-box;
    color: var(--muted);
    border: 2px solid color-mix(in srgb, currentColor 25%, transparent);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 800ms linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 2400ms;
    }
  }
</style>
