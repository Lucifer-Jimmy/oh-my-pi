# oh-my-pi

**A general-purpose agent engine in Go. One engine, three shells: TUI, GUI, SDK.**

`omp` runs a language model in a loop against a real repository — it reads code, edits it,
runs commands, reads the results, and repeats until the task is done — and exposes that loop
as an embeddable engine so other programs can build agent applications on it.

```text
$ omp -p "why does the truncation gate exist?"
Working...
When the model's response hits its output token limit, the tool arguments it emitted
may be half-written...
```

## Status

Early but usable. The kernel, model layer, built-in tools, permission system, public SDK
packages, the interactive terminal UI and the headless JSON / RPC modes are built and tested.
See [docs/ROADMAP.md](docs/ROADMAP.md) for the live table.

| Built | Milestone |
|---|---|
| Neutral model + Anthropic / OpenAI (Completions & Responses) adapters | M0 ✅ |
| Loop, scheduler, registry, contracts, panic + truncation boundaries | M0 ✅ |
| `cmd/omp`: print mode, config file, `!command` key resolution | M1① ✅ |
| System prompt v1, `read` / `glob` / `grep` | M1 ✅ |
| Public SDK surface, edit, write, bash, permission, context files | M2 ⓪–⑤ ✅ |
| Tool metadata, plan mode, streaming dispatch, verifier loop, Windows acceptance, argument validation | M2 ⑥–⑪ ✅ |
| Interactive TUI on a shared reducer | M3 ✅ |
| Elide-first compaction, session persistence, `--continue` / `--resume`, retry classification | M6 ✅ (pulled forward) |
| Baseline feature set (todos, subagents, skills, MCP, hooks, background) | M4 ✅ |
| Trace, run summary, live progress + eval baseline (cost out of the plan, D68) | M5 ✅ |
| SDK contract frozen, headless RPC | M7 ✅ |
| Goal mode: `/goal <objective>` keeps working until the model proves it done (D70) | M9① ✅ |
| Long-term memory: `learn` lessons plus a summary of past sessions, injected at session start (D71) | M9② ✅ |
| GUI on the same contract | M8 |
| Checkpoints, parallel agents and the rest of the [gap ledger](docs/ROADMAP.md#gap-ledger) | not built |

## Install (from source)

```sh
go build -o omp ./cmd/omp
```

Requires Go 1.27+. Windows, macOS and Linux.

## Configure

Credentials come **only** from `~/.omp/config.yml` — never environment variables, never
flags. A value is either the key itself or `!command`, which is executed directly (no
shell) and whose trimmed stdout is the key.

```yaml
providers:
  anthropic:
    api: anthropic-messages
    baseUrl: https://api.anthropic.com
    apiKey: "!powershell -NoProfile -File C:/Users/me/key.ps1 anthropic"
  deepseek:
    api: openai-completions
    baseUrl: https://api.deepseek.com/v1
    apiKey: "!powershell -NoProfile -File C:/Users/me/key.ps1 deepseek"
modelRoles:
  default: anthropic/claude-opus-5-5
memory:
  backend: local        # optional: long-term memory per project, off by default
```

Then:

```sh
omp                                   # interactive terminal UI with the default model
omp "fix the failing test"            # interactive, starting with that message
omp -p "summarize agent/loop.go"      # answer once and exit
omp -p --model deepseek/deepseek-chat "explain the scheduler"
```

In the terminal UI: Enter sends (while a turn runs it steers that turn), Shift+Enter adds a
line, Esc interrupts, Shift+Tab cycles the approval mode, a leading `/` opens the command
palette and `/help` lists every command.

`--model <provider>/<model>` picks up `api`, `baseUrl` and `apiKey` from the config
provider; a bare model id uses `--api` (default `anthropic-messages`). Without `--model`,
`modelRoles.default` is used. `--base-url`
overrides. Two protocols cover the ecosystem: anything OpenAI-compatible (DeepSeek, Qwen,
GLM, Kimi, vLLM, Ollama, OpenRouter) is `openai-completions` or `openai-responses` plus a
base URL.

## Documentation

| Document | What it answers |
|---|---|
| [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) | What the product must do, with IDs and non-goals |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | The laws: layering, contracts, anti-patterns |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Milestones, acceptance tests, gap ledger |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Decision log — its index is the current truth |
| [docs/STATUS.md](docs/STATUS.md) | What exists in the tree right now |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | How work proceeds, and how a slice is verified |

## Design in one paragraph

`cmd/omp` wires a provider, a registry and a loop together — the only package that knows
all others. The loop streams one assistant message, schedules its tool calls (parallel
when a tool declares itself read-only and concurrency-safe, exclusive otherwise, results
always in call order), appends the paired results, and repeats until the model stops.
Approval is middleware wrapped around every tool at registration time, so a dynamically
added MCP tool cannot bypass it. Compaction rewrites only what is sent to the model, never
what is stored. Sessions are append-only JSONL logs that repair themselves on load.

## License

Not yet chosen.
