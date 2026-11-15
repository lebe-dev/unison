<script lang="ts">
  let { title, description }: { title: string; description: string } = $props();
</script>

<section class="placeholder">
  <h3>{title}</h3>
  <p>{description}</p>
</section>

<style>
  h3 {
    margin: 0 0 0.25rem;
    font-size: 1rem;
    font-weight: 600;
  }

  p {
    margin: 0;
    color: var(--muted);
  }
</style>
