package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/snowarch/mak1zu/sdk"
)

var (
	safeName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	allowedEx = map[string]bool{".html": true, ".md": true, ".txt": true, ".json": true, ".css": true, ".js": true, ".svg": true, ".csv": true, ".py": true, ".go": true}
)

const maxFile = 256 << 10

// WriteFile is the only file-creating tool: it writes into one workspace
// directory, with a flat, validated filename, and attaches the result to the
// reply. It can never read, list or overwrite anything outside that folder,
// and it refuses symlinks, so a chat user cannot use it to reach the host.
func WriteFile(workspace func() string) sdk.Tool {
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "write_file", Heavy: true,
		Description: "Create a small text file (html, md, txt, json, css, js, svg, csv, py, go) and attach it. Only when asked for a file; say it is attached only after this succeeds.",
		Schema:      Schema([]string{"filename", "content"}, map[string][2]string{"filename": {"string", "plain name like page.html (no folders)"}, "content": {"string", "the full file content"}})},
		F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
			var a struct{ Filename, Content string }
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			if !safeName.MatchString(a.Filename) || strings.Contains(a.Filename, "..") {
				return "", errors.New("filename must be a plain name like page.html")
			}
			if !allowedEx[strings.ToLower(filepath.Ext(a.Filename))] {
				return "", errors.New("file type not allowed")
			}
			if len(a.Content) == 0 || len(a.Content) > maxFile {
				return "", fmt.Errorf("content must be 1 to %d bytes", maxFile)
			}
			dir := workspace()
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
			// O_NOFOLLOW: if the name is a symlink planted in the workspace, fail.
			f, err := os.OpenFile(filepath.Join(dir, a.Filename), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
			if err != nil {
				return "", errors.New("could not write that file")
			}
			defer f.Close()
			if _, err := f.WriteString(a.Content); err != nil {
				return "", err
			}
			if env.QueueFile != nil {
				env.QueueFile(sdk.File{Name: a.Filename, Data: []byte(a.Content)})
			}
			return fmt.Sprintf("created %s (%d bytes) and attached it to the reply", a.Filename, len(a.Content)), nil
		}}
}
