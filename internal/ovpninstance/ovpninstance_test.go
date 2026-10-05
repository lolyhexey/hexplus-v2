package ovpninstance

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Registries written before spread workers existed have no "worker" field;
// they must read as the operator's extra ports.
func TestWorkerFlagAndFilters(t *testing.T) {
	var list []Instance
	old := `[{"id":2,"port":8443,"proto":"tcp"},{"id":3,"port":21195,"proto":"tcp","worker":true}]`
	if err := json.Unmarshal([]byte(old), &list); err != nil {
		t.Fatal(err)
	}
	if got := Extras(list); !reflect.DeepEqual(got, list[:1]) {
		t.Errorf("Extras = %v", got)
	}
	if got := Workers(list); !reflect.DeepEqual(got, list[1:]) {
		t.Errorf("Workers = %v", got)
	}
	b, _ := json.Marshal(list[:1])
	if string(b) != `[{"id":2,"port":8443,"proto":"tcp"}]` {
		t.Errorf("an extra port gained a field: %s", b)
	}
}
