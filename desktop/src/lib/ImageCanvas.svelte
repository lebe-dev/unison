<script lang="ts">
  import type { Snippet } from "svelte";
  import { loadThumbnail, type Pixels } from "./thumbnails";

  interface Props {
    /** Id of the image item. */
    id: string;
    /** Longer side of the pixels asked from the daemon. */
    maxSide: number;
    /** Accessible name of the image. */
    label?: string;
    /** Content shown while the thumbnail is loading. */
    loading?: Snippet;
    /** Content shown when loading finished without pixels. */
    fallback?: Snippet;
  }

  let { id, maxSide, label = "", loading, fallback }: Props = $props();

  let canvas: HTMLCanvasElement | null = $state(null);
  let pixels = $state<Pixels | null>(null);
  let pending = $state(true);

  $effect(() => {
    let current = true;
    pixels = null;
    pending = true;
    void loadThumbnail(id, maxSide).then((loaded) => {
      if (current) {
        pixels = loaded;
        pending = false;
      }
    });
    return () => {
      current = false;
    };
  });

  /** draw puts the pixels on the canvas. */
  function draw(target: HTMLCanvasElement, image: Pixels): void {
    target.width = image.width;
    target.height = image.height;
    const context: CanvasRenderingContext2D | null = target.getContext("2d");
    context?.putImageData(new ImageData(image.rgba, image.width, image.height), 0, 0);
  }

  $effect(() => {
    if (canvas && pixels) {
      draw(canvas, pixels);
    }
  });
</script>

{#if pixels}
  <canvas bind:this={canvas} aria-label={label}>{label}</canvas>
{:else if pending}
  {#if loading}
    {@render loading()}
  {/if}
{:else if fallback}
  {@render fallback()}
{/if}
