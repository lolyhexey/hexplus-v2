package speedlimit

// learnAddressScript is the POSIX-sh hook OpenVPN invokes with
// "add|update|delete <tun_ip> <cn>" whenever a client joins, renegotiates,
// or leaves. It shapes both directions per-session with tc HTB:
//
//   - egress on the tun device caps the client's DOWNLOAD (server→client)
//   - ingress on the tun device is mirrored via `mirred egress redirect`
//     into an ifb device, and shaped there — capping the client's UPLOAD
//     (client→server) since ingress qdiscs on real devices can only police,
//     not shape
//
// Each session gets a unique HTB class + u32 filter derived from the last
// two octets of its tun IP. The /16 subnet gives 65k possible class IDs,
// well above realistic concurrent-user counts.
//
// The script must always exit 0 — OpenVPN treats a non-zero learn-address
// exit as a hard failure and disconnects the client.
const learnAddressScript = `#!/bin/sh
# hexplus-learn-address — per-session bandwidth shaper for OpenVPN.
# Managed by hexplus; do not edit by hand.
# OpenVPN invocation: $1=add|update|delete  $2=tun_ip  $3=cn  env: dev

CONF=/etc/openvpn/hexplus-speedlimit.conf
DEV="${dev:-tun0}"
IFB="ifb${DEV#tun}"

get_mbps() {
    [ -r "$CONF" ] || return 1
    v=$(awk -F= '/^mbps=/{print $2; exit}' "$CONF" 2>/dev/null)
    [ -n "$v" ] && [ "$v" -gt 0 ] 2>/dev/null && echo "$v" || return 1
}

# Encode last two octets of tun IP into a 16-bit id used for both the HTB
# class ID and the tc filter prio. IP 10.8.0.0 is the network address and
# never assigned to a client, so remapping it to 1 (matching the reserved
# default class) is safe. Real client IPs start at 10.8.0.2 and produce
# unique ids without any low-bit aliasing.
class_of() {
    o3=$(echo "$1" | cut -d. -f3)
    o4=$(echo "$1" | cut -d. -f4)
    v=$(( (o3 * 256) | o4 ))
    [ "$v" -eq 0 ] && v=1
    printf '%04x' "$v"
}
prio_of() {
    o3=$(echo "$1" | cut -d. -f3)
    o4=$(echo "$1" | cut -d. -f4)
    v=$(( (o3 * 256) | o4 ))
    [ "$v" -eq 0 ] && v=1
    echo "$v"
}

ensure_qdisc() {
    # Default class 1:1 is unreachable by real clients (only 10.8.0.0 and
    # 10.8.0.1 map to cid 1, both are network / server-side and never
    # trigger learn-address). tc parses minor as hex, so default 1 == 1:1.
    if ! tc qdisc show dev "$DEV" 2>/dev/null | grep -q 'qdisc htb 1:'; then
        tc qdisc add dev "$DEV" root handle 1: htb default 1 2>/dev/null
        tc class add dev "$DEV" parent 1: classid 1:1 htb rate 1000mbit ceil 1000mbit 2>/dev/null
    fi
    modprobe ifb numifbs=0 2>/dev/null
    if ! ip link show "$IFB" >/dev/null 2>&1; then
        ip link add "$IFB" type ifb 2>/dev/null
    fi
    ip link set "$IFB" up 2>/dev/null
    if ! tc qdisc show dev "$IFB" 2>/dev/null | grep -q 'qdisc htb 1:'; then
        tc qdisc add dev "$IFB" root handle 1: htb default 1 2>/dev/null
        tc class add dev "$IFB" parent 1: classid 1:1 htb rate 1000mbit ceil 1000mbit 2>/dev/null
    fi
    # Ingress qdisc and its mirred redirect filter are checked separately so
    # a lost filter can be re-added on a later invocation.
    if ! tc qdisc show dev "$DEV" 2>/dev/null | grep -q 'ffff:'; then
        tc qdisc add dev "$DEV" handle ffff: ingress 2>/dev/null
    fi
    if ! tc filter show dev "$DEV" parent ffff: 2>/dev/null | grep -q 'mirred'; then
        tc filter add dev "$DEV" parent ffff: protocol ip u32 \
            match u32 0 0 action mirred egress redirect dev "$IFB" 2>/dev/null
    fi
}

apply_shape() {
    ip="$1"
    mbps="$2"
    cid=$(class_of "$ip")
    prio=$(prio_of "$ip")

    tc filter del dev "$DEV" protocol ip parent 1: prio "$prio" 2>/dev/null
    tc class del dev "$DEV" classid "1:$cid" 2>/dev/null
    tc class add dev "$DEV" parent 1: classid "1:$cid" htb rate "${mbps}mbit" ceil "${mbps}mbit" 2>/dev/null
    tc filter add dev "$DEV" protocol ip parent 1: prio "$prio" u32 \
        match ip dst "$ip/32" flowid "1:$cid" 2>/dev/null

    tc filter del dev "$IFB" protocol ip parent 1: prio "$prio" 2>/dev/null
    tc class del dev "$IFB" classid "1:$cid" 2>/dev/null
    tc class add dev "$IFB" parent 1: classid "1:$cid" htb rate "${mbps}mbit" ceil "${mbps}mbit" 2>/dev/null
    tc filter add dev "$IFB" protocol ip parent 1: prio "$prio" u32 \
        match ip src "$ip/32" flowid "1:$cid" 2>/dev/null
}

remove_shape() {
    ip="$1"
    cid=$(class_of "$ip")
    prio=$(prio_of "$ip")
    tc filter del dev "$DEV" protocol ip parent 1: prio "$prio" 2>/dev/null
    tc class del dev "$DEV" classid "1:$cid" 2>/dev/null
    tc filter del dev "$IFB" protocol ip parent 1: prio "$prio" 2>/dev/null
    tc class del dev "$IFB" classid "1:$cid" 2>/dev/null
}

case "$1" in
    add|update)
        mbps=$(get_mbps) || exit 0
        ensure_qdisc
        apply_shape "$2" "$mbps"
        ;;
    delete)
        remove_shape "$2"
        ;;
esac
exit 0
`
