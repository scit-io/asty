import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export const LEVELS = ['trace', 'debug', 'info', 'warn', 'error', 'fatal'] as const
export type Level = (typeof LEVELS)[number]
export type LevelFilterValue = Level | 'all'

// LevelFilter — small Select dropdown above the log list. The 'all'
// option short-circuits the per-row filter in LogsView.
export function LevelFilter({ value, onChange }: { value: LevelFilterValue; onChange: (v: LevelFilterValue) => void }) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as LevelFilterValue)}>
      <SelectTrigger className="h-8 w-[120px] text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">all levels</SelectItem>
        {LEVELS.map((l) => (
          <SelectItem key={l} value={l}>{l}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
