# HEXPLUS v2 (single-binary)

Single-file Linux deploy of HEXPLUS. Drops on a bare VPS, runs, manages the SSH+VPN service from one binary.

```
wget https://github.com/lolyhexey/HEXPLUS/releases/download/v2/hexplus-linux-arm64
chmod +x hexplus-linux-arm64
./hexplus-linux-arm64
```

No `apt install`. No script tarball. No extracted scripts dir. The binary IS the installer, IS the menu, IS the management surface.

## What's in the box

Embedded (extracted to `/usr/local/lib/hexplus/` on first run, never again):

- `openvpn` — static musl, OpenVPN 2.5+
- `squid` — static musl, Squid 3.x for payload-compatible HTTP proxy
- `dropbear` — static musl, SSH server
- `easy-rsa` — for cert generation

Native Go (no extraction):

- TUI menu (bubble tea)
- HTTP CONNECT proxies (replaces proxy.py / wsproxy.py / open.py)
- User management (passwd/shadow direct)
- systemd unit generation
- Cross-arch cert generator (crypto/x509)

## Why a binary

HEXPLUS v1 was 50+ bash files fetched at install time from raw.githubusercontent. Every breakage was "did the wget fail" / "is this distro version compatible" / "did apt pin the right package." v2 collapses that into one artifact you can `wget` and run.

## Build

```bash
make build              # native arch, for local dev
make build-all          # linux/amd64 + linux/arm64 + linux/armv7
make build-statics      # build static openvpn/squid/dropbear via Docker
```

## Layout

```
cmd/hexplus/             entry point
internal/
  assets/                //go:embed-ed binaries + scripts
  extract/               first-run extraction logic
  install/               systemd unit + install/uninstall
  menu/                  TUI
  service/               openvpn/squid/dropbear process management
  user/                  /etc/passwd direct manipulation
  version/               build-time version metadata
build/
  Dockerfile.openvpn     static musl openvpn build
  Dockerfile.squid       static musl squid build
  Dockerfile.dropbear    static musl dropbear build
  build-statics.sh       orchestrate the above
```

## Status

Phase 0 — POC. Bootstrapping the skeleton and verifying the embed/extract/exec loop works before committing to the full rewrite.
