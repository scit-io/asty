import { memo, useCallback, useMemo, useState, type ChangeEvent, type MouseEvent, type ReactNode } from 'react'
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
  // deps(row) returns the values the renderer reads from `row` and
  // surrounding closures. The cell skips its render when these values
  // are === equal to the previous render. Optional: when omitted, the
  // cell falls back to row-reference equality, which is enough when
  // the store dedupes via stableMerge. Use deps when the row object
  // changes (e.g. a single field drifts every tick) but this column
  // displays only a subset of fields and would otherwise re-render
  // for nothing — or when the render closes over outer state (e.g.
  // `pending[row.id]`) that must trigger an update.
  deps?: (row: T) => readonly unknown[]
  sort?: (a: T, b: T) => number
  className?: string
}

// CellSpec — the actions column has the same shape as a regular
// Column but no key/label/sort. Kept separate so callers can opt into
// `deps` for the action cell too (Radix DropdownMenu is one of the
// heaviest things in the row, and benefits the most from memoisation).
export interface CellSpec<T> {
  render: (row: T) => ReactNode
  deps?: (row: T) => readonly unknown[]
}

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  search?: { placeholder: string; match: (row: T, query: string) => boolean }
  pageSizes?: number[]
  onRowClick?: (row: T) => void
  actions?: CellSpec<T>
  emptyMessage?: string
  // rowKey derives a stable React key per row. Default: index.
  rowKey?: (row: T) => string
}

const DEFAULT_PAGE_SIZES = [10, 20, 50]

// CellMemo skips re-rendering a cell when its declared deps haven't
// changed. The render fn ref always updates (closures may capture
// fresh state), but memo skips calling it when (a) deps match the
// previous render or (b) no deps were supplied and the row reference
// is identical. This is the universal mechanism that lets every
// table — Nodes, Allocations, Services, … — benefit without per-
// table cell components.
interface CellMemoProps<T> {
  row: T
  render: (row: T) => ReactNode
  deps?: (row: T) => readonly unknown[]
  className?: string
  rightAlign?: boolean
}

function depsEqual(a: readonly unknown[], b: readonly unknown[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false
  return true
}

// stopRowClick — used only for the actions cell so clicking its
// controls doesn't bubble into the row's navigate handler.
const stopRowClick = (e: MouseEvent) => e.stopPropagation()

function CellMemoImpl<T>({ row, render, className, rightAlign }: CellMemoProps<T>) {
  const cls = `py-2 ${rightAlign ? 'text-right ' : ''}${className ?? ''}`
  return (
    <TableCell className={cls} onClick={rightAlign ? stopRowClick : undefined}>
      {render(row)}
    </TableCell>
  )
}

const CellMemo = memo(CellMemoImpl, (prev, next) => {
  if (prev.className !== next.className) return false
  if (prev.rightAlign !== next.rightAlign) return false
  // deps mode — call deps with each side's row, compare results
  // shallowly. Fresh deps fn each render is fine; we only care about
  // the values it returns.
  if (next.deps) {
    if (!prev.deps) return false
    return depsEqual(prev.deps(prev.row), next.deps(next.row))
  }
  // default — row reference equality (rides on stableMerge in store)
  return prev.row === next.row
}) as typeof CellMemoImpl

// DataRow renders one table row. Outer memo: when the row reference
// AND every relevant prop is stable, the whole row body is skipped
// (no per-cell deps check). Inner CellMemo handles partial-field
// updates within a row.
interface DataRowProps<T> {
  row: T
  columns: Column<T>[]
  onRowClick?: (row: T) => void
  actions?: CellSpec<T>
}

function DataRowImpl<T>({ row, columns, onRowClick, actions }: DataRowProps<T>) {
  return (
    <TableRow
      onClick={onRowClick ? () => onRowClick(row) : undefined}
      className={onRowClick ? 'cursor-pointer' : ''}
    >
      {columns.map((c) => (
        <CellMemo
          key={c.key}
          row={row}
          render={c.render}
          deps={c.deps}
          className={c.className}
        />
      ))}
      {actions && (
        <CellMemo
          row={row}
          render={actions.render}
          deps={actions.deps}
          rightAlign
        />
      )}
    </TableRow>
  )
}
const DataRow = memo(DataRowImpl) as typeof DataRowImpl

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

  // Stable callbacks for the static-state subtrees below. Without
  // these, the search/header/pagination memos would invalidate on
  // every render and we'd lose the JSX caching.
  const onHeaderClick = useCallback((col: Column<T>) => {
    if (!col.sort) return
    if (sortKey !== col.key) {
      setSortKey(col.key)
      setSortDir('asc')
    } else if (sortDir === 'asc') {
      setSortDir('desc')
    } else {
      setSortKey(null)
    }
  }, [sortKey, sortDir])

  const onQueryChange = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value)
    setPage(0)
  }, [])
  const onPageSizeChange = useCallback((v: string) => {
    setPageSize(Number(v))
    setPage(0)
  }, [])
  const onPrev = useCallback(() => setPage((p) => Math.max(0, p - 1)), [])
  const onNext = useCallback(() => setPage((p) => p + 1), [])

  // The search bar only depends on the controlled value (query) and
  // the placeholder. Memo so SSE-driven flushes don't reconcile its
  // subtree — the Input is otherwise stable for the life of the page.
  const searchEl = useMemo(() => search ? (
    <div className="relative max-w-sm">
      <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
      <Input
        placeholder={search.placeholder}
        value={query}
        onChange={onQueryChange}
        className="pl-8"
      />
    </div>
  ) : null, [search, query, onQueryChange])

  // Header subtree depends only on the column definitions and the
  // local sort state; row updates leave it untouched.
  const headerEl = useMemo(() => (
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
  ), [columns, sortKey, sortDir, actions, onHeaderClick])

  // Pagination subtree depends on row-count primitives + local
  // pagination state. Stable across SSE flushes (count + page state
  // unchanged), so its Radix Select doesn't reconcile each tick.
  const paginationEl = useMemo(() => (
    <div className="flex items-center justify-between text-sm">
      <div className="text-muted-foreground">
        {sorted.length} row{sorted.length === 1 ? '' : 's'}
        {query && ` (filtered from ${rows.length})`}
      </div>
      <div className="flex items-center gap-3">
        <Select value={String(pageSize)} onValueChange={onPageSizeChange}>
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
          <Button variant="outline" size="sm" disabled={safePage === 0} onClick={onPrev}>
            Prev
          </Button>
          <Button variant="outline" size="sm" disabled={safePage >= totalPages - 1} onClick={onNext}>
            Next
          </Button>
        </div>
      </div>
    </div>
  ), [sorted.length, rows.length, query, pageSize, pageSizes, safePage, totalPages, onPageSizeChange, onPrev, onNext])

  return (
    <div className="space-y-3">
      {searchEl}

      <div className="rounded-md border">
        <Table>
          {headerEl}
          <TableBody>
            {visible.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length + (actions ? 1 : 0)} className="text-center text-muted-foreground py-8">
                  {emptyMessage}
                </TableCell>
              </TableRow>
            ) : (
              visible.map((row, i) => (
                <DataRow
                  key={rowKey ? rowKey(row) : i}
                  row={row}
                  columns={columns}
                  onRowClick={onRowClick}
                  actions={actions}
                />
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {paginationEl}
    </div>
  )
}
