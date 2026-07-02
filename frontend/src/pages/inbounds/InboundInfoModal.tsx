import { useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Descriptions, Empty, Modal, Space, Table, Tabs, Tag, Tooltip, message,
} from 'antd'
import { CopyOutlined, LinkOutlined, QrcodeOutlined } from '@ant-design/icons'
import { API, type Client, type Inbound } from '@/lib/api'

// InboundInfoModal — the "info" popup that 3x-ui shows when the
// operator hits the Inbound row's Info action. Shows:
//   • full inbound descriptor (protocol / port / listen / stream)
//   • per-client rows with share link + QR link + copy button
//   • sub link (v2ray / clash / sing-box) for the FIRST client so the
//     operator can hand a single URL to the end-user app.

interface Props {
  open: boolean
  inbound: Inbound | null
  onClose: () => void
}

export default function InboundInfoModal({ open, inbound, onClose }: Props) {
  const [clients, setClients] = useState<Client[]>([])
  const [loading, setLoading] = useState(false)
  const [messageApi, contextHolder] = message.useMessage()
  const [qrClient, setQrClient] = useState<Client | null>(null)
  const [qrLink, setQrLink] = useState('')

  useEffect(() => {
    if (!open || !inbound) return
    let cancelled = false
    setLoading(true)
    API.clients.list(inbound.id)
      .then((cs) => { if (!cancelled) setClients(cs ?? []) })
      .catch((e) => { if (!cancelled) messageApi.error(String(e)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, inbound, messageApi])

  const stream = useMemo(() => coerce(inbound?.stream), [inbound])
  const settings = useMemo(() => coerce(inbound?.settings), [inbound])

  const firstClient = clients[0]
  const subBase = useMemo(() => {
    if (!firstClient?.sub_token) return null
    // /sub/{token} lives outside the URL prefix so the URL is
    // origin-relative, not prefixed.
    const origin = window.location.origin
    return `${origin}/sub/${firstClient.sub_token}`
  }, [firstClient])

  async function openQR(c: Client) {
    try {
      const r = await API.clients.link(c.id)
      setQrLink(r.link)
      setQrClient(c)
    } catch (e) { messageApi.error(String(e)) }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={inbound ? `Inbound — ${inbound.tag}` : ''}
      footer={null}
      width={760}
      destroyOnHidden
    >
      {contextHolder}
      {!inbound ? null : (
        <Tabs
          items={[
            {
              key: 'summary', label: 'Summary',
              children: (
                <Descriptions column={2} bordered size="middle">
                  <Descriptions.Item label="Tag">{inbound.tag}</Descriptions.Item>
                  <Descriptions.Item label="Protocol">
                    <Tag color="blue">{inbound.protocol}</Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="Listen">{inbound.listen}</Descriptions.Item>
                  <Descriptions.Item label="Port">{inbound.port}</Descriptions.Item>
                  <Descriptions.Item label="Network">{stream.network || 'tcp'}</Descriptions.Item>
                  <Descriptions.Item label="Security">
                    <Tag color={stream.security === 'reality' ? 'purple' :
                                stream.security === 'tls' ? 'green' : 'default'}>
                      {stream.security || 'none'}
                    </Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="Sniffing">
                    {inbound.sniffing ? <Tag color="success">on</Tag> : <Tag>off</Tag>}
                  </Descriptions.Item>
                  <Descriptions.Item label="Enabled">
                    {inbound.enabled ? <Tag color="success">on</Tag> : <Tag>off</Tag>}
                  </Descriptions.Item>
                  {inbound.remark && (
                    <Descriptions.Item label="Remark" span={2}>{inbound.remark}</Descriptions.Item>
                  )}
                </Descriptions>
              ),
            },
            {
              key: 'clients', label: `Clients (${clients.length})`,
              children: (
                <Table<Client>
                  rowKey="id"
                  size="small"
                  loading={loading}
                  dataSource={clients}
                  pagination={{ pageSize: 10, hideOnSinglePage: true }}
                  columns={[
                    { title: 'Email', dataIndex: 'email' },
                    {
                      title: 'Enabled', dataIndex: 'enabled', width: 90,
                      render: (v: boolean) => v ? <Tag color="success">on</Tag> : <Tag>off</Tag>,
                    },
                    {
                      title: '', width: 130, align: 'right',
                      render: (_: unknown, row) => (
                        <Space>
                          <Tooltip title="Copy share link">
                            <Button size="small" icon={<CopyOutlined />}
                                    onClick={async () => {
                                      try {
                                        const r = await API.clients.link(row.id)
                                        await navigator.clipboard.writeText(r.link)
                                        messageApi.success('copied')
                                      } catch (e) { messageApi.error(String(e)) }
                                    }} />
                          </Tooltip>
                          <Tooltip title="QR / view">
                            <Button size="small" icon={<QrcodeOutlined />}
                                    onClick={() => openQR(row)} />
                          </Tooltip>
                        </Space>
                      ),
                    },
                  ]}
                />
              ),
            },
            {
              key: 'sub', label: 'Subscription',
              children: subBase ? (
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Alert type="info" showIcon
                    message="Subscription URL — first client only"
                    description={`ให้ URL นี้กับผู้ใช้ไปใส่ใน app ของเขา (${firstClient.email})`} />
                  {[
                    { label: 'v2ray (base64)',        url: subBase },
                    { label: 'plain (raw URIs)',      url: `${subBase}?type=plain` },
                    { label: 'Clash / Clash Meta',    url: `${subBase}?type=clash` },
                    { label: 'Sing-box',              url: `${subBase}?type=sing-box` },
                  ].map((r) => (
                    <div key={r.label} style={{
                      display: 'flex', alignItems: 'center', gap: 8,
                      padding: '8px 12px', border: '1px solid rgba(255,255,255,.08)',
                      borderRadius: 6,
                    }}>
                      <div style={{ minWidth: 160, fontWeight: 500 }}>{r.label}</div>
                      <code style={{ flex: 1, fontSize: 12, opacity: 0.75, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {r.url}
                      </code>
                      <Button size="small" icon={<CopyOutlined />}
                              onClick={() => { navigator.clipboard.writeText(r.url); messageApi.success('copied') }}>
                        Copy
                      </Button>
                      <Button size="small" icon={<LinkOutlined />}
                              onClick={() => window.open(r.url, '_blank')}>
                        Open
                      </Button>
                    </div>
                  ))}
                </Space>
              ) : (
                <Empty description="Subscription ต้องมี client อย่างน้อย 1 ตัวถึงจะสร้าง URL ได้" />
              ),
            },
            {
              key: 'stream', label: 'Stream JSON',
              children: (
                <>
                  <div style={{ marginBottom: 6, opacity: 0.7 }}>
                    Stream settings (transport + security)
                  </div>
                  <pre style={{
                    background: 'rgba(0,0,0,.15)', padding: 12, borderRadius: 6,
                    fontSize: 12, overflowX: 'auto',
                  }}>{JSON.stringify(stream, null, 2)}</pre>
                  <div style={{ margin: '12px 0 6px', opacity: 0.7 }}>
                    Protocol-specific settings
                  </div>
                  <pre style={{
                    background: 'rgba(0,0,0,.15)', padding: 12, borderRadius: 6,
                    fontSize: 12, overflowX: 'auto',
                  }}>{JSON.stringify(settings, null, 2)}</pre>
                </>
              ),
            },
          ]}
        />
      )}

      <Modal
        open={!!qrClient}
        onCancel={() => setQrClient(null)}
        title={qrClient ? `Share — ${qrClient.email}` : ''}
        footer={null}
        width={420}
        destroyOnHidden
      >
        <div style={{ textAlign: 'center' }}>
          {qrClient && (
            <img src={API.clients.qrURL(qrClient.id)} alt="QR"
                 style={{
                   border: '1px solid rgba(255,255,255,.12)', borderRadius: 6, padding: 8,
                   background: '#fff', maxWidth: 320, width: '100%',
                 }} />
          )}
          <textarea readOnly value={qrLink}
                    style={{
                      width: '100%', height: 72, marginTop: 12,
                      fontFamily: 'monospace', fontSize: 12, padding: 8,
                    }} />
          <Button icon={<CopyOutlined />} block style={{ marginTop: 8 }}
                  onClick={() => { navigator.clipboard.writeText(qrLink); messageApi.success('copied') }}>
            Copy link
          </Button>
        </div>
      </Modal>
    </Modal>
  )
}

function coerce(v: unknown): any {
  if (!v) return {}
  if (typeof v === 'string') { try { return JSON.parse(v) } catch { return {} } }
  return v
}
