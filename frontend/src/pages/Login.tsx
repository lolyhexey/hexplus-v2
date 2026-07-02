import { useEffect, useMemo, useState } from 'react'
import {
  Button, ConfigProvider, Form, Input, Layout, Space, message, theme as antdTheme,
} from 'antd'
import { LockOutlined, MoonOutlined, SunOutlined, UserOutlined } from '@ant-design/icons'
import { API, APIError } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'

// LoginPage — port of 3x-ui/frontend/src/pages/login/LoginPage.tsx.
// Two-column layout: rotating headline on the left, form on the right.
// Rotating headline mirrors 3x-ui's 2-second alternating h1.

const HEADLINE_INTERVAL_MS = 2000
const HEADLINES = ['Welcome to', 'HEXPLUS Panel']

export default function Login() {
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [headlineIndex, setHeadlineIndex] = useState(0)
  const { isDark, toggle } = useTheme()
  const [messageApi, contextHolder] = message.useMessage()

  useEffect(() => {
    const id = window.setInterval(() => setHeadlineIndex((i) => (i + 1) % HEADLINES.length),
      HEADLINE_INTERVAL_MS)
    return () => window.clearInterval(id)
  }, [])

  async function submit(values: { username: string; password: string }) {
    setBusy(true); setErr(null)
    try {
      await API.login(values.username, values.password)
      const base = (document.getElementById('hexplus-base') as HTMLBaseElement | null)?.href
        || window.location.origin + '/'
      window.location.href = base
    } catch (e) {
      const msg = e instanceof APIError ? e.message : 'login failed'
      setErr(msg)
      messageApi.error(msg)
      setBusy(false)
    }
  }

  const headline = useMemo(() => HEADLINES[headlineIndex], [headlineIndex])

  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 8 },
    }}>
      {contextHolder}
      <Layout style={{ minHeight: '100vh', position: 'relative' }}>
        {/* Theme toggle floating top-right */}
        <button
          type="button"
          onClick={toggle}
          aria-label="Toggle theme"
          style={{
            position: 'absolute', top: 16, right: 16, zIndex: 1,
            background: 'transparent', border: '1px solid var(--border, rgba(255,255,255,.12))',
            borderRadius: 8, padding: '6px 10px', color: 'inherit', cursor: 'pointer',
          }}
        >
          {isDark ? <SunOutlined /> : <MoonOutlined />}
        </button>

        {/* Background gradient blob */}
        <div style={{
          position: 'absolute', inset: 0, pointerEvents: 'none',
          background: 'radial-gradient(60% 50% at 30% 40%, rgba(22,119,255,0.16), transparent 70%)',
        }} />

        <Layout.Content style={{
          display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24,
        }}>
          <div style={{
            display: 'grid', gap: 24, gridTemplateColumns: 'minmax(0,1fr) minmax(0,400px)',
            maxWidth: 900, width: '100%', alignItems: 'center',
          }}>
            {/* Left: rotating headline */}
            <div>
              <div style={{ opacity: 0.55, fontSize: 14, letterSpacing: 2, textTransform: 'uppercase' }}>
                Xray panel
              </div>
              <h1 style={{
                fontSize: 44, lineHeight: 1.1, margin: '12px 0 20px 0', fontWeight: 700,
                transition: 'opacity 300ms ease',
              }}>
                {headline}
              </h1>
              <p style={{ opacity: 0.7, fontSize: 15, maxWidth: 380 }}>
                จัดการ V2Ray/Xray inbound, client, traffic, และ subscription
                จากที่เดียว
              </p>
            </div>

            {/* Right: login form card */}
            <div style={{
              background: 'var(--card, #1f1f1f)',
              border: '1px solid var(--border, rgba(255,255,255,.08))',
              borderRadius: 12, padding: 24, backdropFilter: 'blur(6px)',
            }}>
              <div style={{
                display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4,
              }}>
                <div style={{
                  display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                  width: 36, height: 36, borderRadius: 8, background: '#1677ff',
                  color: '#fff', fontWeight: 700, fontSize: 16,
                }}>H</div>
                <div>
                  <div style={{ fontWeight: 600 }}>HEXPLUS</div>
                  <div style={{ fontSize: 11, opacity: 0.6 }}>V2Ray Panel</div>
                </div>
              </div>
              <p style={{ marginTop: 8, marginBottom: 20, opacity: 0.65, fontSize: 13 }}>
                Sign in to continue
              </p>

              <Form form={form} layout="vertical" onFinish={submit}
                    initialValues={{ username: 'admin' }}>
                <Form.Item name="username" label="Username"
                            rules={[{ required: true, message: 'Enter username' }]}>
                  <Input prefix={<UserOutlined />} autoComplete="username" />
                </Form.Item>
                <Form.Item name="password" label="Password"
                            rules={[{ required: true, message: 'Enter password' }]}>
                  <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
                </Form.Item>
                {err && (
                  <div style={{
                    background: 'rgba(255,77,79,.1)', color: '#ff4d4f',
                    padding: '6px 10px', borderRadius: 6, fontSize: 13, marginBottom: 12,
                  }}>{err}</div>
                )}
                <Button type="primary" htmlType="submit" block loading={busy} size="large">
                  Sign in
                </Button>
              </Form>

              <Space size="small" style={{ marginTop: 16, opacity: 0.5, fontSize: 12 }}>
                <span>HTTP-only panel — use SSH tunnel for public access</span>
              </Space>
            </div>
          </div>
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  )
}
