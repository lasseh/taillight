import { defineStore } from 'pinia'
import { ref } from 'vue'

interface ScrollState {
  top: number
  isPinned: boolean
}

export const useScrollStore = defineStore('scroll', () => {
  const positions = ref<Record<string, ScrollState>>({})
  const scrollToBottomRequested = ref<string | null>(null)
  const pinnedRoutes = ref<Record<string, boolean>>({})
  const jumpSignal = ref<Record<string, number>>({})
  const newEventCounts = ref<Record<string, number>>({})

  function savePosition(route: string, top: number, isPinned: boolean) {
    positions.value[route] = { top, isPinned }
  }

  function getPosition(route: string): ScrollState | null {
    return positions.value[route] ?? null
  }

  function requestScrollToBottom(route: string) {
    scrollToBottomRequested.value = route
  }

  function consumeScrollToBottom(route: string): boolean {
    if (scrollToBottomRequested.value === route) {
      scrollToBottomRequested.value = null
      return true
    }
    return false
  }

  function setPinned(route: string, pinned: boolean) {
    pinnedRoutes.value[route] = pinned
    if (pinned) {
      newEventCounts.value[route] = 0
    }
  }

  // Default to pinned so new routes auto-scroll to the latest events on mount.
  function isPinned(route: string): boolean {
    return pinnedRoutes.value[route] ?? true
  }

  function triggerJump(route: string) {
    jumpSignal.value[route] = (jumpSignal.value[route] ?? 0) + 1
  }

  function getJumpSignal(route: string): number {
    return jumpSignal.value[route] ?? 0
  }

  function addNewEvents(route: string, count: number) {
    newEventCounts.value[route] = (newEventCounts.value[route] ?? 0) + count
  }

  function getNewEventCount(route: string): number {
    return newEventCounts.value[route] ?? 0
  }

  return { positions, savePosition, getPosition, requestScrollToBottom, consumeScrollToBottom, setPinned, isPinned, triggerJump, getJumpSignal, addNewEvents, getNewEventCount }
})
