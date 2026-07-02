import { Card, Space, Tag } from 'antd'
import { TagsOutlined } from '@ant-design/icons'
import { StubPage } from './StubPage'

// GroupsPage stub — 3x-ui feature for grouping clients under labels so
// bulk-add/attach/detach operations can address a whole group at once.
// Backend still needs a group_id column + join table before we can
// ship a real UI.
export default function GroupsPage() {
  return (
    <StubPage
      title="Client groups"
      phase="Phase 15"
      subTitle="Group clients into labels (family, VIP, ทดลอง) for bulk operations across many inbounds at once."
      extra={
        <Card style={{ maxWidth: 640, margin: '0 auto', textAlign: 'left' }}>
          <div style={{ marginBottom: 8, opacity: 0.8 }}>ในระหว่างนี้ทำได้จากหน้า Clients:</div>
          <Space direction="vertical" size={4}>
            <div>• เลือก client หลายตัว → toggle enable/disable ทีเดียว</div>
            <div>• Reset traffic แบบ bulk ผ่าน CLI: <code>hexplus panel bulk reset</code></div>
          </Space>
          <Space wrap style={{ marginTop: 12 }}>
            <Tag icon={<TagsOutlined />}>Family</Tag>
            <Tag icon={<TagsOutlined />} color="gold">VIP</Tag>
            <Tag icon={<TagsOutlined />} color="cyan">Trial</Tag>
            <Tag icon={<TagsOutlined />} color="magenta">Reseller</Tag>
          </Space>
        </Card>
      }
    />
  )
}
