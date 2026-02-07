import { ref, type Ref, onScopeDispose } from 'vue'
import { useIntersectionObserver } from '@vueuse/core'

export function useInfiniteScroll(onLoadMore: () => void) {
  const sentinel = ref<HTMLElement | null>(null)
  const enabled = ref(true)

  const { stop } = useIntersectionObserver(
    sentinel as Ref<HTMLElement | null>,
    ([entry]) => {
      if (entry?.isIntersecting && enabled.value) {
        onLoadMore()
      }
    },
    { rootMargin: '200px' },
  )

  onScopeDispose(stop)

  return { sentinel, enabled }
}
