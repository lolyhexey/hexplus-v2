import { useEffect, useState } from 'react'
import { API, type Rule } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { Table, THead, TBody, TR, TH, TD } from '@/components/ui/table'

export default function Routing() {
  const [rows, setRows] = useState<Rule[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [dlgOpen, setDlgOpen] = useState(false)
  const [form, setForm] = useState({
    priority: 100, outbound_tag: 'direct', inbound_tag: '',
    domains: '', ips: '', remark: '',
  })

  async function refresh() {
    try { setRows(await API.rules.list()) }
    catch (e) { setErr(String(e)) }
  }
  useEffect(() => { refresh() }, [])

  async function create() {
    try {
      await API.rules.create({
        priority: form.priority,
        outbound_tag: form.outbound_tag,
        inbound_tag: form.inbound_tag,
        domains: form.domains.split('\n').filter(Boolean),
        ips: form.ips.split('\n').filter(Boolean),
        remark: form.remark,
      })
      setDlgOpen(false)
      setForm({ priority: 100, outbound_tag: 'direct', inbound_tag: '', domains: '', ips: '', remark: '' })
      await refresh()
    } catch (e) { setErr(String(e)) }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Routing rules</h1>
        <Button onClick={() => setDlgOpen(true)}>+ กฎใหม่</Button>
      </div>
      {err && <div className="text-sm text-destructive">{err}</div>}
      <Card>
        <CardHeader><CardTitle>ทั้งหมด ({rows.length})</CardTitle></CardHeader>
        <CardContent>
          <Table>
            <THead><TR><TH>Pri</TH><TH>Outbound</TH><TH>Domains</TH><TH>IPs</TH><TH>Remark</TH><TH></TH></TR></THead>
            <TBody>
              {rows.map((r) => (
                <TR key={r.id}>
                  <TD>{r.priority}</TD>
                  <TD className="font-medium">{r.outbound_tag}</TD>
                  <TD className="text-xs text-muted-foreground">{r.domains.slice(0, 3).join(', ')}{r.domains.length > 3 ? '…' : ''}</TD>
                  <TD className="text-xs text-muted-foreground">{r.ips.slice(0, 3).join(', ')}{r.ips.length > 3 ? '…' : ''}</TD>
                  <TD className="text-xs">{r.remark}</TD>
                  <TD className="text-right">
                    <Button size="sm" variant="destructive"
                            onClick={() => { if (confirm('ลบ?')) API.rules.remove(r.id).then(refresh) }}>
                      ลบ
                    </Button>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={dlgOpen} onClose={() => setDlgOpen(false)} title="กฎใหม่" wide
              footer={<>
                <Button variant="outline" onClick={() => setDlgOpen(false)}>ยกเลิก</Button>
                <Button onClick={create}>สร้าง</Button>
              </>}>
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1"><Label>Priority</Label>
            <Input type="number" value={form.priority} onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })} /></div>
          <div className="space-y-1"><Label>Outbound tag</Label>
            <Input value={form.outbound_tag} onChange={(e) => setForm({ ...form, outbound_tag: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>Inbound tag (optional)</Label>
            <Input value={form.inbound_tag} onChange={(e) => setForm({ ...form, inbound_tag: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>Domains (บรรทัดละหนึ่ง)</Label>
            <textarea className="w-full h-24 rounded-md border border-input bg-transparent px-3 py-2 text-sm font-mono"
                      value={form.domains}
                      onChange={(e) => setForm({ ...form, domains: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>IPs (บรรทัดละหนึ่ง)</Label>
            <textarea className="w-full h-24 rounded-md border border-input bg-transparent px-3 py-2 text-sm font-mono"
                      value={form.ips}
                      onChange={(e) => setForm({ ...form, ips: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>Remark</Label>
            <Input value={form.remark} onChange={(e) => setForm({ ...form, remark: e.target.value })} /></div>
        </div>
      </Dialog>
    </div>
  )
}
