<script setup lang="ts">
import { useAttrs } from 'vue'

const attrs = useAttrs()
</script>

<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="28"
    height="20"
    viewBox="0 0 28 20"
    fill="currentColor"
    focusable="false"
    :role="attrs['aria-label'] ? 'img' : undefined"
    :aria-hidden="attrs['aria-label'] ? undefined : true"
  >
    <path
      d="M3.3 1.5C2 1.5 1 2.5 1 3.8C1 12 7.5 18.5 15.7 18.5C17 18.5 18 17.5 18 16.2V13.8C18 13.3 17.7 12.9 17.2 12.8L13.4 11.9C12.9 11.8 12.4 12 12.1 12.4L10.8 14.1C7.6 12.6 5.4 10.4 3.9 7.2L5.6 5.9C6 5.6 6.2 5.1 6.1 4.6L5.2 2.3C5 1.8 4.6 1.5 4.1 1.5Z"
    />
    <!-- Outlined lettering keeps LTE legible without depending on an installed font. -->
    <path d="M11 1.5H12.5V6.5H15.5V8H11Z" />
    <path d="M16 1.5H21V3H19.25V8H17.75V3H16Z" />
    <path d="M22 1.5H27V3H23.5V4H26.5V5.5H23.5V6.5H27V8H22Z" />
  </svg>
</template>
