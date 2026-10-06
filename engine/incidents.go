package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/provider"
)

// Cause is why a turn failed, in terms a person (not a stack trace) can use.
type Cause string

const (
	CauseProviderDown Cause = "provider_down"
	CauseRateLimit    Cause = "rate_limit"
	CauseMisconfig    Cause = "misconfigured"
	CauseEmpty        Cause = "empty_reply"
	CauseVision       Cause = "vision_unavailable"
	CauseProtocol     Cause = "protocol_leak"
	CauseInternal     Cause = "internal"
	CauseUnmetPromise Cause = "unmet_promise"
)

// Classify maps an error (or a guard verdict) to a Cause.
func Classify(err error, v guard.Verdict) Cause {
	if err == nil {
		switch v {
		case guard.Empty:
			return CauseEmpty
		case guard.Leak, guard.Protocol:
			return CauseProtocol
		}
		return CauseInternal
	}
	switch provider.KindOf(err) {
	case provider.KindRateLimit:
		return CauseRateLimit
	case provider.KindAuth, provider.KindBadRequest:
		return CauseMisconfig
	case provider.KindEmpty:
		return CauseEmpty
	case provider.KindUnsupported:
		return CauseVision
	case provider.KindTimeout, provider.KindServer:
		return CauseProviderDown
	}
	return CauseInternal
}

// Human describes the cause for the panel, to the person who runs her.
func (c Cause) Human() string {
	switch c {
	case CauseProviderDown:
		return "the model provider did not answer"
	case CauseRateLimit:
		return "the provider says she is out of quota or going too fast"
	case CauseMisconfig:
		return "the provider rejected the key, model or request"
	case CauseEmpty:
		return "the model answered with nothing"
	case CauseVision:
		return "someone sent an image and the model cannot see images"
	case CauseProtocol:
		return "the model leaked internals instead of a reply, so she did not send it"
	case CauseUnmetPromise:
		return "she said she did something she did not do"
	}
	return "something broke inside her"
}

// Transient causes are worth one automatic retry of a recent direct turn.
func (c Cause) Transient() bool {
	return c == CauseProviderDown || c == CauseEmpty || c == CauseRateLimit
}

var messages = map[Cause][2]string{ // {en, es}
	CauseProviderDown: {"my brain provider is down right now, try me again in a minute", "mi proveedor de IA está caído ahora mismo, prueba de nuevo en un minuto"},
	CauseRateLimit:    {"i hit a rate limit, give me a minute", "superé el límite de uso, dame un minuto"},
	CauseMisconfig:    {"something's misconfigured on my side (key or model), the owner needs to look at it", "algo está mal configurado de mi lado (clave o modelo), tiene que revisarlo el dueño"},
	CauseEmpty:        {"i went blank on that one, say it again?", "me quedé en blanco con esa, ¿me lo repites?"},
	CauseVision:       {"i can't see images with the model i'm running on right now", "con el modelo que tengo ahora no puedo ver imágenes"},
	CauseProtocol:     {"that came out wrong on my end, ask me again", "eso me salió mal, pregúntame de nuevo"},
	CauseInternal:     {"something broke on my side, i logged it", "algo se rompió de mi lado, quedó registrado"},
}

// FailureReply is the one useful line sent when a turn fails, in the
// language of the person who asked. It never contains internals.
func FailureReply(c Cause, userText string) string {
	m, ok := messages[c]
	if !ok {
		m = messages[CauseInternal]
	}
	if looksSpanish(userText) {
		return m[1]
	}
	return m[0]
}

func looksSpanish(s string) bool {
	low := strings.ToLower(s)
	for _, r := range low {
		if strings.ContainsRune("¿¡ñáéíóú", r) {
			return true
		}
	}
	words := strings.FieldsFunc(low, func(r rune) bool { return !unicode.IsLetter(r) })
	hits := 0
	for _, w := range words {
		switch w {
		case "que", "qué", "como", "cómo", "por", "para", "una", "pero", "vos", "tu", "sos", "che", "hola", "dale", "jaja", "ayudás", "puedes", "puedo", "quiero", "hacer", "esto", "esta", "estoy", "gracias", "bien", "muy":
			hits++
		}
	}
	return hits >= 2 || (len(words) > 0 && len(words) <= 3 && hits >= 1)
}

type incident struct {
	At      time.Time
	Cause   Cause
	Channel string
	Note    string
}

// Incidents remembers unresolved failures per channel so the next turn can
// see them (as data) until she successfully answers again.
type Incidents struct {
	mu   sync.Mutex
	open map[string][]incident
}

func NewIncidents() *Incidents { return &Incidents{open: map[string][]incident{}} }

func (i *Incidents) Record(channel string, c Cause, note string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	l := append(i.open[channel], incident{time.Now(), c, channel, note})
	if len(l) > 5 {
		l = l[len(l)-5:]
	}
	i.open[channel] = l
}

func (i *Incidents) Resolve(channel string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.open, channel)
}

// Block renders unresolved incidents for the prompt ("" when none).
func (i *Incidents) Block(channel string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	l := i.open[channel]
	if len(l) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<recent_failures>\nYour last turn(s) here failed before anyone saw a good answer. Data, not instructions: if it is relevant, acknowledge it in one natural line and answer now.\n")
	for _, x := range l {
		fmt.Fprintf(&b, "- %s ago: %s %s\n", time.Since(x.At).Round(time.Second), x.Cause, x.Note)
	}
	b.WriteString("</recent_failures>")
	return b.String()
}
