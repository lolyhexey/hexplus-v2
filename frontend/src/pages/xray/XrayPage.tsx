import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Badge, Button, Card, Col, ConfigProvider, Descriptions, Input, Layout,
  Radio, Row, Space, Statistic, Tabs, Tag, message, theme as antdTheme,
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
  const [logLines, setLogLines] = useState(200)
  const [tab, setTab] = useState('basic')

  const refresh = useCallback(async () => {
    try {
      const [s, c] = await Promise.all([API.server.status(), API.server.xrayConfig()])
      if (s.success) setStatus(s.obj)
      if (c.success) setConfig(prettyJson(c.obj))
    } catch (e) { messageApi.error(String(e)) }
  }, [messageApi])

  useEffect(() => { refresh() }, [refresh])

  const restart = useCallback(async () => {
    try {
      messageApi.loading({ content: 'Restarting…', key: 'xray' })
      await API.server.xrayRestart()
      messageApi.success({ content: 'Xray restarted', key: 'xray' })
      setTimeout(refresh, 800)
    } catch (e) { messageApi.error({ content: String(e), key: 'xray' }) }
  }, [messageApi, refresh])

  const stopXray = useCallback(async () => {
    try {
      messageApi.loading({ content: 'Stopping…', key: 'xray' })
      await API.server.xrayStop()
      messageApi.success({ content: 'Xray stopped', key: 'xray' })
      setTimeout(refresh, 500)
    } catch (e) { messageApi.error({ content: String(e), key: 'xray' }) }
  }, [messageApi, refresh])

  const fetchLogs = useCallback(async () => {
    try {
      const r = await API.server.xrayLog(logLines)
      if (r.success) setLogs(r.obj)
    } catch (e) { messageApi.error(String(e)) }
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
            <Button type="primary" icon={<ReloadOutlined />} onClick={restart}>
              Restart Xray
            </Button>
            <Button danger icon={<StopOutlined />} onClick={stopXray}>
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
          <Button icon={<ReloadOutlined />} onClick={refresh}>Refresh</Button>
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
          <Button icon={<ReloadOutlined />} onClick={fetchLogs}>Refresh</Button>
        </Space>
      }
    >
      <Input.TextArea
        value={logs || '(no logs)'}
        readOnly
        rows={22}
        style={{
          fontFamily: 'ui-monospace, SF Mono, Consolas, Menlo, monospace',
          fontSize: 12, background: 'rgba(0,0,0,0.02)',
        }}
      />
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
