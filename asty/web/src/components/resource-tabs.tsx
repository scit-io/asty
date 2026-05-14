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

  return (
    <Tabs value={active} onValueChange={(v) => navigate(v)}>
      <TabsList>
        {items.map((item) => (
          <TabsTrigger key={item.to} value={item.to}>{item.label}</TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
