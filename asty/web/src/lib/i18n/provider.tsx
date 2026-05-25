import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { setFormatUnits } from '@/lib/format'
import { setUptimeUnits } from '@/lib/uptime'
import { dictionaries, type Locale, type MessageKey } from './dictionaries'

// Locale-bound modules whose helpers can't read React context because
// they're called from render callbacks (DataTable cells, Tile format
// prop). Provider keeps them all in lockstep with the active locale.
function syncLocaleAwareModules(locale: Locale): void {
  setFormatUnits(locale)
  setUptimeUnits(locale)
}

type LocaleProviderProps = {
  children: React.ReactNode
  storageKey?: string
}

type LocaleContextValue = {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (key: MessageKey, params?: Record<string, string | number>) => string
}

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined)

// detectDefaultLocale resolves the OS / browser preference: anything
// in `navigator.languages` (or the singular `language`) starting with
// "ru" wins; everything else falls through to English. Stable across
// reloads — only persisted explicit choices override it.
function detectDefaultLocale(): Locale {
  if (typeof navigator === 'undefined') return 'en'
  const candidates: readonly string[] = navigator.languages?.length
    ? navigator.languages
    : navigator.language
      ? [navigator.language]
      : []
  for (const lang of candidates) {
    if (lang.toLowerCase().startsWith('ru')) return 'ru'
  }
  return 'en'
}

function readStoredLocale(storageKey: string): Locale | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(storageKey)
  return raw === 'en' || raw === 'ru' ? raw : null
}

// interpolate inlines {placeholder} occurrences using the params map.
// Unknown placeholders render as-is so unmapped values are visible
// rather than silently elided.
function interpolate(template: string, params?: Record<string, string | number>): string {
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (match, key) => {
    const v = params[key]
    return v === undefined || v === null ? match : String(v)
  })
}

export function LocaleProvider({ children, storageKey = 'asty-locale' }: LocaleProviderProps) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    const initial = readStoredLocale(storageKey) ?? detectDefaultLocale()
    // Seed the module-level unit tables BEFORE first render so
    // formatMB / formatMHz / uptimeLabel return localised suffixes
    // on the very first paint (instead of flashing English on cold
    // load and swapping after the first effect tick).
    syncLocaleAwareModules(initial)
    return initial
  })

  // Mirror to <html lang> so screen readers and browser features
  // (spell-check hints, hyphenation) get the right hint.
  useEffect(() => {
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale
    }
  }, [locale])

  const setLocale = useCallback((next: Locale) => {
    if (typeof localStorage !== 'undefined') localStorage.setItem(storageKey, next)
    syncLocaleAwareModules(next)
    setLocaleState(next)
  }, [storageKey])

  const t = useCallback(
    (key: MessageKey, params?: Record<string, string | number>) => {
      const table = dictionaries[locale]
      const template = table[key] ?? dictionaries.en[key] ?? key
      return interpolate(template, params)
    },
    [locale],
  )

  const value = useMemo<LocaleContextValue>(() => ({ locale, setLocale, t }), [locale, setLocale, t])

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
}

export function useLocale() {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error('useLocale must be used within a LocaleProvider')
  return ctx
}

// useT is the everyday hook — call sites that don't need the locale
// itself only depend on `t`. Re-exports the same function the
// provider memoises, so it stays referentially stable per locale.
export function useT() {
  return useLocale().t
}
