import { useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Form, Input, InputNumber, Radio, Select, Space, Switch, Tabs, Tag,
} from 'antd'
import {
  DeleteOutlined, PlusOutlined, RadarChartOutlined, ReloadOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import { API, type Inbound } from '@/lib/api'
import RealityScannerModal from './RealityScannerModal'

// InboundForm — Ant-Design port modelled after 3x-ui's InboundFormModal.
// Instead of nested antd Form.Item paths ['streamSettings','realitySettings',
// 'target'] we flatten into our own {settings, stream} shape that the
// backend already accepts. Field labels + control choices mirror 3x-ui
// so operators moving in feel at home.

export interface InboundFormValue {
  tag: string
  protocol: string
  listen: string
  port: number
  remark: string
  settings: any
  stream: any
  sniffing?: boolean
}

// ── constants ──────────────────────────────────────────────────────

const PROTOCOLS = [
  { value: 'vless',        label: 'VLESS' },
  { value: 'vmess',        label: 'VMess' },
  { value: 'trojan',       label: 'Trojan' },
  { value: 'shadowsocks',  label: 'Shadowsocks' },
  { value: 'hysteria2',    label: 'Hysteria2' },
  { value: 'wireguard',    label: 'WireGuard' },
  { value: 'http',         label: 'HTTP' },
  { value: 'socks',        label: 'SOCKS' },
  { value: 'dokodemo-door', label: 'Dokodemo-door' },
]

const NETWORKS = [
  { value: 'tcp',         label: 'TCP' },
  { value: 'ws',          label: 'WebSocket' },
  { value: 'grpc',        label: 'gRPC' },
  { value: 'httpupgrade', label: 'HTTPUpgrade' },
  { value: 'xhttp',       label: 'XHTTP' },
  { value: 'kcp',         label: 'mKCP' },
]

const SECURITY = [
  { value: 'none',    label: 'None' },
  { value: 'tls',     label: 'TLS' },
  { value: 'reality', label: 'Reality' },
]

const UTLS_FPS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'random', 'randomized']

const SS_METHODS = [
  '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305',
  'aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305',
]

// Common Reality decoy SNIs (matches 3x-ui default suggestions).
const REALITY_SNIS = [
  'www.microsoft.com', 'www.apple.com', 'www.samsung.com', 'www.nvidia.com',
  'www.google.com', 'www.cloudflare.com', 'aws.amazon.com', 'gateway.icloud.com',
]

// ── component ──────────────────────────────────────────────────────

export function InboundForm({
  initial, onCancel, onSubmit,
}: {
  initial?: Inbound
  onCancel: () => void
  onSubmit: (v: InboundFormValue) => Promise<void>
}) {
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [portInUse, setPortInUse] = useState<boolean | null>(null)
  const [scannerOpen, setScannerOpen] = useState(false)
  // Advanced-mode: when true the operator edits raw stream/settings
  // JSON directly and the friendly tabs stop dictating the payload.
  const [advancedOn, setAdvancedOn] = useState(false)
  const [advancedStream, setAdvancedStream] = useState('{}')
  const [advancedSettings, setAdvancedSettings] = useState('{}')
  const [fallbacks, setFallbacks] = useState<any[]>(
    (asObj(initial?.settings).fallbacks as any[]) ?? [],
  )

  const initialStream = asObj(initial?.stream)
  const initialSettings = asObj(initial?.settings)

  // Non-Form-tracked state (protocol / network / security switch which
  // fields are rendered) needs to be re-hydrated any time `initial`
  // changes. useState only reads the initializer once, so we ALSO
  // useEffect below to reset both this state AND the Form fields.
  const [protocol, setProtocol] = useState<string>(initial?.protocol ?? 'vless')
  const [network, setNetwork] = useState<string>(initialStream.network ?? 'tcp')
  const [security, setSecurity] = useState<string>(initialStream.security ?? 'reality')
  const [port, setPort] = useState<number>(initial?.port ?? suggestPort())

  const supportsSecurity = useMemo(
    () => ['vless', 'vmess', 'trojan'].includes(protocol),
    [protocol],
  )
  useEffect(() => {
    if (!supportsSecurity && security !== 'none') setSecurity('none')
    if (protocol !== 'vless' && security === 'reality') setSecurity('tls')
  }, [protocol, supportsSecurity, security])

  useEffect(() => {
    if (initial && initial.port === port) { setPortInUse(null); return }
    if (port <= 0 || port > 65535) { setPortInUse(null); return }
    const t = window.setTimeout(async () => {
      try {
        const r = await API.probe.port(port, 'tcp')
        setPortInUse(r.in_use)
      } catch { setPortInUse(null) }
    }, 400)
    return () => window.clearTimeout(t)
  }, [port, initial])

  const genShortId = () => {
    const b = crypto.getRandomValues(new Uint8Array(4))
    form.setFieldValue(['reality', 'shortId'],
      [...b].map((x) => x.toString(16).padStart(2, '0')).join(''))
  }
  async function genRealityKeys() {
    try {
      const kp = await crypto.subtle.generateKey({ name: 'X25519' }, true, ['deriveBits']) as CryptoKeyPair
      const rawPriv = await crypto.subtle.exportKey('raw', kp.privateKey)
      const rawPub = await crypto.subtle.exportKey('raw', kp.publicKey)
      form.setFieldValue(['reality', 'privateKey'], b64url(rawPriv))
      form.setFieldValue(['reality', 'publicKey'], b64url(rawPub))
    } catch {
      setErr('เบราว์เซอร์ไม่รองรับ X25519 — วางคีย์เอง')
    }
  }

  async function handleFinish(values: any) {
    setBusy(true); setErr(null)
    try {
      // Advanced mode short-circuits the friendly builders — we ship
      // whatever the operator wrote in the raw editors.
      if (advancedOn) {
        let s: any, st: any
        try { s = JSON.parse(advancedSettings || '{}') }
        catch { setErr('Settings JSON invalid'); setBusy(false); return }
        try { st = JSON.parse(advancedStream || '{}') }
        catch { setErr('Stream JSON invalid'); setBusy(false); return }
        await onSubmit({
          tag: values.tag, protocol,
          listen: values.listen ?? '0.0.0.0', port,
          remark: values.remark ?? '',
          settings: s, stream: st,
          sniffing: values.sniffing ?? true,
        })
        setBusy(false)
        return
      }
      const stream: any = { network }
      if (supportsSecurity && security !== 'none') stream.security = security
      if (network === 'ws' || network === 'httpupgrade' || network === 'xhttp') {
        stream.path = values.ws?.path ?? '/'
        if (values.ws?.host) stream.host = values.ws.host
      }
      if (network === 'grpc') stream.serviceName = values.grpc?.serviceName ?? 'grpc'
      if (security === 'tls') {
        if (values.tls?.sni) stream.sni = values.tls.sni
        if (values.tls?.fingerprint) stream.fingerprint = values.tls.fingerprint
        if (values.tls?.alpn) stream.alpn = values.tls.alpn
      }
      if (security === 'reality') {
        stream.realityDest = values.reality?.dest ?? 'www.microsoft.com:443'
        stream.realityServerNames = [values.reality?.sni ?? 'www.microsoft.com']
        stream.realityPrivateKey = values.reality?.privateKey ?? ''
        stream.realityShortIDs = values.reality?.shortId ? [values.reality.shortId] : []
        stream.fingerprint = values.reality?.fingerprint ?? 'chrome'
      }

      const settings: any = {}
      if (protocol === 'shadowsocks') settings.method = values.ss?.method ?? '2022-blake3-aes-256-gcm'
      if (protocol === 'wireguard') settings.secretKey = values.wg?.secretKey ?? ''
      if (protocol === 'dokodemo-door') {
        settings.address = values.doko?.address ?? '127.0.0.1'
        settings.port = values.doko?.port ?? 80
      }
      if (security === 'reality' && values.reality?.publicKey) {
        settings.realityPublicKey = values.reality.publicKey
      }
      // Attach fallbacks (VLESS/Trojan only) — xray ignores the field
      // on other protocols but we prune to keep the config clean.
      if ((protocol === 'vless' || protocol === 'trojan') && fallbacks.length > 0) {
        settings.fallbacks = fallbacks.filter((f) => f && (f.dest || f.name || f.path))
      }

      const payload: InboundFormValue = {
        tag: values.tag,
        protocol,
        listen: values.listen ?? '0.0.0.0',
        port,
        remark: values.remark ?? '',
        settings,
        stream,
        sniffing: values.sniffing ?? true,
      }
      if (portInUse && !confirm('พอร์ตนี้มี process อื่นถืออยู่ — บันทึกอยู่ดี?')) {
        setBusy(false); return
      }
      await onSubmit(payload)
    } catch (e) { setErr(String(e)) }
    finally { setBusy(false) }
  }

  const initialValues = useMemo(() => ({
    tag: initial?.tag ?? '',
    remark: initial?.remark ?? '',
    listen: initial?.listen ?? '0.0.0.0',
    sniffing: initial?.sniffing ?? true,
    ss:      { method: initialSettings.method ?? '2022-blake3-aes-256-gcm' },
    wg:      { secretKey: initialSettings.secretKey ?? '' },
    doko:    { address: initialSettings.address ?? '127.0.0.1', port: initialSettings.port ?? 80 },
    ws:      { path: initialStream.path ?? '/', host: initialStream.host ?? '' },
    grpc:    { serviceName: initialStream.serviceName ?? 'grpc' },
    tls:     {
      sni: initialStream.sni ?? '',
      fingerprint: initialStream.fingerprint ?? 'chrome',
      alpn: initialStream.alpn ?? [],
    },
    reality: {
      dest: initialStream.realityDest ?? 'www.microsoft.com:443',
      sni: (initialStream.realityServerNames ?? [])[0] ?? 'www.microsoft.com',
      privateKey: initialStream.realityPrivateKey ?? '',
      publicKey:  initialSettings.realityPublicKey ?? '',
      shortId:    (initialStream.realityShortIDs ?? [])[0] ?? '',
      fingerprint: initialStream.fingerprint ?? 'chrome',
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }), [initial?.id])

  // Re-hydrate every time we're editing a different row (or switching
  // between create/edit). Ant Form's `initialValues` is applied only
  // at mount; `resetFields()` + `setFieldsValue` is the officially
  // recommended pattern for programmatic updates.
  useEffect(() => {
    form.resetFields()
    form.setFieldsValue(initialValues)
    setProtocol(initial?.protocol ?? 'vless')
    setNetwork(initialStream.network ?? 'tcp')
    setSecurity(initialStream.security ?? (initial ? 'none' : 'reality'))
    setPort(initial?.port ?? suggestPort())
    setErr(null)
    setAdvancedOn(false)
    setAdvancedStream(JSON.stringify(initialStream, null, 2))
    setAdvancedSettings(JSON.stringify(initialSettings, null, 2))
    setFallbacks((initialSettings.fallbacks as any[]) ?? [])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initial?.id])

  const generalTab = (
    <>
      <Form.Item name="remark" label="Remark">
        <Input placeholder="ตัวหลัก SG" />
      </Form.Item>
      <Form.Item label="Protocol" required>
        <Radio.Group value={protocol} onChange={(e) => setProtocol(e.target.value)}
                     options={PROTOCOLS.map((p) => ({ value: p.value, label: p.label }))} />
      </Form.Item>
      <Form.Item name="tag" label="Tag" required
                 rules={[{ required: true, message: 'ต้องกรอก tag' }]}
                 tooltip="ชื่ออ้างอิงภายใน ต้องไม่ซ้ำ">
        <Input placeholder="my-vless" />
      </Form.Item>
      <Space size="middle" style={{ display: 'flex' }}>
        <Form.Item name="listen" label="Listen IP" style={{ flex: 1 }}>
          <Input />
        </Form.Item>
        <Form.Item label="Port" style={{ flex: 1 }} required
                   validateStatus={portInUse ? 'error' : undefined}
                   help={portInUse === true ? 'พอร์ตนี้มี process อื่นใช้อยู่' : portInUse === false ? '✓ พอร์ตว่าง' : undefined}>
          <InputNumber min={1} max={65535} style={{ width: '100%' }}
                       value={port} onChange={(v) => setPort(Number(v) || 0)} />
        </Form.Item>
      </Space>

      {protocol === 'shadowsocks' && (
        <Form.Item name={['ss', 'method']} label="Method">
          <Select options={SS_METHODS.map((v) => ({ value: v, label: v }))} />
        </Form.Item>
      )}
      {protocol === 'wireguard' && (
        <Form.Item name={['wg', 'secretKey']} label="Server secret key (base64)"
                   tooltip="สร้างด้วย `wg genkey`">
          <Input />
        </Form.Item>
      )}
      {protocol === 'dokodemo-door' && (
        <Space size="middle" style={{ display: 'flex' }}>
          <Form.Item name={['doko', 'address']} label="Upstream address" style={{ flex: 1 }}>
            <Input />
          </Form.Item>
          <Form.Item name={['doko', 'port']} label="Upstream port" style={{ flex: 1 }}>
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
        </Space>
      )}

      <Form.Item name="sniffing" label="Sniffing" valuePropName="checked"
                 tooltip="อ่าน SNI/hostname เพื่อ routing rules">
        <Switch />
      </Form.Item>
    </>
  )

  const transportTab = (
    <>
      <Form.Item label="Network">
        <Radio.Group value={network} onChange={(e) => setNetwork(e.target.value)}
                     options={NETWORKS.map((n) => ({ value: n.value, label: n.label }))}
                     optionType="button" buttonStyle="solid" />
      </Form.Item>
      {(network === 'ws' || network === 'httpupgrade' || network === 'xhttp') && (
        <>
          <Form.Item name={['ws', 'path']} label="Path"><Input /></Form.Item>
          <Form.Item name={['ws', 'host']} label="Host (optional)"><Input /></Form.Item>
        </>
      )}
      {network === 'grpc' && (
        <Form.Item name={['grpc', 'serviceName']} label="Service name"><Input /></Form.Item>
      )}
      {network === 'kcp' && (
        <Alert type="info" showIcon message="mKCP ใช้ default settings — advanced ตั้งใน raw config" />
      )}
    </>
  )

  const securityTab = (
    <>
      {!supportsSecurity && (
        <Alert type="info" showIcon message={`Protocol ${protocol} ไม่ support TLS/Reality`} />
      )}
      {supportsSecurity && (
        <>
          <Form.Item label="Security">
            <Radio.Group value={security} onChange={(e) => setSecurity(e.target.value)}
                         optionType="button" buttonStyle="solid">
              {SECURITY.map((s) => (
                <Radio.Button key={s.value} value={s.value}
                              disabled={s.value === 'reality' && protocol !== 'vless'}>
                  {s.label}
                </Radio.Button>
              ))}
            </Radio.Group>
          </Form.Item>

          {security === 'tls' && (
            <>
              <Form.Item name={['tls', 'sni']} label="Server name (SNI)">
                <Input placeholder="example.com" />
              </Form.Item>
              <Form.Item name={['tls', 'alpn']} label="ALPN">
                <Select mode="multiple" options={['h2', 'http/1.1'].map((v) => ({ value: v, label: v }))} />
              </Form.Item>
              <Form.Item name={['tls', 'fingerprint']} label="uTLS fingerprint">
                <Select options={UTLS_FPS.map((v) => ({ value: v, label: v }))} />
              </Form.Item>
            </>
          )}

          {security === 'reality' && (
            <>
              <Space size="middle" style={{ display: 'flex' }}>
                <Form.Item name={['reality', 'dest']} label="Dest" style={{ flex: 1 }}
                           tooltip="host:port ของ decoy server">
                  <Input placeholder="www.microsoft.com:443" />
                </Form.Item>
                <Form.Item name={['reality', 'sni']} label="Server name" style={{ flex: 1 }}>
                  <Select showSearch options={REALITY_SNIS.map((v) => ({ value: v, label: v }))}
                          allowClear placeholder="เลือกหรือพิมพ์เอง" />
                </Form.Item>
              </Space>

              <Space size="middle" style={{ display: 'flex' }}>
                <Form.Item name={['reality', 'privateKey']} label="Private key" style={{ flex: 1 }}>
                  <Input.Password />
                </Form.Item>
                <Form.Item name={['reality', 'publicKey']} label="Public key" style={{ flex: 1 }}
                           tooltip="ส่งให้ client ผ่าน share link">
                  <Input />
                </Form.Item>
              </Space>

              <Space size="middle" style={{ display: 'flex' }}>
                <Form.Item name={['reality', 'shortId']} label="Short ID (hex)" style={{ flex: 1 }}>
                  <Input placeholder="0-16 hex chars" />
                </Form.Item>
                <Form.Item label=" " style={{ display: 'flex', alignItems: 'flex-end' }}>
                  <Space wrap>
                    <Button icon={<ThunderboltOutlined />} onClick={genRealityKeys}>Gen keys</Button>
                    <Button icon={<ReloadOutlined />} onClick={genShortId}>Rand ShortID</Button>
                    <Button icon={<RadarChartOutlined />} onClick={() => setScannerOpen(true)}>
                      Scan target
                    </Button>
                  </Space>
                </Form.Item>
              </Space>

              <Form.Item name={['reality', 'fingerprint']} label="uTLS fingerprint">
                <Select options={UTLS_FPS.map((v) => ({ value: v, label: v }))} />
              </Form.Item>
            </>
          )}
        </>
      )}
    </>
  )

  const fallbacksTab = (
    <>
      <Alert type="info" showIcon style={{ marginBottom: 12 }}
             message="Fallbacks route non-matching TLS traffic to a decoy web server on the same port."
             description="Only VLESS and Trojan support this. Common use: run VLESS on :443 with a fallback to nginx/decoy site so the port looks like a normal HTTPS site." />
      {(protocol !== 'vless' && protocol !== 'trojan') ? (
        <Alert type="warning" showIcon message={`Protocol "${protocol}" does not support fallbacks.`} />
      ) : (
        <>
          <Space direction="vertical" style={{ width: '100%' }}>
            {fallbacks.map((fb, idx) => (
              <div key={idx} style={{
                border: '1px solid rgba(255,255,255,.10)', borderRadius: 6, padding: 12,
              }}>
                <Space size="middle" style={{ display: 'flex' }}>
                  <div style={{ flex: 1 }}>
                    <label style={{ fontSize: 12, opacity: 0.7 }}>Alpn</label>
                    <Input value={fb.alpn ?? ''}
                           placeholder="empty = any (h2, http/1.1)"
                           onChange={(e) => updateFallback(idx, 'alpn', e.target.value)} />
                  </div>
                  <div style={{ flex: 1 }}>
                    <label style={{ fontSize: 12, opacity: 0.7 }}>Path prefix</label>
                    <Input value={fb.path ?? ''}
                           placeholder="empty = any"
                           onChange={(e) => updateFallback(idx, 'path', e.target.value)} />
                  </div>
                </Space>
                <Space size="middle" style={{ display: 'flex', marginTop: 8 }}>
                  <div style={{ flex: 2 }}>
                    <label style={{ fontSize: 12, opacity: 0.7 }}>Dest (address:port or unix:/path)</label>
                    <Input value={fb.dest ?? ''}
                           placeholder="127.0.0.1:80"
                           onChange={(e) => updateFallback(idx, 'dest', e.target.value)} />
                  </div>
                  <div style={{ width: 100 }}>
                    <label style={{ fontSize: 12, opacity: 0.7 }}>Xver</label>
                    <InputNumber value={fb.xver ?? 0} min={0} max={2} style={{ width: '100%' }}
                                 onChange={(v) => updateFallback(idx, 'xver', Number(v) || 0)} />
                  </div>
                  <div style={{ display: 'flex', alignItems: 'flex-end' }}>
                    <Button danger icon={<DeleteOutlined />}
                            onClick={() => setFallbacks(fallbacks.filter((_, i) => i !== idx))} />
                  </div>
                </Space>
                <Tag style={{ marginTop: 8 }} color="blue">Rule {idx + 1}</Tag>
              </div>
            ))}

            <Button icon={<PlusOutlined />}
                    onClick={() => setFallbacks([...fallbacks, { dest: '127.0.0.1:80', xver: 0 }])}>
              Add fallback rule
            </Button>
          </Space>
        </>
      )}
    </>
  )

  function updateFallback(idx: number, key: string, val: any) {
    const next = [...fallbacks]
    next[idx] = { ...next[idx], [key]: val }
    setFallbacks(next)
  }

  const advancedTab = (
    <>
      <Alert type="warning" showIcon style={{ marginBottom: 12 }}
             message="Advanced mode overrides the General / Transport / Security / Fallbacks tabs."
             description="On save, only the JSON you paste below is sent to the server. Use for niche xray-core features not surfaced in the friendly form (mKCP tuning, XHTTP modes, header spoof, etc)." />

      <Form.Item label="Enable advanced mode" tooltip="When on, only these two JSON blobs are used">
        <Switch checked={advancedOn} onChange={setAdvancedOn} />
      </Form.Item>

      <Form.Item label="settings JSON" tooltip="protocol-specific — clients / method / etc">
        <Input.TextArea rows={9}
                        value={advancedSettings}
                        onChange={(e) => setAdvancedSettings(e.target.value)}
                        style={{ fontFamily: 'monospace', fontSize: 12 }} />
      </Form.Item>
      <Form.Item label="streamSettings JSON" tooltip="transport + security">
        <Input.TextArea rows={9}
                        value={advancedStream}
                        onChange={(e) => setAdvancedStream(e.target.value)}
                        style={{ fontFamily: 'monospace', fontSize: 12 }} />
      </Form.Item>

      <Space>
        <Button onClick={() => {
          try { setAdvancedSettings(JSON.stringify(JSON.parse(advancedSettings), null, 2)) }
          catch { setErr('Settings JSON invalid') }
          try { setAdvancedStream(JSON.stringify(JSON.parse(advancedStream), null, 2)) }
          catch { setErr('Stream JSON invalid') }
        }}>
          Format JSON
        </Button>
      </Space>
    </>
  )

  return (
    <Form
      form={form}
      layout="vertical"
      initialValues={initialValues}
      onFinish={handleFinish}
      style={{ paddingTop: 8 }}
    >
      <Tabs
        items={[
          { key: 'general',   label: 'General',   children: generalTab },
          { key: 'transport', label: 'Transport', children: transportTab, disabled: advancedOn },
          { key: 'security',  label: 'Security',  children: securityTab, disabled: advancedOn },
          { key: 'fallbacks', label: 'Fallbacks', children: fallbacksTab, disabled: advancedOn },
          { key: 'advanced',  label: <Space>Advanced {advancedOn && <Tag color="orange">active</Tag>}</Space>, children: advancedTab },
        ]}
      />
      {err && <Alert type="error" showIcon message={err} style={{ marginBottom: 12 }} />}
      <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
        <Button onClick={onCancel}>ยกเลิก</Button>
        <Button type="primary" loading={busy} htmlType="submit">
          {initial ? 'บันทึกการแก้ไข' : 'สร้าง'}
        </Button>
      </Space>

      <RealityScannerModal
        open={scannerOpen}
        onClose={() => setScannerOpen(false)}
        onPick={(host) => {
          form.setFieldValue(['reality', 'dest'], host + ':443')
          form.setFieldValue(['reality', 'sni'], host)
        }}
      />
    </Form>
  )
}

// ── helpers ────────────────────────────────────────────────────────

function asObj(v: unknown): any {
  if (!v) return {}
  if (typeof v === 'string') { try { return JSON.parse(v) } catch { return {} } }
  return v
}

function suggestPort(): number {
  return 10000 + Math.floor(Math.random() * 50000)
}

function b64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

