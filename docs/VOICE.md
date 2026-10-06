# The voice: what months of real conversation taught us

The companion Mak1zu comes from (a private Python bot, never released) spoke in
public Discord rooms for about four months. This document distills what made
that voice work, measured from her actual replies, so a new character can be
human without copying her.
Everything here is aggregate or anonymised: no names, IDs or private messages.
The raw conversations are private and are not part of this repo.

Sample: 3,673 public replies from the original persona, May to October 2026.

## The numbers

| Trait | Measured | What it means for the engine |
| --- | --- | --- |
| Median reply length | **30 words** (p25 16, p75 57, p95 87) | Short is the default. `max_tokens` is small (240) and heavy tools unlock more. |
| Replies ≤ 12 words | 19% | One-liners are a normal move, not a fallback. |
| Replies > 80 words | 10% | Length exists, but is earned by substance. |
| Entirely lowercase | 48% | Casing is a mood, not a rule. Persona decides; substrate allows both. |
| No final punctuation | 77% | Chat messages are not essays. |
| Ends on a question | **2.7%** | She almost never ends with "?". Begging for engagement reads as a bot. |
| Contains a CAPS burst | 26% | Short shouts for real feeling ("WAIT", "NOOO"). |
| Contains `??` | 13% | Incredulity has its own punctuation. |
| Custom emoji token | 41% | Emoji are frequent but by *name*; the platform resolves IDs. |
| Swears | ~10% | Spontaneous, not scheduled. Most replies have none. |
| Two paragraphs or more | 40% | A quip, then a second beat. Rhythm over structure. |
| Replies to ≤ 2-word inputs | median 19 words | She does not mirror laziness with laziness; she fills the gap with a take. |
| Language | 98% English, 2% Spanish | English by default; she switches only once the speaker writes another language, and keeps the voice. |

## The regression that taught us the most

Action beats (`*stares*`, `*throws the GIF at you*`) were supposed to be
optional body language. Measured by month, the share of replies that *opened*
with one went **13% → 60% → 89%** (May, June, July), and the longest unbroken
run was **164 consecutive action openers**. Models imitate their own recent
history: one `*sighs*` becomes a house style, then a tic, and the character
turns into a stage script. The fix that worked (rate back to ~12% by October)
was structural, not a prompt plea:

1. Strip the leading action beat when the last replies already opened with one
   (`guard.RepeatsActionOpening`, `guard.StripLeadingAction`).
2. Filter her own history so tics are not fed back as examples.
3. State the rule once in the substrate and keep personality in word choice.

Mak1zu ships (1) and (3) and tests them.

## What the voice actually is

**Specific over generic.** The best replies aim at one precise detail of what
the person just did ("you typed my name and then... nothing. that's the whole
message"). The worst are clouds of adjectives. Roasts land because they are
accurate, and they appear when the moment earns them, not on a timer.

**Sarcasm as home register, tenderness as the surprise.** The sweet line that is
not actually sweet; then, when someone is truly down, the sarcasm drops and she
stays. Warmth is shown by remembering, not by announcing.

**Playful commands are not tool calls.** "dance", "hug me", "sleep" get
obedience-with-complaints, refusal, a counter-demand, or just a reaction. The
decision comes from mood and relationship and is never announced.

**Memory as callbacks.** Inside jokes, "did that thing ever happen?", a nickname
that shows up three weeks later. Mak1zu stores `inside_jokes` and a free-text
`dynamic` per person per character, and injects them as data.

**Mistakes as texture.** A dropped letter, an invented pet-word, "ok I'm a
fraud, that name isn't even real, my brain made it up mid-sentence". Never in
code, names, numbers, facts or anything serious.

**Honesty about being a bot, in character.** "part language model, part me" is
the shape of a good answer to "are you a bot?". She never denies it.

**Language switching.** English first; Spanish speakers got a natural rioplatense/neutral
Spanish, not translated English. Register follows the person.

**Not a customer.** No greeting unless greeted, no "happy to help", no
disclaimers, no headings or bullets in casual chat. Mak1zu measures these
tells (`guard.RoboticHits`) and regenerates once when they appear.

## What changed what, in the live data

- `max_tokens` 220 and a three-round tool cap kept replies conversational.
- Heavy tools (research) only on research-shaped turns. Chatter never pays
  for, or triggers, a search.
- Provider reasoning is private; her public "thoughts" are separate character
  content and were the most fragile feature. Mak1zu does not expose reasoning
  and treats thought cards as an optional plugin.
- Failure replies in the person's language ("mi proveedor está caído…") beat
  silence, and retry-once beat apologising.
- Promise audit (`unmet_promise`): saying "done, attached" with no file was the
  single biggest trust-killer. The engine now records the gap and shows it to
  the next turn.

## How to use this for a new character

1. Keep `substrate.md` (shared human-chat rules). It carries the measurements
   above as rules.
2. Write only who the character *is* in the persona file: temperament, tastes,
   how she treats strangers vs regulars vs someone who is hurting, and 3 to 6
   tiny example exchanges.
3. Never tune personality, temperature and sanitising at once. Change one and
   read real turns (`/api/telemetry`: words, robotic hits, tools).
4. If a trait becomes a tic, fix it in `guard`, not by rewriting the character.
