<script lang="ts">
  interface Props {
    size?: 'sm' | 'md' | 'lg'
    class?: string
  }

  let { size = 'md', class: cls = '' }: Props = $props()

  const sizes: Record<string, string> = {
    sm: 'h-4 w-4',
    md: 'h-6 w-6',
    lg: 'h-10 w-10',
  }
</script>

<svg class="animate-spin {sizes[size]} {cls}" viewBox="0 0 24 24" fill="none">
  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
</svg>
