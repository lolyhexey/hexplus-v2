import { useEffect, useState } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { API, APIError } from '@/lib/api'
import { Nav } from '@/components/Nav'
import Login from '@/pages/Login'
import Dashboard from '@/pages/Dashboard'
import Inbounds from '@/pages/Inbounds'
import Clients from '@/pages/Clients'
import Outbounds from '@/pages/Outbounds'
import Routing from '@/pages/Routing'
import Certs from '@/pages/Certs'
import Settings from '@/pages/Settings'

// App: authentication gate + top-level routing.
//
// On mount we probe /api/session; a 401 sends us to /login. Any other
// error is left for the child page to surface — the panel-is-broken
// state is a legitimate reason to still render the shell.
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

  if (!ready) return <div className="p-8 text-muted-foreground">โหลด...</div>

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/*" element={authed ? <Shell /> : <Navigate to="/login" replace />} />
    </Routes>
  )
}

function Shell() {
  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="flex-1 p-6 overflow-auto">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/inbounds" element={<Inbounds />} />
          <Route path="/inbounds/:id/clients" element={<Clients />} />
          <Route path="/outbounds" element={<Outbounds />} />
          <Route path="/routing" element={<Routing />} />
          <Route path="/certs" element={<Certs />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </main>
    </div>
  )
}
