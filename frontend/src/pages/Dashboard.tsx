import { useEffect, useState } from 'react'
import { API, type Inbound } from '@/lib/api'
import { Card, CardHeader, CardTitle, CardContent, CardDescription } from '@/components/ui/card'

// Dashboard: quick counts + traffic totals. Pulls inbounds and rolls
// them up on the client — cheap enough for anything short of thousands
// of rows, and avoids adding a dedicated /api/stats endpoint just for
// three tiles.
export default function Dashboard() {
  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    API.inbounds.list().then(setInbounds).catch((e) => setErr(String(e)))
  }, [])

  const enabled = inbounds.filter((i) => i.enabled).length
  const totalUp = inbounds.reduce((s, i) => s + i.total_up, 0)
  const totalDown = inbounds.reduce((s, i) => s + i.total_down, 0)

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">ภาพรวม</h1>
      {err && <div className="text-sm text-destructive">{err}</div>}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Inbounds ทำงาน</CardDescription>
            <CardTitle className="text-3xl">{enabled} / {inbounds.length}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>รวม Upload</CardDescription>
            <CardTitle className="text-3xl">{formatBytes(totalUp)}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>รวม Download</CardDescription>
            <CardTitle className="text-3xl">{formatBytes(totalDown)}</CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Inbounds ทั้งหมด</CardTitle>
        </CardHeader>
        <CardContent className="text-sm">
          {inbounds.length === 0 ? (
            <div className="text-muted-foreground">ยังไม่มี inbound — สร้างที่หน้า Inbounds</div>
          ) : (
            <ul className="divide-y">
              {inbounds.map((i) => (
                <li key={i.id} className="flex items-center justify-between py-2">
                  <div>
                    <span className={i.enabled ? 'text-green-600' : 'text-muted-foreground'}>●</span>{' '}
                    <span className="font-medium">{i.tag}</span>{' '}
                    <span className="text-muted-foreground">({i.protocol}:{i.port})</span>
                  </div>
                  <div className="text-muted-foreground text-xs">
                    ↑ {formatBytes(i.total_up)} · ↓ {formatBytes(i.total_down)}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// formatBytes returns a human-friendly size like "12.3 MB". Kept in
// this file since it's only used on Dashboard; move to lib/ if a
// second page needs it.
function formatBytes(n: number): string {
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}
