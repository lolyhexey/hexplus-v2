import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { API, type Inbound } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { Table, THead, TBody, TR, TH, TD } from '@/components/ui/table'

// Inbounds page: full list + create + delete. Editing an existing
// inbound reuses the same dialog with pre-filled values.
const protocols = [
  'vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2',
  'wireguard', 'http', 'socks', 'dokodemo-door',
]

export default function Inbounds() {
  const [rows, setRows] = useState<Inbound[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [dlgOpen, setDlgOpen] = useState(false)
  const [editing, setEditing] = useState<Inbound | null>(null)
  const navigate = useNavigate()

  const [form, setForm] = useState({
    tag: '', protocol: 'vless', listen: '0.0.0.0', port: 443, remark: '',
    settings: '{}', stream: '{"network":"tcp","security":"reality"}',
  })

  async function refresh() {
    try { setRows(await API.inbounds.list()) }
    catch (e) { setErr(String(e)) }
  }
  useEffect(() => { refresh() }, [])

  function openCreate() {
    setEditing(null)
    setForm({ tag: '', protocol: 'vless', listen: '0.0.0.0', port: 443, remark: '', settings: '{}', stream: '{"network":"tcp","security":"reality"}' })
    setDlgOpen(true)
  }

  function openEdit(row: Inbound) {
    setEditing(row)
    setForm({
      tag: row.tag,
      protocol: row.protocol,
      listen: row.listen,
      port: row.port,
      remark: row.remark,
      settings: JSON.stringify(row.settings, null, 2),
      stream: JSON.stringify(row.stream, null, 2),
    })
    setDlgOpen(true)
  }

  async function save() {
    try {
      const body = {
        tag: form.tag,
        protocol: form.protocol,
        listen: form.listen,
        port: Number(form.port),
        remark: form.remark,
        settings: JSON.parse(form.settings || '{}'),
        stream: JSON.parse(form.stream || '{}'),
      }
      if (editing) await API.inbounds.update(editing.id, body)
      else await API.inbounds.create(body)
      setDlgOpen(false)
      await refresh()
    } catch (e) { setErr(String(e)) }
  }

  async function remove(id: number) {
    if (!confirm('ลบ inbound นี้?')) return
    await API.inbounds.remove(id).catch((e) => setErr(String(e)))
    await refresh()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Inbounds</h1>
        <Button onClick={openCreate}>+ สร้างใหม่</Button>
      </div>
      {err && <div className="text-sm text-destructive">{err}</div>}

      <Card>
        <CardHeader><CardTitle>ทั้งหมด ({rows.length})</CardTitle></CardHeader>
        <CardContent>
          <Table>
            <THead>
              <TR>
                <TH>Tag</TH><TH>Protocol</TH><TH>Port</TH><TH>Traffic</TH><TH></TH>
              </TR>
            </THead>
            <TBody>
              {rows.map((r) => (
                <TR key={r.id}>
                  <TD className="font-medium">{r.tag}</TD>
                  <TD>{r.protocol}</TD>
                  <TD>{r.listen}:{r.port}</TD>
                  <TD className="text-muted-foreground text-xs">
                    ↑ {r.total_up.toLocaleString()} B · ↓ {r.total_down.toLocaleString()} B
                  </TD>
                  <TD className="text-right space-x-2">
                    <Button size="sm" variant="secondary" onClick={() => navigate(`/inbounds/${r.id}/clients`)}>Clients</Button>
                    <Button size="sm" variant="outline" onClick={() => openEdit(r)}>แก้ไข</Button>
                    <Button size="sm" variant="destructive" onClick={() => remove(r.id)}>ลบ</Button>
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
        title={editing ? `แก้ไข ${editing.tag}` : 'สร้าง Inbound ใหม่'}
        wide
        footer={
          <>
            <Button variant="outline" onClick={() => setDlgOpen(false)}>ยกเลิก</Button>
            <Button onClick={save}>บันทึก</Button>
          </>
        }
      >
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1"><Label>Tag</Label>
            <Input value={form.tag} onChange={(e) => setForm({ ...form, tag: e.target.value })} /></div>
          <div className="space-y-1"><Label>Protocol</Label>
            <select className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                    value={form.protocol}
                    onChange={(e) => setForm({ ...form, protocol: e.target.value })}>
              {protocols.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div className="space-y-1"><Label>Listen</Label>
            <Input value={form.listen} onChange={(e) => setForm({ ...form, listen: e.target.value })} /></div>
          <div className="space-y-1"><Label>Port</Label>
            <Input type="number" value={form.port} onChange={(e) => setForm({ ...form, port: Number(e.target.value) })} /></div>
          <div className="space-y-1 col-span-2"><Label>Remark</Label>
            <Input value={form.remark} onChange={(e) => setForm({ ...form, remark: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>settings (JSON)</Label>
            <textarea className="w-full h-24 rounded-md border border-input bg-transparent px-3 py-2 text-sm font-mono"
                      value={form.settings}
                      onChange={(e) => setForm({ ...form, settings: e.target.value })} /></div>
          <div className="space-y-1 col-span-2"><Label>stream (JSON)</Label>
            <textarea className="w-full h-24 rounded-md border border-input bg-transparent px-3 py-2 text-sm font-mono"
                      value={form.stream}
                      onChange={(e) => setForm({ ...form, stream: e.target.value })} /></div>
        </div>
      </Dialog>
    </div>
  )
}
