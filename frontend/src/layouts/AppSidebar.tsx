import { useCallback, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Layout, Menu } from 'antd'
import type { MenuProps } from 'antd'
import {
  ApiOutlined,
  ClusterOutlined,
  DashboardOutlined,
  ExportOutlined,
  GlobalOutlined,
  ImportOutlined,
  LogoutOutlined,
  MoonOutlined,
  SafetyOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  SunOutlined,
  SwapOutlined,
  TagsOutlined,
  TeamOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { API } from '@/lib/api'
import { useTheme } from '@/hooks/useTheme'

// AppSidebar — modelled after 3x-ui/frontend/src/layouts/AppSidebar.tsx.
// Same menu skeleton, same icons; sub-pages that we do not host yet
// (groups, nodes, hosts, api-docs) stay in the menu as "coming soon"
// so the sider looks the same and menu-order muscle memory carries over.

const SIDEBAR_COLLAPSED_KEY = 'isSidebarCollapsed'
const LOGOUT_KEY = '__logout__'

function readCollapsed(): boolean {
  try { return JSON.parse(localStorage.getItem(SIDEBAR_COLLAPSED_KEY) || 'false') }
  catch { return false }
}

export default function AppSidebar() {
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { isDark, toggle } = useTheme()
  const [collapsed, setCollapsed] = useState<boolean>(() => readCollapsed())

  // Menu entries mirror 3x-ui's `tabs` array 1:1 so the visual order
  // is identical. Sub-menus (Settings, Xray) get their own children.
  const items = useMemo<MenuProps['items']>(() => [
    { key: '/', icon: <DashboardOutlined />, label: 'Dashboard' },
    { key: '/inbounds', icon: <ImportOutlined />, label: 'Inbounds' },
    { key: '/clients', icon: <TeamOutlined />, label: 'Clients' },
    { key: '/groups', icon: <TagsOutlined />, label: 'Groups' },
    { key: '/nodes', icon: <ClusterOutlined />, label: 'Nodes' },
    { key: '/hosts', icon: <GlobalOutlined />, label: 'Hosts' },
    { key: '/outbound', icon: <ExportOutlined />, label: 'Outbounds' },
    { key: '/routing', icon: <SwapOutlined />, label: 'Routing' },
    {
      key: '/settings',
      icon: <SettingOutlined />,
      label: 'Settings',
      children: [
        { key: '/settings#general',      icon: <SettingOutlined />, label: 'Panel Settings' },
        { key: '/settings#security',     icon: <SafetyOutlined />,  label: 'Security' },
        { key: '/settings#subscription', icon: <ApiOutlined />,     label: 'Subscription' },
      ],
    },
    { key: '/xray', icon: <ToolOutlined />, label: 'Xray' },
    { key: '/certs', icon: <SafetyCertificateOutlined />, label: 'Certificates' },
    { key: '/api-docs', icon: <ApiOutlined />, label: 'API Docs' },
    { key: LOGOUT_KEY, icon: <LogoutOutlined />, label: 'Log out' },
  ], [])

  const selectedKey = pathname === '' ? '/' : pathname

  const onClick = useCallback<NonNullable<MenuProps['onClick']>>(async ({ key }) => {
    if (key === LOGOUT_KEY) {
      try { await API.logout() } catch {}
      const base = (document.getElementById('hexplus-base') as HTMLBaseElement | null)?.href
        || window.location.origin + '/'
      window.location.href = base + 'login'
      return
    }
    navigate(String(key))
  }, [navigate])

  return (
    <Layout.Sider
      collapsible
      breakpoint="lg"
      collapsed={collapsed}
      onCollapse={(v, type) => {
        if (type === 'clickTrigger') {
          localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(v))
          setCollapsed(v)
        }
      }}
      style={{ background: isDark ? '#001529' : '#001529' }}
      width={220}
    >
      <div style={{
        height: 56,
        display: 'flex',
        alignItems: 'center',
        justifyContent: collapsed ? 'center' : 'flex-start',
        padding: collapsed ? 0 : '0 16px',
        color: '#fff',
        borderBottom: '1px solid rgba(255,255,255,.06)',
      }}>
        <div style={{
          display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
          width: 32, height: 32, borderRadius: 6,
          background: '#1677ff', color: '#fff', fontWeight: 700, marginRight: collapsed ? 0 : 10,
        }}>H</div>
        {!collapsed && <div>
          <div style={{ fontSize: 14, fontWeight: 600, lineHeight: '18px' }}>HEXPLUS</div>
          <div style={{ fontSize: 11, color: 'rgba(255,255,255,.55)' }}>V2Ray Panel</div>
        </div>}
      </div>

      <Menu
        theme="dark"
        mode="inline"
        selectedKeys={[selectedKey]}
        items={items}
        onClick={onClick}
        style={{ background: '#001529', borderInlineEnd: 'none' }}
      />

      {!collapsed && (
        <div style={{
          position: 'absolute', bottom: 12, left: 12, right: 12,
          color: 'rgba(255,255,255,.55)', fontSize: 12,
        }}>
          <button
            type="button"
            onClick={toggle}
            style={{
              width: '100%',
              display: 'inline-flex', alignItems: 'center', gap: 8,
              justifyContent: 'center',
              padding: '6px 10px', borderRadius: 6,
              background: 'rgba(255,255,255,.06)',
              color: '#fff', border: 'none', cursor: 'pointer',
            }}
          >
            {isDark ? <SunOutlined /> : <MoonOutlined />}
            {isDark ? 'Light mode' : 'Dark mode'}
          </button>
        </div>
      )}
    </Layout.Sider>
  )
}
