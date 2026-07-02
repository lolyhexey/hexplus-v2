import { Card, Descriptions, Space, Tag } from 'antd'
import { GlobalOutlined } from '@ant-design/icons'
import { StubPage } from './StubPage'

// HostsPage stub — 3x-ui feature that lets one client be published
// over multiple "hosts" (address:port pairs) so a subscription URL
// contains alternate connection endpoints (main server + CDN + backup
// server). Our subscription server today emits one link per client;
// hosts multiplies that by the alternate-address set.
export default function HostsPage() {
  return (
    <StubPage
      title="Alternate hosts"
      phase="Phase 16"
      subTitle="Publish one client to multiple (address, port) pairs — main VPS + CDN + failover — inside one subscription URL."
      extra={
        <Card style={{ maxWidth: 640, margin: '0 auto', textAlign: 'left' }}>
          <Descriptions column={1} bordered size="middle">
            <Descriptions.Item label="Current subscription">
              1 link ต่อ client → server ตัวเดียว
            </Descriptions.Item>
            <Descriptions.Item label="Hosts adds">
              <Space direction="vertical" size={2}>
                <span>• Direct: <code>vps.example.com:443</code></span>
                <span>• Cloudflare: <code>cdn.example.com:443</code></span>
                <span>• Backup: <code>backup.example.com:8443</code></span>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="Result">
              <Tag icon={<GlobalOutlined />} color="blue">3 endpoints</Tag> ใน link เดียว
            </Descriptions.Item>
          </Descriptions>
        </Card>
      }
    />
  )
}
