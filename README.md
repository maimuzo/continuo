# continuo

[![ci](https://github.com/maimuzo/continuo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/maimuzo/continuo/actions/workflows/ci.yml)

**[日本語](README.ja.md)**

continuo turns a GitHub Projects v2 board into a work queue for coding agents. Drop an issue into `Ready` — from any of your repositories — and continuo picks it up, prepares a git worktree, and runs Claude Code on it inside a [herdr](https://github.com/herdrdev/herdr) pane. When the agent is done, the result comes back to the board as a Status change.

It is written in Go and implements the [openai/symphony](https://github.com/openai/symphony) service specification. You watch the work happen in a real terminal, on your usual Claude subscription.

---

## How the board drives it

Put your task in an issue, move it to `Ready`, and continuo takes it from there. If it lands in `Blocked`, the agent needs something from you — answer in an issue comment. If it lands in `In Review`, the work is done and waiting for you to look at it.

![The kanban board continuo drives](docs/images/board.png)

| Status | Who moves it | What is happening |
| --- | --- | --- |
| `Ready` | **You** | continuo picks these up in board order and starts them in herdr |
| `In Progress` | continuo | Work has started. A feature branch and a git worktree exist for this issue |
| `In Review` | continuo | The agent finished. Open the issue and check the result — **moving it to `Done` is yours to decide** |
| `Blocked` | continuo | The agent got stuck or needs an answer. Reply in an issue comment, then move it back to `Ready` |
| `Done` | **You** | continuo removes the worktree and the branch |

**Your Status names do not have to match.** `Ready` or `Todo`, `In Review` or `Needs review` — you map your own option names to these five roles once, with `continuo setup`.

## You can see what it is doing

Claude Code runs **in interactive mode, in a herdr pane** — not hidden in the background. Open herdr and watch it work, or read the pane from another terminal:

```bash
herdr agent read continuo-hello-world-188 --source recent-unwrapped --lines 40
```

**No metered API calls.** continuo never uses `claude -p`, the Agent SDK, or the HTTP API. It keeps one interactive session per issue and sends follow-up turns into it, so context accumulates across turns the same way it does when you drive Claude Code yourself.

Start it with `continuo --port 8080` and you get a plain list of what is running at `http://localhost:8080`.

## One board, many repositories

A single board can hold issues from as many repositories as you like. continuo creates a worktree per issue, under the repository it belongs to:

```
~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188/
~/worktrees/github.com/octocat/sample-app/continuo-octocat-sample-app-42/
```

How many issues run at once is a setting (two by default).

## Before you start

**The agent edits your repository, commits, and pushes.** continuo starts Claude Code with permission prompts turned off and allows `Bash` without argument restrictions. Nothing will stop and ask you.

**Issue text is agent instructions.** The default brief tells the agent to run `gh issue view <URL> --comments` and read everything — body and comments alike. **Whatever is written there can execute on your machine, with no prompt.**

**On a public repository, that text is written by other people.** Anyone can open an issue or leave a comment. **If it says "delete this repository", that is what runs.**

| Mitigation | What to do |
| --- | --- |
| **Only advance issues you wrote** | A human moves things into `Ready`. Do not move an issue you did not read |
| **Filter by label** | Set `tracker.required_labels` so only issues carrying your marker are eligible |
| **Isolate it** | Run it under a dedicated account, or on a machine or container you can discard |

**Try it on a repository you can throw away.** Do not point it at production work on day one.

## Requirements

| | |
| --- | --- |
| OS | macOS or Linux. **No native Windows** — use WSL2 |
| [herdr](https://github.com/herdrdev/herdr) | The daemon that owns the panes and worktrees. continuo drives Claude Code through it. **Verified against 0.8.0** (it refuses to start on a socket protocol mismatch) |
| [Claude Code](https://claude.com/claude-code) | Used on a **subscription plan**. Verified against 2.1.233 |
| [`gh`](https://cli.github.com/) | Signed in with `gh auth login -s project`. Verified against 2.97.0 |
| [`git`](https://git-scm.com/) and [`ghq`](https://github.com/x-motemen/ghq) | Creating worktrees, and resolving where a clone lives |
| [Go](https://go.dev/dl/) 1.26+ | Only if you build from source |

**Your board needs five Status options.** GitHub gives you three by default (`Todo`, `In Progress`, `Done`), so **add the missing two from the GitHub UI**: open the board's `Settings`, pick `Status` under `Custom fields`, then `Add option...`. The names are up to you — `continuo setup` maps them to roles afterwards.

`continuo doctor` runs eight checks: config, Claude Code, herdr, `gh` auth, board, clones, trust, and credentials (used to read your plan's usage window). It does **not** check your OS or Go version — that part is on you.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh
```

The installer detects your OS and architecture, pulls the matching binary from [releases](https://github.com/maimuzo/continuo/releases), verifies its checksum, and puts it in `~/.local/bin/continuo`. If `git`, `gh`, or `ghq` is missing, it asks you one at a time and shows the exact command it would run. **It never installs `herdr` or `claude`** — both have their own distribution and sign-in flows, so it points you at them instead.

| Option | Effect |
| --- | --- |
| `--yes` | Answer yes to every prompt, including package installs |
| `--no-deps` | Install nothing but continuo; just list what is missing |
| `--dir DIR` | Install somewhere else (default `~/.local/bin`) |
| `--version V` | Install a specific release instead of the latest |
| `--repo O/R` | Install from a fork. **Using it prints a warning** |

**It stops if it cannot verify the checksum.** Pass `--insecure-no-checksum` to go ahead anyway.

**A checksum alone does not detect tampering** — it ships from the same release as the archive, so anyone who can replace one can replace both. **GitHub's signed build provenance is the stronger check**, and the installer runs it automatically when `gh` is available. To check it yourself:

```bash
gh attestation verify continuo_darwin_arm64.tar.gz --repo maimuzo/continuo
```

Prefer to build it yourself? You need Go 1.26:

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
mise trust && mise install                       # if you manage Go with mise
go build -o ~/.local/bin/continuo ./cmd/continuo
sh scripts/test-like-ci.sh                       # run the tests (~3 min, optional)
```

## Use

**continuo does not create a board.** It attaches to the one you already use.

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init      # writes WORKFLOW.md; owner and board number come from gh
```

**Open `WORKFLOW.md` before you go further.** `trust.repositories` lists every repository it found on the board. Delete the lines you do not want — otherwise Claude Code gets trusted access to repositories that have nothing to do with this.

```bash
continuo setup                    # map your Status options to the five roles (interactive)
continuo trust --dry-run          # show what would be trusted, without doing it
continuo trust                    # trust those repositories; clone them if needed
continuo allow-keychain-access    # macOS only, once — lets continuo read your plan's usage
continuo doctor                   # check that everything is in place

continuo                          # start the daemon
```

From here on, moving an issue to `Ready` is all it takes.

**`Ctrl+C` stops it.** It stops polling and unwinds its turn loops, but **it does not close the panes** — Claude Code keeps running, and the next start picks those panes back up and continues.

### Configuration

`continuo init` writes `WORKFLOW.md`. That single file is both the config and the brief you send to the agent.

The front matter at the top is the configuration. These four are the ones you will actually touch:

```yaml
tracker:
  provider:
    owner: octocat                # your GitHub account
    project_number: 3             # the board number
agent:
  max_concurrent_agents: 2        # issues running at once
claude:
  turn_timeout_ms: 3600000        # give up after this long with no change on screen
```

`turn_timeout_ms` is **not** a cap on how long a turn may take. As long as the pane keeps changing, a single instruction can run for hours.

Everything below the front matter is the first prompt sent to Claude Code — "write `CONTINUO-STATUS: review` when you are done", "commit and push before that", and so on. **Rewrite it to match how your project works.**

Restart continuo after editing it. It does not reload while running.

## How it works

continuo asks herdr to send a prompt and wait; herdr watches the pane and returns once the agent goes `idle`, `done`, or `blocked`. It talks to herdr over its socket directly, in JSON — it does not shell out to the `herdr` command.

1. continuo starts an issue and sets its Status to `In Progress`
2. It creates a worktree, starts Claude Code in a herdr pane, and sends the prompt
3. **herdr reports that the agent has settled**
4. **Claude Code's `Stop` hook confirms it.** The turn is over once `background_tasks` is empty and nothing new starts for a few seconds
5. continuo reads the transcript; a `CONTINUO-STATUS:` line moves the Status
6. If the issue is still in a working state, it sends the next turn (up to `agent.max_dispatch_turns`, 20 by default)

**The board is the only source of truth.** An agent saying it is finished means nothing until the Status has actually moved.

## Project status

**One issue has now been driven end to end on real hardware** — picked up from `Ready`, worked on in a herdr pane, moved to `Done`, and the worktree and branch cleaned up. The walkthrough for trying it yourself is in [docs/trying_it_out.md](docs/trying_it_out.md).

**While it is on v0.x, the configuration format may change.** The front matter in `WORKFLOW.md` rejects unknown keys, so **removing or renaming one will stop older config files from starting.** Any such change goes in the release notes.

**Everything except this file is in Japanese** — the installer, the CLI output, `continuo doctor`, the error messages, and all documentation. There is no English UI yet, and a half-translated one would be worse than none: you would get English and Japanese in the same screen. If you do not read Japanese, this is not usable for you today.

## Learn more

| | |
| --- | --- |
| **Why it is built this way** (start here) | [docs/plans/continuo_design_slim.md](docs/plans/continuo_design_slim.md) (634 lines) |
| The full record: reasoning, measurements, rejected alternatives | [docs/plans/continuo_design.md](docs/plans/continuo_design.md) (nearly 4,800 lines) |
| Use case specifications (RUCM) | [docs/spec/usecases/](docs/spec/usecases/) |
| **Development and testing** | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Third-party software in the binary | [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) |

## The specification it follows

symphony is OpenAI's published specification for coding agent orchestrators (Apache-2.0). continuo implements it. The text lives at [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) and is **not vendored into this repository**.

The specification assumes an orchestrator talking to Codex's app-server over stdio. continuo substitutes Claude Code running in a herdr pane. The `MUST` clauses it cannot honour, and what it does instead, are listed under "8. symphony の仕様と異なるところ" in [docs/plans/continuo_design.md](docs/plans/continuo_design.md).

## License

**MIT** — see [LICENSE](LICENSE).
