// api.ts: fetch wrapper that
//   - joins URLs to the current <base href> (the panel serves the SPA
//     under a random URL prefix, so hard-coded paths would 404)
//   - reads the CSRF cookie and echoes it via X-CSRF-Token
//   - throws on non-2xx with the JSON error body when possible
//
// Every REST call from the UI goes through this so CSRF handling is
// in exactly one place.

function getCookie(name: string): string {
  const parts = document.cookie.split(';')
  for (const part of parts) {
    const trimmed = part.trim()
    if (trimmed.startsWith(name + '=')) {
      return decodeURIComponent(trimmed.slice(name.length + 1))
    }
  }
  return ''
}

export class APIError extends Error {
  status: number
  body: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  const csrf = getCookie('hexplus_csrf')
  if (csrf) headers.set('X-CSRF-Token', csrf)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(path, { ...init, headers, credentials: 'include' })
  const text = await res.text()
  let body: unknown = text
  if (text && res.headers.get('Content-Type')?.includes('application/json')) {
    try {
      body = JSON.parse(text)
    } catch {
      // keep as text
    }
  }
  if (!res.ok) {
    const msg = extractError(body) ?? `HTTP ${res.status}`
    throw new APIError(res.status, msg, body)
  }
  return body as T
}

function extractError(body: unknown): string | null {
  if (typeof body === 'object' && body !== null && 'error' in body) {
    const err = (body as { error: unknown }).error
    if (typeof err === 'string') return err
  }
  return null
}

// Endpoint helpers — one function per resource so pages stay declarative.
export const API = {
  session: () => api<{ ok: boolean }>('api/session'),
  login: (username: string, password: string) =>
    api<{ ok: boolean }>('login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  logout: () => api<{ ok: boolean }>('logout', { method: 'POST' }),

  inbounds: {
    list: () => api<Inbound[]>('api/inbounds'),
    get: (id: number) => api<Inbound>(`api/inbounds/${id}`),
    create: (body: Partial<Inbound>) =>
      api<{ id: number }>('api/inbounds', { method: 'POST', body: JSON.stringify(body) }),
    update: (id: number, body: Partial<Inbound>) =>
      api<{ id: number }>(`api/inbounds/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
    remove: (id: number) =>
      api<{ id: number }>(`api/inbounds/${id}`, { method: 'DELETE' }),
  },

  clients: {
    list: (inboundID: number) => api<Client[]>(`api/inbounds/${inboundID}/clients`),
    create: (inboundID: number, body: Partial<Client>) =>
      api<{ id: number }>(`api/inbounds/${inboundID}/clients`, { method: 'POST', body: JSON.stringify(body) }),
    update: (id: number, body: Partial<Client>) =>
      api<{ id: number }>(`api/clients/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
    remove: (id: number) =>
      api<{ id: number }>(`api/clients/${id}`, { method: 'DELETE' }),
    toggle: (id: number) => api<{ id: number; enabled: boolean }>(`api/clients/${id}/toggle`, { method: 'POST' }),
    reset: (id: number) => api<{ id: number }>(`api/clients/${id}/reset`, { method: 'POST' }),
    extend: (id: number, days: number) =>
      api<{ id: number; expires_at: number }>(`api/clients/${id}/extend?days=${days}`, { method: 'POST' }),
    link: (id: number, address = '') =>
      api<{ link: string }>(`api/clients/${id}/link${address ? '?address=' + encodeURIComponent(address) : ''}`),
    qrURL: (id: number, address = '') =>
      `api/clients/${id}/qr${address ? '?address=' + encodeURIComponent(address) : ''}`,
  },

  outbounds: {
    list: () => api<Outbound[]>('api/outbounds'),
    create: (body: Partial<Outbound>) =>
      api<{ id: number }>('api/outbounds', { method: 'POST', body: JSON.stringify(body) }),
    update: (id: number, body: Partial<Outbound>) =>
      api<{ id: number }>(`api/outbounds/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
    remove: (id: number) => api<{ id: number }>(`api/outbounds/${id}`, { method: 'DELETE' }),
    provisionWARP: (tag = 'warp') =>
      api<{ id: number; tag: string }>(`api/routing/warp?tag=${encodeURIComponent(tag)}`, { method: 'POST' }),
  },

  rules: {
    list: () => api<Rule[]>('api/routing/rules'),
    create: (body: Partial<Rule>) =>
      api<{ id: number }>('api/routing/rules', { method: 'POST', body: JSON.stringify(body) }),
    update: (id: number, body: Partial<Rule>) =>
      api<{ id: number }>(`api/routing/rules/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
    remove: (id: number) => api<{ id: number }>(`api/routing/rules/${id}`, { method: 'DELETE' }),
  },

  certs: {
    list: () => api<Cert[]>('api/certs'),
    uploadManual: (body: { domain: string; cert_pem: string; key_pem: string; remark?: string }) =>
      api<{ id: number; not_after: number }>('api/certs/manual', { method: 'POST', body: JSON.stringify(body) }),
    acme: (body: { domain: string; contact_email?: string; remark?: string }) =>
      api<{ id: number; not_after: number }>('api/certs/acme', { method: 'POST', body: JSON.stringify(body) }),
    renew: (id: number) => api<{ id: number; not_after: number }>(`api/certs/${id}/renew`, { method: 'POST' }),
    remove: (id: number) => api<{ id: number }>(`api/certs/${id}`, { method: 'DELETE' }),
  },
}

// Resource types — mirror the Go View structs in api_*.go.
export interface Inbound {
  id: number
  tag: string
  protocol: string
  listen: string
  port: number
  settings: unknown
  stream: unknown
  sniffing: boolean
  enabled: boolean
  remark: string
  total_up: number
  total_down: number
  created_at: number
  updated_at: number
}

export interface Client {
  id: number
  inbound_id: number
  email: string
  protocol: string
  uuid?: string
  password?: string
  key?: string
  quota_bytes: number
  used_bytes: number
  ip_limit: number
  expires_at: number
  enabled: boolean
  sub_token?: string
  created_at: number
}

export interface Outbound {
  id: number
  tag: string
  protocol: string
  settings: unknown
  stream: unknown
  remark: string
  enabled: boolean
  created_at: number
  updated_at: number
}

export interface Rule {
  id: number
  priority: number
  outbound_tag: string
  inbound_tag: string
  domains: string[]
  ips: string[]
  protocols: string[]
  port_range: string
  remark: string
  enabled: boolean
  created_at: number
  updated_at: number
}

export interface Cert {
  id: number
  domain: string
  source: string
  cert_path: string
  key_path: string
  not_after: number
  last_renewed_at: number
  remark: string
  created_at: number
}
