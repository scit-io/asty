import { isValidElement, memo, type ReactElement, type ReactNode } from 'react'
import { formatDistanceToNow } from 'date-fns'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// Tile is THE dashboard card. CardContent is built bottom-up: the
// Bottom block holds the lowest text (hint, or bar+hint for metric)
// and sits flush with the card bottom; the Top block (the "middle
// text" — value or controls) is anchored to the TOP of the Bottom
// block via justify-end inside a flex-1 region.
//
// Every text node gets `leading-none` so its line-box equals its
// font-size — there's no extra space above/below to make text-sm
// "float" higher in its slot than text-2xl. With line-boxes tight
// to the glyphs and bottoms anchored to the same y, the visible
// letter bottoms of text-sm and text-2xl values land on the same
// line within a row whose Bottom heights match.

type Base = { title: string; icon?: ReactNode; className?: string }

type TileProps = Base & (
  | { variant: 'metric'; usage: number; total: number; format?: (n: number) => string; unit?: string }
  | { variant: 'stat'; value: ReactNode; hint?: ReactNode; size?: 'lg' | 'sm'; mono?: boolean; bar?: boolean }
  | { variant: 'timestamp'; timestamp?: string }
  | { variant: 'actions'; actions: ReactNode; hint?: ReactNode }
)

const defaultFormat = (n: number) => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return n.toFixed(0)
}

function GradientBar({ pct }: { pct: number }) {
  return (
    <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-secondary">
      <div className="absolute inset-0 bg-gradient-to-r from-emerald-500 via-amber-500 to-red-500" />
      <div className="absolute inset-y-0 right-0 bg-secondary" style={{ width: `${100 - pct}%` }} />
    </div>
  )
}

function Hint({ children }: { children: ReactNode }) {
  return <p className="text-xs text-muted-foreground leading-none">{children}</p>
}

function Top(props: TileProps): ReactNode {
  switch (props.variant) {
    case 'metric': {
      const pct = props.total > 0 ? Math.min(100, (props.usage / props.total) * 100) : 0
      return <div className="text-2xl font-bold leading-none mb-2">{pct.toFixed(0)}%</div>
    }
    case 'stat': {
      const small = props.size === 'sm'
      const size = small ? 'text-sm' : 'text-2xl'
      const lead = small ? '' : ' leading-none'
      const mono = props.mono ? ' font-mono' : ''
      return <div className={`${size} font-bold${lead} mb-2${mono}`}>{props.value}</div>
    }
    case 'timestamp': {
      const valid = !!props.timestamp && !props.timestamp.startsWith('0001-')
      return (
        <div className="text-sm font-bold mb-2">
          {valid ? formatDistanceToNow(new Date(props.timestamp!), { addSuffix: true }) : '—'}
        </div>
      )
    }
    case 'actions': {
      // mb-2 only when there's a hint to keep the row baseline aligned
      // with value+hint neighbours; without a hint the controls pin to
      // the card's bottom edge.
      const margin = props.hint != null ? ' mb-2' : ''
      return <div className={`flex items-end gap-2 w-full${margin}`}>{props.actions}</div>
    }
  }
}

function Bottom(props: TileProps): ReactNode {
  switch (props.variant) {
    case 'metric': {
      const fmt = props.format ?? defaultFormat
      const pct = props.total > 0 ? Math.min(100, (props.usage / props.total) * 100) : 0
      return (
        <>
          {props.total > 0 && <div className="mt-1 mb-2"><GradientBar pct={pct} /></div>}
          <Hint>{fmt(props.usage)} / {fmt(props.total)}{props.unit ? ` ${props.unit}` : ''}</Hint>
        </>
      )
    }
    case 'stat':
      return (
        <>
          {props.bar && <div className="mt-1 mb-2 h-1.5" />}
          {props.hint != null && <Hint>{props.hint}</Hint>}
        </>
      )
    case 'timestamp': {
      const valid = !!props.timestamp && !props.timestamp.startsWith('0001-')
      return valid ? <Hint>{new Date(props.timestamp!).toLocaleString()}</Hint> : null
    }
    case 'actions':
      return props.hint != null ? <Hint>{props.hint}</Hint> : null
  }
}

// iconEqual compares two icon-prop ReactNodes structurally: same React
// component type + same className. Call sites use a fresh JSX element
// per render (`icon={<Cpu className="h-4 w-4" />}`), so default
// referential equality always fails. The shape we care about — which
// glyph and what size — is captured by (type, className).
function iconEqual(a: ReactNode | undefined, b: ReactNode | undefined): boolean {
  if (a === b) return true
  if (!a || !b) return false
  if (!isValidElement(a) || !isValidElement(b)) return false
  const ea = a as ReactElement<{ className?: string }>
  const eb = b as ReactElement<{ className?: string }>
  return ea.type === eb.type && ea.props.className === eb.props.className
}

function tilePropsEqual(prev: TileProps, next: TileProps): boolean {
  if (prev.variant !== next.variant) return false
  if (prev.title !== next.title) return false
  if (prev.className !== next.className) return false
  if (!iconEqual(prev.icon, next.icon)) return false
  switch (prev.variant) {
    case 'metric': {
      const n = next as Extract<TileProps, { variant: 'metric' }>
      return prev.usage === n.usage && prev.total === n.total
        && prev.format === n.format && prev.unit === n.unit
    }
    case 'stat': {
      const n = next as Extract<TileProps, { variant: 'stat' }>
      return prev.value === n.value && prev.hint === n.hint
        && prev.size === n.size && prev.mono === n.mono && prev.bar === n.bar
    }
    case 'timestamp': {
      const n = next as Extract<TileProps, { variant: 'timestamp' }>
      return prev.timestamp === n.timestamp
    }
    case 'actions':
      // actions is fresh JSX per render at every call site — skipping
      // would require a structural diff we don't have. Re-render.
      return false
  }
}

export const Tile = memo(function Tile(props: TileProps) {
  return (
    <Card className={`flex flex-col${props.className ? ' ' + props.className : ''}`}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{props.title}</CardTitle>
        {props.icon && <span className="text-muted-foreground">{props.icon}</span>}
      </CardHeader>
      <CardContent className="flex-1 flex flex-col">
        <div className="flex-1 flex flex-col justify-end">
          <Top {...props} />
        </div>
        <Bottom {...props} />
      </CardContent>
    </Card>
  )
}, tilePropsEqual)
