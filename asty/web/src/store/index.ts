import { create } from 'zustand'
import { makeScheduleSet } from './stream'
import { createAllocationsSlice } from './slices/allocations'
import { createClusterSlice, makeStatusHandler } from './slices/cluster'
import { createNodesSlice } from './slices/nodes'
import { createServicesSlice } from './slices/services'
import type { ClusterStore, SliceDeps } from './types'

// useClusterStore — one zustand store split into four slices for
// readability. Pages keep importing `useClusterStore` from
// '@/store/cluster' (re-export); the slice files own the
// subscribe* / refresh* / update* fns one resource at a time.
//
// Shared infrastructure (scheduleSet + attachStatusHandler) is built
// once per store instance and threaded into every slice via SliceDeps
// so cross-slice mutations stay coalesced — one render per snapshot,
// not one per slice listener.
export const useClusterStore = create<ClusterStore>((set) => {
  const scheduleSet = makeScheduleSet<ClusterStore>(set)
  const attachStatusHandler = makeStatusHandler(scheduleSet)
  const deps: SliceDeps = { set, scheduleSet, attachStatusHandler }
  return {
    ...createClusterSlice(deps),
    ...createNodesSlice(deps),
    ...createServicesSlice(deps),
    ...createAllocationsSlice(deps),
  }
})
