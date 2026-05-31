import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select'
import { useLocale, useT } from '@/lib/i18n'
import type { Locale } from '@/lib/i18n'

// LocaleToggle sits next to ThemeToggle in the global Header. A
// shadcn Select so the trigger reads "EN" / "RU" inline (no extra
// icon button); the chevron and dropdown panel handle the affordance.
const OPTIONS: { value: Locale; label: string }[] = [
  { value: 'en', label: 'English' },
  { value: 'ru', label: 'Русский' },
]

export function LocaleToggle() {
  const { locale, setLocale } = useLocale()
  const t = useT()

  return (
    <Select value={locale} onValueChange={(v) => setLocale(v as Locale)}>
      <SelectTrigger
        aria-label={t('toggle.language')}
        className="w-[72px] gap-1.5 px-2.5 text-xs uppercase"
      >
        {/* SelectValue uses the selected item's children by default; we
            want the trigger to read as a compact "EN"/"RU" code while
            the dropdown items still show "EN English" / "RU Русский".
            Bypassing SelectValue and reading the live locale directly
            keeps the trigger short without juggling textValue. */}
        {locale.toUpperCase()}
      </SelectTrigger>
      <SelectContent align="end">
        {OPTIONS.map((opt) => (
          <SelectItem key={opt.value} value={opt.value}>
            <span className="mr-2 text-xs text-muted-foreground">{opt.value.toUpperCase()}</span>
            {opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
