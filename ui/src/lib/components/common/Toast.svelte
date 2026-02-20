<script lang="ts">
  import { Toaster } from 'svelte-sonner'
</script>

<Toaster
  position="top-right"
  richColors={true}
  closeButton={true}
  visibleToasts={5}
  expand={true}
  duration={4400}
  offset={16}
  mobileOffset={12}
  gap={10}
  containerAriaLabel="CH-UI notifications"
/>

<style>
  :global([data-sonner-toaster]) {
    --width: min(92vw, 26rem);
  }

  :global([data-sonner-toast][data-styled='true']) {
    border-radius: 12px;
    backdrop-filter: blur(8px);
    box-shadow: 0 14px 30px rgba(0, 0, 0, 0.28);
  }

  :global([data-sonner-toast][data-styled='true'] [data-title]) {
    font-size: 13px;
    font-weight: 600;
    line-height: 1.35;
  }

  :global([data-sonner-toast][data-styled='true'] [data-description]) {
    font-size: 12px;
    line-height: 1.4;
    opacity: 0.9;
  }

  :global([data-sonner-toast][data-styled='true'] [data-close-button]) {
    opacity: 0.72;
  }

  :global([data-sonner-toast][data-styled='true'] [data-close-button]:hover) {
    opacity: 1;
  }
</style>
