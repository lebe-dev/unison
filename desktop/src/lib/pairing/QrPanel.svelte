<script lang="ts">
  import { toString as qrToString } from "qrcode";

  let {
    uri = "",
  }: { /** Text of the pairing code, empty without an open window. */ uri?: string } = $props();

  let drawing = $state("");
  let failure = $state("");
  let copied = $state(false);

  $effect(() => {
    const text = uri;
    drawing = "";
    failure = "";
    copied = false;
    if (!text) {
      return;
    }

    let active = true;
    qrToString(text, { type: "svg", margin: 1, errorCorrectionLevel: "M" })
      .then((svg) => {
        if (!active) {
          return;
        }
        // The xml header of the drawing is not part of the markup of the page.
        const start = svg.indexOf("<svg");
        drawing = start > 0 ? svg.slice(start) : svg;
      })
      .catch((error: unknown) => {
        if (active) {
          failure = error instanceof Error ? error.message : "The code could not be drawn.";
        }
      });
    return () => {
      active = false;
    };
  });

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(uri);
      copied = true;
    } catch {
      copied = false;
    }
  }
</script>

<section class="panel qr">
  <h3>Pair with a phone</h3>
  {#if !uri}
    <p class="muted">Open the pairing window to show a square that another device can scan.</p>
  {:else}
    <p class="muted">
      Scan the square with the other device. The code works once and only while the window is open.
    </p>
    <div class="code" aria-label="Pairing code">
      {#if drawing}
        <!-- The markup comes from the local QR encoder. -->
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html drawing}
      {:else if failure}
        <p class="notice error">{failure}</p>
      {:else}
        <p class="muted">Drawing the code...</p>
      {/if}
    </div>
    <div class="toolbar">
      <button class="button ghost small" type="button" onclick={() => void copy()}>Copy code</button
      >
      {#if copied}
        <span class="muted">Copied.</span>
      {/if}
    </div>
    <details>
      <summary>Code as text</summary>
      <p class="mono code-text">{uri}</p>
    </details>
  {/if}
</section>

<style>
  .qr {
    align-items: center;
    text-align: center;
  }

  .code {
    display: grid;
    place-items: center;
    padding: 12px;
    border-radius: 12px;
    background-color: #ffffff;
    box-shadow: var(--box-shadow);
  }

  .code :global(svg) {
    width: 200px;
    height: 200px;
    max-width: 100%;
  }

  .code-text {
    margin: 0;
    overflow-wrap: anywhere;
    font-size: 0.75rem;
    text-align: left;
  }

  details {
    width: 100%;
    text-align: left;
  }

  summary {
    color: var(--muted);
    font-size: 12px;
  }
</style>
