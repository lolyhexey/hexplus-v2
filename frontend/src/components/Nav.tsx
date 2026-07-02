import { useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import {
  LayoutGrid, Box, ArrowRightLeft, Route, Shield, Settings,
  LogOut, ChevronsLeft, ChevronsRight, Zap,
} from 'lucide-react'
import { API } from '@/lib/api'
import { cn } from '@/lib/cn'

// Nav mirrors 3x-ui's dark sider: brand at the top, icon+label rows in
// the middle, logout pinned to the bottom. Collapses to icon-only for
// operators who want to fit more inbounds/clients on screen.
const items = [
  { to: '/', label: 'Overview', icon: LayoutGrid },
  { to: '/inbounds', label: 'Inbounds', icon: Box },
  { to: '/outbounds', label: 'Outbounds', icon: ArrowRightLeft },
  { to: '/routing', label: 'Routing', icon: Route },
  { to: '/certs', label: 'Certificates', icon: Shield },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function Nav({ collapsed, onToggle }: { collapsed: boolean; onToggle: () => void }) {
  const navigate = useNavigate()
  async function logout() {
    try { await API.logout() } catch { /* still send them to login */ }
    navigate('/login')
  }
  return (
    <aside
      className={cn(
        'flex flex-col shrink-0 border-r border-white/5 bg-sider text-sider-foreground transition-[width] duration-200',
        collapsed ? 'w-16' : 'w-60',
      )}
    >
      {/* Brand row. Logo mark stays the same width whether or not the
          label is visible, so the sider doesn't jiggle on collapse. */}
      <div className="flex items-center gap-2 px-4 py-4 border-b border-white/5">
        <div className="grid h-8 w-8 place-items-center rounded-md bg-primary text-primary-foreground shrink-0">
          <Zap className="h-4 w-4" />
        </div>
        {!collapsed && (
          <div className="min-w-0">
            <div className="text-sm font-semibold tracking-tight">HEXPLUS</div>
            <div className="text-[11px] text-sider-muted">V2Ray Panel</div>
          </div>
        )}
      </div>

      <nav className="flex-1 py-2 space-y-0.5 px-2">
        {items.map((it) => {
          const Icon = it.icon
          return (
            <NavLink
              key={it.to}
              to={it.to}
              end={it.to === '/'}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors',
                  isActive
                    ? 'bg-primary/15 text-primary'
                    : 'text-sider-muted hover:bg-white/5 hover:text-sider-foreground',
                  collapsed && 'justify-center px-0',
                )
              }
              title={collapsed ? it.label : undefined}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {!collapsed && <span className="truncate">{it.label}</span>}
            </NavLink>
          )
        })}
      </nav>

      <div className="p-2 space-y-0.5 border-t border-white/5">
        <button
          onClick={logout}
          className={cn(
            'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-sider-muted hover:bg-white/5 hover:text-sider-foreground',
            collapsed && 'justify-center px-0',
          )}
          title={collapsed ? 'Log out' : undefined}
        >
          <LogOut className="h-4 w-4 shrink-0" />
          {!collapsed && <span>Log out</span>}
        </button>
        <button
          onClick={onToggle}
          className={cn(
            'flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-sider-muted hover:bg-white/5 hover:text-sider-foreground',
            collapsed && 'justify-center px-0',
          )}
          title={collapsed ? 'Expand' : 'Collapse'}
        >
          {collapsed ? <ChevronsRight className="h-4 w-4" /> : <ChevronsLeft className="h-4 w-4" />}
          {!collapsed && <span>Collapse</span>}
        </button>
      </div>
    </aside>
  )
}

// Backwards-compatible zero-arg export so callers not yet updated still
// render the collapsed-aware version.
export function NavDefault() {
  const [collapsed, setCollapsed] = useState(false)
  return <Nav collapsed={collapsed} onToggle={() => setCollapsed((c) => !c)} />
}
