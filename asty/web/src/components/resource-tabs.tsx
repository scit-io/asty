import { useLocation, useNavigate } from 'react-router-dom'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export interface TabItem {
  to: string
  label: string
}

interface ResourceTabsProps {
  items: TabItem[]
}

// ResourceTabs renders shadcn Tabs as navigation, not as a state
// switch — picking a tab pushes the corresponding URL. Active tab is
// derived from the longest matching prefix of location.pathname, so
// nested URLs (/nodes/{id}/allocations/{id}) light up the parent
// (/nodes/{id}/allocations) tab correctly.
export function ResourceTabs({ items }: ResourceTabsProps) {
  const location = useLocation()
  const navigate = useNavigate()

  const active = items.reduce<string>((best, item) => {
    if (location.pathname === item.to || location.pathname.startsWith(item.to + '/')) {
      if (item.to.length > best.length) return item.to
    }
    return best
  }, items[0]?.to ?? '')

  // grid-cols-{n} can't be templated through Tailwind's JIT, so we
  // pick from a small enumerated set covering the realistic tab
  // counts (2 = allocation/alloc-logs; 3 = node section; 4 = service
  // section). Falls back to flex layout if the count slips outside.
  const colsClass: Record<number, string> = {
    2: 'grid-cols-2',
    3: 'grid-cols-3',
    4: 'grid-cols-4',
    5: 'grid-cols-5',
  }
  const layout = colsClass[items.length] ? `grid w-full ${colsClass[items.length]}` : 'flex w-full'

  return (
    <Tabs value={active} onValueChange={(v) => navigate(v)} className="w-full">
      <TabsList className={layout}>
        {items.map((item) => (
          <TabsTrigger key={item.to} value={item.to} className="cursor-pointer">{item.label}</TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
