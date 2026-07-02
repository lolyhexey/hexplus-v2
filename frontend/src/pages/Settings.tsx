import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

// Settings for MVP is intentionally read-only guidance. The port +
// admin password + panel install/uninstall live under the hexplus TUI
// (menu 17→33) because they touch systemd units and firewall.
export default function Settings() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">ตั้งค่า</h1>
      <Card>
        <CardHeader>
          <CardTitle>การจัดการระดับระบบ</CardTitle>
          <CardDescription>คำสั่งพวกนี้อยู่ในเมนู hexplus (SSH เข้า VPS แล้วพิมพ์ menu)</CardDescription>
        </CardHeader>
        <CardContent className="text-sm space-y-2">
          <div><code className="rounded bg-muted px-2 py-0.5">menu → 17 → 33 → 04</code> รีเซ็ตรหัสผ่าน Admin</div>
          <div><code className="rounded bg-muted px-2 py-0.5">menu → 17 → 33 → 05</code> เปลี่ยนพอร์ต Panel</div>
          <div><code className="rounded bg-muted px-2 py-0.5">menu → 17 → 33 → 06</code> Restart Panel + Xray</div>
          <div><code className="rounded bg-muted px-2 py-0.5">menu → 17 → 33 → 08</code> ปิดบริการ Panel</div>
          <div><code className="rounded bg-muted px-2 py-0.5">hexplus panel backup</code> สำรอง (ต้อง root)</div>
          <div><code className="rounded bg-muted px-2 py-0.5">hexplus panel restore ไฟล์.tar.gz</code> กู้คืน</div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>ความปลอดภัย</CardTitle>
          <CardDescription>Panel รันผ่าน HTTP ล้วน</CardDescription>
        </CardHeader>
        <CardContent className="text-sm space-y-2">
          <p>แนะนำเข้าใช้งานผ่านหนึ่งในนี้:</p>
          <ul className="list-disc pl-5 space-y-1">
            <li>SSH tunnel: <code className="rounded bg-muted px-1">ssh -L 2053:localhost:2053 root@server</code></li>
            <li>Cloudflare Tunnel / nginx reverse proxy พร้อม TLS</li>
          </ul>
          <p>ระบบ Login มี rate-limit (5 ครั้ง / 10 นาที = แบน 1 ชั่วโมง) + CSRF token</p>
        </CardContent>
      </Card>
    </div>
  )
}
