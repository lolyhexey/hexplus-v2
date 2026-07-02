import { ConfigProvider, Layout, Result, Space, Tag, theme as antdTheme } from 'antd'
import AppSidebar from '@/layouts/AppSidebar'
import { useTheme } from '@/hooks/useTheme'
import type { ReactNode } from 'react'

// StubPage — placeholder used by pages listed in AppSidebar that 3x-ui
// ships but our backend doesn't host yet. Keeps the sider navigation
// looking complete without shipping half-working pages.

export function StubPage({
  title, subTitle, extra, phase,
}: {
  title: string
  subTitle: string
  extra?: ReactNode
  phase?: string
}) {
  const { isDark } = useTheme()
  return (
    <ConfigProvider theme={{
      algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    }}>
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout>
          <Layout.Content style={{ padding: 16 }}>
            <Result
              status="info"
              title={
                <Space>
                  <span>{title}</span>
                  <Tag color="blue">Coming soon</Tag>
                  {phase && <Tag>{phase}</Tag>}
                </Space>
              }
              subTitle={subTitle}
              extra={extra}
            />
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  )
}
