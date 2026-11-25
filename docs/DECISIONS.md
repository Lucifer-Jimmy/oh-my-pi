# Decisions — oh-my-pi

> The decisions that still govern the code and the way it is built: what was chosen, why, and
> what would reverse it. A decision that has been superseded, or that only ordered work that is
> now finished, is removed instead of being kept as history; the entry that replaced it says what
> changed. Numbers are never reused, so a gap in the sequence is a removed entry.
>
> The product definition is D29 (a general-purpose agent platform: TUI + GUI + SDK) with D30
> (baseline parity).

## Index

| # | Decision | Date | Relation |
|---|---|---|---|
| D1 | Official SDKs instead of a hand-written wire layer | 2026-10-26 | — |
| D2 | Two protocols, no per-vendor adapters | 2026-10-26 | — |
| D3 | Neutral model is small and lossy on purpose | 2026-10-26 | — |
| D4 | Credentials only from `~/.omp/config.yml` | 2026-11-06 | — |
| D5 | Key command failure is fatal, with stderr | 2026-11-06 | — |
| D6 | Fail-closed tool declarations | 2026-11-04 | — |
| D7 | Truncation retries capped at 3 | 2026-11-04 | narrowed by D51: only the cut call is refused |
| D8 | Panic recovery scoped to `Tool.Execute` | 2026-11-04 | — |
| D9 | One assembly root, no undeclared access | 2026-10-28 | — |
| D11 | Compaction never rewrites stored history | 2026-10-28 | refined by D26, executed by D47 |
| D12 | Approval + worktree, no OS sandbox | 2026-11 | worktree half delivered by D61 |
| D13 | Anthropic `max_tokens` defaults to 64000 | 2026-11-06 | revisit with a model catalog, which D63 left open |
| D14 | No `--provider`; `--model provider/model` | 2026-11-06 | — |
| D15 | `go.yaml.in/yaml/v3` | 2026-11-06 | — |
| D18 | Reject `noa` entirely | 2026-10-23 | — |
| D20 | Tool metadata beyond an approval tier | 2026-11-10 | executed by D44 |
| D23 | Eval scores the tool trajectory | 2026-11-10 | — |
| D25 | Session is a linear log; branching cut | 2026-11-10 | — |
| D26 | Compaction is elide-first, summarize-last | 2026-11-10 | executed by D47 |
| D29 | Product is a general-purpose agent platform | 2026-11-10 | — |
| D30 | Baseline parity with Claude Code / Codex | 2026-11-10 | — |
| D32 | `read` does not sniff for binary | 2026-11-12 | — |
| D33 | Windows shell: Git Bash, PowerShell fallback | 2026-11-12 | — |
| D34 | `bash` tiers: inspection=write, code=exec | 2026-11-12 | — |
| D35 | File mutation: read first, atomic, format-keeping | 2026-11-12 | — |
| D36 | Permissions follow Claude Code: rules, four modes | 2026-11-12 | amended by D41 |
| D37 | The binary is `omp`; `tools` is one package | 2026-11-12 | where packages live: D48 |
| D39 | TUI v1: Bubble Tea v2, inline scrollback | 2026-11-12 | commits refined by D67 |
| D40 | Default model from `modelRoles.default` | 2026-11-12 | — |
| D41 | Shared `Policy`; session approvals | 2026-11-12 | amends D36 |
| D42 | Config keys follow `omp` where meaning matches | 2026-11-13 | amends R2.6, R2.7 |
| D43 | Plan mode v1: policy blocks, reminder tells | 2026-11-13 | — |
| D44 | Tool metadata: three dimensions | 2026-11-15 | executes D20 |
| D46 | Tool arguments checked against schema | 2026-11-16 | — |
| D47 | Compaction v1: elide first, summarize last | 2026-11-17 | implements D11, D26 |
| D48 | Every package public; tree renamed | 2026-11-17 | — |
| D49 | Sessions are append-only JSONL, repaired on load | 2026-11-17 | implements D11 |
| D50 | Verification lives outside the model | 2026-11-17 | — |
| D51 | Tools dispatch when the argument block closes | 2026-11-17 | — |
| D52 | Block lifecycle synthesized for Completions | 2026-11-17 | records `llm/openai/convert.go` |
| D54 | Traces are our own NDJSON, one file per run | 2026-11-17 | summary extended by D68 |
| D55 | Context windows come from a small built-in table | 2026-11-17 | — |
| D56 | Skills are declared in `SKILL.md`, loaded on demand | 2026-11-17 | — |
| D57 | MCP is a client over the official SDK, bridged as write-tier tools | 2026-11-18 | transports extended by D66 |
| D58 | Hooks are subprocesses exchanging one JSON object each way | 2026-11-18 | — |
| D59 | Background tasks notify with pointers, never with output | 2026-11-18 | — |
| D60 | Session titles and exports are append-only operations | 2026-11-18 | — |
| D61 | Isolated subagents work in a git worktree and merge a patch | 2026-11-18 | delivers D12's worktree half |
| D62 | Model calls retry by class, never after output | 2026-11-18 | — |
| D63 | M6 closes on two measured acceptance runs, not inference | 2026-11-18 | kill test corrected by D68 |
| D64 | The wire is a versioned NDJSON envelope, not the Go event struct | 2026-11-19 | — |
| D65 | Host-executed tools are a declaration plus a frame; exports without an outside consumer stay | 2026-11-19 | — |
| D66 | Read the blueprint before inventing | 2026-11-19 | — |
| D67 | Markdown is parsed by goldmark and laid out by our own renderer | 2026-11-20 | — |
| D68 | Observability is measured where it happens and folded where it is read; cost leaves the plan | 2026-11-23 | extends D54 |
| D69 | User-level files live in `~/.omp/`, beside the TS `omp`'s `agent/` | 2026-11-23 | moves D4's file |
| D70 | Goal mode: an outer runner keeps an objective going until the model completes it with evidence | 2026-11-24 | admits an M9 candidate; amends D58 |
| D71 | Long-term memory: lessons saved on demand and a summary distilled from past sessions, injected at session start | 2026-11-24 | admits an M9 candidate |
| D72 | The kernel keeps a run's state in one value; the assembly root builds the runner stack once | 2026-11-25 | no behaviour change; records three API warts |
| D73 | A resumed session replays its stored conversation; injected context is hidden by tag | 2026-11-25 | M3 ⑨ |
| D74 | Approvals show the change they would make; Ctrl+O opens the whole transcript | 2026-11-25 | M3 ⑩; amends D39 |
| D75 | Rendering fixes found by rendering real answers at three widths | 2026-11-25 | M3 ⑪; amends D67 |
| D76 | A session picker in the prompt line, and the key/flow edges around it | 2026-11-25 | M3 ⑦, ⑫ |

## Open questions

| # | Question | Needed by | State |
|---|---|---|---|
| O4 | Session interop with `omp` | — | Deferred by D49: own format v1; an importer can be added later |
| O10 | GUI framework | M8 | Lean: Wails v2 with the engine in-process (WebView2 ships on Windows; Fyne/Gio mean rebuilding markdown, diff and IME) |
| O11 | GUI transport | M8 | In-process for latency vs out-of-process to exercise the wire protocol |

## Entries

### D1 — Official SDKs instead of a hand-written wire layer

**Decided:** 2026-10-26. **Status:** Accepted.

`anthropic-sdk-go/v2` and `openai-go/v3` own wire parsing, retries and cancellation. We
write no SSE parser, no retry loop, and no wire structs.

**Why:** the SDK route does not remove the correctness work an adapter must do anyway
(field mapping, message legality, usage semantics, block completeness) — it only removes
work we would otherwise duplicate. `omp` maintains its own client because it needs dense
beta fields, a zoo of Anthropic-compatible gateways and frame-level SSE control; none of
those apply here. `Norma`'s hand-written layer was superseded by SDK adoption in its own
design.

**Reopen if:** (a) an SDK plus all three escape hatches (`Raw` JSON, `WithJSONSet`,
middleware) cannot express a needed wire feature; (b) we must swallow frame errors from a
non-conforming Anthropic gateway (then wrap a decode layer, still not a rewrite);
(c) the SDK cannot parse a billing field the product needs; (d) SDK beta support lags a
shipped feature.

**Evidence:** a model-layer sweep of `omp` (154 findings) and the SDK upgrade cost analysis,
both read from the source at `C:\Code\oh-my-pi-origin` at decision time; the source, not a note
about it, is the authority.

### D2 — Two protocols, no per-vendor adapters

**Decided:** 2026-10-26. **Status:** Accepted.

Anthropic Messages and OpenAI (Completions + Responses) are the complete adapter set.
Every compatible vendor is a base URL.

**Why:** the industry converged on two wire protocols. A vendor-specific adapter package
is permanent maintenance for a temporary difference. Vendor quirks live in exactly three
places: SDK options, the adapter's convert escape hatch, and a capability matrix.

**Reopen if:** a target vendor has no trustworthy two-protocol endpoint and the
compatibility path cannot express a feature we need.

### D3 — Neutral model is small and lossy on purpose

**Decided:** 2026-10-26. **Status:** Accepted.

`llm.Message`/`ContentBlock` carry only state that must survive persistence, replay or
cross-layer consumption. Everything provider-specific rides in `Raw` and is forwarded
verbatim.

**Why:** every field added to the neutral model must be filled by every adapter and kept
legal for every provider. Fields that do not survive a round trip are configuration, not
state.

### D4 — Credentials only from `~/.omp/config.yml`

**Decided:** 2026-11-06, user directive. **Status:** Accepted. Deliberate divergence from `omp`.

A key is a literal or `!command`. No environment variables, no `--api-key`, no `.env`.
`!command` does not go through a shell: split on whitespace, quotes group, backslashes are
literal (Windows paths), 10s timeout, trimmed stdout.

**Why:** the user keeps keys in an encrypted store behind a command, and environment
variables leak across tools and child processes. Merging `config.yml` and `models.yml` into
one file (unlike `omp`) is the same instinct: fewer key-bearing files.

**Consequence that must be guarded:** because one file holds keys *and* is user-editable,
a **project-level** `.omp/config.yml` must never read `providers`. A malicious repo could
otherwise execute a command. No project-level config exists yet (D42); the first one ships with
a test that pins this.

**Reopen if:** the user asks for a credential store or OAuth; then it is a new
`!command`-backed source, not a new precedence rule.

### D5 — Key command failure is fatal, with stderr attached

**Decided:** 2026-11-06. **Status:** Accepted.

A failing `!command` aborts with the command and its stderr. `omp` silently treats it as
"no key" and the user sees a 401.

**Why:** a silent 401 is the worst debugging experience in the product.

### D6 — Fail-closed tool declarations

**Decided:** 2026-11-04. **Status:** Accepted. Deliberate divergence from `omp`.

A tool that does not declare concurrency safety is scheduled exclusively; a tool that does
not declare an approval tier needs `exec` approval.

**Why:** the default must be safe for a tool written by someone who did not read the docs.
`omp` defaults to `shared`, which is faster and wrong for anything that writes.

**Cost:** slower batches when a tool forgets to declare. Mitigated by making declaration a
checklist item in the tool-writing protocol.

### D7 — Truncation retries are capped at 3

**Decided:** 2026-11-04. **Status:** Accepted.

A `length`-truncated turn goes back to the model at most 3 times in a row, and a call whose
arguments were cut never runs (D51). `omp` leaves it uncapped, relying on a deadline.

**Why:** an uncapped retry loop with a model that keeps emitting oversized payloads is an
infinite expensive loop. A cap turns it into a clear error.

### D8 — Panic recovery is scoped to `Tool.Execute` only

**Decided:** 2026-11-04. **Status:** Accepted.

Panic → error result for that call, batch continues. No recovery anywhere else.

**Why:** tools are the untrusted boundary (they touch the OS); the kernel is our code, and
its bugs must fail loudly. A panic in a goroutine a tool spawned itself cannot be caught —
a Go limitation, documented rather than papered over.

### D9 — Undeclared access is deprecated in favour of one assembly root

**Decided:** 2026-10-28. **Status:** Accepted.

Only `cmd/omp` knows all packages. `Registry.SetWrapper` applies approval middleware at
registration time, including for tools registered later (MCP).

**Why:** "dynamic registration forgot to wrap, so it bypassed approval" is a class of bug
that cannot exist if wrapping is not a separate step.

### D11 — Compaction never rewrites stored history

**Decided:** 2026-10-28. **Status:** Accepted.

The compactor produces a projection for the next request; the store keeps everything. The
summary is traceable to the entries it replaced.

**Why:** four independent implementations converge on this invariant. Rewriting storage
makes resume, audit and cost accounting all lie.

### D12 — Security model is approval plus worktree, not OS sandboxing

**Decided:** 2026-11. **Status:** Accepted.

No seccomp, landlock, containers or gVisor. Dangerous calls are stopped by permission;
subagent edits are isolated by git worktree and merged deliberately.

**Why:** `omp` — a mature shipping product — does the same. OS sandboxing is
platform-specific (Windows has no equivalent), high cost and high false-confidence risk.
The scheduler is agnostic: if a sandbox is ever needed it lives in the exec function or
the subagent layer with zero scheduler changes.

### D13 — Anthropic `max_tokens` defaults to 64000

**Decided:** 2026-11-06. **Status:** Accepted.

The API requires the field; `omp` sends 64000, `Norma` sends 8192. We follow `omp` because
8192 truncates long answers. It is the value sent when nothing else gives one; once a model
catalog exists (D63 left it open), the value comes from the model, clamped to the window.

### D14 — No `--provider` flag; `--model provider/model`

**Decided:** 2026-11-06. **Status:** Accepted.

`--model` splits at the first `/`; a known provider prefix supplies `api`, `baseUrl`,
`apiKey`. A bare model id uses `--api`.

**Why:** model names themselves contain `/` (gateways), so provider selection is a
prefix, and only the first `/` can be the separator.

### D15 — `go.yaml.in/yaml/v3`

**Decided:** 2026-11-06. **Status:** Accepted.

`gopkg.in/yaml.v3` is unmaintained; the YAML organisation took it over under the new
module path. `trpc-agent-go` made the same move.

### D18 — Reject `noa` entirely

**Decided:** 2026-10-23. **Status:** Accepted.

`Norma`'s `noa` (adaptive context growth) is not ported, not partially, not behind a flag.

**Why:** its scale violates Law 8; `omp` has no equivalent in its default stack, so no gap
appears by omitting it. Three `noa` lessons are kept: measure the projected
view, count "ignored" as failure in the ladder, store summaries as tool arguments so they
can be rebuilt byte-for-byte.

### D20 — Tool metadata carries more than an approval tier

**Decided:** 2026-11-10. **Status:** Accepted.

Beyond `Tier` (read / write / exec), every tool declares `destructive`, `open-world` and
`max-result-size`.

**Why:** a tier answers "how much damage can this do to my repository", which is the wrong
question for a network fetch or a vendored API call. `trpc-agent-go` reached the same
conclusion with `ToolMetadata{ReadOnly, Destructive, ConcurrencySafe, OpenWorld,
MaxResultSize}`; `omp` encodes part of it in per-tool approval code instead of data. Data
beats code here because the approval dialog and the eval report both need to read it.

**Cost:** three more fields to fill in per tool. Mitigated by the fail-closed rule — omitted
means the conservative value.

### D23 — Evaluation scores the tool trajectory, not just the answer

**Decided:** 2026-11-10. **Status:** Accepted.

An eval case can assert the ordered sequence of tool calls and their arguments, with
tolerance, in addition to the final answer.

**Why:** for a coding agent the interesting failure is "it edited the wrong file and then
ran the wrong test" — the final prose can still look convincing. Scoring the trajectory is
the only way to catch it. `trpc-agent-go`'s `evaluation` package does this (with argument
tree matching and mocks); `omp` does not, and pays for it by hand-testing.

**Cost:** expectations are more brittle than answer matching; use tolerance and mock only
where the argument values matter.

### D25 — Session is a linear log; branching is cut

**Decided:** 2026-11-10. **Status:** Accepted.

The entry schema keeps `parentId`, but it always points at the previous entry. No fork,
no `/tree`, no `/branch`, no branch summaries, no sibling-aware recovery. No content-addressed
blob store until the first image exists.

**Why:** the tree's cost is not the format, it is everything it implies — an unpersisted
`leafId` to rebuild, summary passes per branch, recovery that must consider siblings, and a
renderer that must draw a tree. A coding agent's session is a conversation that happened:
persist, resume, self-repair. Branching is an exploration feature.

**Reopen if:** a real workflow needs to try three approaches in one session. The schema
already anticipates it, so it returns without a format break.

> **Note (D30):** the GUI milestone makes this revival more likely, not less — a tree view is
> a natural GUI affordance. The decision stands for v1 and the schema is ready.

### D26 — Compaction is elide-first, summarize-last

**Decided:** 2026-11-10. **Status:** Accepted.

Tool results split into two classes at compaction time: *reproducible* (`read`, `glob`,
`grep`, `skill` — replaced with a re-run hint, zero loss, zero cost) and *non-reproducible*
(`bash` output and edits — the outcome and the last lines stay, D47). Conversation is summarized
only when elision is insufficient.

**Why:** context bloat in a coding session is dominated by tool output, which is cheap to
reclaim. Summarizing conversation with a model call is expensive, lossy and non-deterministic.
Elision is deterministic and testable without a network, and it keeps the elision decision
auditable.

**Evidence:** `omp`'s `pruneToolOutputs`/`pruneSupersededToolResults` do a weaker version
(no recovery pointer); `trpc-agent-go`'s `context_compact.go` carries `event_id` /
`tool_call_id` recovery hints — that is the right shape.

### D29 — The product is a general-purpose agent platform: TUI + GUI + SDK

**Decided:** 2026-11-10. **Status:** Accepted.

The goal is a high-performance **general-purpose agent** with three first-class shells — TUI,
GUI, and a headless SDK — positioned to become the orchestration substrate for other agent
applications.

**Why:** the earlier framing (smallest Windows coding agent) optimised against the wrong
constraint. The engine already exists and is sound; the shells and the SDK are what turn it
into a product and a platform. "General-purpose" also means the kernel must stay
feature-blind and the coding toolset must become *one bundle among others* — which was
already Law 8, but is now load-bearing rather than tidy.

**Consequences that are structural, not cosmetic:**

1. **The public surface must exist.** An SDK cannot live behind `internal/` (D48).
2. **The shell contract becomes a protocol**, not just a Go interface — one engine, three
   shells, one schema, two transports (in-process Go values for the TUI, serialized frames
   for the GUI and for out-of-process embedders).
3. **Performance becomes a requirement with numbers**, not an adjective: render throughput,
   event batching, time-to-first-token, per-turn overhead.
4. **Delegation moves from conditional to core.** "Orchestration substrate for other agent
   applications" is subagents and parallel work as a designed feature, not an experiment.
5. **The admission test changes.** From "does it make a Windows coding task succeed more
   often?" to "does it make an agent application cheaper to build, or the agent more capable
   to use?" Cut-rather-than-defer still applies — the test changed, the discipline did not.

### D30 — Parity with the common agent feature set is a requirement; plan mode returns

**Decided:** 2026-11-10. **Status:** Accepted.

The baseline capability set of Claude Code and Codex is required, not aspirational. That
includes, at minimum: plan mode (read-only exploration → presented plan → approval → execute),
a todo/plan tracker, subagents, slash commands, context files, MCP, hooks, skills, permission
modes with per-tool policy, session resume, compaction, mid-run steering, diff review, and
background commands.

**Why:** an agent that lacks plan mode cannot be trusted with a large task, because the user
has no cheap point to review intent before side effects.

**What this does *not* mean:** we do not copy `omp`'s implementations wholesale. Plan mode
in particular should be judged on its own mechanics (read-only enforcement, approval of the
plan, mode flip mid-loop) rather than adopting the `PermissionModeFunc` trick wholesale.
Goal tracking and `coordinator` came off the cut list here, but what shipped is narrower: the
`todo` tracker (M4①) and one `task` subagent at a time (M4②). `omp`'s goal mode — a persistent
objective with a token budget, automatic continuation and an audited completion — and its
parallel background agents are not built; they sit in the ROADMAP gap ledger with the rest of
the `omp` surface we do not have. TTSR, `xd://`, speculative execution and computer use remain
cut until argued with a named task.

### D32 — `read` does not sniff for binary content

**Decided:** 2026-11-12. **Status:** Accepted.

`read` treats every existing regular file as text. It does not probe for NUL bytes or
invalid UTF-8 and does not refuse to return a binary file's bytes.

**Why not refuse:** a refusal is only useful when it comes with a way forward. `omp` pairs
its binary rejection with three escape hatches — `:raw` for verbatim bytes, per-format
conversion for images/PDFs/notebooks, and IDA for executables. We have none of them, and a
refusal without a way forward is a dead end, which is worse than returning noise the model can
recognise and route around.

**The cost is bounded, not open-ended.** `read` caps output at `readMaxLines = 2000` and
`readMaxBytes = 50 KB`, so a mistaken read of a PNG costs roughly 12k tokens of mojibake at
most. `bufio.ScanLines` does not validate UTF-8, so invalid bytes reach `llm.TextBlock`
unchanged and become U+FFFD at JSON encoding — the request still succeeds, it is not an API
error. A bounded wrong answer the model can see is cheaper than a dead end.

**Where binary handling belongs:** `bash` (D33), through tools built for it — `file`,
`strings`, `hexdump`, PowerShell's `Format-Hex` — which express the intent (`show the
header`, `find printable runs`) that a byte-level sniff inside `read` cannot.

**Reverse when:** an image/PDF reading path lands (then `read` gains format dispatch, not a
sniff), or `read` is the only file access a shell-less deployment has and callers report
context blowups. At that point the fix is a format-aware route, not a rejection.

### D33 — Windows shell: Git Bash next to git.exe, PowerShell fallback, never the WSL launcher

**Decided:** 2026-11-12. **Status:** Accepted.

`tools.ResolveShell` runs once per process in `cmd/omp`; the same `Shell` goes to the `bash`
tool and its name goes into the system prompt and the tool description, so the model is told
the shell that will actually run its command. Order on Windows: Git Bash found through the
location of `git.exe` (`<git>/../bin/bash.exe` or `<git>/../../bin/bash.exe`), then `pwsh`,
then Windows PowerShell with `[Console]::OutputEncoding` forced to UTF-8, then `cmd`.
Elsewhere: `bash`, then `sh`.

**Why never `bash` from PATH on Windows:** on a machine with WSL, `C:\Windows\system32\bash.exe`
wins the PATH search and runs the command inside Linux. Measured on the development machine:
`pwd` printed `/mnt/c/Code/oh-my-pi`; the Windows Go toolchain and every Windows path
assumption are gone, and the model cannot tell why.

**Why Git Bash before PowerShell:** the tool is called `bash` and models write bash. Windows
PowerShell 5.1 rejects `&&` outright. Native Claude Code on Windows relies on Git Bash for the
same reason. Without Git for Windows we fall back to PowerShell and the description says so.

**Process tree:** timeout and cancellation kill the whole tree (`taskkill /T /F` on Windows, a
process group on Unix). A mutation test proves it: with only the shell killed, the child holds
the output pipe and the call takes the full `WaitDelay` (10.1 s in the test); with the tree
killed it returns in about 1.7 s. `taskkill` itself costs about 1.25 s, which is the current
cancel latency. **Revisit** with a job object if a stated interrupt budget needs it.

### D34 — `bash` tiers: inspection is write, running code is exec, nothing is read

**Decided:** 2026-11-12. **Status:** Accepted.

`bash` never declares `read`: a shell can always write, and `read` would let plan mode run it.
A command is `write` only when every sub-command after `permission.SplitCommand` is an
inspection command from a fixed list (`ls`, `cat`, `head`, `tail`, `wc`, `grep`, `echo`,
`git status|log|diff|show|rev-parse|ls-files|blame`, a few PowerShell `Get-*` cmdlets), the
program is not path-qualified (`./ls` is exec), and no risky flag appears (`--output`,
`--ext-diff`, `--pre`, `-exec`, `-delete`). Everything else is `exec`; known destructive
patterns also force a prompt. Under `cmd.exe` nothing is downgraded, because it runs programs
from the current directory first.

**Consequence:** `acceptEdits` edits files but does not run code. `go test`, `npm run`,
`python`, `make` need an allow rule (`Bash(go test *)`) or an approval. An earlier draft let
those through as `write`; with `acceptEdits` as the print default that was arbitrary code
execution without asking, and it is reverted here.

**Timeouts:** 120 s default, 600 s cap, the tighter of the two references. Output is held in a
bounded head and rolling tail (12 KB + 18 KB) whatever the command prints, cut on rune
boundaries.

### D35 — File mutation contract: read before change, atomic, format-preserving

**Decided:** 2026-11-12. **Status:** Accepted.

`read`, `write` and `edit` share one `tools.FileState`. `read` records a file's modification
time and size; `write` (over an existing file) and `edit` refuse a file that was not read in
this session or that changed on disk since, and record the new state after their own write, so
consecutive edits need no re-read. A tool built without a `FileState` refuses to mutate rather
than silently skipping the check.

Writes go to a temporary file in the same directory and are renamed over the target, falling
back to a direct write only when the rename fails. The file's UTF-8 BOM and line ending
(detected from the first line break) are kept; model text is normalised to LF first. `write`
refuses content that is a partial `read` view: a trailing truncation notice, a cut-line marker,
or `read`'s line-number prefixes. `edit` reports every match line when `old_string` is
ambiguous, and when it is missing points at lines that match after indentation or typographic
quotes are ignored.

Any path with a `.git` segment forces a prompt: a hook or config written there runs code on
the next git command, which would turn `acceptEdits` into code execution. `ForcePrompt` is now
honoured by `permission.Evaluate`: it overrides the mode's automatic allow, but not an explicit
allow rule, `bypass`, or a deny.

### D36 — Permissions follow Claude Code: rules first, four modes

**Decided:** 2026-11-12. **Status:** Accepted.

Policy is three rule lists in `~/.omp/config.yml` (`permissions.allow|ask|deny`, entries like
`Bash(git commit *)` or `Read(**/.env)`); tool names match case-insensitively. Evaluation order
is fixed: deny, plan restriction, bypass, ask, allow, forced prompt, mode default. A deny rule
or a single denied sub-command blocks the call; an allow rule must cover every sub-command, so
`git status; rm -rf /` does not inherit `Bash(git status)`. Command substitution and file
redirection make a command unmatchable; `2>&1`, `>&2` and redirects to `/dev/null` do not.

Modes only decide the default when no rule matches: `default` (read automatic, everything else
asks), `acceptEdits` (read and write automatic), `plan` (read only, bypass cannot override it),
`bypass` (everything except deny rules). There is no separate auto or yolo mode: an earlier
five-mode design expressed rules as modes and was dropped. Print mode defaults to
`acceptEdits`; with no interactive host an ask becomes a deny whose text tells the model to add
an allow rule or change `--approval`.

### D37 — The binary is `omp`; `tools` is one package

**Decided:** 2026-11-12. **Status:** Accepted.

- **The binary and assembly root are `omp` (`cmd/omp`)**, chosen by the user. User-level files
  live in `~/.omp/` (D69).
- **`tools` stays a single package**, the one plural package name (Architecture §8). The built-in
  tools share path resolution, argument decoding, `FileState` and the truncation markers; upstream
  `pi` keeps its tool surface in one package (`agenttool`). Splitting would invent a home for
  shared helpers with no consumer (Law 8). Revisit at the first consumer that wants one tool
  without the others.

Where packages live is D48.

### D39 — TUI v1: Bubble Tea v2, committed scrollback plus a live tail, reducer without patches

**Decided:** 2026-11-12. **Status:** Accepted.

- **Library:** `charm.land/bubbletea/v2` v2.1.1 with `bubbles/v2` textarea and `lipgloss/v2`.
  v2 rather than v1: declarative views with a real cursor (IME candidate windows land on the
  composer), separate paste messages, keyboard enhancements for Shift+Enter, a cell-diffing
  renderer, and Windows console input. v1 is in maintenance; starting on it means a migration.
- **Layout:** inline mode, never the alt screen. Finished items are printed above the program
  with `tea.Println` and become ordinary terminal scrollback; only the live tail (streaming line,
  running tools, approval dock, queue, composer, status bar) repaints. Assistant text commits
  block by block as each markdown block finishes (D67), so the live tail stays small however long
  the answer is.
- **Ordering:** Bubble Tea runs every command on its own goroutine, so two `Println` commands
  can land out of order. The shell keeps one print in flight (`Sequence(Println, printed)`) and
  queues the rest; scrollback order is therefore deterministic.
- **Reducer:** `presentation.State` with `Submit`, `Apply(agent.Event)` and `Notice`; it closes
  streams on the next non-delta event, pairs tool results by ID, ignores duplicate and stale
  sequence numbers, marks unfinished tools cancelled on an abort, and serialises to JSON.
  `Patch` output is cut until an out-of-process consumer needs it — the TUI renders from the
  state directly (Law 8), and the M7 wire sends events, not patches.
- **Lanes:** the shell drains every event already buffered into one message (up to 256), which
  coalesces deltas without a timer. A separate lossy lane is not built: the reducer benchmark
  (2026-11-17) replays 10,600 events in about 2.7 ms against a budget of 200 events/s.
- **Interaction:** Enter sends or queues while a turn runs; queued messages flush as one prompt
  when the turn completes and return to the composer when it is interrupted. Esc interrupts;
  Ctrl+C interrupts, clears, then exits; Shift+Tab cycles default → acceptEdits → plan (bypass
  only when the session started in it). Approval keys are explicit — digits, `y`, `n`, Esc —
  and Enter never approves, so a keystroke typed as the dock appears cannot run a command.

### D40 — Default model from `modelRoles.default`

**Decided:** 2026-11-12. **Status:** Accepted.

`--model` is optional. When it is absent the top-level `modelRoles.default` in
`~/.omp/config.yml` supplies it, using `omp`'s key so an `omp` config keeps working; other roles
and non-string values are ignored, never fatal (R2.7). With neither, the run fails with a
message naming both. Interactive mode needs this: typing `--model` on every launch was the
request made twice.

### D41 — Permissions are a shared `Policy`; session approvals; sub-commands covered individually

**Decided:** 2026-11-12. **Status:** Accepted. **Amends D36.**

- `permission.Wrap(tool, *Policy)` replaces the value `Config`. The policy holds the mode and
  the rules behind a lock, so the shell can change the mode mid-session and every wrapped tool
  sees it on its next call; calls already evaluated are unaffected.
- An approval is a three-way select: **Yes**, **Yes and don't ask again this session for
  `<rules>`**, **No**. The remembered rules are derived from the call — one `prefix *` rule per
  sub-command (first word, plus the second when it is a sub-command such as `go test`), or the
  whole tool for path and argument-less calls. Asks forced by an explicit ask rule or by the
  tool's own flag are sticky and offer no session option; unmatchable commands offer none
  either. Session rules live in memory only.
- `firstCovering` now requires every sub-command to be matched by *some* allow rule instead of
  one rule matching all of them: `git status && go test ./...` passes with `Bash(git status)`
  and `Bash(go test *)`, while `git status; rm -rf /` still asks.
- The approval preview renders arguments as `key: value` lines in their original order instead
  of raw JSON.

### D42 — Config file follows `omp`'s keys where the meaning is the same

**Decided:** 2026-11-13. **Status:** Accepted. **Extends D40; amends R2.6 and R2.7.**

- **Thinking selector:** `provider/id:level` — a trailing `:level` naming one of the seven levels
  sets the thinking level, `omp`'s syntax; any other suffix stays in the id, so
  `ollama/qwen3:8b` is untouched. Precedence follows `omp`: `--thinking`, then the suffix, then
  the top-level `defaultThinkingLevel`. Values we do not support there — `omp`'s `auto` and
  `inherit`, or a typo — are ignored, as `omp` ignores an invalid setting and uses the default.
  With no model catalog there is no fuzzy matching, and an id that really ends in `:high` cannot
  be addressed; acceptable until the catalog exists.
- **`providers:` entries are decoded only when selected.** `omp` keeps settings under the same
  key (`providers.maxInFlightRequests`, `providers.anthropic.serverSideFallback`), and a broken
  entry for a provider not in use no longer stops the run. The selected entry still fails
  loudly, naming the provider.
- **Built-in providers:** an entry named `anthropic` or `openai` may omit `api` and `baseUrl`
  (`anthropic-messages` and `openai-responses`, what `omp`'s catalog uses for their chat
  models), so `apiKey` alone is enough. Other `omp` built-ins still need both until the catalog
  exists. A prefix with no entry stays part of the id, so OpenRouter-style
  `anthropic/claude-…` with `--base-url` keeps working.
- **Anthropic base URL:** a trailing `/v1` is removed in the adapter, as `omp`'s
  `normalizeAnthropicBaseUrl` does. The SDK appends `/v1/messages` itself, so
  `https://gw/v1` used to request `/v1/v1/messages`. OpenAI-style URLs keep their `/v1`.
- **`permissions.defaultMode`** sets the starting approval mode; `--approval` overrides it. The
  key and its four values are Claude Code's, consistent with D36. `omp`'s `tools.approvalMode`
  (`always-ask`/`write`/`yolo`, default `yolo`) and `tools.approval` are deliberately not read:
  different vocabulary, different default.
- **The `permissions` section is strict**, the exception to R2.7: an unknown mode or a
  malformed rule list stops the run, because skipping either fails open — a dropped deny rule,
  or `plan` silently becoming `acceptEdits`. `omp` has no `permissions` key, so a pasted `omp`
  config never trips it.

**Not adopted, each with its trigger:** per-provider `headers`/`authHeader` (the first gateway
that needs them); `models[]` metadata such as context window and max tokens (a model catalog,
which D63 left open); model roles other than `default` (M9; subagents shipped without them); a
project-level config layer (the first per-project setting with a consumer — `providers` are
never read from it); `apiKey` naming an environment variable (never, D4); `--provider` and a
`config set` command (no consumer).

### D43 — Plan mode v1: the policy blocks, a reminder tells, `exit_plan_mode` asks

**Decided:** 2026-11-13. **Status:** Accepted. **Executes M2⑦; amends ARCHITECTURE §4.7.**

- **Enforcement stays in the policy** (D36): in `plan` every write and exec call is denied, and
  since `bash` is never read tier (D34) that includes every shell command. The mode is entered
  with Shift+Tab, `--approval plan` or `permissions.defaultMode: plan`.
- **The model is told by a reminder on each input.** While the mode is `plan`, the shell prefixes
  each user input with a `<system-reminder>` stating the rules; the first input after plan mode
  ends carries one "plan mode is off" note. The TUI shows the input as typed. Whether the last
  input carried the reminder is read from the session history, so nothing else is stored. The
  system prompt does not change with the mode, so switching modes never invalidates the cache.
  This is Claude Code's mechanism; `omp` swaps the system prompt instead.
- **`exit_plan_mode`** has the shape Claude Code and Norma share: one `plan` argument in
  markdown, read tier, never run in parallel. In plan mode it asks the host, with the plan as
  the message: **Yes, and auto-accept edits** → `acceptEdits`; **Yes, and manually approve
  edits** → `default`; **No, keep planning** or Esc → stays in `plan` and returns an error
  telling the model to ask what to change. Outside plan mode it answers "no approval is
  needed". With no host the plan cannot be approved, so the tool tells the model to give the
  plan as its final answer: `omp -p --approval plan` prints a plan and writes nothing.
- **The mode flips when the user approves**, inside the tool call. Every call before it was
  evaluated under `plan`; calls after it in the same response run under the approved mode, which
  is what the user just approved. This replaces "at a loop boundary": `omp` aborts the turn and
  starts a new one, which needs turn machinery we do not have, for no measured gain.
- **The plan is stored as the tool call's input** in the session — no plan file, no plan object.
  `omp`'s `local://<slug>-plan.md` and Claude Code's plan files let a plan survive context
  clearing and reach subagents. Compaction (D47) and subagents (M4②) shipped without needing
  one; the trigger is something that must read the plan outside the conversation.
- **TUI:** when the `exit_plan_mode` card appears, the whole plan is printed to the scrollback,
  where the terminal's own scrolling reads it and where it stays after the decision. The dock
  then shows only the title and the choices, and the finished card shows the outcome line.
- **Package:** plan mode is a mode, not a tool (user, same day). Its reminders and its control
  tool live together in `plan`, as `omp` keeps the mode in `plan-mode/` and Norma in
  `plan`; the policy still does the blocking. This amends the rule that every model-callable
  tool lives in `tools/`: a mode's own control tool lives with the mode, and `tools/` stays the
  catalog of general-purpose tools. `omp` has no plan
  tool at all — the model writes the plan's name to the virtual device `xd://propose`, served by
  a handler the mode installs — which rests on the internal URL schemes we cut.

**Not adopted:** `omp`'s "approve in a fresh context", "compact" and "save and quit" options and
its plan model role (no consumer has asked for them, and model roles do not exist);
`EnterPlanMode` as a tool the model calls (Norma; the user decides when to plan); editing the
plan before approving it (Claude Code). `/plan` arrived with M3⑥.

### D44 — Tool metadata: three declared dimensions, one consumer each

**Decided:** 2026-11-15. **Status:** Accepted. **Executes M2⑥; extends D36.**

- **Every built-in declares `Metadata`** — `Destructive`, `OpenWorld`, `MaxResultSize` — through
  `agent.MetadataDeclarer`. The contract, the conservative fallback and the panic boundary were
  already in `agent/tool.go`; this slice gave them their first consumer instead of adding fields.
- **Risk flags follow what the call can do, not the tier.** `read`, `glob` and `grep` are neither
  destructive nor open-world; `edit` and `write` are destructive; `bash` is both, because the
  shell reaches the network and other processes whatever this machine happens to have installed.
  Tier still decides *who asks*; metadata decides *what the approval says*.
- **`MaxResultSize` declares the tool's worst output** (the contract in `agent/tool.go` requires
  the declared value to match the real output). `read` 64 KiB (byte budget 50 KiB plus line
  numbering and the continue notice) and `bash` 32 KiB (12 KiB head + 18 KiB tail + the elision
  marker) are hard bounds, and a test drives each past its budget to prove it. `edit`, `write`
  and `exit_plan_mode` (8 KiB) return one line or a short list. `glob` (64 KiB, 200 paths) and
  `grep` (2304 KiB, 1000 matches × a 2000-byte line) are bounded by counts, not bytes; since D66
  the loop's central clamp cuts any result above its declared size, so the declaration is also
  the bound.
- **The approval dock reads the metadata**: `permission.Wrap` forwards `MetadataDeclarer` (so a
  wrapped tool keeps publishing, and an inner tool without a declaration still falls back to
  maximally risky), and the ask message gains one line per declared risk, before the argument
  preview. `omp` carries the same fields on its tool metadata; `trpc-agent-go` documents them as
  advisory and consumes none, which is the state we are leaving.
- **Workspace-wide searches honour deny rules.** `Glob` and `Grep` take a `Denied(rel)` predicate
  wired from `Policy.DeniesPath`, so `*(**/.env)` now stops `grep` and `glob` as well as `read`.
  Previously such a call produced no path spec at all, so a deny rule could not see it. The
  predicate is a plain function on the tool, not a policy import: `tools` already imports
  `permission` for rule kinds, and a callback keeps the policy out of the tool's inner loop's
  API surface. A denied directory is skipped whole; a denied single file yields "no files
  searched" rather than content.
- **Not adopted:** `trpc-agent-go`'s `ReadOnly`, `ConcurrencySafe` and `SearchOrRead` fields, which
  restate what `Schedulable` already declares and would give the same fact two homes.

### D46 — Tool arguments are checked against the schema before every call

**Decided:** 2026-11-16. **Status:** Accepted.

- **Where:** in the loop, in `runTool`, after the tool is found and before `Execute`, inside the
  same panic boundary. Approval happens inside the wrapped tool's `Execute` (D36), so it sees the
  corrected arguments and discipline 6 ("approval sees final arguments") holds. An invalid call
  gets an error `tool_result` naming every problem; the tool, the policy and the host are never
  reached. The pre-tool hook (D58) runs before validation, so arguments it rewrites are
  validated too.
- **What is checked:** the subset of JSON Schema that tool inputs use — `type` (one or a list),
  `properties`, `required`, `additionalProperties: false`, `enum`, `items`, nested objects.
  Other keywords are ignored rather than guessed at — `anyOf`, `oneOf` and `$ref` included, so
  an MCP schema that uses them is checked only where it stays inside this subset.
- **What is corrected**, only towards the declared type and never inventing a value: a numeric
  string for an integer or number (`"20"` → `20`); an integral number for an integer (`20.0`,
  `1e2` — Go's `encoding/json` refuses `20.0` for an `int` field); `"true"`/`"false"` in any case
  for a boolean; `null` on an optional property that does not allow null is dropped
  (`encoding/json` already tolerated it, and a validator must not reject what used to work).
  Nothing else: no `"yes"`, no 0/1 booleans, no stringified objects, no wrapped singletons.
  `omp` does all of those in a pipeline of about 2,000 lines; we add the next correction when a
  trace shows a model needing it.
- **Valid arguments reach the tool byte for byte.** Only a corrected call is re-encoded (keys
  sorted). Numbers are decoded with `UseNumber`, so large integers keep every digit.
- **Unknown properties are rejected where the schema is closed**, and every built-in schema now
  is (`additionalProperties: false`). The error names the valid properties:
  `unknown property "file_path" (valid properties: limit, offset, path)`. `omp` drops unknown
  keys; Norma and `trpc-agent-go` ignore them; Claude Code rejects them. Ignoring changes the
  meaning silently: a Claude-trained model sending `-i` to our `grep` would get a case-sensitive
  search and could conclude that code does not exist; `timeout` instead of `timeout_ms` runs a
  long build into the 120 s default. Cost: one turn when a model carries a habit over from
  another harness. Open schemas (MCP tools that do not close theirs) keep JSON Schema semantics and pass unknown
  keys through.
- **Wire compatibility:** `additionalProperties: false` is what Claude Code and Codex send. Both
  adapters already put it on the wire — Anthropic through `ExtraFields` (covered by an existing
  test), OpenAI unchanged; the Responses API is called with `strict: false`, so OpenAI's
  strict-mode schema rules do not apply.
- **Schema and argument struct are kept in step by a test, not a generator.**
  `tools/schema_test.go` checks every built-in: property names equal the struct's json tags,
  schema types match Go kinds, every property has a description, `required` names real
  properties, the schema is closed; `exit_plan_mode` has its own check (its arguments became the
  named `exitArgs`). Renaming `timeout_ms` in the struct alone fails it (mutation run). `grep`'s
  `output_mode` is now an `enum`, so the model sees the three values in the schema. Generating
  schemas from structs (`trpc-agent-go`; `invopop/jsonschema` is already in the binary through
  the Anthropic SDK) was not adopted: long descriptions in struct tags are hard to read and edit,
  and the generated `$schema`/`$defs` need stripping for some gateways — all to save one test.
- **Error text:** `<tool>: invalid arguments: <problem>; <problem>`, in a deterministic order,
  with strings shortened to 40 characters. The model's own call is already in its context, so
  the arguments are not echoed back (`omp` echoes them).
- **Known limit:** since D51 a call whose arguments are not a JSON object is refused with the
  truncation message before validation sees it, so invalid JSON is reported as an output-limit
  cut even when the limit was not the cause.

### D47 — Compaction v1: elide first, summarize last, never rewrite storage

**Decided:** 2026-11-17. **Status:** Accepted. **Implements D11 and D26.**

- **Where it lives.** `session` is its own package: the `Session` contract and the in-memory
  implementation moved out of `agent`, so the store, the compactor and the shells share one
  session contract (`agent` depends on `session`, Law 2). The compactor is
  `compaction`, a function-plus-struct satisfying `agent.Compactor{Compact}`, reached
  through `RunDeps.Compactor` — nil disables it. The loop asks once before every request
  (`TriggerAuto`), reacts once to an overflow (`TriggerOverflow`, retried only if the compactor
  actually recorded something), and exposes `Loop.Compact` for `/compact` (`TriggerManual`).
- **Storage is never rewritten** (D11). Edits to history are appended as `session.Compaction`
  records on the `Session` seam; the projection is rebuilt from the messages plus the latest
  record on every request. `Message.Usage` (local only, never sent) is what a projection is
  measured from.
- **Measurement** prefers the provider's own count: the last assistant message's reported usage
  plus the local estimate of the messages added after it, falling back to `bytes/4` over system,
  tools and all messages. The estimator is `(len+3)/4` for ASCII and one token per non-ASCII
  rune, the same shape `omp` uses when it has no native tokenizer.
- **Threshold** is `window − max(15% of window, 16384)`, and **keep recent** is 20,000 tokens —
  `omp`'s numbers. The window is configured, not guessed: `compaction.contextWindow` in
  `~/.omp/config.yml`, with `keepRecentTokens`, `reserveTokens`, `minElideTokens` and `disabled`
  beside it. **With no window configured, automatic compaction never fires**; elision still
  happens on `/compact`, and overflow recovery still works, which is the same fallback `omp`
  documents for unknown models. D55 later fills the window from a built-in table when the config leaves it unset.
- **Elision is the first and cheapest step** (D26). Tool results older than the kept-recent
  boundary are replaced in the projection: `read`, `glob` and `grep` become
  `[elided tool result: read succeeded, 512 lines; re-run read to see it again]`; everything else
  (bash, edits) keeps the outcome, the tool name and the last 10 lines, with a re-run hint, so a
  failing exit status or a final error line survives. A result shorter than 512 tokens is left
  alone, and a replacement is used only if it is genuinely smaller. This is `omp`'s
  reproducible/non-reproducible split (D26) with the recovery wording `trpc-agent-go` uses; it
  does not adopt `omp`'s prune-in-place, which rewrites stored history.
- **Summarization is the fallback.** It runs only when elision still leaves the projection above
  the threshold (automatic), or on `/compact` when anything at all can be elided first and is not
  enough. The region is everything older than the kept tail, cut at a turn boundary so a tool
  call never loses its result; with a previous summary the region starts at the previous cut and
  the previous summary is handed to the model inside `<previous-summary>`. The request carries a
  summarizer system prompt, the conversation serialized as `[User]:` / `[Assistant]:` /
  `[Tool Call]:` / `[Tool Result]:` inside `<conversation>` tags, tool results clipped to 2,000
  bytes head and tail, **no tools**, and an optional `Additional focus:` from `/compact <text>`.
  Output is capped at `min(0.8 × reserve, 16384)`.
- **Prompts** are ours, in the shape `omp` uses: a structured handoff (`## Goal`, `## Progress`,
  `## Key Decisions`, `## Next Steps`, `## Critical Context`) that keeps an unanswered question
  word for word, and an update variant that preserves and refreshes a previous summary. The
  summarizer is told the conversation is untrusted data and that it must never continue it.
- **In later requests** the summary is the first block of a user message wrapped in
  `<summary>`, letting the model know it is reading prior work, inside the kept tail's first user
  message when possible so the message sequence stays user/assistant.
- **Overflow detection** is a pattern table in `llm.IsContextOverflow`: `prompt is too long`,
  `maximum context length`, `context_length_exceeded`, `exceeds the context window`, Gemini,
  Bedrock, llama.cpp, LM Studio, MiniMax and Kimi phrasings, and `too many tokens`. It matches on
  the error text because both adapters surface the server's message. A different, more specific
  kind of rejection — a 429 whose body mentions request tokens — must not match, and a test pins
  that.
- **Overflow during summarizing** drops the oldest quarter of the region and retries, at most
  three attempts. Every other error fails the compaction immediately and the loop latches
  automatic compaction off for the rest of the run, telling the shell once; a failed projection
  is never used and no checkpoint is recorded.
- **What the user sees:** one `EventCompaction` with the record (tokens before → after, how many
  results were elided and messages summarized). The TUI renders it as a notice, keeps the
  scrollback, and the reducer owns the wording so a future GUI shows the same line. `/compact`
  while a turn runs is refused rather than queued.
- **Not adopted:** `omp`'s speculative background summary, its provider-native remote compaction,
  snapcompact/handoff/shake, the second "short summary" call, per-turn mid-run compaction, and
  the cached-prefix rebuild (the summary sits at the head of the projection, so a boundary costs
  one cache miss per compaction — measured cost, not a correctness issue).

**Known limits:** the summarizer model is the active model (no model roles yet); the estimate is
byte-based; a manual `/compact` on a conversation that only just exceeds the kept window elides
and stops without a summary (that is the intended cheap path); `Loop.Compact` shares the run
channel so it cannot run while a turn is in flight.

### D48 — Every package is public; the tree is renamed for the long run

**Decided:** 2026-11-17. **Status:** Accepted (user: "我认为我这个也是公开的 Agent Harness SDK…我支持全面调整代码，包括各个功能模块的命名要正式，并且是能够长期使用下去的").

- **No `internal/` at all.** `omp` is an Agent Harness SDK first and a terminal program
  second. A consumer embeds the loop, supplies its own tools, host, permission policy,
  compactor or recorder, and may replace any layer — so every package that implements a layer
  is importable: `llm`, `agent`, `session`, `permission`, `tools`, `compaction`, `prompt`,
  `plan`, `presentation`, `tui`. Our shells are the first consumers of that surface rather than
  a privileged inside.
- **Why nothing stays internal.** The earlier plan (2026-11-10) kept the shells and the
  still-moving contracts internal, assuming that publishing a package early freezes it. The real
  asymmetry runs the other way: `internal/X` → `X` is additive and free, while `X` →
  `internal/X` breaks every downstream import and can never be done. So the cheap mistake is to
  publish early and keep refining under `v0.x`, and the expensive mistake is to publish late.
  Norma (an Agent Harness SDK in Go) has seventeen top-level packages and zero `internal/`;
  trpc-agent-go hides thirty-four internal packages because its surface is a product module
  tree, not an embedding contract. We follow Norma's shape, with the shells kept as our own
  designs rather than removed.
- **The renames**, each chosen so the name says what the package *is*:
  | was | now | why |
  |---|---|---|
  | `ai` | `llm` | it is the model layer, not "AI"; `llm.Message`, `llm.Provider` read correctly in a consumer's import block |
  | `internal/compact` | `compaction` | a capability is a noun, like `permission` and `session`; `compact` also collided with a common variable name |
  | `internal/prompt` | `prompt` | the system-prompt builder is something a consumer replaces |
  | `internal/plan` | `plan` | plan mode is a feature with its own tool (D43) |
  | `internal/ui` | `presentation` | it builds presentation state for a shell; `ui` said nothing about what it produces and is a name every project wastes |
  | `internal/tui` | `tui` | shell 1, public because a consumer may embed it |
  | `ai.Api` | `llm.Protocol` (and `ApiAnthropicMessages` → `ProtocolAnthropicMessages`, …) | it names a wire protocol; `Api` also violates Go's initialism rule |
- **Unchanged on purpose:** the config file and its keys (`providers.<n>.api`, `compaction.*`)
  follow `omp` because users copy snippets between the two products. The wire-protocol strings
  (`anthropic-messages`, `openai-completions`, `openai-responses`) are data, not identifiers.
- **Naming rules are now constitutional** (ARCHITECTURE §3.1): a package is a noun, never
  `util` / `common` / `helpers` / `types`; a layer is named for its role; a capacity is a noun; a
  shell is named for its surface; a reducer is named for what it produces; a protocol type is
  `Protocol`; initialisms are all-caps. New packages follow the table from their first commit.
- **Cost:** the whole surface is a compatibility promise from the first tag, and the import
  paths changed once — the one moment this is cheap. Mitigation: the Go packages stay `v0.x`,
  every breaking change is recorded here, and the wire is versioned on its own (D64).
- **Not adopted:** keeping the shells internal (Norma ships no shell, but ours are a stated
  product surface); `internal/` for not-yet-built features (`mcp`, `subagent`, …). They are
  created public when their milestone starts, under the same naming rules.

**Known limit:** a public `tui` means its primary types (`Options`, `Runner`, `CompactFunc`)
are contract; internal render helpers stay unexported so only the shell's entry point is
promised.

### D49 — Sessions are an append-only JSONL log, repaired on load, never rewritten

**Decided:** 2026-11-17. **Status:** Accepted. **Implements D11.**

- **Format.** One file per session at `<root>/<encoded-cwd>/<id>.jsonl`, `root` =
  `~/.omp/sessions`. Line 1 is a header `{type:"header", version:1, id, timestamp, cwd, model}`;
  every later line is an entry `{type, id, parentId, timestamp, message|compaction|title}`. `id`s are
  `<session>-<n>` and form a line, so a later branch (resume-and-fork) only adds a second child
  — nothing in the file has to change to support it. `id` = `20060102-150405-<8 hex>`: sorts
  chronologically, is readable in a directory listing, and 4 random bytes keep same-second
  collisions rare rather than inevitable (2 bytes collided ~8% of the time in 100 draws).
- **Append-only, like the compactor.** A compaction is a record, not a rewrite: the raw messages
  stay on disk and the view is recomputed on load. Storage is the audit trail; projection is
  the model's view (D11). Rewriting the file to "compact" it would lose the only copy of what
  the user actually said.
- **Crash repair on load, three cases.** A torn last line (the only line that can be torn, since
  every write ends with `\n`) is dropped and reported. Damage anywhere else is a real
  corruption and fails the load with the line number rather than silently shortening history.
  A tool call whose result never arrived gets a synthetic `is_error` result saying *outcome
  unknown* — the model must not be told the call succeeded, and must not be invited to re-run a
  side-effecting command blindly. An orphaned result (no matching call) is dropped. Every
  repair is counted in a `Report` the shell shows at startup.
- **`Load` reopens for append.** A loaded session is the continuation of that file, not a
  read-only view: entry numbering and `parentId` continue from the replayed tail. A write
  failure is latched (first error wins, `Err()` reports it, `Close` returns it) so a full disk
  cannot turn into a per-message error storm or silently interleave half a turn.
- **`/new` creates a new file; it never truncates the old one** (`/clear` only clears the
  screen, D66). The old conversation stays readable by `--resume`; the new one starts empty.
  `--continue` skips sessions with zero messages, so an empty file left by a `/new` then quit does
  not shadow real work.
- **Resume matching.** `--resume` takes a path, an exact id, or an id prefix; `ReadDir` +
  `HasPrefix` rather than `Glob`, so a `*` in user input stays a literal character. An
  ambiguous prefix is an error naming the count, never a silent pick.
- **Directory names.** `<home>`-relative paths encode as `-Code-oh-my-pi`; anything outside
  home as `--c--Code-oh-my-pi--`; home itself as `-home`. Separators and `:` become `-`, and on
  Windows the whole name is lowercased because drive letters arrive as both `C:` and `c:` and
  would otherwise split one project into two session directories.
- **Not adopted:** a single global session database (one corrupt write would take every session
  with it; JSONL survives line-level damage); rewriting the file on compaction (loses raw
  history, D47's elide-first design assumes it is still there); storing provider-native payloads
  verbatim (the neutral types are the format — a renamed field is a version bump, which the
  header version is there to catch).
- **Interop with `omp` (O4) stays open, leaning own format.** The header carries `version` so a
  future importer can distinguish; nothing in the format claims to be `omp`'s. Our tool calls
  are content blocks where `omp` uses a `toolCall` block plus a separate tool-result message, so
  an importer translates rather than copies.

**Known limit:** the file grows without bound until a retention policy exists; the compactor
keeps the *view* bounded but never the *file*. Deferred deliberately — nothing consumes
pruning yet, and pruning is the one operation that can destroy the audit trail.

### D50 — Verification lives outside the model: a check decides, not the assistant's claim

**Decided:** 2026-11-17. **Status:** Accepted. **Executes M2 ⑨.**

- **The judge is external.** `agent.Verifier` gets the session and returns pass or feedback;
  `agent.VerifierRunner` wraps any `Runner`, and a turn that ends `completed` runs the checks.
  The model's own "done" is never the stop condition — that is the whole point of the slice,
  and it is why the check receives the session rather than the model's last message.
- **Feedback goes back as the next user turn**, prefixed with the failure and the rule *do not
  claim success without running it, do not change the check*. Not a system message: the retry
  is a new instruction in the conversation, and the provider sees it exactly as a user would
  send it. Every attempt is appended to the session, so a resumed run sees the full history of
  what failed.
- **Attempts are capped (default 3).** Exhaustion ends with `stop_reason: verifier` and the last
  failure text in `Err` — a run that never passes is reported as failed, never as completed.
- **Intermediate results are not announced.** The inner loop still ends each turn with a
  `Result`; the wrapper swallows it and emits one final `Result` (`Complete`) with usage summed
  across attempts. A shell that renders every inner result would clear its live area and print
  a false "done" per attempt.
- **A failed turn skips the checks.** Checks run only after `completed`; running them after
  `aborted`, `max_turns` or `error` would spend a shell invocation to confirm what is already
  known.
- **`verifier.Command` is the v1 check**: the resolved shell (D33), the workspace as `Dir`,
  bounded output (8 KiB, cut marked), a timeout treated as *failed check* rather than an error
  (a hung check is a real failure), and cancellation propagating as an error. Tree kill reuses
  the same per-OS `prepare`/`taskkill` shape as `bash`, because a check that spawns children
  must not leave them behind.
- **Not adopted:** a promise tag (`<promise>…</promise>`) as the stop signal — text the model
  writes about itself is the thing this slice distrusts; a session-level verifier without an
  outer attempt loop (it would pass the failure back with no mechanism to continue).

**Known limit:** a single command per run (`--verify`); several checks, per-language detection
and model-as-judge are not built. `--verify` runs in the workspace regardless of plan mode —
plan mode still denies the writes the fixed turn would need, which is the intended wall.

### D51 — Tools are dispatched when their argument block closes, not when the message ends

**Decided:** 2026-11-17. **Status:** Accepted. **Executes M2 ⑧.**

- **The trigger is `block_stop` on a `tool_use` block.** That event means the arguments are
  complete — both adapters emit it before `message_delta`. The loop snapshots the block
  (`Accumulator.Block`, a copy with the raw input) and pushes it into the scheduler while the
  model is still emitting later blocks. The tool card appears at that moment, so the UI shows
  work starting during generation instead of after it.
- **The scheduler takes incremental pushes.** `Scheduler` now has `Push` / `Close` / `Collect`:
  a safe tool starts the moment it is pushed; a non-safe tool runs alone and everything queued
  behind it waits (head-of-line blocking preserved); results are released strictly in call
  order regardless of finish order, so pairing never depends on timing. `Run(uses)` — push-all, close,
  collect — survives only as a helper in `scheduler_test.go`.
- **A block whose arguments are not a JSON object is never dispatched.** Inline
  `PartialJSON`/`arguments` can be cut by the output limit, and a block stop may still arrive
  (the OpenAI synth closes open tool blocks at `finish_reason`). Such a call is answered with
  the truncation message and returned to the model, exactly like before; `json.Valid` on the
  whole object is the test, because a half-written object is the one shape that must never run.
- **Ordering with the session is unchanged on the wire.** The assistant message is appended
  before any tool result; results are appended as one user message afterwards. Tools can
  therefore start before that message exists in the session — a tool reading
  `RecentMessages` during its run sees history up to the previous turn. Accepted: that view is
  only used for read-only context.
- **Every exit path drains the scheduler.** Success, abort and stream error all close and
  collect before returning, because `drive` closes the event channel on return and a late
  `em.send` would panic. On abort the context is cancelled first so running tools die and
  queued ones are answered as aborted; on success the turn context lives until the results are
  collected.
- **Degradation is explicit.** `openai-completions` can only close tool blocks at
  `finish_reason` (the protocol has no per-call terminator), so on that transport dispatch
  starts when the message ends — correct, just not early. Anthropic and `openai-responses`
  close each block when it completes and get the full benefit. Detecting a new tool index to
  close the previous one was rejected: a gateway that interleaves fragments would make the
  loop start a tool with half its arguments.
- **Not adopted:** starting a tool on the first argument fragment (arguments are not complete
  until the block closes); parallel calls behind a non-safe call (that is the ordering guarantee
  the scheduler exists to keep); emitting results live as they finish (would break call order
  and the tool-card pairing).

**Known limit:** a long non-safe tool still stalls every later call in the same message — the
model must be told to split work, which is what the truncation and ordering messages already
do. No cancellation of a single tool exists; Esc cancels the whole turn.

### D52 — Block lifecycle is synthesized for Completions by five fixed laws

**Decided:** 2026-11-17. **Status:** Accepted. **Records the rationale behind
`llm/openai/convert.go` (`completionSynth`).**

The neutral event vocabulary is a block lifecycle (Architecture §4.2). Anthropic Messages and
OpenAI Responses announce block start, delta and stop natively; Completions streams a flat
field soup, so its adapter must synthesize the lifecycle. Getting it wrong fails silently — a
tool call executes with empty arguments, or two calls merge into one — so the rules live here:

1. **Lazy open.** Emit a block start only on the first fragment carrying non-empty data.
   Decide on "data non-empty", never on "field present": a `"content": ""` heartbeat is not data.
2. **Close on transition.** Data of a different kind closes the open scalar block; the same
   kind resuming opens a new block, so wire order survives (`text`, `tool`, `text` stay three).
3. **Renumber by arrival.** Wire fragment keys map through a table to contiguous block
   indices; the next index always equals the number of opens emitted.
4. **Never invent completion.** Tool blocks stay open until `finish_reason` or end of stream.
   "The index changed, so the previous tool is done" is unreliable under parallel calls.
5. **One terminal point, fail loud.** Stop reason and usage aggregate into exactly one delta
   plus one stop at the end; a real error takes the error channel with no lifecycle tail;
   anything the vocabulary cannot express is an error, never a dropped event.

**Consequence, stated rather than hidden:** under Completions a tool cannot be dispatched
before the message ends, so D51's early dispatch degrades to end-of-message there. This is the
protocol's shape, not a defect to optimize away. Two vocabulary gaps are handled explicitly:
a fragmented tool name has no backfill channel and is buffered, and the trailing usage chunk
carries an empty `choices` array.

### D54 — Traces are our own NDJSON, one file per run

**Decided:** 2026-11-17. **Status:** Accepted. **First slice of M5; delivers R8.1 and R8.2.**

`--trace <path>` writes every `agent.Event` to one JSONL file: a `header` line
(`{type, version, id, started_at, cwd, model, session}`), one `{type: "event", event: …}`
line per event in emission order, and a `summary` line
(`{reason, err, turns, events, usage, started_at, finished_at}`) written on close. D68 added
request and tool timings, truncations, compactions and peak context to the summary, and turns
and usage now add up across every run in the file.

**Why not OpenTelemetry:** the trace has to be replayable offline by our own eval runner and
readable by a person diagnosing a bad turn; OTel adds a collector, a schema translation and a
dependency to read our own file. The `omp` comparison exports OTel because it is a hosted
product with dashboards; we are not. Revisit when a third party asks for a standard exporter —
the event schema is already the wire format, so translation is mechanical.

**Rules that make it usable:**

- **Versioned header.** `version` follows the session format's discipline: a reader refuses a
  newer version with a clear reason rather than guessing.
- **Best effort, never fatal.** A write error is latched on the recorder and reported once at
  close; it never breaks or blocks a turn (R4.8). A trace that dies mid-run leaves every line
  up to that point intact, which is what a post-mortem needs.
- **Same events the shells see.** The recorder wraps the `runner` (and `Compact`) in `cmd/omp`,
  so print mode, the TUI and `/compact` all record the identical stream — one vocabulary, no
  second instrumentation path.
- **Off by default.** Tracing is a diagnostic, not telemetry: nothing is written unless
  `--trace` names a path.

### D55 — Context windows come from a small built-in table

**Decided:** 2026-11-17. **Status:** Accepted. **Closes the "model window unknown" debt.**

`llm.ContextWindow(modelID)` matches the id against a short built-in table
(`claude-5` → 1M, `claude-3/4` → 200k, `gpt-4.1` → ~1.05M, `deepseek`/`glm-4`/`gpt-4o` → 128k,
`kimi-k2` → 256k, `qwen3` → 131k), longest/specific entry first. When `compaction.contextWindow`
is unset, `cmd/omp` fills it from this table; the config value always wins.

**Why a table and not a catalog:** without a window, automatic compaction never fires and a long
session hits a provider overflow that only the reactive path can rescue — the feature exists but
is off by default for everyone. A catalog (models.dev-style, fetched and cached) is the right
long-run answer and is what M6 planned; a table of the families we have actually seen unblocks
the feature today at about thirty lines.

**Deliberately conservative:** an unknown model id returns no window, and then no automatic
compaction — a wrong window is worse than none, because it silently compacts at the wrong size.
Entries are added only for models someone has run, with the window verified against the provider
docs at that moment. `ContextWindow` is public, so the eval harness and future shells can use it
without loading config.

### D56 — Skills are declared in `SKILL.md`, loaded on demand

**Decided:** 2026-11-17. **Status:** Accepted. **M4③; delivers R6.2.**

A skill is a directory containing `SKILL.md`: a `---` frontmatter block with `name` and
`description`, then the procedure as markdown. Discovery reads `<cwd>/.omp/skills/<dir>/` first,
then `~/.omp/skills/<dir>/`; the first definition of a name wins, so a repository overrides a
global skill. A directory whose frontmatter lacks a name or description, or whose body is empty,
is skipped — a broken skill must not stop the agent from starting.

**Two blocks, two costs.** The system prompt carries only `name: description` per skill, so a
dozen installed skills cost a dozen lines. The body is returned by the `skill` tool when the
model calls it, and nowhere else. This is the whole point: a procedure that is only sometimes
needed must not be paid for on every request.

**After compaction, a loaded body can be elided — and that is correct.** `skill` is in the
compactor's reproducible set, so an elided body becomes "re-run skill to see it again", exactly
as a file body becomes "re-read it". The body is not pinned to the request, because pinning is
the mechanism that makes context grow without bound; re-loading is one cheap call and is exact.

**Why not `Extra` re-injection:** `ToolResult.Extra` survives in the session as an ordinary user
message, but the compactor summarizes the conversation region regardless of message origin. A
mechanism that pins skill text through compaction needs a per-message marker the neutral model
does not have; adding one for this alone is premature (Law 8). Revisit when a real session shows
the model re-loading the same skill repeatedly after a boundary.

### D57 — MCP is a client over the official SDK, bridged as write-tier tools

**Decided:** 2026-11-18. **Status:** Accepted. **M4④; delivers R6.1.**

`mcpServers.<name>` in `~/.omp/config.yml` names a command, its arguments and extra environment.
At startup each is spawned with `exec.Command` (never through a shell) and connected with
`github.com/modelcontextprotocol/go-sdk`; `tools/list` is read and every remote tool is bridged
into an `agent.Tool`. An entry with a `url` instead connects over streamable HTTP, or SSE with
`transport: sse` — added by D66, which also renamed an invented `type` key to the blueprint's
`transport`.

**The bridge owns four decisions:**

- **Names are `mcp__<server>_<tool>`**, lower-cased, non-alphanumerics collapsed to `_`, server
  capped at 16 characters and the whole capped at 64 so a long remote name cannot break a
  provider's tool-name rules. `omp`'s single-underscore form is used because a model
  primed on it emits the name reliably; the alias fallback is not built until a model is seen
  missing the name.
- **Write tier and `open-world` by default.** The local side cannot know what a remote tool does,
  so the conservative declaration is the correct one; an approval rule narrows it per tool
  (`mcp__browser_screenshot` etc). Registration goes through the same `permission.Wrap` as the
  built-ins, so a runtime-added tool cannot bypass approval.
- **No scheduling declaration** — the fail-closed default (exclusive, one at a time) stands until
  annotations prove otherwise.
- **A failed server is reported, not fatal.** A broken `mcpServers` entry prints one line to
  stderr and the agent starts without it; these are user-configured external programs, and one
  of them being unavailable must not stop the session. Each connect has a 20 s timeout.

**Why the SDK and not a hand-written client:** the protocol is JSON-RPC with version negotiation,
capability discovery and paging; that is exactly the kind of wire work Law 6 says belongs to a
library. The SDK surface is used only through `Connect` / `ListTools` / `CallTool` / `Close`, so
replacing it later is a change inside this package.

**Not adopted yet:** resources and prompts (tools only),
server-initiated notifications, and re-connecting a dropped session. Each waits for a consumer.

### D58 — Hooks are subprocesses exchanging one JSON object each way

**Decided:** 2026-11-18. **Status:** Accepted. **M4⑤; delivers R6.3.**

`hooks.preTool | postTool | stop | prompt` in `~/.omp/config.yml` each name a command, arguments,
environment and an optional timeout (10 s default). Each hook is spawned per call with
`exec.Command` (never a shell), receives one JSON object on stdin, and answers with one JSON
object on stdout:

| Event | In | Out |
|---|---|---|
| `pre_tool` | `{event, tool, input}` | `{block, reason, input}` |
| `post_tool` | `{event, tool, result:{text,is_error}}` | `{result:{text,is_error}}` |
| `stop` | `{event}` | `{prevent, reason}` |
| `prompt` | `{event}` | `{messages:[{role,text}]}` |

**Where each lands in the loop:**

- `pre_tool` runs **before approval and before execution**, so what was approved is what runs
  (Architecture §4.3). It can block, rewrite the arguments, or both. A hook that errors or times
  out is reported as a notification and **does not block** — an observer must not stop real work
  by breaking.
- `post_tool` rewrites the result only; a nil or failing hook keeps the original.
- `stop` runs immediately before `completed` is emitted; `prevent` appends its reason to the
  session as a user message and the turn continues. This is what makes "keep going until the
  check passes" expressible as a hook rather than a new stop reason.
- `prompt` runs at the top of every round, before the request is built. When it fails, the
  messages it returned before failing are still injected and the failure is reported once
  (D70), so one broken hook cannot erase another's injection.

**Format choices:** newline-delimited JSON in, one JSON object out — the same wire discipline as
the trace and the RPC mode (D64), so there is one vocabulary to learn. A `system` message from a
prompt hook is wrapped in `<hook role="system">` because the neutral model has no system role
inside the message list and pretending otherwise would quietly upgrade a hook's authority.
Non-JSON stdout, an exit code, or an over-long output are protocol errors carrying stderr's first
line.

**Why subprocesses:** the same reason `omp` chose them — a hook must be writable in any language,
must not be able to crash the agent, and must not need a plugin API we would then have to keep
stable (Architecture §5, Requirements §2.2).

### D59 — Background tasks notify with pointers, never with output

**Decided:** 2026-11-18. **Status:** Accepted. **M4⑥; delivers R6.6.**

A `background` tool with three actions: `start` launches the command through the same shell as
`bash`, redirects both streams to `~/.omp/background/<id>.log`, and returns the id and the log
path at once; `wait` blocks until that task ends or its timeout (30 s default, 600 s cap);
`list` shows what this process has started. The tool is `exec` tier — it runs code.

**The notification is a pointer.** When a task finishes, the manager queues
`{id, status, log path, last 8 lines}`. The kernel drains notifications at every round boundary
through `RunDeps.Notify` (the same seam shape as steering), appends one `<background-task
id="…">` user message and emits an event; a task that finished while the model was about to stop
keeps the turn alive rather than waiting for the user to type. The full output is never pasted
into the conversation — it is one `read` away, and reading a 5,000-line build log into context is
exactly what background execution exists to avoid.

**Why a manager and not `bash &`:** the shell's backgrounding loses the exit code, gives no
notification, and on Windows detaches into a pipe nobody drains. The manager owns the process, so
the status is real and the log file is closed on exit (a Windows open handle otherwise blocks
cleanup).

**Known limits, stated:** tasks do not survive the process — this is a background command inside a
session, not a daemon; the manager does not kill a running task on shutdown; and the 8-line tail
is a summary, not a contract (the log is the contract).

### D60 — Session titles and exports are append-only operations

**Decided:** 2026-11-18. **Status:** Accepted. **M4⑦; delivers R4.4's "sessions are listed and
selectable" half.**

Three operations, each on the `session` package rather than the shell:

- **Rename** appends a `title` entry (`{type:"title", title}`). The last one wins and `describe`
  reads it for listings, so the picker and `/resume` show a human name instead of a truncated
  first prompt. A title never enters `Messages()`, never reaches a request, and does not count as
  damage on replay. Rewriting the header instead would have broken the one rule the session
  format has — storage is append-only (D49) — to save four kilobytes.
- **Export** renders the stored messages to markdown (`**user**` / `**assistant**`, tool calls as
  `[tool name]`, tool results truncated at 4k) and writes it wherever the shell says; the default
  is `<session id>.md` in the working directory. Export reads the session, never rewrites it.
- **Delete** removes one `.jsonl` file and refuses anything else — a path must end in the session
  suffix and must exist, because delete resolves user input through the same prefix matching as
  `--resume`. The current session cannot be deleted; the shell answers "start a new one first".

**What is deliberately absent:** session *fork* / branch and a tree picker (D25 cut branching, and
nothing since has needed it); renaming compacting or rewriting old files; and a trash-can undo.
Each is a new request with a consumer, not a gap to paper over.

### D61 — Isolated subagents work in a git worktree and merge a patch

**Decided:** 2026-11-18. **Status:** Accepted. **Executes M4⑧; delivers D12's worktree half.**

**Problem.** A subagent shares the working directory with its parent. A subagent that edits is
therefore a subagent that can leave half-finished files in the tree the user is editing, and no
amount of approval prompts fixes that because the parent already approved the same kind of edit.
What was missing was an undo that does not depend on the model behaving.

**Decided:** the `task` tool takes `isolated: true`. The child then runs in a throwaway
`git worktree` of the repository the session sits in, and its changes reach the parent only
through a patch that applies cleanly.

- **The worktree** is created with `git worktree add --detach <dir> HEAD`, where `<dir>` lives in
  `%TEMP%\omp-worktrees/<timestamp>-<random>` — outside the repository, so it can never be
  accidentally committed or picked up by a glob. A detached HEAD keeps the user's branch list
  clean. The worktree shares the object database, which is what makes the patch computable from
  the parent afterwards.
- **The child is rooted there by options, not by rebuilding tools.** `Options.CWD` becomes the
  worktree and `Options.Workspaces` is set to it, and `rootsOf` prefers workspaces, so every
  built-in tool resolves paths inside the worktree and an absolute path into the parent repo is
  rejected as out of tree. The registry is shared; only the per-run options differ. This is why
  `Options` on `subagent.Config` is a function of a directory rather than a struct.
- **The merge is a patch, not a branch.** Inside the worktree the child's result is staged
  (`git add -A`) and committed on the detached HEAD with an identity and signing passed on the
  command line, so the user's git config is neither read nor needed. The parent then runs
  `git diff <base> <child>` and applies it with `git apply`. A branch merge or `cherry-pick`
  would have written a commit into the user's history, which is a decision the user did not ask
  the tool to make. A patch touches the working tree and leaves history alone.
- **Failure is safe by construction.** `git apply` is atomic per patch and writes nothing when
  it fails, so a conflict cannot leave half-applied files. The patch is saved as a `.patch`
  beside the worktrees and named in the result, the worktree is removed anyway, and the child is
  reported as failed with `merge conflict` — the model learns what happened and that its work is
  still recoverable.
- **The worktree is always removed**, on success, on failure, and after a panic in the child.
  `git worktree remove --force` plus `prune`; a locked directory falls back to a plain recursive
  delete. On Windows this is the one step that can fail for reasons outside our control (an
  editor holding a file open), so it is best-effort and never masks the result.

**Why a worktree rather than a copy.** Copying the tree would need to know what to copy (ignored
files, large directories) and would produce diffs git cannot apply with any confidence. A
worktree is the same repository state by definition, costs one checkout, and makes the merge a
git operation rather than a diff we wrote ourselves.

**Also narrowed while here:** the child registry now omits `exit_plan_mode` and `background` in
addition to `task` — a subagent cannot leave plan mode for its parent and its background
processes outlive the throwaway worktree they were started in. The child keeps only the
pre-tool and post-tool hook families: `stop` and `prompt` belong to the shell's turn loop, and
letting a child trigger them made one subagent run the user's prompt injections twice. The
child no longer inherits `Notify`, so it cannot drain background notifications meant for the
parent.

**What is deliberately absent:** automatic isolation (the model asks for it — a read-only
subagent has nothing to isolate), a review UI for the patch before it applies, and nested
isolation for subagents of subagents (they cannot exist; `task` is not in the child registry).

**The child has no UI, so approval does not cross the boundary per call.** A subagent loop is
built with no `Host`, and in default mode every write tool evaluates to "ask", so the child
could never write anything — isolation would have produced an empty patch every time. The child
now gets its own policy derived once, when the parent assembles the registry: if the parent's own
mode and rules would allow a write-tier call, the child runs in bypass mode over the same rules;
deny and ask rules still win because `Evaluate` checks them first, and a parent in plan mode hands
down plan mode unchanged. In other words the parent approves the delegation, not each of the
child's steps — which is the point of delegating — while explicit denials remain explicit.

**Bash resolves its directory from the run, not from construction.** Every other tool already
preferred the per-run tool context, but `bash` took its construction-time `CWD` first, which in a
subagent is still the parent repository. It now prefers the run context's workspaces, then its
cwd, then the construction values. This is the one place where an isolated child could have
written into the parent tree while every test passed, which is why it carries its own test.

### D62 — Model calls retry by class, never after output

**Decided:** 2026-11-18. **Status:** Accepted. **Closes M6's retry slice.**

**Problem.** A provider call fails for three unrelated reasons: the network or the service is
momentarily unavailable (retrying is correct), the request is wrong (retrying wastes the user's
money and minutes), or the conversation no longer fits (retrying is exactly the wrong move — the
compaction path exists for that). Without a classifier every failure was terminal, which turned a
one-second blip into a dead turn.

**Decided:** errors are classified into `transient`, `fatal` and `overflow`, and a provider
wrapper retries the first kind with backoff.

- **Classification lives in the adapter.** Both SDKs keep their HTTP error type in an `internal`
  package, so the only place a status code can be read is next to the SDK — `llm/anthropic` and
  `llm/openai` each translate their error into `llm.HTTPError{Status, RetryAfter, RequestID}`,
  which is neutral, wraps the original (so `errors.As` still works), and carries the `Retry-After`
  header when the server sent one. `llm.Classify` then works on that alone.
- **Retries are a `Provider` wrapper**, `llm.NewRetrying`, sitting between the effort wrapper and
  the real provider. The loop, the tools and the compaction path never learn that a retry
  happened; `cmd/omp` is the only assembly that knows the policy exists.
- **The defaults:** four attempts, 500 ms doubling to a 20 s ceiling, ±25% jitter, a two-minute
  total budget, and the server's `Retry-After` wins over the computed backoff. `context.Canceled`
  is fatal, never retried.
- **Overflow is never retried here.** It is classified, then handed back untouched, because
  `agent/Loop` already knows what to do with it (compact, then re-stream). Retrying a request the
  model itself rejected as too long would just burn attempts before the same answer.
- **A stream that already produced output is never replayed.** The wrapper only retries when
  nothing reached the consumer. Once text or a tool-call block has been yielded, the failure is
  surfaced: replaying would duplicate content the user has already read, and for tool calls it
  would duplicate side effects. This is why `Stream` — not `Complete` — owns the retry loop and
  `Complete` is built on top of it.
- **Visibility:** in print mode each retry writes one line to stderr naming the class, the delay
  and the attempt. In the TUI nothing is written — a raw stderr write would corrupt the live
  area — and a notification event for retries is deferred until something can render it without
  a fake tool card.

**Deliberately absent:** retry counts and delays in `config.yml` (config v2 has no second
consumer yet), fallback to another provider (M9, with model roles), and retrying
a partially delivered stream by resuming from a saved offset (no provider supports resumption
today).

The tests that matter: a transient failure is retried and the turn succeeds; an auth failure is
not retried; the attempt budget ends the loop with the last error intact; `Retry-After` beats
backoff; a stream that already yielded text surfaces the error without a second attempt.

### D63 — M6 closes on two measured acceptance runs, not inference

**Decided:** 2026-11-18. **Status:** Accepted. **Closes M6's acceptance criteria** except the
metadata slice, which has no consumer yet.

M6's promise was "a long session compacts and keeps working; a killed process resumes with every
tool call still paired". Both halves were previously argued from unit tests — the compactor's own
math, and the store's repair function tested on hand-built message lists. Argued is not measured,
so both are now acceptance tests that drive the real components.

- **Two times the window** (`cmd/omp/compaction_acceptance_test.go`): a 4000-token window, 40
  scripted turns of ~3200-byte tool results, and the real `compaction.Compactor` behind the real
  `agent.Loop`. The run finishes, and it crosses the threshold **fifteen** times, each compaction
  shrinking the projection. The fixture is sized so the task genuinely totals more than twice the
  window — the test asserts that arithmetic rather than trusting the turn count to imply it.
- **A real kill** (`cmd/omp/kill_test.go`): a child process of the test binary writes a session
  through the real store — one user turn, one assistant turn holding a tool call — closes the
  file so the bytes are durable, and then blocks on its stdin, which the parent holds open as a
  pipe, until the parent kills it. The parent does kill
  it with no chance to clean up, then loads the file and asserts the repair: one synthetic
  `tool_result`, marked as an error, naming the call and saying the outcome is unknown. It then
  feeds the loaded session to a loop and checks the request the model received pairs the call.
  A second test goes through `openConversation` with `--continue` and asserts the shell says the
  session was repaired.
- **Why the kill is a process kill, not a simulated crash:** the failure being defended against is
  bytes on disk when the runtime stops. Unwinding a goroutine would leave the buffer flushed, and
  the test would pass on a store that writes nothing until `Close`.

**Left open deliberately:** model metadata and config v2. The context window already comes from
D55's built-in table and `Config.Window`; what remains is a place to declare retry counts and
delays, and neither has asked for a second consumer yet. Adding knobs before the first real
override is the over-design the constitution's rules 7 and 8 forbid.

**What this does not claim:** that compaction is cheap or that the summaries are good. M6 measures
survival, not quality; the trajectory eval is where quality would show.

### D64 — The wire is a versioned NDJSON envelope, not the Go event struct

**Decided:** 2026-11-19. **Status:** Accepted. **Executes M7 ②, ③ and ④.**

**Problem.** The engine's event struct was already JSON-tagged, so the tempting move is to
`json.Marshal` each `agent.Event` and call that the protocol. That would freeze every field the
kernel happens to own — `Result.Messages`, `Result.Complete`, the internal `Seq` bookkeeping —
into a promise we would then have to keep. And it would give a consumer no way to tell which
schema version it is reading.

**Decided:** the wire has its own types.

- **One envelope, one version.** Every frame is `{"v":1,"type":…}`. `v` is the protocol version,
  bumped when a frame changes meaning; a consumer that sees a higher number can refuse instead
  of guessing. The frame types in v1: `ready`, `event`, `ask`, `error`, `bye`, `idle`.
- **The event body is a projection, not the struct.** `wireEventBody` copies what a consumer can
  act on — kind, seq, timestamp, text, tool id/name/input, approval, tool result as `{text,
  is_error}`, usage, message, stop reason, turns, compaction — and drops `Result.Messages` and
  `Result.Complete`. The transcript is available through the session file and the state snapshot;
  repeating it in every result frame would double the payload for no consumer.
- **`tool_result` is flattened.** Internally a result is content blocks; on the wire it is the
  text plus an error flag, because that is all the two shells ever rendered. A future block type
  (an image) extends this deliberately, rather than leaking `llm.ContentBlock` into the contract.
- **`--mode json` is the whole run as frames**, ending with the `result` event carrying a state
  snapshot for a GUI that joins late. `--mode rpc` is bidirectional: the same event frames plus
  `ask` frames for approval. Commands in v1 are `prompt`, `answer`, `quit`; `answer` names the ask
  id, so two approvals can be open at once.
- **Quit during a run is deferred, not dropped.** The command loop records `pendingQuit`, lets the
  turn finish, then sends `bye` and exits. Killing the loop on the first `quit` would have
  cancelled a run the client never asked to abort, and the exit status would have described a
  turn nobody saw.

**Why not gRPC or JSON-RPC over HTTP?** The consumers are an editor plugin and a GUI spawned by
the same binary; NDJSON on stdio needs no port, no handshake and no dependency, and a line is a
frame that a human can `tail`. If a network transport is ever needed, the envelope is the part
worth keeping and the framing is replaceable.

**Deliberately absent:** streaming partial tool-call arguments on the wire (D51's block lifecycle
is internal; a consumer sees a call when it is complete and valid), a `state` command (the result
frame carries the snapshot), and authentication (stdio implies the same trust boundary as the
process).

The tests that freeze this: every frame carries `v:1`; a `--mode json` run yields tool and result
frames whose fields match the documented shape; an rpc approval reaches the client as an `ask`
frame and an `answer` releases the tool; a `quit` arriving mid-turn ends with `bye` and a clean
exit.

### D65 — Host-executed tools are a declaration plus a frame; exports without an outside consumer stay

**Decided:** 2026-11-19. **Status:** Accepted. **Executes M7 ⑤, ⑥ and ①.**

**External tools (⑤).** Some tools only a host can run: an editor knows its open buffers, a GUI
knows its window state, and the host runs them on its own terms: its process, its credentials,
its approval.

- **`agent.RemoteTool`** is a first-class `Tool` that carries its name, description, schema and
  safety declarations, and delegates `Execute` to the `Ext` the tool context's host implements.
  It is registered like any other tool, so the approval wrapper, the scheduler and the schema
  check all apply without special cases. Declared read-only means it schedules in parallel;
  declared write means it asks. Missing host is an error result that says so, not a panic.
- **`--external-tools <file>`** declares them: a JSON array of `{name, description, schema,
  read_only, write, open_world}`. No manifest, no remotely hosted tools. A declaration that omits
  the name is rejected at load, because a nameless tool can never be called.
- **On the wire** a call is a `tool_call` frame (`{id, call:{name,input}}`) and the answer is the
  client's `tool_result` command (`{id, text, is_error}`), which resolves the waiting call by id.
  Approval and external execution therefore share one request/response discipline.

**The embedding example (⑥)** is a test, `cmd/omp/embed_test.go`, not prose: it registers a
custom tool, a custom compactor and a permissive host, runs a multi-turn session, and asserts
the tool ran, the answer arrived and the compactor was consulted. It imports only public
packages, so it fails the moment the public surface stops being sufficient — which is the only
way an "embedding guide" stays true.

**The export review (①)** counted real consumers for every exported symbol added or touched
since D48 made the tree public. Two findings, both recorded rather than silently fixed:

- `Registry.NotFound` and `Registry.Schemas` have **no consumers outside the `agent` package** —
  the loop and the tests are the only callers. They stay exported because the SDK's stated goal
  is that a host can render what the engine would say (an unknown-tool message with suggestions)
  and can list schemas for its own tool picker, but that is intent, not usage. Marked here so a
  future review does not mistake them for proven API.
- Everything else added this milestone — `SetWrapper`, `SetHost`, `RemoteTool`, `ServeStdio`,
  `Ext` — has at least one consumer outside its defining file, most of them in `cmd/omp` or the
  frame tests.
- **Kept on purpose (user, 2026-11-23).** A later review found more exports whose only callers
  are tests — `Loop.SetHost`, `llm.ThinkingBlock`, `mcp.ServeStdio`, `permission.Decision.Allowed`,
  `trace.Recorder.Err`, `eval.Reason` — and `ToolResult.Extra`, which the loop reads but no
  built-in tool sets. They are SDK surface and stay: a review records such findings, it does not
  delete them.

**Deliberately absent:** authentication on the wire (stdio assumes the same trust boundary as the
process), a schema for the external tool manifest beyond the fields listed (a host that needs more
defines its own tool set in code), and streaming a partial tool result (v1 sends one result).

### D66 — Read the blueprint before inventing

**Decided:** 2026-11-19. **Status:** Accepted.

**The rule**, alongside R11.2: before building a mechanism, read how the blueprint does it. An
audit of REQUIREMENTS against the tree found five gaps — result-size limits declared but never
enforced, a `token_budget` stop reason no code path produced, no cost accounting, MCP over stdio
only, and `/clear` sharing `/new`'s implementation. Four were first built from memory; reading the
blueprint then changed two of those designs and removed a third piece outright. Reading costs ten
minutes; guessing produced a mode that never could have worked and a config field that disagreed
with the reference.

**What reading it settled.**

- **The blueprint has no live-model golden suite.** `packages/coding-agent/bench/` is ten files
  of `model: "mock"`. Its golden-style assertions pin the *script*, not the model, and a live
  model takes its own path — a `--eval-live` mode failed 13 of 15 cases exactly that way. That was
  a design error, not a harness bug: **the mode is deleted** and the harness stays scripted-only.
  Evaluating a real model is not built.
- **MCP's config field is `transport`**, defaulting to stdio with a command and http with a url
  (`mcp/config.ts`). We had invented `type`. Renamed; the `url` field and the http/sse split
  already matched the blueprint.
- **Truncation is artifact-first in the blueprint** (`spillLargeResultToArtifact`: store the full
  body, inline a head+tail view with an `artifact://N` pointer). Our central `clampResult` is
  only the last-resort cap, and that is the part worth keeping today: artifact storage needs a
  consumer that reads pointers back, and none exists until a UI renders them. The clamp stays;
  the spill is written down as the trigger for when one does.

The cost display this entry also corrected left with cost itself (D68).

### D67 — Markdown is parsed by goldmark and laid out by our own renderer

**Decided:** 2026-11-20. **Status:** Accepted. **Replaces** the line-by-line regex renderer of M3④.

**Problem.** The shipped renderer matched one line at a time with regular expressions, so it
could not see a block. Tables arrived as raw pipes. `#42 is fixed` became a bold heading with
the `#` dropped. Italic and strikethrough stayed literal, and so did escapes and entities. CJK
prose has no spaces, so `ansi.Wrap` either broke it at an arbitrary cell (a row could start with
`，`) or at the last ASCII space, leaving ragged rows. The user's verdict was that the markdown
rendering was not done well, and it was right.

**Cross-check (Law 7).**

| Reference | How it renders | What we take |
|---|---|---|
| `omp` `packages/tui/src/components/markdown.ts` (3,989 lines) | `marked` lexer plus its own token renderer: hanging list indents, column-fitted tables with a raw fallback when too narrow, bordered quotes, highlighted code, OSC 8 links, math, mermaid; a streaming freeze that commits up to a stable block boundary (`stableBlockBoundary`) | The split (a real parser, our own layout), block-boundary commits, the narrow-table fallback, and the theme roles: quote text gray, code blocks in their own colour, bullets in the accent |
| `pigo` `internal/cli/tui/markdown.go` | glamour over a finished turn only; it had to stop glamour's own OSC 11 background query because the reply leaked into the input box, and it hard-wraps glamour's output because tables and code overflow | The warning: glamour competes with Bubble Tea for the terminal and cannot stream |
| `trpc-agent-go` | goldmark for parsing (knowledge chunking, Telegram formatting); no terminal renderer | goldmark as the Go CommonMark parser |

Norma has no markdown library in its modules.

**Decided.**

- **goldmark v1.8.2 parses; `tui` lays out.** GFM tables, strikethrough and task lists are on.
  goldmark has no dependencies of its own. The renderer walks the AST in `tui/markdown.go`,
  `markdown_table.go` and `wrap.go`.
- **Emphasis is CommonMark except next to CJK.** Under the stock flanking rule the closer in
  `**注意：**这个` sits between punctuation and a letter and cannot close, so a plain CommonMark
  parser would regress a case the old regex got right by accident. The emphasis parser is
  replaced by one that treats CJK characters and fullwidth punctuation as letters for `*`, so that
  sentence and `这是**“引用”**吗` are bold. `_` keeps the strict rule, so `snake_case_name` stays
  literal.
- **Layout is glyph-based.** Every grapheme cluster carries its display width. Latin text breaks
  at spaces, CJK text between any two characters, except that closing punctuation never starts a
  row and opening punctuation never ends one. Soft line breaks stay line breaks, as in `omp`,
  because models write them as such.
- **Tables fit their content.** Each column starts at its longest word (capped at 30 cells);
  spare width goes one cell at a time to whichever column saves the most rows. Below one glyph per
  column the table falls back to one line per row.
- **Finished blocks commit while the answer streams.** The text up to its last newline is
  parsed, and everything before the last top-level block is final: appended text can only change
  that last block (a paragraph can still become a table or a heading, a list can still gain
  items). A test feeds a mixed document one character at a time and checks that the committed rows
  equal a render of the whole document. The live tail is re-rendered only when its text changes.
- **Styles follow `omp`'s themes, without highlighting.** Inline code and code-block lines are
  coloured. Quotes are dimmed behind a bar. Bullets and numbers use the accent colour, as do all
  headings (D75). A link's target is shown dimmed after its label unless the two are equal. Each style
  combination's escape-code prefix and suffix is computed once from the existing lipgloss styles.
  The palette therefore has one home, and lipgloss is not called for every span.

**Why not glamour.** It lays out whole documents only, so blocks cannot be committed mid-stream.
Its automatic style queries the terminal without going through Bubble Tea (pigo's documented
leak). Its emphasis is goldmark's stock rule and its wrapping is space-based, so both CJK
problems remain. It also pulls in chroma: measured today, chroma adds 5.6 MB to an empty program,
and registering its lexers costs 6.5 ms and 2.5 MB of allocation at startup.

**Measured cost (2026-11-20).**

- Binary: `omp.exe` is 51.7 MB against 50.6 MB for the 2026-11-19 build. goldmark's own
  symbols are 236 KB. goldmark with its three extensions adds 1.37 MB to an empty program.
- Startup: package init is about 1.75 ms for goldmark (`util` 0.71, `parser` 0.53, `extension`
  0.51) and 0.52 ms for `tui`.
- 23.6 KB of mixed CJK and Latin markdown with tables, lists, quotes and code parses in 0.68 ms;
  parsing and rendering together take 6.1 ms (6.9 ms before the style prefixes were cached).

**Deliberately absent.**

- Syntax highlighting: chroma costs too much (see above). The trigger is in the gap ledger.
- OSC 8 hyperlinks: the targets models write are mostly relative paths, and there is nothing yet
  to resolve them against.
- Math, mermaid and HTML normalisation: no consumer.
- Incremental parsing: the live tail is one block in practice.

**Known consequence.** A link reference definition that arrives after the block using it cannot
turn the already committed block into a link.

**Revisit when** real model output shows the CJK emphasis rule matching where it should not (the
fallback is one line in `newMarkdownParser`), or when highlighting is asked for.

### D68 — Observability is measured where it happens and folded where it is read; cost leaves the plan

**Decided:** 2026-11-23. **Status:** Accepted. **Closes M5 without the cost line.**

**Problem.** A run could not answer four questions: how long each tool and each model call took,
how often the output cap truncated a response, how often the context was compacted and how close
it came to the window, and what a subagent was doing while its card spun. The blueprint answers
them in two places. A run collector (`packages/agent/src/run-collector.ts`) buffers one record per
chat and per tool call and folds them into a summary on `agent_end`. The task executor's
`AgentProgress` (`task/executor.ts`) counts the child's tool calls, names the current tool and
adds up its tokens. pigo flattened the first into a `TelemetryEvent` emitted just before
`agent_end`.

**Decided.**

- **Measure at the source.** The scheduler times each call from the moment it holds a slot until
  it returns. That covers the pre-tool hook, the approval wait (the policy wrapper runs inside
  the call), execution, the post-tool hook and the result clamp. Waiting behind an exclusive tool
  or the concurrency gate is not counted. `tool_result` carries the time as `duration_ms`. Each
  `usage` event carries the response's `stop_reason` and the model call's `duration_ms`, provider
  retries and backoff included.
- **Fold where it is read, not in a new event.** A telemetry event needs a consumer that reads
  events but not the trace, and none exists. The trace recorder folds the stream into its summary
  line instead:
  - `requests` and `request_ms`
  - `truncations`, the responses stopped by `length`
  - `compactions`, successful ones only
  - `tools`: calls, errors and total milliseconds for each tool name
  - `peak_context_tokens`, plus `context_window` and `peak_context_utilization` when the window
    is known

  Turns and usage now add up across every run in the file. Before this, the last run overwrote
  them, and so did a `/compact`. Headless consumers get the same raw facts on the wire: tool
  result frames carry `duration_ms`, usage frames carry `stop_reason` and `duration_ms`. Both
  fields are additive, so `v` stays 1 (D64).
- **The peak, not the latest.** The TUI shows the latest share live, in the blueprint's format
  (`formatContextUsage`: `ctx 27.0%/200k`). A trace reader wants to know how close the run came
  to the window, and the latest value after a compaction hides exactly that.
- **Subagent progress is the child's own events, filtered.** `Task.Run` and `Task.Isolated`
  take a progress callback, and the child loop feeds it every event. On each tool call and each
  response, the task tool reports the current tool name and input and the tokens so far. The
  kernel stamps any event a tool emits with the calling tool's id, so the reducer attaches the
  report to the right card. The TUI shows it as one live line under the running card
  (`⎿ Grep(truncat) · 1.2k tokens`) and drops it when the card finishes or is cancelled. The
  blueprint also keeps recent tools, recent output, request counts and cost; nothing here reads
  them.
- **Finished cards show execution time.** A card used to show the gap between its `tool_use`
  and `tool_result` events. That gap includes queueing and, because results are released in call
  order after the message ends, the rest of the model's stream: a 5 ms read could show 12 s. The
  card now shows the measured `duration_ms`. The live counter still runs from dispatch, because
  no event marks the start of execution.
- **Cost leaves the plan and the code.** This was the user's call. The gateway models in use
  have no price, and a price typed in by hand is worse than none (D66). The pricing code went
  with it:
  - `llm/pricing.go`: the seven-family table, `ModelPrice` and `CostOf`
  - `Usage.CostUSD`
  - `RunDeps.Model`, whose only reader was the pricing call
  - the status bar's cost segment

  Sessions and traces written while usage carried `cost_usd` still load, because the field is
  ignored; a session test pins that. A `prices:` config block or a catalog comes back when the
  user asks for it.

**Two corrections made on the way.**

- `VerifierRunner` built its own result whenever the check ran. That result had `turns: 0` and
  only the last attempt's usage, and a failed attempt reported only itself. Both now add up
  across attempts.
- `cmd/omp/kill_test.go` did not always kill a live process. The helper blocked on its stdin,
  but it was given the null device, so it usually exited before the parent's kill, which then
  failed with `TerminateProcess: Access is denied` under load; the repair assertions passed anyway
  because the file on disk was the same. The parent now holds the helper's stdin open as a pipe
  (D63).

**Public surface changed.**

- `subagent.Task.Run` and `Task.Isolated` gained the progress parameter. This is a breaking
  change to a public package, made while the package has no external consumer.
- `agent.Event` gained `StopReason` and `DurationMs`, and `agent.Done` gained `Elapsed`.
- `presentation.Tool` gained `DurationMs` and `Progress`.
- `trace.Meta` and `tui.Options` gained `Window`.
- Removed: `llm.Price`, `llm.PriceEntry`, `llm.ModelPrice`, `llm.CostOf`, `llm.Usage.CostUSD`
  and `agent.RunDeps.Model`. Usage objects on the wire, in traces and in session files lose
  `cost_usd`. The field was optional and absent for every unpriced model, so readers already
  handled its absence, and `v` stays 1.

**Deliberately absent.**

- A `compaction_start` event. No consumer asks for it: the shell already shows the run as busy,
  and a finished compaction prints its notice. Revive it if a long summarization is reported as
  confusing.
- Tool status buckets beyond error. `ToolResult` cannot tell blocked from timed out.
- Coverage of registered but unused tools.
- OTel export (D54).

**Known consequences.**

- A subagent's tokens appear in its task result and its progress reports, but not in the
  parent's usage, its token budget or the trace summary.
- A tool that queued shows a longer live counter than its finished time.

**Revisit when** a consumer reads events but not the trace (a summary event would then earn its
place), when subagent tokens must count toward the session's usage or budget, or when the user
asks for cost.

### D69 — User-level files live in `~/.omp/`, beside the TS `omp`'s `agent/`

**Decided:** 2026-11-23. **Status:** Accepted (user: "我想把配置文件目录也改成.omp/吧方便统一管理").

- **Everything user-level moved from `~/.pi/` to `~/.omp/`**: `config.yml`, `sessions/`,
  `background/` and `skills/`. Project-level files were already under `.omp/` (D56), so one name
  now covers both levels, and it is the binary's name. It also ends the clash with upstream
  `pi`, which keeps its own files under `~/.pi/`.
- **Flat under `~/.omp/`, not `~/.omp/agent/`.** The TS `omp` keeps its live state in
  `~/.omp/agent/` — `config.yml`, `models.yml`, sessions in its own format, skills that other
  installers manage — and `reports/`, `logs/`, `plugins/` and `remote/` at the root
  (`packages/utils/src/dirs.ts`). Sharing `agent/` would let `--continue` pick up a session in a
  format we cannot read and put two products' settings in one file. None of our names collide
  with the TS `omp`'s, and its `~/.omp/agent/skills` are not read.
- **No fallback to `~/.pi/`.** The one installation was moved on 2026-11-23 — the files renamed in
  place and the empty `~/.pi/` removed — so a fallback would have no consumer (Law 8). A binary
  built before this change still looks in `~/.pi/` and finds nothing.
- **In `cmd/omp`, `configPath()` is the only place that names the file**; the config loader, the
  `/model` switch and the first-run wizard all call it. Each package keeps its own default
  directory (`session.Root`, `skill`, `background.NewDefault`).
- **Unchanged:** every config key, and the rule that keys come only from this file (D4).

**Reverse if** the user wants the TS `omp`'s skills as well: add `~/.omp/agent/skills` as a third
discovery root rather than moving ours.

### D70 — Goal mode: an outer runner keeps an objective going until the model completes it with evidence

**Decided:** 2026-11-24. **Status:** Accepted (user: "开始实现goal模式和memory"). **Admits the
M9 "goal mode" candidate** — the task it unblocks: a long task that runs unattended until an
audited finish, without a person typing "continue".

**Cross-check (Law 7).** `omp` (`goals/runtime.ts`, `goals/tools/goal-tool.ts`,
`prompts/goals/*.md`, the continuation timer in `modes/interactive-mode.ts`): one persistent
objective per session, a token budget, automatic continuation turns after each idle turn, and a
`goal` tool whose `complete` op is the only way out. `trpc-agent-go`'s ralph loop
(`runner/ralph_loop.go`): a fixed iteration cap and an external verifier or a `<promise>` tag
decide completion. Our verifier loop (D50) already rejects a model's claim as proof; goal mode is
the other half — the objective outlives a turn, and the model must show evidence before it may
end it.

**Decided.**

- **A `goal` package**, a driver in Law 4's sense. `goal.Tracker` holds at most one goal:
  objective, status (`active`, `paused`, `budget-limited`, `complete`, `dropped`), optional token
  budget, tokens and seconds used. `goal.Tool` is the model's `goal` tool (`create`, `get`,
  `resume`, `complete`, `drop`; `get` is read tier, the rest write tier, as in `omp`).
  `goal.Runner` wraps any `agent.Runner`, like `VerifierRunner`.
- **Continuation is a runner, not a timer.** When an inner run ends `completed` and the goal is
  still active, the runner starts the next run itself with a continuation prompt, and the shell
  sees one run with one final result. `omp` schedules a hidden turn 800 ms after the agent goes
  idle, in each shell separately; a runner gives the TUI, print, JSON and RPC modes the same
  behaviour from one implementation. Typing during an automatic turn steers it as usual.
- **What stops it:** the goal leaving `active` (the model completed it, the budget ran out, the
  user paused or dropped it); an inner run that did not complete — Esc pauses the goal, as
  `omp` pauses on interruption, and an error or `--max-turns` leaves it active for the next
  message; plan mode; and **no progress**: an automatic turn with no tool calls, or with exactly
  the calls and results of the previous automatic turn, ends the loop — `omp`'s
  `goalContinuationActivity` rule. There is no iteration cap beyond these; `/goal budget` is the
  cap a user sets.
- **The model is told on every run.** While the goal is active, each input is prefixed with a
  `<goal_context>` block — objective, budget, and the audit rules: turn the objective into
  deliverables, find current evidence for each, a narrow check does not prove a broad claim,
  uncertain means not done, a low budget is not completion. Continuation turns get
  `<goal_continuation>` instead. Both are user messages, the same mechanism as plan mode's
  reminder (D43), so the goal survives compaction because every run restates it. The objective
  is XML-escaped so it cannot close the block it sits in. The wording follows `omp`'s prompts.
- **Accounting is `omp`'s:** input + output + cache-write tokens from each response's usage
  event, cache reads excluded as reused prefix; wall time per run. Crossing the budget flips the
  goal to `budget-limited`, and a wrap-up instruction is delivered once at the next round
  boundary through the prompt-hook seam (`omp` sends a hidden steer). This is why D58 changed: a
  failing user prompt hook used to discard every message of that round, including this one.
- **The goal lives in the session file** as an append-only `goal` entry, the last one wins,
  like `title` (D60). A resumed session's active goal comes back **paused** and the startup note
  says how to resume it (`omp`'s `onThreadResumed`); `/new` starts without one.
- **The user drives it with `/goal`:** `/goal <objective>` (or `set`) sets or replaces the goal
  and starts on it, `show`, `pause`, `resume`, `budget <tokens|off>` (`200k` accepted), `drop`.
  The status bar shows `goal active 12k/200k` while the goal is live. The model may also create a
  goal through the tool when the user asks for one.
- **Subagents do not inherit `goal`**: a child must not complete or drop its parent's objective.
  The exclusion list now lives in one place, `subagent.Inherits`, which also stopped the child's
  system prompt from describing `exit_plan_mode`, a tool the child never had.

**Not adopted.** `/guided-goal` (an interview that drafts the objective — the user can ask for the
same in plain words); the `replace` op and the `exiting` state; `goal.continuationModes` (a goal
continues in every mode; it exists only because someone asked for one); a status-bar setting;
injecting the todo list into the goal context (`todo` state is already in the history); `omp`'s
"all open work is blocked" check.

**Known limits.** A subagent's tokens are not counted against the goal (the same limit as D68);
the time of a run in which a paused goal was resumed by the tool is not counted; an automatic turn
that keeps calling different tools without finishing runs until the budget or Esc stops it.

**Reverse if** real goals stall where `omp`'s idle-timer design would not, or users want goals
that survive across sessions rather than inside one.

### D71 — Long-term memory: lessons saved on demand and a summary distilled from past sessions, injected at session start

**Decided:** 2026-11-24. **Status:** Accepted (user: "开始实现goal模式和memory"). **Admits the
M9 "long-term memory" candidate**. The task it unblocks: cross-session recall of a project's
conventions, decisions and pitfalls, without the user restating them.

**Cross-check (Law 7).** Four references, read at decision time:

- **`omp` `local` backend** (`memories/index.ts`, `memory-backend/local-backend.ts`, `tools/learn.ts`,
  `prompts/memories/*.md`, `docs/memory.md`):
  - `learn` appends to `learned.md`.
  - At startup a background pipeline extracts durable memory from each idle past session
    (phase 1), then consolidates it into `memory_summary.md`, `MEMORY.md` and skills (phase 2).
  - A 5000-token "Memory Guidance" block carrying both is injected per session.
  - The backend is off by default.
- **`trpc-agent-go` `memory/`:** id-addressed memories per app and user, `memory_add|update|delete|search|load`
  tools, and an optional LLM extractor after each run.
- **Norma `memory/`:** one markdown file per memory with a `MEMORY.md` index, `RecordMemory` and
  `RecallMemory`, and the index in the system prompt.
- **pigo:** markdown memories written with the normal file tools, full-text search, and a
  background "dream" consolidation.

All four keep a durable store, give the model a way to write to it, and put memory back in front
of the model with a warning that it may be stale; three of the four also distil memory
automatically. `omp`'s shape fits ours best: per project like our sessions, plain files rather
than a service, and a summary small enough to inject whole.

**Decided.**

- **A `memory` package.** `memory.Store` is one directory per project, `~/.omp/memory/<project>/`,
  named by `session.ProjectDir` like the session directory, so `C:\Code\oh-my-pi` is
  `--c--code-oh-my-pi--` exactly as in `omp`.
- **Lessons — the `learn` tool, write tier.** `omp`'s `learned.md` rules, unchanged:
  - one bullet per lesson, newest first, and an exact repeat moves to the top;
  - at most 100 lessons, 2000 characters each plus 400 of context;
  - control characters, `<`, `>` and backticks are stripped, and secrets are redacted;
  - hand-written lines outside the bullets survive every write.

  `learn` is available whenever memory is on. `omp` also requires `autolearn.enabled`, but a
  memory that cannot be added to on purpose is the surprise: Norma, `trpc-agent-go` and pigo all
  expose a write path.
- **Summary — the pipeline, in `omp`'s two phases.**
  - **Phase 1** reads each past session of this project that is not the running one, has been
    idle for 12 hours, is at most 30 days old and changed since it was last read. It sends a
    4000-token transcript (head 60%, tail 40%, tool calls and results clipped, thinking left out)
    and asks for JSON `{summary, memory}`.
  - **Phase 2** merges the newest 200 useful extracts, at most 32k tokens of input, into
    `memory_summary.md`.
  - **Storage:** extracts are JSON files under `sessions/<id>.json` instead of `omp`'s SQLite
    tables.
  - **Failure:** a failed session is retried three times, then waits until its file changes.
  - **Phase 2 runs only** when an extract's memory changed or the summary is missing. When
    nothing durable remains, the summary is deleted.
  - **Lock:** a lock file, stale after 15 minutes, keeps two processes from updating one project
    at once.
- **When it runs.** In the background when the TUI starts a saved session, and on
  `/memory sync`, which also reads sessions used in the last 12 hours. It does not run in `-p`,
  JSON or RPC runs (a one-shot process would cut it off or wait for it), in subagents, or under
  `--no-session`.
- **Sequential, at most 16 sessions per run, on the active model and thinking level**, like the
  compaction summarizer (D47); the rest is read next time. `omp` claims up to 64 sessions, 8 in
  parallel, on role models (`default` at low effort for phase 1, `smol` at medium for phase 2).
  We have one model role (D40), and 64 calls at once on the first startup is a burst a personal
  gateway should not see.
- **Injection.** One `<memory>` block is appended after the project context, as the last system
  block, which is `omp`'s position. It holds:
  - the rules: memory is advice; the repository, command output and the user win; verify before
    relying on it; say when memory changed the plan;
  - then the summary, then the lessons.

  Summary and lessons share a 5000-token budget, summary first, cut in the middle when over. The
  block is built once at startup, so `learn` and a background update change the next session's
  prompt and never the running one's prompt cache (`omp`'s snapshot rule). `<` and `>` are
  escaped so the block cannot be closed from inside.
- **Secrets.** `omp`'s redaction patterns (fixed provider prefixes, a keyword followed by a
  credential-shaped run, JWT shapes) run on every lesson, extract and summary, and again when
  lessons are read.
- **`/memory`** shows the project's directory, the lesson count, the last update and the injected
  block. `/memory sync` runs an update now; `/memory clear` deletes the project's memory, and the
  running session keeps the block it started with. With memory off, `/memory` says how to turn it
  on.
- **Config.** `memory.backend: local` in `~/.omp/config.yml`, off by default as in `omp`. Other
  backend names are reported and treated as off. The `memories.*` tuning keys are not read: the
  constants above are pinned by a test until someone needs to change one.

**Not adopted.**

- `MEMORY.md`, `rollout_summaries/`, `raw_memories.md` and generated skills: nothing reads them
  without `memory://`, an internal URL scheme we cut.
- `/memory stats`, `diagnose`, `queue` and `enqueue`.
- The hindsight, mnemopi and sharpshooter backends, the global scope, and managed skills from
  `learn`.
- Search tools (`recall`, `memory_search`): the whole memory fits in the prompt.
- Refreshing the running session's prompt after a startup update. `omp` does it while no message
  has been sent; our loop's options are fixed when the loop is built.

**Known limits.**

- Without `/memory sync`, the summary trails the work by at least 12 hours.
- Memory model calls appear neither in the trace nor in the usage counters.
- Two processes saving a lesson at the same instant can lose one; within one process writes are
  serialized, as in `omp`.
- A subagent can save lessons but does not see the memory block.

**Reverse if** injected memory misleads the model more than it helps (then inject the lessons
only), or a per-project scope proves too narrow (then add a global store, as `omp`'s mnemopi
does).

### D72 — The kernel keeps a run's state in one value; the assembly root builds the runner stack once

**Decided:** 2026-11-25. **Status:** Accepted.

**Problem.** The user stopped M8 on 2026-11-24 because the agent's implementation had not been
sorted out. Reading it confirmed that. A run's state — usage, turns, consecutive truncations,
whether automatic compaction was still on — was threaded through pointer parameters, so one turn
took eight arguments. The model stream and the request builder lived in `compact.go`, and the
tool pipeline was spread through `loop.go`. The `Runner` interface was defined four times
(`agent`, `tui`, the GUI start, `cmd/omp`). `cmd/omp/dispatch.go` had grown to 724 lines and
wrote the same four-layer wrapper chain out five times, once per mode.

**Decided.** The public API does not change; the code is cut by responsibility.

- **One value per run.** `Run` and `Compact` create a `run` holding the session, the emitter,
  the semaphore and the counters; every step is a method on it. The stop chain is one method,
  `keepGoing`, with the order written once: steering, notifications, stop hook, then
  `completed`. The token budget uses `llm.Usage.Total` instead of a private copy of it. One
  helper, `eventSeq`, turns a producer into the iterator and is the only place that closes the
  event channel.
- **Files follow the steps of a run:** `loop.go` (contracts, the run, the stop chain), `stream.go`
  (request and model stream, the overflow retry), `exec.go` (one tool call, collecting results
  in call order, the tool context), `compact.go` (compaction only).
- **One `Runner`.** `agent.Runner` sits beside `Loop`, `agent.RunnerFunc` adapts a function, and
  `tui.Runner` is an alias of it, so the exported name stays.
- **One runner stack.** `dispatch` builds an `app` once, and every mode gets its runner from
  `app.stack`: loop → plan reminder → verifier → goal → trace. The assembly root is split into
  `dispatch.go`, `assemble.go`, `provider.go`, `runner.go` and `interactive.go`; the largest is
  197 lines.
- ARCHITECTURE §4.4 now states who owns what and walks one run and one tool call step by step;
  §6 states the stack and the modes.

**Recorded, not changed.** Each needs a breaking change to an exported symbol, and the user
keeps exports unless asked otherwise:

- `Options.Hooks` are callouts, so by the rule above they belong in `RunDeps`.
- `RunDeps.NewID` is read by nothing; setting it does nothing.
- `agent.Done` is the scheduler's internal record, exported by accident; nothing outside the
  package uses it.

**Verified.** The 66 agent tests pass without a change to their logic, and the whole suite
passes under `-race`. Behaviour is unchanged.

**Revisit** when the user decides on the three API changes above.

### D73 — A resumed session replays its stored conversation; injected context is hidden by tag

**Decided:** 2026-11-25. **Status:** Accepted. **Executes M3 ⑨.**

**Problem.** `--continue`, `--resume` and `/resume` printed one line ("Resumed session with N
messages") and nothing else, so the user resumed a conversation they could not see. The todo strip
and the token counters started from zero as well, although STATUS claimed the strip was restored.

**Blueprint.** `omp` rebuilds the whole transcript on resume (`renderSessionContext` in
`modes/utils/ui-helpers.ts`): user turns, assistant text and thinking, tool calls paired with their
results, usage rows, in bounded chunks so input stays responsive.

**Decided.**

- **The reducer replays, the shell prints.** `presentation.State.Replay` turns stored messages into
  the same items live events produce: user turns, text, thinking, tool cards paired by id with
  their results. Projectors run on the stored tool inputs, so the todo strip comes back. Usage is
  summed and the context size is the last response's. A call with no stored result replays as
  cancelled. The TUI prints the replayed items through its normal commit path, after the banner at
  startup and after the marker on `/resume`; an approved plan is printed on replay as it is live.
- **Injected context is hidden by tag, and the tags come from the assembly root.** Plan mode, goal
  mode, background notifications and system-role prompt hooks put their text into user messages
  inside tagged blocks (`system-reminder`, `goal_context`, `goal_continuation`, `goal_budget_limit`,
  `background-task`, `hook`). The reducer stays feature-blind: it drops leading blocks whose tag is
  in `State.InjectedTags`, and `cmd/omp` supplies the list. A test builds the texts with the real
  producers (`plan.WithReminder`, the goal prompts, a real loop draining a notification) and checks
  that only the user's words replay, so a renamed tag fails the test instead of leaking. Unknown
  tags, longer names and unclosed blocks are the user's text and stay.
- **Everything is replayed.** Like `omp`, and unlike a "last N messages" cut, which would hide the
  part of the conversation the user came back for. The terminal scrollback holds it.

**Known limits.**

- Tool durations are not stored, so replayed cards show none.
- Compaction checkpoints are not shown; the full stored history is.
- A verifier re-prompt and a stop hook's message are plain user messages without a tag, so they
  replay as user turns, although live they appear as notices.
- The whole transcript is printed at once; `omp` chunks it. Revisit if a long session stalls input.

### D74 — Approvals show the change they would make; Ctrl+O opens the whole transcript

**Decided:** 2026-11-25. **Status:** Accepted. **Executes M3 ⑩. Amends D39** (the TUI enters the
alternate screen for one view).

**Problem.** An approval showed the tool's arguments as `key: value` text cut at 500 bytes, so
approving an edit meant reading `old_string` and `new_string` as two blobs; a `write` that would
replace a file gave no hint of what it replaced. Tool cards cut output at five lines and edit diffs
at eight with no way to see the rest. The TUI host refused a free-form `ask` and any choice list
longer than nine, so the model was told nobody could answer while the user sat at the terminal.

**Blueprint.** `omp` binds Ctrl+O (`app.tools.expand`) to expand tool output in the transcript. Its
approval prompt is built from per-tool details (`formatApprovalDetails`; `write` gives the path and
the content, cut) in a full and a compact form, and the host shows the compact one when the tool
call is already visible beside the dialog (`tools/approval.ts`). It does not diff a `write` against
the file it would replace; we do, because what the user is deciding is what gets lost.

**Decided.**

- **The approval request carries the call.** `agent.AskRequest` gains `Tool`, `Input` and `Detail`:
  the approval wrapper and `exit_plan_mode` fill the first two, and the wrapper moves its argument
  preview from `Message` to `Detail`. A shell that can render the call does; the RPC wire and the
  parked GUI put `Detail` back after the message, so the `ask` frame reads as before (D64 holds).
- **The dock shows the change.** An `edit` approval is a line diff of `old_string` against
  `new_string` (LCS over lines, two lines of context, runs of unchanged lines collapsed); a `write`
  approval reads the target file and diffs against it — BOM and CRLF are normalised so they do not
  show as changes — or lists the new file's lines; `bash` shows the command. The diff is computed
  once per request. Enter still never approves (D39).
- **Ctrl+O opens a full-screen transcript.** The live tail cannot scroll, and the terminal's
  scrollback already holds the committed cards, so expanding a card in place is impossible in an
  inline program. The viewer is a separate view on the alternate screen: every item, with each
  tool's whole input and output wrapped, and at the end the whole diff of a pending approval. It
  follows the end while a run streams; arrows, PgUp/PgDn, `g`/`G` scroll; Esc, `q` or Ctrl+O
  close. Bubble Tea writes printed lines straight to the terminal even in the alternate screen, so
  nothing is printed while the viewer is up, and printing resumes 80 ms after it closes, once the
  renderer has left the alternate screen (more than one frame at 60 fps); a print still in flight
  delays opening until it lands. Every cut card and dock ends with `… +N lines · ctrl+o shows all`.
- **Cards show what changed.** `edit` cards use the same line diff, `write` cards list the written
  lines; both stop at eight rows.
- **Every ask method works in the TUI.** A free-form question gets an answer box — a textarea, like
  the composer, because the text input places the cursor by rune count and Chinese text would put
  the IME window in the wrong column; Enter sends a non-empty answer, Shift+Enter breaks a line,
  Esc dismisses. A choice list of any length shows nine rows and scrolls; digits pick the first
  nine, and Enter picks only after an arrow key has highlighted an option, so a stray Enter cannot
  answer. Approvals keep digits, `y`, `n` and Esc only.

**Not adopted.** Expanding cards in place (impossible above the live tail); a diff payload on tool
results so a `write` card can show what it replaced (a contract change with one consumer today);
syntax highlighting (D67).

**Known limits.** A `write` card shows the new content, not a diff; the dock previews only regular
text files up to 256 KiB; the viewer shows at most 4,000 output lines per tool and says how many it
left out.

### D75 — Rendering fixes found by rendering real answers at three widths

**Decided:** 2026-11-25. **Status:** Accepted. **Executes M3 ⑪. Amends D67** (heading style and
spacing, table fitting, where a long word breaks).

**Method.** Two answers of the kind the model writes went through the TUI's own path (`textRows`)
at 100, 60 and 40 columns: one in Chinese with headings, nested lists, a table, a quote, a code
block and a link; one in English with a long Windows path, a long test name, a URL, a fenced block
inside a list, a task list and a commit hash. The status bar was rendered with the user's model
label from 120 down to 40 columns. No row was wider than the terminal; these were the defects.

**Fixed.**

- **A numbered heading read as a list item.** `### 1. 调用顺序` over `1. 追加用户消息` printed two
  lines that both began `1.`: a level-3 heading was plain bold, without its marker or any space
  around it. Every heading is now bold in the accent colour, keeps its `#` marks from level 3 on,
  and has a blank line before and after it whether or not the source has one. The marks, the
  colour and the space after follow `omp` (`packages/tui/src/components/markdown.ts`). A streamed
  answer is committed block by block, so each commit carries whether its last block was a
  heading; a test streams a document with no blank lines one character at a time and gets the same
  rows as a whole render.
- **Tables wrapped their short cells.** At 60 columns `默认值` took two rows and `0（不限制）` four
  while the long column had room. Every column started at its longest unbreakable run, which in
  Chinese is one character, and narrowing always took from the widest column. A column now starts
  at its whole text up to 16 cells and gives up width down to its longest word before it breaks
  words; each cell of width is taken where it adds the fewest rows.
- **A wrapped code line looked like two lines of code.** `r.finish(ReasonAbo` continued as
  `rted, err.Error())` at the left edge. A continuation row now starts with a dim `↳ `.
- **Long words broke at any character.** A path split as `…dispatch_queue_t` / `est.go:142`, and a
  URL left its last digit on a row of its own. A run wider than the row now breaks after its last
  `/`, `\`, `_`, `-`, `.`, `:`, `?`, `&`, `=`, `#`, `,` or `;` when that keeps at least half the
  row, and at the width otherwise.
- **The status bar cut the fields that change.** With `mify/volcengine_maas/deepseek-v4.1-flash`,
  100 columns cut the hint to `es...`, 80 lost the token counters and the hint, and 60 cut the mode
  to `acceptEdi...`. The label now shortens first (`volcengine_maas/deepseek-v4.1-flash`, then
  `deepseek-v4.1-flash`). When that is not enough, fields drop: `/help`, then the token counters,
  then the hint that a run or an approval is waiting. Then the label is cut to no fewer than eight
  cells, or left out. The mode and the context share go last, and down to the 20-column minimum
  nothing but the model label is cut mid-word.

**Not changed.** A line directly over `---` is a heading in CommonMark, so it renders as one. Blank
lines between other blocks still follow the source, and code is still one colour (D67).

**Known consequence.** Copying a wrapped code line from the scrollback copies the `↳`; before, it
copied a bare line break in the same place.

### D76 — A session picker in the prompt line, and the key/flow edges around it

**Decided:** 2026-11-25. **Status:** Accepted. **Executes M3 ⑦ and ⑫** (`/resume` had a list but
no picker; history, completion, queue and interrupt had rough edges).

**Method.** The blueprint's selector (`packages/tui/src/overlays/session-selector.ts`) was read
first: a search line over a session list, ↑↓ and page keys, Enter to resume, Esc to cancel, Tab to
switch between this folder and all projects, Delete to delete after a confirmation, relative dates,
a `current` marker and day groups. Our shell is an inline scrollback with a dock rather than an
overlay stack, so the picker lives in the dock and the prompt line itself becomes the filter —
the same place the command palette already takes its query from.

**Fixed.**

- **`/resume` printed a list and asked you to retype it.** With no argument it noticed up to eight
  sessions followed by `/resume <id|prefix> to switch`. It now opens a picker: the prompt line is
  the filter (case-insensitive substring of the title or the id, placeholder "Search saved
  sessions"), ↑↓ move one row and PgUp/PgDn a window of eight that follows the selection with `↑ n
  more` / `↓ n more`, Enter resumes the highlighted session through the same `Options.Resume` path
  as `/resume <id>`, and Esc or Ctrl+C close it without resuming and restore the composer's
  placeholder and text. Each row is the title (or the id when the session has none), the relative
  last-modified time ("just now", `2h ago`, `2d ago`, then a date), the message count, and
  `current` for the session in the prompt; picking the current session only says `Already in
  session <id>` and does not reload the file. `/resume <id|prefix>` is unchanged.
- **The command palette's selection could leave its window.** Six rows were drawn whatever was
  selected, so at a bare `/` the sixteenth command could be selected off-screen and Tab completed
  to something invisible. The rows now window around the selection like the picker and the
  approval list, with the counts at both ends. A changed query — typing, erasing or pasting — puts
  the selection back on the first match; before, it kept the old index, so Tab after narrowing
  could complete to a command that was no longer listed.
- **Esc was dead when idle and swallowed while browsing.** It now, in order: closes the picker,
  leaves history browsing with an empty composer, interrupts a run, or clears the draft. Ctrl+C
  keeps its order (interrupt, clear, exit), and inside the picker it closes the picker instead of
  exiting `omp` — the one place it departs from the blueprint, whose selector exits on Ctrl+C.
- **A long queue pushed the answer off the screen.** The dock shows the three oldest queued
  prompts and counts the rest. When an interrupt returns queued prompts to the composer, the
  history cursor is reset, so ↑ afterwards starts from the newest prompt instead of wherever
  browsing had stopped.

**Not built.** The blueprint's Tab scope switch to all projects (the session layer has no
cross-project list — `session.List` reads one project directory), Delete inside the picker
(`/delete <id>` stays the only path, and it is the one operation that does not belong in a
picker's Enter key), its ranked fuzzy search, day grouping and preview pane, and prompt history
persisted across processes (it keeps that in SQLite for its search).

**Known consequence.** The filter is a plain substring over title and id, not a ranking; with
sessions named after their first prompt this is enough for the list sizes `/resume` sees.

