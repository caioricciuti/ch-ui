<script lang="ts">
  import { Toaster } from 'svelte-sonner'
  import { getTheme } from '../../stores/theme.svelte'
</script>

<!-- Sonner defaults to light; its rich colours follow this, not the app tokens. -->
<Toaster
  theme={getTheme()}
  position="bottom-right"
  richColors={true}
  closeButton={true}
  visibleToasts={5}
  expand={true}
  duration={4400}
  offset={{ bottom: 48, right: 16 }}
  mobileOffset={12}
  gap={8}
  containerAriaLabel="CH-UI notifications"
/>

<style>
  :global([data-sonner-toaster]) {
    --width: min(92vw, 24rem);
    font-family: var(--font-sans);
  }

  :global([data-sonner-toast][data-styled='true']) {
    border-radius: var(--radius-md);
    border: 1px solid var(--edge);
    background: var(--elevated);
    color: var(--fg);
    box-shadow: var(--shadow-popover);
  }

  :global([data-sonner-toast][data-styled='true'] [data-title]) {
    font-size: 13px;
    font-weight: 600;
    line-height: 1.35;
  }

  :global([data-sonner-toast][data-styled='true'] [data-description]) {
    font-size: 12px;
    line-height: 1.4;
    color: var(--fg-3);
  }

  :global([data-sonner-toast][data-styled='true'] [data-close-button]) {
    opacity: 0.7;
  }

  :global([data-sonner-toast][data-styled='true'] [data-close-button]:hover) {
    opacity: 1;
  }
</style>
