import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Card, Col, ConfigProvider, Dropdown, Empty, Form, Input, Layout, Modal,
  Row, Space, Statistic, Table, Tag, Tabs, message, theme as antdTheme,
  type MenuProps, type TableColumnsType,
} from 'antd'
import {
  DeleteOutlined, LockOutlined, MoreOutlined, PlusOutlined,
  ReloadOutlined, SafetyCertificateOutlined, UploadOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Cert } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'

// CertsPage — TLS certificate management. Two acquisition paths:
//   • Let's Encrypt via HTTP-01 (needs port 80 free + DNS pointing here)
//   • Manual upload (paste PEM cert + key)
// Renew is available for acme-source certs.

export default function CertsPage() {
  const { isDark } = useTheme()
  const [messageApi, contextHolder] = message.useMessage()
  const [rows, setRows] = useState<Cert[]>([])
  const [loading, setLoading] = useState(true)
  const [rowBusyId, setRowBusyId] = useState<number | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const refresh = useCallback(async () => {
    try { setLoading(true); setRows((await API.certs.list()) ?? []) }
    catch (e) { messageApi.error(String(e)) }
    finally { setLoading(false) }
  }, [messageApi])
  useEffect(() => { refresh() }, [refresh])

  const remove = useCallback(async (row: Cert) => {
    if (!confirm(`ลบ cert ของ "${row.domain}"?`)) return
    setRowBusyId(row.id)
    try { await API.certs.remove(row.id); messageApi.success('deleted'); await refresh() }
    catch (e) { messageApi.error(String(e)) }
    finally { setRowBusyId(null) }
  }, [messageApi, refresh])

  const renew = useCallback(async (row: Cert) => {
    setRowBusyId(row.id)
    try {
      messageApi.loading({ content: 'Renewing…', key: 'renew', duration: 0 })
      await API.certs.renew(row.id)
      messageApi.success({ content: 'Renewed', key: 'renew' })
      await refresh()
    } catch (e) { messageApi.error({ content: String(e), key: 'renew' }) }
    finally { setRowBusyId(null) }
  }, [messageApi, refresh])

  const totals = useMemo(() => {
    const now = Math.floor(Date.now() / 1000)
    return {
      total: rows.length,
      expiring: rows.filter((r) => r.not_after > 0 && r.not_after - now < 14 * 86400).length,
      expired: rows.filter((r) => r.not_after > 0 && r.not_after <= now).length,
    }
  }, [rows])

  const columns: TableColumnsType<Cert> = useMemo(() => [
    { title: '#', dataIndex: 'id', width: 64 },
    {
      title: 'Domain', dataIndex: 'domain',
      render: (v: string, r) => (
        <Space direction="vertical" size={0}>
          <span style={{ fontWeight: 500 }}>{v}</span>
          {r.remark && <span style={{ fontSize: 12, opacity: 0.6 }}>{r.remark}</span>}
        </Space>
      ),
    },
    {
      title: 'Source', dataIndex: 'source', width: 120,
      render: (v: string) => v === 'acme'
        ? <Tag color="green">Let's Encrypt</Tag>
        : <Tag>Manual</Tag>,
    },
    {
      title: 'Expires', dataIndex: 'not_after', width: 180,
      render: (v: number) => {
        if (!v) return <Tag>Unknown</Tag>
        const now = Math.floor(Date.now() / 1000)
        const daysLeft = Math.floor((v - now) / 86400)
        const dt = new Date(v * 1000).toLocaleDateString('th-TH')
        if (v <= now) return <Tag color="red">Expired {dt}</Tag>
        return <Tag color={daysLeft < 14 ? 'orange' : 'green'}>{dt} ({daysLeft}d)</Tag>
      },
    },
    {
      title: 'On-disk', dataIndex: 'cert_path', ellipsis: true,
      render: (v: string) => <code style={{ fontSize: 11 }}>{v}</code>,
    },
    {
      title: '', width: 60, fixed: 'right', align: 'center',
      render: (_: unknown, row) => {
        const menu: MenuProps['items'] = [
          ...(row.source === 'acme' ? [
            { key: 'renew', icon: <ReloadOutlined />, label: 'Renew (Let\'s Encrypt)' },
          ] : []),
          { type: 'divider' },
          { key: 'delete', icon: <DeleteOutlined />, label: 'Delete', danger: true },
        ] as MenuProps['items']
        return (
          <Dropdown trigger={['click']} placement="bottomRight" menu={{
            items: menu,
            onClick: ({ key }) => {
              if (key === 'renew') renew(row)
              if (key === 'delete') remove(row)
            }
          }}>
            <Button size="small" icon={<MoreOutlined />} loading={rowBusyId === row.id} />
          </Dropdown>
        )
      },
    },
  ], [remove, renew, rowBusyId])

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
              <Col xs={24} md={8}>
                <Card><Statistic title="Total certificates" value={totals.total}
                  prefix={<SafetyCertificateOutlined />} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Expiring in 14 days"
                  value={totals.expiring}
                  valueStyle={{ color: totals.expiring > 0 ? '#faad14' : undefined }} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Expired"
                  value={totals.expired}
                  valueStyle={{ color: totals.expired > 0 ? '#ff4d4f' : undefined }} /></Card>
              </Col>

              <Col span={24}>
                <Card
                  hoverable
                  title={
                    <Space>
                      <Button type="primary" icon={<PlusOutlined />}
                              onClick={() => setDialogOpen(true)}>
                        Add certificate
                      </Button>
                    </Space>
                  }
                >
                  {rows.length === 0 && !loading ? (
                    <Empty description={'ยังไม่มี cert — VLESS/Trojan+TLS ต้องมี cert ก่อน\n(Reality ไม่ต้อง)'} />
                  ) : (
                    <Table<Cert>
                      rowKey="id"
                      size="middle"
                      loading={loading}
                      columns={columns}
                      dataSource={rows}
                      pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
                      scroll={{ x: 900 }}
                    />
                  )}
                </Card>
              </Col>
            </Row>
          </Layout.Content>
        </Layout>
      </Layout>

      <CertModal
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        onSaved={() => { setDialogOpen(false); refresh() }}
        messageApi={messageApi}
      />
    </ConfigProvider>
  )
}

function CertModal({
  open, onClose, onSaved, messageApi,
}: {
  open: boolean
  onClose: () => void
  onSaved: () => void
  messageApi: any
}) {
  const [acmeForm] = Form.useForm()
  const [manualForm] = Form.useForm()
  const [busy, setBusy] = useState(false)

  async function acquireAcme(v: any) {
    setBusy(true)
    try {
      messageApi.loading({ content: 'Requesting Let\'s Encrypt cert…', key: 'acme', duration: 0 })
      await API.certs.acme({ domain: v.domain, contact_email: v.contact_email, remark: v.remark })
      messageApi.success({ content: 'Cert issued', key: 'acme' })
      acmeForm.resetFields()
      onSaved()
    } catch (e) { messageApi.error({ content: String(e), key: 'acme' }) }
    finally { setBusy(false) }
  }

  async function uploadManual(v: any) {
    setBusy(true)
    try {
      await API.certs.uploadManual({
        domain: v.domain, cert_pem: v.cert_pem, key_pem: v.key_pem, remark: v.remark,
      })
      messageApi.success('Cert uploaded')
      manualForm.resetFields()
      onSaved()
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusy(false) }
  }

  return (
    <Modal open={open} onCancel={onClose} title="Add certificate"
           footer={null} width={640} destroyOnHidden>
      <Tabs items={[
        {
          key: 'acme',
          label: <span><LockOutlined /> Let's Encrypt</span>,
          children: (
            <>
              <Alert type="info" showIcon style={{ marginBottom: 16 }}
                     message="HTTP-01 challenge"
                     description="Port 80 ต้องว่าง + Domain ต้องชี้มาที่ VPS นี้ก่อน" />
              <Form form={acmeForm} layout="vertical" onFinish={acquireAcme}>
                <Form.Item name="domain" label="Domain"
                            rules={[{ required: true, message: 'required' }]}>
                  <Input placeholder="example.com" />
                </Form.Item>
                <Form.Item name="contact_email" label="Contact email (optional)"
                            rules={[{ type: 'email', message: 'อีเมลไม่ถูกต้อง' }]}>
                  <Input placeholder="admin@example.com" />
                </Form.Item>
                <Form.Item name="remark" label="Remark">
                  <Input />
                </Form.Item>
                <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
                  <Button onClick={onClose}>ยกเลิก</Button>
                  <Button type="primary" htmlType="submit" loading={busy}>
                    Request certificate
                  </Button>
                </Space>
              </Form>
            </>
          ),
        },
        {
          key: 'manual',
          label: <span><UploadOutlined /> Manual upload</span>,
          children: (
            <>
              <Form form={manualForm} layout="vertical" onFinish={uploadManual}>
                <Form.Item name="domain" label="Domain"
                            rules={[{ required: true, message: 'required' }]}>
                  <Input placeholder="example.com" />
                </Form.Item>
                <Form.Item name="cert_pem" label="Certificate PEM (fullchain)"
                            rules={[{ required: true }]}>
                  <Input.TextArea rows={6}
                    placeholder="-----BEGIN CERTIFICATE-----"
                    style={{ fontFamily: 'monospace', fontSize: 11 }} />
                </Form.Item>
                <Form.Item name="key_pem" label="Private key PEM"
                            rules={[{ required: true }]}>
                  <Input.TextArea rows={6}
                    placeholder="-----BEGIN PRIVATE KEY-----"
                    style={{ fontFamily: 'monospace', fontSize: 11 }} />
                </Form.Item>
                <Form.Item name="remark" label="Remark">
                  <Input />
                </Form.Item>
                <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
                  <Button onClick={onClose}>ยกเลิก</Button>
                  <Button type="primary" htmlType="submit" loading={busy}>Upload</Button>
                </Space>
              </Form>
            </>
          ),
        },
      ]} />
    </Modal>
  )
}
