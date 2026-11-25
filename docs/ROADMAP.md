# Roadmap — oh-my-pi

> Build order. Each milestone is a vertical slice that ends in something a user can
> actually do, verified by an acceptance test — not a layer completed in isolation.
>
> Requirements delivered: [REQUIREMENTS.md](REQUIREMENTS.md). Rules the slices obey:
> [ARCHITECTURE.md](ARCHITECTURE.md). How a slice is executed: [DEVELOPMENT.md](DEVELOPMENT.md).
> Why each mechanism was chosen: [DECISIONS.md](DECISIONS.md).

---

## Status at a glance

| Milestone | Goal | State |
|---|---|---|
| **M0** | Kernel and model layer that cannot desynchronize | ✅ Done |
| **M1** | Reads a repo and answers about it | ✅ Done (live acceptance passed 2026-11-12) |
| **M2** | **Public surface + edits + approval + plan mode** | ✅ ⓪–⑪, including the ⑩ Windows acceptance and the ⑨ live verifier run (2026-11-17) |
| **M3** | **TUI** — the first shell, on a shared reducer | ✅ ①②④⑤⑥⑦⑧ done; ③ the lane split is deferred by measurement; markdown is parsed by goldmark and laid out by our own renderer (D67). ✅ **Polish (user, 2026-11-24)**: ⑨–⑫ done (D73–D76) |
| **M4** | **Baseline agent** — the Claude Code / Codex feature set | ✅ **Done** — ①–⑧ shipped and the combined acceptance runs all seven slices in one session (real MCP process, real hook process, real background command, a subagent editing through a worktree), `cmd/omp/acceptance_test.go` |
| **M5** | Measures itself (trace, run summary, trajectory eval, baseline) | ✅ **Done** — trace + usage (D54); run summary and live progress (D68): tool and request time, truncations, compactions, peak context, a subagent's current tool and tokens; eval harness scripted-only (D66 — the blueprint has no live golden suite); **baseline published: 15/15 (100%), 19 turns**. Cost is out of the plan and the code (D68) |
| **M6** | Survives interruption and context exhaustion | ✅ compaction v1 (D47), persistence v1 (D49), retry classification (D62); both acceptance runs measured (D63) — 15 compactions across 40 turns, and a real process kill followed by `--continue`. What remains — a model catalog and the config-v2 retry knobs — has no consumer yet (D63) |
| **M7** | **SDK + headless** — the public contract, frozen | ✅ ② event schema frozen (D64), ③ `--mode json`, ④ `--mode rpc` with approvals, ⑤ host-executed tools (D65), ⑥ a worked embedding test over public packages only, ① export review done (two findings recorded in D65). ⑦ ACP stays optional |
| **M8** | **GUI** — the second shell on the same contract | ⏸ stopped by the user 2026-11-24: polish the TUI and consolidate the agent core first; a parked start sits in `gui/`, unwired |
| **M9** | Differentiators (each must name the task it unblocks) | ◐ goal mode ✅ (D70), long-term memory ✅ (D71); the other candidates wait for a named task |

**Order as executed:** argument validation → compaction v1 → persistence v1 →
verifier loop → streaming dispatch → M5 trace, usage and eval baseline → M2 ⑩ Windows
acceptance → rest of M3 → model windows (D55) → M4 → retry classification (D62) → M6 (D63) →
M7 (D64, D65) → M5 run summary (D68, cost removed from the plan) → `~/.omp/` (D69) → M9① goal
mode (D70) → M9② long-term memory (D71) → M8 started and stopped by the user → agent-core
consolidation (D72) → M3 polish ⑨–⑫ (D73–D76). **M3 is complete; M8 stays stopped by the user
(2026-11-24), so the next milestone is their call.** M5 ran ahead of M4: the baseline existed
before the feature set and stayed at 100% through it.

Milestones are ordered by dependency and by "what does a using developer need next", not by
package convenience. A milestone is done when its acceptance test passes on a real repository
with a real key, not when the code compiles.

**Two structural constraints drive this order:**

1. **The public surface moves before the tools (M2⓪).** `internal/X` → `X` is a one-way move;
   doing it with three packages and zero consumers costs an afternoon, doing it after forty
   tool files costs a week.
2. **The TUI comes before the bulk of features (M3), and the GUI last (M8).** The TUI is what
   makes the engine usable and what proves the shell contract; building twenty features against
   an unproven contract is how you end up rewriting them. The GUI is deliberately last because
   it validates the SDK as its first real external consumer instead of guessing at it.

**The ordering principle: earn the right to build each layer by measuring the layer below it.**
The one deliberate exception — compaction and persistence (D47, D49) landed before measurement so
the TUI could survive a real session — was paid back by publishing the M5 baseline before any M4
feature.

---

## M0 — Kernel and model layer ✅

**Goal:** a loop that cannot lose or invent a tool call, on top of a provider layer that
speaks three wire protocols through one neutral model.

**Delivered**

- `llm` (then `internal/ai`): neutral block/message/usage model, provider contract, streaming
  accumulator, thinking levels, stop reasons.
- `llm/anthropic`, `llm/openai` (then `internal/ai/*`): three protocol adapters
  (`anthropic-messages`, `openai-completions`, `openai-responses`), SDK types confined,
  server error messages surfaced.
- `agent` (then `internal/agent`): `Tool`/`ToolContext`/`Event`/`Hook`/`Host`/`Session` contracts,
  registry with registration-time wrapping, batch scheduler (parallel safe segment, serial
  else, results in call order; replaced by streaming dispatch, D51), loop with max turns, truncation gate, panic boundary,
  unknown-tool suggestions, empty-error substitution.
- Tests green under `-race`; 18 kernel tests, adapter suites for all three protocols.

**Decisions made here** (see [DECISIONS.md](DECISIONS.md)): official SDKs over hand-rolled
wire; no vendor-specific adapter packages; panic recovery scoped to `Execute`; truncation
retries capped at 3; undeclared tool = exclusive + exec tier.

**Lessons learned:** the SDK upgrade (anthropic v1→v2, openai v1→v3) was done before the
adapters had downstream consumers and cost almost nothing. Do irreversible-looking
upgrades while the test net is thickest.

---

## M1 — Reads a repo and answers about it ✅

**Goal:** `omp -p "how does the truncation gate work?"` in this repo makes the model
locate `agent/loop.go` itself using `glob`/`grep`/`read` and answer correctly.

**Acceptance:** the above transcript, produced against a live Anthropic endpoint, plus
`glob`/`grep`/`read` unit tests covering Windows paths and no external binaries required.
Second acceptance: one OpenAI-compatible gateway and one `responses` endpoint answer the
same question.

**Result (2026-11-12):** passed on `anthropic-messages` through the user's gateway — the model
found `agent/loop.go` with the tools and cited correct lines. The run exposed that the
Anthropic adapter had never sent `tools` on the wire; fixed with a wire test. The two OpenAI
protocols remain unverified live.

**Slices**

| # | Slice | Deliverable |
|---|---|---|
| ① | `cmd/omp` + print text mode + minimal config | ✅ done — arg parsing, exit codes, signal handling, `--api/--model/--base-url/--thinking/--max-tokens` |
| ①b | Config file v1 | ✅ done — `~/.omp/config.yml`, `providers`, `!command` key resolution, key never from env; keys aligned with `omp` where the meaning matches (D42) |
| ② | System prompt v1 | ✅ done — two blocks, static instructions generated from the registered tool set, then `<project-context>`; cache boundary between them |
| ③ | Tool infrastructure | ✅ done — `resolve` (workspace confinement, symlink-aware), `decode`, shared truncation markers, `FileState` |
| ④ | `read` | ✅ done — line ranges, directory listing, 2000 lines / 50 KB / 2000-byte line caps, continuation offset, scanner errors surfaced |
| ⑤ | `glob` | ✅ done — gitignore-aware walk, newest first, 5 s timeout marked not proof of absence, files only |
| ⑥ | `grep` | ✅ done — in-process parallel search, literal fast path, binary skip, three output modes, unreadable files reported |
| ⑦ | Tools wired into the loop | ✅ done — registered in `cmd/omp/dispatch.go` behind `permission.Wrap` |

**Reference:** `omp` `tools/read.ts`, `grep.ts`, `glob.ts`, `tool-result.ts`,
`tool-timeouts.ts`, `system-prompt.md` (264 lines), `project-prompt.md`; upstream `pi`
`core/tools/*.ts` for the minimal shapes (sources at `C:\Code\oh-my-pi-origin`,
`C:\Code\pi`).

**Deferrals:** `ls` as a separate tool (read covers directories in v1); image/PDF/notebook
reading; URL fetch (an M9 candidate).

---

## M2 — Public surface, edits, approval, plan mode

**Goal:** the engine becomes an addressable library (SDK surface moved out of `internal/`),
and can edit code, run commands, and **plan before acting** under a real approval policy.

**Acceptance:** in a scratch repo, `omp -p "make the failing test pass"` edits code, runs the
test, reads the failure, edits again, and reports success — using the **verifier loop**, and
never claiming success itself. Plus: `omp -p --approval plan` on a multi-file task produces a plan and
makes **zero** writes until approved; a denied command produces a tool error the model reads
and works around; a cancelled run leaves every tool call paired. All of it on Windows with no
external tools installed.

**Slices**

| # | Slice | Deliverable |
|---|---|---|
| ⓪ | **Public surface move** | ✅ done — D37 moved `ai`, `agent`, `permission`, `tools` to the top level, kept `tools` one package and named the binary `omp`; D48 later removed `internal/` entirely and renamed `ai` to `llm` |
| ① | Permission core | ✅ done — D36: rule lists, four modes, deny-first `Evaluate`, `Wrap` at registration |
| ② | `bash` | ✅ done — D33/D34: Git Bash or PowerShell, bounded output, tree kill, inspection-only downgrade |
| ③ | `write` | ✅ done — D35: atomic, read-before-write with staleness, CRLF/BOM kept, partial-read content refused |
| ④ | `edit` | ✅ done — D35: exact match, every ambiguous line listed, indentation and quote hints, CRLF/BOM kept |
| ⑤ | Context files | ✅ done — `AGENTS.md` / `CLAUDE.md` from cwd to repo root, one per depth (AGENTS.md wins), root first, inside `<repo-rules>` in `<project-context>`; user-level files, `@` imports and dedup deferred |
| ⑥ | Tool metadata | ✅ done — D44: every built-in declares `destructive` / `open-world` / `max-result-size`; the approval dock states the declared risks; workspace-wide `grep` / `glob` honour path deny rules |
| ⑦ | **Plan mode** | ✅ done — D43: read-only enforced in the policy; a reminder on each input in plan mode; `exit_plan_mode` presents the plan and switches to `acceptEdits` or `default` only on approval; the plan stays in the session as the call's input; the TUI prints the plan above the dock |
| ⑧ | **Streaming dispatch** | ✅ done — D51: a safe tool starts the moment its argument block closes, while later blocks still stream; a non-safe tool runs alone and blocks the queue behind it; results still released in call order; a call whose arguments are not a JSON object is refused, never run. `openai-completions` cannot close a block before `finish_reason`, so that transport degrades to end-of-message dispatch |
| ⑨ | **Verifier loop** | ✅ done — D50: `agent.Verifier` + `VerifierRunner` re-prompt with the check's failure until it passes or the attempt cap is hit; `--verify` / `--verify-attempts`; `stop_reason: verifier` on exhaustion. **Live acceptance passed 2026-11-17**: a temp repo with a failing `Add`, `omp -p --verify "go test ./..."` found the bug (`a - b`), edited `calc.go`, and the external check turned the suite green — the trajectory in the trace is bash → glob → read×3 → bash → edit → bash |
| ⑩ | Windows acceptance | ✅ offline — `tools/acceptance_test.go`: CRLF round-trip, Unicode paths through read/edit/write/glob/grep, no external binary, a shell always resolves; the code-page debt was measured and does not reproduce through a pipe. Live smoke passed 2026-11-17 (`omp -p` through the gateway, traced end to end, session persisted), and the multi-tool "make the failing test pass" half passed live with ⑨ the same day |
| ⑪ | Argument validation | ✅ done — D46: every call checked against its schema before it runs (required, types, enum, closed objects); numeric and boolean strings corrected; built-in schemas closed and tested against their argument structs |

**Plan mode mechanics (D30, D43):** approval of the plan is a `Host.Ask` with the plan as
evidence. While planning, every write and exec tool is denied by the policy, so a buggy tool
cannot escape the mode. v1 keeps the plan in the session as the `exit_plan_mode` input and in
the scrollback. Compaction (M6) and subagents (M4②) shipped without needing a separate plan
object; one arrives when something has to read the plan outside the conversation.

**Reference:** `omp` `tools/approval.ts:205-320`, `extensions/wrapper.ts:243-425`,
`tools/bash.ts:185-371`, `write.ts`, `edit/`, `docs/context-files.md`, `plan/`; `Norma`
`permission/permission.go`, `plan/plan.go`; `trpc-agent-go` `runner/ralph_loop.go`;
upstream `pi` `powershell.ts`.

**Settled here:** the default approval mode (D36), the bash timeout (D34) and the Windows shell
(D33).

---

## M3 — TUI: the first shell

**Goal:** `omp` is usable interactively at the terminal, on a shared **reducer** that a future
GUI will consume unchanged.

**Acceptance:** a recorded session in which the user streams a long answer, watches a tool run
with live progress, interrupts with Esc (partial answer retained), answers an approval without
losing the draft, reviews a diff card, accepts a plan, then resumes the session. Plus a
benchmark: **10k-line transcript at 200 events/s with no dropped input and no input latency
regression on Windows Terminal.**

**Slices**

| # | Slice | Deliverable |
|---|---|---|
| ① | Kernel interaction contracts | ✅ follow-up queue in the shell; **steering in the kernel** — `RunDeps.Steer` is drained at every round boundary, each message joins the running session and the turn continues instead of completing. The shell queues and delivers, the kernel decides when to take it |
| ② | **Reducer package** | ✅ v1 — `presentation.State`: items with stable IDs and kinds, tool pairing, usage, context size, stop reason, duplicate and stale rejection, JSON snapshot. `Apply → []Patch` cut until an out-of-process consumer (D39) |
| ③ | Event lanes | ◐ the shell drains all buffered events per message, coalescing deltas. The reducer benchmark ran 2026-11-17 and clears the budget by four orders of magnitude, so a separate lossy lane stays unbuilt until a real terminal shows a need |
| ④ | TUI renderer | ✅ v1 — Bubble Tea v2, inline committed scrollback + live tail, ordered single-flight printing; tool cards, edit diffs, collapsed thinking, markdown from a goldmark AST — fitted tables, CJK-aware wrapping, finished blocks committed while streaming (D67) — status bar with mode / context / tokens |
| ⑤ | Approval dock + `ask` tool | ✅ dock — confirm and select, digits / y / n / Esc, never Enter, draft kept, session approvals (D41), Shift+Tab mode cycling. `ask` tool done: free-form or choice, fails closed without a host |
| ⑥ | Slash commands + palette | ✅ `/help`, `/clear`, `/new`, `/model` (show and switch), `/compact`, `/effort`, `/plan`, `/resume`, `/exit`, `/quit`, prompt history, and a completion palette: a leading `/` lists matching commands, Up/Down select, Tab completes |
| ⑦ | Session picker | ✅ D76 — an arrow-key picker over `session.List`, opened by `/resume`; D73 makes the switch replay the conversation |
| ⑧ | First-run wizard | ✅ interactive: a first run with no model asks provider and model, writes the `~/.omp/config.yml` skeleton, and **never asks for or writes a key** (the placeholder is replaced by the user with a literal or `!command`). Non-interactive runs print the same guide as text |
| ⑨ | Resume shows the history | ✅ D73 — `--continue`, `--resume` and `/resume` replay the stored conversation through the reducer: user turns without the engine's injected context, answers, thinking, tool cards with their results, an approved plan, the todo strip and the token counters |
| ⑩ | Tool cards and approvals | ✅ D74 — an approval shows the line diff it would apply (an overwrite is diffed against the file on disk); Ctrl+O opens a full-screen transcript with every tool's whole output and the whole pending diff; cut cards say so; edit and write cards show the change; free-form questions and choice lists of any length work in the TUI |
| ⑪ | Rendering | ✅ D75 — found by rendering real answers at three widths: headings are set apart and keep `###` from level 3, so a numbered heading no longer reads as a list item; tables keep short cells whole and wrap the long column; a wrapped code line is marked `↳`; a URL or path breaks after a separator; the status bar shortens the model name and drops the token counters before it cuts the mode or the context |
| ⑫ | Keys and flow | ✅ D76 — `/resume` opens a picker whose filter is the prompt line (title or id, ↑↓ and PgUp/PgDn, Enter resumes, Esc closes, the current session marked and never reloaded); the command palette's window follows its selection and resets on a new query; Esc closes the picker, leaves history, interrupts a run, or clears the draft; the dock caps a queue at three rows and the history cursor resets when an interrupt returns queued prompts |

**Architecture rule:** the TUI consumes Go values in-process, so it costs no serialization —
**but it uses the same event types the GUI will serialize.** One schema, two transports. The
TUI therefore does not validate the wire format; the frame-level tests added in M7 do (D64).

**Reference:** the shell's information architecture is fixed by Architecture §4.6 (append-only
transcript + live dock + status bar; the reducer is the only presentation state). The two-lane
batching rule stays unbuilt: the reducer benchmark did not ask for it.

**Settled:** the TUI library — D39 chose Bubble Tea v2, and the 2026-11-17 reducer benchmark
cleared the 200 events/s budget by four orders of magnitude.

---

## M4 — Baseline agent: the common feature set

**Goal:** the capability set a developer expects from Claude Code or Codex is present and
usable, not aspirational (D30).

**Acceptance:** a task that uses todos, a subagent, a skill, an MCP tool, a hook and a
background command in one session, each visibly working in the TUI.

**Slices**

| # | Slice | Deliverable |
|---|---|---|
| ① | Todo tracker | ✅ — `todo` tool (whole list per call, at most one in_progress, rendered as a checklist), state lives in the tool call so a resumed session replays the strip, reducer extracts it into `State.Todos`, TUI shows one plan row above the composer |
| ② | Subagents | ✅ v1 — `subagent.NewTaskTool`: a fresh loop over a child registry that **does not contain `task`** (depth guard by construction, not by erroring), no steering and no compactor in the child, result = final text plus turns and token counters, progress event while it runs |
| ③ | Skills | ✅ — `SKILL.md` discovery (project before user, broken ones skipped, CRLF safe), one prompt line per skill, body returned by the `skill` tool on demand. Not pinned through compaction: `skill` is in the reproducible set, so an elided body becomes a reload hint (D56) |
| ④ | MCP | ✅ stdio (D57), streamable HTTP and SSE by `url` (D66) — `mcpServers` config, official SDK, sanitised and capped `mcp__<server>_<tool>` names, write tier + open-world by default, registered through the same approval wrapper, a failed server reported but not fatal. The `__` alias is not built |
| ⑤ | Command hooks | ✅ (D58) — `hooks.preTool/postTool/stop/prompt` subprocesses, one JSON object each way, 10 s default timeout; the kernel now actually calls all four hook families (pre runs before approval, a failing observer never blocks, `stop` can keep the turn alive, `prompt` injects before every request). `system` text is wrapped so it cannot pass as a user message |
| ⑥ | Background commands | ✅ (D59) — `background` tool (`start` / `wait` / `list`), output to `~/.omp/background/<id>.log`, pointer-only notifications drained at every round boundary (a finished task keeps the turn alive), exec tier |
| ⑦ | Session operations | ✅ (D60) — `/resume` lists and switches, `/rename` appends a title entry (last wins, never a request), `/export` writes markdown, `/delete` removes another `.jsonl` through the same prefix resolution and refuses the current one. A tree picker and branching stay cut (D25) |
| ⑧ | Worktree isolation | ✅ (D61) — `task` takes `isolated`: the child runs in a throwaway `git worktree` under `%TEMP%\omp-worktrees`, its changes are committed there and applied to the parent working tree as a patch, and the worktree is always removed. A patch that does not apply leaves the parent files untouched and keeps the `.patch` beside the worktrees. The child registry also loses `exit_plan_mode` and `background`, and hooks narrow to pre/post-tool |

**Not in M4:** `omp`'s goal mode and long-term memory came later as M9 slices; parallel
background agents are not built. The gap ledger lists the rest of the `omp` surface we do not
have.

**Why this is a milestone and not a backlog:** these features interact — a subagent inherits
skills and hooks, background notifications must not break the reducer, plan mode must survive a
subagent boundary. Built one at a time and never tested together, each works alone and the
combination does not.

---

## M5 — Measures itself

**Goal:** every run leaves a trace a human can audit and a number a regression can be measured
against.

**Acceptance:** every run emits a JSONL trace with all events, per-turn usage and a run summary
(the cost line was removed from the plan by D68, 2026-11-23);
an eval runner executes ~20 golden tasks with a scripted provider and prints pass/fail, turns
and tokens, scoring **both** the final answer and the tool trajectory; the suite publishes a
baseline success rate and exits non-zero on regression.

**Slices:** usage accounting ✅ + trace log ✅ (D54, `--trace`) → eval harness ✅ (answer +
trajectory scoring, scripted provider) → golden tasks backfilled ✅ (15 tasks) → **baseline
published ✅** (100% pass, 19 turns; no per-turn latency budget is stated yet) → **run
summary and live progress ✅** (D68: tool and request time, truncations, compactions, peak
context and its share of the window; a running subagent card shows the child's current tool and
tokens). Cost per task is out of the plan (D68). The suite grows with every milestone (R8.4).

**Rationale:** agent bugs are non-deterministic and expensive to find by hand. The TUI just
added a large surface; measuring now catches reducer and interaction regressions before the
SDK freezes them into a contract.

---

## M6 — Survives interruption and context exhaustion

**Goal:** a long session compacts and keeps working; a killed process resumes with every tool
call still paired.

**Acceptance:** a task exceeding the window by 2× completes across two compaction boundaries;
kill -9 during a tool call, then `--continue` yields a valid request and the task completes;
the session file round-trips byte-exactly and migrates from the previous version.

**Pulled forward (2026-11-16):** compaction v1 (elide first) and the session log v1 came right after
argument validation, ahead of the rest of M2, M3, M4 and M5, and carried this milestone's
acceptance tests with them. Both are done (D47, D49).

**Slices:** linear append-only session log (version header, `parentId` present but linear) ✅
D49 → load recovery (torn tail, dangling call → `outcome unknown`, orphan result dropped) ✅
D49 → continuation primitives (`--continue`, `--resume`, `--no-session`) ✅ D49 → retry
classification ✅ D62 (transient/fatal/overflow, per-attempt backoff, `Retry-After`, one visible
line; a stream that already delivered text is never replayed) → model metadata + config v2 (the
retry knobs wait for a second consumer) → **elide-first compaction** (reproducible results → re-read
pointers; non-reproducible → spill + pointer; summarize only as a last resort; cache-safe
prefix) ✅ v1 D47.

**Acceptance state:** round-trip byte-exact ✅ (tested); killed mid-tool-call then `--continue`
yields a valid request ✅ **measured with a real process kill** (`cmd/omp/kill_test.go`), and the
2× compaction run ✅ **measured across fifteen boundaries** (`cmd/omp/compaction_acceptance_test.go`,
D63) — the window comes from D55, not from live model metadata.

**Deliberately cut** (D25): branching, fork, `/tree`, branch summaries, blob store until the
first image.

**Not yet exercised:** the third acceptance clause, "migrates from the previous version", has no
previous version to migrate from — `session.FileVersion` is 1 and has never been raised. The
loader already refuses a newer file with a readable error; a migration path is written with the
first format change, not before it.

---

## M7 — SDK + headless: the public contract, frozen

**Goal:** another program — in Go or any other language — can drive `omp` as a library or as a
subprocess, and the contract it depends on stops moving.

**Acceptance:** a Go program imports the public packages, builds an agent with its own tools,
its own `Host` and its own compactor, and runs a multi-turn session. A script spawns
`omp --mode rpc`, sends a prompt, receives every event as one NDJSON frame with a version
field, answers an approval, and reads a final result. A third program replays a trace with no
engine attached.

**Slices**

| # | Slice | Deliverable |
|---|---|---|
| ① | Contract review | ✅ D65 — every exported symbol checked for a consumer; `Registry.NotFound` and `Registry.Schemas` recorded as intent, not proven use |
| ② | Event schema freeze | ✅ D64 — every frame is `{"v":1,…}` with its own snake_case wire types, so the Go structs are not the contract; frame-level tests independent of the TUI |
| ③ | `--mode json` | ✅ D64 — one-shot NDJSON of the whole run; the result frame carries a state snapshot |
| ④ | `--mode rpc` | ✅ D64 — bidirectional NDJSON: `prompt` / `answer` / `quit` in, event and `ask` frames out, approval round-trip; no separate state query |
| ⑤ | External tool execution | ✅ D65 — `--external-tools` declares them; a `tool_call` frame goes out, the host's `tool_result` command comes back and the loop resumes |
| ⑥ | Embedding guide | ✅ D65 — `cmd/omp/embed_test.go`: a custom tool, a custom compactor and a host over public packages only |
| ⑦ | Optional ACP | ⬜ only if an editor integration is wanted; the protocol already exists |

**Not yet exercised:** the third acceptance clause, a program that replays a recorded trace with
no engine attached, has no test. The reducer throughput test replays synthetic events, not a
trace file; a replay test is written with the first trace reader outside `trace` itself.

**Why the SDK gets its own milestone:** an SDK is a compatibility promise. It needs a review of
what is exported, a frozen event schema, and a worked example a stranger can follow. Shipped as
a by-product, it becomes a public API nobody can change.

---

## M8 — GUI: the second shell on the same contract

**Goal:** a desktop application that consumes the SDK as **its first real external consumer**,
proving the contract instead of guessing at it.

**Acceptance:** the GUI renders a live session from the reducer (the same state the TUI
renders), answers approvals, shows diffs with accept/reject, and drives a session to
completion; it imports no shell package.

**Slices:** GUI technology decision (O10) → shell skeleton consuming the reducer → streaming
transcript with virtualization → approval and diff review → session picker → packaging and
installer → platform smoke (Windows first).

**Why last:** the GUI is the best test of the SDK. Built before the contract is frozen, it
shapes the contract by accident; built after, it finds the gaps a real consumer actually has.

**Open question:** GUI framework (O10) — Wails vs Fyne vs Gio vs local HTTP + system webview.
Criteria: Windows quality, streaming throughput, binary size, testability, iteration speed.

---

## M9 — Differentiators

**Rule for admission:** a candidate must name **the task it unblocks**. "`omp` has it" is not
a reason. Admitted candidates move into the slice table below; the user admitted goal mode and
long-term memory on 2026-11-23.

| # | Slice | Task it unblocks | State |
|---|---|---|---|
| ① | Goal mode | Run a long task unattended until an audited finish, without a person typing "continue" | ✅ D70 — `goal` tool, `/goal`, an outer runner that continues until the model completes the goal with evidence, a token budget, persisted in the session, paused on Esc and on resume |
| ② | Long-term memory | Cross-session recall of project conventions | ✅ D71 — `learn` saves lessons; a background pipeline distils idle past sessions into a summary; both are injected at session start as advice the repository overrides; `/memory`, `/memory sync`, `/memory clear`; off unless `memory.backend: local` |

**Remaining candidates**

| Candidate | Task it unblocks | Cost |
|---|---|---|
| Commit / review commands | "commit this with a sensible message" | Low |
| LSP: post-write diagnostics | See your own syntax errors without running anything | Medium-high |
| URL read + `web_search` | Questions whose answer is outside the repo | Medium |
| Model roles + fallback chains | Cheap models for cheap work; resilience | Medium |
| AST-aware search/edit | Precise structural rewrites across many files | High |
| DAP debugging | Step-level introspection when a test will not reproduce | High |

**Not planned at any horizon:** computer use, image generation, TTS, collaboration server,
in-process plugin scripting, OS sandboxing.

---

## Cross-milestone discipline

1. **Offline tests first, live smoke at the end.** Each milestone closes with a real-key
   run; smoke tests read the key from `~/.omp/config.yml`, never the environment.
2. **Every tool call paired** — normal, cancelled, truncated, crashed.
3. **Compaction changes the projection, never storage.**
4. **Notifications carry pointers, not content.**
5. **Subagents do not share parent history and do not call back into the assembly root.**
6. **Argument rewriting happens only in the pre-tool hook; approval sees final arguments.**
7. **Declare safety attributes before writing a tool** — read-only, concurrency-safe,
   approval tier. Undeclared means exclusive and approval-required.
8. **Fakes must express failure branches**, or the recovery path is untested.
9. **A new hook or event needs a named consumer today.**
10. **Platform code = one interface + per-GOOS file + explicit error.**
11. **Every deferral carries its revival trigger** and lands in the gap ledger below.

## Gap ledger

Status key: ✅ done · ◐ partly done · 🔜 in progress · ⏸ deferred with a trigger · ⬜ not built,
no decision yet · ✗ cut (re-propose with a named task)

| Capability | `omp` evidence | State |
|---|---|---|
| Three wire protocols | `packages/ai` | ✅ M0 |
| Compatible vendors via `baseUrl` | providers | ✅ M0 |
| Bedrock / Vertex / Azure | providers | ⏸ revive when a user needs them |
| Model metadata (window, max tokens) | catalog | ◐ window from the built-in table (D55); the catalog was left open by D63 |
| Key resolution chain | `ai/auth/cascade.ts` | ✅ config file only — deliberate divergence |
| Thinking selector `:level`, `defaultThinkingLevel` | `model-resolver.ts`, settings | ✅ D42 — no fuzzy matching until the catalog |
| Approval mode in config | `tools.approvalMode` | ✅ `permissions.defaultMode`, Claude Code vocabulary — deliberate divergence (D36, D42) |
| OAuth / credential store | `ai/auth*` | ⏸ ask the user first (D4) |
| Retry classification / empty completion | `turn-recovery.ts` | ◐ retry classification ✅ D62; an empty completion is not retried |
| Tool argument validation and repair | `ai/src/utils/validation.ts` | ✅ D46 — a subset; unknown keys rejected where `omp` drops them, a deliberate divergence |
| Elide-first compaction | `pruning.ts`, `trpc-agent-go` `context_compact.go` | ✅ D47 v1 — elide first, summarize last, projection only; thresholds come from config until the model catalog |
| Fallback chains / credential rotation | `retry-fallback-chains.ts` | ⏸ M9, with model roles |
| Steering / follow-up queues | `agent.ts:1387`, `agent-loop.ts:1744` | ✅ M3① — queue in the shell, steering in the kernel (`RunDeps.Steer`) |
| Streaming tool dispatch | `agent-loop.ts:3770` | ✅ D51 — M2 ⑧: starts on the argument block's stop, call-ordered results |
| Verifier loop | `trpc-agent-go` `runner/ralph_loop.go` | ✅ D50 — M2 ⑨: external check decides; live acceptance passed 2026-11-17 |
| Trace + run summary | `telemetry.ts` (OTel), `run-collector.ts` | ✅ M5 — our own NDJSON, not OTel (D54); the summary folds the run (D68) |
| Cost accounting | `progress-hud.ts`, catalog pricing | ⏸ removed from the plan and the code (D68) — revive when the user asks |
| Subagent live progress | `task/executor.ts` `AgentProgress` | ✅ D68 — current tool and tokens; recent tools, recent output and cost not built |
| Eval / benchmark harness | `src/eval/` | ✅ M5 — scripted only (D66), 15 tasks, baseline 100% |
| Session **tree**, fork, `/tree`, `/branch` | `session-entries.ts`, `tree.md` | ✗ **cut** — linear log only (D25) |
| Content-addressed blob store | `blob-store.ts` | ⏸ ship with the first image |
| Compaction method chain (snapcompact, handoff, shake) | `CMP/` | ✗ cut — elide first, summarize last (D26), shipped as D47 |
| Speculative execution | `speculative-execution.ts` | ✗ cut — no measured latency win |
| TTSR injection | `docs/ttsr-injection-lifecycle.md` | ✗ cut — re-propose with a task |
| Internal URL schemes (`xd://`) | `internal-urls/` | ✗ cut — re-propose with a task |
| Plan mode | `plan-mode/`, `tools/plan-mode-guard.ts` | ✅ M2⑦ (D43) — revived by D30 |
| Todo tracker | `tools/todo.ts` | ✅ M4① |
| Goal mode: a persistent objective with a token budget, automatic continuation and an audited completion (`/goal`, `goal` tool) | `goals/`, `prompts/goals/` | ✅ M9① (D70) — `/guided-goal` and the per-mode continuation setting not built |
| Parallel and background agents (`vibe_spawn` / `send` / `wait` / `list` / `kill`) | `tools/vibe.ts` | ⬜ not built; `task` runs one child at a time and waits for it |
| Long-term memory: backends, consolidation, `/memory`, `learn`, `recall` / `reflect` / `retain`, `memory://` | `memories/`, `memory-backend/`, `docs/memory.md` | ◐ M9② (D71) — the `local` backend: `learn`, the two-phase pipeline, injection, `/memory`. Not built: the other backends, `recall` / `reflect` / `retain`, `memory://`, `MEMORY.md` and generated skills |
| Checkpoint and rewind | `tools/checkpoint.ts` | ⬜ not built |
| GitHub tools: PR checkout and diff, run watch, search | `tools/gh*.ts` | ⬜ not built; `bash` can call `gh` |
| Code-execution REPL (`eval`) | `tools/eval.ts` | ⬜ not built; `bash` runs scripts |
| Rich reads: PDF, SQLite, archives, images, notebooks | `tools/read-*.ts` | ⏸ M1 deferral — with the first image |
| Web fetch and search, LSP, AST search and edit, debugger (DAP) | `web/`, `exa/`, `lsp/`, `tools/ast-*.ts`, `dap/` | ⬜ M9 candidates |
| `think`, `wait`, `yield`, `security_scan`; `/loop`, `/handoff`, `/context`, `/usage`, `/retry` and other slash commands | `tools/`, `slash-commands/` | ⬜ not built; each needs a named task |
| SSH, browser automation | `ssh/`, `tools/browser.ts` | ⬜ not built |
| Plugin marketplace, in-process extensions | `slash-commands/builtin-marketplace.ts`, `extensibility/` | ✗ non-goal (REQUIREMENTS §2.2) |
| Session sharing / HTML export | session ops | ✗ cut |
| Computer use, TTS, image gen, collab server | `docs/computer-use.md` etc. | ✗ cut |
