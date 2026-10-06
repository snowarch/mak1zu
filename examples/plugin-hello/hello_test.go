package hello

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/snowarch/mak1zu/sdk"
)

func TestDiceTool(t *testing.T) {
	tool := New().Tools()[0]
	out, err := tool.Call(context.Background(), json.RawMessage(`{"sides":20}`), &sdk.CallEnv{Speaker: sdk.Identity{Name: "Ana"}})
	if err != nil || out == "" {
		t.Fatal(out, err)
	}
	if tool.Spec().Name != "dice" {
		t.Fatal("tool name")
	}
}
