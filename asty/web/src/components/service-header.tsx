import { Badge } from '@/components/ui/badge'
import { Breadcrumbs, type Crumb } from '@/components/breadcrumbs'
import { routes } from '@/lib/routes'
import type { ServiceDefinition } from '@/types'

interface ServiceHeaderProps {
  // Pass service when the page has it (snapshot from the store);
  // pass name alone on pages opened before the SSE warmup has
  // delivered the service object — the type Badge then renders only
  // when the data finally arrives.
  service?: ServiceDefinition | null
  name: string
  // Crumbs the page-specific tail expects (e.g. 'Allocations'); the
  // Services › {name} prefix is built here so every page under
  // /services/{name} shares it verbatim. Services is its own
  // top-level section in the dashboard nav, not a child of Cluster —
  // the breadcrumb starts there.
  tail?: Crumb[]
}

// ServiceHeader renders the canonical split-row header for any page
// inside /services/{name}: breadcrumbs left, service name big title +
// type Badge right.
export function ServiceHeader({ service, name, tail = [] }: ServiceHeaderProps) {
  const crumbs: Crumb[] = [
    { label: 'Services', to: routes.services },
    tail.length === 0
      ? { label: name }
      : { label: name, to: routes.service(name) },
    ...tail,
  ]
  return (
    <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
      <Breadcrumbs items={crumbs} />
      <div className="flex items-center gap-3">
        <h1 className="text-2xl sm:text-3xl font-bold">{name}</h1>
        {service && <Badge variant={service.Type === 'system' ? 'secondary' : 'default'}>{service.Type}</Badge>}
      </div>
    </div>
  )
}
