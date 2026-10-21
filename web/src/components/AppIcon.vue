<script setup lang="ts">
import type { IconName } from './icons'

withDefaults(
  defineProps<{
    name: IconName
    size?: number
  }>(),
  { size: 18 },
)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <g v-if="name === 'dashboard'">
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
    </g>
    <g v-else-if="name === 'records'">
      <path d="M3 3v18h18" />
      <path d="m7 15 4-4 3 3 5-6" />
    </g>
    <g v-else-if="name === 'archives'">
      <path d="M4 7h16v13H4z" />
      <path d="M3 3h18v4H3z" />
      <path d="M9 11h6" />
    </g>
    <g v-else-if="name === 'settings'">
      <path d="M4 7h10" />
      <path d="M18 7h2" />
      <circle cx="16" cy="7" r="2" />
      <path d="M4 17h2" />
      <path d="M10 17h10" />
      <circle cx="8" cy="17" r="2" />
    </g>
    <g v-else-if="name === 'refresh'">
      <path d="M20 11a8 8 0 1 0-2.34 5.66" />
      <path d="M20 4v7h-7" />
    </g>
    <g v-else-if="name === 'sun'">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.66 6.34l1.41-1.41" />
    </g>
    <g v-else-if="name === 'moon'">
      <path d="M20.5 14.1A8.5 8.5 0 0 1 9.9 3.5 8.5 8.5 0 1 0 20.5 14.1z" />
    </g>
    <g v-else-if="name === 'auto'">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none" />
    </g>
    <g v-else-if="name === 'battery'">
      <rect x="2" y="7" width="17" height="10" rx="2" />
      <path d="M22 10v4" />
      <path d="M5 10h8v4H5z" fill="currentColor" stroke="none" />
    </g>
    <g v-else-if="name === 'alert'">
      <path d="M10.3 3.9 2.4 18a1.6 1.6 0 0 0 1.4 2.4h16.4a1.6 1.6 0 0 0 1.4-2.4L13.7 3.9a1.9 1.9 0 0 0-3.4 0z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </g>
    <g v-else-if="name === 'check'">
      <path d="m4 12 5 5L20 6" />
    </g>
    <g v-else-if="name === 'close'">
      <path d="M6 6l12 12M18 6 6 18" />
    </g>
    <g v-else-if="name === 'download'">
      <path d="M12 3v12" />
      <path d="m7 10 5 5 5-5" />
      <path d="M4 21h16" />
    </g>
    <g v-else-if="name === 'trash'">
      <path d="M4 7h16" />
      <path d="M9 7V4h6v3" />
      <path d="m6 7 1 14h10l1-14" />
      <path d="M10 11v6M14 11v6" />
    </g>
    <g v-else-if="name === 'plus'">
      <path d="M12 5v14M5 12h14" />
    </g>
    <g v-else-if="name === 'copy'">
      <rect x="8" y="8" width="12" height="12" rx="2" />
      <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" />
    </g>
    <g v-else-if="name === 'external'">
      <path d="M14 4h6v6" />
      <path d="m20 4-9 9" />
      <path d="M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5" />
    </g>
    <g v-else-if="name === 'login'">
      <path d="M14 4h5a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-5" />
      <path d="M3 12h12" />
      <path d="m11 8 4 4-4 4" />
    </g>
    <g v-else-if="name === 'room'">
      <path d="m3 11 9-8 9 8" />
      <path d="M5 10v10h14V10" />
      <path d="M9 20v-5h6v5" />
    </g>
    <g v-else-if="name === 'shield'">
      <path d="M12 3 4.5 6v5.5c0 4.6 3 8.2 7.5 9.5 4.5-1.3 7.5-4.9 7.5-9.5V6z" />
      <path d="m9 12 2 2 4-4" />
    </g>
    <g v-else-if="name === 'info'">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v6" />
      <path d="M12 7h.01" />
    </g>
    <g v-else-if="name === 'chevron-down'">
      <path d="m6 9 6 6 6-6" />
    </g>
    <g v-else-if="name === 'chart'">
      <path d="M4 20V10M10 20V4M16 20v-7M22 20H2" />
    </g>
    <g v-else-if="name === 'table'">
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M3 9h18M9 9v11M15 9v11" />
    </g>
  </svg>
</template>
