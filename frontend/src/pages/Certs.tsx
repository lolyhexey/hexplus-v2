import { useEffect, useState } from 'react'
import { API, type Cert } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { Table, THead, TBody, TR, TH, TD } from '@/components/ui/table'

export default function Certs() {
  const [rows, setRows] = useState<Cert[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [acmeOpen, setAcmeOpen] = useState(false)
  const [manualOpen, setManualOpen] = useState(false)
  const [acmeForm, setAcmeForm] = useState({ domain: '', contact_email: '' })
  const [manualForm, setManualForm] = useState({ domain: '', cert_pem: '', key_pem: '' })

  async function refresh() {
    try { setRows(await API.certs.list()) }
    catch (e) { setErr(String(e)) }
  }
  useEffect(() => { refresh() }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">TLS Certificates</h1>
        <div className="space-x-2">
          <Button variant="outline" onClick={() => setManualOpen(true)}>อัปโหลดเอง</Button>
          <Button onClick={() => setAcmeOpen(true)}>+ Let's Encrypt</Button>
        </div>
      </div>
      {err && <div className="text-sm text-destructive">{err}</div>}
      <Card>
        <CardHeader><CardTitle>ทั้งหมด ({rows.length})</CardTitle></CardHeader>
        <CardContent>
          <Table>
            <THead><TR><TH>Domain</TH><TH>Source</TH><TH>หมดอายุ</TH><TH></TH></TR></THead>
            <TBody>
              {rows.map((c) => (
                <TR key={c.id}>
                  <TD className="font-medium">{c.domain}</TD>
                  <TD>{c.source}</TD>
                  <TD>{c.not_after ? new Date(c.not_after * 1000).toLocaleDateString('th-TH') : '?'}</TD>
                  <TD className="text-right space-x-2">
                    {c.source === 'acme' && (
                      <Button size="sm" variant="outline" onClick={() => API.certs.renew(c.id).then(refresh).catch((e) => setErr(String(e)))}>ต่ออายุ</Button>
                    )}
                    <Button size="sm" variant="destructive"
                            onClick={() => { if (confirm('ลบ cert?')) API.certs.remove(c.id).then(refresh) }}>
                      ลบ
                    </Button>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={acmeOpen} onClose={() => setAcmeOpen(false)} title="Let's Encrypt HTTP-01"
              footer={<>
                <Button variant="outline" onClick={() => setAcmeOpen(false)}>ยกเลิก</Button>
                <Button onClick={async () => {
                  try {
                    await API.certs.acme(acmeForm)
                    setAcmeOpen(false)
                    setAcmeForm({ domain: '', contact_email: '' })
                    await refresh()
                  } catch (e) { setErr(String(e)) }
                }}>ขอ Cert</Button>
              </>}>
        <div className="space-y-3">
          <div className="text-sm text-muted-foreground">
            ต้องให้ port 80 ว่างและ domain ชี้มาที่ VPS นี้ก่อน
          </div>
          <div className="space-y-1"><Label>Domain</Label>
            <Input value={acmeForm.domain} onChange={(e) => setAcmeForm({ ...acmeForm, domain: e.target.value })} placeholder="example.com" /></div>
          <div className="space-y-1"><Label>Contact email (optional)</Label>
            <Input value={acmeForm.contact_email} onChange={(e) => setAcmeForm({ ...acmeForm, contact_email: e.target.value })} /></div>
        </div>
      </Dialog>

      <Dialog open={manualOpen} onClose={() => setManualOpen(false)} title="อัปโหลด Cert เอง" wide
              footer={<>
                <Button variant="outline" onClick={() => setManualOpen(false)}>ยกเลิก</Button>
                <Button onClick={async () => {
                  try {
                    await API.certs.uploadManual(manualForm)
                    setManualOpen(false)
                    setManualForm({ domain: '', cert_pem: '', key_pem: '' })
                    await refresh()
                  } catch (e) { setErr(String(e)) }
                }}>อัปโหลด</Button>
              </>}>
        <div className="space-y-3">
          <div className="space-y-1"><Label>Domain</Label>
            <Input value={manualForm.domain} onChange={(e) => setManualForm({ ...manualForm, domain: e.target.value })} /></div>
          <div className="space-y-1"><Label>Cert PEM (fullchain)</Label>
            <textarea className="w-full h-32 rounded-md border border-input bg-transparent px-3 py-2 text-xs font-mono"
                      value={manualForm.cert_pem} onChange={(e) => setManualForm({ ...manualForm, cert_pem: e.target.value })} placeholder="-----BEGIN CERTIFICATE-----" /></div>
          <div className="space-y-1"><Label>Key PEM</Label>
            <textarea className="w-full h-32 rounded-md border border-input bg-transparent px-3 py-2 text-xs font-mono"
                      value={manualForm.key_pem} onChange={(e) => setManualForm({ ...manualForm, key_pem: e.target.value })} placeholder="-----BEGIN PRIVATE KEY-----" /></div>
        </div>
      </Dialog>
    </div>
  )
}
