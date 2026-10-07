package voice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasureCountsWhatItSays(t *testing.T) {
	r := []string{"yeah.", "*stares* no", "THIS is a SHOUT", "are you sure?", "I'd be happy to help with that!", ""}
	s := Measure(r)
	if s.N != 5 {
		t.Fatalf("empty messages must not count: %d", s.N)
	}
	if s.ActionPct != 20 || s.QuestionPct != 20 || s.CapsPct != 20 || s.RoboticPct != 20 {
		t.Fatalf("%+v", s)
	}
	if s.LowerPct != 60 { // "yeah." "*stares* no" "are you sure?"
		t.Fatalf("lower %v", s.LowerPct)
	}
}

func TestATicIsFoundNoMatterWhereInTheReply(t *testing.T) {
	var r []string
	for i := 0; i < 10; i++ {
		r = append(r, "ok that really landed with me "+strings.Repeat("x", i))
	}
	r = append(r, "something else entirely", "and another one here")
	s := Measure(r)
	if s.TopPhrase != "ok that really" && !strings.Contains(s.TopPhrase, "landed") && s.TopPhrasePct < 80 {
		t.Fatalf("%+v", s)
	}
	if fails := s.Check(DefaultTargets()); !strings.Contains(strings.Join(fails, "|"), "tic") {
		t.Fatalf("a phrase in 10 of 12 replies passed the gate: %v", fails)
	}
}

func TestOneWayOfStartingMustNotDominate(t *testing.T) {
	var r []string
	for i := 0; i < 14; i++ {
		r = append(r, "honestly "+strings.Repeat("word ", i%5+2))
	}
	if fails := Measure(r).Check(DefaultTargets()); !strings.Contains(strings.Join(fails, "|"), "start with") {
		t.Fatalf("%v", fails)
	}
}

func TestSmallSamplesAreNotJudgedOnRepetition(t *testing.T) {
	r := []string{"honestly no", "honestly yes", "honestly maybe"}
	for _, f := range Measure(r).Check(DefaultTargets()) {
		if strings.Contains(f, "start with") || strings.Contains(f, "tic") {
			t.Fatalf("3 replies were judged on repetition: %s", f)
		}
	}
}

func TestLengthBoundsFailBothWays(t *testing.T) {
	long := strings.Repeat("word ", 90)
	var r []string
	for i := 0; i < 5; i++ {
		r = append(r, long)
	}
	if f := Measure(r).Check(DefaultTargets()); len(f) == 0 || !strings.Contains(f[0], "median") {
		t.Fatalf("essays passed: %v", f)
	}
	if f := Measure([]string{"k", "k", "k"}).Check(DefaultTargets()); len(f) == 0 {
		t.Fatal("one-letter replies passed the default floor")
	}
}

func TestTargetsFileOverridesOnlyWhatItSays(t *testing.T) {
	p := filepath.Join(t.TempDir(), "voice.json")
	os.WriteFile(p, []byte(`{"median_words":[1,8],"min_lower_pct":80}`), 0o644)
	tg, err := LoadTargets(p)
	if err != nil || tg.MedianWords != [2]int{1, 8} || tg.MinLowerPct != 80 || tg.MaxActionPct != 20 {
		t.Fatalf("%+v %v", tg, err)
	}
	if tg, err := LoadTargets(filepath.Join(t.TempDir(), "none.json")); err != nil || tg.MaxActionPct != 20 {
		t.Fatalf("a missing file means the defaults: %+v %v", tg, err)
	}
	os.WriteFile(p, []byte(`{nope`), 0o644)
	if _, err := LoadTargets(p); err == nil {
		t.Fatal("a broken file must be an error, not silently the defaults")
	}
}

func TestTargetsFromAPersonHoldTheirOwnMessagesInsideThem(t *testing.T) {
	vocab := strings.Fields("cat rain tea bus moon code lamp fork sleep noodle train glass river piano")
	var mine []string
	for i := 0; i < 20; i++ {
		var w []string
		for j := 0; j < 6+i%4; j++ {
			w = append(w, vocab[(i*3+j*5)%len(vocab)])
		}
		mine = append(mine, strings.Join(w, " "))
	}
	s := Measure(mine)
	if f := s.Check(FromStats(s)); len(f) != 0 {
		t.Fatalf("a person fails their own targets: %v", f)
	}
	if f := Measure([]string{strings.Repeat("word ", 90)}).Check(FromStats(s)); len(f) == 0 {
		t.Fatal("an essay passes a terse person's targets")
	}
}
