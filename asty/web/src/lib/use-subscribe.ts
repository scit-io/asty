import { useEffect } from 'react'

// useSubscribe wraps the SSE-subscribe pattern every data page
// repeats: open on mount with whatever ids the URL carries, skip
// while those ids are still undefined, and return the subscribe
// fn's cleanup so React tears the stream down on unmount or when
// the ids change.
//
// Pages now write:
//   useSubscribe(subscribeNode, nodeId)
// instead of:
//   useEffect(() => {
//     if (!nodeId) return
//     return subscribeNode(nodeId)
//   }, [nodeId, subscribeNode])
export function useSubscribe<A extends string[]>(
  fn: (...args: A) => () => void,
  ...args: { [K in keyof A]: A[K] | undefined }
): void {
  useEffect(() => {
    if (args.some((a) => a === undefined)) return
    return fn(...(args as A))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fn, ...args])
}
