import { useEffect, useState } from 'react'
import {
  Card, Col, ConfigProvider, Empty, Input, Layout, Row, Space, Tag,
  theme as antdTheme,
} from 'antd'
import { ApiOutlined, LockOutlined, SearchOutlined } from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { useTheme } from '@/hooks/useTheme'

// ApiDocsPage — a static browser for the panel's REST surface. 3x-ui
// ships a Swagger UI here; ours is hand-crafted so we don't need to
// generate and embed an OpenAPI spec yet.
//
// The list is exhaustive for endpoints exposed by internal/panel —
// keep in sync with api_*.go when adding new routes.

interface Route {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  desc: string
  group: string
}

const ROUTES: Route[] = [
  // Session
  { group: 'Session', method: 'POST',   path: '/login',              desc: 'Login with username + password' },
  { group: 'Session', method: 'POST',   path: '/logout',             desc: 'Kill current session' },
  { group: 'Session', method: 'GET',    path: '/api/session',        desc: 'Whoami — 401 when not logged in' },

  // Server / Xray
  { group: 'Server',  method: 'GET',    path: '/api/server/status',       desc: 'System stats (CPU, mem, disk, xray)' },
  { group: 'Server',  method: 'GET',    path: '/api/server/xray/config',  desc: 'Current xray config.json (read-only)' },
  { group: 'Server',  method: 'POST',   path: '/api/server/xray/restart', desc: 'Restart hexplus-xray.service' },
  { group: 'Server',  method: 'POST',   path: '/api/server/xray/stop',    desc: 'Stop hexplus-xray.service' },
  { group: 'Server',  method: 'GET',    path: '/api/server/xray/log',     desc: 'journalctl tail (?lines=N)' },

  // Inbounds
  { group: 'Inbounds', method: 'GET',    path: '/api/inbounds',           desc: 'List inbounds' },
  { group: 'Inbounds', method: 'POST',   path: '/api/inbounds',           desc: 'Create inbound' },
  { group: 'Inbounds', method: 'GET',    path: '/api/inbounds/{id}',      desc: 'Read one' },
  { group: 'Inbounds', method: 'PUT',    path: '/api/inbounds/{id}',      desc: 'Update' },
  { group: 'Inbounds', method: 'DELETE', path: '/api/inbounds/{id}',      desc: 'Delete' },

  // Clients
  { group: 'Clients', method: 'GET',    path: '/api/inbounds/{id}/clients', desc: 'List clients under an inbound' },
  { group: 'Clients', method: 'POST',   path: '/api/inbounds/{id}/clients', desc: 'Create client' },
  { group: 'Clients', method: 'PUT',    path: '/api/clients/{cid}',         desc: 'Update client' },
  { group: 'Clients', method: 'DELETE', path: '/api/clients/{cid}',         desc: 'Delete client' },
  { group: 'Clients', method: 'POST',   path: '/api/clients/{cid}/toggle',  desc: 'Flip enabled' },
  { group: 'Clients', method: 'POST',   path: '/api/clients/{cid}/reset',   desc: 'Reset used_bytes counter' },
  { group: 'Clients', method: 'POST',   path: '/api/clients/{cid}/extend',  desc: 'Extend expiry (?days=N)' },
  { group: 'Clients', method: 'GET',    path: '/api/clients/{cid}/link',    desc: 'Compute share URI' },
  { group: 'Clients', method: 'GET',    path: '/api/clients/{cid}/qr',      desc: 'PNG QR code of the share link' },

  // Outbounds
  { group: 'Outbounds', method: 'GET',    path: '/api/outbounds',            desc: 'List user-defined outbounds' },
  { group: 'Outbounds', method: 'POST',   path: '/api/outbounds',            desc: 'Create outbound' },
  { group: 'Outbounds', method: 'PUT',    path: '/api/outbounds/{id}',       desc: 'Update' },
  { group: 'Outbounds', method: 'DELETE', path: '/api/outbounds/{id}',       desc: 'Delete' },
  { group: 'Outbounds', method: 'POST',   path: '/api/routing/warp',         desc: 'Provision Cloudflare WARP' },

  // Routing
  { group: 'Routing', method: 'GET',    path: '/api/routing/rules',        desc: 'List rules' },
  { group: 'Routing', method: 'POST',   path: '/api/routing/rules',        desc: 'Create rule' },
  { group: 'Routing', method: 'PUT',    path: '/api/routing/rules/{id}',   desc: 'Update rule' },
  { group: 'Routing', method: 'DELETE', path: '/api/routing/rules/{id}',   desc: 'Delete rule' },

  // Certs
  { group: 'Certs', method: 'GET',    path: '/api/certs',              desc: 'List certificates' },
  { group: 'Certs', method: 'POST',   path: '/api/certs/manual',       desc: 'Upload PEM cert + key' },
  { group: 'Certs', method: 'POST',   path: '/api/certs/acme',         desc: "Let's Encrypt HTTP-01 flow" },
  { group: 'Certs', method: 'POST',   path: '/api/certs/{id}/renew',   desc: 'Re-run ACME for existing cert' },
  { group: 'Certs', method: 'DELETE', path: '/api/certs/{id}',         desc: 'Delete cert' },

  // Settings
  { group: 'Settings', method: 'GET',  path: '/api/settings',                 desc: 'Read panel config' },
  { group: 'Settings', method: 'PUT',  path: '/api/settings',                 desc: 'Update port/listen/subscription' },
  { group: 'Settings', method: 'POST', path: '/api/settings/password',        desc: 'Change admin credential' },
  { group: 'Settings', method: 'POST', path: '/api/settings/url-prefix/rotate', desc: 'Rotate the URL prefix' },

  // Probe
  { group: 'Probe',   method: 'GET',    path: '/api/probe/port',             desc: 'Is a TCP port in use?' },

  // Subscription
  { group: 'Subscription', method: 'GET', path: '/sub/{token}', desc: 'Public — sub link for one client (?type=v2ray|plain)' },
]

const METHOD_COLOR: Record<string, string> = {
  GET: 'green', POST: 'blue', PUT: 'gold', DELETE: 'red',
}

export default function ApiDocsPage() {
  const { isDark } = useTheme()
  const [query, setQuery] = useState('')

  const filtered = ROUTES.filter((r) =>
    (r.path + r.desc + r.group).toLowerCase().includes(query.toLowerCase()),
  )

  // Group filtered routes by category preserving the original section order.
  const grouped: Record<string, Route[]> = {}
  for (const r of filtered) {
    if (!grouped[r.group]) grouped[r.group] = []
    grouped[r.group].push(r)
  }

  useEffect(() => { document.title = 'API — HEXPLUS Panel' }, [])

  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    }}>
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout>
          <Layout.Content style={{ padding: 16 }}>
            <Card
              hoverable
              title={<Space><ApiOutlined /> REST API</Space>}
              extra={
                <Input
                  allowClear
                  prefix={<SearchOutlined />}
                  placeholder="Search endpoints"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  style={{ width: 260 }}
                />
              }
            >
              <div style={{ marginBottom: 12, opacity: 0.7 }}>
                <Space size="small">
                  <span>Authentication:</span>
                  <Tag icon={<LockOutlined />}>session cookie + X-CSRF-Token</Tag>
                  <span>except</span>
                  <Tag>public /sub/*</Tag>
                </Space>
              </div>

              {Object.keys(grouped).length === 0 && (
                <Empty description="No endpoints match your search" />
              )}

              {Object.entries(grouped).map(([group, routes]) => (
                <div key={group} style={{ marginTop: 12 }}>
                  <div style={{
                    fontSize: 11, textTransform: 'uppercase', letterSpacing: 1,
                    fontWeight: 600, opacity: 0.55, marginBottom: 4,
                  }}>{group}</div>
                  <Row gutter={[0, 6]}>
                    {routes.map((r) => (
                      <Col span={24} key={r.method + r.path}>
                        <div style={{
                          display: 'flex', alignItems: 'baseline', gap: 12,
                          padding: '8px 10px', borderRadius: 6,
                          border: '1px solid var(--border, rgba(255,255,255,.08))',
                        }}>
                          <Tag color={METHOD_COLOR[r.method]} style={{ minWidth: 62, textAlign: 'center', margin: 0 }}>
                            {r.method}
                          </Tag>
                          <code style={{ fontSize: 13, minWidth: 280 }}>{r.path}</code>
                          <span style={{ opacity: 0.7 }}>{r.desc}</span>
                        </div>
                      </Col>
                    ))}
                  </Row>
                </div>
              ))}
            </Card>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  )
}
