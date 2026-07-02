import { useState } from 'react'
import {
  Alert, Badge, Button, Input, Modal, Space, Table, Tag, message,
  type TableColumnsType,
} from 'antd'
import { CheckCircleOutlined, CloseCircleOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { API, type RealityScanResult } from '@/lib/api'

// RealityScannerModal — port of 3x-ui's Reality target scanner. Lets
// the operator paste a list of candidate hosts and pick the fastest
// TLS 1.3 + h2 responder as the decoy target for a Reality inbound.

const DEFAULT_TARGETS = [
  'www.microsoft.com',
  'www.apple.com',
  'www.samsung.com',
  'www.nvidia.com',
  'www.cloudflare.com',
  'aws.amazon.com',
  'gateway.icloud.com',
  'www.tesla.com',
  'www.lovelive-anime.jp',
  'time.cloudflare.com',
]

export default function RealityScannerModal({
  open, onClose, onPick,
}: {
  open: boolean
  onClose: () => void
  onPick: (host: string) => void
}) {
  const [messageApi, contextHolder] = message.useMessage()
  const [targets, setTargets] = useState(DEFAULT_TARGETS.join('\n'))
  const [busy, setBusy] = useState(false)
  const [results, setResults] = useState<RealityScanResult[]>([])

  async function scan() {
    setBusy(true); setResults([])
    try {
      const list = targets.split('\n').map((s) => s.trim()).filter(Boolean)
      const r = await API.reality.scan(list)
      setResults(r.results)
      const ok = r.results.filter((x) => x.ok).length
      messageApi.success(`Scanned ${r.results.length} — ${ok} viable`)
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusy(false) }
  }

  const columns: TableColumnsType<RealityScanResult> = [
    {
      title: 'Host', dataIndex: 'host',
      render: (v: string) => <code style={{ fontSize: 12 }}>{v}</code>,
    },
    {
      title: 'Status', width: 90, align: 'center',
      render: (_: unknown, r) => r.ok
        ? <Badge status="success" text="OK" />
        : <Badge status="error" text="fail" />,
    },
    {
      title: 'TLS', dataIndex: 'tls_version', width: 100,
      render: (v?: string) => v ? <Tag color={v === 'TLS 1.3' ? 'green' : 'red'}>{v}</Tag> : '—',
    },
    {
      title: 'ALPN', dataIndex: 'alpn', width: 90,
      render: (v?: string) => v ? <Tag color={v === 'h2' ? 'green' : 'orange'}>{v}</Tag> : '—',
    },
    {
      title: 'RTT', dataIndex: 'rtt_ms', width: 100,
      render: (v?: number) => v ? `${v} ms` : '—',
      sorter: (a, b) => (a.rtt_ms ?? 999999) - (b.rtt_ms ?? 999999),
    },
    {
      title: 'Reason', dataIndex: 'reason', ellipsis: true,
      render: (v?: string) => v ? <span style={{ opacity: 0.7, fontSize: 12 }}>{v}</span> : '',
    },
    {
      title: '', width: 90, align: 'right',
      render: (_: unknown, r) => r.ok
        ? <Button size="small" type="primary" icon={<CheckCircleOutlined />}
                  onClick={() => { onPick(r.host); onClose() }}>
            Use
          </Button>
        : null,
    },
  ]

  return (
    <Modal open={open} onCancel={onClose} title="Reality target scanner"
           footer={null} width={780} destroyOnHidden>
      {contextHolder}
      <Alert type="info" showIcon style={{ marginBottom: 12 }}
             message="A viable decoy is a site that terminates TLS 1.3 + h2 on port 443."
             description="Xray relays the outer TLS handshake to this host — pick a fast, always-online, non-blocked one." />

      <Space direction="vertical" style={{ width: '100%' }}>
        <Input.TextArea
          rows={4}
          value={targets}
          onChange={(e) => setTargets(e.target.value)}
          style={{ fontFamily: 'monospace', fontSize: 12 }}
          placeholder="hostname per line"
        />

        <Space>
          <Button type="primary" loading={busy} icon={<ThunderboltOutlined />} onClick={scan}>
            Scan {targets.split('\n').filter((l) => l.trim()).length} targets
          </Button>
          <Button onClick={() => setTargets(DEFAULT_TARGETS.join('\n'))} icon={<CloseCircleOutlined />}>
            Reset list
          </Button>
        </Space>

        <Table<RealityScanResult>
          rowKey="host"
          size="small"
          columns={columns}
          dataSource={results}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: busy ? 'Scanning…' : 'No scan yet' }}
        />
      </Space>
    </Modal>
  )
}
