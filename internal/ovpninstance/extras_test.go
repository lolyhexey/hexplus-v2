package ovpninstance

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func fakeRegistry(t *testing.T, list []Instance, listErr error, failIDs ...int) (removed *[]int) {
	t.Helper()
	oldList, oldRemove := listInstances, removeInstance
	t.Cleanup(func() { listInstances, removeInstance = oldList, oldRemove })
	listInstances = func() ([]Instance, error) { return list, listErr }
	var got []int
	removeInstance = func(id int) error {
		got = append(got, id)
		for _, f := range failIDs {
			if f == id {
				return errors.New("boom")
			}
		}
		return nil
	}
	return &got
}

// hexplus uninstall removed no extra port: their units kept running a
// deleted binary and their rules and registry stayed.
func TestRemoveExtrasLeavesSpreadWorkers(t *testing.T) {
	removed := fakeRegistry(t, []Instance{
		{ID: 2, Port: 8443, Proto: "tcp"},
		{ID: 3, Port: 21195, Proto: "tcp", Worker: true},
		{ID: 4, Port: 9000, Proto: "udp"},
	}, nil)
	if err := RemoveExtras(); err != nil {
		t.Fatal(err)
	}
	if want := []int{2, 4}; !reflect.DeepEqual(*removed, want) {
		t.Errorf("removed %v, want %v", *removed, want)
	}
}

func TestRemoveExtrasContinuesAfterAFailure(t *testing.T) {
	removed := fakeRegistry(t, []Instance{{ID: 2}, {ID: 3}, {ID: 4}}, nil, 2)
	err := RemoveExtras()
	if err == nil || !strings.Contains(err.Error(), "#2") {
		t.Errorf("err = %v, want one naming #2", err)
	}
	if want := []int{2, 3, 4}; !reflect.DeepEqual(*removed, want) {
		t.Errorf("removed %v, want %v", *removed, want)
	}
}

func TestRemoveExtrasListError(t *testing.T) {
	removed := fakeRegistry(t, []Instance{{ID: 2}}, errors.New("unreadable"))
	if err := RemoveExtras(); err == nil {
		t.Error("a registry that cannot be read must be reported")
	}
	if len(*removed) != 0 {
		t.Errorf("removed %v", *removed)
	}
}
