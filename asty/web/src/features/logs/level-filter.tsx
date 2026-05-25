import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useT } from '@/lib/i18n'

export const LEVELS = ['trace', 'debug', 'info', 'warn', 'error', 'fatal'] as const
export type Level = (typeof LEVELS)[number]
export type LevelFilterValue = Level | 'all'

// LevelFilter — small Select dropdown above the log list. The 'all'
// option short-circuits the per-row filter in LogsView. Level names
// themselves are zerolog wire values and stay English on both locales.
export function LevelFilter({ value, onChange }: { value: LevelFilterValue; onChange: (v: LevelFilterValue) => void }) {
  const t = useT()
  return (
    <Select value={value} onValueChange={(v) => onChange(v as LevelFilterValue)}>
      <SelectTrigger className="h-8 w-[120px] text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{t('logs.level.all')}</SelectItem>
        {LEVELS.map((l) => (
          <SelectItem key={l} value={l}>{l}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
