import { useEffect, useState } from 'react'

// useTheme — thin light/dark toggle, persisted in localStorage.
// 3x-ui has a 3-state theme (light / dark / ultra); we ship the two
// most common ones since the ultra tokens require a separate palette
// and the frontend still reads clean in either state.

const THEME_KEY = 'hexplus-theme'

export function useTheme() {
  const [isDark, setIsDark] = useState<boolean>(
    () => (localStorage.getItem(THEME_KEY) as 'light' | 'dark' | null) !== 'light',
  )

  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDark)
    localStorage.setItem(THEME_KEY, isDark ? 'dark' : 'light')
  }, [isDark])

  return {
    isDark,
    toggle: () => setIsDark((d) => !d),
    // antd theme config — imported by pages that wrap their content
    // in <ConfigProvider theme={antdThemeConfig}>.
    antdThemeConfig: {
      algorithm: isDark ? 'darkAlgorithm' as const : 'defaultAlgorithm' as const,
      token: { colorPrimary: '#1677ff', borderRadius: 6 },
    },
  }
}
