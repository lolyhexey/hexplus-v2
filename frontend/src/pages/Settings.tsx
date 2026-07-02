import { useCallback, useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  Alert, Button, Card, Col, ConfigProvider, Descriptions, Form, Input, InputNumber,
  Layout, Modal, Row, Space, Spin, Switch, Tabs, Tag, message, theme as antdTheme,
} from 'antd'
import {
  ApiOutlined, LockOutlined, MailOutlined, ReloadOutlined, SafetyOutlined,
  SendOutlined, SettingOutlined, WarningOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Settings } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'

// SettingsPage — port of 3x-ui/frontend/src/pages/settings/SettingsPage.tsx.
// Layout: Ant Tabs (card style) with URL hash routing so /settings#security
// deep-links to the Security tab from AppSidebar sub-menu.
//
// Tabs we host (matches 3x-ui order):
//   general      — panel port / listen / URL prefix
//   security     — admin username + password + prefix rotate
//   subscription — sub server toggle
//   telegram     — stubbed (bot not wired)
//   email        — stubbed (mail not wired)

const TAB_SLUGS = ['general', 'security', 'telegram', 'email', 'subscription']

export default function SettingsPage() {
  const { isDark } = useTheme()
  const location = useLocation()
  const navigate = useNavigate()
  const [messageApi, contextHolder] = message.useMessage()
  const [modal, modalContextHolder] = Modal.useModal()

  const [settings, setSettings] = useState<Settings | null>(null)
  const [loading, setLoading] = useState(true)

  const slug = location.hash.replace(/^#/, '')
  const activeTab = TAB_SLUGS.includes(slug) ? slug : 'general'

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setSettings(await API.settings.get())
    } catch (e) {
      messageApi.error(String(e))
    } finally { setLoading(false) }
  }, [messageApi])

  useEffect(() => { refresh() }, [refresh])

  const restartConfirm = () => modal.confirm({
    title: 'Restart panel?',
    icon: <WarningOutlined style={{ color: '#faad14' }} />,
    content: 'The web session will drop for ~2 seconds. Use the CLI menu (menu → 10 → 08 → 06) or SSH into the VPS to run: systemctl restart hexplus-panel',
    okText: 'Understood',
    cancelButtonProps: { style: { display: 'none' } },
  })

  const tabItems = useMemo(() => [
    {
      key: 'general',
      label: <span><SettingOutlined /> General</span>,
      children: settings && <GeneralTab settings={settings} onSaved={refresh}
                                        onRestart={restartConfirm} messageApi={messageApi} />,
    },
    {
      key: 'security',
      label: <span><SafetyOutlined /> Security</span>,
      children: settings && <SecurityTab settings={settings} onSaved={refresh}
                                          messageApi={messageApi} onRestart={restartConfirm} />,
    },
    {
      key: 'telegram',
      label: <span><SendOutlined /> Telegram</span>,
      children: <ComingSoonCard title="Telegram bot"
        note="Backend integration พร้อมใน roadmap phase 15 — ตอนนี้ยังไม่มี" />,
    },
    {
      key: 'email',
      label: <span><MailOutlined /> Email</span>,
      children: <ComingSoonCard title="Email notifications"
        note="สำหรับส่ง alert เมื่อ client ใกล้หมด quota / expiry — ยังไม่ทำ" />,
    },
    {
      key: 'subscription',
      label: <span><ApiOutlined /> Subscription</span>,
      children: settings && <SubscriptionTab settings={settings} onSaved={refresh}
                                              messageApi={messageApi} />,
    },
  ], [settings, refresh, messageApi])

  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    }}>
      {contextHolder}
      {modalContextHolder}
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout>
          <Layout.Content style={{ padding: 16 }}>
            <Spin spinning={loading && !settings} tip="Loading settings…">
              <Tabs
                type="card"
                activeKey={activeTab}
                onChange={(k) => navigate(`/settings#${k}`, { replace: false })}
                items={tabItems}
              />
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  )
}

// ── General ────────────────────────────────────────────────────────

function GeneralTab({
  settings, onSaved, onRestart, messageApi,
}: {
  settings: Settings
  onSaved: () => void
  onRestart: () => void
  messageApi: any
}) {
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    form.setFieldsValue({
      listen_addr: settings.listen_addr,
      port: settings.port,
      subscription_enabled: settings.subscription_enabled,
    })
  }, [settings, form])

  async function submit(values: any) {
    setBusy(true)
    try {
      const r = await API.settings.update({
        listen_addr: values.listen_addr,
        port: values.port,
        subscription_enabled: values.subscription_enabled,
      })
      onSaved()
      if (r.requires_restart) {
        messageApi.success({ content: 'Saved — restart required for port/bind change', duration: 4 })
        onRestart()
      } else {
        messageApi.success('Saved')
      }
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusy(false) }
  }

  return (
    <Card
      hoverable
      title={<Space><SettingOutlined />Panel — General</Space>}
      extra={<Button type="primary" loading={busy} onClick={() => form.submit()}>Save</Button>}
    >
      <Alert type="info" showIcon closable style={{ marginBottom: 16 }}
             message="Changes to port and listen address require a panel restart."
             description="Use CLI (menu → 10 → 08 → 06) or systemctl restart hexplus-panel." />

      <Descriptions column={1} bordered size="middle" style={{ marginBottom: 16 }}>
        <Descriptions.Item label="Current URL">
          <code>http://&lt;server&gt;:{settings.port}{settings.url_prefix}/</code>
        </Descriptions.Item>
        <Descriptions.Item label="Admin username">
          <Tag>{settings.admin_username}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="Xray version">{settings.xray_version}</Descriptions.Item>
      </Descriptions>

      <Form form={form} layout="vertical" onFinish={submit}>
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <Form.Item name="listen_addr" label="Listen address"
                        tooltip="0.0.0.0 = ทุก interface, 127.0.0.1 = loopback เท่านั้น">
              <Input placeholder="0.0.0.0" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="port" label="Panel port"
                        rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
        </Row>

        <Form.Item name="subscription_enabled" label="Enable subscription server"
                    valuePropName="checked"
                    tooltip="/sub/{token} endpoint สำหรับ client sync">
          <Switch />
        </Form.Item>
      </Form>
    </Card>
  )
}

// ── Security ───────────────────────────────────────────────────────

function SecurityTab({
  settings, onSaved, messageApi, onRestart,
}: {
  settings: Settings
  onSaved: () => void
  messageApi: any
  onRestart: () => void
}) {
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [rotBusy, setRotBusy] = useState(false)

  async function submit(values: any) {
    if (values.new_password !== values.new_password_confirm) {
      messageApi.error('รหัสผ่านใหม่ 2 ช่องไม่ตรงกัน'); return
    }
    if (values.new_password && values.new_password.length < 6) {
      messageApi.error('รหัสผ่านใหม่สั้นเกิน (>= 6 ตัวอักษร)'); return
    }
    setBusy(true)
    try {
      await API.settings.changePassword({
        current_password: values.current_password,
        new_password: values.new_password,
        new_username: values.new_username !== settings.admin_username ? values.new_username : undefined,
      })
      messageApi.success('Credential updated — other sessions revoked')
      form.setFieldsValue({ current_password: '', new_password: '', new_password_confirm: '' })
      onSaved()
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusy(false) }
  }

  async function rotate() {
    setRotBusy(true)
    try {
      const r = await API.settings.rotatePrefix()
      messageApi.success(`New URL prefix: ${r.url_prefix} — restart required`)
      onSaved()
      onRestart()
    } catch (e) { messageApi.error(String(e)) }
    finally { setRotBusy(false) }
  }

  return (
    <Row gutter={[16, 16]}>
      <Col span={24}>
        <Card hoverable title={<Space><LockOutlined />Admin credentials</Space>}
              extra={<Button type="primary" loading={busy} onClick={() => form.submit()}>Change</Button>}>
          <Form form={form} layout="vertical" onFinish={submit}
                initialValues={{ new_username: settings.admin_username }}>
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <Form.Item name="new_username" label="Username">
                  <Input autoComplete="username" />
                </Form.Item>
              </Col>
              <Col xs={24} md={12}>
                <Form.Item name="current_password" label="Current password"
                            rules={[{ required: true, message: 'required' }]}>
                  <Input.Password autoComplete="current-password" />
                </Form.Item>
              </Col>
              <Col xs={24} md={12}>
                <Form.Item name="new_password" label="New password"
                            rules={[{ min: 6, message: '>= 6 ตัวอักษร' }]}>
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
              </Col>
              <Col xs={24} md={12}>
                <Form.Item name="new_password_confirm" label="Confirm new password">
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
              </Col>
            </Row>
          </Form>
        </Card>
      </Col>

      <Col span={24}>
        <Card hoverable title={<Space><SafetyOutlined />URL prefix</Space>}>
          <Alert type="warning" showIcon style={{ marginBottom: 16 }}
                 message="Current URL prefix"
                 description={<code style={{ fontSize: 13 }}>{settings.url_prefix}</code>} />
          <p style={{ opacity: 0.7, fontSize: 13 }}>
            Rotate the URL prefix if you suspect it has leaked. Old
            bookmarks will 404 after restart.
          </p>
          <Button icon={<ReloadOutlined />} loading={rotBusy} onClick={rotate}>
            Rotate URL prefix
          </Button>
        </Card>
      </Col>
    </Row>
  )
}

// ── Subscription ───────────────────────────────────────────────────

function SubscriptionTab({
  settings, onSaved, messageApi,
}: {
  settings: Settings
  onSaved: () => void
  messageApi: any
}) {
  const [busy, setBusy] = useState(false)
  const [enabled, setEnabled] = useState(settings.subscription_enabled)

  useEffect(() => { setEnabled(settings.subscription_enabled) }, [settings])

  async function toggle(v: boolean) {
    setEnabled(v)
    setBusy(true)
    try {
      await API.settings.update({ subscription_enabled: v })
      messageApi.success(v ? 'Subscription enabled' : 'Subscription disabled')
      onSaved()
    } catch (e) {
      setEnabled(!v)
      messageApi.error(String(e))
    } finally { setBusy(false) }
  }

  return (
    <Card hoverable title={<Space><ApiOutlined />Subscription server</Space>}>
      <Descriptions column={1} bordered style={{ marginBottom: 16 }}>
        <Descriptions.Item label="Endpoint">
          <code>/sub/&lt;token&gt;</code>
        </Descriptions.Item>
        <Descriptions.Item label="Response formats">
          <Space wrap>
            <Tag color="blue">?type=v2ray</Tag>
            <Tag color="cyan">?type=plain</Tag>
            <Tag color="default" style={{ opacity: 0.5 }}>clash (planned)</Tag>
            <Tag color="default" style={{ opacity: 0.5 }}>sing-box (planned)</Tag>
          </Space>
        </Descriptions.Item>
        <Descriptions.Item label="Headers">
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            <li><code>Subscription-Userinfo</code></li>
            <li><code>Profile-Update-Interval</code></li>
            <li><code>Profile-Title</code></li>
          </ul>
        </Descriptions.Item>
      </Descriptions>

      <Space align="baseline" size="large">
        <span style={{ fontWeight: 500 }}>Enable subscription server</span>
        <Switch checked={enabled} loading={busy} onChange={toggle} />
      </Space>

      <Alert type="info" showIcon style={{ marginTop: 16 }}
             message="Each client gets an opaque token generated on create — the token IS the credential (do not include it in a public URL)."
             description="Rotate a client's token by deleting + recreating them from the Clients page." />
    </Card>
  )
}

// ── ComingSoonCard ─────────────────────────────────────────────────

function ComingSoonCard({ title, note }: { title: string; note: string }) {
  return (
    <Card hoverable title={title}>
      <Alert type="info" showIcon
             message="Coming soon"
             description={note} />
    </Card>
  )
}
