// Re-export so pages can keep their existing `@/store/cluster` import
// path. The real store lives in ./index.ts (split into four slices
// under ./slices/).
export { useClusterStore } from './index'
