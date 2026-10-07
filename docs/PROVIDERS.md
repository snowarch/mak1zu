# Providers

Mak1zu talks to any endpoint that speaks the OpenAI API (`/chat/completions`, or `/responses` for the few models that only have that). A provider is a base URL, a model id and a key. Everything below is that, with the keys fetched for you.

Checked on 2026-10-07. Model lists, free tiers and prices move weekly; `mak1zu doctor` tells you when one of these went stale.

## Pick one

```bash
mak1zu providers                  # every preset: cost, default model, where the key lives
mak1zu init --provider <id>       # or run `mak1zu init` and pick from the menu
```

| Preset | Cost | Key from | Notes |
| --- | --- | --- | --- |
| `openrouter-free` | free, rate-limited | [openrouter.ai/keys](https://openrouter.ai/keys) | Many `:free` models behind one key. [Details](#openrouter) |
| `cline` | free promos, then pay as you go | [app.cline.bot](https://app.cline.bot) → Settings → API Keys | Hundreds of models behind one key, a few free at any time. [Details](#cline) |
| `opencode-go` | subscription | [opencode.ai](https://opencode.ai) | Open-weight models (DeepSeek, GLM, Qwen, Kimi, MiniMax). Works from any client. [Details](#opencode) |
| `opencode-zen` | pay as you go | [opencode.ai](https://opencode.ai) | Needs credits. Its free models do **not** work outside the OpenCode app. [Details](#opencode) |
| `openrouter` | pay as you go | [openrouter.ai/keys](https://openrouter.ai/keys) | Same key as the free one, any paid model. |
| `gemini` | free tier or pay as you go | [aistudio.google.com/apikey](https://aistudio.google.com/apikey) | Limits on Google's pricing page. |
| `groq` | free tier or pay as you go | [console.groq.com/keys](https://console.groq.com/keys) | Fast. Limits on Groq's console. |
| `deepseek` | pay as you go | [platform.deepseek.com](https://platform.deepseek.com/api_keys) | Cheap. |
| `mistral` | free tier or pay as you go | [console.mistral.ai](https://console.mistral.ai/api-keys) | |
| `openai` | pay as you go | [platform.openai.com](https://platform.openai.com/api-keys) | |
| `anthropic` | pay as you go | [console.anthropic.com](https://console.anthropic.com/settings/keys) | Through Anthropic's OpenAI-compatible endpoint. |
| `ollama` | free, your machine | none | [Details](#running-it-locally) |
| `lmstudio` | free, your machine | none | [Details](#running-it-locally) |

The key goes in `.makizu/.env` under the variable `mak1zu providers` prints (`mak1zu init --provider X` writes the line for you, empty). It is read automatically, never printed, never sent anywhere but that provider.

## Free, and what free costs you

A companion hears private things. Free endpoints are paid for somehow, usually with your prompts or with a quota that vanishes. Read this before you put a free model in a DM.

- **OpenRouter free models** may train on what you send. OpenRouter has a privacy setting for whether your account can use endpoints that do; check it.
- **Free quotas rotate.** A model that is free this week can be gone next week; `mak1zu doctor` reports a retired model with the provider's own replacement list.
- **Local models** (below) are the only option where nothing leaves your machine.

## OpenRouter

One key, hundreds of models, a handful free (ids ending in `:free`).

1. Sign in at [openrouter.ai](https://openrouter.ai), open **Keys**, create one.
2. `mak1zu init --provider openrouter-free`, put the key in `.makizu/.env`, run `mak1zu doctor`.

The preset uses `openrouter/free`, a router that picks a free model that fits the request. To pin one, set `model` to any id ending in `:free`. At the time of writing, `google/gemma-4-31b-it:free` accepts images and tools. List them yourself, no key needed:

```bash
curl -s https://openrouter.ai/api/v1/models | jq -r '.data[] | select(.id|endswith(":free")) | .id'
```

Limits for free models: 20 requests a minute, and a daily cap that depends on whether you have ever bought credits (50 a day until you buy about $10, 1,000 after). Ask OpenRouter what your key has left: `curl -s -H "Authorization: Bearer $OPENROUTER_API_KEY" https://openrouter.ai/api/v1/key`. A negative credit balance breaks free models too. ([limits](https://openrouter.ai/docs/api-reference/limits); the table itself is built from variables on that page, so the figures above come from OpenRouter's published numbers, not from a count I ran.)

## Cline

Cline sells access to many models through one OpenAI-compatible endpoint, and runs rotating free promotions.

1. Sign in at [app.cline.bot](https://app.cline.bot), **Settings → API Keys → Create API Key**. The key is shown once.
2. `mak1zu init --provider cline`, put the key in `.makizu/.env`, run `mak1zu doctor`.

Base URL `https://api.cline.bot/api/v1`. The model list is public:

```bash
curl -s https://api.cline.bot/api/v1/models | jq -r '.data[].id' | grep ':free$'
```

Free models are "limited-time promotions with a usage quota"; after the quota you switch to credits or the $9.99/month ClinePass ([docs](https://docs.cline.bot/getting-started/free-models)). The preset's default is one of the `:free` ids from that list.

## opencode

Two different products share the `opencode.ai` account. Sign up on the web, create a workspace, copy the API key. Mak1zu already sends the headers opencode wants (an honest `User-Agent` and a per-process `x-opencode-session`), so there is nothing to configure.

**Go** (`opencode-go`, `https://opencode.ai/zen/go/v1`) is the subscription, and it works from any client. Verified from Mak1zu on 2026-10-07: `deepseek-v4-flash` (the preset), `glm-5.3-flash`, `qwen3.8-flash`, `kimi-k3`, `minimax-m3` and `longcat-2.5-preview-free` all answered. List what your key can use:

```bash
curl -s https://opencode.ai/zen/go/v1/models | jq -r '.data[].id'
```

Go is built for coding agents, and its docs say traffic is monitored for abuse and that clients should send their own `User-Agent` and a stable `x-opencode-session`, which Mak1zu does. A chat companion is not typical coding-agent traffic, so read [their terms](https://opencode.ai/docs/go) before you lean on it. Usage limits are dollar amounts per 5 hours, week and month, shared across models.

Two things to know: some Go models only speak the `responses` protocol (set `"protocol": "responses"` for those), and `minimax-m3` thinks out loud inside `<think>` tags, which Mak1zu strips.

**Zen** (`opencode-zen`, `https://opencode.ai/zen/v1`) is pay-as-you-go. Its price list has several models marked Free. I called `big-pickle` and `nemotron-3-ultra-free` from Mak1zu on 2026-10-07, with and without a valid key and the session header: both returned `403 FreeTierError: OpenCode's free tier can only be used from within OpenCode`. Two more free ids were already dead (`mimo-v2.5-free` answers 410 deprecated, `deepseek-v4-flash-free` answers 400 unavailable). Mak1zu does not pretend to be the OpenCode app to get around that, and you should not need to. If you want Zen, add credits and use a paid model. I did not run a paid Zen model, because my test key is a Go key. GPT models on Zen use `/responses`, so set `"protocol": "responses"` for them.

## Running it locally

Nothing leaves your machine, and there is no key.

```bash
ollama pull llama3.2                 # any model you like
mak1zu init --provider ollama
```

LM Studio: load a model, start its local server (default port 1234), then `mak1zu init --provider lmstudio` and set `model` to the id LM Studio shows. llama.cpp's `llama-server` and vLLM also speak this API: use any preset and change `base_url` and `model`.

The first message can take a while while the model loads. If `doctor` reports a timeout, raise `timeout_seconds`.

## Anything else

Any OpenAI-compatible gateway works. In `.makizu/config.json`:

```json
"llm": {
  "providers": {
    "main": {
      "enabled": true,
      "base_url": "https://your-gateway.example/v1",
      "model": "the-model-id",
      "protocol": "chat",
      "api_key_env": "MY_GATEWAY_KEY",
      "vision": false
    }
  },
  "routing": { "text": ["main"], "vision": ["main"] }
}
```

Or add it in the panel: **Models → Add a provider**, pick any preset, change the URL and the model. Reasoning models spend completion tokens thinking; if `doctor` says the model "answered with nothing visible", set `reasoning_headroom` to 1000 or more.

## Fallbacks

`routing.text` is an ordered list. If the first provider fails, the next one answers; a circuit breaker stops hammering one that is down. A good shape: a free model first, a cheap paid one second, so a quota running out is not a silent bot.

```json
"routing": { "text": ["free", "paid"], "vision": ["paid"] }
```

## When it breaks

`mak1zu doctor` makes one real call and says what went wrong. The usual ones:

| It says | It means | Do |
| --- | --- | --- |
| `no key: the environment variable X is empty` | `.makizu/.env` has `X=` with nothing after it | paste the key after the `=` |
| `rejected the key (401)` | wrong, expired, or no access to that model | make a new key; check the model id |
| `rate limited or out of quota` | free quota used up, or no balance | wait, add credit, or add a fallback |
| `free-tier model that only works inside the app` | an OpenCode Zen free model | use Go or a paid Zen model |
| `has been retired` | the provider removed the model | pick from the list `doctor` prints |
| `blocked the request (bot protection)` | a VPN or datacenter IP | try another network |
| `nothing answered at localhost:11434` | the local server is not running | `ollama serve` |

OpenAI's newer models refuse `max_tokens` and any `temperature` except their default. Mak1zu reads that refusal, switches the parameter and remembers it, so the `openai` preset works without you touching a setting.

## Sources

- opencode Zen docs and model list: [opencode.ai/docs/zen](https://opencode.ai/docs/zen), `https://opencode.ai/zen/v1/models`, `https://opencode.ai/zen/go/v1/models`
- OpenRouter: [limits](https://openrouter.ai/docs/api-reference/limits), `https://openrouter.ai/api/v1/models`
- Cline: [API overview](https://docs.cline.bot/api/overview), [free models](https://docs.cline.bot/getting-started/free-models), `https://api.cline.bot/api/v1/models`
- Model retirements: [Gemini deprecations](https://ai.google.dev/gemini-api/docs/deprecations), [DeepSeek API docs](https://api-docs.deepseek.com/), [OpenAI models](https://developers.openai.com/docs/models)
