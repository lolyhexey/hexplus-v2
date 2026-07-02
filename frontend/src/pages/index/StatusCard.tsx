import { Card, Col, Progress, Row, Tooltip } from 'antd'
import { AreaChartOutlined } from '@ant-design/icons'
import type { ServerStatus } from '@/lib/api'
import { formatBytes, formatCpuCores, formatMhz } from './formatters'

// StatusCard — direct port of 3x-ui/frontend/src/pages/index/StatusCard.tsx.
// Four dashboard-style Progress gauges (CPU, Memory, Swap, Storage)
// arranged as 2x2 on desktop, single column stacked on mobile.
export default function StatusCard({
  status, isMobile,
}: { status: ServerStatus; isMobile: boolean }) {
  const gaugeSize = isMobile ? 60 : 90
  const strokeWidth = isMobile ? 7 : 5
  const railColor = 'rgba(255,255,255,0.10)'

  return (
    <Card hoverable>
      <Row gutter={[0, isMobile ? 16 : 0]}>
        <Col xs={24} md={12}>
          <Row>
            <Col span={12} style={{ textAlign: 'center' }}>
              <Progress
                type="dashboard"
                strokeColor={status.cpu.color}
                trailColor={railColor}
                strokeWidth={strokeWidth}
                percent={status.cpu.percent}
                size={gaugeSize}
              />
              <div>
                <b>CPU:</b> {formatCpuCores(status.cpuCores)}
                <Tooltip
                  title={
                    <>
                      <div><b>Logical processors:</b> {status.logicalPro}</div>
                      <div><b>Frequency:</b> {formatMhz(status.cpuSpeedMhz)}</div>
                    </>
                  }
                >
                  <AreaChartOutlined style={{ marginInlineStart: 4 }} />
                </Tooltip>
              </div>
            </Col>
            <Col span={12} style={{ textAlign: 'center' }}>
              <Progress
                type="dashboard"
                strokeColor={status.mem.color}
                trailColor={railColor}
                strokeWidth={strokeWidth}
                percent={status.mem.percent}
                size={gaugeSize}
              />
              <div><b>Memory:</b> {formatBytes(status.mem.current)} / {formatBytes(status.mem.total)}</div>
            </Col>
          </Row>
        </Col>

        <Col xs={24} md={12}>
          <Row>
            <Col span={12} style={{ textAlign: 'center' }}>
              <Progress
                type="dashboard"
                strokeColor={status.swap.color}
                trailColor={railColor}
                strokeWidth={strokeWidth}
                percent={status.swap.percent}
                size={gaugeSize}
              />
              <div><b>Swap:</b> {formatBytes(status.swap.current)} / {formatBytes(status.swap.total)}</div>
            </Col>
            <Col span={12} style={{ textAlign: 'center' }}>
              <Progress
                type="dashboard"
                strokeColor={status.disk.color}
                trailColor={railColor}
                strokeWidth={strokeWidth}
                percent={status.disk.percent}
                size={gaugeSize}
              />
              <div><b>Storage:</b> {formatBytes(status.disk.current)} / {formatBytes(status.disk.total)}</div>
            </Col>
          </Row>
        </Col>
      </Row>
    </Card>
  )
}
