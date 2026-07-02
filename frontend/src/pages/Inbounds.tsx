import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Button, Card, Col, ConfigProvider, Dropdown, Layout,
  Row, Space, Statistic, Switch, Table, Tag, theme as antdTheme, message,
  type MenuProps, type TableColumnsType,
} from 'antd'
import {
  ArrowDownOutlined, ArrowUpOutlined,
  DeleteOutlined, EditOutlined, ExportOutlined, ImportOutlined,
  InfoCircleOutlined, MenuOutlined, PieChartOutlined,
  PlusOutlined, QrcodeOutlined, ReloadOutlined, TeamOutlined,
} from '@ant-design/icons'
import AppSidebar from '@/layouts/AppSidebar'
import { API, type Inbound } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'
import { InboundForm, type InboundFormValue } from '@/components/InboundForm'
import { Modal, Upload, Radio } from 'antd'
import { formatBytes } from '@/pages/index/formatters'
import InboundInfoModal from '@/pages/inbounds/InboundInfoModal'

// InboundsPage — 3x-ui-shaped list view. Top row has three stat tiles
// (upload / download / online), body is a Card wrapping an Ant Table
// with the same columns 3x-ui exposes plus a per-row actions dropdown.
//
// The dialog for create/edit reuses our existing InboundForm — it's
// the protocol-aware form we built earlier and matches 3x-ui's field
// set for VLESS/VMess/Trojan/etc.

const protocolColor: Record<string, string> = {
  vless:       'geekblue',
  vmess:       'blue',
  trojan:      'volcano',
  shadowsocks: 'purple',
  hysteria2:   'magenta',
  wireguard:   'cyan',
  http:        'gold',
  socks:       'orange',
  'dokodemo-door': 'default',
}

export default function InboundsPage() {
  const { isDark } = useTheme()
  const navigate = useNavigate()
  const [messageApi, contextHolder] = message.useMessage()

  const [rows, setRows] = useState<Inbound[]>([])
  const [selectedKeys, setSelectedKeys] = useState<number[]>([])
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<Inbound | null>(null)
  const [infoRow, setInfoRow] = useState<Inbound | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [importMode, setImportMode] = useState<'merge' | 'replace'>('merge')

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setRows((await API.inbounds.list()) ?? [])
    } catch (e) {
      messageApi.error(String(e))
    } finally { setLoading(false) }
  }, [messageApi])

  useEffect(() => { refresh() }, [refresh])

  const totals = useMemo(() => rows.reduce(
    (a, r) => ({ up: a.up + r.total_up, down: a.down + r.total_down }),
    { up: 0, down: 0 },
  ), [rows])

  const toggleEnable = useCallback(async (row: Inbound, next: boolean) => {
    try {
      await API.inbounds.update(row.id, {
        tag: row.tag, protocol: row.protocol, listen: row.listen, port: row.port,
        settings: row.settings, stream: row.stream, remark: row.remark,
        enabled: next,
      } as any)
      refresh()
    } catch (e) { messageApi.error(String(e)) }
  }, [refresh, messageApi])

  const rowMenu = (_row: Inbound): MenuProps['items'] => [
    { key: 'edit',   icon: <EditOutlined />,     label: 'แก้ไข' },
    { key: 'info',   icon: <InfoCircleOutlined />, label: 'รายละเอียด' },
    { key: 'qr',     icon: <QrcodeOutlined />,   label: 'QR / Share' },
    { key: 'clients', icon: <TeamOutlined />,    label: 'จัดการ clients' },
    { type: 'divider' },
    { key: 'reset',  icon: <ReloadOutlined />,   label: 'Reset traffic' },
    { key: 'delete', icon: <DeleteOutlined />,   label: 'ลบ', danger: true },
  ] as MenuProps['items']

  const onRowAction = useCallback(async ({ key }: { key: string }, row: Inbound) => {
    switch (key) {
      case 'edit': setEditing(row); setDialogOpen(true); break
      case 'clients': navigate(`/inbounds/${row.id}/clients`); break
      case 'delete':
        if (!confirm(`ลบ inbound "${row.tag}"?`)) return
        await API.inbounds.remove(row.id)
        messageApi.success('deleted')
        refresh()
        break
      case 'reset':
        if (!confirm(`Reset traffic ของ "${row.tag}" ให้เป็น 0?`)) return
        try {
          await API.inbounds.resetTraffic(row.id)
          messageApi.success('traffic reset')
          refresh()
        } catch (e) { messageApi.error(String(e)) }
        break
      case 'info':
      case 'qr':
        setInfoRow(row)
        break
    }
  }, [messageApi, refresh, navigate])

  const bulkDelete = useCallback(async () => {
    if (selectedKeys.length === 0) return
    if (!confirm(`ลบ inbound ${selectedKeys.length} รายการ?`)) return
    await Promise.all(selectedKeys.map((id) => API.inbounds.remove(id)))
    messageApi.success(`deleted ${selectedKeys.length}`)
    setSelectedKeys([])
    refresh()
  }, [selectedKeys, messageApi, refresh])

  const columns: TableColumnsType<Inbound> = useMemo(() => [
    {
      title: '#', dataIndex: 'id', width: 72, fixed: 'left',
    },
    {
      title: 'Enable', dataIndex: 'enabled', width: 90, align: 'center',
      render: (v: boolean, row) => (
        <Switch checked={v} onChange={(next) => toggleEnable(row, next)} />
      ),
    },
    {
      title: 'Remark', dataIndex: 'tag',
      render: (_: unknown, row) => (
        <Space direction="vertical" size={2}>
          <span style={{ fontWeight: 500 }}>{row.tag}</span>
          {row.remark && <span style={{ fontSize: 12, opacity: 0.65 }}>{row.remark}</span>}
        </Space>
      ),
    },
    {
      title: 'Protocol', dataIndex: 'protocol', width: 120,
      render: (v: string) => <Tag color={protocolColor[v] || 'default'}>{v}</Tag>,
    },
    {
      title: 'Port', dataIndex: 'port', width: 110,
      render: (v: number, row) => (
        <span style={{ fontVariantNumeric: 'tabular-nums' }}>
          {row.listen === '0.0.0.0' ? '' : `${row.listen}:`}{v}
        </span>
      ),
    },
    {
      title: 'Traffic ↑↓', width: 200,
      render: (_: unknown, row) => (
        <Space direction="vertical" size={0}>
          <span><ArrowUpOutlined style={{ color: '#5cadff' }} /> {formatBytes(row.total_up)}</span>
          <span><ArrowDownOutlined style={{ color: '#52c41a' }} /> {formatBytes(row.total_down)}</span>
        </Space>
      ),
    },
    {
      title: 'Created', dataIndex: 'created_at', width: 140,
      render: (v: number) => v ? new Date(v * 1000).toLocaleDateString() : '—',
    },
    {
      title: '', width: 60, fixed: 'right', align: 'center',
      render: (_: unknown, row) => (
        <Dropdown menu={{ items: rowMenu(row), onClick: (info) => onRowAction(info, row) }}
                  trigger={['click']} placement="bottomRight">
          <Button size="small" icon={<MenuOutlined />} />
        </Dropdown>
      ),
    },
  ], [onRowAction, toggleEnable])

  const doExport = useCallback(() => {
    // Full-page navigate so the browser downloads the file with the
    // Content-Disposition header the server sets.
    window.location.href = API.inbounds.exportURL()
  }, [])

  const doResetAll = useCallback(async () => {
    if (!confirm(`Reset traffic ของ ${rows.length} inbounds ทั้งหมด?`)) return
    try {
      const r = await API.inbounds.resetAllTraffic()
      messageApi.success(`reset ${r.count} inbounds`)
      if (Object.keys(r.failed).length) {
        messageApi.warning(`${Object.keys(r.failed).length} failed`)
      }
      refresh()
    } catch (e) { messageApi.error(String(e)) }
  }, [rows.length, messageApi, refresh])

  const generalActions: MenuProps = {
    items: [
      { key: 'import', icon: <ImportOutlined />, label: 'Import inbounds…' },
      { key: 'export', icon: <ExportOutlined />, label: 'Export JSON' },
      { type: 'divider' },
      { key: 'reset',  icon: <ReloadOutlined />, label: 'Reset all traffic', danger: true },
    ],
    onClick: ({ key }) => {
      if (key === 'import') setImportOpen(true)
      if (key === 'export') doExport()
      if (key === 'reset') doResetAll()
    },
  }

  async function handleSave(v: InboundFormValue) {
    try {
      if (editing) await API.inbounds.update(editing.id, v)
      else await API.inbounds.create(v)
      messageApi.success(editing ? 'updated' : 'created')
      setDialogOpen(false)
      refresh()
    } catch (e) { messageApi.error(String(e)) }
  }

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
                <Card><Statistic
                  title="Total upload"
                  value={formatBytes(totals.up)}
                  prefix={<ArrowUpOutlined style={{ color: '#5cadff' }} />}
                /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic
                  title="Total download"
                  value={formatBytes(totals.down)}
                  prefix={<ArrowDownOutlined style={{ color: '#52c41a' }} />}
                /></Card>
              </Col>
              <Col xs={24} md={8}>
                <Card><Statistic
                  title="Inbounds"
                  value={`${rows.filter((r) => r.enabled).length} / ${rows.length}`}
                  prefix={<PieChartOutlined />}
                /></Card>
              </Col>

              <Col span={24}>
                <Card
                  hoverable
                  title={
                    <Space>
                      <Button type="primary" icon={<PlusOutlined />}
                              onClick={() => { setEditing(null); setDialogOpen(true) }}>
                        Add inbound
                      </Button>
                      <Dropdown trigger={['click']} menu={generalActions}>
                        <Button icon={<MenuOutlined />}>Actions</Button>
                      </Dropdown>
                      {selectedKeys.length > 0 && (
                        <>
                          <Tag color="blue" closable onClose={() => setSelectedKeys([])}
                               style={{ marginInlineEnd: 0 }}>
                            {selectedKeys.length} selected
                          </Tag>
                          <Button danger icon={<DeleteOutlined />} onClick={bulkDelete}>
                            Delete
                          </Button>
                        </>
                      )}
                    </Space>
                  }
                >
                  <Table<Inbound>
                    rowKey="id"
                    size="middle"
                    loading={loading}
                    columns={columns}
                    dataSource={rows}
                    rowSelection={{
                      selectedRowKeys: selectedKeys,
                      onChange: (keys) => setSelectedKeys(keys as number[]),
                    }}
                    pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
                    scroll={{ x: 900 }}
                  />
                </Card>
              </Col>
            </Row>
          </Layout.Content>
        </Layout>
      </Layout>

      <Modal
        open={dialogOpen}
        onCancel={() => setDialogOpen(false)}
        title={editing ? `Edit ${editing.tag}` : 'New inbound'}
        footer={null}
        width={760}
        destroyOnHidden
      >
        {/* keyed remount is a belt on top of destroyOnHidden's braces
            so switching from create → edit → different-edit always
            gets a clean form even if antd's animation defers unmount. */}
        <InboundForm
          key={editing?.id ?? 'new'}
          initial={editing ?? undefined}
          onCancel={() => setDialogOpen(false)}
          onSubmit={handleSave}
        />
      </Modal>

      <InboundInfoModal
        open={!!infoRow}
        inbound={infoRow}
        onClose={() => setInfoRow(null)}
      />

      <Modal
        open={importOpen}
        onCancel={() => setImportOpen(false)}
        title="Import inbounds"
        footer={null}
        width={520}
        destroyOnHidden
      >
        <div style={{ marginBottom: 12 }}>
          Upload a JSON file exported from a HEXPLUS panel
          (or a copy-paste of the JSON payload).
        </div>
        <Radio.Group value={importMode} onChange={(e) => setImportMode(e.target.value)}
                     style={{ marginBottom: 12 }}>
          <Radio.Button value="merge">Skip conflicts</Radio.Button>
          <Radio.Button value="replace">Replace conflicts</Radio.Button>
        </Radio.Group>
        <Upload.Dragger
          accept="application/json,.json"
          multiple={false}
          maxCount={1}
          showUploadList={false}
          beforeUpload={async (file) => {
            try {
              const text = await file.text()
              const env = JSON.parse(text)
              const r = await API.inbounds.import(env, importMode)
              messageApi.success(`imported ${r.inserted}, skipped ${r.skipped}`)
              if (Object.keys(r.failed).length) {
                messageApi.warning(`${Object.keys(r.failed).length} failed`)
              }
              setImportOpen(false)
              refresh()
            } catch (e) { messageApi.error(String(e)) }
            return false
          }}
        >
          <p style={{ fontSize: 32, margin: 8 }}><ImportOutlined /></p>
          <p>Click or drag a .json file to import</p>
          <p style={{ opacity: 0.6, fontSize: 12 }}>
            Envelope: <code>{'{ version, inbounds: [...] }'}</code>
          </p>
        </Upload.Dragger>
      </Modal>
    </ConfigProvider>
  )
}
