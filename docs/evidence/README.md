# Evidence

Raw observations that the design in [../plans/continuo_design.md](../plans/continuo_design.md) rests on.

設計が根拠にしている実機観測の生ログです。

## Files

| File | What it is |
| --- | --- |
| `hooks_probe_20260817.jsonl` | Every hook payload Claude Code delivered during the 2026-08-17 experiment, one JSON object per line |
| `hooks_probe_settings.local.json` | The `.claude/settings.local.json` that registered those hooks |
| `hooks_probe.py` | The hook handler — writes stdin verbatim to the JSONL |
| `hooks_probe_permission_20261004.jsonl` | Every hook payload Claude Code delivered during the 2026-10-04 permission-prompt experiment (70 lines, 4 sessions), one JSON object per line |
| `hooks_probe_permission_settings.json` | The settings file passed with `--settings` for sessions 2 to 4 (session 1 used the same hooks with `permissions.allow` set to `Read` / `Glob` / `Grep`) |
| `push_u_origin_head.md` | Whether `git push -u origin HEAD` reaches the issue's branch from a continuo-shaped worktree — the script and its verbatim output |

**`push_u_origin_head.md` は git そのものの振る舞いだけを記録したものです。**remote はローカルの
bare repository で、**GitHub 側（認証・branch protection）は含みません。**その旨はファイルの冒頭にも書いてあります。

## How it was produced

A throwaway git repository was created under a scratch directory, two worktrees were cut from it, and `hooks_probe.py` was registered as the handler for nine hook events in the worktree's `.claude/settings.local.json`. Claude Code 2.1.233 was then launched in a herdr pane with that worktree as its working directory, and given two prompts: one that spawns a subagent, one that starts a background shell command. Everything was removed afterwards.

**使い捨ての git リポジトリから worktree を切り、その worktree の `.claude/settings.local.json` に9種類の hook を仕掛けて観測しました。**subagent を起動させるプロンプトと、バックグラウンドの shell を起動させるプロンプトを1つずつ送っています。検証環境は削除済みです。

## What it establishes

- `Stop` fires **while a subagent is still running** — the running work shows up in `background_tasks`
- Subagents (`"type": "subagent"`) and background shells (`"type": "shell"`) land in the **same** array, so there is no need to count them separately
- `background_tasks`, `stop_hook_active` and `agent_transcript_path` all exist in practice
- Hooks registered in a worktree's `.claude/settings.local.json` do fire, even though `.git` there is a file rather than a directory

> **注意。この記録は 2026-08-17 の観測だけを対象にしています。**
> **`background_tasks` が空の `Stop` は turn の途中にも発火することが、その後の実測で分かりました**
> （空の `Stop` 20件のうち4件）。**この記録だけでは turn の終わりを判定できません。**
> 最新の判定方法は [../plans/continuo_design.md](../plans/continuo_design.md) の 1-3 と 3-2 にあります。

## Sanitisation / サニタイズ

Two things were replaced before committing, because this repository is public:

| Field | Replacement |
| --- | --- |
| `CLAUDE_CODE_MESSAGING_TOKEN` | `<redacted: 32 hex chars>` |
| Session UUIDs (in `session_id`, `transcript_path`, `agent_transcript_path`, `CLAUDE_CODE_SESSION_ID`) | Sequential placeholder UUIDs — **the same original maps to the same placeholder everywhere**, so the correlation the design relies on is preserved |
| Absolute paths of the throwaway experiment directory | A placeholder. In `hooks_probe_settings.local.json` the hook command reads `<検証用ディレクトリ>/hooks_probe.py`, which is the file committed next to it |

Nothing else was altered. Timestamps, payload keys, ordering and message bodies are exactly as delivered.

**置換したのは上の3種類です。**設定ファイルが指すスクリプトの名前は、隣に置いてある `hooks_probe.py` と一致しています
（2026-08-18 に、食い違っていたものを修正しました）。パスの部分だけが伏せてあります。

**公開リポジトリなので、トークンとセッション UUID だけ置換しています。**UUID は同じものが同じ置換値になるので、「同じセッションで turn が続いている」という設計上の根拠は保たれています。それ以外は一切変えていません。

## 2026-10-04: 確認の画面が出たときの hook（issue #82）

**言いたいこと。**確認の画面で止まったとき、**道具の名前は `Notification` には入らず、`PreToolUse` と `PermissionRequest` に入る。**
記録は `hooks_probe_permission_20261004.jsonl` の70行である。行番号は1始まりで書く。

### どう測ったか

使い捨ての git リポジトリを1つ作り、`hooks_probe.py`（上と同じもの）を12種類の hook へ仕掛けた設定ファイルを
`claude --settings <ファイル> --permission-mode <モード>` で渡した。Claude Code 2.1.289 を herdr 0.9.1 の pane で起動し、
herdr が `blocked` を返したら `esc` を送った。`PreToolUse` / `PostToolUse` / `PostToolUseFailure` の matcher は `*` である。

| セッション | 行 | モード | `permissions.allow` | 何をさせたか |
| --- | --- | --- | --- | --- |
| 1 | 1〜21 | `default` | `Read` / `Glob` / `Grep` | 親に `Bash` で `touch` を1回。次に subagent に同じことを1回 |
| 2 | 22〜29 | `default` | `Bash` / `Read` / `Glob` / `Grep` / `Edit` / `Write` | 親に `Edit` を2回（`README.md` と `.claude/skills/sample/SKILL.md`） |
| 3 | 30〜48 | `dontAsk` | 同上 | 親に `Edit` を2回と `WebFetch` を1回 |
| 4 | 49〜70 | `auto` | 同上 | 親と subagent に `.claude/skills/sample/SKILL.md` の `Edit` を1回ずつ。次に親に `AskUserQuestion` を1回 |

### 何が分かったか

| 確かめたこと | 結果 | 行 |
| --- | --- | --- |
| `Notification` に道具の名前が入るか | **入らない。**キーは `session_id` / `transcript_path` / `cwd` / `scratchpad_dir` / `prompt_id` / `hook_event_name` / `message` / `notification_type` の8つで、`message` は4件とも `Claude needs your permission`、`notification_type` は4件とも `permission_prompt` だった | 5 / 14 / 28 / 69 |
| `Notification` に、親と subagent の別が入るか | **入らない。**subagent が出した確認（14行目）にも `agent_id` が無い | 14 |
| `Notification` はいつ届くか | **`PreToolUse` の 6.07〜6.10 秒後**（4/4）。herdr が `blocked` を返したのは `PreToolUse` から1秒以内だったので（4/4。返った時刻は秒までしか控えていない）、**`blocked` の時点ではまだ届いていない** | 3→5 / 10→14 / 26→28 / 67→69 |
| `PreToolUse` に道具の名前が入るか | **入る。**`tool_name` / `tool_input` / `tool_use_id` がある（17/17） | 3 ほか |
| 親と subagent を見分けられるか | **`PreToolUse` で見分けられる。**subagent が呼んだものには `agent_id` と `agent_type` が付き（3/3）、親が呼んだものには付かない（14/14） | 10 / 57 / 60 |
| `PermissionRequest` は何を運ぶか | 確認の画面が出るときだけ、`PreToolUse` の 0.03〜0.04 秒後に届いた（4/4）。`tool_name` / `tool_input` / `permission_suggestions` と、subagent なら `agent_id` / `agent_type` がある。**continuo はこの hook を張っていない** | 4 / 11 / 27 / 68 |
| `tool_input` に手元の絶対パスが入るか | **入る。**`Edit` の `file_path` は6件とも絶対パスだった。`Agent` の `prompt` にも入った（2/2） | 24 / 26 / 32 / 34 / 51 / 57 / 7 / 53 |
| `esc` で確認を取り消したあと | **その呼び出しの `PostToolUse` も `PostToolUseFailure` も `PermissionDenied` も `Stop` も届かなかった**（親で3/3）。subagent の確認を取り消したときは、その subagent の `SubagentStop` が約4秒後に届いた（1/1） | 5→6 / 28→29 / 69→70 / 15 |
| `dontAsk` で拒否されたとき | **`PreToolUse` だけが届き、対になる `PostToolUse` が来ない**（2/2）。`Notification` も `PermissionRequest` も `PermissionDenied` も `PostToolUseFailure` も0件で、herdr は `blocked` を返さなかった | 34 / 37 |
| `Edit` を許可していても `.claude/skills/` 配下の編集で確認が出るか | **`default` では出た**（1/1。同じセッションの `README.md` の編集は通った）。`dontAsk` では確認を出さずに拒否された（1/1）。`auto` では通った（親と subagent で 2/2） | 24〜28 / 34 / 51〜52 / 57〜58 |
| `AskUserQuestion` | **`auto` で確認の画面と同じ扱いになった**（1/1）。herdr は `blocked` を返し、`PermissionRequest` と `Notification`（`permission_prompt`）が届いた | 67〜69 |

**subagent の確認を `esc` で取り消すと、その subagent の記録にはこう残った**（1/1。`esc` を送ったのと同じ秒）。
issue #65 の報告にある文面と同じである。

```
Permission for this tool use was denied. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). Try a different approach or report the limitation to complete your task.
```

### 測っていないもの

- **`auto` の判定役が拒否したときの hook。**判定役に断られる操作を、検証用の Claude Code にさせていない
- **agent teams を有効にしたとき。**continuo は agent teams に対応していない（設計 3-70）
- **Claude Code 2.1.289 以外の版**
- **`PermissionDenied` と `PostToolUseFailure` が届く条件。**張ったが、70行に1件も無い

### 伏せたもの

上の「Sanitisation」と同じ3種類に加えて、次を置き換えた。**それ以外は届いたままである。**

| 何 | 置き換え |
| --- | --- |
| UUID の形をした値すべて（`session_id` のほか `prompt_id` も含む） | 連番の UUID。**同じ値は同じ置き換えになる** |
| 利用者名 | `<user>`（スキルの名前の一部に入っていたものも含む） |
| 一時ディレクトリの置き場所 | `/tmp/claude-<uid>` |
