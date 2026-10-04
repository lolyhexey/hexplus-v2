package proxy

import (
	"testing"

	"github.com/lolyhexey/hexplus/internal/unitpolicy"
)

func TestProxyUnitTemplateRestartLimit(t *testing.T) {
	if err := unitpolicy.Check(proxyUnitTemplate); err != nil {
		t.Fatal(err)
	}
}
