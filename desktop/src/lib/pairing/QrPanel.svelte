<script lang="ts">
  let { addr = "" }: { /** Address of the pairing listener. */ addr?: string } = $props();
</script>

<section class="panel qr">
  <h3>Pair with a phone</h3>
  <p class="muted">Pairing by QR code is not available in this version.</p>
  <div class="placeholder" aria-hidden="true">
    <span>QR</span>
  </div>
  {#if addr}
    <p class="muted">This device listens on <span class="mono">{addr}</span>.</p>
  {/if}
</section>

<style>
  .qr {
    align-items: center;
    text-align: center;
  }

  .placeholder {
    display: grid;
    place-items: center;
    width: 140px;
    height: 140px;
    border: 1px dashed var(--border);
    border-radius: 10px;
    color: var(--muted);
    background-color: var(--surface-soft);
    font-size: 2rem;
    letter-spacing: 0.2rem;
  }
</style>
