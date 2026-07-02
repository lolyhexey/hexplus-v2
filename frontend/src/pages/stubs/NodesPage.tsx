import { Alert, Card, Descriptions, Tag } from 'antd'
import { ClusterOutlined } from '@ant-design/icons'
import { StubPage } from './StubPage'

// NodesPage stub — 3x-ui uses "nodes" for multi-server deployments:
// one master panel controls Xray daemons on several remote nodes and
// the same subscription URL serves configs from every node.
//
// Our schema already reserves a `node_id` column on inbounds and
// clients (see internal/panel/db/migrations.go v1) so adding real
// multi-node support later is a schema-preserving change.
export default function NodesPage() {
  return (
    <StubPage
      title="Multi-node cluster"
      phase="Phase 17 — v2.x"
      subTitle="Manage a cluster of Xray nodes from one master panel — shared subscription URL, per-node traffic, failover."
      extra={
        <Card style={{ maxWidth: 720, margin: '0 auto', textAlign: 'left' }}>
          <Descriptions column={1} bordered size="middle" style={{ marginBottom: 16 }}>
            <Descriptions.Item label="This node">
              <Tag icon={<ClusterOutlined />} color="green">local</Tag>
              — running Xray + Panel on this box
            </Descriptions.Item>
            <Descriptions.Item label="Remote nodes">
              <span style={{ opacity: 0.5 }}>none configured</span>
            </Descriptions.Item>
            <Descriptions.Item label="Cluster mode">
              <Tag>Single-node</Tag>
            </Descriptions.Item>
          </Descriptions>
          <Alert
            type="info"
            showIcon
            message="Schema is ready"
            description={
              <>
                <code>inbounds.node_id</code> และ <code>clients.node_id</code>{' '}
                มีอยู่แล้ว (default = 1) — เพิ่ม node จริงได้โดยไม่ต้อง migration.
              </>
            }
          />
        </Card>
      }
    />
  )
}
