<script>
  import Button from "$lib/components/Button.svelte";
</script>

<div class="hosted-page hosted-page--narrow">
  <h1 class="hosted-title">Local Stripe mock</h1>
  <p class="hosted-sub">
    The customer portal is simulated in local development. In production, Stripe
    hosts invoices, payment methods, and plan changes here.
  </p>
  <p class="hosted-hint">
    <Button variant="secondary" href="/hosted/organizations">Back to organizations</Button>
  </p>
</div>
