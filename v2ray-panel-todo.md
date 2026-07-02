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
- [x] **UI language default**: TH (มี toggle EN)
- [x] **Traffic stats polling**: 10 วินาที/ครั้ง
- [x] **Session timeout**: 24 ชั่วโมง sliding (active = ต่อเวลา)
- [x] **Default inbound port**: suggest random free port ตอน create (user แก้ได้)
- [x] **Frontend build**: dev machine ต้องมี node 20+ / pnpm ก่อน `make build-all` (ไม่ commit dist/)
- [x] **Multi-arch xray**: build ครบ `amd64 / arm64 / armv7` เหมือน hexplus
- [x] **Test env**: dev บน Windows local + integration test บน VPS Linux ก่อน release

---

## Phase 1 — Foundation (xray binary + skeleton)

### Build pipeline
- [x] `build/Dockerfile.xray` — Alpine + Go toolchain, CGO_ENABLED=0 static build จาก `XLTS/Xray-core`
- [x] `build/build-statics.sh` — เพิ่ม `build_one xray xray` + summary entry
- [x] `internal/assets/bin/xray` — placeholder file (จริงมาจาก build-statics.sh)
- [x] `internal/assets/embed.go` — เพิ่ม `bin/xray` เข้า `//go:embed`
- [ ] verify multi-arch build (amd64/arm64/armv7) — ต้อง run บน Linux + docker

### Xray wrapper package
- [x] สร้าง `internal/xray/` package (`xray.go` doc + skeletons)
- [x] `xray/config.go` — struct schema สำหรับ config.json (real gen ทำใน Phase 3)
- [x] `xray/daemon.go` — Reload() stub
- [x] `xray/stats.go` — Sample struct + Poll() stub
- [x] `xray/client.go` — Client struct

### Paths + Systemd
- [x] `internal/paths/paths.go` — เพิ่ม `XrayStateDir`, `XrayConfigPath`, `PanelStateDir`, `PanelDBPath`
- [x] `hexplus-xray.service` — เพิ่มใน `service.All()`
- [x] `hexplus-panel.service` — เพิ่มใน `service.All()`
- [x] ทั้ง 2 unit เขียนไฟล์ตอน install แต่ **ไม่ auto-enable** (per systemd.go docstring)
- [x] `go build ./...` + `go vet ./...` ผ่านหลัง scaffolding

---

## Phase 2 — Panel backend

### HTTP server + auth
- [x] `internal/panel/` package (`config.go`, `server.go`, `auth.go`, `install.go`)
- [x] `panel/server.go` — HTTP router, URLPrefix strip, /sub /login /logout /api/session /healthz
- [x] `panel/auth.go` — bcrypt login, HMAC-signed session cookie, 24h sliding expiry, RequireSession middleware
- [x] TLS: **skipped ตาม decision** — HTTP-only, warning โชว์ตอนติดตั้ง

### Database
- [x] `panel/db/` — SQLite via `modernc.org/sqlite` (pure Go), WAL + FK + busy_timeout pragmas
- [x] schema v1: `nodes`, `admin_users`, `settings`, `inbounds`, `clients`, `traffic_samples`, `sessions` — `node_id` เผื่อ multi-node ทุก user-scoped table
- [x] migration system via `PRAGMA user_version` + transactional apply
- [ ] backup/restore helper — defer ไป Phase 12

### Subcommands (wired ใน `cmd/hexplus/main.go` + `cmd/hexplus/panel.go`)
- [x] `hexplus panel serve` — systemd ExecStart, SIGTERM-aware shutdown
- [x] `hexplus panel install` — gen config + random admin pw, seed DB, write units, ไม่ auto-enable
- [x] `hexplus panel uninstall [--wipe-db]` — stop/disable/remove units, clean XrayStateDir, prompt keep/wipe DB
- [x] `hexplus panel show` — โชว์ port/URL/admin username
- [x] `hexplus panel port <N>` — เปลี่ยน port + restart unit
- [x] `hexplus panel reset-password [--user] [--password]` — เปลี่ยน admin password
- [x] `hexplus xray reload` — hook ให้ panel เรียกเวลา regen config
- [x] `go build ./...` + `go vet ./...` ผ่านหลัง Phase 2

---

## Phase 3 — Protocol support (Tier 1 - core)

### VLESS + REALITY
- [x] config builder `proto_vless.go` (VLESSClient + settings + Fallback)
- [x] Reality privateKey / publicKey (x25519) + shortIds via `keys.go`
- [x] flow `xtls-rprx-vision` เมื่อ security=reality
- [x] share link `vless://uuid@host:port?...&security=reality&pbk=...&sid=...&flow=xtls-rprx-vision#remark`
- [x] QR (`skip2/go-qrcode` PNG output via `/api/clients/{cid}/qr`)

### VMess
- [x] `proto_vmess.go` (alterId 0, AEAD default)
- [x] share link `vmess://<base64 JSON>` (v2rayN classic format)

### Trojan
- [x] `proto_trojan.go` + Fallback support
- [x] share link `trojan://password@host:port?...`

### Shadowsocks + SS2022
- [x] `proto_shadowsocks.go` — classic ใช้ single password, SS2022 auto-switch เป็น clients[]
- [x] gen shared key ตามความยาว method (16/32 bytes)
- [x] share link `ss://<base64 method:password>@host:port#remark`

### Transport variants (ผ่าน `transport.go` shared)
- [x] TCP (Raw)
- [x] WebSocket + path/host header
- [x] gRPC + serviceName
- [x] HTTPUpgrade
- [x] XHTTP (mode auto)
- [x] mKCP (default settings)

### Security layers
- [x] TLS (SNI, ALPN, fingerprint, certificates)
- [x] XTLS (via flow=xtls-rprx-vision บน Reality)
- [x] REALITY (dest + serverNames + privateKey + shortIds + spiderX)
- [ ] Let's Encrypt auto-issue — defer ไป Phase 9
- [ ] ECH / Post-Quantum — xray-core support ยัง scaffolded ผ่าน TransportParams

---

## Phase 4 — Protocol support (Tier 2 - extra)

- [x] WireGuard inbound (`proto_wireguard.go` — secretKey + peers[])
- [x] Hysteria2 (`proto_hysteria2.go` — users + obfs salamander)
- [x] HTTP proxy inbound (`proto_simple.go`)
- [x] SOCKS inbound (`proto_simple.go` — password/noauth auto)
- [x] Dokodemo-door / Tunnel (`proto_simple.go`)
- [ ] TUN device — ต้องเพิ่ม `with_gvisor` build tag ใน `Dockerfile.xray` (defer)

---

## Phase 3-4 — Backend integration (bonus)

- [x] `internal/xray/generate.go` — DB → `Config` struct → JSON → atomic write to `paths.XrayConfigPath`
- [x] built-in API inbound (`127.0.0.1:10085`) เตรียมไว้ให้ Phase 7 poll stats
- [x] `direct` + `block` outbounds + basic routing (api → api)
- [x] `internal/xray/daemon.go` `Reload(db)` — regen + `systemctl try-reload-or-restart hexplus-xray`
- [x] `internal/panel/api_inbounds.go` — GET/POST/PUT/DELETE `/api/inbounds[/:id]`
- [x] `internal/panel/api_clients.go` — GET/POST/PUT/DELETE + `/api/clients/{cid}/link` + `/qr`
- [x] auto-fill identity (uuid / trojan password / SS key / hy2 password) ตอน create client
- [x] mount routes ใน `server.go` (require session)
- [x] `go build ./...` + `go vet ./...` ผ่านหลัง Phase 3+4

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
