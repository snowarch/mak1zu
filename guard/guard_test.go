package guard

import (
	"strings"
	"testing"
)

func TestCleanStripsProtocolButKeepsProse(t *testing.T) {
	cases := []struct {
		in   string
		want string
		v    Verdict
	}{
		{"<think>hmm</think>ok sure", "ok sure", OK},
		{"<recalled_memories>- x</recalled_memories>", "", Protocol},
		{"<open_threads>- exam</open_threads>", "", Protocol},
		{"<running_bits>- toaster</running_bits>", "", Protocol},
		{"<on_your_mind>- ask about the cat</on_your_mind>", "", Protocol},
		{"hi <recalled_memories>- x</recalled_memories>there", "hi there", OK},
		{`{"tool": "web_search", "args": {}}`, "", Protocol},
		{"i love the <b>bold</b> option, 1 < 2 and 3 > 2", "i love the <b>bold</b> option, 1 < 2 and 3 > 2", OK},
		{"wait.... what", "wait... what", OK},
		{"## How you write\nbe short", "", Leak},
		{"   ", "", Empty},
		{"nice.  :stare:", "nice :stare:", OK},
	}
	for _, c := range cases {
		got, v := Clean(c.in)
		if got != c.want || v != c.v {
			t.Errorf("Clean(%q) = %q,%v want %q,%v", c.in, got, v, c.want, c.v)
		}
	}
}

func TestEveryoneIsDefused(t *testing.T) {
	got, _ := Clean("hey @everyone look")
	if strings.Contains(got, "@everyone") {
		t.Fatal(got)
	}
}

func TestActionOpeningStreak(t *testing.T) {
	a, b := "*stares* ok", "*sighs* fine"
	if !RepeatsActionOpening("*blinks* what", []string{a, b}, 3) {
		t.Fatal("3rd consecutive action opener must be flagged")
	}
	if RepeatsActionOpening("*blinks* what", []string{"no action here", b}, 3) {
		t.Fatal("broken streak flagged")
	}
	if got := StripLeadingAction("*blinks* what is that"); got != "what is that" {
		t.Fatal(got)
	}
	if got := StripLeadingAction("*blinks*"); got != "*blinks*" {
		t.Fatal("must not delete the whole reply")
	}
}

func TestLoopDetectionIgnoresShortReplies(t *testing.T) {
	if IsLoop("lol", []string{"lol"}) {
		t.Fatal("short replies are fine")
	}
	long := "you really thought that was going to work, huh, the audacity of this man"
	if !IsLoop(long, []string{long + " today"}) {
		t.Fatal("near-duplicate not caught")
	}
	if IsLoop("totally different thought about pizza toppings and regrets", []string{long}) {
		t.Fatal("false positive")
	}
}

func TestSplitKeepsFencesBalanced(t *testing.T) {
	code := "```go\n" + strings.Repeat("x := 1\n", 60) + "```"
	for _, c := range Split(code, 120) {
		if strings.Count(c, "```")%2 != 0 {
			t.Fatalf("unbalanced fence in %q", c)
		}
		if len([]rune(c)) > 130 {
			t.Fatalf("chunk too long: %d", len([]rune(c)))
		}
	}
}

func TestLimitEmojis(t *testing.T) {
	if got := LimitEmojis("a :x1: b :x2: c :x3:", 2); got != "a :x1: b :x2: c" {
		t.Fatal(got)
	}
}

func TestRobotic(t *testing.T) {
	if len(RoboticHits("I'd be happy to help! Let me know if you need more")) != 2 {
		t.Fatal("expected two tells")
	}
}
