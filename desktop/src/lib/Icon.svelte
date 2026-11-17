<script lang="ts" module>
  /** Names of the line icons drawn in the style of SF Symbols. */
  export type IconName =
    | "devices"
    | "add"
    | "requests"
    | "transfers"
    | "history"
    | "settings"
    | "laptop"
    | "desktop"
    | "phone"
    | "person"
    | "file"
    | "clipboard"
    | "image"
    | "plus"
    | "close"
    | "refresh"
    | "upload"
    | "shield"
    | "clock";
</script>

<script lang="ts">
  let {
    name,
    size = 16,
    stroke = 1.4,
  }: {
    /** Icon to draw. */
    name: IconName;
    /** Width and height in pixels. */
    size?: number;
    /** Width of the line in units of the 16 point grid. */
    stroke?: number;
  } = $props();
</script>

<svg
  class="icon"
  aria-hidden="true"
  width={size}
  height={size}
  viewBox="0 0 16 16"
  fill="none"
  stroke="currentColor"
  stroke-width={stroke}
  stroke-linecap="round"
  stroke-linejoin="round"
>
  {#if name === "devices"}
    <rect x="2" y="3" width="9" height="6.5" rx="1" />
    <path d="M1 11.5h11" />
    <rect x="11.5" y="6" width="3.5" height="7" rx="0.8" />
  {:else if name === "add"}
    <circle cx="6.5" cy="5.5" r="2.8" />
    <path d="M1.5 14c.6-2.8 2.6-4.3 5-4.3 1.2 0 2.3.4 3.1 1M12.5 9.5v4.5M10.25 11.75h4.5" />
  {:else if name === "requests"}
    <path d="M3 3.5h10v7H9.5L6 13.5v-3H3z" />
    <path d="M6 7h4" />
  {:else if name === "transfers"}
    <path d="M5 13V3M2.5 5.5 5 3l2.5 2.5" />
    <path d="M11 3v10M8.5 10.5 11 13l2.5-2.5" />
  {:else if name === "history" || name === "clock"}
    <circle cx="8" cy="8" r="6" />
    <path d="M8 4.5V8l2.5 1.5" />
  {:else if name === "settings"}
    <circle cx="8" cy="8" r="2.2" />
    <path
      d="M8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4"
    />
  {:else if name === "laptop"}
    <rect x="2" y="3" width="12" height="7.5" rx="1" />
    <path d="M1 12.5h14" />
  {:else if name === "desktop"}
    <rect x="1.5" y="2.5" width="13" height="8.5" rx="1" />
    <path d="M6 14h4M8 11v3" />
  {:else if name === "phone"}
    <rect x="4.5" y="1.5" width="7" height="13" rx="1.6" />
    <path d="M7 12.5h2" />
  {:else if name === "person"}
    <circle cx="8" cy="5.5" r="2.8" />
    <path d="M2.5 14c.6-2.8 2.8-4.3 5.5-4.3s4.9 1.5 5.5 4.3" />
  {:else if name === "file"}
    <path d="M4 2h5l3 3v9H4z" />
    <path d="M9 2v3h3" />
  {:else if name === "clipboard"}
    <rect x="4" y="2.5" width="8" height="11.5" rx="1.2" />
    <path d="M6 2.5V1.8h4v.7M6 7h4M6 9.5h3" />
  {:else if name === "image"}
    <rect x="2" y="3" width="12" height="10" rx="1.5" />
    <circle cx="5.75" cy="6.5" r="1.1" />
    <path d="m2.5 12 3.5-3.5 2.5 2.5 2-2 3 3" />
  {:else if name === "plus"}
    <path d="M8 3v10M3 8h10" />
  {:else if name === "close"}
    <path d="m4.5 4.5 7 7M11.5 4.5l-7 7" />
  {:else if name === "refresh"}
    <path d="M13 8a5 5 0 1 1-1.5-3.6" />
    <path d="M13 2.5v3h-3" />
  {:else if name === "upload"}
    <path d="M8 11V3M4.5 6.5 8 3l3.5 3.5" />
    <path d="M2.5 10.5v2.5h11v-2.5" />
  {:else if name === "shield"}
    <path d="M8 1.8 13.5 4v4c0 3.2-2.3 5.4-5.5 6.2C4.8 13.4 2.5 11.2 2.5 8V4z" />
    <path d="m5.8 8 1.6 1.6L10.4 6.5" />
  {/if}
</svg>
