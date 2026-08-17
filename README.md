# continuo

**A long-running orchestrator that watches a GitHub Projects v2 board, prepares a git worktree per issue, and runs an interactive Claude Code session for it inside a [herdr](https://github.com/herdrdev/herdr) pane.**

GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して、herdr の pane で Claude Code を対話モードで起動し、完了までを面倒見る常駐プロセスです。

> **Status: designing.** No implementation yet — see [docs/plans/continuo_design.md](docs/plans/continuo_design.md).
> **状態: 設計中。**実装はまだありません。

## What it does / 何をするか

| | |
| --- | --- |
| **Watches** | one GitHub Projects v2 board — issues from multiple repositories live on it |
| **Prepares** | a git worktree per issue, in the repository that issue belongs to |
| **Runs** | Claude Code in interactive mode inside a herdr pane (**never `claude -p`**) |
| **Continues** | sends follow-up prompts to the same session while the issue stays active, up to `max_turns` |
| **Detects completion** | via Claude Code's `Stop` hook — a turn has ended only when `background_tasks` is empty |
| **Trusts** | the tracker, not the agent's self-report. The agent moves the Status itself with `gh`; continuo only reads |
| **Cleans up** | the worktree and the branch once the issue reaches a terminal state |

## Why the name / 名前の由来

**Basso continuo** — the bass line in Baroque music that sounds without interruption from the first bar to the last, holding the whole harmony together. That is what this process does: it stays up, keeps the queue moving, and never becomes the thing you have to watch.

**通奏低音。**バロック音楽で、曲の最初から最後まで途切れず鳴り続け、全体の和声を支える低音パートのことです。

> Not to be confused with *continuous integration / continuous deployment* tooling. This is an agent orchestrator, not a CI system.
> **`continuous-*` 系のツールとは無関係です。**CI ではなく、コーディングエージェントのオーケストレーターです。

## Specification / 準拠する仕様

continuo implements the [openai/symphony](https://github.com/openai/symphony) service specification (Apache-2.0). A copy of the spec is kept at [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md).

Where the spec assumes a Codex app-server subprocess speaking a structured protocol over stdio, continuo substitutes an interactive Claude Code session driven through herdr, with turn completion delivered by Claude Code hooks. The full conformance mapping — including the `MUST` clauses that cannot be met and what replaces them — is in the design document.

**仕様が前提とする Codex app-server のプロトコルは使えないため、herdr 上の対話型 Claude Code と hooks による turn 終了検知に読み替えています。**守れない `MUST` とその代替は設計文書にあります。

## Requirements / 前提

| | |
| --- | --- |
| Go | 1.26 or later |
| [herdr](https://github.com/herdrdev/herdr) | 0.8.0 or later (Apache-2.0) |
| [Claude Code](https://claude.com/claude-code) | 2.1.233 or later |
| `gh` | 2.97.0 or later — the Status update form used in prompts needs it |
| Platform | macOS and Linux (including Ubuntu on WSL2) |

## Documents / 資料

| Path | Contents |
| --- | --- |
| [docs/plans/continuo_design.md](docs/plans/continuo_design.md) | The design. Every decision and its evidence |
| [docs/naming.md](docs/naming.md) | How the name was chosen |
| [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) | The symphony service specification (Apache-2.0, upstream copy) |
| [docs/evidence/](docs/evidence/) | Raw observations from the hook behaviour experiments |

## License

MIT — see [LICENSE](LICENSE).

One exception: [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) is a redistributed copy of the [openai/symphony](https://github.com/openai/symphony) specification, licensed by its authors under **Apache-2.0**. Its full license text is at [LICENSE-APACHE-2.0.txt](LICENSE-APACHE-2.0.txt), and the attribution is recorded in [NOTICE](NOTICE) and in the file's own header.
