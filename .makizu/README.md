# .makizu

This folder is her. Everything here is plain Markdown you can edit by hand or
from the panel (**Persona** and **Rules & skills** tabs). She re-reads it on
every message, so changes apply without a restart.

| Path | What it is |
| --- | --- |
| `personas/<id>/persona.md` | Who she is. Pick the active one in the panel. |
| `rules/*.md` | House rules, always in her prompt, in filename order. `enabled: false` in the front matter switches one off. |
| `servers/<server-id>.md` | Rules for one server only (the numeric Discord ID). |
| `channels/<channel-id>.md` | Rules for one channel only. |
| `skills/<name>/SKILL.md` | Know-how she reads on demand. The `description:` line is what she sees in her catalog; the body is loaded with `read_skill` when a request matches. Optional `references/*.md` for long material. |
| `config.json` | Settings (models, behavior, channels). Never commit it. |
| `.env` | Secrets (API key, Discord token). Never commit it. |
| `data/` | Memory database, telemetry, her workspace. Never commit it. |

Rules and skills are your words to her: they outrank her habits and moods but
never her hard lines (no doxxing, no malware, no real harm). Keep rules short
and specific; a page of vague rules does less than three sharp ones.
