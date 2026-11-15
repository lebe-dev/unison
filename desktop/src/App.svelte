<script lang="ts">
  const status = $state({ devices: 0, paused: false });
</script>

<main>
  <h1>Unison</h1>
  <p class="status">
    {status.devices} devices connected{status.paused ? ", paused" : ""}
  </p>
  <p class="hint">The daemon is not connected yet.</p>
</main>
