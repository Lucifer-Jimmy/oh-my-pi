# Status — what exists in the tree

> A literal inventory, verified against the working tree. Updated at the end of each
> milestone. Roadmap state lives in [ROADMAP.md](ROADMAP.md); this file answers only
> "what is on disk and does it pass".

Live smoke (2026-11-17): `omp -p --trace …` answered through the configured gateway —
`anthropic-messages`, model `mify/volcengine_maas/deepseek-v4.1-flash` — with the trace showing
the full header → text_delta → usage → result → summary sequence (2025 in / 3 out) and the
session persisted.

Last verified: 2026-11-25 — `go build ./...`, `go vet ./...`, `gofmt -l .` clean and
`go test ./... -race` green across 26 packages (730 tests, ~39.1k lines of Go with tests);
`staticcheck` and `deadcode -test` find no unused code.
Milestone state lives in [ROADMAP.md](ROADMAP.md): M0–M7 are closed, M5 with a run summary and
live progress in place of the cost line (D68); of M9, goal mode (D70) and long-term memory (D71)
are built. M8 was stopped by the user on 2026-11-24: the agent core was consolidated (D72) and the
TUI is being polished before any new milestone.

---

## Packages

| Path | Contents | Source / test lines | Tests |
|---|---|---|---|
| `llm` | Neutral message, block, usage and stream model; accumulator; `Message.Timestamp` and `Message.Usage` (local only, never sent); `IsContextOverflow`; retry classification (D62) — `HTTPError`, `Classify`, `Retrying` with backoff, `Retry-After` and the never-replay rule | 677 / 722 | 34 |
| `llm/anthropic` | SDK translator, tool schemas on the wire, cache breakpoints from `CacheBoundary`, server error surfacing, base URL `/v1` normalisation | 546 / 751 | 23 |
| `llm/openai` | SDK translator for `completions` and `responses` | 823 / 777 | 29 |
| `agent` | One run is one value (D72): `loop.go` drives it and holds the stop chain, `stream.go` talks to the model, `exec.go` runs one tool call and collects results in call order, `compact.go` compacts; `Runner` and `RunnerFunc` beside `Loop`. `Tool` contract, single-table `Registry`, streaming scheduler — a tool starts when its argument block closes, non-safe tools run alone and block the queue behind them, results in call order (D51); loop with truncation gate and panic boundary; arguments checked against the tool schema before every call (D46); compaction seams — `Compactor`, `RunDeps.Compactor`, auto and overflow triggers, `Loop.Compact` (D47); verifier seam — `Verifier`, `VerifierRunner`, `stop_reason: verifier` (D50); one `tool_result` construction point; events carry `Seq`, `Timestamp`, the declared `Approval` and compaction records; a tool result carries its measured execution time, a usage event the response's stop reason and the model call's time, and an event a tool emits its call's id (D68); a prompt hook that fails part-way still injects what it returned (D70) | 1881 / 2298 | 66 |
| `verifier` | The v1 external check for the verifier loop (D50): a shell command in the workspace, bounded output with the cut marked, a timeout counted as a failed check, cancellation propagated, tree kill per OS | 108 / 125 | 5 |
| `goal` | Goal mode (D70): `Tracker` (one goal per session — status, optional token budget, tokens and seconds used, `omp`'s accounting without cache reads), the `goal` tool (`create`, `get`, `resume`, `complete`, `drop`), `Runner` that wraps any runner and starts continuation runs while the goal is active and stops on completion, budget, pause, plan mode or a turn without progress; prompts that escape the objective; the `/goal` subcommand parser | 777 / 632 | 19 |
| `memory` | Long-term memory (D71): a per-project `Store` under `~/.omp/memory/` named like the session directory; `learned.md` with `omp`'s rules (newest first, deduplicated, 100 lessons, sanitized, redacted); the `learn` tool; the two-phase `Pipeline` — per-session JSON extracts of idle past sessions, then one merged `memory_summary.md` — with retries, a per-run cap and a cross-process lock; `Guidance`, the injected block with a 5000-token budget; `Redact`, `omp`'s secret patterns; `Control` behind `/memory` | 981 / 573 | 21 |
| `permission` | Rule lists, four modes, deny-first `Evaluate`, `SplitCommand`, path globs, shared `Policy` with session approvals and `DeniesPath`, `Wrap` middleware that reads tool metadata into the approval text (D36, D41, D44); an approval ask carries the tool's name and arguments (`AskRequest.Tool`, `Input`) for a shell that can render them, and the argument preview separately in `Detail` (D74) | 754 / 942 | 58 |
| `prompt` | Two-block system prompt generated from the registered tool set; `<project-context>` with cwd, OS, date, shell and `<repo-rules>` from `AGENTS.md`/`CLAUDE.md` | 210 / 282 | 22 |
| `plan` | Plan mode: the reminder prefixed to each input, and the `exit_plan_mode` tool that asks for approval and switches the mode (D43) | 145 / 175 | 7 |
| `presentation` | Reducer: presentation state built from engine events (D39); compaction notices (D47); a tool's measured time and the live progress of a running tool, dropped when it finishes or is cancelled (D68); `Replay` rebuilds the state from stored messages, hiding blocks whose tags the assembly root lists as injected (D73) | 357 / 486 | 16 |
| `compaction` | Compaction (D47): byte estimate, provider-usage measurement, `omp` thresholds, elide-first projection with recovery hints, structured summaries with an update path, overflow-shrinking retries | 534 / 331 | 11 |
| `tui` | Bubble Tea v2 shell: committed scrollback + live tail, approval dock, queue, slash commands including `/compact`, `Host` bridge; the plan printed above the dock (D43); markdown parsed by goldmark and laid out here — CJK-aware emphasis and wrapping, fitted tables, task lists, finished blocks committed while the answer streams (D67); a running subagent card shows the child's current tool and tokens, a finished card the tool's measured time, and the status bar the context as a share of the window when it is known (D68); `/goal` and a goal segment in the status bar (D70); `/memory` (D71); `Runner` is an alias of `agent.Runner`; markdown parsing comes from `markdown`; a resumed session replays its transcript at startup and on `/resume` (D73); approvals show a line diff (an overwrite against the file on disk), Ctrl+O opens a full-screen transcript, a free-form question has an answer box with a real cursor, choice lists of any length scroll (D74); headings set apart with `###` kept from level 3, tables that keep short cells whole, wrapped code lines marked `↳`, URLs and paths broken after a separator, and a status bar that shortens the model name and drops fields by rank instead of cutting them (D75); `/resume` opens a picker whose prompt line is the filter — ↑↓/PgUp/PgDn over a window of eight, Enter resumes, Esc closes, the current session is marked and never reloaded — the command palette's window follows its selection, Esc closes the picker, leaves history, interrupts a run or clears the draft, and a queue is capped at three dock rows (D76) | 3661 / 2504 | 100 |
| `markdown` | The CommonMark/GFM parser shared by the shells, with the CJK-aware emphasis rule of D67, and an HTML rendering that drops raw HTML and dangerous link targets | 113 / 115 | 3 |
| `gui` | **Parked.** An M8 start (local HTTP server, token cookie, SSE state frames, embedded page) that the user stopped on 2026-11-24; not wired into the binary and not a feature. Kept until the user decides | 1013 / 769 | 10 |
| `subagent` | The `task` tool (M4②, M4⑧): runs a child loop over a registry without `task`, `exit_plan_mode`, `background` or `goal` (`Inherits` is the one list); the child gets no steering, no compactor and does not drain the parent's background notifications; `isolated` runs it in a temporary git worktree and applies the resulting patch to the parent tree, keeping the patch on conflict; the child's tool calls and tokens are reported as progress while it runs (D68) | 403 / 637 | 19 |
| `skill` | Declarative skills (D56): `SKILL.md` discovery at `.omp/skills/*` and `~/.omp/skills/*` with frontmatter parsing, one prompt line each, and a `skill` tool that returns a body on demand; CRLF and project-before-user precedence tested | 199 / 141 | 7 |
| `background` | Background commands (D59): a manager over real subprocesses with per-task log files, `start` / `wait` / `list`, pointer-only notifications drained by the kernel; tested against real processes | 331 / 186 | 8 |
| `mcp` | MCP client over the official SDK — stdio (D57), streamable HTTP and SSE by `url` (D66): `Pool.Connect` / `Server.Tools`, sanitised and capped `mcp__<server>_<tool>` names, write tier and open-world by default; `ServeStdio` hosts a server for tests and embedders | 271 / 232 | 10 |
| `hook` | Command hooks (D58): four families, each a subprocess exchanging one JSON object, timeout and output caps, a failing observer never blocks a tool | 219 / 201 | 10 |
| `eval` | Scripted-provider golden suite scoring answer and trajectory (M5), 15 tasks, publishes the baseline | 544 / 92 | 4 |
| `trace` | One NDJSON file per run (D54): header, every event, and a summary that folds the run — requests and their time, truncations, compactions, calls, errors and time per tool, peak context and its share of the window, turns and usage across every run in the file (D68) | 232 / 218 | 6 |
| `cmd/omp` | Assembly root, split by responsibility (D72): `dispatch.go` builds one `app` and every mode takes its runner from `app.stack` (loop → plan reminder → verifier → goal → trace), `assemble.go` the registry and tools, `provider.go` the provider and model switch, `runner.go` the stack's layers, `interactive.go` the TUI's session operations. Also: config, first-run wizard, flags, conversation, dispatch, the combined M4 acceptance (todos, subagent in a worktree, skill, MCP tool, hooks and a background command in one session), the versioned wire (`--mode json`, `--mode rpc`, every command-line prompt run in order in both), external tool manifests and the embedding example; wire frames carry tool and request timings and stop reasons (D68); goal mode wired into every mode, persisted in the session and restored paused (D70); `memory.backend`, the `learn` tool and the memory block when it is on, the background update at TUI start (D71) | 2397 / 2677 | 68 |
| `tools` | The built-in tools — `read`, `glob`, `grep`, `edit`, `write`, `bash`, `todo`, `ask` — with shared path resolution, truncation markers and read-before-write `FileState`, declared metadata (D44) and the offline Windows acceptance (`acceptance_test.go`) | 2231 / 2251 | 142 |
| `sysproc` | `Prepare` for child processes: its own process group on Unix, a hidden window and a `taskkill /T /F` tree kill on Windows, so cancelling a context ends the whole tree | 38 / 42 | 2 |
| `session` | `Session` contract (append, messages, compaction records), in-memory implementation (D47); append-only `title` entries for rename, markdown export and guarded delete (D60); append-only `goal` entries, the last one restored on load (D70); `Read`, a replay that never opens the file for writing (D71); append-only JSONL `Store` with a version header, `parentId` line, torn-tail and orphan repair with an `outcome unknown` result for dangling calls, latched write errors, `List`/`Latest`, per-project directory encoding (D49) | 698 / 777 | 30 |

Total: about 39,087 lines of Go including tests. Counted 2026-11-25 per package directory
(`*.go` against `*_test.go`, tests as `func Test` declarations); earlier rows used a different
count, so a row's change is not all new code. The binary is `omp` (D37); everything user-level —
`config.yml`, `sessions/`, `background/`, `skills/`, `memory/` — lives in `~/.omp/` (D69).

## Behaviour available today

- `omp` in a terminal starts the interactive TUI; `omp "prompt"` starts it with a first message;
  `omp -p "prompt"` (or piped stdin) answers once and exits; `--mode json` and `--mode rpc` speak
  the versioned NDJSON wire (D64). Built-in tools: `read`, `glob`, `grep`, `edit`, `write`,
  `bash`, `ask`, `todo`, plus `exit_plan_mode`, `goal`, `task` and `background`; `skill` and MCP
  tools appear when configured.
- `--model` is optional when `modelRoles.default` is set in `~/.omp/config.yml` (D40). Either may
  end in `:level` (`mygw/m1:high`); `--thinking` wins over the suffix, the suffix over a
  top-level `defaultThinkingLevel` (D42).
- `/model <provider>/<id>` switches the provider mid-session: the key is re-resolved from the
  config file, the provider is swapped under the same wrapper, and the compaction window follows
  the new model (built-in table, D55). `/effort <level>` changes the thinking level; both apply
  from the next request.
- `providers.anthropic` / `providers.openai` need only `apiKey`; other providers need `api` and
  `baseUrl`. Only the selected provider entry is decoded, so `omp` settings under `providers:`
  and broken entries for other providers are ignored. An Anthropic `baseUrl` may end in `/v1`.
- In the TUI: answers stream in as rendered markdown (headings, emphasis, lists and task lists,
  quotes, tables, code) and each finished block becomes normal terminal scrollback; a tool
  card appears the moment the model finishes that call's arguments, so a safe tool can already
  be running while the rest of the message streams (D51); cards carry a result preview and edit
  diffs; Enter while a turn runs **steers** it (the message is drained at the next round
  boundary and joins the running session; it is queued when the session cannot steer); Esc
  interrupts; approvals appear in a dock above the composer (digits / `y` / `n` / Esc) with a
  "don't ask again this session" option; Shift+Tab cycles default → acceptEdits → plan; a
  leading `/` opens the command palette and `/help` lists every command and key; Up/Down recall
  prompts.
- `--approval default|acceptEdits|plan|bypass`, default `acceptEdits`, or `permissions.defaultMode`
  in the config file (the flag wins). `permissions.allow|ask|deny` in `~/.omp/config.yml` take
  rules like `Bash(go test *)` or `Read(**/.env)`; a malformed `permissions` section stops the
  run rather than being skipped (D42). In print mode an ask becomes a deny that tells the model
  how to proceed.
- `acceptEdits` edits files and runs inspection commands (`git status`, `ls`, `cat`) without
  asking; anything that runs code (`go test`, `npm run`, scripts) needs approval or an allow rule.
- `AGENTS.md` / `CLAUDE.md` from the working directory up to the repository root are injected
  into the system prompt, root first.
- Every tool call is checked against the tool's schema before it runs. A missing, misspelled or
  wrongly typed argument goes back to the model as one error naming every problem and the valid
  property names; numbers and booleans sent as strings (`"20"`, `"true"`) are corrected. Built-in
  tools accept no properties beyond their schema (D46).
- `edit` and `write` require the file to have been read and unchanged since; both keep CRLF and
  BOM and write atomically.
- On Windows `bash` uses Git Bash when Git for Windows is installed, otherwise PowerShell; the
  system prompt and tool description name the shell in use.
- The verifier loop (`--verify` / `--verify-attempts`, D50): the run is not reported done until the
  command exits 0; on a failed check the model is re-prompted with its output. Live-verified
  2026-11-17 on a temp repo with a failing test.
- Session operations (M4⑦): `/rename <title>` appends a title (the `/resume` picker shows it),
  `/export [path]` writes the conversation as markdown, `/delete <id>` removes another session and
  refuses the current one. `/resume` with no argument opens a picker: the prompt line filters by
  title or id, ↑↓/PgUp/PgDn move, Enter resumes, Esc closes, and the current session is marked and
  never reloaded (D76).
- Background commands (M4⑥): the `background` tool starts a command, returns its id and log
  path, and the model is woken with the id, status and last lines when it finishes; `wait`
  blocks up to a bound, `list` shows what is running. Full output stays in the log file.
- Hooks (M4⑤): `hooks:` in `~/.omp/config.yml` runs subprocesses for `preTool`, `postTool`,
  `stop` and `prompt`; a pre-tool hook can block or rewrite what runs (before approval), a stop
  hook can keep the turn going, and a broken hook is reported without stopping work.
- MCP (M4④): `mcpServers.<name>` in `~/.omp/config.yml` starts a stdio server (`command`, `args`,
  `env`) or connects to a `url` (streamable HTTP, or SSE with `transport: sse`) at launch; its
  tools appear as `mcp__<server>_<tool>`, ask for approval by default and
  are registered through the same wrapper as the built-ins. A server that fails to start is
  reported once and skipped.
- Skills (M4③): a `SKILL.md` in `.omp/skills/<name>/` (project) or `~/.omp/skills/<name>/`
  (user) contributes one line to the system prompt; the `skill` tool loads the body when the
  model asks for it. Project definitions override user ones; a broken skill is skipped.
- `task` tool (M4②): the model can delegate one self-contained job to a fresh subagent with
  the same tools except `task`; only its final text and counters come back. It keeps a large
  investigation out of this conversation's context.
- `todo` tool (M4①): the model keeps a task list, whole-list per call, at most one item in
  progress. It renders as one plan row above the composer (`● current step (2/5)`); the list
  lives in the tool call, so a resumed session replays it.
- Approvals and tool output (D74): an approval for `edit` shows the line diff it would apply, one for
  `write` the diff against the file it would replace (or the new file's lines), one for `bash` the
  command; Enter never approves. A cut card or dock ends with `… +N lines · ctrl+o shows all`, and
  Ctrl+O opens a full-screen transcript — every tool's whole output, and at the end the whole diff
  of a pending approval — scrolled with the arrows, PgUp/PgDn, `g`/`G`, closed with Esc.
- `ask` tool (M3 ⑤): the model can ask one question mid-run — free-form, or a choice when it
  passes options. With no interactive host the call fails closed and tells the model to state an
  assumption instead.
- Plan mode (Shift+Tab, `/plan`, `--approval plan` or `permissions.defaultMode: plan`): only `read`,
  `glob` and `grep` run; the model is reminded on every input and presents its plan with
  `exit_plan_mode`. The TUI prints the plan, then offers auto-accept edits / approve edits one
  by one / keep planning; approving switches the mode. In print mode the plan becomes the answer
  and nothing is written (D43).
- Goal mode (D70): `/goal <objective>` sets a goal and starts on it; while it is active, every
  run carries the objective and the audit rules, and when a run ends the session continues on its
  own until the model calls `goal` with `complete`. `/goal budget 200k` caps it, Esc or
  `/goal pause` pauses it, `/goal resume` continues, `/goal drop` abandons it, `/goal` shows it,
  and the status bar reads `goal active 12k/200k`. It also stops in plan mode and after an
  automatic turn with no new tool activity. The goal is saved in the session; `--continue` and
  `/resume` bring it back paused. In `-p`, `--mode json` and `--mode rpc` a goal the model creates
  keeps the run going the same way.
- Long-term memory (D71), with `memory: {backend: local}` in `~/.omp/config.yml`: every session
  in a project starts with a `<memory>` block — a summary distilled from its past sessions and
  the lessons saved with the `learn` tool — as advice the repository overrides. When the TUI
  starts a saved session, past sessions idle for 12 hours are read in the background with the
  active model, at most 16 per start, and the summary is rebuilt; `/memory sync` does it now,
  including recent sessions. `/memory` shows the files, the last update and this session's
  block; `/memory clear` deletes it all. Files live in `~/.omp/memory/<project>/`; secrets are
  redacted before anything is written.
- Compaction (D47): with `compaction.contextWindow` set, the loop compacts before a request that
  would exceed `window − max(15%, 16384)`, and once more if the provider reports a context
  overflow. Old tool results become re-read hints first (bash output keeps its last lines and
  exit status); only if that is not enough is the older conversation summarized. `/compact [focus]`
  does it on demand. Stored history is never rewritten; the summary and the elision boundary are
  appended as checkpoint records.
- The eval harness runs offline with a scripted provider (`go test ./eval`): fixture files, a
  scripted turn list, and checks on the final text and on the ordered tool trajectory. Nothing
  touches the network. It found its first real defect on day one — a misspelled tool name got no
  suggestion — now fixed with bounded edit distance in `agent.suggestTools`.
- Traces (`--trace <path>`, D54): every event of every turn is appended as JSONL — a versioned
  header, one line per event, and a summary. The summary (D68) carries the last run's reason;
  turns and the four usage buckets added up across every prompt in the file; the number of model
  requests and their total time; truncated responses; successful compactions; calls, errors and
  milliseconds per tool; and the peak context, with its share of the window when the window is
  known. A write failure is reported once and never breaks the turn.
- Live measurement (D68): a finished tool card shows the tool's own execution time; while `task`
  runs, its card shows the subagent's current tool and tokens; the status bar shows the context
  as a share of the window when the window is known (`ctx 27.0%/200k`). `--mode json` and
  `--mode rpc` frames carry `duration_ms` on tool results, `stop_reason` and `duration_ms` on
  usage, and the subagent's progress.
- A resumed session shows its history (D73): `--continue`, `--resume` and `/resume` print the stored
  conversation — your prompts without the engine's injected context, answers, tool cards with their
  results, an approved plan — and restore the todo strip and the token counters.
- Sessions persist (D49). Every conversation is appended to
  `~/.omp/sessions/<project>/<id>.jsonl` (override with `--sessions-dir`); `--continue` resumes the
  newest non-empty session in the current directory, `--resume <id|prefix|path>` picks one,
  `--no-session` keeps it in memory. A killed process is repaired on load: a torn last line is
  dropped, a tool call with no result comes back as an `outcome unknown` error result, an orphan
  result is dropped, and the shell says so. `/new` starts a new file and leaves the old one
  resumable; `/clear` only clears the screen.

## Not yet built

M0–M7 are closed; M8 (the GUI) is stopped — a parked start sits in `gui/`, unwired. Of the M9 differentiators, goal mode (D70) and
long-term memory (D71) are built. Much of `omp`'s surface is not built — checkpoints, parallel
background agents and more — and the [gap ledger](ROADMAP.md#gap-ledger) lists it.
What remains inside closed milestones is under the gaps below.

## Verification commands

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./... -race
```

Windows note: `-race` needs `CGO_ENABLED=1` and a gcc on PATH
(`C:\ProgramData\mingw64\mingw64\bin` is not on PATH by default — add it for the run).

## Known gaps and debts

| Item | Detail | Revival |
|---|---|---|
| OpenAI protocols unverified live | Only `anthropic-messages` has been run against a real endpoint (with tools, 2026-11-12) | when an OpenAI-compatible endpoint is available |
| TUI untested in a real terminal by a person | Covered by unit tests and an end-to-end test of the real Bubble Tea program over pipes; Windows Terminal, IME input and resize have not been exercised by hand | first interactive session |
| Plan mode untested with a real model | The reminder, the tool description and the approval flow ran against a scripted gateway and unit tests only; whether a real model plans before calling `exit_plan_mode` is unmeasured, and the eval harness cannot measure it because it is scripted only (D66) | first interactive session |
| TUI throughput | **Measured 2026-11-17** (`presentation/throughput_test.go`): 10,600 events replay in ~2.7 ms (~3.8M events/s) against a 200 events/s budget; a 10k-line transcript replays in ~1.4 s. This is the reducer's throughput — the Bubble Tea renderer's own cost under a real terminal is not covered | reopen if a real 10k-line transcript feels slow |
| Session files never shrink | The compactor bounds the model's view, not the file; there is no retention or pruning policy (D49) | when a user asks, or when a file is large enough to slow `--continue` |
| Model window unknown for new families | **Mostly closed by D55**: a built-in table supplies the window for the families we have run, so automatic compaction works with no config. A model outside the table still gets no automatic compaction (a wrong window is worse than none); a model catalog, left open by D63, would replace the table | when a new family is used — add one line |
| Summarizer cost | Each summarization is one extra model call on the active model, and a compaction boundary costs one prompt-cache miss | model roles (M9) |
| Byte-based estimate | `bytes/4` over-estimates code and CJK-heavy text, so compaction fires earlier than needed | measured against real usage; refine if it misfires |
| Malformed tool arguments | A call whose arguments are not a JSON object is refused with the truncation message (D51), so invalid JSON is reported as an output-limit cut even when the limit was not the cause (D46) | when a trace shows it |
| Session approvals | "Don't ask again" rules live in memory and are lost on exit | when a user asks to persist them |
| Cancel latency | `taskkill /T /F` takes about 1.25 s, so a timed-out or cancelled command returns after that (D33) | when an interrupt budget is stated; then a job object |
| Directory junctions | Since Go 1.23 `filepath.EvalSymlinks` does not follow junctions, so a junction inside the workspace pointing outside is not detected; symlinks are | when junctions appear in a real workspace |
| Mixed line endings | `edit` and `write` normalise a file to its first line ending | if it causes noisy diffs |
| Console code page | **Verified 2026-11-17**: a native program's non-ASCII output arrives as UTF-8 bytes because the tool captures a pipe, not a console handle — measured with a Go binary printing `中文输出` under Git Bash. A program that writes to a console handle directly (`/dev/tty`) is still out of scope | reopen if a real command garbles output |
| Trace JSON tags | The wire has its own snake_case frames (D64), but the trace writes `agent.Event` as is, so `tool_result` and `approval` inside a trace line use Go field names (`Content`, `IsError`, `Tier`) | when a trace reader outside this repo needs it; it is a trace version bump |
| Empty-completion retry | D62 retries failed calls by class; a successful but empty completion is not retried | when a trace shows one |
| Subagent transcript | A subagent's history lives in memory and is discarded after the call; the sidechain transcript R6.5 asks for is not written | when someone needs to audit what a subagent did |
| Parallel subagents | `task` declares itself not concurrency-safe, so two delegations in one response run one after the other; REQUIREMENTS §2 asks for parallel work | when a task needs two subagents at once |
| Memory freshness | Without `/memory sync`, the summary trails the work by at least 12 hours, and an update during a session only reaches the next one (D71) | if stale memory misleads a real session |
| Memory cost visibility | The pipeline's model calls (one per past session, one merge) are in neither the trace nor the usage counters (D71) | when a user asks what memory costs |
| Goal mode not live-tested | The runner, budget and resume ran against scripted models only; whether a real model audits before `goal complete`, or stalls into the no-progress stop, is unmeasured (D70) | first real `/goal` |
| Memory not live-tested | The pipeline ran against a scripted model only; extraction quality with a real model is unmeasured | first real use with `memory.backend: local` |
| Code highlighting | Code blocks are one colour; chroma would add 5.6 MB to the binary and 6.5 ms to startup (D67) | when a user asks, or a highlighter costs well under a megabyte |
| Wrapped code lines | A code line wider than the terminal continues on rows marked `↳`, so copying it from the scrollback copies the marker; the Ctrl+O viewer wraps the same way (D75) | if copying code out of the terminal matters |
| Markdown re-render | The live tail is re-parsed and re-rendered whenever its text changes; 23.6 KB renders in 6.1 ms and the tail is usually one block, so only a very long table or list that is still streaming pays for it (D67) | reopen if such a stream feels slow |
| Subagent tokens | A subagent's tokens appear in its task result and its progress reports, not in the parent's usage, its token budget or the trace summary (D68) | when the budget or a summary reader needs the whole spend |
| Write card is not a diff | After a `write` has run its card shows the new content, not what it replaced; only the approval dock (asked before the write) diffs against the file on disk, and in `acceptEdits` no approval is asked (D74) | a display payload on tool results, when a second shell needs it too |
| Approval preview limits | The dock diffs a `write` against the file only when it is a regular text file of at most 256 KiB; a larger or binary file is shown as new content. Inputs over a million line pairs skip the diff and show removal then addition (D74) | if a real approval misleads |
| Viewer output cap | The Ctrl+O transcript shows at most 4,000 output lines per tool and says how many it left out; the model saw the whole result (D74) | if someone needs more |
| Live tool timer | A running card counts from dispatch, so queueing and the in-order release of results show as running time; the finished card shows the measured execution time (D68) | an event that marks the start of execution, if the difference is noticed |

