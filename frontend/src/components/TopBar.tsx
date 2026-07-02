import { useEffect, useState } from 'react'
import { Moon, Sun, Circle } from 'lucide-react'
import { API } from '@/lib/api'
import { cn } from '@/lib/cn'

// TopBar sits above every routed page and gives the operator two
// always-visible readouts:
//   • whether the panel considers itself healthy (a single dot beats
//     a whole health page for a state that's binary)
//   • a running count of enabled inbounds so the "did I break anything"
//     glance takes no clicks
//
// Also holds the theme toggle. We persist the choice to localStorage
// so switching light/dark survives across sessions.

export function TopBar() {
  const [theme, setTheme] = useState<'light' | 'dark'>(() =>
    (localStorage.getItem('hexplus-theme') as 'light' | 'dark') || 'dark',
  )
  const [inboundCount, setInboundCount] = useState<{ enabled: number; total: number } | null>(null)
  const [alive, setAlive] = useState<boolean | null>(null)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    localStorage.setItem('hexplus-theme', theme)
  }, [theme])

  useEffect(() => {
    let cancelled = false
    async function refresh() {
      try {
        const list = await API.inbounds.list()
        if (cancelled) return
        setInboundCount({
          enabled: (list ?? []).filter((i) => i.enabled).length,
          total: (list ?? []).length,
        })
        setAlive(true)
      } catch {
        if (!cancelled) setAlive(false)
      }
    }
    refresh()
    const id = window.setInterval(refresh, 15000)
    return () => { cancelled = true; window.clearInterval(id) }
  }, [])

  return (
    <header className="flex h-14 items-center justify-between border-b bg-card px-6">
      <div className="flex items-center gap-6 text-sm">
        <StatusIndicator label="Panel" alive={alive} />
        <div className="hidden sm:flex items-center gap-2 text-muted-foreground">
          <span>Inbounds</span>
          <span className="text-foreground font-medium">
            {inboundCount ? `${inboundCount.enabled} / ${inboundCount.total}` : '—'}
          </span>
        </div>
      </div>
      <button
        onClick={() => setTheme((t) => (t === 'dark' ? 'light' : 'dark'))}
        className="grid h-8 w-8 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
        title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
      >
        {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
      </button>
    </header>
  )
}

function StatusIndicator({ label, alive }: { label: string; alive: boolean | null }) {
  const color =
    alive === null ? 'text-muted-foreground' :
    alive        ? 'text-[hsl(var(--success))]' : 'text-destructive'
  return (
    <div className="flex items-center gap-2 text-muted-foreground">
      <Circle className={cn('h-2.5 w-2.5 fill-current', color)} />
      <span>{label}</span>
    </div>
  )
}
