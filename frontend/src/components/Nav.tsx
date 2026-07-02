import { NavLink, useNavigate } from 'react-router-dom'
import { Activity, Box, Route, Shield, Settings, LogOut, ArrowRightLeft } from 'lucide-react'
import { API } from '@/lib/api'
import { cn } from '@/lib/cn'

// Nav is the persistent sidebar drawn on every authenticated page.
// Clicking the icon jumps to that route; the active route gets a
// filled background from Tailwind's group-based selectors.
const items = [
  { to: '/', label: 'ภาพรวม', icon: Activity },
  { to: '/inbounds', label: 'Inbounds', icon: Box },
  { to: '/outbounds', label: 'Outbounds', icon: ArrowRightLeft },
  { to: '/routing', label: 'Routing', icon: Route },
  { to: '/certs', label: 'Certs', icon: Shield },
  { to: '/settings', label: 'ตั้งค่า', icon: Settings },
]

export function Nav() {
  const navigate = useNavigate()
  async function logout() {
    try { await API.logout() } catch { /* even if server 500s, kick user to login */ }
    navigate('/login')
  }
  return (
    <aside className="w-56 shrink-0 border-r bg-card">
      <div className="flex flex-col h-full">
        <div className="px-6 py-4 border-b">
          <div className="text-lg font-semibold">HEXPLUS</div>
          <div className="text-xs text-muted-foreground">V2Ray Panel</div>
        </div>
        <nav className="flex-1 p-2 space-y-1">
          {items.map((it) => {
            const Icon = it.icon
            return (
              <NavLink
                key={it.to}
                to={it.to}
                end={it.to === '/'}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-2 px-3 py-2 rounded-md text-sm',
                    isActive ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60',
                  )
                }
              >
                <Icon className="h-4 w-4" />
                {it.label}
              </NavLink>
            )
          })}
        </nav>
        <button onClick={logout} className="m-2 flex items-center gap-2 px-3 py-2 rounded-md text-sm hover:bg-accent/60 text-left">
          <LogOut className="h-4 w-4" /> ออกจากระบบ
        </button>
      </div>
    </aside>
  )
}
