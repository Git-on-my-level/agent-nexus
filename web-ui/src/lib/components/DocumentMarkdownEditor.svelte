<!--
  Document body editor for the docs detail page.

  A Google-Docs / Notion-flavoured Markdown editor: a formatting toolbar, a
  Write / Split / Preview mode switch, live preview powered by the shared
  `renderMarkdown`, and a footer that surfaces dirty / saving state plus the
  base revision used for optimistic concurrency. It stays a thin presentational
  layer: the page owns `value` (bindable), the save action, and the
  `if_base_revision` write so storage semantics are unchanged.
-->
<script>
  import { renderMarkdown } from "$lib/markdown.js";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import Button from "$lib/components/Button.svelte";
  import Icon from "$lib/components/Icon.svelte";

  let {
    value = $bindable(""),
    placeholder = "Write in Markdown…",
    saving = false,
    saveError = "",
    dirty = false,
    baseRevisionId = "",
    onsave = () => {},
    oncancel = () => {},
  } = $props();

  /** @type {"write" | "split" | "preview"} */
  let mode = $state("split");
  let textareaEl = $state(null);
  let previewHtml = $derived(renderMarkdown(value));

  const MODES = [
    { value: "write", label: "Write" },
    { value: "split", label: "Split" },
    { value: "preview", label: "Preview" },
  ];

  const TOOLBAR = [
    {
      key: "bold",
      icon: "B",
      label: "Bold",
      shortcut: "b",
      wrap: ["**", "**"],
      placeholder: "bold text",
    },
    {
      key: "italic",
      icon: "I",
      label: "Italic",
      shortcut: "i",
      wrap: ["_", "_"],
      placeholder: "italic text",
    },
    {
      key: "strike",
      icon: "S",
      label: "Strikethrough",
      wrap: ["~~", "~~"],
      placeholder: "strikethrough",
    },
    { key: "sep1" },
    {
      key: "h1",
      icon: "H1",
      label: "Heading 1",
      prefix: "# ",
      placeholder: "Heading",
    },
    {
      key: "h2",
      icon: "H2",
      label: "Heading 2",
      prefix: "## ",
      placeholder: "Heading",
    },
    {
      key: "h3",
      icon: "H3",
      label: "Heading 3",
      prefix: "### ",
      placeholder: "Heading",
    },
    { key: "sep2" },
    {
      key: "ul",
      icon: "•",
      label: "Bullet list",
      prefix: "- ",
      placeholder: "List item",
    },
    {
      key: "ol",
      icon: "1.",
      label: "Numbered list",
      prefix: "1. ",
      placeholder: "List item",
    },
    {
      key: "task",
      icon: "☐",
      label: "Task list",
      prefix: "- [ ] ",
      placeholder: "Task",
    },
    { key: "sep3" },
    {
      key: "code",
      icon: "<>",
      label: "Inline code",
      shortcut: "e",
      wrap: ["`", "`"],
      placeholder: "code",
    },
    {
      key: "codeblock",
      icon: "⌞⌝",
      label: "Code block",
      block: ["```\n", "\n```"],
      placeholder: "code",
    },
    {
      key: "quote",
      icon: "❝",
      label: "Blockquote",
      prefix: "> ",
      placeholder: "quote",
    },
    { key: "sep4" },
    {
      key: "link",
      icon: "🔗",
      label: "Link",
      shortcut: "k",
      template: "[${text}](url)",
      placeholder: "link text",
    },
    { key: "hr", icon: "—", label: "Horizontal rule", insert: "\n---\n" },
  ];

  function shortcutHint(action) {
    if (!action.shortcut) return "";
    return ` (${formatShortcut(action.shortcut.toUpperCase())})`;
  }

  function focusTextarea() {
    if (mode === "preview") mode = "split";
    requestAnimationFrame(() => textareaEl?.focus());
  }

  /**
   * Apply a single text edit through the browser's native editing pipeline so
   * it joins the textarea undo stack (⌘Z works). `document.execCommand`
   * ("insertText") is deprecated but is still the only widely supported way to
   * insert/replace text in a `<textarea>` while preserving undo history.
   * Falls back to a direct value mutation (no undo) only if execCommand fails.
   *
   * @param {HTMLTextAreaElement} el
   * @param {{ from: number, to: number, text: string, selStart: number, selEnd: number }} edit
   */
  function performEdit(el, { from, to, text, selStart, selEnd }) {
    el.focus();
    el.setSelectionRange(from, to);
    let inserted = false;
    try {
      inserted = document.execCommand("insertText", false, text);
    } catch {
      inserted = false;
    }
    if (!inserted) {
      value = value.slice(0, from) + text + value.slice(to);
    }
    // The execCommand `input` updates the bound value; restoring the selection
    // on the next frame survives that reactive pass.
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(selStart, selEnd);
    });
  }

  /**
   * Compute the range to replace, the replacement text, and the resulting
   * selection for a toolbar action. Returns null for a no-op.
   *
   * @returns {{ from: number, to: number, text: string, selStart: number, selEnd: number } | null}
   */
  function computeEdit(action, source, start, end) {
    const selected = source.slice(start, end);

    if (action.wrap || action.block) {
      const [before, after] = action.wrap ?? action.block;
      const inner = selected || action.placeholder;
      const text = `${before}${inner}${after}`;
      const selStart = start + before.length;
      return {
        from: start,
        to: end,
        text,
        selStart,
        selEnd: selStart + inner.length,
      };
    }

    if (action.template) {
      const inner = selected || action.placeholder;
      const text = action.template.replace("${text}", inner);
      const urlIdx = text.indexOf("(url)");
      if (urlIdx !== -1 && selected) {
        return {
          from: start,
          to: end,
          text,
          selStart: start + urlIdx + 1,
          selEnd: start + urlIdx + 4,
        };
      }
      return {
        from: start,
        to: end,
        text,
        selStart: start + 1,
        selEnd: start + 1 + inner.length,
      };
    }

    if (action.insert) {
      const text = action.insert;
      return {
        from: start,
        to: end,
        text,
        selStart: start + text.length,
        selEnd: start + text.length,
      };
    }

    if (action.prefix) {
      const isOrdered = action.key === "ol";
      const matchPrefix = (line) =>
        isOrdered
          ? (line.match(/^\d+\.\s/)?.[0] ?? null)
          : line.startsWith(action.prefix)
            ? action.prefix
            : null;

      const blockStart = source.lastIndexOf("\n", start - 1) + 1;
      const nlIdx = source.indexOf("\n", end);
      const blockEnd = nlIdx === -1 ? source.length : nlIdx;
      const block = source.slice(blockStart, blockEnd);
      const lines = block.split("\n");
      const multiLine = start !== end;

      // Toggle off when every affected line already carries the prefix.
      if (lines.every((line) => matchPrefix(line) !== null)) {
        const stripped = lines.map((line) =>
          line.slice(matchPrefix(line).length),
        );
        const text = stripped.join("\n");
        if (!multiLine) {
          const removed = matchPrefix(lines[0]).length;
          const caret = Math.max(blockStart, start - removed);
          return {
            from: blockStart,
            to: blockEnd,
            text,
            selStart: caret,
            selEnd: caret,
          };
        }
        return {
          from: blockStart,
          to: blockEnd,
          text,
          selStart: blockStart,
          selEnd: blockStart + text.length,
        };
      }

      // Caret on an empty line: drop in a placeholder so there is something to type over.
      if (!multiLine && block.trim().length === 0) {
        const pfx = isOrdered ? "1. " : action.prefix;
        const text = `${pfx}${action.placeholder}`;
        return {
          from: blockStart,
          to: blockEnd,
          text,
          selStart: blockStart + pfx.length,
          selEnd: blockStart + text.length,
        };
      }

      const prefixed = lines.map((line, i) =>
        isOrdered ? `${i + 1}. ${line}` : `${action.prefix}${line}`,
      );
      const text = prefixed.join("\n");
      if (!multiLine) {
        // Caret on a non-empty line: insert the prefix and keep the caret in
        // place (shifted by the prefix) rather than selecting trailing text.
        const pfxLen = isOrdered ? 3 : action.prefix.length;
        const caret = start + pfxLen;
        return {
          from: blockStart,
          to: blockEnd,
          text,
          selStart: caret,
          selEnd: caret,
        };
      }
      return {
        from: blockStart,
        to: blockEnd,
        text,
        selStart: blockStart,
        selEnd: blockStart + text.length,
      };
    }

    return null;
  }

  function applyAction(action) {
    const el = textareaEl;
    if (!el) return;
    if (mode === "preview") mode = "split";
    const start = el.selectionStart ?? value.length;
    const end = el.selectionEnd ?? start;
    const edit = computeEdit(action, value, start, end);
    if (!edit) return;
    performEdit(el, edit);
  }

  function handleKeydown(e) {
    const mod = e.metaKey || e.ctrlKey;
    if (mod && e.key === "Enter") {
      e.preventDefault();
      if (!saving && dirty) onsave();
      return;
    }
    if (!mod) {
      if (e.key === "Tab" && textareaEl) {
        e.preventDefault();
        const el = textareaEl;
        const start = el.selectionStart;
        performEdit(el, {
          from: start,
          to: el.selectionEnd,
          text: "  ",
          selStart: start + 2,
          selEnd: start + 2,
        });
      }
      return;
    }
    for (const action of TOOLBAR) {
      if (action.shortcut && action.shortcut === e.key.toLowerCase()) {
        e.preventDefault();
        applyAction(action);
        return;
      }
    }
  }
</script>

<div
  class="overflow-hidden rounded-lg border border-line bg-bg shadow-sm"
  data-anx-save-scope
>
  <div
    class="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-line bg-panel px-2 py-1.5"
  >
    <div
      class="flex items-center rounded-md bg-bg-soft p-0.5"
      role="tablist"
      aria-label="Editor mode"
    >
      {#each MODES as m (m.value)}
        <button
          type="button"
          role="tab"
          aria-selected={mode === m.value}
          class="rounded px-2 py-1 text-micro font-medium transition-colors {mode ===
          m.value
            ? 'bg-panel text-fg shadow-sm'
            : 'text-fg-muted hover:text-fg'}"
          onclick={() => {
            mode = m.value;
            if (m.value !== "preview") focusTextarea();
          }}
        >
          {m.label}
        </button>
      {/each}
    </div>

    {#if mode !== "preview"}
      <div class="flex flex-wrap items-center gap-0.5">
        {#each TOOLBAR as action (action.key)}
          {#if action.key.startsWith("sep")}
            <span class="mx-1 h-4 w-px bg-line-strong" aria-hidden="true"
            ></span>
          {:else}
            <button
              type="button"
              class="rounded px-1.5 py-1 font-mono text-[11px] leading-none text-fg-muted transition-colors hover:bg-line hover:text-fg {action.key ===
              'bold'
                ? 'font-sans font-bold'
                : ''} {action.key === 'italic'
                ? 'font-sans italic'
                : ''} {action.key === 'strike' ? 'font-sans line-through' : ''}"
              title={`${action.label}${shortcutHint(action)}`}
              aria-label={`${action.label}${shortcutHint(action)}`}
              onclick={() => applyAction(action)}
            >
              {action.icon}
            </button>
          {/if}
        {/each}
      </div>
    {/if}
  </div>

  <div class="flex {mode === 'split' ? 'flex-col lg:flex-row' : ''}">
    {#if mode !== "preview"}
      <textarea
        bind:this={textareaEl}
        bind:value
        class="block min-h-[24rem] w-full resize-y border-none bg-bg px-4 py-3 font-mono text-meta leading-relaxed text-fg outline-none placeholder:text-fg-subtle {mode ===
        'split'
          ? 'lg:w-1/2 lg:border-r lg:border-line'
          : ''}"
        onkeydown={handleKeydown}
        {placeholder}
        rows="22"
        aria-label="Content (Markdown)"
      ></textarea>
    {/if}
    {#if mode !== "write"}
      <div
        class="min-h-[24rem] w-full overflow-auto px-5 py-4 {mode === 'split'
          ? 'lg:w-1/2 border-t border-line lg:border-t-0'
          : ''}"
      >
        {#if previewHtml}
          <div class="markdown-rendered markdown-rendered--doc text-fg">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
            {@html previewHtml}
          </div>
        {:else}
          <p class="text-meta italic text-fg-subtle">Nothing to preview yet.</p>
        {/if}
      </div>
    {/if}
  </div>

  <div
    class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-t border-line bg-panel px-3 py-2"
  >
    <div class="flex min-w-0 items-center gap-2 text-micro text-fg-muted">
      {#if saving}
        <span class="inline-flex items-center gap-1.5 text-fg">
          <svg class="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          Saving…
        </span>
      {:else if dirty}
        <span class="inline-flex items-center gap-1.5 text-warn-text">
          <span class="h-1.5 w-1.5 rounded-full bg-warn-text" aria-hidden="true"
          ></span>
          Unsaved changes
        </span>
      {:else}
        <span class="inline-flex items-center gap-1.5">
          <Icon name="check" class="h-3.5 w-3.5 text-ok-text" />
          Saved
        </span>
      {/if}
      {#if baseRevisionId}
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <span class="truncate" title={`Base revision ${baseRevisionId}`}>
          Base <span class="font-mono">{baseRevisionId}</span>
        </span>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      <Button
        variant="secondary"
        size="compact"
        type="button"
        onclick={oncancel}
        title={`Cancel (${formatShortcut("Esc")})`}
      >
        Cancel
      </Button>
      <Button
        variant="primary"
        size="compact"
        type="button"
        disabled={saving || !dirty}
        onclick={onsave}
        saveShortcut
        aria-keyshortcuts="Meta+S Control+S"
        title={`Save revision (${formatShortcut("S")}, ${formatShortcut("Enter")})`}
      >
        {saving ? "Saving…" : "Save revision"}
      </Button>
    </div>
  </div>

  {#if saveError}
    <div
      class="border-t border-danger bg-danger-soft px-3 py-2 text-micro text-danger-text"
      role="alert"
    >
      {saveError}
    </div>
  {/if}
</div>
