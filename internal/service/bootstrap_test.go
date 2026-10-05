package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The squid ACL set lives in exactly one place, the menu's buildSquidConf.
// A starter squid.conf in this package once carried an unrestricted
// "http_access allow CONNECT SSL_ports" (an open relay on every interface)
// and ran whenever the menu's own write failed. Writing no squid.conf here
// makes squid fail closed instead, so no source file of this package may
// carry squid ACL rules again.
func TestServicePackageCarriesNoSquidACL(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if strings.Contains(string(data), "http_access") {
			t.Errorf("%s defines squid http_access rules; the only squid.conf writer is the menu's buildSquidConf", f)
		}
	}
	if checked == 0 {
		t.Fatal("no source files inspected")
	}
}
