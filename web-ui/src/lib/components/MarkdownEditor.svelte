<script>
  import { renderMarkdown } from "$lib/markdown.js";

  let {
    value = $bindable(""),
    placeholder = "Write markdown...",
    rows = 12,
    class: className = "",
    label = "",
    readonly = false,
  } = $props();

  let mode = $state("write");
  let textareaEl = $state(null);
  let previewHtml = $derived(renderMarkdown(value));

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
      icon: "⌜⌟",
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

  function applyAction(action) {
    if (readonly || !textareaEl) return;
    const el = textareaEl;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const selected = value.slice(start, end);
    let newText;
    let cursorStart;
    let cursorEnd;

    if (action.wrap) {
      const [before, after] = action.wrap;
      const inner = selected || action.placeholder;
      newText = `${before}${inner}${after}`;
      cursorStart = start + before.length;
      cursorEnd = cursorStart + inner.length;
    } else if (action.block) {
      const [before, after] = action.block;
      const inner = selected || action.placeholder;
      newText = `${before}${inner}${after}`;
      cursorStart = start + before.length;
      cursorEnd = cursorStart + inner.length;
    } else if (action.prefix) {
      const lineStart = value.lastIndexOf("\n", start - 1) + 1;
      const currentLine = value.slice(lineStart, end);
      if (selected.includes("\n")) {
        const lines = selected.split("\n");
        const prefixed = lines.map((l, i) => {
          const p = action.key === "ol" ? `${i + 1}. ` : action.prefix;
          return `${p}${l}`;
        });
        newText = prefixed.join("\n");
        cursorStart = start;
        cursorEnd = start + newText.length;
      } else if (currentLine.startsWith(action.prefix)) {
        value =
          value.slice(0, lineStart) +
          currentLine.slice(action.prefix.length) +
          value.slice(end);
        tick(el, lineStart, end - action.prefix.length);
        return;
      } else {
        const inner = selected || action.placeholder;
        value =
          value.slice(0, lineStart) +
          action.prefix +
          currentLine +
          value.slice(end);
        cursorStart = start + action.prefix.length;
        cursorEnd = end + action.prefix.length;
        tick(
          el,
          cursorStart,
          selected ? cursorEnd : cursorStart + inner.length,
        );
        return;
      }
    } else if (action.template) {
      const inner = selected || action.placeholder;
      newText = action.template.replace("${text}", inner);
      const urlIdx = newText.indexOf("(url)");
      if (urlIdx !== -1 && selected) {
        cursorStart = start + urlIdx + 1;
        cursorEnd = cursorStart + 3;
      } else {
        cursorStart = start + 1;
        cursorEnd = cursorStart + inner.length;
      }
    } else if (action.insert) {
      newText = action.insert;
      cursorStart = start + newText.length;
      cursorEnd = cursorStart;
    } else {
      return;
    }

    if (newText !== undefined) {
      value = value.slice(0, start) + newText + value.slice(end);
      tick(el, cursorStart, cursorEnd);
    }
  }

  function tick(el, selStart, selEnd) {
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(selStart, selEnd);
    });
  }

  function handleKeydown(e) {
    if (readonly) return;
    if (!(e.metaKey || e.ctrlKey)) {
      if (e.key === "Tab") {
        e.preventDefault();
        const el = textareaEl;
        const start = el.selectionStart;
        const end = el.selectionEnd;
        value = value.slice(0, start) + "  " + value.slice(end);
        tick(el, start + 2, start + 2);
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

<div class="md-editor {className}" class:md-editor--readonly={readonly}>
  {#if label}
    <span class="md-editor-label">{label}</span>
  {/if}

  <div class="md-editor-chrome">
    <div class="md-editor-toolbar">
      <div class="md-editor-tabs">
        <button
          class="md-editor-tab"
          class:md-editor-tab--active={mode === "write"}
          onclick={() => (mode = "write")}
          type="button">Write</button
        >
        <button
          class="md-editor-tab"
          class:md-editor-tab--active={mode === "preview"}
          onclick={() => (mode = "preview")}
          type="button">Preview</button
        >
      </div>

      {#if mode === "write" && !readonly}
        <div class="md-editor-actions">
          {#each TOOLBAR as action}
            {#if action.key?.startsWith("sep")}
              <span class="md-editor-sep"></span>
            {:else}
              <button
                class="md-editor-btn"
                class:md-editor-btn--bold={action.key === "bold"}
                class:md-editor-btn--italic={action.key === "italic"}
                class:md-editor-btn--strike={action.key === "strike"}
                onclick={() => applyAction(action)}
                title="{action.label}{action.shortcut
                  ? ` (${navigator?.platform?.includes('Mac') ? '⌘' : 'Ctrl+'}${action.shortcut.toUpperCase()})`
                  : ''}"
                aria-label="{action.label}{action.shortcut
                  ? ` (${navigator?.platform?.includes('Mac') ? '⌘' : 'Ctrl+'}${action.shortcut.toUpperCase()})`
                  : ''}"
                type="button">{action.icon}</button
              >
            {/if}
          {/each}
        </div>
      {/if}
    </div>

    {#if mode === "write"}
      <textarea
        bind:this={textareaEl}
        bind:value
        class="md-editor-textarea"
        onkeydown={handleKeydown}
        {placeholder}
        {readonly}
        {rows}
      ></textarea>
    {:else}
      <div class="md-editor-preview markdown-rendered">
        {#if previewHtml}
          <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
          {@html previewHtml}
        {:else}
          <p class="md-editor-empty">Nothing to preview</p>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .md-editor {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .md-editor-label {
    font-size: 12px;
    font-weight: 500;
    color: #9ca1ab;
  }

  .md-editor-chrome {
    border: 1px solid var(--line);
    border-radius: 0.375rem;
    background: var(--bg);
    overflow: hidden;
  }

  .md-editor-chrome:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(129, 140, 248, 0.2);
  }

  .md-editor--readonly .md-editor-chrome:focus-within {
    border-color: var(--line);
    box-shadow: none;
  }

  .md-editor-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
    padding: 0 0.5rem;
    min-height: 2.25rem;
    flex-wrap: wrap;
  }

  .md-editor-tabs {
    display: flex;
    gap: 0;
  }

  .md-editor-tab {
    border: none;
    background: none;
    padding: 0.375rem 0.625rem;
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-muted);
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition:
      color 100ms,
      border-color 100ms;
  }

  .md-editor-tab:hover {
    color: var(--fg);
  }

  .md-editor-tab--active {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  .md-editor-actions {
    display: flex;
    align-items: center;
    gap: 1px;
    flex-wrap: wrap;
    padding: 0.125rem 0;
  }

  .md-editor-sep {
    width: 1px;
    height: 1rem;
    background: var(--line-strong);
    margin: 0 0.25rem;
  }

  .md-editor-btn {
    border: none;
    background: none;
    padding: 0.1875rem 0.375rem;
    font-size: 11px;
    font-family:
      ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    color: var(--fg-muted);
    border-radius: 0.25rem;
    cursor: pointer;
    line-height: 1.4;
    transition:
      background 80ms,
      color 80ms;
    white-space: nowrap;
  }

  .md-editor-btn:hover {
    background: var(--line);
    color: var(--fg);
  }

  .md-editor-btn--bold {
    font-weight: 700;
    font-family: inherit;
  }

  .md-editor-btn--italic {
    font-style: italic;
    font-family: inherit;
  }

  .md-editor-btn--strike {
    text-decoration: line-through;
    font-family: inherit;
  }

  .md-editor-textarea {
    display: block;
    width: 100%;
    min-height: 8rem;
    border: none;
    background: var(--bg);
    color: var(--fg);
    padding: 0.625rem 0.75rem;
    font-size: 13px;
    font-family:
      ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    line-height: 1.6;
    resize: vertical;
    outline: none;
    tab-size: 2;
  }

  .md-editor-textarea::placeholder {
    color: var(--fg-subtle);
  }

  .md-editor-preview {
    min-height: 8rem;
    padding: 0.625rem 0.75rem;
    font-size: 13px;
    color: #d0d4db;
    overflow: auto;
  }

  .md-editor-empty {
    color: var(--fg-subtle);
    font-style: italic;
    margin: 0;
  }
</style>
