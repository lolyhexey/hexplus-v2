import { useEffect, useState } from 'react'
import {
  Alert, Button, Form, Input, InputNumber, Radio, Select, Space, Switch, Tabs, Tag,
} from 'antd'
import type { Outbound } from '@/lib/api'

// OutboundForm — custom outbound editor. Backend accepts any JSON in
// `settings` and `stream`, so we just render a small friendly form
// per-protocol and hand the assembled object off to the parent's
// submit handler. Advanced JSON mode lets power-users bypass this.

export interface OutboundFormValue {
  tag: string
  protocol: string
  remark: string
  settings: any
  stream: any
  enabled: boolean
}

const PROTOCOLS = [
  { value: 'freedom',     label: 'Freedom (direct)' },
  { value: 'blackhole',   label: 'Blackhole (drop)' },
  { value: 'vless',       label: 'VLESS' },
  { value: 'vmess',       label: 'VMess' },
  { value: 'trojan',      label: 'Trojan' },
  { value: 'shadowsocks', label: 'Shadowsocks' },
  { value: 'wireguard',   label: 'WireGuard' },
  { value: 'http',        label: 'HTTP' },
  { value: 'socks',       label: 'SOCKS' },
]

export default function OutboundForm({
  initial, onCancel, onSubmit,
}: {
  initial?: Outbound
  onCancel: () => void
  onSubmit: (v: OutboundFormValue) => Promise<void>
}) {
  const [form] = Form.useForm()
  const initialSettings = asObj(initial?.settings)
  const initialStream = asObj(initial?.stream)

  const [protocol, setProtocol] = useState<string>(initial?.protocol ?? 'freedom')
  const [advancedOn, setAdvancedOn] = useState(false)
  const [advSettings, setAdvSettings] = useState(JSON.stringify(initialSettings, null, 2))
  const [advStream, setAdvStream] = useState(JSON.stringify(initialStream, null, 2))
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    form.resetFields()
    form.setFieldsValue({
      tag: initial?.tag ?? '',
      remark: initial?.remark ?? '',
      enabled: initial?.enabled ?? true,
      // VLESS/VMess/Trojan target
      target: { address: initialSettings.address ?? '',
                port: initialSettings.port ?? 443,
                uuid: initialSettings.id ?? '',
                password: initialSettings.password ?? '' },
      ss: { method: initialSettings.method ?? '2022-blake3-aes-256-gcm',
            password: initialSettings.password ?? '',
            address: initialSettings.address ?? '',
            port: initialSettings.port ?? 8388 },
      wg: { secretKey: initialSettings.secretKey ?? '',
            address: initialSettings.address ?? [],
            peer: (initialSettings.peers ?? [{}])[0] ?? {} },
      http: { user: (initialSettings.users ?? [{}])[0]?.user ?? '',
              pass: (initialSettings.users ?? [{}])[0]?.pass ?? '',
              address: initialSettings.address ?? '',
              port: initialSettings.port ?? 8080 },
    })
    setProtocol(initial?.protocol ?? 'freedom')
    setAdvancedOn(false)
    setAdvSettings(JSON.stringify(initialSettings, null, 2))
    setAdvStream(JSON.stringify(initialStream, null, 2))
    setErr(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initial?.id])

  async function finish(values: any) {
    setBusy(true); setErr(null)
    try {
      let settings: any = {}
      if (advancedOn) {
        try { settings = JSON.parse(advSettings || '{}') }
        catch { setErr('Settings JSON invalid'); setBusy(false); return }
      } else {
        settings = buildSettings(protocol, values)
      }
      let stream: any = {}
      if (advancedOn) {
        try { stream = JSON.parse(advStream || '{}') }
        catch { setErr('Stream JSON invalid'); setBusy(false); return }
      }
      await onSubmit({
        tag: values.tag, protocol, remark: values.remark ?? '',
        settings, stream, enabled: values.enabled,
      })
    } catch (e) { setErr(String(e)) }
    finally { setBusy(false) }
  }

  const basicsTab = (
    <>
      <Form.Item name="tag" label="Tag" required
                 rules={[{ required: true, message: 'ต้องกรอก tag' }]}>
        <Input placeholder="my-outbound" disabled={!!initial} />
      </Form.Item>
      <Form.Item label="Protocol" required>
        <Radio.Group value={protocol} onChange={(e) => setProtocol(e.target.value)}
                     optionType="button" buttonStyle="solid">
          {PROTOCOLS.map((p) => (
            <Radio.Button key={p.value} value={p.value}>{p.label}</Radio.Button>
          ))}
        </Radio.Group>
      </Form.Item>
      <Form.Item name="remark" label="Remark">
        <Input placeholder="Cloudflare WARP / Home lab / …" />
      </Form.Item>
      <Form.Item name="enabled" label="Enabled" valuePropName="checked">
        <Switch />
      </Form.Item>

      {protocol === 'freedom' && (
        <Alert type="info" showIcon
               message="Freedom is xray's direct outbound — traffic egresses via the server's own IP." />
      )}
      {protocol === 'blackhole' && (
        <Alert type="warning" showIcon
               message="Blackhole silently drops matched traffic." />
      )}

      {(protocol === 'vless' || protocol === 'vmess' || protocol === 'trojan') && (
        <>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name={['target', 'address']} label="Server address" style={{ flex: 2 }}
                       rules={[{ required: true }]}>
              <Input placeholder="upstream.example.com" />
            </Form.Item>
            <Form.Item name={['target', 'port']} label="Port" style={{ flex: 1 }}
                       rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          {(protocol === 'vless' || protocol === 'vmess') && (
            <Form.Item name={['target', 'uuid']} label="UUID" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
          )}
          {protocol === 'trojan' && (
            <Form.Item name={['target', 'password']} label="Password" rules={[{ required: true }]}>
              <Input.Password />
            </Form.Item>
          )}
        </>
      )}

      {protocol === 'shadowsocks' && (
        <>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name={['ss', 'address']} label="Server" style={{ flex: 2 }}
                       rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name={['ss', 'port']} label="Port" style={{ flex: 1 }}
                       rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name={['ss', 'method']} label="Method">
            <Select options={[
              '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305',
              'aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305',
            ].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name={['ss', 'password']} label="Password" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
        </>
      )}

      {protocol === 'wireguard' && (
        <>
          <Form.Item name={['wg', 'secretKey']} label="Local secret key" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name={['wg', 'peer', 'publicKey']} label="Peer public key" style={{ flex: 2 }}
                       rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name={['wg', 'peer', 'endpoint']} label="Endpoint host:port" style={{ flex: 2 }}
                       rules={[{ required: true }]}>
              <Input placeholder="peer.example.com:51820" />
            </Form.Item>
          </Space>
        </>
      )}

      {(protocol === 'http' || protocol === 'socks') && (
        <>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name={['http', 'address']} label="Server" style={{ flex: 2 }}
                       rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name={['http', 'port']} label="Port" style={{ flex: 1 }}
                       rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name={['http', 'user']} label="Username (optional)" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
            <Form.Item name={['http', 'pass']} label="Password (optional)" style={{ flex: 1 }}>
              <Input.Password />
            </Form.Item>
          </Space>
        </>
      )}
    </>
  )

  const advancedTab = (
    <>
      <Alert type="warning" showIcon style={{ marginBottom: 12 }}
             message="Advanced mode replaces the Basics tab on save." />
      <Form.Item label="Enable advanced mode">
        <Switch checked={advancedOn} onChange={setAdvancedOn} />
      </Form.Item>
      <Form.Item label="settings JSON">
        <Input.TextArea rows={9} value={advSettings}
                        onChange={(e) => setAdvSettings(e.target.value)}
                        style={{ fontFamily: 'monospace', fontSize: 12 }} />
      </Form.Item>
      <Form.Item label="stream JSON">
        <Input.TextArea rows={9} value={advStream}
                        onChange={(e) => setAdvStream(e.target.value)}
                        style={{ fontFamily: 'monospace', fontSize: 12 }} />
      </Form.Item>
    </>
  )

  return (
    <Form form={form} layout="vertical" onFinish={finish} style={{ paddingTop: 8 }}>
      <Tabs items={[
        { key: 'basics',   label: 'Basics',   children: basicsTab, disabled: advancedOn },
        { key: 'advanced', label: <Space>Advanced {advancedOn && <Tag color="orange">active</Tag>}</Space>,
          children: advancedTab },
      ]} />
      {err && <Alert type="error" showIcon message={err} style={{ marginBottom: 12 }} />}
      <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
        <Button onClick={onCancel}>ยกเลิก</Button>
        <Button type="primary" htmlType="submit" loading={busy}>
          {initial ? 'บันทึกการแก้ไข' : 'สร้าง'}
        </Button>
      </Space>
    </Form>
  )
}

// buildSettings — shape the friendly form values into the JSON blob
// each xray protocol expects in outbound.settings.
function buildSettings(protocol: string, v: any): any {
  switch (protocol) {
    case 'freedom':   return {}
    case 'blackhole': return {}
    case 'vless':
    case 'vmess':
      return {
        vnext: [{
          address: v.target.address,
          port: v.target.port,
          users: [{ id: v.target.uuid, encryption: 'none' }],
        }],
      }
    case 'trojan':
      return {
        servers: [{
          address: v.target.address,
          port: v.target.port,
          password: v.target.password,
        }],
      }
    case 'shadowsocks':
      return {
        servers: [{
          address: v.ss.address,
          port: v.ss.port,
          method: v.ss.method,
          password: v.ss.password,
        }],
      }
    case 'wireguard':
      return {
        secretKey: v.wg.secretKey,
        peers: [v.wg.peer],
      }
    case 'http':
    case 'socks':
      return {
        servers: [{
          address: v.http.address,
          port: v.http.port,
          users: v.http.user ? [{ user: v.http.user, pass: v.http.pass }] : [],
        }],
      }
  }
  return {}
}

function asObj(v: unknown): any {
  if (!v) return {}
  if (typeof v === 'string') { try { return JSON.parse(v) } catch { return {} } }
  return v
}
