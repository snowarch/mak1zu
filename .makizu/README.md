# .makizu

This folder is her. Everything here is plain Markdown you can edit by hand or
from the panel (**Persona** and **House rules** tabs). She re-reads it on
every message, so changes apply without a restart.

| Path | What it is |
| --- | --- |
| `personas/<id>/persona.md` | Who she is. Pick the active one in the panel. |
| `rules/*.md` | House rules, always in her prompt, in filename order. `enabled: false` in the front matter switches one off. |
| `servers/<server-id>.md` | Rules for one server only (the numeric Discord ID). |
| `channels/<channel-id>.md` | Rules for one channel only. |
| `skills/<name>/SKILL.md` | Know-how she reads on demand. The `description:` line is what she sees in her catalog; the body is loaded with `read_skill` when a request matches. Optional `references/*.md` for long material. |
| `.defaults.json` | What `mak1zu init` installed, so `mak1zu init --update` can tell your edits from new defaults. Leave it alone. |
| `config.json` | Settings (models, behavior, channels). Never commit it. |
| `.env` | Secrets (API key, Discord token). Never commit it. |
| `data/` | Memory database, telemetry, her workspace. Never commit it. |

Rules and skills are your words to her: they outrank her habits and moods but
never her hard lines (no doxxing, no malware, no real harm). Keep rules short
and specific; a page of vague rules does less than three sharp ones.

## Getting newer defaults

New versions of Mak1zu improve the shipped personas, rules and skills, but an
install never changes on its own. Run `mak1zu init --update` (add `--dry-run` to
look first). Files you never touched move to the new version, new files are
added, and anything you edited is left exactly as it is: if the release changed
that file too, the new version is saved under `.updates/` so you can compare with
`diff -u`. `--force` takes the new version anyway and keeps your old copy in
`.backup/`. `mak1zu doctor` tells you when something is waiting.
