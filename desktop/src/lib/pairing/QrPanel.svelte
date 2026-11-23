<script lang="ts">
  import { toString as qrToString } from "qrcode";
  import { t } from "../i18n";

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
          failure = error instanceof Error ? error.message : t("pairing.drawFailed");
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
  <h3>{t("pairing.qrHeading")}</h3>
  {#if !uri}
    <p class="muted">{t("pairing.qrClosed")}</p>
  {:else}
    <p class="muted">{t("pairing.qrHint")}</p>
    <div class="code" aria-label={t("pairing.codeAria")}>
      {#if drawing}
        <!-- The markup comes from the local QR encoder. -->
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html drawing}
      {:else if failure}
        <p class="notice error">{failure}</p>
      {:else}
        <p class="muted">{t("pairing.drawing")}</p>
      {/if}
    </div>
    <div class="toolbar">
      <button class="button ghost small" type="button" onclick={() => void copy()}>
        {t("pairing.copyCode")}
      </button>
      {#if copied}
        <span class="muted">{t("pairing.copied")}</span>
      {/if}
    </div>
    <details>
      <summary>{t("pairing.codeAsText")}</summary>
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
