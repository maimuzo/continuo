# Evidence

Raw observations that the design in [../plans/continuo_design.md](../plans/continuo_design.md) rests on.

設計が根拠にしている実機観測の生ログです。

## Files

| File | What it is |
| --- | --- |
| `hooks_probe_20260817.jsonl` | Every hook payload Claude Code delivered during the 2026-08-17 experiment, one JSON object per line |
| `hooks_probe_settings.local.json` | The `.claude/settings.local.json` that registered those hooks |
| `hooks_probe.py` | The hook handler — writes stdin verbatim to the JSONL |

## How it was produced

A throwaway git repository was created under a scratch directory, two worktrees were cut from it, and `hooks_probe.py` was registered as the handler for nine hook events in the worktree's `.claude/settings.local.json`. Claude Code 2.1.233 was then launched in a herdr pane with that worktree as its working directory, and given two prompts: one that spawns a subagent, one that starts a background shell command. Everything was removed afterwards.

**使い捨ての git リポジトリから worktree を切り、その worktree の `.claude/settings.local.json` に9種類の hook を仕掛けて観測しました。**subagent を起動させるプロンプトと、バックグラウンドの shell を起動させるプロンプトを1つずつ送っています。検証環境は削除済みです。

## What it establishes

- `Stop` fires **while a subagent is still running** — the running work shows up in `background_tasks`
- Subagents (`"type": "subagent"`) and background shells (`"type": "shell"`) land in the **same** array, so there is no need to count them separately
- `background_tasks`, `stop_hook_active` and `agent_transcript_path` all exist in practice
- Hooks registered in a worktree's `.claude/settings.local.json` do fire, even though `.git` there is a file rather than a directory

**したがって turn の終わりは「`Stop` が発火し、かつ `background_tasks` が空配列であること」で判定できます。**

## Sanitisation / サニタイズ

Two things were replaced before committing, because this repository is public:

| Field | Replacement |
| --- | --- |
| `CLAUDE_CODE_MESSAGING_TOKEN` | `<redacted: 32 hex chars>` |
| Session UUIDs (in `session_id`, `transcript_path`, `agent_transcript_path`, `CLAUDE_CODE_SESSION_ID`) | Sequential placeholder UUIDs — **the same original maps to the same placeholder everywhere**, so the correlation the design relies on is preserved |

Nothing else was altered. Timestamps, payload keys, ordering and message bodies are exactly as delivered.

**公開リポジトリなので、トークンとセッション UUID だけ置換しています。**UUID は同じものが同じ置換値になるので、「同じセッションで turn が続いている」という設計上の根拠は保たれています。それ以外は一切変えていません。
