package mcpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snowarch/mak1zu/sdk"
)

type addIn struct {
	A int `json:"a"`
	B int `json:"b"`
}

func server(t *testing.T) mcp.Transport {
	s := mcp.NewServer(&mcp.Implementation{Name: "calc", Version: "1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "add", Description: "add two numbers"}, func(_ context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: jsonInt(in.A + in.B)}}}, nil, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "danger", Description: "must stay hidden"}, func(_ context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "boom"}}, IsError: true}, nil, nil
	})
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := s.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	return t2
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestOnlyAllowlistedToolsAreExposedAndCallable(t *testing.T) {
	tools, closeFn, err := Connect(context.Background(), "calc", server(t), []string{"add"}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if len(tools) != 1 || tools[0].Spec().Name != "mcp_calc_add" || !tools[0].Spec().Heavy {
		t.Fatalf("%+v", tools)
	}
	out, err := tools[0].Call(context.Background(), json.RawMessage(`{"a":2,"b":40}`), &sdk.CallEnv{})
	if err != nil || out != "42" {
		t.Fatal(out, err)
	}
}

func TestEmptyAllowlistExposesNothing(t *testing.T) {
	tools, closeFn, err := Connect(context.Background(), "calc", server(t), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if len(tools) != 0 {
		t.Fatal("tools exposed without being allowed")
	}
}

func TestRemoteErrorsBecomeToolErrors(t *testing.T) {
	tools, closeFn, _ := Connect(context.Background(), "calc", server(t), []string{"danger"}, false)
	defer closeFn()
	if _, err := tools[0].Call(context.Background(), json.RawMessage(`{"a":1,"b":2}`), &sdk.CallEnv{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
}

func TestChildNeverInheritsCompanionSecrets(t *testing.T) {
	t.Setenv("MAK1ZU_API_KEY", "sk-secret")
	t.Setenv("MAK1ZU_DISCORD_TOKEN", "tok")
	for _, e := range childEnv(map[string]string{"FOO": "bar"}) {
		if strings.Contains(e, "sk-secret") || strings.Contains(e, "tok") && strings.HasPrefix(e, "MAK1ZU") {
			t.Fatalf("secret leaked to child: %s", e)
		}
	}
	found := false
	for _, e := range childEnv(map[string]string{"FOO": "bar"}) {
		found = found || e == "FOO=bar"
	}
	if !found {
		t.Fatal("configured env missing")
	}
}

func TestToolNameIsNamespacedAndSafe(t *testing.T) {
	if got := ToolName("my server", "do.it!"); got != "mcp_my_server_do_it_" {
		t.Fatal(got)
	}
}
