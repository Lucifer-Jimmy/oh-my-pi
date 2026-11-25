# Development protocol — oh-my-pi

> How work happens. The rules in [ARCHITECTURE.md](ARCHITECTURE.md) say what the code
> must look like; this document says how a slice gets from idea to committed code.

---

## 1. Working mode

The teaching-first protocol this section used to specify (teach → agree → print the code →
land) was **suspended on 2026-11-12** at the user's request ("全力推进开发"). Work now runs as:

1. **Read the mechanism** in the reference source (`C:\Code\oh-my-pi-origin` and the
   others in Architecture §10) before writing anything. Cross-check against a second
   reference before adopting a semantic (Law 7); a single blueprint is not evidence.
2. **Build with tests.** Implement and test together; do not land either alone.
3. **Verify.** `go build ./...`, `go vet ./...`, `gofmt -l .` clean and the suite green under
   `-race` before a slice counts as done.
4. **Record.** Non-obvious adjudications go into [DECISIONS.md](DECISIONS.md) with a date and
   a reversal condition; milestone state updates in [ROADMAP.md](ROADMAP.md), tree state in
   [STATUS.md](STATUS.md).

Teach-first returns when the user asks for it, or for a decision large enough that getting it
wrong would not surface in a compiler or a test.

## 2. Code conventions

- **New code carries no comments**, tests included (user directive, 2026-11-12). Build
  directives such as `//go:build` are not comments. Comments already in older packages stay as
  they are, neither cleaned up nor added to. Design reasons, reference comparisons and
  adjudications go into [DECISIONS.md](DECISIONS.md) or the conversation, never the source.
- Names follow Go community convention and the reference projects' idiom. No cutesy
  abbreviations (`realMain`, `doStuff`).
- Return concrete types, accept interfaces. Define interfaces where they are consumed.
- No exported symbol without a consumer today.

## 3. Testing protocol

| Layer | What is tested | How |
|---|---|---|
| `llm/*` adapters | Wire shape, streaming edge cases, error surfacing | Synthetic SSE fixtures inside the adapter package; `httptest` allowed here only |
| `agent` kernel | Loop invariants, pairing, truncation, cancellation, stop chain | Scripted `RunDeps.CallModel`; pure-data assertion on the terminal `Result`; never `httptest` |
| Tools | Output format, caps, path handling, error text | `t.TempDir` fixtures; Windows path cases |
| Session | Round-trip, migration, recovery | Byte-level comparisons |
| `eval` | Kernel invariants as tasks: answer + tool trajectory | Scripted provider, `t.TempDir` fixtures, no network |
| End-to-end | Real behaviour | Print mode against a live endpoint, key read from config |

Rules:

1. **Offline first.** The full suite runs with no network and no key.
2. **`-race` green.** Not "mostly green" — green.
3. **Anchor every constant.** A `TestDefaultsUnchanged`-style test pins each default so a
   silent change fails the build.
4. **Fakes must be able to fail.** A fake compactor whose `IsOverflow` is always false
   leaves the overflow path untested — the exact defect found in the reference
   implementation.
5. **Each milestone closes with a live smoke run** against a real endpoint on all
   supported protocols.

## 4. Per-slice checklist

Before marking a slice done:

- [ ] Behaviour matches the blueprint or the deviation is recorded
- [ ] Every new tool declares `IsReadOnly`, `IsConcurrencySafe`, `Approval` and `Metadata` (D44)
- [ ] No comments in new code, including lines added to older files (§2)
- [ ] Every new constant has an anchor test
- [ ] Error paths produce non-empty model-readable text
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] Full suite green under `-race`
- [ ] Non-obvious choice recorded in DECISIONS with its reversal condition
- [ ] Deferrals recorded with a revival trigger in the gap ledger

## 5. Where things go

| Question | Document |
|---|---|
| What must the product do? | [REQUIREMENTS.md](REQUIREMENTS.md) |
| Is this allowed structurally? | [ARCHITECTURE.md](ARCHITECTURE.md) |
| What is built, in what order? | [ROADMAP.md](ROADMAP.md) |
| Why is it this way? | [DECISIONS.md](DECISIONS.md) |
| What does the tree contain today? | [STATUS.md](STATUS.md) |
| How does the blueprint do it? | the reference source on disk (Architecture §10) |

Files never overlap in purpose. A fact lives in one document; others link to it. In
particular: milestone **state** lives in ROADMAP only, tree **inventory** in STATUS only,
**why** in DECISIONS only (its index is the current truth), and the **laws** in ARCHITECTURE
only.

## 6. Conventions for the documents themselves

- English only.
- Requirements are numbered (`R4.5`) and referenced by number in milestones and code
  reviews.
- Decisions are numbered (`D12`) and a number is never reused. A decision that is superseded,
  or that only ordered work now finished, is removed; the entry that replaced it says what
  changed (user, 2026-11-23).
- Open questions carry the milestone that must resolve them (`O10`, needed by M8). A resolved
  question is removed; the decision that resolved it carries the answer.
- The roadmap is the only place milestone state lives; the README table is a summary of it
  and is corrected whenever it disagrees.
