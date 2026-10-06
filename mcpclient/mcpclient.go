// Package mcpclient turns the tools of Model Context Protocol servers into
// sdk.Tools, using the official Go SDK. This is the language-agnostic
// extension tier: anyone can write a server in any language and Mak1zu can
// use it, with an explicit allowlist per server.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/sdk"
)

var nameClean = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ToolName namespaces a remote tool so servers cannot shadow built-ins.
func ToolName(server, tool string) string {
	n := "mcp_" + nameClean.ReplaceAllString(server, "_") + "_" + nameClean.ReplaceAllString(tool, "_")
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// childEnv is what a spawned server gets: a minimal base plus exactly what the
// config lists. The companion's own secrets (API keys, Discord token) are
// never inherited by third-party processes.
func childEnv(extra map[string]string) []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TMPDIR", "XDG_RUNTIME_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// Start launches a configured stdio server and returns its allowed tools and a
// closer that ends the session.
func Start(ctx context.Context, name string, c config.MCPServer) ([]sdk.Tool, func() error, error) {
	if c.Command == "" {
		return nil, nil, fmt.Errorf("mcp server %q has no command", name)
	}
	cmd := exec.Command(c.Command, c.Args...)
	cmd.Env = childEnv(c.Env)
	return Connect(ctx, name, &mcp.CommandTransport{Command: cmd}, c.Allow, c.Heavy)
}

// Connect lists the server's tools and exposes those on the allowlist.
// An empty allowlist exposes nothing: you opt tools in, not out.
func Connect(ctx context.Context, name string, t mcp.Transport, allow []string, heavy bool) ([]sdk.Tool, func() error, error) {
	cl := mcp.NewClient(&mcp.Implementation{Name: "mak1zu", Version: "0.1"}, nil)
	sess, err := cl.Connect(ctx, t, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp %s: %w", name, err)
	}
	res, err := sess.ListTools(ctx, nil)
	if err != nil {
		sess.Close()
		return nil, nil, fmt.Errorf("mcp %s: list tools: %w", name, err)
	}
	ok := map[string]bool{}
	for _, a := range allow {
		ok[a] = true
	}
	var out []sdk.Tool
	for _, rt := range res.Tools {
		if !ok[rt.Name] {
			continue
		}
		schema, _ := json.Marshal(rt.InputSchema)
		remote := rt.Name
		out = append(out, sdk.ToolFunc{
			S: sdk.ToolSpec{Name: ToolName(name, remote), Description: rt.Description, Schema: schema, Heavy: heavy},
			F: func(ctx context.Context, args json.RawMessage, _ *sdk.CallEnv) (string, error) {
				var a map[string]any
				if len(args) > 0 {
					if err := json.Unmarshal(args, &a); err != nil {
						return "", err
					}
				}
				r, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: remote, Arguments: a})
				if err != nil {
					return "", err
				}
				text := render(r)
				if r.IsError {
					return "", fmt.Errorf("%s", text)
				}
				return text, nil
			}})
	}
	return out, sess.Close, nil
}

func render(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			b.WriteString(v.Text)
		default:
			b.WriteString("[non-text content omitted]")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
