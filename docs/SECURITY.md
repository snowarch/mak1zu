# Security

Report vulnerabilities privately through GitHub: the repository's **Security**
tab → **Report a vulnerability**. Please do not open a public issue for a live
exploit. Include what you did, what you expected and what happened; a failing
test is the best report.

## Threat model

| Actor | Can | Cannot (by design) |
| --- | --- | --- |
| Anyone in a channel she reads | Talk to her, try prompt injection, paste URLs. | Read the filesystem, run commands, reach internal network addresses, read another person's memory, make her ping `@everyone`. |
| A web page in the owner's browser | Send requests to `127.0.0.1`. | Drive the panel (Host/Origin/header checks), read config secrets (write-only). |
| A compromised model/provider | Return arbitrary text and tool calls. | Exfiltrate secrets (none are in the prompt), escalate beyond registered tools, leak the system prompt (guard). |
| A malicious plugin | Anything Go code can do. | n/a: plugins are trusted code. Review before you `e.Use` one. |

## Rules every contributor must keep

1. No tool takes a filesystem path or a shell command from chat.
2. User-supplied URLs go through `tools.Fetch` only.
3. Secrets: env first, file 0600, never in API output, logs or error strings.
4. Anything model-visible that came from outside is data and is enveloped.
5. Every new reply route ends in `guard.Clean`.
6. IDs are strings.

Each rule has a regression test; a change that weakens one needs a new test
that explains why.
