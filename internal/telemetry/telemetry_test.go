package telemetry

import (
	"encoding/json"
	"testing"
)

func TestEmptyRecentMarshalsAsArray(t *testing.T) {
	b, _ := json.Marshal(New(5, "").Recent(10, ""))
	if string(b) != "[]" {
		t.Fatalf("panel iterates this: got %s", b)
	}
}

func TestRingIsBounded(t *testing.T) {
	l := New(3, "")
	for i := 0; i < 10; i++ {
		l.Add(Record{Kind: "turn", Words: i})
	}
	if r := l.Recent(100, ""); len(r) != 3 || r[2].Words != 9 {
		t.Fatalf("%+v", r)
	}
}
