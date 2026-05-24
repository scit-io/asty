import { useMemo, useState, type ReactNode } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ArrowDown, ArrowUp, ArrowUpDown, Search } from 'lucide-react'

export interface Column<T> {
  key: string
  label: string
  render: (row: T) => ReactNode
  sort?: (a: T, b: T) => number
  className?: string
}

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  search?: { placeholder: string; match: (row: T, query: string) => boolean }
  pageSizes?: number[]
  onRowClick?: (row: T) => void
  actions?: (row: T) => ReactNode
  emptyMessage?: string
  // rowKey derives a stable React key per row. Default: index.
  rowKey?: (row: T) => string
}

const DEFAULT_PAGE_SIZES = [10, 20, 50]

// DataTable is the workhorse for list-views (Nodes / Allocations /
// Services). Local state for search, sort, and pagination — no
// coupling to the global store. Columns can opt into sorting via the
// `sort` comparator; the header then toggles asc/desc/off in a cycle.
export function DataTable<T>({
  columns,
  rows,
  search,
  pageSizes = DEFAULT_PAGE_SIZES,
  onRowClick,
  actions,
  emptyMessage = 'No rows.',
  rowKey,
}: DataTableProps<T>) {
  const [query, setQuery] = useState('')
  const [sortKey, setSortKey] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(pageSizes[0])

  const filtered = useMemo(() => {
    if (!query || !search) return rows
    return rows.filter((r) => search.match(r, query))
  }, [rows, query, search])

  const sorted = useMemo(() => {
    if (!sortKey) return filtered
    const col = columns.find((c) => c.key === sortKey)
    if (!col?.sort) return filtered
    const cmp = col.sort
    const arr = [...filtered]
    arr.sort((a, b) => (sortDir === 'asc' ? cmp(a, b) : cmp(b, a)))
    return arr
  }, [filtered, sortKey, sortDir, columns])

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const visible = sorted.slice(safePage * pageSize, (safePage + 1) * pageSize)

  const onHeaderClick = (col: Column<T>) => {
    if (!col.sort) return
    if (sortKey !== col.key) {
      setSortKey(col.key)
      setSortDir('asc')
    } else if (sortDir === 'asc') {
      setSortDir('desc')
    } else {
      setSortKey(null)
    }
  }

  return (
    <div className="space-y-3">
      {search && (
        <div className="relative max-w-sm">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder={search.placeholder}
            value={query}
            onChange={(e) => { setQuery(e.target.value); setPage(0) }}
            className="pl-8"
          />
        </div>
      )}

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((c) => {
                const arrow = sortKey === c.key
                  ? (sortDir === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />)
                  : c.sort ? <ArrowUpDown className="h-3 w-3 opacity-40" /> : null
                return (
                  <TableHead
                    key={c.key}
                    onClick={() => onHeaderClick(c)}
                    className={`${c.sort ? 'cursor-pointer select-none' : ''} ${c.className ?? ''}`}
                  >
                    <span className="inline-flex items-center gap-1">{c.label}{arrow}</span>
                  </TableHead>
                )
              })}
              {actions && <TableHead className="w-12 text-right" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length + (actions ? 1 : 0)} className="text-center text-muted-foreground py-8">
                  {emptyMessage}
                </TableCell>
              </TableRow>
            ) : (
              visible.map((row, i) => (
                <TableRow
                  key={rowKey ? rowKey(row) : i}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  className={onRowClick ? 'cursor-pointer' : ''}
                >
                  {columns.map((c) => (
                    <TableCell key={c.key} className={`py-2 ${c.className ?? ''}`}>
                      {c.render(row)}
                    </TableCell>
                  ))}
                  {actions && (
                    <TableCell className="py-2 text-right" onClick={(e) => e.stopPropagation()}>
                      {actions(row)}
                    </TableCell>
                  )}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between text-sm">
        <div className="text-muted-foreground">
          {sorted.length} row{sorted.length === 1 ? '' : 's'}
          {query && ` (filtered from ${rows.length})`}
        </div>
        <div className="flex items-center gap-3">
          <Select value={String(pageSize)} onValueChange={(v) => { setPageSize(Number(v)); setPage(0) }}>
            <SelectTrigger className="w-20 h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {pageSizes.map((s) => (
                <SelectItem key={s} value={String(s)}>{s}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-muted-foreground">
            Page {safePage + 1} / {totalPages}
          </span>
          <div className="flex gap-1">
            <Button variant="outline" size="sm" disabled={safePage === 0} onClick={() => setPage(safePage - 1)}>
              Prev
            </Button>
            <Button variant="outline" size="sm" disabled={safePage >= totalPages - 1} onClick={() => setPage(safePage + 1)}>
              Next
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
