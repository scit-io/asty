import type { Node } from '@/types'

// nodeStatusSwitchClass overrides both checked and unchecked tracks of
// a Radix Switch so the switch background always reflects node status,
// independent of toggle position. Palette mirrors statusDot in
// components/node-header.tsx (minus its `animate-pulse` on `draining`
// — the switch track stays visually still).
export const nodeStatusSwitchClass = (s?: Node['status']): string => {
  switch (s) {
    case 'ready':
      return 'data-[state=checked]:bg-green-500 data-[state=unchecked]:bg-green-500'
    case 'draining':
    case 'drained':
    case 'paused':
      return 'data-[state=checked]:bg-yellow-500 data-[state=unchecked]:bg-yellow-500'
    case 'down':
      return 'data-[state=checked]:bg-red-500 data-[state=unchecked]:bg-red-500'
    default:
      return 'data-[state=checked]:bg-gray-400 data-[state=unchecked]:bg-gray-400'
  }
}
