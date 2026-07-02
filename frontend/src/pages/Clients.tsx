import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { API, type Client } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { Table, THead, TBody, TR, TH, TD } from '@/components/ui/table'

// Clients page: nested under /inbounds/:id/clients. Actions: create,
// edit, toggle, reset traffic, extend expiry, show share link + QR.
export default function Clients() {
  const { id } = useParams<{ id: string }>()
  const inboundID = Number(id)
  const [rows, setRows] = useState<Client[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [dlgOpen, setDlgOpen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  const [shareData, setShareData] = useState<{ link: string; qrURL: string } | null>(null)
  const [form, setForm] = useState({ email: '', quota_gb: 0, expire_days: 0, ip_limit: 0 })

  async function refresh() {
    try { setRows(await API.clients.list(inboundID)) }
    catch (e) { setErr(String(e)) }
  }
  useEffect(() => { refresh() }, [inboundID])

  async function create() {
    try {
      await API.clients.create(inboundID, {
        email: form.email,
        quota_bytes: form.quota_gb * 1024 * 1024 * 1024,
        expires_at: form.expire_days > 0 ? Math.floor(Date.now() / 1000) + form.expire_days * 86400 : 0,
        ip_limit: form.ip_limit,
      })
      setDlgOpen(false)
      setForm({ email: '', quota_gb: 0, expire_days: 0, ip_limit: 0 })
      await refresh()
    } catch (e) { setErr(String(e)) }
  }

  async function showShare(c: Client) {
    try {
      const { link } = await API.clients.link(c.id)
      setShareData({ link, qrURL: API.clients.qrURL(c.id) })
      setShareOpen(true)
    } catch (e) { setErr(String(e)) }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Clients (inbound #{inboundID})</h1>
        <Button onClick={() => setDlgOpen(true)}>+ เพิ่ม client</Button>
      </div>
      {err && <div className="text-sm text-destructive">{err}</div>}

      <Card>
        <CardHeader><CardTitle>ทั้งหมด ({rows.length})</CardTitle></CardHeader>
        <CardContent>
          <Table>
            <THead>
              <TR>
                <TH>Email</TH><TH>ใช้/โควตา</TH><TH>หมดอายุ</TH><TH>สถานะ</TH><TH></TH>
              </TR>
            </THead>
            <TBody>
              {rows.map((c) => (
                <TR key={c.id}>
                  <TD className="font-medium">{c.email}</TD>
                  <TD className="text-xs text-muted-foreground">
                    {formatBytes(c.used_bytes)} / {c.quota_bytes ? formatBytes(c.quota_bytes) : '∞'}
                  </TD>
                  <TD className="text-xs">{c.expires_at ? new Date(c.expires_at * 1000).toLocaleDateString('th-TH') : '∞'}</TD>
                  <TD>{c.enabled ? <span className="text-green-600">●</span> : <span className="text-muted-foreground">○</span>}</TD>
                  <TD className="text-right space-x-1">
                    <Button size="sm" variant="secondary" onClick={() => showShare(c)}>Link</Button>
                    <Button size="sm" variant="outline" onClick={() => API.clients.toggle(c.id).then(refresh)}>toggle</Button>
                    <Button size="sm" variant="outline" onClick={() => API.clients.reset(c.id).then(refresh)}>reset</Button>
                    <Button size="sm" variant="outline" onClick={() => {
                      const days = Number(prompt('เพิ่มกี่วัน?', '30') ?? 0)
                      if (days) API.clients.extend(c.id, days).then(refresh)
                    }}>+วัน</Button>
                    <Button size="sm" variant="destructive" onClick={() => {
                      if (confirm('ลบ client?')) API.clients.remove(c.id).then(refresh)
                    }}>ลบ</Button>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog
        open={dlgOpen}
        onClose={() => setDlgOpen(false)}
        title="เพิ่ม Client"
        footer={
          <>
            <Button variant="outline" onClick={() => setDlgOpen(false)}>ยกเลิก</Button>
            <Button onClick={create}>สร้าง</Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="space-y-1"><Label>Email / ชื่อ</Label>
            <Input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></div>
          <div className="space-y-1"><Label>โควตา (GB, 0 = ∞)</Label>
            <Input type="number" value={form.quota_gb} onChange={(e) => setForm({ ...form, quota_gb: Number(e.target.value) })} /></div>
          <div className="space-y-1"><Label>หมดอายุใน N วัน (0 = ∞)</Label>
            <Input type="number" value={form.expire_days} onChange={(e) => setForm({ ...form, expire_days: Number(e.target.value) })} /></div>
          <div className="space-y-1"><Label>จำกัด IP พร้อมกัน (0 = ∞)</Label>
            <Input type="number" value={form.ip_limit} onChange={(e) => setForm({ ...form, ip_limit: Number(e.target.value) })} /></div>
        </div>
      </Dialog>

      <Dialog open={shareOpen} onClose={() => setShareOpen(false)} title="Share link"
              footer={<Button onClick={() => setShareOpen(false)}>ปิด</Button>}>
        {shareData && (
          <div className="space-y-3">
            <div className="flex justify-center">
              <img src={shareData.qrURL} alt="QR" className="rounded-md border" />
            </div>
            <div className="space-y-1">
              <Label>Link</Label>
              <textarea readOnly value={shareData.link}
                        className="w-full h-24 rounded-md border border-input bg-transparent px-3 py-2 text-xs font-mono" />
              <Button size="sm" variant="secondary" onClick={() => navigator.clipboard.writeText(shareData.link)}>คัดลอก</Button>
            </div>
          </div>
        )}
      </Dialog>
    </div>
  )
}

function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}
