<script>
  /** Local mutation / validation surfaces; prefer over ad-hoc danger-soft blocks. */
  let { variant = "danger", class: className = "", children } = $props();
</script>

<div
  role="alert"
  class="ui-inline-alert ui-inline-alert--{variant} {variant === 'danger'
    ? 'border border-danger'
    : ''} {className}"
>
  {@render children?.()}
</div>
