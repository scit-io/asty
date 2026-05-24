import { cn } from '@/lib/utils'

// TailButton brings the reader back to the live edge. Three visual
// states, all tied to how far the reader has drifted:
//   - unseen === 0  → neutral, just an action hint.
//   - 1..49         → amber, light pulse, count badge.
//   - >=50          → rose, stronger pulse, count badge.
// Reaching the bottom by scroll also resets unseen via onScroll, so
// the colour escalates only when the reader is actively behind.
export function TailButton({ unseen, onClick }: { unseen: number; onClick: () => void }) {
  const { tone, dot } = tailStyle(unseen)
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex h-8 cursor-pointer items-center gap-1.5 rounded-md border px-3 text-xs transition-colors',
        tone,
      )}
    >
      {dot && <span className={dot} />}
      <span>tail ↓</span>
      {unseen > 0 && <span className="font-semibold tabular-nums">+{unseen > 999 ? '999+' : unseen}</span>}
    </button>
  )
}

function tailStyle(unseen: number) {
  if (unseen >= 50) {
    return {
      tone: 'border-rose-500/50 bg-rose-500/15 text-rose-700 hover:bg-rose-500/25 dark:text-rose-300',
      dot: 'h-1.5 w-1.5 animate-pulse rounded-full bg-rose-500',
    }
  }
  if (unseen > 0) {
    return {
      tone: 'border-amber-500/50 bg-amber-500/15 text-amber-700 hover:bg-amber-500/25 dark:text-amber-300',
      dot: 'h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500',
    }
  }
  return {
    tone: 'border-border bg-background text-muted-foreground hover:bg-muted',
    dot: '',
  }
}
