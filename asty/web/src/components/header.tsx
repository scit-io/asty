import { Link, useLocation } from 'react-router-dom'
import { ThemeToggle } from '@/components/theme-toggle'
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuList,
  NavigationMenuTrigger,
  navigationMenuTriggerStyle,
} from '@/components/ui/navigation-menu'
import { cn } from '@/lib/utils'
import logo from '@/assets/img/logo.svg'

// Sections map exactly the model: top-level menu entries open into
// dropdowns listing their static child pages. Dynamic pages
// (/nodes/:id, /services/:name, etc.) are reached from list views;
// they don't appear in the global header.
interface Section {
  label: string
  pages: { to: string; label: string }[]
}

// CLUSTER_SECTION_TABS / SERVICES_SECTION_TABS are exported so the
// Cluster section's static pages (/, /nodes, /logs) can render the
// same children inside the page as tabs, duplicating the dropdown's
// affordance one click closer to the content.
export const CLUSTER_SECTION_TABS = [
  { to: '/', label: 'Overview' },
  { to: '/nodes', label: 'Nodes' },
  { to: '/logs', label: 'Logs' },
]

export const SERVICES_SECTION_TABS = [
  { to: '/services', label: 'Overview' },
]

const SECTIONS: Section[] = [
  { label: 'Cluster', pages: CLUSTER_SECTION_TABS },
  { label: 'Services', pages: SERVICES_SECTION_TABS },
]

export function Header() {
  const location = useLocation()

  const isActiveSection = (section: Section) =>
    section.pages.some((p) =>
      p.to === '/' ? location.pathname === '/' : location.pathname.startsWith(p.to))

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur-sm supports-backdrop-filter:bg-background/60">
      <div className="container mx-auto flex h-16 items-center justify-between px-4 sm:px-6">
        <div className="flex items-center gap-6">
          <Link to="/" className="flex items-center gap-2 sm:gap-3 hover:opacity-80 transition-opacity">
            <img src={logo} alt="Asty" className="h-7 w-7 sm:h-8 sm:w-8" />
            <h1 className="text-lg sm:text-xl font-semibold">Asty</h1>
          </Link>
          <NavigationMenu className="hidden sm:block">
            <NavigationMenuList>
              {SECTIONS.map((section) => {
                const active = isActiveSection(section)
                // Sections with a single static page collapse into a
                // direct link — the dropdown adds zero affordance.
                if (section.pages.length === 1) {
                  const page = section.pages[0]
                  return (
                    <NavigationMenuItem key={section.label}>
                      <Link
                        to={page.to}
                        className={cn(
                          navigationMenuTriggerStyle(),
                          active ? 'text-foreground' : 'text-muted-foreground',
                        )}
                      >
                        {section.label}
                      </Link>
                    </NavigationMenuItem>
                  )
                }
                return (
                  <NavigationMenuItem key={section.label}>
                    <NavigationMenuTrigger
                      className={cn(
                        navigationMenuTriggerStyle(),
                        active ? 'text-foreground' : 'text-muted-foreground',
                      )}
                    >
                      {section.label}
                    </NavigationMenuTrigger>
                    <NavigationMenuContent>
                      <ul className="grid w-48 gap-1 p-2">
                        {section.pages.map((page) => {
                          const pageActive = page.to === '/'
                            ? location.pathname === '/'
                            : location.pathname.startsWith(page.to)
                          return (
                            <li key={page.to}>
                              <Link
                                to={page.to}
                                className={cn(
                                  'block rounded px-3 py-2 text-sm transition-colors',
                                  pageActive
                                    ? 'bg-accent text-accent-foreground'
                                    : 'hover:bg-accent/50 text-muted-foreground',
                                )}
                              >
                                {page.label}
                              </Link>
                            </li>
                          )
                        })}
                      </ul>
                    </NavigationMenuContent>
                  </NavigationMenuItem>
                )
              })}
            </NavigationMenuList>
          </NavigationMenu>
        </div>
        <ThemeToggle />
      </div>
    </header>
  )
}
