# Architecture — oh-my-pi

> The structural constitution. Adding or removing a package, changing a dependency
> direction, or reshaping a contract requires passing the laws below first.
>
> Requirements it serves: [REQUIREMENTS.md](REQUIREMENTS.md). Build order:
> [ROADMAP.md](ROADMAP.md). Rationale for individual choices: [DECISIONS.md](DECISIONS.md).
> Field-level truth about what exists: [STATUS.md](STATUS.md).

---

## 1. Position and reference frame

The product is a **general-purpose agent platform**: one engine behind three shells — TUI,
GUI and an SDK — with the common coding-agent feature set as the baseline (D29, D30). The
engine is one binary and a set of public Go packages; the coding toolset is one bundle among
whatever a host registers, because the kernel is feature-blind and stays that way.

The admission test for any surface: **does it make an agent application cheaper to build, or
the agent more capable to use?** Cut rather than defer; a deferral names the condition that
revives it. `omp` and the other references are sources of mechanisms, not targets to match
feature for feature.

| Reference | Path | What we take | What we refuse |
|---|---|---|---|
| `omp` (TS coding agent) | `C:\Code\oh-my-pi-origin` | Product surface, tool semantics, approval tiers × modes, system-prompt block layout, compaction thresholds, MCP naming | In-process TS extensions, session tree/fork/branches, 191k-line TUI, 25-helper `AgentSession`, `xd://`, speculative execution, 148-flag CLI, feature parity as a goal |
| `trpc-agent-go` | `C:\Code\trpc-agent-go` | SDK translator shape, tool metadata (`destructive`/`open-world`/`max-result-size`), external tool execution, elision with recovery pointers, tool-trajectory eval, verifier loop | Hosted-platform concerns: artifact store, RAG, evaluation service, tenancy, graph engine |
| `Norma` (Go harness) | `C:\Code\Norma` | Loop mechanisms: scheduling, pairing, cancellation, permission pipeline as pure function | God-package layout, feature-aware `QueryInput`, library-level assembly root, `noa` |
| Official SDKs | `anthropic-sdk-go/v2 v2.4.0`, `openai-go/v3 v3.91.1` | Wire parsing, retries, cancellation semantics | — (we write no wire code) |

### 1.1 What "smaller and stricter" means concretely

`omp` is ~503k lines of TypeScript plus Rust natives. Nothing is ported wholesale. What we
build is the part that carries the job — loop, tools, session, approval, compaction,
subagents, MCP — with invariants the original leaves to convention. We are stricter on:

- Pairing on **every** exit path, not just the happy one.
- Approval that cannot be bypassed by rewriting arguments after the decision.
- Compaction that cannot mutate stored history.
- Contract seams that make the kernel testable without a network.

## 2. The laws

1. **One module, one binary.** A single `go.mod`
   (`github.com/Lucifer-Jimmy/oh-my-pi`). Directory boundaries are package boundaries.
   Split a package out for release only when there is a consumer outside this module.

2. **Acyclic dependencies, one legal direction.**
   ```text
   llm            -> (nothing in this module)
   llm/anthropic  -> llm   (SDK types stay inside)
   llm/openai     -> llm   (SDK types stay inside)
   prompt         -> (stdlib only)
   sysproc        -> (stdlib only)
   session        -> llm
   agent          -> llm, session
   permission     -> llm, agent
   tools          -> llm, agent, permission, sysproc
   plan           -> llm, agent, permission
   compaction     -> llm, session, agent
   presentation   -> llm, agent
   hook           -> llm, agent
   mcp            -> llm, agent
   skill          -> llm, agent
   trace          -> llm, agent
   verifier       -> session, agent, sysproc, tools
   goal           -> llm, session, agent
   memory         -> llm, session, agent
   subagent       -> llm, session, agent
   background     -> llm, agent, tools
   eval           -> llm, session, agent, permission, tools
   tui            -> llm, session, agent, permission, presentation
   cmd/omp        -> every package except sysproc and eval (the only assembly root)
   ```
   These are the direct imports, not an aspiration — `go list -f '{{.Imports}}' ./...` is the
   check. `agent` imports no feature package; features interact through `agent`'s hook and event
   seams. A feature may import a lower layer it genuinely uses (`permission` imports `agent` for
   `Tool`; `tools` imports `permission` for rule specs and `SplitCommand`; `plan` imports both), but **no two
   features import each other**. A mode's own control tool lives in the mode's package, not
   in `tools/` (`plan`, D43).

3. **Interfaces are defined by their consumer.** `Tool`, `ToolContext`, `Host`,
   `Compactor`, `Verifier`, `Runner` and the four hook interfaces all live in `agent`.
   Implementors satisfy them structurally. Contract signatures use only standard-library,
   `agent`, `llm` and `session` types.

4. **A feature is an adaptation of a contract**, in exactly one of four shapes:
   middleware (`permission` wraps tools), adapter (`mcp` bridges a protocol),
   constructor (`subagent` builds child loops), driver (`background` feeds the loop from
   outside through `RunDeps.Notify`; `goal` wraps the runner and starts the next run itself,
   D70; `memory` distils finished sessions beside the loop and feeds the next session's
   prompt, D71). If you cannot name which shape a feature is, the contract is
   wrong — fix the contract before creating the package.

5. **`cmd/` is the only assembly root.** Exactly one package knows every other package.
   No global singletons, no `init()` registration magic. Deeply coupled features may
   expose an `Enable(opts)` helper whose only caller is `cmd`.

6. **The model layer is a neutral model plus SDK translators.**
   Wire parsing and retries belong to the official SDKs; we write no SSE parsing, no
   retry loops, no wire types. SDK types never leave an adapter package; neutral types
   never reach the wire. Translator rules: exhaustive switch, explicit field picking,
   a loud `default`, and a `Raw` escape hatch that forwards unknown blocks verbatim.
   The neutral model admits only state that must survive across providers (persisted,
   replayed, or consumed by another layer).
   **Two protocols cover the ecosystem**: `anthropic-messages` and `openai`
   (`completions` + `responses`). DeepSeek, Qwen, GLM, Kimi, vLLM, Ollama, OpenRouter
   all ride the OpenAI package via `baseUrl`. Vendor differences live in exactly three
   places: SDK options, the adapter's convert escape hatch, and a capability matrix
   (thinking levels, clamping). Never add an adapter package for a single vendor. A
   third adapter requires: a target vendor with no trustworthy two-protocol endpoint,
   and a needed feature that the compatibility path cannot express.

7. **Cross-check before deciding.** Any decision about semantics, structure, contracts
   or blueprint adoption is verified against ≥2 references from §1 before code is
   written. A single blueprint is not evidence — copying it imports its bugs and its
   era. Disagreements get surfaced and adjudicated explicitly, and the adjudication is
   recorded in [DECISIONS.md](DECISIONS.md).

8. **No over-engineering.** Do not pre-pay complexity for imagined needs. Extension
   points with no consumer are not defined; hooks with no consumer are not added. If
   you cannot answer "who consumes this now?", do not build it. Postpone with a written
   trigger condition; refactoring later is cheaper than permanent debt now.

## 3. Package layout

Everything is a public package (D48). `omp` is an Agent Harness SDK first and a terminal
program second: a consumer embeds the loop and swaps any layer, and our shells are just the
first two consumers of that same surface. There is no `internal/` — nothing is hidden from the
SDK, and the naming rules in §3.1 are what keep the surface formal.

```text
oh-my-pi/
├── go.mod                          # single module (Law 1)
├── docs/                           # this document set
├── cmd/omp/                        # ★ the only assembly root (Law 5)
├── llm/                            # PUBLIC. neutral model, Provider contract, accumulator, overflow test
│   └── anthropic/  openai/         #   wire translators (SDK types stay inside)
├── agent/                          # PUBLIC. contracts + loop + scheduler + hooks + host + compaction seam
├── session/                        # PUBLIC. Session contract and compaction records
├── permission/                     # PUBLIC. policy + Wrap; the safety contract consumers plug into
├── tools/                          # PUBLIC. the built-in tool catalog, one package (D37)
├── compaction/                     # PUBLIC. elide-first compaction; satisfies agent.Compactor (D47)
├── prompt/                         # PUBLIC. system-prompt builder and context files
├── plan/                           # PUBLIC. plan mode — reminders and its exit_plan_mode tool (D43)
├── presentation/                   # PUBLIC. the reducer: presentation state, no terminal, no DOM (D39)
├── verifier/                       # PUBLIC. the external check that decides "done" (D50)
├── goal/                           # PUBLIC. goal mode: tracker, goal tool, continuation runner (D70)
├── memory/                         # PUBLIC. long-term memory: lessons, past-session summary, learn tool (D71)
├── trace/                          # PUBLIC. the JSONL run recorder (D54)
├── eval/                           # PUBLIC. offline golden tasks, scripted provider (M5)
├── tui/                            # PUBLIC. shell 1: Bubble Tea, inline scrollback + live tail
├── markdown/                       # PUBLIC. the CommonMark/GFM parser with CJK-aware emphasis (D67); shells lay it out
├── hook/                           # PUBLIC. subprocess hooks, four families (D58)
├── mcp/                            # PUBLIC. stdio MCP client bridged as tools (D57)
├── skill/                          # PUBLIC. SKILL.md discovery and the skill tool (D56)
├── subagent/                       # PUBLIC. the task tool and worktree isolation (D61)
├── background/                     # PUBLIC. background commands and pointer notifications (D59)
├── sysproc/                        # PUBLIC. the platform seam: process-group setup and tree kill
└── gui/                            # parked M8 start, not wired into the binary (STATUS)
```

Role test: **public surface** (a consumer imports it) → **kernel** (contracts + engine) →
**feature** (implements a contract) → **tool surface** (model-callable) → **shell**
(implements `Host`, or speaks the wire protocol). A package that fits no role does not belong.

Being public is still a compatibility promise (D48). The wire protocol froze at M7 and is
versioned (D64): a frame that changes meaning bumps `v`. The Go packages are still `v0.x`: a
breaking change to an exported symbol is allowed, but it is recorded in DECISIONS, as D68 did.

### 3.1 Naming rules

Package names are nouns that say what the package *is* — never abbreviations, never `util`,
`common`, `helpers` or `types` (Law 8). D48 applied these rules once; new packages follow them
from their first commit.

| Rule | Example |
|---|---|
| A layer of the stack is named for its role | `llm` (the model layer), `agent` (the kernel), `tools` (the catalog) |
| A capability is a noun | `compaction`, `permission`, `session` |
| A shell is named for the surface it drives | `tui`, `gui` |
| A reducer is named for what it produces | `presentation` (not `ui`) |
| A protocol type is `Protocol`, not `Api` | `llm.ProtocolAnthropicMessages` |
| Compile-time initialisms are all-caps | `APIKey` |
| Config keys mirror the product we compare against (`omp`) | `providers.<n>.api`, `compaction.contextWindow` |

### 3.2 Build state

Not in this document. [STATUS.md](STATUS.md) is the single inventory of what exists, verified
against the tree; [ROADMAP.md](ROADMAP.md) is the authority on milestone state. A table here
would be a third copy — and it drifted when it was one.

## 4. Core contracts (all defined in `agent`)

### 4.1 Tool and optional capabilities (fail-closed)

```go
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult
}
type ToolResult struct {
	Content []llm.ContentBlock
	IsError bool
	Extra   []llm.Message // follow-up messages appended after the tool results; no built-in tool sets it (D56)
}
```

Optional capability interfaces — **not implementing one is the most conservative
behavior**, and that is a feature, not a gap:

- `Schedulable{IsReadOnly() bool; IsConcurrencySafe(input) bool}` — undeclared means
  exclusive scheduling.
- `ApprovalDeclarer{Approval(input) Approval}` — undeclared means `exec` tier; MCP tools
  default to `write`.
- `MetadataDeclarer{Metadata(input) Metadata}` — `Destructive`, `OpenWorld`,
  `MaxResultSize`; undeclared means destructive, open-world and a 64 KiB result cap (D44).

Capabilities are always added as optional interfaces plus type assertions; the main
interface never grows a method. Background-task capability is injected by the tool
constructor, not carried in `ToolContext`. `ToolContext` has exactly five methods:
`CWD / Workspaces / Emit / Ask / RecentMessages`. Nothing is named `Context`.

### 4.2 Events: the iterator is the only loop event source

The loop yields events; the shell consumes them. Kinds: `text_delta`,
`thinking_delta`, `tool_use`, `tool_result`, `progress`, `usage` (per response, with its stop
reason and duration), `notification`, `compaction`, and a terminal `result` carrying
reason/error/usage/turns/message snapshot/text. Stop reasons include `verifier` (D50). All
kinds are pure data and JSON-serializable. The external wire protocol is a versioned
projection of these events, not the Go struct itself (D64). Channels are used only inside a
package; bridging pull→push happens in the shell.

Ordering: the event layer may emit safely-parallel tool results in completion order,
but the persistence layer always records call order. The scheduler in fact emits in
call order, which is the stronger guarantee and keeps replay trivial.

### 4.3 Hooks: separate single-method families

```go
PreToolCallHook{BeforeToolCall → Decision}   // may block or rewrite; rewrite happens inside the approval boundary
StopHook{OnStop → StopDecision}              // may prevent stopping
PostToolCallHook{AfterToolCall → *ToolResult} // v1 observe/rewrite results
PromptInjectHook{OnPrompt → []llm.Message}    // runs before every request (D58)
```
No hook is defined without a consumer (Law 8). Approval is **not** a hook: it needs a
bidirectional UI channel and belongs in middleware.

### 4.4 Loop: one call is one complete loop

```go
func NewLoop(reg *Registry, deps RunDeps, host Host, opts Options) *Loop
func (l *Loop) Run(ctx, sess, userInput) iter.Seq2[Event, error]
type Runner interface{ Run(ctx, sess, input) iter.Seq2[Event, error] } // RunnerFunc adapts a function
```

**Who owns what.** A `Loop` is built once and holds four things that never change while it
runs. Each `Run` call creates one `run` value that holds everything that does; nothing about a
run is threaded through pointers or kept on the `Loop` (D72).

| Held by | Part | What it is |
|---|---|---|
| `Loop` | `Registry` | the tools, each already wrapped by the approval middleware |
| `Loop` | `RunDeps` | what the loop calls out to: `CallModel`, `Compactor`, the `Steer` and `Notify` feeds, the clock |
| `Loop` | `Options` | what shapes a request and bounds a run: system prompt and cache boundary, `MaxTokens`, `Thinking`, `MaxTurns`, `TokenBudget`, `Concurrency`, workspace — and `Hooks` |
| `Loop` | `Host` | the round-trip channel for `Ask`; `nil` means no interactive user |
| `run` | per call | session, emitter (sequence numbers), concurrency semaphore, usage so far, turns, consecutive truncations, whether automatic compaction is still on |

`Hooks` are callouts and belong with `RunDeps` by that rule, and `RunDeps.NewID` is read by
nothing; D72 records both instead of moving them, because either fix changes the public API.

**One run, step by step** — `loop.go` drives, `stream.go` talks to the model, `exec.go` runs
tools, `compact.go` compacts:

1. The input is appended to the session as a user message.
2. Every round starts with the **halt checks**: a cancelled context ends the run `aborted`, a
   spent token budget ends it `token_budget`. Then the round **feeds** the session — pending
   steering, background notifications, the prompt hook — builds the request from it, and
   compacts it when a compactor is set. A failed automatic compaction turns automatic
   compaction off for the rest of the run.
3. **The turn** streams the response. A tool call goes to the scheduler the moment its argument
   block closes (D51); a call whose arguments are not a complete JSON object is refused. A
   context overflow reported before anything arrived compacts the request once and resends it.
4. A cancelled or failed stream **settles** the tools already running — their results are
   emitted, nothing is appended — and ends the run.
5. Otherwise the assistant message is recorded with its usage. Without tool calls the **stop
   chain** decides, in this order: pending steering, background notifications, then the stop
   hook may each continue the run; if none does, it ends `completed`.
6. With tool calls, results are collected in call order. A call that never ran gets the
   truncation error; the results message is appended, then any `Extra` messages. A truncated
   turn is not counted, and the fourth consecutive one ends the run `error`; `MaxTurns` ends it
   `max_turns`.

**One tool call** (`run.exec`): look the tool up (unknown → an error naming the closest tools) →
pre-tool hook (may block or rewrite the arguments) → arguments checked against the schema and
coerced (D46) → the registry's wrapper evaluates the approval policy and asks the host (D36,
D41) → `Execute` inside the panic boundary → result clamped to the declared size (D44) →
post-tool hook (may rewrite) → an empty error replaced by a fixed text. The content becomes a
`tool_result` block in one place, `wrapToolResult`.

**Invariants**

- `RunDeps` is the test seam: a scripted provider is 15 lines and needs no network.
- **One context per turn.** The model stream and that turn's tools run under one context
  derived from `ctx`. A tool's own timeout (`bash`: 120 s by default) is derived inside the
  call, so it never cuts the stream; parent cancellation aborts the turn. A loop-level
  wall-clock budget that ends in a wrap-up turn is deferred until something needs it.
- **Budget invariants.** `Turn` counts tool rounds only; recovery, nudges and
  continuation do not consume it; each continuation path has its own cap; budgets are
  checked only at round boundaries.
- **Pairing is a runtime invariant**, not crash-recovery special-casing (see R1.2).
- **Two mandatory gates**: wait for or cancel already-dispatched tools before resuming
  after a stream error (no orphans); a call whose arguments did not arrive as a complete
  JSON object is never executed and gets a truncation error, while calls that completed
  before the cut have already run and are paired normally (D51).

### 4.5 Collaboration contracts (defined by the consumer; nil is allowed)

`Compactor{Compact(ctx, CompactRequest) ([]llm.Message, *session.Compaction, error)}` and
`Verifier{Verify(ctx, sess) (CheckResult, error)}`, driven from outside by `VerifierRunner`
(D50). Steering and background notifications are plain functions on `RunDeps` (`Steer`,
`Notify`); the trace recorder wraps the `Runner` in `cmd/omp` instead of plugging into the
loop (D54). `CompactRequest` carries the request that would be sent, the last checkpoint, the
trigger (auto / overflow / manual) and the user's instructions. Compaction invariant: **compaction never changes storage, only the
request projection**; derived content is traceable back to the entries it replaced.

### 4.6 The shell contract: one schema, three transports

The engine is shell-agnostic. A shell participates in exactly three directions:

| Direction | Content | TUI | GUI | Out-of-process SDK |
|---|---|---|---|---|
| **out** | the event stream (deltas, tool use/result, usage, result) | Go values in-process | serialized frames | serialized frames |
| **in** | commands (prompt, steer, follow-up, abort, approve/deny, set model/effort, session ops) | Go method calls | frames | frames |
| **round-trip** | `Ask` / approval — bidirectional, unbounded latency | `Host` interface | request/response frames | request/response frames |

Two rules follow:

1. **The TUI does not serialize, but it uses the types that will be serialized.** No second
   event vocabulary, no presentation types in the engine. The TUI therefore does not validate
   the wire format — the frame-level tests do (D64).
2. **Presentation state is not the engine's business.** A UI-agnostic *reducer* package owns
   the turn list, item kinds, tool pairing, plan state, usage and composer state. It exposes
   `Apply(event)` into that state and a JSON snapshot of it; rendering is a pure function of
   the state. The TUI and the GUI consume the same reducer, so they cannot silently diverge,
   and golden JSON snapshots become the contract test. Diff-based `Patch` output is cut until
   an out-of-process consumer exists (D39).

`agent.Host` is the in-process embodiment of the round-trip direction. `Ask` with no host
returns `ErrNoUI`; callers turn that into a hard error, never a default answer. The shell
services `Ask` from its own loop so painting continues while the tool goroutine blocks, and a
cancelled turn returns a cancelled `AskResponse` so the tool fails closed.

**Event batching lives in the shell, not the engine.** The shell drains every event already
buffered into one message (up to a per-message cap), which coalesces deltas without a timer;
tool start/end, approvals, results and errors are never dropped or reordered. A separate
lossy/reliable lane split is not built: the reducer benchmark (2026-11-17) put the reducer four
orders of magnitude above the budget, so it waits for a real terminal to show a need (D39).
Sequence numbers are assigned when events leave the engine; the reducer rejects duplicates and
out-of-order state transitions.

### 4.7 Plan mode

Plan mode is **policy, not tool behaviour**. While active, the evaluator denies every write and
exec tool, so no tool implementation can escape the mode by forgetting a guard. The model learns
the mode from a reminder the shell prefixes to each input, and leaves it only through
`exit_plan_mode`, which presents the plan through `Host.Ask` and switches the mode when the user
approves. The plan is stored in the session as that call's input. Compaction and subagents
shipped without a separate plan object; one arrives when something has to read the plan
outside the conversation. See D43.

## 5. How features attach

```go
permission.Wrap(tool, policy) → Tool         // middleware: pure evaluate, no rewrite outlet
mcp.Connect(ctx, name, cfg) → *Server        // adapter: Server.Tools() bridges MCP → agent.Tool
skill.Discover(cwd) → []Skill                // declarative: SKILL.md, body served by skill.Tool
subagent.NewTaskTool(cfg) → *Task            // constructor: builds a child Loop directly
background.Manager.Start / Take              // driver: owns processes, Take feeds RunDeps.Notify
goal.Runner{Runner, Tracker} → Runner        // driver: continues an active goal until it is completed (D70)
memory.Pipeline.Run / memory.Guidance        // driver beside the loop: distils past sessions; the block joins the next prompt (D71)
hook.Set.Hooks() → agent.Hooks               // subprocess hooks, four families (D58)
```

`Registry.SetWrapper(fn func(Tool) Tool)`: the assembly root injects `permission.Wrap`,
and **registration applies the wrapper**, including runtime registration — this closes
the "dynamically registered tool forgot its approval" hole by construction.

## 6. Assembly root and data flow

`cmd/omp` is the only package that knows every other one. `dispatch` builds the parts once, in
this order — provider (retry, thinking level) → registry (approval wrapper first, then the
built-ins, plan, goal, skill, MCP, background and `task` tools) → options and hooks →
conversation (the session file) → trace — and keeps them in one `app` value. Every mode then
gets its runner from the same method, `app.stack`, which wraps the loop in four layers, each an
`agent.Runner` around the next:

| Layer | What it adds |
|---|---|
| `Loop` | one complete loop per input (§4.4) |
| plan reminder | prefixes the plan-mode reminder to each input (D43) |
| `agent.VerifierRunner` | re-prompts until the external check passes (D50); a pass-through without `--verify` |
| `goal.Runner` | starts continuation runs while a goal is active (D70) |
| trace | records every event when `--trace` is set (D54) |

| Mode | Host | Shell |
|---|---|---|
| `-p` (print) | none | `print.go` |
| `--mode json` | none | `jsonmode.go` |
| `--mode rpc` | `rpcHost`: `ask` and `tool_call` frames | `rpc.go` |
| interactive | `tui.Host`: the approval dock | `tui` |

Files follow the same cut: `dispatch.go` (startup, the `app`, the stack, the mode),
`assemble.go` (registry, tool set, subagent wiring, system-prompt options, hooks, MCP),
`provider.go` (provider construction, model switch, context window, compactor), `runner.go` (the
stack's layers, the steering queue, the trace file), `interactive.go` (session operations for
the TUI). A new shell implements `Host` in-process or speaks the RPC wire protocol, and takes
its runner from `app.stack`; zero contract changes.

The data flow is always: shell → runner stack → Loop → Provider/Tools → events back through
the stack to the shell. No package calls a Provider directly, bypassing the Loop; no tool
drives the Loop.

## 7. Persistence model

**A linear append-only log.** One JSONL file per session: a version header, then one entry per
line. Every entry carries `{id, parentId, timestamp, type}`; `parentId` always points at the
previous entry today. The schema therefore anticipates branching, but the product does not
implement it — no fork, no `/tree`, no branch summaries, no sibling-aware recovery, and no
content-addressed blob store until the first image lands (D25).

Entry types today: `message`, `compaction` (a checkpoint naming the retained tail, trigger
and pre-token count), `title` (a rename, last one wins, D60) and `goal` (the session's goal,
last one wins, D70). Usage is persisted per entry and summed on load. `notification`,
`skill` and `custom` arrive with their first consumer; adding one is a schema addition, not a
format break (D49).

**Recovery on load** (D49): a torn last line is dropped → a tool call with no result comes
back as an `outcome unknown` error result → an orphan result is dropped; the shell reports
what was repaired. The header carries a version: a newer version is refused with a clear
reason, and migration for older versions begins at the first version that needs one.

**Compaction is a projection, and it elides before it summarizes** (D11, D26):

1. Tool results are split by reproducibility — `read`/`glob`/`grep`/`skill` output is
   replaced by a re-run hint; every other result (`bash`, edits) keeps its outcome, the tool
   name and its last 10 lines (D47). Nothing is spilled to a file; a spill with a pointer waits
   for a consumer that reads pointers back (D66).
2. Only when elision is insufficient is a span of conversation summarized, and only the span
   that must go.
3. A rebuild costs **one prompt-cache miss** at the boundary: the checkpoint sits at the head
   of the projection, so nothing before it can stay byte-identical. A cache-safe prefix
   rebuild was deliberately not adopted (D47) — buy it when a measurement shows the miss
   hurting.

Storage is never rewritten by any of this. Derived content stays traceable to the entries it
replaced.

## 8. Anti-patterns (hard red lines)

- No `types`, `common`, `util`, `helper` packages. No type named `Context`.
- No god objects: a struct over ~500 lines importing three or more feature packages is
  a signal that the contract must be split.
- **No feature-aware core.** A universal `QueryInput` struct with fields for todos,
  budgets and tasks is the anti-pattern. Loop stays ignorant; features attach at seams.
- One event model, one assembly root, one dependency direction. Return concrete types,
  accept interfaces.
- **Platform seam rule**: platform-specific capability = one interface + per-GOOS
  build-tagged file + explicit unsupported error. Silent stubs are forbidden.
- **Security = approval + worktree, no OS sandbox** (see R5 and DECISIONS). The scheduler
  and tool execution layers stay agnostic: how a tool runs is decided by the injected
  exec function.
- Lowercase single-word package names; `tools` as a plural package name is the accepted
  exception (one package, D37).
- Every exported symbol gets a second look for necessity — does it have a consumer today?

## 9. Testability (constitutional)

Three things are mandatory: an injected model seam (`RunDeps.CallModel` has the same
signature as `Provider.Stream` so either can stand in), a pure-data assertion surface
(the terminal `Result`), and nil-tolerant collaborators. Red line: **`agent` tests never
import `httptest`** — wire tests belong to adapter packages.

Blind spots that must be tested: stream error paths, both overflow paths, an
always-blocking stop hook, streaming cancellation. Every default and constant gets a
`TestDefaultsUnchanged`-style anchor.

## 10. Reference material

Notes taken from the references drifted; they were removed. The sources on disk are the
authority — read them at decision time, not a copy of them.

| Topic | Location |
|---|---|
| `omp` product and kernel (the blueprint) | `C:\Code\oh-my-pi-origin`, architecture in its `docs/` |
| `pi` harness spec / session format | `C:\Code\pi\packages\agent\docs\harness.md`, `packages\coding-agent\docs\session-format.md` |
| `Norma` mechanisms and defects | `C:\Code\Norma` |
| `trpc-agent-go` (tool metadata, elision, verifier) | `C:\Code\trpc-agent-go` |
| `pigo` (Go port of `pi`, TUI + SDK shape) | `C:\Code\pigo` |
| Official SDK sources | `go env GOMODCACHE` |
| Why a mechanism was chosen, with its evidence | [DECISIONS.md](DECISIONS.md) |
