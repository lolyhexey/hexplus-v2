import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Card, Col, ConfigProvider, Dropdown, Empty, Layout, Modal,
  Row, Space, Statistic, Table, Tag, message, theme as antdTheme,
  type MenuProps, type TableColumnsType,
} from 'antd'
import {
  CloudOutlined, DeleteOutlined, EditOutlined, MoreOutlined, PlusOutlined,
  SwapOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Outbound } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'
import OutboundForm, { type OutboundFormValue } from '@/components/OutboundForm'

// OutboundsPage — 3x-ui-shaped outbound list. Adds a one-click
// Cloudflare WARP provisioner (backend hits Cloudflare's public reg
// endpoint and stores the resulting WireGuard peer).

const protocolColor: Record<string, string> = {
  freedom: 'green', blackhole: 'red',
  wireguard: 'cyan', vless: 'geekblue', vmess: 'blue',
  trojan: 'volcano', shadowsocks: 'purple', http: 'gold', socks: 'orange',
}

export default function OutboundsPage() {
  const { isDark } = useTheme()
  const [messageApi, contextHolder] = message.useMessage()
  const [rows, setRows] = useState<Outbound[]>([])
  const [loading, setLoading] = useState(true)
  const [busyWarp, setBusyWarp] = useState(false)
  const [editing, setEditing] = useState<Outbound | null>(null)
  const [createOpen, setCreateOpen] = useState(false)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setRows((await API.outbounds.list()) ?? [])
    } catch (e) { messageApi.error(String(e)) }
    finally { setLoading(false) }
  }, [messageApi])
  useEffect(() => { refresh() }, [refresh])

  const provisionWARP = useCallback(async () => {
    setBusyWarp(true)
    try {
      const r = await API.outbounds.provisionWARP('warp')
      messageApi.success(`WARP provisioned as "${r.tag}"`)
      refresh()
    } catch (e) { messageApi.error(String(e)) }
    finally { setBusyWarp(false) }
  }, [messageApi, refresh])

  const remove = useCallback(async (row: Outbound) => {
    if (!confirm(`ลบ outbound "${row.tag}"?`)) return
    try { await API.outbounds.remove(row.id); messageApi.success('deleted'); refresh() }
    catch (e) { messageApi.error(String(e)) }
  }, [messageApi, refresh])

  const columns: TableColumnsType<Outbound> = useMemo(() => [
    { title: '#', dataIndex: 'id', width: 64 },
    {
      title: 'Tag', dataIndex: 'tag',
      render: (v: string, row) => (
        <Space direction="vertical" size={0}>
          <span style={{ fontWeight: 500 }}>{v}</span>
          {row.remark && <span style={{ fontSize: 12, opacity: 0.6 }}>{row.remark}</span>}
        </Space>
      ),
    },
    {
      title: 'Protocol', dataIndex: 'protocol', width: 140,
      render: (v: string) => <Tag color={protocolColor[v] || 'default'}>{v}</Tag>,
    },
    {
      title: 'Enabled', dataIndex: 'enabled', width: 100, align: 'center',
      render: (v: boolean) => v ? <Tag color="success">on</Tag> : <Tag>off</Tag>,
    },
    {
      title: 'Created', dataIndex: 'created_at', width: 140,
      render: (v: number) => v ? new Date(v * 1000).toLocaleDateString() : '—',
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
              if (key === 'edit') setEditing(row)
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
                <Card><Statistic title="Total outbounds"
                  value={rows.length}
                  prefix={<SwapOutlined />} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Enabled"
                  value={rows.filter((r) => r.enabled).length}
                  valueStyle={{ color: '#52c41a' }} /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic title="Cloudflare WARP"
                  value={rows.filter((r) => r.tag === 'warp' || r.protocol === 'wireguard').length}
                  prefix={<CloudOutlined />} /></Card>
              </Col>

              <Col span={24}>
                <Card
                  hoverable
                  title={
                    <Space>
                      <Button type="primary" icon={<PlusOutlined />}
                              onClick={() => { setEditing(null); setCreateOpen(true) }}>
                        Add outbound
                      </Button>
                      <Button icon={<CloudOutlined />} loading={busyWarp}
                              onClick={provisionWARP}>
                        Provision Cloudflare WARP
                      </Button>
                    </Space>
                  }
                >
                  {rows.length === 0 && !loading ? (
                    <Empty
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                      description="ยังไม่มี outbound — freedom/blackhole ถูก generate อัตโนมัติทุกครั้งใน xray config"
                    />
                  ) : (
                    <Table<Outbound>
                      rowKey="id"
                      size="middle"
                      loading={loading}
                      columns={columns}
                      dataSource={rows}
                      pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
                      scroll={{ x: 700 }}
                    />
                  )}
                </Card>
              </Col>

              <Col span={24}>
                <Alert type="info" showIcon
                       message={<Space><ThunderboltOutlined />About outbounds</Space>}
                       description={
                         <>
                           <div style={{ marginBottom: 4 }}>
                             <b>freedom</b> and <b>blackhole</b> outbounds are always synthesized by the config
                             generator — you don't need to add them here.
                           </div>
                           <div>
                             User-defined outbounds appear in <code>routing</code> rules as valid targets and land
                             in the final xray config.json.
                           </div>
                         </>
                       } />
              </Col>
            </Row>
          </Layout.Content>
        </Layout>
      </Layout>

      <Modal
        open={createOpen || !!editing}
        onCancel={() => { setCreateOpen(false); setEditing(null) }}
        title={editing ? `Edit ${editing.tag}` : 'New outbound'}
        footer={null}
        width={640}
        destroyOnHidden
      >
        <OutboundForm
          key={editing?.id ?? 'new'}
          initial={editing ?? undefined}
          onCancel={() => { setCreateOpen(false); setEditing(null) }}
          onSubmit={async (v: OutboundFormValue) => {
            try {
              if (editing) {
                await API.outbounds.update(editing.id, v as any)
              } else {
                await API.outbounds.create(v as any)
              }
              messageApi.success(editing ? 'updated' : 'created')
              setCreateOpen(false); setEditing(null)
              refresh()
            } catch (e) { messageApi.error(String(e)) }
          }}
        />
      </Modal>
    </ConfigProvider>
  )
}
