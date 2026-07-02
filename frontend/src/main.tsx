import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import './index.css'

// react-router v6 reads BrowserRouter's basename to prefix every route
// push/pop. The <base href> tag in index.html handles relative URLs for
// fetch() and asset loads, but history.pushState bypasses it — without
// basename here, navigate('/') writes '/' straight to window.location
// and drops the panel's URL prefix, breaking every post-login redirect.
//
// The Go handler rewrites <base href> to include the current prefix
// (see internal/panel/frontend/embed.go), so reading it here gives us
// the same value the panel injected without a build-time env var.
function basenameFromDoc(): string {
  const el = document.getElementById('hexplus-base') as HTMLBaseElement | null
  if (!el) return '/'
  // el.href is absolute; take its pathname and trim any trailing slash
  // (react-router prefers no trailing slash, and mounts "" as "/").
  const path = new URL(el.href, window.location.origin).pathname
  return path.replace(/\/$/, '') || '/'
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter basename={basenameFromDoc()}>
      <App />
    </BrowserRouter>
  </React.StrictMode>,
)
