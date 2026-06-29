# HEXPLUS v2

เครื่องมือจัดการ VPS สำหรับงาน SSH/VPN แบบ single binary ไม่ต้อง `apt install` ไม่มี script ที่ต้อง extract ออกมา — binary เดียวคือทั้ง installer, menu, และ management surface

```bash
wget https://github.com/lolyhexey/hexplus-v2/releases/latest/download/hexplus-linux-$(uname -m) -O hexplus
chmod +x hexplus
./hexplus
```

`uname -m` คืน `x86_64` บน Intel/AMD 64-bit (VPS ทั่วไป) — script จะดาวน์โหลด binary ที่ตรงกับ CPU ของเครื่องเองโดยไม่ต้องเลือก

---

## ฟีเจอร์

### จัดการผู้ใช้
- สร้าง/ลบ SSH user พร้อม password, วันหมดอายุ, และ limit จำนวนอุปกรณ์
- export `.ovpn` อัตโนมัติเมื่อสร้าง user (อ่าน proto/port จาก server.conf จริง)
- built-in file server (port 82) เพื่อแจก `.ovpn` ผ่าน HTTP link

### Service ที่รองรับ
| Service | เวอร์ชัน | หมายเหตุ |
|---|---|---|
| OpenVPN | 2.5.9 | static musl, auth ผ่าน PAM หรือ openssl-passwd |
| Squid | 3.3.8 | glibc Ubuntu 20.04, OpenSSL 1.0.2u, full-feature |
| Dropbear | 2024.85 | static musl, multi-binary (dropbear/key/scp) |
| OpenSSH | system | จัดการ port ผ่านเมนู |

### SOCKS Proxy (Go-native แทน wsproxy.py)
- SOCKS SSH — HTTP CONNECT tunnel ไปที่ port 22
- WEBSOCKET — WebSocket spoof handshake
- SOCKS OpenVPN — tunnel ไปที่ OpenVPN port
- รองรับหลาย port ต่อ slot, custom response code/message

### ระบบ
- systemd unit generation (สร้าง/อัพเดต unit file อัตโนมัติ)
- `StartLimitIntervalSec=0` — service restart ไม่มีวันยอมแพ้
- PKI จาก Go native `crypto/x509` — ไม่ต้องพึ่ง easy-rsa
- speed test, bandwidth chart, VPS info, connection limiter

---

## Build จาก source

### ความต้องการ
- Go 1.24.2+
- Docker + buildx (สำหรับ build static binaries เท่านั้น)
- Linux หรือ WSL

### วิธีที่ 1 — build เร็ว (static binaries ถูก commit ไว้แล้ว)

ไม่ต้อง build OpenVPN/Squid/Dropbear เอง เพราะ commit ไว้ใน repo แล้ว:

```bash
git clone https://github.com/lolyhexey/hexplus-v2.git
cd hexplus-v2

# build สำหรับ machine ปัจจุบัน
make build

# หรือ cross-compile
make build-all          # amd64 + arm64 + armv7 → dist/
```

output: `hexplus` (native) หรือ `dist/hexplus-linux-*`

---

### วิธีที่ 2 — build static binaries เอง (ถ้าต้องการปรับแต่ง)

ใช้เมื่อ: อยาก patch OpenVPN/Squid/Dropbear หรือ เปลี่ยน version

#### ต้องการ
```bash
# ติดตั้ง qemu สำหรับ cross-arch (ถ้า build arm64 บน amd64)
docker run --privileged --rm tonistiigi/binfmt --install all
```

#### Build ทีละตัว

**OpenVPN 2.5.9** (Alpine 3.19, musl static):
```bash
docker buildx build \
  --platform linux/amd64 \
  --file build/Dockerfile.openvpn \
  --output type=local,dest=internal/assets/bin/_openvpn \
  --target export \
  build/

cp internal/assets/bin/_openvpn/openvpn internal/assets/bin/openvpn
```

**Dropbear 2024.85** (Alpine 3.19, musl static):
```bash
docker buildx build \
  --platform linux/amd64 \
  --file build/Dockerfile.dropbear \
  --output type=local,dest=internal/assets/bin/_dropbear \
  --target export \
  build/

cp internal/assets/bin/_dropbear/dropbearmulti internal/assets/bin/dropbearmulti
```

**Squid 3.3.8** (Ubuntu 20.04, glibc 2.31, OpenSSL 1.0.2u):
```bash
docker buildx build \
  --platform linux/amd64 \
  --file build/Dockerfile.squid \
  --output type=local,dest=internal/assets/bin/_squid \
  --target export \
  build/

cp internal/assets/bin/_squid/squid               internal/assets/bin/squid
cp internal/assets/bin/_squid/mime.conf            internal/assets/bin/mime.conf
cp internal/assets/bin/_squid/squid-errors.tar.gz internal/assets/bin/squid-errors.tar.gz
```

#### หรือ build ทุกตัวพร้อมกัน (script)
```bash
PLATFORM=linux/amd64 bash build/build-statics.sh
# arm64:
PLATFORM=linux/arm64 bash build/build-statics.sh
```

#### สุดท้าย build hexplus
```bash
make build-all
```

---

### เปลี่ยน version ของ component

แก้ `ARG` ที่บนสุดของแต่ละ Dockerfile:

```dockerfile
# build/Dockerfile.openvpn
ARG OPENVPN_VERSION=2.5.9   # ← เปลี่ยนที่นี่

# build/Dockerfile.dropbear
ARG DROPBEAR_VERSION=2024.85

# build/Dockerfile.squid
ARG SQUID_VERSION=3.3.8
ARG OPENSSL_VERSION=1.0.2u  # Squid 3.3.8 ต้องการ OpenSSL 1.0.x
```

> **หมายเหตุ Squid**: Squid 3.3.8 เขียนสำหรับ OpenSSL 1.0.x การใช้ 1.1.x ทำให้ compile ไม่ผ่าน นอกจากนี้ build บน Alpine (musl) ทำให้ crash ที่ startup บน Ubuntu 22.04 จึงใช้ Ubuntu 20.04 + glibc แทน

---

## Static binaries ที่ embed อยู่ใน binary

| ไฟล์ | ขนาด | Build จาก |
|---|---|---|
| `openvpn` | ~4.4 MB | Alpine 3.19, musl, static |
| `dropbearmulti` | ~400 KB | Alpine 3.19, musl, static |
| `squid` | ~5.6 MB | Ubuntu 20.04, glibc 2.31 |
| `squid-errors.tar.gz` | ~29 KB | Ubuntu Squid error pages |
| `mime.conf` | ~12 KB | Squid MIME config |

PKI (shared CA ทุก VPS ใช้ key ชุดเดียวกัน):

| ไฟล์ | หมายเหตุ |
|---|---|
| `pki/ca.crt` | CA certificate |
| `pki/ca.key` | CA private key |
| `pki/ta.key` | OpenVPN TLS-auth key |

---

## Layout

```
cmd/hexplus/            entry point + subcommands
internal/
  assets/               //go:embed สำหรับ binaries + PKI
  extract/              แตกไฟล์ครั้งแรกไปที่ /usr/local/lib/hexplus/
  install/              install/uninstall + systemd units
  menu/                 TUI menu ทั้งหมด
    main.go             main menu + Thai visual padding
    conexao.go          โหมดฟังชั่น (service management hub)
    service_menus.go    submenu แต่ละ service (squid/dropbear/openvpn)
    proxies.go          SOCKS proxy menu
    users.go            จัดการผู้ใช้
    sys.go              speed test, bandwidth chart, VPS info
    page2.go            หน้า 2 (admin tools)
  pki/                  PKI management (CA, server cert, client cert)
  proxy/                SOCKS proxy handler + systemd unit
  service/              service management (start/stop/status/install)
  user/                 user DB + /etc/passwd manipulation
  paths/                path constants (/usr/local/lib/hexplus/ etc.)
  progress/             progress bar animation
  version/              version metadata (set via -ldflags)
build/
  Dockerfile.openvpn    static OpenVPN build
  Dockerfile.dropbear   static Dropbear build
  Dockerfile.squid      Squid 3.3.8 + Ubuntu 20.04 + OpenSSL 1.0.2u
  build-statics.sh      orchestrate all three builds
```

---

## Release อัตโนมัติ

push tag `v*` → GitHub Actions build amd64 + arm64 แบบ native runner (ไม่ใช้ QEMU เพราะ Squid build ช้ามากและ QEMU segfault ระหว่าง smoke test):

```bash
git tag v2.3.0
git push origin v2.3.0
```

ผลลัพธ์ใน GitHub Releases:
- `hexplus-linux-amd64` และ `hexplus-linux-x86_64` (ไฟล์เดียวกัน 2 ชื่อ — รองรับการค้นด้วยชื่อทั้งสองแบบ)
- `SHA256SUMS`

---

## Requirements ของ VPS

- OS: Ubuntu 20.04+ หรือ Debian 11+ (glibc 2.31+)
- Arch: x86_64 หรือ arm64
- Root access
- systemd
