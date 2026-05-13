import { memo, useEffect, useRef, useCallback } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { MetricPoint } from '@/types'

interface MetricsChartProps {
  title: string
  data: MetricPoint[]
  color?: string
  unit?: string
}

function resolveColor(raw: string): string {
  if (!raw.includes('var(')) return raw
  const match = raw.match(/var\(--([^)]+)\)/)
  if (!match) return raw
  const value = getComputedStyle(document.documentElement).getPropertyValue(`--${match[1]}`).trim()
  if (!value) return '#888'
  return `hsl(${value})`
}

function getThemeColors() {
  const style = getComputedStyle(document.documentElement)
  const resolve = (v: string) => {
    const val = style.getPropertyValue(v).trim()
    return val ? `hsl(${val})` : '#888'
  }
  return {
    foreground: resolve('--foreground'),
    mutedForeground: resolve('--muted-foreground'),
    border: resolve('--border'),
  }
}

export const MetricsChart = memo(function MetricsChart({ title, data, color = 'hsl(var(--chart-1))', unit = '%' }: MetricsChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const plotRef = useRef<uPlot | null>(null)
  const colorRef = useRef(color)
  colorRef.current = color

  const createPlot = useCallback(() => {
    if (!containerRef.current) return
    if (plotRef.current) {
      plotRef.current.destroy()
      plotRef.current = null
    }

    const resolved = resolveColor(colorRef.current)
    const theme = getThemeColors()

    const fillColor = (() => {
      const match = resolved.match(/hsl\(([^)]+)\)/)
      if (match) return `hsla(${match[1]} / 0.12)`
      return resolved + '1f'
    })()

    const opts: uPlot.Options = {
      width: containerRef.current.clientWidth,
      height: 128,
      padding: [8, 8, 0, 0],
      cursor: {
        show: true,
        x: true,
        y: false,
        points: { show: false },
      },
      select: { show: false, left: 0, top: 0, width: 0, height: 0 },
      legend: { show: false },
      axes: [
        {
          stroke: theme.mutedForeground,
          grid: { show: false },
          ticks: { show: false },
          gap: 4,
          size: 28,
          font: '10px ui-sans-serif, system-ui, sans-serif',
          values: (_u, vals) => vals.map(v => {
            const d = new Date(v * 1000)
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
          }),
        },
        {
          stroke: theme.mutedForeground,
          grid: { stroke: theme.border, width: 1 },
          ticks: { show: false },
          gap: 4,
          size: 44,
          font: '10px ui-sans-serif, system-ui, sans-serif',
          values: (_u, vals) => vals.map(v => `${Math.round(v)}${unit}`),
        },
      ],
      series: [
        {},
        {
          stroke: resolved,
          fill: fillColor,
          width: 2,
          points: { show: false },
          paths: uPlot.paths.spline!(),
        },
      ],
    }

    const timestamps = data.map(p => p.timestamp)
    const values = data.map(p => Math.round(p.value * 10) / 10)

    plotRef.current = new uPlot(opts, [timestamps, values], containerRef.current)
  }, [data, unit])

  useEffect(() => {
    createPlot()
    return () => {
      plotRef.current?.destroy()
      plotRef.current = null
    }
  }, [createPlot])

  useEffect(() => {
    if (!plotRef.current || data.length === 0) return
    const timestamps = data.map(p => p.timestamp)
    const values = data.map(p => Math.round(p.value * 10) / 10)
    plotRef.current.setData([timestamps, values])
  }, [data])

  useEffect(() => {
    if (!containerRef.current) return
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        if (plotRef.current) {
          plotRef.current.setSize({ width: entry.contentRect.width, height: 128 })
        }
      }
    })
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [])

  // Re-create on theme change
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => createPlot()
    mq.addEventListener('change', handler)

    const observer = new MutationObserver(() => createPlot())
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })

    return () => {
      mq.removeEventListener('change', handler)
      observer.disconnect()
    }
  }, [createPlot])

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
            No data yet
          </div>
        ) : (
          <div ref={containerRef} className="h-32 w-full [&_.u-wrap]:!bg-transparent" />
        )}
      </CardContent>
    </Card>
  )
})
