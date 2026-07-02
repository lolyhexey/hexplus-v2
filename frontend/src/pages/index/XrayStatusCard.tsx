import { Button, Card, Space, Statistic, Tag } from 'antd'
import { ReloadOutlined, StopOutlined, FileTextOutlined, SwapOutlined } from '@ant-design/icons'
import type { ServerStatus } from '@/lib/api'

// XrayStatusCard — 3x-ui-shaped card that shows Xray running/stopped
// state, version, and buttons to restart / stop / view logs / switch
// version. Actions call handlers passed by IndexPage (same interface
// 3x-ui uses).
export default function XrayStatusCard({
  status,
  isMobile,
  onRestartXray,
  onStopXray,
  onOpenXrayLogs,
  onOpenVersionSwitch,
}: {
  status: ServerStatus
  isMobile: boolean
  onRestartXray: () => void
  onStopXray: () => void
  onOpenXrayLogs: () => void
  onOpenVersionSwitch: () => void
}) {
  const running = status.xray.state === 'running'
  const stateTag = (
    <Tag color={running ? 'green' : status.xray.state === 'starting' ? 'gold' : 'red'}>
      {running ? 'Running' : status.xray.state === 'starting' ? 'Starting' : 'Stopped'}
    </Tag>
  )
  return (
    <Card
      hoverable
      title={<Space>Xray {stateTag}</Space>}
      actions={[
        <Space key="restart" onClick={onRestartXray} style={{ cursor: 'pointer' }}>
          <ReloadOutlined />{!isMobile && <span>Restart</span>}
        </Space>,
        <Space key="stop" onClick={onStopXray} style={{ cursor: 'pointer' }}>
          <StopOutlined />{!isMobile && <span>Stop</span>}
        </Space>,
        <Space key="logs" onClick={onOpenXrayLogs} style={{ cursor: 'pointer' }}>
          <FileTextOutlined />{!isMobile && <span>Logs</span>}
        </Space>,
        <Space key="version" onClick={onOpenVersionSwitch} style={{ cursor: 'pointer' }}>
          <SwapOutlined />{!isMobile && <span>Version</span>}
        </Space>,
      ]}
    >
      <Statistic
        title="Version"
        value={status.xray.version || 'not installed'}
      />
      {status.xray.errorMsg && (
        <div style={{ marginTop: 8, color: '#ff4d4f', fontSize: 12 }}>
          {status.xray.errorMsg}
        </div>
      )}
      <Button
        block
        type="primary"
        style={{ marginTop: 12 }}
        onClick={onRestartXray}
      >
        {running ? 'Restart Xray service' : 'Start Xray service'}
      </Button>
    </Card>
  )
}
