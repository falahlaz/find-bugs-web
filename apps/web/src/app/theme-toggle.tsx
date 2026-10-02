import { Button } from '@/components/ui/button'
import { useTheme, type Theme } from '@/lib/theme'

const next: Record<Theme, Theme> = { light: 'dark', dark: 'system', system: 'light' }
const label: Record<Theme, string> = { light: 'Terang', dark: 'Gelap', system: 'Ikuti sistem' }

const icons: Record<Theme, React.ReactNode> = {
  light: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </>
  ),
  dark: <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />,
  system: (
    <>
      <rect x="2" y="3" width="20" height="14" rx="2" />
      <path d="M8 21h8M12 17v4" />
    </>
  ),
}

/** Cycles Terang → Gelap → Ikuti sistem. */
export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme()
  const title = `Tema: ${label[theme]} (klik untuk ${label[next[theme]].toLowerCase()})`
  return (
    <Button variant="ghost" size="sm" className={className} onClick={() => setTheme(next[theme])} title={title} aria-label={title}>
      <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        {icons[theme]}
      </svg>
      <span className="hidden sm:inline">{label[theme]}</span>
    </Button>
  )
}
