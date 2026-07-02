import { useEffect, useState } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { API, APIError } from '@/lib/api'
import IndexPage from '@/pages/index/IndexPage'
import Login from '@/pages/Login'
import Inbounds from '@/pages/Inbounds'
import Clients from '@/pages/Clients'
import Outbounds from '@/pages/Outbounds'
import Routing from '@/pages/Routing'
import Certs from '@/pages/Certs'
import Settings from '@/pages/Settings'
import XrayPage from '@/pages/xray/XrayPage'
import GroupsPage from '@/pages/stubs/GroupsPage'
import NodesPage from '@/pages/stubs/NodesPage'
import HostsPage from '@/pages/stubs/HostsPage'
import ApiDocsPage from '@/pages/stubs/ApiDocsPage'

// App: auth gate. Each page owns its own AntD ConfigProvider + Layout
// (mirrors 3x-ui: every top-level page renders AppSidebar itself so
// dashboard/inbounds/etc. can pick per-page antd theme tokens).
export default function App() {
  const [ready, setReady] = useState(false)
  const [authed, setAuthed] = useState(false)
  const loc = useLocation()
  const nav = useNavigate()

  useEffect(() => {
    API.session()
      .then(() => { setAuthed(true); setReady(true) })
      .catch((e) => {
        if (e instanceof APIError && e.status === 401) {
          setAuthed(false)
          if (loc.pathname !== '/login') nav('/login', { replace: true })
        }
        setReady(true)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Apply the persisted theme before first paint so the login page
  // doesn't flash light-mode on a dark-preferring admin's screen.
  useEffect(() => {
    const stored = (localStorage.getItem('hexplus-theme') as 'light' | 'dark' | null) ?? 'dark'
    document.documentElement.classList.toggle('dark', stored === 'dark')
  }, [])

  if (!ready) {
    return (
      <div className="min-h-screen grid place-items-center text-muted-foreground">
        <div className="flex items-center gap-3 text-sm">
          <span className="h-2 w-2 rounded-full bg-primary animate-pulse" /> loading…
        </div>
      </div>
    )
  }

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/"                  element={authed ? <IndexPage /> : <Navigate to="/login" replace />} />
      <Route path="/inbounds"          element={authed ? <Inbounds /> : <Navigate to="/login" replace />} />
      <Route path="/inbounds/:id/clients" element={authed ? <Clients /> : <Navigate to="/login" replace />} />
      <Route path="/outbound"          element={authed ? <Outbounds /> : <Navigate to="/login" replace />} />
      <Route path="/routing"           element={authed ? <Routing /> : <Navigate to="/login" replace />} />
      <Route path="/certs"             element={authed ? <Certs /> : <Navigate to="/login" replace />} />
      <Route path="/settings"          element={authed ? <Settings /> : <Navigate to="/login" replace />} />
      <Route path="/xray"              element={authed ? <XrayPage /> : <Navigate to="/login" replace />} />
      <Route path="/groups"            element={authed ? <GroupsPage /> : <Navigate to="/login" replace />} />
      <Route path="/nodes"             element={authed ? <NodesPage /> : <Navigate to="/login" replace />} />
      <Route path="/hosts"             element={authed ? <HostsPage /> : <Navigate to="/login" replace />} />
      <Route path="/api-docs"          element={authed ? <ApiDocsPage /> : <Navigate to="/login" replace />} />
      <Route path="*"                  element={<Navigate to="/" replace />} />
    </Routes>
  )
}
