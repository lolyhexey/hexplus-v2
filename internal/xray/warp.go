package xray

// warp.go: provision a Cloudflare WARP account and return an
// xray-shaped WireGuard outbound settings blob pointing at it.
//
// WARP flow (based on the wgcf tool's public API usage):
//   1. Generate an x25519 key pair.
//   2. POST /reg to the Cloudflare WARP API with the public key.
//   3. Response contains: server public key, allocated client IPv4/IPv6,
//      and endpoint host (usually engage.cloudflareclient.com:2408).
//   4. Build a WireGuard outbound with our private + their public.
//
// Cloudflare has historically kept this API open (used by their own
// Android/iOS clients + wgcf); if they ever gate it we surface a clean
// error and let the user configure WARP manually.

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WARPRegAPI is Cloudflare's public WARP registration endpoint. The
// server accepts unauthenticated POSTs and returns a JSON blob with
// the peer config for the newly-registered client.
const WARPRegAPI = "https://api.cloudflareclient.com/v0a4005/reg"

// WARPSettings is the outbound.settings shape xray expects for a
// WireGuard outbound. Exported so api_routing can marshal it.
type WARPSettings struct {
	SecretKey  string   `json:"secretKey"`
	Address    []string `json:"address"`
	Peers      []WARPPeer `json:"peers"`
	MTU        int      `json:"mtu,omitempty"`
	Reserved   []int    `json:"reserved,omitempty"`
	DomainStrategy string `json:"domainStrategy,omitempty"`
}

type WARPPeer struct {
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowedIPs"`
	KeepAlive  int      `json:"keepAlive,omitempty"`
}

// ProvisionWARP runs the registration flow and returns the ready-to-
// insert xray outbound settings.
func ProvisionWARP() (WARPSettings, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return WARPSettings{}, fmt.Errorf("gen key: %w", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())

	body := map[string]any{
		"key":     pubB64,
		"install_id": "",
		"fcm_token":  "",
		"tos":        time.Now().UTC().Format(time.RFC3339Nano),
		"model":      "PC",
		"type":       "Android",
		"locale":     "en_US",
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", WARPRegAPI, bytes.NewReader(raw))
	if err != nil {
		return WARPSettings{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return WARPSettings{}, fmt.Errorf("warp reg: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return WARPSettings{}, fmt.Errorf("warp reg http %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Config struct {
			Interface struct {
				Addresses struct {
					V4 string `json:"v4"`
					V6 string `json:"v6"`
				} `json:"addresses"`
			} `json:"interface"`
			Peers []struct {
				PublicKey string `json:"public_key"`
				Endpoint  struct {
					Host string `json:"host"`
					V4   string `json:"v4"`
					V6   string `json:"v6"`
				} `json:"endpoint"`
			} `json:"peers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return WARPSettings{}, fmt.Errorf("warp reg parse: %w", err)
	}
	if len(parsed.Config.Peers) == 0 {
		return WARPSettings{}, errors.New("warp reg returned no peers")
	}
	peer := parsed.Config.Peers[0]

	addresses := []string{}
	if v4 := parsed.Config.Interface.Addresses.V4; v4 != "" {
		addresses = append(addresses, v4+"/32")
	}
	if v6 := parsed.Config.Interface.Addresses.V6; v6 != "" {
		addresses = append(addresses, v6+"/128")
	}
	endpoint := peer.Endpoint.Host
	if endpoint == "" {
		endpoint = "engage.cloudflareclient.com:2408"
	}

	return WARPSettings{
		SecretKey: base64.StdEncoding.EncodeToString(priv.Bytes()),
		Address:   addresses,
		MTU:       1280,
		Peers: []WARPPeer{{
			PublicKey:  peer.PublicKey,
			Endpoint:   endpoint,
			AllowedIPs: []string{"0.0.0.0/0", "::/0"},
		}},
	}, nil
}
