import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Badge, Button, Card, Col, ConfigProvider, Descriptions, Input, Layout,
  Radio, Row, Space, Spin, Statistic, Tabs, Tag, message, theme as antdTheme,
} from 'antd'
import {
  CodeOutlined, CopyOutlined, DownloadOutlined, FileTextOutlined,
  ReloadOutlined, StopOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type ServerStatus } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'
import { formatBytes, formatUptime } from '@/pages/index/formatters'

// XrayPage — port of 3x-ui/frontend/src/pages/xray/XrayPage.tsx layout.
// Because our routing / outbounds / DNS / balancers live under separate
// backend endpoints, this page focuses on the "Basic" and "Advanced"
// tabs of 3x-ui — control + JSON config + live journalctl log tail.

export default function XrayPage() {
  const { isDark } = useTheme()
  const [messageApi, contextHolder] = message.useMessage()
  const [status, setStatus] = useState<ServerStatus | null>(null)
  const [config, setConfig] = useState<string>('')
  const [logs, setLogs] = useState<string>('')
  const [logsLoading, setLogsLoading] = useState(false)
  const [logLines, setLogLines] = useState(200)
  const [tab, setTab] = useState('basic')
  // Two-phase loading:
  //   fetched  = false until the first status + config round-trip
  //              lands, so the whole page shows a centered Spin
  //              instead of empty cards.
  //   busy     = true while a mutating call is in flight (restart,
  //              stop, refresh).  Applied to the outer Spin as well
  //              so the operator gets clear feedback the request
  //              is running.
  const [fetched, setFetched] = useState(false)
  const [busy, setBusy] = useState(false)
  const [busyLabel, setBusyLabel] = useState('Loading…')

  const refresh = useCallback(async (opts?: { silent?: boolean }) => {
    if (!opts?.silent) { setBusy(true); setBusyLabel('Refreshing…') }
    try {
      const [s, c] = await Promise.all([API.server.status(), API.server.xrayConfig()])
      if (s.success) setStatus(s.obj)
      if (c.success) setConfig(prettyJson(c.obj))
      setFetched(true)
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusy(false) }
  }, [messageApi])

  useEffect(() => { refresh({ silent: true }) }, [refresh])

  const restart = useCallback(async () => {
    setBusy(true); setBusyLabel('Restarting Xray…')
    try {
      messageApi.loading({ content: 'Restarting…', key: 'xray', duration: 0 })
      await API.server.xrayRestart()
      messageApi.success({ content: 'Xray restarted', key: 'xray' })
      await new Promise((r) => setTimeout(r, 800))
      await refresh({ silent: true })
    } catch (e) { messageApi.error({ content: String(e), key: 'xray' }) }
    finally { setBusy(false) }
  }, [messageApi, refresh])

  const stopXray = useCallback(async () => {
    setBusy(true); setBusyLabel('Stopping Xray…')
    try {
      messageApi.loading({ content: 'Stopping…', key: 'xray', duration: 0 })
      await API.server.xrayStop()
      messageApi.success({ content: 'Xray stopped', key: 'xray' })
      await new Promise((r) => setTimeout(r, 500))
      await refresh({ silent: true })
    } catch (e) { messageApi.error({ content: String(e), key: 'xray' }) }
    finally { setBusy(false) }
  }, [messageApi, refresh])

  const fetchLogs = useCallback(async () => {
    setLogsLoading(true)
    try {
      const r = await API.server.xrayLog(logLines)
      if (r.success) setLogs(r.obj)
    } catch (e) { messageApi.error(String(e)) }
    finally { setLogsLoading(false) }
  }, [logLines, messageApi])

  useEffect(() => { if (tab === 'logs') fetchLogs() }, [tab, fetchLogs])

  const stateTag = useMemo(() => {
    const st = status?.xray.state ?? 'stopped'
    const color = st === 'running' ? 'green' : st === 'starting' ? 'gold' : 'red'
    const label = st === 'running' ? 'Running' : st === 'starting' ? 'Starting' : 'Stopped'
    return <Tag color={color}><Badge status={color === 'green' ? 'success' : color === 'gold' ? 'processing' : 'error'} text={label} /></Tag>
  }, [status])

  const basicTab = (
    <Row gutter={[16, 12]}>
      <Col xs={24} lg={12}>
        <Card>
          <Descriptions
            title={<Space>Xray core {stateTag}</Space>}
            column={1}
            size="middle"
          >
            <Descriptions.Item label="Version">
              {status?.xray.version || <span style={{ opacity: 0.5 }}>not installed</span>}
            </Descriptions.Item>
            <Descriptions.Item label="Config path">
              <code style={{ fontSize: 12 }}>/var/lib/hexplus/xray/config.json</code>
            </Descriptions.Item>
            <Descriptions.Item label="Systemd unit">
              <code style={{ fontSize: 12 }}>hexplus-xray.service</code>
            </Descriptions.Item>
            <Descriptions.Item label="Server uptime">
              {status ? formatUptime(status.uptime) : '—'}
            </Descriptions.Item>
          </Descriptions>
          <Space style={{ marginTop: 16 }}>
            <Button type="primary" icon={<ReloadOutlined />} loading={busy} onClick={restart}>
              Restart Xray
            </Button>
            <Button danger icon={<StopOutlined />} loading={busy} onClick={stopXray}>
              Stop
            </Button>
          </Space>
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card>
          <Row gutter={[12, 12]}>
            <Col span={12}>
              <Statistic
                title={<Space><ThunderboltOutlined />CPU</Space>}
                value={`${status?.cpu.percent ?? 0}%`}
                valueStyle={{ color: status && status.cpu.percent > 80 ? '#ff4d4f' : '#5cadff' }}
              />
            </Col>
            <Col span={12}>
              <Statistic
                title="Memory"
                value={status ? `${status.mem.percent}%` : '—'}
              />
            </Col>
            <Col span={12}>
              <Statistic
                title="Uplink"
                value={formatBytes(status?.netTraffic.sent ?? 0)}
              />
            </Col>
            <Col span={12}>
              <Statistic
                title="Downlink"
                value={formatBytes(status?.netTraffic.recv ?? 0)}
              />
            </Col>
          </Row>
        </Card>
      </Col>
    </Row>
  )

  const configTab = (
    <Card
      hoverable
      title={
        <Space>
          <CodeOutlined />
          <span>config.json</span>
          <Badge status="processing" text="live" />
        </Space>
      }
      extra={
        <Space>
          <Button icon={<CopyOutlined />}
                  onClick={() => { navigator.clipboard.writeText(config); messageApi.success('copied') }}>
            Copy
          </Button>
          <Button icon={<DownloadOutlined />}
                  onClick={() => {
                    const blob = new Blob([config], { type: 'application/json' })
                    const url = URL.createObjectURL(blob)
                    const a = document.createElement('a')
                    a.href = url; a.download = 'config.json'; a.click()
                    URL.revokeObjectURL(url)
                  }}>
            Download
          </Button>
          <Button icon={<ReloadOutlined />} loading={busy} onClick={() => refresh()}>Refresh</Button>
        </Space>
      }
    >
      <Alert type="info" showIcon closable
             message="Read-only preview"
             description="Config is generated automatically from inbounds/outbounds/routing rules — edit them in their pages to change this file."
             style={{ marginBottom: 12 }} />
      <Input.TextArea
        value={config}
        readOnly
        rows={24}
        style={{
          fontFamily: 'ui-monospace, SF Mono, Consolas, Menlo, monospace',
          fontSize: 12,
        }}
      />
    </Card>
  )

  const logsTab = (
    <Card
      hoverable
      title={
        <Space>
          <FileTextOutlined />
          <span>journalctl — hexplus-xray.service</span>
        </Space>
      }
      extra={
        <Space>
          <Radio.Group value={logLines} onChange={(e) => setLogLines(e.target.value)}
                        optionType="button" buttonStyle="solid" size="small"
                        options={[100, 200, 500, 1000].map((n) => ({ value: n, label: `${n} lines` }))} />
          <Button icon={<ReloadOutlined />} loading={logsLoading} onClick={fetchLogs}>Refresh</Button>
        </Space>
      }
    >
      <Spin spinning={logsLoading} tip="Loading logs…">
        <Input.TextArea
          value={logs || '(no logs)'}
          readOnly
          rows={22}
          style={{
            fontFamily: 'ui-monospace, SF Mono, Consolas, Menlo, monospace',
            fontSize: 12, background: 'rgba(0,0,0,0.02)',
          }}
        />
      </Spin>
    </Card>
  )

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
            <Spin spinning={!fetched || busy} tip={busyLabel} size="large" delay={200}>
              {fetched ? (
                <Tabs
                  activeKey={tab}
                  onChange={setTab}
                  type="card"
                  items={[
                    { key: 'basic',  label: 'Basic',        children: basicTab },
                    { key: 'config', label: 'Config',       children: configTab },
                    { key: 'logs',   label: 'Logs',         children: logsTab },
                  ]}
                />
              ) : (
                <div style={{ minHeight: 320 }} />
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  )
}

// prettyJson attempts to reformat a JSON string. Returns the input
// unchanged when parsing fails — useful when the file is truncated.
function prettyJson(raw: string): string {
  try { return JSON.stringify(JSON.parse(raw), null, 2) }
  catch { return raw }
}
