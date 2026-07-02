<script setup lang="ts">
// One entry in AppHeader's DASHBOARD/NETLOG/SRVLOG/APPLOG strip. Renders a
// router-link when `to` is set (dashboard) and a button otherwise (log feeds,
// which scroll-to-bottom via the click handler). Desktop passes `title` for
// the keyboard-shortcut hint; mobile uses taller padding.
defineProps<{
  label: string
  active: boolean
  activeClass: string
  inactiveClass: string
  to?: string | undefined
  title?: string
  mobile?: boolean
}>()

defineEmits<{ click: [] }>()
</script>

<template>
  <router-link
    v-if="to"
    :to="to"
    :title="title"
    class="px-2 text-xs transition-colors"
    :class="[mobile ? 'py-1.5' : 'py-0.5', active ? activeClass : inactiveClass]"
    @click="$emit('click')"
  >
    {{ label }}
  </router-link>
  <button
    v-else
    :title="title"
    class="px-2 text-xs transition-colors"
    :class="[mobile ? 'py-1.5 text-left' : 'py-0.5', active ? activeClass : inactiveClass]"
    @click="$emit('click')"
  >
    {{ label }}
  </button>
</template>
