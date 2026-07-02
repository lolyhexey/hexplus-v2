import { useMemo, useState } from 'react'
import {
  Alert, Button, Form, Input, InputNumber, Modal, Space, Tabs, message,
} from 'antd'
import { API, type Inbound } from '@/lib/api'

// ClientBulkAddModal — 3x-ui's bulk-create modal. Two modes:
//   pattern:   base name + digits → {base}1..{base}N
//   list:      pasted lines, one email per line
// Everything else (quota, expire, ip limit) applies to the whole batch.

export default function ClientBulkAddModal({
  open, inbound, onClose, onSaved,
}: {
  open: boolean
  inbound: Inbound | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [mode, setMode] = useState<'pattern' | 'list'>('pattern')
  const [busy, setBusy] = useState(false)
  const [messageApi, contextHolder] = message.useMessage()

  const emails = useMemo(() => (values: any): string[] => {
    if (mode === 'list') {
      return (values.list ?? '')
        .split('\n')
        .map((s: string) => s.trim())
        .filter(Boolean)
    }
    const base = values.base ?? 'user'
    const start = values.start ?? 1
    const count = values.count ?? 10
    const out: string[] = []
    for (let i = 0; i < count; i++) out.push(`${base}${start + i}`)
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode])

  async function submit(values: any) {
    if (!inbound) return
    const list = emails(values)
    if (list.length === 0) { messageApi.error('No emails'); return }
    setBusy(true)
    let ok = 0, fail = 0
    for (const email of list) {
      try {
        await API.clients.create(inbound.id, {
          email,
          quota_bytes: (values.quota_gb || 0) * 1024 ** 3,
          expires_at: values.expire_days > 0
            ? Math.floor(Date.now() / 1000) + values.expire_days * 86400 : 0,
          ip_limit: values.ip_limit || 0,
          enabled: true,
        })
        ok++
      } catch { fail++ }
    }
    setBusy(false)
    if (ok > 0) messageApi.success(`created ${ok}`)
    if (fail > 0) messageApi.error(`failed ${fail}`)
    form.resetFields()
    onSaved()
  }

  return (
    <Modal open={open} onCancel={onClose} title="Bulk add clients" footer={null}
           width={560} destroyOnHidden>
      {contextHolder}
      <Alert type="info" showIcon style={{ marginBottom: 12 }}
             message="สร้าง client หลายตัวพร้อมกัน"
             description={`Inbound: ${inbound?.tag ?? '—'} (${inbound?.protocol ?? '—'})`} />
      <Form form={form} layout="vertical" onFinish={submit}
            initialValues={{ base: 'user', start: 1, count: 10, quota_gb: 0, expire_days: 30, ip_limit: 0 }}>
        <Tabs
          activeKey={mode}
          onChange={(k) => setMode(k as 'pattern' | 'list')}
          items={[
            {
              key: 'pattern', label: 'Pattern',
              children: (
                <Space size="middle" style={{ display: 'flex' }}>
                  <Form.Item name="base" label="Base name" style={{ flex: 1 }}>
                    <Input />
                  </Form.Item>
                  <Form.Item name="start" label="Start #" style={{ flex: 1 }}>
                    <InputNumber min={0} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item name="count" label="Count" style={{ flex: 1 }}>
                    <InputNumber min={1} max={1000} style={{ width: '100%' }} />
                  </Form.Item>
                </Space>
              ),
            },
            {
              key: 'list', label: 'List',
              children: (
                <Form.Item name="list" label="Emails (one per line)">
                  <Input.TextArea rows={6}
                    placeholder={'alice\nbob\ncarol'}
                    style={{ fontFamily: 'monospace', fontSize: 12 }} />
                </Form.Item>
              ),
            },
          ]}
        />

        <Space size="middle" style={{ display: 'flex' }}>
          <Form.Item name="quota_gb" label="Quota (GB, 0 = ∞)" style={{ flex: 1 }}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="expire_days" label="Expire (days, 0 = ∞)" style={{ flex: 1 }}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="ip_limit" label="IP limit (0 = ∞)" style={{ flex: 1 }}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Space>

        <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
          <Button onClick={onClose}>ยกเลิก</Button>
          <Button type="primary" htmlType="submit" loading={busy}>Create</Button>
        </Space>
      </Form>
    </Modal>
  )
}
