package persona

import (
	"math"
	"strings"
	"sync"
	"time"
)

// Mood is a tiny two-axis state that conversation tints and time heals. It
// never appears in output; it only changes what the model is told.
type Mood struct {
	mu      sync.Mutex
	Valence float64 // -1 sour .. +1 delighted
	Energy  float64 // 0 asleep .. 1 wired
	Reason  string
	last    time.Time
	// flustered is a quick, sharp reaction to being praised to her face. It fades
	// in minutes, not hours, unlike the slow axes above.
	flustered   float64
	flusteredAt time.Time
}

func NewMood() *Mood { return &Mood{Energy: 0.5, last: time.Now()} }

var (
	posWords    = []string{"lol", "lmao", "xd", "haha", "jaja", "nice", "thanks", "thank you", "gracias", "love", "peak", "cooked"}
	praiseWords = []string{"cute", "adorable", "love you", "best bot", "you're the best", "so pretty", "proud of you", "marry me", "my favorite", "my favourite"}
	negWords    = []string{"annoying", "broken", "hate this", "fuck this", "pissed", "awful", "terrible", "odio"}
	sleepyWords = []string{"tired", "sleepy", "good night", "gn", "bored", "sueño"}
)

// Observe tints the mood from one inbound message.
func (m *Mood) Observe(author, text string, direct bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	mins := math.Min(now.Sub(m.last).Minutes(), 240)
	drift := math.Pow(0.985, mins)
	m.Valence *= drift
	m.Energy = 0.5 + (m.Energy-0.5)*drift
	m.last = now

	t := strings.ToLower(text)
	has := func(ws []string) bool {
		for _, w := range ws {
			if strings.Contains(t, w) {
				return true
			}
		}
		return false
	}
	if has(posWords) {
		m.Valence += 0.035
		m.Energy += 0.025
		m.Reason = author + " said something funny or nice"
	}
	if has(negWords) {
		m.Valence -= 0.045
		m.Reason = author + " is being negative"
	}
	if has(sleepyWords) {
		m.Energy -= 0.055
		m.Reason = "the conversation is getting sleepy"
	}
	caps := 0
	for _, r := range text {
		if r >= 'A' && r <= 'Z' {
			caps++
		}
	}
	if strings.Contains(text, "!") || caps >= 6 {
		m.Energy += 0.035
		m.Reason = author + " is loud"
	}
	if len(text) > 500 {
		m.Energy += 0.015
	}
	if direct && has(praiseWords) {
		m.flustered, m.flusteredAt = 1, now
		m.Reason = author + " said something nice to her face"
	}
	if direct {
		m.Valence += 0.02
		m.Energy += 0.02
		m.Reason = author + " is talking to her directly"
	}
	m.Valence = clamp(m.Valence, -1, 1)
	m.Energy = clamp(m.Energy, 0.05, 1)
	if math.Abs(m.Valence) < 0.05 && m.Energy < 0.6 && m.flusteredNow() <= 0.25 {
		m.Reason = ""
	}
}

// flusteredNow is the praise reaction, decayed to the present moment.
func (m *Mood) flusteredNow() float64 {
	if m.flustered <= 0 {
		return 0
	}
	return m.flustered * math.Pow(0.6, time.Since(m.flusteredAt).Minutes())
}

// MoodState is the mood as data: one dominant name from a closed set plus how
// strongly it holds, for things that react to her mood (the avatar, the panel).
type MoodState struct {
	Name      string  `json:"name"`      // neutral | amused | smug | irritated | sleepy | wired | flustered
	Intensity float64 `json:"intensity"` // 0..1, 0 for neutral
	Valence   float64 `json:"valence"`
	Energy    float64 `json:"energy"`
}

// Snapshot reports the dominant mood using the same thresholds as Describe.
func (m *Mood) Snapshot() MoodState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := MoodState{Name: "neutral", Valence: m.Valence, Energy: m.Energy}
	score := func(over, span float64) float64 { return clamp(0.3+over/span*0.7, 0.3, 1) }
	try := func(name string, ok bool, in float64) {
		if ok && in > st.Intensity {
			st.Name, st.Intensity = name, in
		}
	}
	// flustered goes first so it keeps a tie: a sharp reaction beats a slow mood
	if f := m.flusteredNow(); f > 0.25 {
		try("flustered", true, clamp(0.3+f*0.7, 0.3, 1))
	}
	try("amused", m.Valence > 0.25 && m.Valence <= 0.6, score(m.Valence-0.25, 0.35))
	try("smug", m.Valence > 0.6, score(m.Valence-0.6, 0.4))
	try("irritated", m.Valence < -0.25, score(-m.Valence-0.25, 0.75))
	try("wired", m.Energy > 0.75, score(m.Energy-0.75, 0.25))
	try("sleepy", m.Energy < 0.3, score(0.3-m.Energy, 0.25))
	return st
}

// Describe returns a short natural-language mood, "" when baseline.
func (m *Mood) Describe() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var parts []string
	if m.flusteredNow() > 0.25 {
		parts = append(parts, "flustered")
	}
	switch {
	case m.Valence > 0.6:
		parts = append(parts, "smug")
	case m.Valence > 0.25:
		parts = append(parts, "warm and amused")
	case m.Valence < -0.25:
		parts = append(parts, "irritated")
	}
	switch {
	case m.Energy > 0.75:
		parts = append(parts, "wired")
	case m.Energy < 0.3:
		parts = append(parts, "sleepy")
	}
	if len(parts) == 0 {
		return ""
	}
	s := strings.Join(parts, ", ")
	if m.Reason != "" {
		s += " (" + m.Reason + ")"
	}
	return s
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
