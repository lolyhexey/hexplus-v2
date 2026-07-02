import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Card, Col, ConfigProvider, Dropdown, Empty, Form, Input, InputNumber,
  Layout, Modal, Row, Select, Space, Statistic, Table, Tag, message, theme as antdTheme,
  type MenuProps, type TableColumnsType,
} from 'antd'
import {
  DeleteOutlined, EditOutlined, MoreOutlined, PlusOutlined,
  SortAscendingOutlined, SwapOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Rule } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'

// RoutingPage — 3x-ui-shaped rule list. Each rule maps
// (domains ∪ ips ∪ inbound_tag) → outbound_tag with a numeric priority.
// Lower priority runs first.

export default function RoutingPage() {
  const { isDark } = useTheme()
  const [messageApi, contextHolder] = message.useMessage()
  const [rows, setRows] = useState<Rule[]>([])
  const [outboundTags, setOutboundTags] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<Rule | null>(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const [rs, obs] = await Promise.all([
        API.rules.list(),
        API.outbounds.list().catch(() => []),
      ])
      setRows(rs ?? [])
      setOutboundTags(['direct', 'block', ...(obs ?? []).map((o) => o.tag)])
    } catch (e) { messageApi.error(String(e)) }
    finally { setLoading(false) }
  }, [messageApi])

  useEffect(() => { refresh() }, [refresh])

  const remove = useCallback(async (row: Rule) => {
    if (!confirm(`ลบ rule #${row.id}?`)) return
    try { await API.rules.remove(row.id); messageApi.success('deleted'); refresh() }
    catch (e) { messageApi.error(String(e)) }
  }, [messageApi, refresh])

  const columns: TableColumnsType<Rule> = useMemo(() => [
    {
      title: <Space><SortAscendingOutlined />Priority</Space>,
      dataIndex: 'priority', width: 100,
      sorter: (a, b) => a.priority - b.priority,
      defaultSortOrder: 'ascend' as const,
    },
    {
      title: 'Match', width: 340,
      render: (_: unknown, r) => (
        <Space direction="vertical" size={2}>
          {r.inbound_tag && (
            <div><span style={{ opacity: 0.55 }}>inbound = </span>
              <Tag>{r.inbound_tag}</Tag></div>
          )}
          {r.domains.length > 0 && (
            <div style={{ fontSize: 12 }}>
              <span style={{ opacity: 0.55 }}>domains: </span>
              {r.domains.slice(0, 3).join(', ')}{r.domains.length > 3 ? ` +${r.domains.length - 3}` : ''}
            </div>
          )}
          {r.ips.length > 0 && (
            <div style={{ fontSize: 12 }}>
              <span style={{ opacity: 0.55 }}>ips: </span>
              {r.ips.slice(0, 3).join(', ')}{r.ips.length > 3 ? ` +${r.ips.length - 3}` : ''}
            </div>
          )}
        </Space>
      ),
    },
    {
      title: <Space><SwapOutlined />Outbound</Space>,
      dataIndex: 'outbound_tag', width: 160,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: 'Remark', dataIndex: 'remark', ellipsis: true,
      render: (v: string) => v || <span style={{ opacity: 0.4 }}>—</span>,
    },
    {
      title: 'Enabled', dataIndex: 'enabled', width: 90, align: 'center',
      render: (v: boolean) => v ? <Tag color="success">on</Tag> : <Tag>off</Tag>,
    },
    {
      title: '', width: 60, fixed: 'right', align: 'center',
      render: (_: unknown, row) => {
        const menu: MenuProps['items'] = [
          { key: 'edit',   icon: <EditOutlined />,   label: 'Edit' },
          { type: 'divider' },
          { key: 'delete', icon: <DeleteOutlined />, label: 'Delete', danger: true },
        ]
        return (
          <Dropdown trigger={['click']} placement="bottomRight" menu={{
            items: menu,
            onClick: ({ key }) => {
              if (key === 'edit') { setEditing(row); setDialogOpen(true) }
              if (key === 'delete') remove(row)
            }
          }}>
            <Button size="small" icon={<MoreOutlined />} />
          </Dropdown>
        )
      },
    },
  ], [remove])

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
            <Row gutter={[16, 12]}>
              <Col xs={24} md={8}>
                <Card><Statistic title="Rules" value={rows.length}
                  prefix={<SwapOutlined />} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Enabled"
                  value={rows.filter((r) => r.enabled).length}
                  valueStyle={{ color: '#52c41a' }} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Outbound targets"
                  value={outboundTags.length} /></Card>
              </Col>

              <Col span={24}>
                <Card
                  hoverable
                  title={
                    <Space>
                      <Button type="primary" icon={<PlusOutlined />}
                              onClick={() => { setEditing(null); setDialogOpen(true) }}>
                        Add rule
                      </Button>
                    </Space>
                  }
                >
                  {rows.length === 0 && !loading ? (
                    <Empty description="ยังไม่มี routing rule — traffic ทั้งหมดออก `direct` outbound" />
                  ) : (
                    <Table<Rule>
                      rowKey="id"
                      size="middle"
                      loading={loading}
                      columns={columns}
                      dataSource={rows}
                      pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
                      scroll={{ x: 900 }}
                    />
                  )}
                </Card>
              </Col>

              <Col span={24}>
                <Alert type="info" showIcon
                  message="How routing works"
                  description={
                    <ul style={{ margin: 0, paddingLeft: 18 }}>
                      <li>Rules ทำงานตาม priority (น้อย → มาก) — rule แรกที่ match ชนะ</li>
                      <li>Match: domain / IP / inbound tag — เว้นว่างหมด = match ทุกอย่าง</li>
                      <li>Outbound tag: <code>direct</code> (freedom), <code>block</code> (blackhole),
                          หรือชื่อจาก Outbounds page</li>
                    </ul>
                  } />
              </Col>
            </Row>
          </Layout.Content>
        </Layout>
      </Layout>

      <RuleModal
        open={dialogOpen}
        editing={editing}
        outboundTags={outboundTags}
        onClose={() => setDialogOpen(false)}
        onSaved={() => { setDialogOpen(false); refresh() }}
        messageApi={messageApi}
      />
    </ConfigProvider>
  )
}

function RuleModal({
  open, editing, outboundTags, onClose, onSaved, messageApi,
}: {
  open: boolean
  editing: Rule | null
  outboundTags: string[]
  onClose: () => void
  onSaved: () => void
  messageApi: any
}) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return
    form.setFieldsValue(editing ? {
      priority: editing.priority,
      outbound_tag: editing.outbound_tag,
      inbound_tag: editing.inbound_tag,
      domains: editing.domains.join('\n'),
      ips: editing.ips.join('\n'),
      remark: editing.remark,
      enabled: editing.enabled,
    } : {
      priority: 100, outbound_tag: 'direct', inbound_tag: '',
      domains: '', ips: '', remark: '', enabled: true,
    })
  }, [open, editing, form])

  async function finish(values: any) {
    try {
      const body = {
        priority: values.priority,
        outbound_tag: values.outbound_tag,
        inbound_tag: values.inbound_tag ?? '',
        domains: (values.domains ?? '').split('\n').map((s: string) => s.trim()).filter(Boolean),
        ips: (values.ips ?? '').split('\n').map((s: string) => s.trim()).filter(Boolean),
        protocols: values.protocols ?? [],
        port_range: values.port_range ?? '',
        remark: values.remark ?? '',
        enabled: values.enabled,
      }
      if (editing) await API.rules.update(editing.id, body)
      else await API.rules.create(body)
      messageApi.success('saved')
      onSaved()
    } catch (e) { messageApi.error(String(e)) }
  }

  return (
    <Modal open={open} onCancel={onClose}
           title={editing ? `Edit rule #${editing.id}` : 'New routing rule'}
           footer={null} width={640} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={finish}>
        <Row gutter={16}>
          <Col xs={24} md={8}>
            <Form.Item name="priority" label="Priority"
                        tooltip="น้อย = ทำก่อน">
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
          <Col xs={24} md={16}>
            <Form.Item name="outbound_tag" label="Outbound"
                        rules={[{ required: true }]}>
              <Select
                showSearch
                options={outboundTags.map((v) => ({ value: v, label: v }))}
              />
            </Form.Item>
          </Col>
        </Row>

        <Form.Item name="inbound_tag" label="Inbound tag (optional)"
                    tooltip="เว้นว่าง = match inbound ใดๆ">
          <Input placeholder="เว้นว่างเพื่อ match ทุก inbound" />
        </Form.Item>
        <Form.Item name="domains" label="Domains (บรรทัดละ 1)">
          <Input.TextArea rows={4} style={{ fontFamily: 'monospace', fontSize: 12 }}
                          placeholder={'example.com\ngeosite:cn\ndomain:youtube.com'} />
        </Form.Item>
        <Form.Item name="ips" label="IPs (บรรทัดละ 1)">
          <Input.TextArea rows={4} style={{ fontFamily: 'monospace', fontSize: 12 }}
                          placeholder={'1.1.1.1\n8.8.8.0/24\ngeoip:cn'} />
        </Form.Item>
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <Form.Item name="protocols" label="Protocols (optional)"
                       tooltip="match traffic by app-layer protocol">
              <Select mode="multiple" allowClear
                      options={['http', 'tls', 'bittorrent'].map((v) => ({ value: v, label: v }))} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="port_range" label="Ports (optional)"
                       tooltip="e.g. 80,443 or 1000-2000">
              <Input placeholder="80,443 or 1000-2000" />
            </Form.Item>
          </Col>
        </Row>
        <Form.Item name="remark" label="Remark">
          <Input />
        </Form.Item>

        <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
          <Button onClick={onClose}>ยกเลิก</Button>
          <Button type="primary" htmlType="submit">
            {editing ? 'บันทึก' : 'สร้าง'}
          </Button>
        </Space>
      </Form>
    </Modal>
  )
}
