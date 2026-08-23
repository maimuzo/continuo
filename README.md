# continuo

**[日本語版はこちら / Japanese version](README.ja.md)**

continuo is a development orchestrator that uses a GitHub Projects v2 board as a kanban.

Collect issues from multiple repositories onto a single board. Put one in the `Ready` column, and continuo prepares a git worktree and starts Claude Code in a [herdr](https://github.com/herdrdev/herdr) pane to work on it. When the work is done, continuo moves the Status, and the agent leaves a comment on the issue summarising what it did.

Written in Go, it implements the [openai/symphony](https://github.com/openai/symphony) service specification ([SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md), Apache-2.0).

![The kanban continuo drives](docs/images/board.png)

## Before you start

**The agent edits your repository, commits, and pushes.** continuo runs Claude Code without permission prompts and allows `Bash` without argument restrictions. **No confirmation dialog is shown.**

**Try it on a throwaway repository first.**

**continuo has not yet driven a single issue end to end on real hardware.**

> **The English documentation is not available yet.**
> **Please read [README.ja.md](README.ja.md) for now** — it covers what continuo does, what you need, how to install it, and how to run it.

## License

**MIT** — [LICENSE](LICENSE)
