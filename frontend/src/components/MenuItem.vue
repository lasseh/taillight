<script setup lang="ts">
// One entry in AppHeader's settings menu: icon + label + click. The desktop
// dropdown and mobile panel variants intentionally differ in padding and
// hover treatment; `danger` gives the logout entry its red hover.
import MenuIcon from '@/components/MenuIcon.vue'
import type { IconName } from '@/components/MenuIcon.vue'

defineProps<{
  icon: IconName
  label: string
  mobile?: boolean
  danger?: boolean
}>()

defineEmits<{ click: [] }>()
</script>

<template>
  <button
    class="text-t-fg-dark flex items-center gap-2 py-1.5 text-left text-xs transition-colors"
    :class="[
      mobile ? 'px-2' : 'hover:bg-t-bg-hover w-full px-3',
      danger ? 'hover:text-t-red' : 'hover:text-t-fg',
    ]"
    @click="$emit('click')"
  >
    <MenuIcon :name="icon" />
    <span>{{ label }}</span>
  </button>
</template>
