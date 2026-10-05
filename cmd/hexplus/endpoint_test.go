package main

import (
	"flag"
	"io"
	"testing"
)

// Regression guard on the real flag definitions: a fixed 1194/udp default
// cannot tell "operator did not say" from "operator said 1194/udp", and
// produced profiles that cannot connect to a 443/tcp server.
func TestEndpointFlagsDefaultToUnset(t *testing.T) {
	fs := flag.NewFlagSet("user add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port, proto := endpointFlags(fs)
	if _, err := parseFlagsAnywhere(fs, []string{"bob"}); err != nil {
		t.Fatal(err)
	}
	if *port != 0 || *proto != "" {
		t.Errorf("defaults = %d/%q, want 0/\"\" (resolved from server.conf later)", *port, *proto)
	}

	fs = flag.NewFlagSet("user export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port, proto = endpointFlags(fs)
	if _, err := parseFlagsAnywhere(fs, []string{"bob", "--remote-port", "8443", "--proto", "udp"}); err != nil {
		t.Fatal(err)
	}
	if *port != 8443 || *proto != "udp" {
		t.Errorf("explicit flags = %d/%q, want 8443/udp", *port, *proto)
	}
}
