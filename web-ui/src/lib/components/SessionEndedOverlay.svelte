<script>
  import { tick } from "svelte";

  import Button from "$lib/components/Button.svelte";

  let { open = false, cpOrigin = "", workspaceUrl = "" } = $props();

  let signInHref = $derived.by(() => {
    const base = String(cpOrigin ?? "")
      .trim()
      .replace(/\/+$/, "");
    const target = String(workspaceUrl ?? "").trim();
    if (!base || !target) {
      return `${base}/login`;
    }
    const params = new URLSearchParams();
    params.set("return_url", target);
    return `${base}/login?${params.toString()}`;
  });

  let primaryWrapEl = $state(null);
  let reduceMotion = $state(false);

  $effect(() => {
    if (!open || typeof window === "undefined") {
      return;
    }
    if (typeof window.matchMedia !== "function") {
      reduceMotion = false;
      return;
    }
    reduceMotion = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
  });

  $effect(() => {
    if (!open || typeof document === "undefined") {
      return;
    }
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  });

  $effect(() => {
    if (!open) {
      return;
    }
    void tick().then(() => {
      const el = primaryWrapEl?.querySelector?.("a, button");
      el?.focus?.();
    });

    function onTrapKeydown(e) {
      if (e.key === "Tab") {
        e.preventDefault();
      }
    }
    document.addEventListener("keydown", onTrapKeydown, true);
    return () => document.removeEventListener("keydown", onTrapKeydown, true);
  });
</script>

{#if open}
  <div
    class="session-ended-layer"
    class:session-ended-layer--motion={!reduceMotion}
  >
    <div class="session-ended-backdrop" aria-hidden="true"></div>
    <div
      class="session-ended-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="session-ended-title"
      tabindex="-1"
    >
      <div class="session-ended-inner">
        <div class="session-ended-icon" aria-hidden="true">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
            <polyline points="16 17 21 12 16 7" />
            <line x1="21" y1="12" x2="9" y2="12" />
          </svg>
        </div>
        <h2 id="session-ended-title" class="session-ended-title">
          Your session ended
        </h2>
        <p class="session-ended-body">
          You've been signed out. Click below to sign in again and return to
          this workspace.
        </p>
        <div class="session-ended-cta">
          <span bind:this={primaryWrapEl} class="session-ended-focus-wrap">
            <Button
              variant="primary"
              size="default"
              href={signInHref}
              class="session-ended-signin !w-full min-h-8 max-sm:min-h-10 max-sm:h-10 max-sm:text-body"
            >
              Sign in again
            </Button>
          </span>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .session-ended-layer {
    position: fixed;
    inset: 0;
    z-index: 9999;
    pointer-events: auto;
  }

  .session-ended-layer--motion {
    animation: session-ended-fade-in var(--motion-modal) forwards;
    opacity: 0;
  }

  @keyframes session-ended-fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .session-ended-layer--motion {
      animation: none;
      opacity: 1;
    }
  }

  .session-ended-backdrop {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
  }

  .session-ended-dialog {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    width: min(480px, calc(100vw - 48px));
    max-width: 480px;
    background: var(--panel, #161922);
    border: 1px solid var(--line, #2a2f3d);
    border-radius: var(--radius-lg, 10px);
    box-shadow: var(--shadow-modal);
    padding: 32px;
  }

  .session-ended-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
  }

  .session-ended-icon {
    display: flex;
    justify-content: center;
    color: var(--fg-muted, #a1a7b4);
    margin-bottom: 12px;
  }

  .session-ended-title {
    margin: 0 0 8px;
    font-size: 17px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--fg, #e8ebf1);
  }

  .session-ended-body {
    margin: 0;
    font-size: 15px;
    line-height: 1.5;
    color: var(--fg-muted, #a1a7b4);
    max-width: 100%;
  }

  .session-ended-cta {
    margin-top: 24px;
    width: 100%;
  }

  .session-ended-focus-wrap {
    display: block;
    width: 100%;
  }

  @media (max-width: 639px) {
    .session-ended-backdrop {
      display: none;
    }

    .session-ended-dialog {
      left: 0;
      top: 0;
      transform: none;
      width: 100%;
      max-width: none;
      height: 100%;
      border: none;
      border-radius: 0;
      box-shadow: none;
      padding: 24px;
      display: flex;
      flex-direction: column;
      align-items: stretch;
      justify-content: center;
    }

    .session-ended-inner {
      flex: 1;
      justify-content: center;
      padding-bottom: 64px;
      position: relative;
    }

    .session-ended-cta {
      position: absolute;
      left: 24px;
      right: 24px;
      bottom: 24px;
      margin-top: 0;
    }
  }
</style>
