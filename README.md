# continuo

**A long-running orchestrator that watches a GitHub Projects v2 board, prepares a git worktree per issue, and runs an interactive Claude Code session for it inside a [herdr](https://github.com/herdrdev/herdr) pane.**

GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して、herdr の pane で Claude Code を対話モードで起動し、完了までを面倒見る常駐プロセスです。

> **Status: implemented, not yet run against a live issue.** Every stage builds and passes tests,
> and the setup commands were exercised against a real board (read-only). **Driving one issue from
> `Ready` to `Done` has not been done yet.** See [docs/trying_it_out.md](docs/trying_it_out.md).
>
> **状態: 実装済み。ただし issue を1件通すところまでは未実行。**
> 全段がビルドとテストを通り、セットアップのコマンドは実在のボードに対して（読み取りのみで）叩いてあります。
> **`Ready` から `Done` まで通す確認はこれからです。**

## What it does / 何をするか

| | |
| --- | --- |
| **Watches** | one GitHub Projects v2 board — issues from multiple repositories live on it |
| **Prepares** | a git worktree per issue, in the repository that issue belongs to |
| **Runs** | Claude Code in interactive mode inside a herdr pane (**never `claude -p`**) |
| **Continues** | sends follow-up prompts to the same session while the issue stays active, up to `max_dispatch_turns` |
| **Detects completion** | via Claude Code's `Stop` hook — a turn has ended only when `background_tasks` is empty |
| **Trusts** | the tracker, not the agent's self-report. The agent only prints a one-line marker (`CONTINUO-STATUS: review`); **continuo is what moves the Status**, and it re-reads the board before acting on it |
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
| `gh` | 2.97.0 or later — `continuo doctor` reads the scopes from the default `gh auth status` output (there is no `--show-scopes` flag in 2.97.0) |
| `git` | any recent version — continuo runs it to create and remove worktrees |
| `ghq` | any recent version — continuo runs `ghq list -p -e` to locate each clone, and `ghq get` from `continuo trust` when one is missing |
| `jq` | only for the commands in [docs/trying_it_out.md](docs/trying_it_out.md) — continuo itself does not use it |
| Platform | macOS and Linux. **Windows is not supported natively — use WSL2** ([why](docs/plans/continuo_design.md)) |

Do not check these by hand. `continuo doctor` inspects every prerequisite — the config file, herdr, the `gh` login and its scopes, the board, the local clones, the folder trust, and the credentials — then prints what is missing and how to fix it. It exits `1` when something is missing and `0` when nothing is.

```bash
continuo doctor            # reads ./WORKFLOW.md
continuo doctor path/to/WORKFLOW.md
```

**揃っているかは手で確かめないでください。**`continuo doctor` が前提を全部検査し、足りないものと直し方を出します。足りないものがあれば終了コードは 1、無ければ 0 です。

## Trying it out / 試してみる

Walk one issue from `Ready` to `Done` and watch it happen: [docs/trying_it_out.md](docs/trying_it_out.md).
**No new board is created** — `continuo` attaches to the board you already have. It covers building,
checking that the existing `Status` field carries the five options, assigning them with
`continuo setup`, registering folder trust with `continuo trust` (**which clones any repository
that is missing locally**), and how to stop and clean up.
**Step 8 spends real Claude Code quota**, so read the steps first.

**1件の issue が `Ready` から `Done` まで通るのを実際に見る手順は [docs/trying_it_out.md](docs/trying_it_out.md) にあります。**
**ボードは新しく作りません。**いま使っているボードにそのまま足して使います。
ビルド・`Status` の確認・`continuo setup` での割り当て・`continuo trust` での信頼登録（**手元に無いリポジトリは
`ghq get` で取ってきます**）・止め方まで書いてあります。
**段8 から実際に Claude Code が動いて枠を消費する**ので、先に読んでください。

## Documents / 資料

| Path | Contents |
| --- | --- |
| [docs/trying_it_out.md](docs/trying_it_out.md) | How to run it end to end / 実際に動かして確かめる手順 |
| [docs/plans/continuo_design.md](docs/plans/continuo_design.md) | The design. Every decision and its evidence |
| [docs/plans/review/README.md](docs/plans/review/README.md) | The code review and what was fixed / レビューの結果と対応 |
| [docs/naming.md](docs/naming.md) | How the name was chosen |
| [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) | The symphony service specification (Apache-2.0, upstream copy) |
| [docs/evidence/](docs/evidence/) | Raw observations from the hook behaviour experiments |

## License

MIT — see [LICENSE](LICENSE).

One exception: [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) is a redistributed copy of the [openai/symphony](https://github.com/openai/symphony) specification, licensed by its authors under **Apache-2.0**. Its full license text is at [LICENSE-APACHE-2.0.txt](LICENSE-APACHE-2.0.txt), and the attribution is recorded in [NOTICE](NOTICE) and in the file's own header.
