<script>
  import { page } from "$app/stores";
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let status = $state("working");

  onMount(async () => {
    const sessionId = String(
      $page.url.searchParams.get("session_id") ?? "",
    ).trim();
    if (!sessionId) {
      status = "bad";
      await goto("/hosted/organizations?billing_error=1");
      return;
    }
    const res = await hostedCpFetch(
      `billing/checkout-session/${encodeURIComponent(sessionId)}`,
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!res.ok) {
      status = "bad";
      await goto("/hosted/organizations?billing_error=1");
      return;
    }
    let body;
    try {
      body = await res.json();
    } catch {
      status = "bad";
      await goto("/hosted/organizations?billing_error=1");
      return;
    }
    const organizationId = String(body.organization_id ?? "").trim();
    if (!organizationId) {
      status = "bad";
      await goto("/hosted/organizations?billing_error=1");
      return;
    }
    // Local Stripe mock has no webhooks; activate plan the same way as billing-local-smoke.
    if (sessionId.startsWith("cs_mock_")) {
      await hostedCpFetch(
        `organizations/${encodeURIComponent(organizationId)}/billing/mock-checkout-complete`,
        {
          method: "POST",
          body: JSON.stringify({ session_id: sessionId }),
        },
      );
    }
    status = "ok";
    await goto(
      `/hosted/organizations/${encodeURIComponent(organizationId)}/billing?activating=1`,
    );
  });
</script>

<div class="mx-auto max-w-[26rem] px-5 pt-6 pb-12 leading-normal text-fg">
  <h1 class="text-[1.35rem] font-semibold mb-2">Returning from checkout</h1>
  {#if status === "working"}
    <p class="text-fg-muted">Redirecting…</p>
  {:else}
    <p class="text-fg-muted">Redirecting…</p>
  {/if}
</div>
