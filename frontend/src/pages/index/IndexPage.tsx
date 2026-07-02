import { useCallback, useEffect, useState } from 'react'
import {
  Card, Col, ConfigProvider, Layout, Row, Space, Spin, Statistic, theme as antdTheme,
} from 'antd'
import {
  ArrowDownOutlined, ArrowUpOutlined,
  BarsOutlined, ClockCircleOutlined, ControlOutlined,
  CloudDownloadOutlined, CloudServerOutlined, CloudUploadOutlined,
  GlobalOutlined, SwapOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type ServerStatus } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'
import StatusCard from './StatusCard'
import XrayStatusCard from './XrayStatusCard'
import { formatBps, formatBytes, formatUptime } from './formatters'

// IndexPage — port of 3x-ui/frontend/src/pages/index/IndexPage.tsx.
// Layout: AppSidebar on the left; content area holds a 24-col grid.
// Row 1 spans the full width with StatusCard (CPU/mem/swap/disk).
// Row 2 splits into XrayStatusCard | Link card (Logs/Config/Backup).
// Row 3: HEXPLUS brand card | Uptime/loads. Row 4: Traffic + IP info.
//
// Data refreshes every 2 seconds via polling. Realtime WS is 3x-ui's
// choice; polling is simpler and the payload is small.

export default function IndexPage() {
  const { isDark } = useTheme()
  const [status, setStatus] = useState<ServerStatus | null>(null)
  const [fetched, setFetched] = useState(false)
  const [isMobile, setIsMobile] = useState(false)

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 768px)')
    setIsMobile(mq.matches)
    const on = () => setIsMobile(mq.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])

  const refresh = useCallback(async () => {
    try {
      const res = await API.server.status()
      if (res.success) setStatus(res.obj)
      setFetched(true)
    } catch { setFetched(true) }
  }, [])

  useEffect(() => {
    refresh()
    const id = window.setInterval(refresh, 2000)
    return () => window.clearInterval(id)
  }, [refresh])

  async function stopXray() {
    // POST placeholder — backend endpoint /api/server/xray/stop lands
    // in the next iteration.
    try { await fetch('api/server/xray/stop', { method: 'POST', credentials: 'include' }) } catch {}
    refresh()
  }
  async function restartXray() {
    try { await fetch('api/server/xray/restart', { method: 'POST', credentials: 'include' }) } catch {}
    refresh()
  }

  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    }}>
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout>
          <Layout.Content style={{ padding: 16 }}>
            <Spin spinning={!fetched} delay={200} size="large">
              {status && (
                <Row gutter={[16, 12]}>
                  <Col span={24}>
                    <StatusCard status={status} isMobile={isMobile} />
                  </Col>

                  <Col xs={24} lg={12}>
                    <XrayStatusCard
                      status={status}
                      isMobile={isMobile}
                      onStopXray={stopXray}
                      onRestartXray={restartXray}
                      onOpenXrayLogs={() => {}}
                      onOpenVersionSwitch={() => {}}
                    />
                  </Col>

                  <Col xs={24} lg={12}>
                    <Card
                      title="Link"
                      hoverable
                      actions={[
                        <Space key="logs"><BarsOutlined />{!isMobile && <span>Logs</span>}</Space>,
                        <Space key="config"><ControlOutlined />{!isMobile && <span>Config</span>}</Space>,
                        <Space key="backup"><CloudServerOutlined />{!isMobile && <span>Backup</span>}</Space>,
                      ]}
                    >
                      <Space direction="vertical" size={4}>
                        <span><b>OS:</b> {status.osVersion}</span>
                        <span><b>Kernel:</b> Linux</span>
                        <span><ClockCircleOutlined /> Uptime: {formatUptime(status.uptime)}</span>
                      </Space>
                    </Card>
                  </Col>

                  <Col xs={24} md={12} lg={6}>
                    <Card>
                      <Statistic
                        title="Real-time speed (upload)"
                        value={formatBps(status.netIO.up)}
                        prefix={<ArrowUpOutlined />}
                        valueStyle={{ color: '#5cadff' }}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} md={12} lg={6}>
                    <Card>
                      <Statistic
                        title="Real-time speed (download)"
                        value={formatBps(status.netIO.down)}
                        prefix={<ArrowDownOutlined />}
                        valueStyle={{ color: '#52c41a' }}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} md={12} lg={6}>
                    <Card>
                      <Statistic
                        title="Total upload"
                        value={formatBytes(status.netTraffic.sent)}
                        prefix={<CloudUploadOutlined />}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} md={12} lg={6}>
                    <Card>
                      <Statistic
                        title="Total download"
                        value={formatBytes(status.netTraffic.recv)}
                        prefix={<CloudDownloadOutlined />}
                      />
                    </Card>
                  </Col>

                  <Col xs={24} md={12}>
                    <Card>
                      <Statistic
                        title={<Space><SwapOutlined />TCP / UDP connections</Space>}
                        value={`${status.tcpCount} / ${status.udpCount}`}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} md={12}>
                    <Card>
                      <Statistic
                        title={<Space><ThunderboltOutlined />Load average (1m/5m/15m)</Space>}
                        value={status.loads.map((n) => n.toFixed(2)).join(' / ')}
                      />
                    </Card>
                  </Col>

                  <Col xs={24} md={12}>
                    <Card>
                      <Space direction="vertical" size={2}>
                        <Statistic title={<Space><GlobalOutlined />IPv4</Space>} value={status.publicIP.v4 || '—'} />
                      </Space>
                    </Card>
                  </Col>
                  <Col xs={24} md={12}>
                    <Card>
                      <Space direction="vertical" size={2}>
                        <Statistic title={<Space><GlobalOutlined />IPv6</Space>} value={status.publicIP.v6 || '—'} />
                      </Space>
                    </Card>
                  </Col>
                </Row>
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  )
}
