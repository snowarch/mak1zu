package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

func TestBlockedIPs(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.1", "169.254.169.254", "::1", "100.64.0.1", "0.0.0.0", "172.16.5.5"} {
		if !blockedIP(net.ParseIP(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if blockedIP(net.ParseIP(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}

func TestFetchRefusesInternalTargets(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8787/api/config", "http://localhost/", "file:///etc/passwd", "http://169.254.169.254/latest/meta-data", "http://user:pw@example.com/", "http://[::1]/", "http://printer.local/"} {
		if _, _, err := Fetch(context.Background(), u, 1000); err == nil {
			t.Errorf("%s was fetched", u)
		}
	}
}

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, err := ParseWhen("2h30m", "", now)
	if err != nil || !got.Equal(now.Add(150*time.Minute)) {
		t.Fatal(got, err)
	}
	if _, err := ParseWhen("tomorrow", "", now); err == nil {
		t.Fatal("vague phrases need a clarifying question, not a guess")
	}
	if _, err := ParseWhen("", "2025-01-01T00:00:00Z", now); err == nil {
		t.Fatal("past accepted")
	}
}

func TestWorthKeeping(t *testing.T) {
	if WorthKeeping("lol") || WorthKeeping("ok thanks") || !WorthKeeping("likes Frieren more than Naruto") {
		t.Fatal("filter wrong")
	}
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText("<html><script>evil()</script><p>Hello &amp; welcome</p><div>bye</div></html>")
	if strings.Contains(got, "evil") || !strings.Contains(got, "Hello & welcome") {
		t.Fatal(got)
	}
}

func TestWriteFileStaysInWorkspace(t *testing.T) {
	dir := t.TempDir()
	ws := dir + "/ws"
	tool := WriteFile(func() string { return ws })
	var got []sdk.File
	env := &sdk.CallEnv{QueueFile: func(f sdk.File) { got = append(got, f) }}
	call := func(name, content string) error {
		_, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"filename":%q,"content":%q}`, name, content)), env)
		return err
	}
	for _, bad := range []string{"../evil.html", "/etc/passwd", "a/b.html", "x.exe", ".hidden.html", "..html", "page"} {
		if call(bad, "x") == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := call("page.html", "<h1>hi</h1>"); err != nil || len(got) != 1 {
		t.Fatal(err, got)
	}
	// a symlink planted in the workspace must not be followed
	secret := dir + "/secret.txt"
	os.WriteFile(secret, []byte("keep"), 0o600)
	os.Symlink(secret, ws+"/link.txt")
	if call("link.txt", "pwned") == nil {
		t.Error("followed a symlink")
	}
	if b, _ := os.ReadFile(secret); string(b) != "keep" {
		t.Fatal("symlink target overwritten")
	}
	if call("big.txt", strings.Repeat("x", maxFile+1)) == nil {
		t.Error("oversize accepted")
	}
}

func TestAnimeSearchParsesAniList(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"Media":{"title":{"romaji":"Sousou no Frieren","english":"Frieren"},"averageScore":91,"episodes":28,"status":"FINISHED","season":"FALL","seasonYear":2023,"genres":["Fantasy"],"description":"An <b>elf</b> mage.","siteUrl":"https://anilist.co/anime/1"}}}`)
	}))
	defer s.Close()
	old := anilistURL
	anilistURL = s.URL
	defer func() { anilistURL = old }()
	out, err := Anime()[0].Call(context.Background(), json.RawMessage(`{"query":"frieren"}`), &sdk.CallEnv{})
	if err != nil || !strings.Contains(out, "Frieren") || !strings.Contains(out, "91/100") || strings.Contains(out, "<b>") {
		t.Fatal(out, err)
	}
}

func TestReactionGIFOnlyNekosHostAndKnownCategories(t *testing.T) {
	var path string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, `{"results":[{"url":"https://nekos.best/api/v2/hug/x.gif","anime_name":"Frieren"}]}`)
	}))
	defer s.Close()
	old := nekosURL
	nekosURL = s.URL
	defer func() { nekosURL = old }()
	var got string
	env := &sdk.CallEnv{AttachLink: func(u string) { got = u }}
	tool := ReactionGIF()
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"category":"hug"}`), env); err != nil || got != "https://nekos.best/api/v2/hug/x.gif" || path != "/hug" {
		t.Fatal(err, got, path)
	}
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"category":"../../etc/passwd"}`), env); err == nil {
		t.Fatal("arbitrary category accepted")
	}
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results":[{"url":"https://evil.example/x.gif","anime_name":"x"}]}`)
	}))
	defer evil.Close()
	nekosURL = evil.URL
	got = ""
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"category":"hug"}`), env); err == nil || got != "" {
		t.Fatal("posted a link from a foreign host")
	}
}
