import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  Badge, Button, Card, Col, ConfigProvider, Dropdown, Form, Input, InputNumber, Layout, Modal,
  Progress, Row, Space, Statistic, Switch, Table, Tag, message, theme as antdTheme,
  type MenuProps, type TableColumnsType,
} from 'antd'
import {
  ArrowLeftOutlined, ClockCircleOutlined,
  CopyOutlined, DeleteOutlined, DoubleRightOutlined, EditOutlined, LinkOutlined, MoreOutlined,
  PlusOutlined, QrcodeOutlined, ReloadOutlined, TeamOutlined, UsergroupAddOutlined,
} from '@ant-design/icons'
import ClientBulkAddModal from '@/pages/clients/ClientBulkAddModal'
import ClientBulkAdjustModal from '@/pages/clients/ClientBulkAdjustModal'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Client, type Inbound } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'
import { formatBytes } from '@/pages/index/formatters'

// ClientsPage — 3x-ui-shaped view scoped to one inbound. Backend mirrors
// what 3x-ui exposes: enable/disable, quota, expiry, IP limit, share
// link, QR, reset traffic. Table renders per-client rows with a traffic
// progress bar (used/quota) and expiry countdown Tag.

const protocolColor: Record<string, string> = {
  vless: 'geekblue', vmess: 'blue', trojan: 'volcano',
  shadowsocks: 'purple', hysteria2: 'magenta', wireguard: 'cyan',
  http: 'gold', socks: 'orange',
}

export default function ClientsPage() {
  const { id } = useParams<{ id: string }>()
  const inboundID = Number(id)
  const { isDark } = useTheme()
  const navigate = useNavigate()
  const [messageApi, contextHolder] = message.useMessage()

  const [rows, setRows] = useState<Client[]>([])
  const [inbound, setInbound] = useState<Inbound | null>(null)
  const [loading, setLoading] = useState(true)

  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<Client | null>(null)
  const [qrOpen, setQrOpen] = useState<Client | null>(null)
  const [qrDataURL, setQrDataURL] = useState<string>('')
  const [shareLink, setShareLink] = useState<string>('')
  const [bulkAddOpen, setBulkAddOpen] = useState(false)
  const [bulkAdjustOpen, setBulkAdjustOpen] = useState(false)
  const [selectedKeys, setSelectedKeys] = useState<number[]>([])
  const [rowBusyId, setRowBusyId] = useState<number | null>(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const [c, i] = await Promise.all([
        API.clients.list(inboundID),
        API.inbounds.get(inboundID).catch(() => null),
      ])
      setRows(c ?? [])
      setInbound(i)
    } catch (e) {
      messageApi.error(String(e))
    } finally { setLoading(false) }
  }, [inboundID, messageApi])

  useEffect(() => { refresh() }, [refresh])

  const totals = useMemo(() => {
    const now = Math.floor(Date.now() / 1000)
    return {
      active:  rows.filter((r) => r.enabled && (!r.expires_at || r.expires_at > now)
                              && (!r.quota_bytes || r.used_bytes < r.quota_bytes)).length,
      expired: rows.filter((r) => r.expires_at && r.expires_at <= now).length,
      quotaOver: rows.filter((r) => r.quota_bytes && r.used_bytes >= r.quota_bytes).length,
    }
  }, [rows])

  const toggle = useCallback(async (row: Client) => {
    setRowBusyId(row.id)
    try { await API.clients.toggle(row.id); await refresh() }
    catch (e) { messageApi.error(String(e)) }
    finally { setRowBusyId(null) }
  }, [refresh, messageApi])

  const resetTraffic = useCallback(async (row: Client) => {
    setRowBusyId(row.id)
    try { await API.clients.reset(row.id); messageApi.success('reset'); await refresh() }
    catch (e) { messageApi.error(String(e)) }
    finally { setRowBusyId(null) }
  }, [refresh, messageApi])

  const remove = useCallback(async (row: Client) => {
    if (!confirm(`ลบ client "${row.email}"?`)) return
    setRowBusyId(row.id)
    try { await API.clients.remove(row.id); messageApi.success('deleted'); await refresh() }
    catch (e) { messageApi.error(String(e)) }
    finally { setRowBusyId(null) }
  }, [refresh, messageApi])

  const openQR = useCallback(async (row: Client) => {
    try {
      const link = (await API.clients.link(row.id)).link
      setShareLink(link)
      setQrDataURL(API.clients.qrURL(row.id))
      setQrOpen(row)
    } catch (e) { messageApi.error(String(e)) }
  }, [messageApi])

  const columns: TableColumnsType<Client> = useMemo(() => [
    { title: '#', dataIndex: 'id', width: 64, fixed: 'left' },
    {
      title: 'Enable', width: 90, align: 'center', dataIndex: 'enabled',
      render: (v: boolean, row) => <Switch checked={v} onChange={() => toggle(row)} />,
    },
    {
      title: 'Email / name', dataIndex: 'email',
      render: (v: string, row) => (
        <Space direction="vertical" size={0}>
          <span style={{ fontWeight: 500 }}>{v}</span>
          <Tag color={protocolColor[row.protocol] || 'default'} style={{ marginTop: 2 }}>
            {row.protocol}
          </Tag>
        </Space>
      ),
    },
    {
      title: 'Traffic', width: 220,
      render: (_: unknown, row) => {
        const q = row.quota_bytes
        const u = row.used_bytes
        const pct = q > 0 ? Math.min(100, Math.round((u / q) * 100)) : 0
        return (
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            <span style={{ fontSize: 12 }}>
              {formatBytes(u)} / {q > 0 ? formatBytes(q) : '∞'}
            </span>
            {q > 0 && (
              <Progress percent={pct} showInfo={false} size="small"
                        strokeColor={pct >= 100 ? '#ff4d4f' : pct >= 80 ? '#faad14' : '#1677ff'} />
            )}
          </Space>
        )
      },
    },
    {
      title: 'Expires', width: 160,
      render: (_: unknown, row) => {
        if (!row.expires_at) return <Tag icon={<ClockCircleOutlined />}>Never</Tag>
        const now = Math.floor(Date.now() / 1000)
        const daysLeft = Math.floor((row.expires_at - now) / 86400)
        const dt = new Date(row.expires_at * 1000).toLocaleDateString('th-TH')
        if (row.expires_at <= now) return <Tag color="red">Expired {dt}</Tag>
        return <Tag color={daysLeft < 7 ? 'orange' : 'green'}>{dt} ({daysLeft}d)</Tag>
      },
    },
    {
      title: 'IP limit', dataIndex: 'ip_limit', width: 90, align: 'center',
      render: (v: number) => v > 0 ? v : <span style={{ opacity: 0.4 }}>∞</span>,
    },
    {
      title: '', width: 60, fixed: 'right', align: 'center',
      render: (_: unknown, row) => {
        const menu: MenuProps['items'] = [
          { key: 'qr',     icon: <QrcodeOutlined />, label: 'QR / Share' },
          { key: 'link',   icon: <LinkOutlined />,   label: 'Copy link' },
          { key: 'edit',   icon: <EditOutlined />,   label: 'Edit' },
          { key: 'reset',  icon: <ReloadOutlined />, label: 'Reset traffic' },
          { type: 'divider' },
          { key: 'delete', icon: <DeleteOutlined />, label: 'Delete', danger: true },
        ]
        return (
          <Dropdown menu={{
            items: menu,
            onClick: async ({ key }) => {
              switch (key) {
                case 'qr': openQR(row); break
                case 'link':
                  try {
                    const link = (await API.clients.link(row.id)).link
                    await navigator.clipboard.writeText(link)
                    messageApi.success('copied')
                  } catch (e) { messageApi.error(String(e)) }
                  break
                case 'edit': setEditing(row); break
                case 'reset': resetTraffic(row); break
                case 'delete': remove(row); break
              }
            }
          }} trigger={['click']} placement="bottomRight">
            <Button size="small" icon={<MoreOutlined />} loading={rowBusyId === row.id} />
          </Dropdown>
        )
      },
    },
  ], [messageApi, openQR, remove, resetTraffic, toggle, rowBusyId])

  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    }}>
      {contextHolder}
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout>
          <Layout.Content style={{ padding: 16 }}>
            <Row gutter={[16, 12]}>
              <Col xs={24}>
                <Space>
                  <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/inbounds')}>Back to inbounds</Button>
                  {inbound && (
                    <Space size="small">
                      <span style={{ color: 'var(--muted-foreground)' }}>Inbound:</span>
                      <span style={{ fontWeight: 500 }}>{inbound.tag}</span>
                      <Tag color={protocolColor[inbound.protocol] || 'default'}>{inbound.protocol}</Tag>
                      <span style={{ opacity: 0.7 }}>{inbound.listen}:{inbound.port}</span>
                    </Space>
                  )}
                </Space>
              </Col>

              <Col xs={24} md={6}>
                <Card><Statistic title="Total"
                  value={rows.length}
                  prefix={<TeamOutlined />} /></Card>
              </Col>
              <Col xs={24} md={6}>
                <Card><Statistic title="Active"
                  value={totals.active}
                  valueStyle={{ color: '#52c41a' }} /></Card>
              </Col>
              <Col xs={24} md={6}>
                <Card><Statistic title="Expired"
                  value={totals.expired}
                  valueStyle={{ color: '#faad14' }} /></Card>
              </Col>
              <Col xs={24} md={6}>
                <Card><Statistic title="Over quota"
                  value={totals.quotaOver}
                  valueStyle={{ color: '#ff4d4f' }} /></Card>
              </Col>

              <Col span={24}>
                <Card
                  hoverable
                  title={
                    <Space wrap>
                      <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
                        Add client
                      </Button>
                      <Button icon={<UsergroupAddOutlined />} onClick={() => setBulkAddOpen(true)}>
                        Bulk add
                      </Button>
                      {selectedKeys.length > 0 && (
                        <>
                          <Tag color="blue" closable onClose={() => setSelectedKeys([])}
                               style={{ marginInlineEnd: 0 }}>
                            {selectedKeys.length} selected
                          </Tag>
                          <Button icon={<DoubleRightOutlined />} onClick={() => setBulkAdjustOpen(true)}>
                            Bulk adjust
                          </Button>
                        </>
                      )}
                      <Badge count={rows.length} showZero color="#1677ff" />
                    </Space>
                  }
                >
                  <Table<Client>
                    rowKey="id"
                    size="middle"
                    loading={loading}
                    columns={columns}
                    dataSource={rows}
                    rowSelection={{
                      selectedRowKeys: selectedKeys,
                      onChange: (keys) => setSelectedKeys(keys as number[]),
                    }}
                    pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
                    scroll={{ x: 900 }}
                  />
                </Card>
              </Col>
            </Row>
          </Layout.Content>
        </Layout>
      </Layout>

      <ClientFormModal
        open={addOpen || !!editing}
        editing={editing}
        inbound={inbound}
        onClose={() => { setAddOpen(false); setEditing(null) }}
        onSaved={() => { setAddOpen(false); setEditing(null); refresh() }}
      />

      <ClientBulkAddModal
        open={bulkAddOpen}
        inbound={inbound}
        onClose={() => setBulkAddOpen(false)}
        onSaved={() => { setBulkAddOpen(false); refresh() }}
      />

      <ClientBulkAdjustModal
        open={bulkAdjustOpen}
        selected={rows.filter((c) => selectedKeys.includes(c.id))}
        onClose={() => setBulkAdjustOpen(false)}
        onSaved={() => { setBulkAdjustOpen(false); setSelectedKeys([]); refresh() }}
      />

      <Modal
        open={!!qrOpen}
        onCancel={() => setQrOpen(null)}
        title={qrOpen ? `Share — ${qrOpen.email}` : ''}
        footer={null}
        width={420}
        destroyOnHidden
      >
        <div style={{ textAlign: 'center' }}>
          {qrDataURL && (
            <img src={qrDataURL} alt="QR" style={{
              border: '1px solid var(--border)', borderRadius: 6, padding: 8, background: '#fff',
              maxWidth: 320, width: '100%',
            }} />
          )}
          <Input.TextArea rows={3} value={shareLink} readOnly
                          style={{ marginTop: 12, fontFamily: 'monospace', fontSize: 12 }} />
          <Button icon={<CopyOutlined />} block style={{ marginTop: 8 }}
                  onClick={() => { navigator.clipboard.writeText(shareLink); messageApi.success('copied') }}>
            Copy link
          </Button>
        </div>
      </Modal>
    </ConfigProvider>
  )
}

// ── Client form modal ──────────────────────────────────────────────

function ClientFormModal({
  open, editing, inbound, onClose, onSaved,
}: {
  open: boolean
  editing: Client | null
  inbound: Inbound | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    form.setFieldsValue(editing ? {
      email: editing.email,
      quota_gb: editing.quota_bytes ? Math.round(editing.quota_bytes / (1024 ** 3)) : 0,
      expire_days: editing.expires_at
        ? Math.max(0, Math.floor((editing.expires_at - Date.now() / 1000) / 86400))
        : 0,
      ip_limit: editing.ip_limit,
      uuid: editing.uuid ?? '',
      password: editing.password ?? '',
      key: editing.key ?? '',
      enabled: editing.enabled,
    } : {
      email: '', quota_gb: 0, expire_days: 0, ip_limit: 0,
      uuid: '', password: '', key: '', enabled: true,
    })
  }, [open, editing, form])

  const protocol = inbound?.protocol ?? ''

  async function finish(values: any) {
    if (!inbound) return
    setBusy(true)
    try {
      const body: any = {
        email: values.email,
        quota_bytes: (values.quota_gb || 0) * 1024 ** 3,
        expires_at: values.expire_days > 0
          ? Math.floor(Date.now() / 1000) + values.expire_days * 86400 : 0,
        ip_limit: values.ip_limit || 0,
        enabled: values.enabled,
      }
      if (values.uuid) body.uuid = values.uuid
      if (values.password) body.password = values.password
      if (values.key) body.key = values.key
      if (editing) await API.clients.update(editing.id, body)
      else await API.clients.create(inbound.id, body)
      onSaved()
    } catch { /* backend error message will show via API layer */ }
    finally { setBusy(false) }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={editing ? `Edit — ${editing.email}` : 'New client'}
      footer={null}
      width={560}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" onFinish={finish} style={{ paddingTop: 8 }}>
        <Space size="middle" style={{ display: 'flex' }}>
          <Form.Item name="email" label="Email / Name" style={{ flex: 1 }}
                     rules={[{ required: true, message: 'required' }]}>
            <Input placeholder="alice@example.com" />
          </Form.Item>
          <Form.Item name="enabled" label="Enable" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Space>

        <Space size="middle" style={{ display: 'flex' }}>
          <Form.Item name="quota_gb" label="Quota (GB)" style={{ flex: 1 }}
                     tooltip="0 = ไม่จำกัด">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="expire_days" label="Expire (days)" style={{ flex: 1 }}
                     tooltip="0 = ไม่หมดอายุ">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="ip_limit" label="IP limit" style={{ flex: 1 }}
                     tooltip="0 = ไม่จำกัด">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Space>

        <Typography>Credential</Typography>
        <div style={{ fontSize: 12, opacity: 0.7, marginBottom: 8 }}>
          เว้นว่างให้ระบบสุ่มอัตโนมัติตาม protocol
        </div>

        {(protocol === 'vless' || protocol === 'vmess') && (
          <Form.Item name="uuid" label="UUID">
            <Input addonAfter={
              <Button type="link" size="small"
                      onClick={() => form.setFieldValue('uuid', crypto.randomUUID())}>
                Rand
              </Button>
            } placeholder="ปล่อยว่าง = สุ่มให้" />
          </Form.Item>
        )}
        {(protocol === 'trojan' || protocol === 'hysteria2' || protocol === 'http' || protocol === 'socks') && (
          <Form.Item name="password" label="Password">
            <Input placeholder="ปล่อยว่าง = สุ่มให้" />
          </Form.Item>
        )}
        {protocol === 'shadowsocks' && (
          <Form.Item name="key" label="Shared key (base64)">
            <Input placeholder="ปล่อยว่าง = สุ่มให้ตาม method" />
          </Form.Item>
        )}
        {protocol === 'wireguard' && (
          <Form.Item name="key" label="Client public key" required>
            <Input placeholder="base64 จาก wg pubkey" />
          </Form.Item>
        )}

        <Space style={{ justifyContent: 'flex-end', width: '100%', marginTop: 8 }}>
          <Button onClick={onClose}>ยกเลิก</Button>
          <Button type="primary" htmlType="submit" loading={busy}>
            {editing ? 'บันทึก' : 'สร้าง'}
          </Button>
        </Space>
      </Form>
    </Modal>
  )
}

// Tiny typography label used inside forms — kept inline so we don't
// pull antd's Typography section headers globally.
function Typography({ children }: { children: React.ReactNode }) {
  return (
    <div style={{
      fontSize: 12, fontWeight: 600, textTransform: 'uppercase',
      opacity: 0.65, letterSpacing: '0.05em',
      borderBottom: '1px solid var(--border)', paddingBottom: 4, marginBottom: 8,
    }}>{children}</div>
  )
}
