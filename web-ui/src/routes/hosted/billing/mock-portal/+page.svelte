<script>
  import Button from "$lib/components/Button.svelte";
</script>

<div class="mx-auto max-w-[26rem] px-5 pt-6 pb-12 leading-normal text-fg">
  <h1 class="text-[1.35rem] font-semibold mb-2">Local Stripe mock</h1>
  <p class="mb-6 text-fg-muted text-[0.95rem]">
    The customer portal is simulated in local development. In production, Stripe
    hosts invoices, payment methods, and plan changes here.
  </p>
  <p class="text-[0.88rem] text-fg-muted mb-4">
    <Button variant="secondary" href="/hosted/organizations">Back to organizations</Button>
  </p>
</div>
