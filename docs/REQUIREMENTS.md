# Requirements — oh-my-pi

> The "what and why" document. Every requirement below has an ID and at least one
> milestone in [ROADMAP.md](ROADMAP.md) that delivers it. If a requirement has no
> milestone, it is not a requirement yet.
>
> Companion documents: [ARCHITECTURE.md](ARCHITECTURE.md) (how it is built),
> [ROADMAP.md](ROADMAP.md) (in what order), [DECISIONS.md](DECISIONS.md) (why this way).

---

## 1. The core problem

A coding agent is a program that runs a language model in a loop against a real
repository: it reads code, edits it, runs things, reads the results, and repeats
until the task is done. The hard parts are not the model call. They are:

1. **The loop never desynchronizes.** Every tool call the model emits must come back
   with exactly one result, in call order, even when the user cancels, the stream
   dies, or the output is truncated. A single unpaired call corrupts the next request
   and the model starts hallucinating around the hole.
2. **Context is finite and the work is not.** A real task exceeds any context window.
   The session must survive compaction, and compaction must never silently destroy
   what the model needs to continue.
3. **The work must survive the process.** Agents get interrupted, laptops sleep,
   terminals close. A session that cannot be resumed is a demo, not a tool.
4. **Tool output is hostile input.** Files, command output and web content reach the
   model as text. Tool definitions, result shapes and truncation policy are the
   product surface, not plumbing.
5. **Every action has side effects.** Editing files and running commands is the point,
   and also the risk. Safety must be a first-class mechanism with a fail-closed default.

oh-my-pi exists to build all five properly, in Go, as one engine behind three shells.

## 2. Product definition

**A high-performance general-purpose agent in Go, with a TUI, a GUI, and an SDK — built to
serve as the orchestration substrate for other agent applications.**

The engine is one binary and a set of public Go packages. The shells are interchangeable
clients of one contract: the TUI ships first, the GUI follows on the same contract, and the
SDK is what both of them — and any third party — is built on.

`omp` and the common coding agents (Claude Code, Codex) define the **baseline feature set** we
must match; mechanisms are cross-checked against at least two implementations before adoption
([ARCHITECTURE.md](ARCHITECTURE.md) §2, Law 7). Matching a feature is not the same as copying
its implementation.

| Dimension | Requirement |
|---|---|
| Distribution | One static binary per OS/arch, no runtime, no Node, no Python |
| Primary platform | Windows 11 first-class; macOS and Linux by the same code with per-GOOS files where the OS forces it |
| Shells | TUI (first), GUI (M8), headless RPC/SDK; one-shot `-p` for scripting |
| Embedding | Public Go packages for in-process use; NDJSON frames over stdio for other languages |
| Performance | 10k-line transcript at 200 events/s with no dropped input (TUI); stated latency budget per turn |
| General purpose | The kernel is feature-blind; the coding toolset is one bundle among others |
| Orchestration | Subagents and parallel work as designed features, not experiments |
| Concurrency | Parallel tool calls when declared safe; deterministic result order |
| Providers | Anthropic Messages and OpenAI (Completions + Responses) wire protocols; every OpenAI-compatible vendor through `baseUrl` |
| Extensibility | MCP servers, skills, command hooks, context files |

### 2.1 Baseline capability set

Required, not aspirational — the features a developer expects from Claude Code or Codex
(D30): plan mode, a todo/plan tracker, subagents, slash commands, context files, MCP, hooks,
skills, permission modes with per-tool policy, diff review, session resume, compaction,
mid-run steering, background commands.

### 2.2 Non-goals

Explicitly **not** built. Each has a rationale in [DECISIONS.md](DECISIONS.md); the
common thread is that they are large, platform-specific, or only meaningful to a
hosted multi-tenant product.

- OS-level sandboxing (seccomp, landlock, containers, gVisor). Safety comes from
  approvals plus git worktree isolation — the same posture `omp` takes.
- A hosted service, web UI, multi-tenant control plane, or user account system.
- A plugin marketplace, or in-process plugin scripting (TypeScript extensions are
  not portable to Go; hooks become subprocess protocols instead).
- An embedded editor or IDE replacement. `omp` drives the terminal and the repo.
- A model catalog of 77 providers at launch. Two wire protocols cover the ecosystem.

## 3. Functional requirements

### R1 — The loop (M0, M2, M9)

- R1.1 A prompt runs to completion: model → tool calls → tool results → model, until the
  model stops without tool calls, or a stop condition fires.
- R1.2 **Pairing invariant.** Exactly one result per tool call, in call order, on every
  exit path: normal, cancelled, truncated, timed out, panicked, unknown tool, denied.
  Synthetic results carry a marker so persistence can distinguish them.
- R1.3 A tool panic becomes an error result for that call; the batch and the process
  continue. Our own bugs still fail fast.
- R1.4 A tool call whose arguments were cut by the output limit is never executed: it gets an
  explanatory error result and the loop asks the model to split the work. Calls that were
  complete before the cut have already been dispatched and are paired normally (D51).
- R1.5 Unknown tool names return a suggestion list, case-insensitive match first.
- R1.6 Terminal stop reasons are distinct and reported: `completed`, `max_turns`,
  `token_budget`, `stop_hook`, `aborted`, `verifier`, `error`. `stop_hook` is reserved: a stop
  hook can only keep a turn going today (D58).
- R1.7 The loop is provider-agnostic and knows nothing about permissions, todo lists,
  compaction policy, goals, or subagents. Features attach through declared seams.
- R1.8 The model stream is never cut mid-stream by a tool deadline: a tool's timeout is a
  context derived inside that call, and only cancellation of the turn reaches the stream.
- R1.9 An objective can outlive a turn (goal mode, D70): while a goal is active the session
  continues on its own until the model completes it with evidence, its token budget runs out,
  the user pauses or drops it, or an automatic turn makes no progress. The model's own "done"
  ends it only through the `goal` tool, after an audit the prompt requires.

### R2 — Model layer (M0, M1, M6)

- R2.1 One neutral message model that survives persistence and replay across providers;
  no SDK type leaves its adapter package.
- R2.2 Streaming for all three protocols, normalized to one event vocabulary: block
  start → deltas → block stop, with a contiguous block index.
- R2.3 Tool calls, thinking/reasoning deltas and signatures, and usage buckets
  (`input`, `output`, `cache_read`, `cache_write`) are first-class.
- R2.4 Thinking level maps onto each provider's capability without silent clamping.
- R2.5 Credentials come **only** from `~/.omp/config.yml` — a literal value or `!command`
  resolved by running the program directly. Never from environment variables, never
  from a flag. A key command failure is a hard error with the command's stderr attached.
- R2.6 Provider selection: `--model <provider>/<model>` resolves against `providers:` in
  the config file; a bare model id uses the default protocol. `--api` and `--base-url`
  override. A trailing `:<level>` sets the thinking level (D42).
- R2.7 Malformed or unknown fields in the config file are skipped, never fatal — an
  `omp` `models.yml` or `config.yml` may be pasted in whole. Unsupported `api` values for the
  *selected* provider are fatal, and so is a malformed `permissions` section, which would
  otherwise fail open (D42).
- R2.8 Transient failures retry with classification (transient / overflow / fatal),
  honoring `retry-after`; overflow routes to compaction, not to retry.

### R3 — Tool surface (M1, M2, M3)

- R3.1 A tool declares: name, description, JSON schema, execute. Optional capability
  interfaces declare scheduling safety and approval tier. **Not implementing an
  optional interface means the most conservative behavior** — exclusive scheduling,
  exec-tier approval.
- R3.1a Tool metadata distinguishes dimensions our tiers alone cannot express:
  `destructive` (irreversible side effects), `open-world` (reaches the network or the
  outside world), and `max-result-size`. A network fetch is neither a local read nor a
  local write, and the approval UI must be able to say so.
- R3.2 Arguments are checked once, in the loop after the tool is found and before `Execute`
  (D46), then decoded at the tool boundary; errors become that call's result and are phrased
  so the model can correct itself.
- R3.3 Results are structured (`content` blocks + `is_error`), never bare strings, and
  an error result always carries non-empty text.
- R3.4 Large output is bounded by each tool (head and tail where the end matters) and capped
  centrally at the declared `max-result-size`; per-tool limits are declared, not improvised
  (D44). Spilling the full body behind a pointer waits for a consumer that reads pointers
  back (D66).
- R3.5 Path arguments resolve `~`, relative paths, and workspace confinement uniformly;
  escapes are reported, not silently allowed.
- R3.6 Built-in set: `read`, `glob`, `grep`, `write`, `edit`, `bash`, `ask`, `todo`. Each works on
  Windows without external binaries (no `rg`, no GNU coreutils requirement); `ask` is the
  model's one channel to the user and fails closed when there is no UI.
- R3.7 Every tool ships tests that pin its output format; format changes are decisions,
  not accidents.

### R4 — Session and context (M2, M4, M6, M9)

- R4.1 Append-only JSONL session files with a version header and `{id, parentId, timestamp,
  type}` per entry. The entry is **linear**: `parentId` points at the previous entry, and
  branching is not built (D25). No content-addressed blob store until the first image (D25).
- R4.2 The header carries a format version. A session written by a newer version is refused
  with a clear reason; migration for older versions is written at the first version that
  changes the schema, so no session ever needs manual steps.
- R4.3 Load-time recovery: drop a torn last line, record a synthetic "outcome unknown"
  result for a tool call killed mid-flight, and drop an orphan result; the shell says what was
  repaired. "Include siblings" no longer applies — a linear log has none (D25, D49).
- R4.4 `--continue`, `--resume <id|path>`, `--no-session`; sessions are listed and
  selectable.
- R4.5 Compaction changes the **request projection only**, never stored history; the
  summary is traceable to the entries it replaced, and artifacts it points to remain
  readable.
- R4.5a Eliding a tool result keeps it paired with its call and replaces the body with a hint
  that says what the result was and how to get it back, so the model can go back for detail
  instead of relying on a summary of it (D47).
- R4.5b A rebuild costs one prompt-cache miss at the boundary, and that is the accepted
  cost: a cache-safe prefix rebuild was deliberately not adopted (D47). Buy it when a
  measurement shows the miss hurting.
- R4.5c The model's own tool-call output is never trusted as proof of completion: a driver
  layer can re-prompt until an external verifier passes.
- R4.6 Token accounting prefers provider-reported usage and falls back to a local
  estimate; the budget covers the projection actually sent, not raw history.
- R4.7 On a provider overflow the projection shrinks by dropping the oldest quarter, at most
  three attempts per episode (D47). A skill body is reproducible: compaction may elide it to a
  reload hint rather than pin it (D56).
- R4.8 Disk writes never block or break a turn: persistence failures are latched and
  surfaced, and the turn completes.
- R4.9 Long-term memory, per project, when turned on (D71): lessons the model saves with
  `learn`, and a summary distilled from finished sessions, are in front of the model at the
  start of every later session, as advice the repository and the user override. No secret is
  stored, the running session's prompt never changes underneath it, and the user can see,
  refresh and delete the memory.

### R5 — Permission and reversibility (M2, M4)

- R5.1 Three approval tiers (`read`, `write`, `exec`) and four modes — `default`,
  `acceptEdits`, `plan`, `bypass` — with rule lists (`allow` / `ask` / `deny`) and a per-mode
  default; deny wins over everything, including `bypass` (D36, D41).
- R5.2 The policy evaluator is a **pure decision function** with no input-rewriting
  outlet. Rewriting happens inside the approval boundary, so what was approved is what
  runs.
- R5.3 A prompt with no UI to answer it is a hard error, not a silent allow.
- R5.4 Registration wraps every tool, including tools registered at runtime (MCP), so
  a dynamically added tool cannot bypass approval.
- R5.5 Subagent edits can be isolated in a git worktree with a reviewable merge.
- R5.6 Approval has three independent layers: per-tool declaration, per-run policy supplied
  by the shell, and the mode. `ask` is a first-class outcome, not an error.

### R6 — Extension surface (M2, M4, M7)

- R6.1 MCP over stdio (D57), streamable HTTP and SSE (D66). Tool names are deterministic,
  sanitized and capped: `omp`'s `mcp__<server>_<tool>` (D57). MCP tools default to the write
  tier.
- R6.2 Skills are declarative (`SKILL.md` with frontmatter + body); the prompt lists each one
  in a line and the `skill` tool returns the procedure on demand (D56).
- R6.3 Hooks are lifecycle callbacks (pre-tool, post-tool, stop, prompt-inject) with
  narrow single-method interfaces; a hook that errors is reported and never blocks the tool or
  crashes the run (D58).
- R6.4 Context files (`AGENTS.md` / `CLAUDE.md` from the working directory up to the
  repository root, one per depth) load into a trailing `<project-context>` block so the
  static prefix stays cacheable. User-level files, `@` imports and cross-file deduplication
  are deferred until a user asks for them.
- R6.5 Subagents are context firewalls: fresh history, final result only, depth guard by
  tool removal (not by erroring), sidechain transcript, separate token accounting.
- R6.6 Background commands run to completion and wake the agent with a notification that
  carries pointers (id, status, path, summary), never full output.
- R6.7 The loop can stop after emitting a tool-call response and resume when a tool message
  arrives, so an embedding host may execute tools itself (external execution).

### R7 — Interfaces (M1, M3, M7)

- R7.1 One-shot `-p`: stdout gets the final answer, stderr gets progress; exit codes are
  stable (0 ok, 1 failure, 2 usage, 130/143 signals).
- R7.2 Interactive: streaming output, tool progress lines, approval prompts,
  mid-run steering, follow-ups, Esc to interrupt **leaving the partial answer visible**,
  slash commands (`/model`, `/effort`, `/new`, `/clear`, `/compact`, `/resume`, `/goal`, `/exit`).
  The TUI is the default shell: inline scrollback plus a live tail, never the alternate
  screen (D39).
- R7.3 Headless: NDJSON event stream, versioned event schema, a request/response channel
  for approvals. The schema is a wire protocol because consumers may be other processes.
- R7.4 Terminal output from model text is sanitized (ANSI and control characters
  stripped) before printing.

### R8 — Observability and evaluation (M5)

- R8.1 Usage is accounted per response, per run and per bucket (`input`, `output`,
  `cache_read`, `cache_write`). No price is computed: cost left the plan (D68).
- R8.2 A structured trace log of every loop event, replayable offline.
- R8.3 A golden-task eval harness: fixture repos, a scripted provider, pass/fail plus tokens
  and turn count, scoring the answer and the tool trajectory. Runs headless with no network.
  A live-model mode was tried and removed (D66).
- R8.4 The eval suite grows with the roadmap — no milestone is "done" without a task
  that would catch its regression.

## 4. Engineering requirements

### R9 — Structure

- R9.1 One Go module, one binary, one assembly root (`cmd/omp`). Only that package knows
  every other package.
- R9.2 Acyclic dependencies with one legal direction; the kernel imports no feature
  package; features never import each other except through declared combinators.
- R9.3 Contracts are defined by their consumer, not their implementor. Implementations
  satisfy them structurally.
- R9.4 Concrete types where returned, interfaces where accepted.

### R10 — Verification

- R10.1 Every package builds and vets clean; the full suite runs green under `-race`.
- R10.2 The kernel's tests never use `httptest`; wire tests live in the adapter packages.
- R10.3 Every default value and threshold has an anchor test, so a silent change fails.
- R10.4 Fakes must be able to express failure branches, or the recovery code is untested.
- R10.5 Each milestone ends with a real-key smoke run against a live endpoint.

### R11 — Discipline

- R11.1 No over-engineering: if you cannot name the current consumer of an abstraction,
  do not build it. Postpone with a written trigger condition instead.
- R11.2 Decisions are cross-checked against at least two reference implementations before
  adoption; disagreements are resolved explicitly and recorded.
- R11.3 Deferrals are recorded with the condition that revives them.
- R11.4 Code comments follow [DEVELOPMENT.md](DEVELOPMENT.md) §2: new code carries none.
