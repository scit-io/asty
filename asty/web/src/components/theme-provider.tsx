import { createContext, useContext, useEffect, useState } from "react"

type Theme = "dark" | "light" | "system"

type ThemeProviderProps = {
  children: React.ReactNode
  defaultTheme?: Theme
  storageKey?: string
}

type ThemeProviderState = {
  theme: Theme
  setTheme: (theme: Theme) => void
}

const ThemeProviderContext = createContext<ThemeProviderState | undefined>(undefined)

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = "asty-theme",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem(storageKey) as Theme) || defaultTheme
  )

  useEffect(() => {
    const root = window.document.documentElement
    const mql = window.matchMedia("(prefers-color-scheme: dark)")

    const apply = () => {
      root.classList.remove("light", "dark")
      if (theme === "system") {
        root.classList.add(mql.matches ? "dark" : "light")
      } else {
        root.classList.add(theme)
      }
    }
    apply()

    // Track OS theme flips while the page is open — only meaningful
    // when the operator picked `system`; for an explicit light/dark
    // choice the listener fires but apply() ignores mql.matches.
    if (theme !== "system") return
    mql.addEventListener("change", apply)
    return () => mql.removeEventListener("change", apply)
  }, [theme])

  const value = {
    theme,
    setTheme: (theme: Theme) => {
      localStorage.setItem(storageKey, theme)
      setTheme(theme)
    },
  }

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext)
  if (context === undefined)
    throw new Error("useTheme must be used within a ThemeProvider")
  return context
}
