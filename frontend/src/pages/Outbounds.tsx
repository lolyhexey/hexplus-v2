import { useEffect, useState } from 'react'
import { API, type Outbound } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, THead, TBody, TR, TH, TD } from '@/components/ui/table'

// Outbounds: list + one-click WARP provisioning. Custom outbound
// creation (raw JSON) is intentionally read-only here for MVP; users
// wanting an arbitrary outbound POST directly to /api/outbounds for now.
export default function Outbounds() {
  const [rows, setRows] = useState<Outbound[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function refresh() {
    try { setRows(await API.outbounds.list()) }
    catch (e) { setErr(String(e)) }
  }
  useEffect(() => { refresh() }, [])

  async function provisionWARP() {
    setBusy(true); setErr(null)
    try {
      await API.outbounds.provisionWARP('warp')
      await refresh()
    } catch (e) { setErr(String(e)) }
    finally { setBusy(false) }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Outbounds</h1>
        <Button onClick={provisionWARP} disabled={busy}>
          {busy ? 'กำลังลงทะเบียน...' : '+ Cloudflare WARP'}
        </Button>
      </div>
      {err && <div className="text-sm text-destructive">{err}</div>}
      <Card>
        <CardHeader><CardTitle>ทั้งหมด ({rows.length})</CardTitle></CardHeader>
        <CardContent>
          <Table>
            <THead><TR><TH>Tag</TH><TH>Protocol</TH><TH>Remark</TH><TH></TH></TR></THead>
            <TBody>
              {rows.map((r) => (
                <TR key={r.id}>
                  <TD className="font-medium">{r.tag}</TD>
                  <TD>{r.protocol}</TD>
                  <TD className="text-muted-foreground text-xs">{r.remark}</TD>
                  <TD className="text-right">
                    <Button size="sm" variant="destructive"
                            onClick={() => { if (confirm('ลบ?')) API.outbounds.remove(r.id).then(refresh) }}>
                      ลบ
                    </Button>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
