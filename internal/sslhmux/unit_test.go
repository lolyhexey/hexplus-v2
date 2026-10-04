package sslhmux

import (
	"testing"

	"github.com/lolyhexey/hexplus/internal/unitpolicy"
)

func TestUnitTemplateRestartLimit(t *testing.T) {
	if err := unitpolicy.Check(unitTemplate); err != nil {
		t.Fatal(err)
	}
}
