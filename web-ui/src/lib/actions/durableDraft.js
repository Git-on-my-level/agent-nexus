import { tick } from "svelte";
import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import {
  loadLocalWork,
  localWorkPrincipal,
  saveLocalWork,
} from "$lib/localWorkStore.js";

/** Explicit target keys, shared by panes and detail pages. DOM restoration
 * dispatches input so Svelte's bound state and the visible text stay aligned. */
export function durableDraft(node, target) {
  let scope = "";
  let generation = 0;
  let edited = false;
  let restoring = false;
  let destroyed = false;

  async function restore() {
    const version = ++generation;
    edited = false;
    if (!target || !canUseLocalWorkScope(scope)) return;
    const value = await loadLocalWork(scope, "draft", target);
    if (destroyed || version !== generation || edited || value === null) return;
    restoring = true;
    node.value = String(value);
    node.dispatchEvent(new Event("input", { bubbles: true }));
    restoring = false;
  }

  function save() {
    if (restoring || !target || !canUseLocalWorkScope(scope)) return;
    edited = true;
    const version = generation;
    void saveLocalWork(scope, "draft", target, node.value).then(
      () => {
        if (version === generation)
          node.removeAttribute("data-local-save-error");
      },
      (error) => {
        if (version === generation) {
          node.setAttribute("data-local-save-error", error.message);
          node.dispatchEvent(
            new CustomEvent("local-save-error", {
              detail: error.message,
              bubbles: true,
            }),
          );
        }
      },
    );
  }

  function edit(event) {
    if (event.detail?.scope !== scope || event.detail?.target !== target)
      return;
    node.value = event.detail.text;
    node.dispatchEvent(new Event("input", { bubbles: true }));
    const version = generation;
    void tick().then(() => {
      if (!destroyed && version === generation) node.focus();
    });
  }
  function purge(event) {
    if (event.detail?.principal !== localWorkPrincipal(scope)) return;
    generation += 1;
    restoring = true;
    node.value = "";
    node.dispatchEvent(new Event("input", { bubbles: true }));
    restoring = false;
  }
  node.addEventListener("input", save);
  window.addEventListener("anx:edit-draft", edit);
  window.addEventListener("anx:local-work-purged", purge);
  const unsubscribe = localWorkScope.subscribe((next) => {
    const switchedPrincipal = scope && scope !== next;
    scope = next;
    if (switchedPrincipal) {
      restoring = true;
      node.value = "";
      node.dispatchEvent(new Event("input", { bubbles: true }));
      restoring = false;
    }
    void restore();
  });
  return {
    update(next) {
      if (next === target) return;
      target = next;
      void restore();
    },
    destroy() {
      destroyed = true;
      generation += 1;
      unsubscribe();
      node.removeEventListener("input", save);
      window.removeEventListener("anx:edit-draft", edit);
      window.removeEventListener("anx:local-work-purged", purge);
    },
  };
}
