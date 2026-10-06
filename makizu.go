// Package makizu embeds the default .makizu directory (personas, rules,
// skills) that `mak1zu init` copies into a new install. Only these folders are
// embedded, never config.json, .env or data/, so a developer's local secrets
// cannot end up inside a released binary.
package makizu

import "embed"

//go:embed .makizu/README.md .makizu/personas .makizu/rules .makizu/skills .makizu/servers .makizu/channels
var Defaults embed.FS
