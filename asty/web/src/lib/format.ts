// All formatters round their input first so float-point artefacts
// (e.g. 0.6 + 0.7 → 1.3000000000000003 from JSON encoding) never
// leak to the UI. Callers don't need to pre-round.
//
// Zero collapses to a bare "0" without a unit — saying "0 ГБ" implies
// precision the system doesn't have, and a single "0" reads cleaner
// in dense table cells. Percent is the exception (0% means something
// specific) and keeps the unit.
//
// Unit suffixes (Mb/Gb/MHz/GHz) localise via a module-level table
// that LocaleProvider keeps in sync via `setFormatUnits` — call sites
// pass `format={formatMB}` to <Tile/> as a value, so the function
// can't read React context. The setter is called eagerly during the
// provider's state init so the very first render sees the right
// units; subsequent locale flips re-run it before the context update
// propagates.

type Units = { mb: string; gb: string; mhz: string; ghz: string }

const UNITS_BY_LOCALE: Record<string, Units> = {
  en: { mb: 'Mb', gb: 'Gb', mhz: 'MHz', ghz: 'GHz' },
  ru: { mb: 'Мб', gb: 'Гб', mhz: 'МГц', ghz: 'ГГц' },
}

let UNITS: Units = UNITS_BY_LOCALE.en

export function setFormatUnits(locale: string): void {
  UNITS = UNITS_BY_LOCALE[locale] ?? UNITS_BY_LOCALE.en
}

// formatCount renders monotonic counters (NATS in_msgs, out_msgs, etc.)
// compactly so a tile doesn't have to grow to fit ten-digit lifetime
// totals. K/M/B suffixes are SI-adjacent and read identically in
// every UI locale, so no translation table here.
export function formatCount(n: number): string {
  const v = Math.round(n)
  if (v < 1000) return v.toString()
  if (v < 1_000_000) return `${(v / 1000).toFixed(1)}K`
  if (v < 1_000_000_000) return `${(v / 1_000_000).toFixed(1)}M`
  return `${(v / 1_000_000_000).toFixed(1)}B`
}

// formatMB renders Mb sizes with Gb suffix past the 1024 cutoff.
// Used by MetricTile callers showing memory/disk totals.
export function formatMB(mb: number): string {
  const v = Math.round(mb)
  if (v === 0) return '0'
  return v >= 1024 ? `${(v / 1024).toFixed(1)} ${UNITS.gb}` : `${v} ${UNITS.mb}`
}

// formatMHz renders CPU frequency with a unit-aware threshold:
// values past 10 000 MHz read more naturally in GHz than as
// "17.6k MHz". Returns the unit baked in, so callers should leave
// MetricTile's `unit` prop empty when using this formatter.
export function formatMHz(mhz: number): string {
  const v = Math.round(mhz)
  if (v === 0) return '0'
  return v >= 10000 ? `${(v / 1000).toFixed(1)} ${UNITS.ghz}` : `${v} ${UNITS.mhz}`
}

// formatPercent rounds float percentages from the backend (cpu_usage,
// memory_usage_pct, etc.) to a whole number so table cells don't
// suddenly read "0.6000000000000001%". Keeps the unit at zero —
// "0%" is meaningful in a way "0 Mb" isn't.
export function formatPercent(n: number): string {
  return `${Math.round(n)}%`
}
