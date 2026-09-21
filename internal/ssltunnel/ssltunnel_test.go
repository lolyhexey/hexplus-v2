package ssltunnel

import "testing"

func TestValidateTarget(t *testing.T) {
	valid := []string{
		"127.0.0.1:22",
		"127.0.0.1:80",
		"127.0.0.1:1194",
		"localhost:1194",
		"[::1]:1194",
	}
	for _, target := range valid {
		if err := ValidateTarget(target); err != nil {
			t.Errorf("ValidateTarget(%q) = %v, want nil", target, err)
		}
	}

	invalid := []string{
		"",
		"1194",
		"127.0.0.1",
		":1194",
		"127.0.0.1:",
		"127.0.0.1:0",
		"127.0.0.1:65536",
		"127.0.0.1:abc",
		"::1:1194",
	}
	for _, target := range invalid {
		if err := ValidateTarget(target); err == nil {
			t.Errorf("ValidateTarget(%q) = nil, want error", target)
		}
	}
}
