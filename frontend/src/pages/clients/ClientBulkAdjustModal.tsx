import { useState } from 'react'
import {
  Alert, Button, Form, InputNumber, Modal, Radio, Space, Switch, Tag, message,
} from 'antd'
import { API, type Client } from '@/lib/api'

// ClientBulkAdjustModal — apply the same operation (extend expiry,
// change quota, toggle enable, reset traffic, delete) to a selected
// set of clients. Mirrors 3x-ui's "Bulk adjust" flow.

type Op = 'extend' | 'quota' | 'toggle' | 'reset' | 'delete'

export default function ClientBulkAdjustModal({
  open, selected, onClose, onSaved,
}: {
  open: boolean
  selected: Client[]
  onClose: () => void
  onSaved: () => void
}) {
  const [op, setOp] = useState<Op>('extend')
  const [days, setDays] = useState(30)
  const [quotaGB, setQuotaGB] = useState(0)
  const [enabled, setEnabled] = useState(true)
  const [busy, setBusy] = useState(false)
  const [messageApi, contextHolder] = message.useMessage()

  async function apply() {
    if (selected.length === 0) return
    setBusy(true)
    let ok = 0, fail = 0
    for (const c of selected) {
      try {
        if (op === 'extend') {
          await API.clients.extend(c.id, days)
        } else if (op === 'quota') {
          await API.clients.update(c.id, {
            email: c.email,
            quota_bytes: quotaGB * 1024 ** 3,
            expires_at: c.expires_at,
            ip_limit: c.ip_limit,
            enabled: c.enabled,
          } as any)
        } else if (op === 'toggle') {
          if (c.enabled !== enabled) await API.clients.toggle(c.id)
        } else if (op === 'reset') {
          await API.clients.reset(c.id)
        } else if (op === 'delete') {
          await API.clients.remove(c.id)
        }
        ok++
      } catch { fail++ }
    }
    setBusy(false)
    if (ok > 0) messageApi.success(`updated ${ok}`)
    if (fail > 0) messageApi.error(`failed ${fail}`)
    onSaved()
  }

  return (
    <Modal open={open} onCancel={onClose} title="Bulk adjust clients"
           footer={null} width={520} destroyOnHidden>
      {contextHolder}
      <Alert type="info" showIcon style={{ marginBottom: 12 }}
             message="Apply the same operation to every selected client"
             description={
               <div>
                 Selected: <Tag color="blue">{selected.length}</Tag>{' '}
                 {selected.slice(0, 3).map((c) => c.email).join(', ')}
                 {selected.length > 3 ? `, +${selected.length - 3} more` : ''}
               </div>
             } />

      <Form layout="vertical">
        <Form.Item label="Operation">
          <Radio.Group value={op} onChange={(e) => setOp(e.target.value)}
                       optionType="button" buttonStyle="solid">
            <Radio.Button value="extend">Extend expiry</Radio.Button>
            <Radio.Button value="quota">Set quota</Radio.Button>
            <Radio.Button value="toggle">Enable/Disable</Radio.Button>
            <Radio.Button value="reset">Reset traffic</Radio.Button>
            <Radio.Button value="delete">Delete</Radio.Button>
          </Radio.Group>
        </Form.Item>

        {op === 'extend' && (
          <Form.Item label="Days">
            <InputNumber min={-3650} max={3650} value={days}
                          onChange={(v) => setDays(Number(v) || 0)} style={{ width: 200 }} />
            <span style={{ marginLeft: 8, opacity: 0.6 }}>
              (negative to shorten expiry)
            </span>
          </Form.Item>
        )}
        {op === 'quota' && (
          <Form.Item label="Quota (GB, 0 = ∞)">
            <InputNumber min={0} value={quotaGB} onChange={(v) => setQuotaGB(Number(v) || 0)}
                          style={{ width: 200 }} />
          </Form.Item>
        )}
        {op === 'toggle' && (
          <Form.Item label="Enabled">
            <Switch checked={enabled} onChange={setEnabled} />
          </Form.Item>
        )}
        {op === 'reset' && (
          <Alert type="warning" showIcon
                 message="Set used_bytes = 0 for every selected client."
                 style={{ marginBottom: 12 }} />
        )}
        {op === 'delete' && (
          <Alert type="error" showIcon
                 message="ลบ client ถาวร — action นี้ไม่สามารถย้อนได้"
                 style={{ marginBottom: 12 }} />
        )}

        <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
          <Button onClick={onClose}>ยกเลิก</Button>
          <Button type={op === 'delete' ? 'primary' : 'primary'}
                  danger={op === 'delete'}
                  loading={busy}
                  onClick={apply}>
            Apply to {selected.length} client{selected.length === 1 ? '' : 's'}
          </Button>
        </Space>
      </Form>
    </Modal>
  )
}
