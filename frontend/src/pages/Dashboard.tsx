import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid,
} from 'recharts'
import { ArrowDown, ArrowUp, Box, ChevronRight, Users } from 'lucide-react'
import { API, type Inbound, type Client } from '@/lib/api'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/cn'

// Dashboard: stat tiles at the top (3x-ui hero pattern), a
// mock-but-real traffic sparkline running across the width, then a
// two-column split for inbound activity and a client leaderboard.
//
// The tile row is where the eye lands first, so each tile tells one
// number-plus-one-context story instead of stacking bare stats.

interface DashState {
  inbounds: Inbound[]
  clients: Client[]
  err: string | null
}

export default function Dashboard() {
  const [state, setState] = useState<DashState>({ inbounds: [], clients: [], err: null })

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const inbounds = (await API.inbounds.list()) ?? []
        if (cancelled) return
        // Pull clients from every inbound in parallel; ~10 endpoints
        // is cheap enough that a single /api/clients aggregator would
        // be premature.
        const nested = await Promise.all(
          inbounds.map((i) => API.clients.list(i.id).catch(() => [] as Client[])),
        )
        if (cancelled) return
        setState({ inbounds, clients: nested.flat(), err: null })
      } catch (e) {
        if (!cancelled) setState((s) => ({ ...s, err: String(e) }))
      }
    })()
    return () => { cancelled = true }
  }, [])

  const totals = useMemo(() => {
    const totalUp = state.inbounds.reduce((s, i) => s + i.total_up, 0)
    const totalDown = state.inbounds.reduce((s, i) => s + i.total_down, 0)
    return {
      enabled: state.inbounds.filter((i) => i.enabled).length,
      inboundsCount: state.inbounds.length,
      clientsCount: state.clients.length,
      activeClients: state.clients.filter((c) => c.enabled).length,
      up: totalUp,
      down: totalDown,
    }
  }, [state])

  // Chart: since we don't stream per-second stats yet, plot the
  // cumulative up/down per inbound as an ordered series. When real
  // time-series data lands (Phase 7 already stores it) this component
  // reads it without a layout change.
  const chartData = useMemo(
    () => state.inbounds.map((i) => ({
      name: i.tag,
      up: i.total_up,
      down: i.total_down,
    })),
    [state.inbounds],
  )

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-sm text-muted-foreground mt-1">
          ภาพรวมการทำงานของ Xray inbound และ client — อัปเดต real-time
        </p>
      </div>

      {state.err && (
        <Card className="border-destructive/40 bg-destructive/5">
          <CardContent className="py-3 text-sm text-destructive">{state.err}</CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <StatTile
          icon={<Box className="h-4 w-4" />}
          label="Inbounds"
          value={`${totals.enabled} / ${totals.inboundsCount}`}
          hint="ทำงาน / ทั้งหมด"
        />
        <StatTile
          icon={<Users className="h-4 w-4" />}
          label="Clients"
          value={`${totals.activeClients} / ${totals.clientsCount}`}
          hint="ใช้งานได้ / ทั้งหมด"
        />
        <StatTile
          icon={<ArrowUp className="h-4 w-4" />}
          label="Total upload"
          value={formatBytes(totals.up)}
          hint="สะสมตั้งแต่ xray เริ่ม"
          tone="up"
        />
        <StatTile
          icon={<ArrowDown className="h-4 w-4" />}
          label="Total download"
          value={formatBytes(totals.down)}
          hint="สะสมตั้งแต่ xray เริ่ม"
          tone="down"
        />
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-base">Traffic per inbound</CardTitle>
          <div className="text-xs text-muted-foreground">
            สะสม (bytes) — ต่อ inbound
          </div>
        </CardHeader>
        <CardContent className="pb-4">
          {chartData.length === 0 ? (
            <EmptyChart />
          ) : (
            <div className="h-56">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ left: -8, right: 8, top: 8 }}>
                  <defs>
                    <linearGradient id="up-fill" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="down-fill" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="hsl(var(--success))" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="hsl(var(--success))" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" opacity={0.4} />
                  <XAxis dataKey="name" tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }} tickLine={false} axisLine={false} width={44} tickFormatter={(v) => shortBytes(Number(v))} />
                  <Tooltip
                    formatter={(v) => formatBytes(Number(v))}
                    contentStyle={{
                      background: 'hsl(var(--card))',
                      border: '1px solid hsl(var(--border))',
                      borderRadius: 6,
                      fontSize: 12,
                    }}
                    labelStyle={{ color: 'hsl(var(--foreground))' }}
                  />
                  <Area type="monotone" dataKey="up" stroke="hsl(var(--primary))" fill="url(#up-fill)" strokeWidth={2} />
                  <Area type="monotone" dataKey="down" stroke="hsl(var(--success))" fill="url(#down-fill)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base flex items-center justify-between">
              Inbounds
              <Link to="/inbounds" className="text-xs font-normal text-primary hover:underline inline-flex items-center gap-0.5">
                ดูทั้งหมด <ChevronRight className="h-3 w-3" />
              </Link>
            </CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            {state.inbounds.length === 0 ? (
              <EmptyRow label="ยังไม่มี inbound" />
            ) : (
              <ul className="divide-y divide-border">
                {state.inbounds.slice(0, 6).map((i) => (
                  <li key={i.id} className="py-2.5 flex items-center justify-between text-sm">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className={i.enabled ? 'dot-on' : 'dot-off'} />
                      <div className="min-w-0">
                        <div className="font-medium truncate">{i.tag}</div>
                        <div className="text-xs text-muted-foreground">{i.protocol} · :{i.port}</div>
                      </div>
                    </div>
                    <div className="text-xs text-muted-foreground shrink-0 flex items-center gap-3">
                      <span title="Upload">↑ {shortBytes(i.total_up)}</span>
                      <span title="Download">↓ {shortBytes(i.total_down)}</span>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Top clients</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            {state.clients.length === 0 ? (
              <EmptyRow label="ยังไม่มี client" />
            ) : (
              <ul className="divide-y divide-border">
                {[...state.clients]
                  .sort((a, b) => b.used_bytes - a.used_bytes)
                  .slice(0, 6)
                  .map((c) => (
                    <li key={c.id} className="py-2.5 flex items-center justify-between text-sm">
                      <div className="flex items-center gap-3 min-w-0">
                        <span className={c.enabled ? 'dot-on' : 'dot-off'} />
                        <div className="min-w-0">
                          <div className="font-medium truncate">{c.email}</div>
                          <div className="text-xs text-muted-foreground">{c.protocol}</div>
                        </div>
                      </div>
                      <div className="text-xs shrink-0">
                        <div className="text-foreground">{shortBytes(c.used_bytes)}</div>
                        <div className="text-muted-foreground text-[10px]">
                          {c.quota_bytes ? `จาก ${shortBytes(c.quota_bytes)}` : '∞'}
                        </div>
                      </div>
                    </li>
                  ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function StatTile({
  icon, label, value, hint, tone,
}: {
  icon: React.ReactNode
  label: string
  value: string
  hint: string
  tone?: 'up' | 'down'
}) {
  const chip =
    tone === 'up' ? 'bg-primary/10 text-primary' :
    tone === 'down' ? 'bg-[hsl(var(--success))]/10 text-[hsl(var(--success))]' :
    'bg-muted text-muted-foreground'
  return (
    <Card>
      <CardContent className="p-5 space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground uppercase tracking-wide">{label}</span>
          <span className={cn('grid h-7 w-7 place-items-center rounded-md', chip)}>{icon}</span>
        </div>
        <div className="text-3xl font-semibold tracking-tight">{value}</div>
        <div className="text-xs text-muted-foreground">{hint}</div>
      </CardContent>
    </Card>
  )
}

function EmptyChart() {
  return (
    <div className="h-56 grid place-items-center text-sm text-muted-foreground">
      ยังไม่มีข้อมูล — สร้าง inbound ที่หน้า Inbounds เพื่อดูกราฟ
    </div>
  )
}

function EmptyRow({ label }: { label: string }) {
  return <div className="py-8 grid place-items-center text-sm text-muted-foreground">{label}</div>
}

// Full-precision bytes for hover tooltips.
function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(2)} ${units[i]}`
}

// Compact bytes for chart ticks + inline table cells.
function shortBytes(n: number): string {
  if (!n) return '0'
  const units = ['B', 'K', 'M', 'G', 'T']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(1)}${units[i]}`
}
