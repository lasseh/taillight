// Returns a wrapper that delays calling fn until ms of call inactivity.
// Local replacement for @vueuse/core's useDebounceFn.
export function useDebounceFn<A extends unknown[]>(fn: (...args: A) => void, ms: number) {
  let timer: ReturnType<typeof setTimeout> | undefined
  return (...args: A) => {
    clearTimeout(timer)
    timer = setTimeout(() => fn(...args), ms)
  }
}
