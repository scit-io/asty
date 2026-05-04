import { Link } from 'react-router-dom'
import { ThemeToggle } from '@/components/theme-toggle'
import logo from '@/assets/img/logo.svg'

export function Header() {
  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container mx-auto flex h-16 items-center justify-between px-4 sm:px-6">
        <Link to="/" className="flex items-center gap-2 sm:gap-3 hover:opacity-80 transition-opacity">
          <img src={logo} alt="Asty" className="h-7 w-7 sm:h-8 sm:w-8" />
          <h1 className="text-lg sm:text-xl font-semibold">Asty Dashboard</h1>
        </Link>
        <ThemeToggle />
      </div>
    </header>
  )
}
