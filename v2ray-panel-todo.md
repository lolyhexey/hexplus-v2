# V2Ray Web Panel — TODO

Checklist สำหรับงานเพิ่ม V2Ray/Xray Web Panel เข้า hexplus

**Concept:**
- Single binary — xray-core embed แบบเดียวกับ openvpn (`//go:embed`)
- hexplus = installer เท่านั้น (กดเมนู → ติดตั้ง panel), ไม่ยุ่งกับ v2ray หลังติดตั้ง
- Panel ยืนคนเดียว มี user DB / config / cert ของตัวเอง แยกจาก OpenVPN
- ไม่มี Telegram bot, ไม่มี REST API/Swagger public

**Stack ที่ตัดสินใจแล้ว:**
- Backend: Go (extend hexplus)
- Frontend: React + shadcn/ui + Tailwind
- DB: SQLite (`modernc.org/sqlite` — pure Go, ไม่ CGO)
- xray-core: option B — embed binary, extract runtime (pattern เดียวกับ openvpn)

---

## Phase 0 — Decisions (ล็อคทั้งหมดแล้ว)

- [x] **xray-core version pin**: latest tagged release — `build/Dockerfile.xray` ดึงจาก GitHub tag ล่าสุดตอน build
- [x] **Panel port**: ตั้ง/เปลี่ยนได้ผ่านเมนู (default `2053`)
- [x] **Admin password**: random gen ตอนติดตั้งครั้งแรก + โชว์ครั้งเดียว, มีเมนู "เปลี่ยน admin password" ทีหลัง
- [x] **HTTPS**: HTTP ล้วน — แนะนำ user ใช้ SSH tunnel หรือ reverse proxy เอง, เมนูโชว์ warning ตอนติดตั้ง
- [x] **Multi-node**: defer ไป v2 — MVP รองรับ single-node เท่านั้น แต่ DB schema เผื่อ column `node_id` ไว้
- [x] **Reinstall behavior**: ถาม user ทุกครั้ง — "เก็บ client + inbound เดิมมั้ย?" (Y = migrate DB, N = wipe)
- [x] **URL prefix**: random path prefix (เช่น `http://IP:2053/xY9kQ2/`) — gen ตอน install, เปลี่ยนได้ผ่านเมนู
- [x] **Subscription port**: พอร์ตเดียวกับ panel (`:2053/sub/<token>`)
- [x] **Path config/data**: `/var/lib/hexplus/` ทั้งหมด (panel DB, xray config, backups, cert)
- [x] **gRPC stats API**: bind `127.0.0.1:10085` เท่านั้น (ไม่ expose ออกนอก)
- [x] **Xray log**: journald เท่านั้น (`journalctl -u hexplus-xray`) ไม่เขียน log file

---

## Phase 1 — Foundation (xray binary + skeleton)

### Build pipeline
- [ ] `build/Dockerfile.xray` — static musl build ของ xray-core
- [ ] `build/build-statics.sh` — เพิ่มขั้นตอน build xray
- [ ] `internal/assets/bin/xray-core` — วาง binary + `//go:embed` entry
- [ ] `internal/assets/embed.go` — เพิ่ม xray-core เข้า `Binaries()`
- [ ] verify multi-arch build (amd64/arm64/armv7)

### Xray wrapper package
- [ ] สร้าง `internal/xray/` package
- [ ] `xray/config.go` — gen `/etc/xray/config.json` จาก DB
- [ ] `xray/daemon.go` — start/stop/reload xray subprocess
- [ ] `xray/stats.go` — pull traffic stats จาก xray gRPC API (`:10085`)
- [ ] `xray/client.go` — CRUD client (add/remove/quota/expiry)

### Systemd
- [ ] `hexplus-xray.service` — เพิ่มใน `service/service.go` (`All()`)
- [ ] `hexplus-panel.service` — เพิ่มใน `service/service.go`
- [ ] ทั้ง 2 unit **ไม่ auto-enable** ตอน install หลัก — enable เมื่อ user กดจากเมนู

---

## Phase 2 — Panel backend

### HTTP server + auth
- [ ] `internal/panel/` package
- [ ] `panel/server.go` — HTTP router (net/http)
- [ ] `panel/auth.go` — session + bcrypt login
- [ ] `panel/tls.go` — self-signed cert gen (reuse `internal/pki`)

### Database
- [ ] `panel/db/` — SQLite driver setup (`modernc.org/sqlite`)
- [ ] schema: `admin_users`, `inbounds`, `clients`, `traffic_stats`, `certs`, `settings`
- [ ] migration system (numbered SQL files หรือ code migrations)
- [ ] backup/restore helper

### Subcommands
- [ ] `hexplus panel serve` — รัน web server
- [ ] `hexplus panel install` — gen admin password + write unit + open firewall
- [ ] `hexplus panel uninstall` — cleanup ครบ (unit, DB, iptables, xray config)
- [ ] `hexplus panel show` — โชว์ URL + admin password ปัจจุบัน
- [ ] `hexplus xray reload` — hook สำหรับ panel เรียก

---

## Phase 3 — Protocol support (Tier 1 - core)

### VLESS + REALITY (priority สูงสุด)
- [ ] config template สำหรับ VLESS + Reality
- [ ] gen private/public key คู่ (x25519)
- [ ] gen shortIds
- [ ] share link format (`vless://...?security=reality`)
- [ ] QR code (server-side render, `skip2/go-qrcode`)

### VMess
- [ ] config template VMess + WS+TLS
- [ ] share link (`vmess://<base64 json>`)

### Trojan
- [ ] config template Trojan + TCP+TLS
- [ ] share link (`trojan://...`)

### Shadowsocks (+ SS2022)
- [ ] config template SS + SS2022
- [ ] gen shared key
- [ ] share link (`ss://...`)

### Transport variants (ทำร่วมกับ protocol ข้างบน)
- [ ] TCP (Raw)
- [ ] WebSocket
- [ ] gRPC
- [ ] HTTPUpgrade
- [ ] XHTTP
- [ ] mKCP

### Security layers
- [ ] TLS (Let's Encrypt auto-issue)
- [ ] XTLS
- [ ] REALITY (ไม่ต้อง cert)
- [ ] ECH
- [ ] Post-Quantum

---

## Phase 4 — Protocol support (Tier 2 - extra)

- [ ] WireGuard inbound
- [ ] Hysteria2
- [ ] HTTP proxy inbound
- [ ] SOCKS inbound
- [ ] Dokodemo-door / Tunnel
- [ ] TUN device

---

## Phase 5 — Client management

- [ ] add/list/edit/remove client
- [ ] traffic quota ต่อ user (bytes limit)
- [ ] วันหมดอายุ (expiry timestamp)
- [ ] IP concurrent limit (fail2ban-style)
- [ ] online status (poll xray stats)
- [ ] one-click share link + QR
- [ ] enable/disable client (soft-toggle ไม่ต้องลบ)
- [ ] reset counter ต่อ user
- [ ] bulk operations (extend expiry, reset traffic ทั้งกลุ่ม)

---

## Phase 6 — Subscription server

- [ ] subscription endpoint (`/sub/<token>`)
- [ ] custom template หน้า sub
- [ ] serve config ตาม client (Clash / v2rayN / Shadowrocket format)
- [ ] token rotation

---

## Phase 7 — Traffic / Stats

- [ ] poll xray gRPC stats ทุก N วินาที
- [ ] เก็บ time-series ใน SQLite (per-inbound / per-client / per-outbound)
- [ ] dashboard: total up/down, top clients, per-inbound bandwidth
- [ ] auto-disable client เมื่อ traffic เกิน quota
- [ ] auto-disable client เมื่อ expiry ผ่าน
- [ ] reset counter (manual + scheduled)

---

## Phase 8 — Routing / Outbound

- [ ] custom routing rules (domain / IP / geosite / geoip)
- [ ] outbound chain
- [ ] WARP integration (auto-provision WARP interface)
- [ ] NordVPN outbound
- [ ] fallback หลายโปรโตคอลบนพอร์ตเดียว

---

## Phase 9 — Cert manager

- [ ] Let's Encrypt auto-issue (HTTP-01 + DNS-01)
- [ ] cert renewal cron
- [ ] manual cert upload
- [ ] show expiry ใน UI

---

## Phase 10 — Frontend (React + shadcn)

### Setup
- [ ] `frontend/` scaffold (Vite + React 19 + shadcn/ui + Tailwind + TS)
- [ ] `pnpm build` → output ไป `internal/panel/frontend/dist/`
- [ ] Makefile target `make frontend` (รันก่อน `build-all`)
- [ ] `//go:embed frontend/dist/*` ใน panel package
- [ ] dev mode: อ่านจาก disk / prod: อ่านจาก embed (build tag)

### Pages
- [ ] Login
- [ ] Dashboard (traffic overview, service status)
- [ ] Inbounds list + create/edit
- [ ] Clients list + create/edit (per inbound)
- [ ] Subscription settings
- [ ] Routing rules
- [ ] Outbounds
- [ ] Certificates
- [ ] Backup/Restore
- [ ] Settings (admin password, panel port, theme)

### UI polish
- [ ] Dark/light theme toggle
- [ ] i18n: TH + EN (ขั้นต้น)
- [ ] Toast / dialog / form validation
- [ ] Copy-to-clipboard + QR modal

---

## Phase 11 — hexplus installer menu

- [ ] เพิ่ม menu entry ใน `internal/menu/` (option ใหม่หรือ sub-menu)
- [ ] submenu:
  - [ ] ติดตั้ง V2Ray Panel (ถามพอร์ต + gen admin password random โชว์ครั้งเดียว)
  - [ ] ถอนการติดตั้ง
  - [ ] แสดง URL + admin password ปัจจุบัน
  - [ ] restart panel
  - [ ] restart xray
  - [ ] เปลี่ยน panel port (validate port ว่าง + update unit + reload)
  - [ ] เปลี่ยน / reset admin password (bcrypt hash → เขียน DB)
- [ ] check port ว่างก่อนติดตั้ง (ชนกับ fileserver 82 หรือ OpenVPN?)
- [ ] เตือนถ้าพอร์ต Xray inbound ชนกับ OpenVPN
- [ ] auto-open firewall port
- [ ] uninstall clean: unit + DB + iptables + xray extracted binary
- [ ] header ปัจจุบัน (V2RAY line) โชว์สถานะ + port

---

## Phase 12 — Backup + Migration

- [ ] backup: dump SQLite + xray config → tarball
- [ ] restore: จาก tarball
- [ ] auto-backup cron (daily → เก็บใน `/var/lib/hexplus/backups/`)
- [ ] schema migration versioned

---

## Phase 13 — Security hardening

- [ ] rate-limit login endpoint
- [ ] Fail2ban-style ban IP หลัง N failed logins
- [ ] session timeout
- [ ] CSRF token
- [ ] HTTPS force
- [ ] secure cookies

---

## Phase 14 — QA / Release

- [ ] unit tests (xray config gen, share link, stats parser)
- [ ] integration test: install → create inbound → connect real client → verify traffic
- [ ] test บน amd64 / arm64 / armv7
- [ ] update `README.md` เพิ่มหัวข้อ V2Ray Panel
- [ ] release binary + changelog

---

## Deferred (ไม่ทำใน v1)

- Telegram bot
- REST API + Swagger public
- Multi-node master/agent (พิจารณาเพิ่มใน v2 ถ้า user ขอ)
- PostgreSQL backend (SQLite พอ)
- 13 ภาษา (TH + EN พอ)
