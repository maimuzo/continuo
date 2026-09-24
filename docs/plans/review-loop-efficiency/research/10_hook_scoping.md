# continuo から起動したセッションだけ、返答検査の hook を止める — 実現パターンの比較

**この文書は比較資料である。決めるのは人間で、ここでは推奨までしか書かない。**

**この文書が名指しする3本の hook は、いずれもリポジトリから消えている。**
`check-reply-clarity.py` と `check-verified-commands.py` は 2026-09-20 にプラグインへ移し、
`block-merge-without-review.py` は 2026-09-21 に廃止した。**リンクは外した。**

調査日: 2026-09-16。対象 commit: `df36f9d7`（worktree `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency`、branch `docs/review-loop-efficiency`）。

---

## 0. 前置きを読んだうえでの4つの答え

**1. この作業は、どの規則に当てはまるか。**

| 規則 | どう効くか |
| --- | --- |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「不特定多数の環境と、maimuzo の環境を混同しない」 | **この調査の中心軸である。**返答を検査する hook 3本は maimuzo の環境のものであり、continuo の利用者（不特定多数）は1本も持っていない。パターンごとに「maimuzo の環境だけで済むか / continuo の全利用者に及ぶか」を分けて書いた（下の 5-2） |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「公開してよい情報かを常に判断する」 | 成果物は公開リポジトリの worktree に置かれる。個人の絶対パスは `~/` から書き、トークンは書かない |
| [.claude/rules/plan-file.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md) の「何かを書くと書いたら、パスと中身のサンプルを必ず添える」 | 設定を書くパターンには、実際のパスと JSON / YAML の実物を添えた |
| [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) の「案を並べる表は、列を『選ぶと何が起きるか』にする」 | 比較表は「選ぶと何が起きるか」と「選ばないと続くこと」を持たせた |

**2. 飛ばしてよい段はあるか。**

**無い。**ただし、この作業は**調査だけ**であり、設計でも実装でもない。
[.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) の9段は、**人間がパターンを選んだあとに段1（設計を固める）から始まる。**
いまはその前段の材料集めなので、段2以降には入らない。

**3. 公開してよくない情報を、成果物に書きうる場面はあるか。**

**ある。3箇所で当たった。**

| どこで | どう処理したか |
| --- | --- |
| hook スクリプトの置き場所 | `~/.claude/plugins/marketplaces/maimuzo-marketplace/plugins/…` と `~/` から書いた |
| worktree の置き場所 | `~/worktrees/github.com/<owner>/<repo>/…` と書いた。実在の worktree 8本の一覧は載せない |
| プラグインの一覧 | **`maimuzo-chat-response` だけを名指しした。**ほかに有効化されている16個は、この件に関係が無いので書かない |

**4. 作業に効く場所をどう探し、何を読んだか。**

| 叩いたもの | 何が出たか |
| --- | --- |
| `git grep -n "CONTINUO_" internal/` | continuo が使う環境変数は `CONTINUO_RUNTIME_DIR` と `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` の2本だけ。**自分が起動したことを名乗る環境変数は1本も無い** |
| `grep -n "environ\|getenv" <hook 3本>` | 3本とも、読んでいる環境変数は診断の切り替えだけ（下の 3-2） |
| `grep -n "disableAllHooks\|settings\.local\|enabledPlugins\|worktree_local" docs/plans/continuo_design.md` | 設計 3-12 が「worktree に settings.local.json を置く」案を**明示的に却下している**（下の 6-5） |
| `gh issue list --state all --limit 200` を hook / プラグインで絞った | 近いものは #129（返答を検査する hook のやり直しが1セッションで210回。根本対策を入れる）と #166（応答が hook に差し戻されたのに、continuo は turn が終わったと判断して次の指示を送る）。**この件そのものの issue は無い** |
| 公式文書7ページ | 下の 4 に一覧 |

**読めなかったもの。**組織の managed settings（OS ごとに場所が違い、この環境に在るかを確かめていない）。

---

## 1. 問題の定義

**何が起きているか。**
maimuzo の環境では、Claude Code の turn が終わる直前に、返答の形を検査する `Stop` hook が **3本**動く。
検査に落ちると hook が `{"decision":"block"}` を返し、**エージェントは turn を終えられずに返答を書き直す。**
この3本は、人間がチャットで読む返答のためのものである。
**ところが continuo が起動したセッションでも、同じ3本がそのまま動く。**
そちらの返答を読む人間は居ない（無人で回している）ので、**書き直しはレートリミットの枠を消費するだけで、誰の役にも立たない。**

**なぜそれが困るか。**
書き直しは1回につき turn を1つ増やす。実測値として、[.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) は
「2026-08-31 からの5日間・18本のセッションで261回のやり直しがあり、そのうち186回（71%）がこの検査だった」と記録している。
**無人のセッションでその割合が出ると、issue 1件あたりの枠の消費がそのぶん増える。**

**いま何を決めるのか。**
**下の5から8のパターンのうち、どれを実装するかを人間が選ぶ。**この文書では選ばない。

---

## 2. 人間が出した要求（原文）

> また、hookを使ったプラグインは副作用も強い。たとえばcontinuoから呼び出されたセッション内では、基本的に無人で運用するんだから、チャット応答の内容をhookで確認して訂正させるような仕組みは無駄なコストでしか無い。よって、プラグイン単位もしくはスキル単位でcontinuoからのセッションでは無効化させるような仕組みが欲しい。
> 具体的には、claude codeを直接起動して会話している場合はhookで応答フォーマットを強制させるようにし、並行して同じディレクトリ上でcontinuoから起動されたセッションではhookを無効化するような仕組みを検討して。実現パターンは色々ありそうなので、比較して人間が選択できるようにして。

**「並行して同じディレクトリ上で」が、この比較のいちばん重い軸である。**
ディレクトリを見て判定するパターンは、この条件を満たせない（下の 5-1 の表）。

---

## 3. いま動いているもの（実測）

### 3-1. 返答を検査する `Stop` hook は3本。出どころが2つに割れている

| 何 | どこに書いてあるか |
| --- | --- |
| **返答の5段構成を検査する**（プラグイン） | `~/.claude/plugins/marketplaces/maimuzo-marketplace/plugins/maimuzo-chat-response/hooks/hooks.json` |
| **引用80文字・名札・カテゴリの名乗りを検査する**（リポジトリ） | [.claude/settings.json](../../../../.claude/settings.json) が `.claude/hooks/check-reply-clarity.py` を張る |
| **検証していないコマンドの報告を検査する**（リポジトリ） | 同じ [.claude/settings.json](../../../../.claude/settings.json) が `.claude/hooks/check-verified-commands.py` を張る |

**プラグインの hook 定義の原文**（`hooks.json` の全文）。

```json
{
  "description": "返答の5段構成を turn の終了直前に検査する Stop hook。",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 \"${CLAUDE_PLUGIN_ROOT}/hooks/check-reply-structure.py\"",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
```

**この「出どころが2つ」が、パターンの効き方を分ける。**
プラグインの1本は「プラグインを切る」だけで止まるが、
**リポジトリの2本はプラグインではないので、プラグインを切っても止まらない。**

**数えた件数・検索パターン・範囲。**
`git grep -n '"Stop"' -- '.claude/'` を `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency` で叩き、**3件**。
うち2件（`.claude/hooks/check-reply-clarity.py:37` と
`.claude/hooks/check-verified-commands.py:25`）は
**docstring の中の設置例**であって、実際に張っているのは [.claude/settings.json](../../../../.claude/settings.json) の1件だけである。

### 3-2. 3本とも、無人かどうかを判定する材料を1つも読んでいない

`grep -n "environ\|getenv" <3本>` の結果、読んでいる環境変数は次の3つだけで、**どれも診断の切り替えである。**

| スクリプト | 読んでいる環境変数 | 何のためか |
| --- | --- | --- |
| `.claude/hooks/check-reply-clarity.py:159` | `REPLY_CLARITY_HOOK_DEBUG` | stderr へ traceback を出す |
| `.claude/hooks/check-verified-commands.py:114` | `CLAUDE_HOOK_DEBUG` | 同上 |
| `check-reply-structure.py:98`（プラグイン） | `CHAT_RESPONSE_HOOK_DEBUG` | 同上 |

**`CLAUDE_PROJECT_DIR` を読んでいる箇所が1つある**
（`.claude/hooks/check-reply-clarity.py:466`）が、
これは issue の題名を引くための `.git` の在りかを求めるもので、無人かどうかの判定ではない。

**3本に共通する早期の抜け道は2つだけである。**

| 条件 | 何が起きるか |
| --- | --- |
| `stop_hook_active` が真 | **何もせず 0 で抜ける**（差し戻しの無限ループの防止）。`check-reply-clarity.py:984`、`check-verified-commands.py:302`、`check-reply-structure.py:291` |
| コードフェンスを除いた散文が200文字未満 | **検査せず 0 で抜ける。**`check-reply-clarity.py:90` の `MIN_LEN_FOR_CHECK = 200`、`check-reply-structure.py:35` の `MIN_LEN_FOR_STRUCTURE = 200` |

**つまり、いま無人のセッションを見分ける材料は1バイトも入っていない。**
**検索パターン**: `environ|getenv|CLAUDE_|CONTINUO`。**対象パス**: 上の3本。**対象コミット**: `df36f9d7`。

### 3-3. continuo は Claude Code へ `--settings` で設定ファイルを渡し、その中に `env` を書ける

**ここが、この件でいちばん効く既存の仕組みである。**

[internal/orchestrator/settings.go:425](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L425) が起動フラグを組み立てる。

```go
args := []string{"--settings", settingsPath}
```

そのファイルの中身は [internal/orchestrator/settings.go:326-333](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L326-L333) の docstring にサンプルがある。

```json
{
  "hooks": { "Stop": [{"hooks":[{"type":"command",
              "command":"'/usr/local/bin/continuo' hook --socket '/…/hooks.sock' --pending-dir '/…/pending'"}]}] },
  "permissions": { "allow": ["Bash","Read","Glob","Grep","Edit","Write"], "deny": ["AskUserQuestion"] },
  "env": { "CLAUDE_CODE_RETRY_WATCHDOG": "1" }
}
```

置き場所は `<実行時ディレクトリ>/issues/<issue のスラグ>/settings.json` で、
**worktree の外である**（[internal/orchestrator/settings.go:18-23](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L18-L23)）。

`env` の中身は、利用者が WORKFLOW.md に書いた `claude.env` がそのまま入る
（[internal/config/types.go:457-458](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/types.go#L457-L458)。
[internal/orchestrator/settings.go:393](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L393) が `Env: o.cfg.Claude.Env` で載せる）。
雛形の既定値は [internal/scaffold/template.go:148-149](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/scaffold/template.go#L148-L149) にある。

```yaml
  env:                                      # Claude Code に渡す環境変数
    CLAUDE_CODE_RETRY_WATCHDOG: "1"         # turn の途中で 429 / 529 が返ってきたときに、リトライを続けさせる
```

**つまり、利用者が WORKFLOW.md を1行足すだけで、continuo が起動したセッションにだけ環境変数を届けられる。**
**continuo のコードを1行も変えずに。**

### 3-4. continuo が張る hook は8種類。`Stop` を含む

[internal/orchestrator/settings.go:93-107](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L93-L107) が並べている。

`Stop` / `UserPromptSubmit` / `SubagentStop` / `SubagentStart` / `Notification` / `SessionStart` /
`PreToolUse`（matcher は `*`）/ `PostToolUse`（matcher は `*`）の8つ。

**このうち `Stop` が turn の終わりを知る唯一の経路である。**
だから「`Stop` hook を全部止める」パターンは、**continuo 自身を黙らせることになる**（下の 6-4）。

### 3-5. worktree の置き場所と、そこに何が在るか

| 何 | 値 |
| --- | --- |
| worktree の根 | `~/worktrees`。中の並べ方は `<root>/<ホスト>/<owner>/<repo>/<branch>` に固定（[internal/scaffold/template.go:103-105](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/scaffold/template.go#L103-L105)） |
| 身元ファイル | `<worktree>/.continuo.json`（[internal/config/default.go:147](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/default.go#L147) の `IdentityFile: ".continuo.json"`） |

**`.claude/settings.json` は git の追跡下にある**（`git ls-files .claude/` に出る）。
**つまり continuo が切る worktree にも、この2本の hook 定義がそのまま入る。**
一方 `.claude/settings.local.json` は `.gitignore` 済みで、
**実測でも worktree には1つも無かった**（`find ~/Sources/github/continuo/.claude/worktrees -maxdepth 3 -name "settings.local.json"` が0件）。
設計も同じことを書いている（[docs/plans/continuo_design.md:7479](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L7479)）。

> | 対象リポジトリの `.claude/settings.local.json` | 読まない | gitignore されるので worktree に出てこない |

---

## 4. 公式文書で確かめたこと

**読んだページ**（いずれも 2026-09-16 に取得）。

| ページ | URL |
| --- | --- |
| Hooks reference | https://code.claude.com/docs/en/hooks |
| Automate actions with hooks | https://code.claude.com/docs/en/hooks-guide |
| Settings files and precedence | https://code.claude.com/docs/en/settings |
| All settings | https://code.claude.com/docs/en/settings-reference |
| CLI reference | https://code.claude.com/docs/en/cli-reference |
| Create plugins / Discover plugins | https://code.claude.com/docs/en/plugins ・ /discover-plugins |
| Skills | https://code.claude.com/docs/en/skills |

### 4-1. hook をセッション単位で止める公式の手段は `disableAllHooks` の1つだけ。しかも全部が止まる

https://code.claude.com/docs/en/hooks の "Disabling Hooks" の原文。

> To temporarily disable all hooks without removing them, set `"disableAllHooks": true` in your settings file. Claude Code reads the value left after settings precedence applies, so a `"disableAllHooks": false` in a project's `.claude/settings.json` overrides a `true` in your user settings. To turn hooks off for one run whatever the project's settings say, pass `--settings '{"disableAllHooks": true}'`, which takes precedence over project and local settings. **There is no way to disable an individual hook while keeping it in the configuration.**

**訳。**
> hook を消さずに一時的に全部止めるには、設定ファイルへ `"disableAllHooks": true` を書く。
> Claude Code は設定の優先順位を当てたあとに残った値を読むので、
> プロジェクトの `.claude/settings.json` の `"disableAllHooks": false` が、利用者の設定の `true` を上書きする。
> プロジェクトの設定が何であれ1回の実行だけ hook を止めたいなら、`--settings '{"disableAllHooks": true}'` を渡す。
> これはプロジェクトとローカルの設定より優先される。
> **設定に残したまま個別の hook だけを無効にする方法は無い。**

**最後の1文が決定的である。**「プラグイン単位もしくはスキル単位で」という人間の要求に、
**hook 側の公式の仕組みは1つも応えていない。**

### 4-2. `--setting-sources` は無い

**検索パターン**: `--setting-sources`。**対象**: https://code.claude.com/docs/en/cli-reference（2026-09-16 取得）。
フラグ表を `--session-id` の前後まで読ませたうえで、返答は
「**No**, a flag named `--setting-sources` does not exist in this documentation.」であった。
**オーケストレーターの記憶にあった候補のうち、これだけが実在しない。**

`--settings` は実在し、次の説明である。

> Load settings from a JSON file or string (overrides saved settings for this session only). Accepts file paths ending in `.json` or `.jsonc`, JSON strings starting with `{`, and environment variable references like `$VAR_NAME`.

**訳。**
> JSON のファイルか文字列から設定を読む（そのセッションだけ、保存済みの設定を上書きする）。
> `.json` / `.jsonc` で終わるファイルのパス、`{` で始まる JSON の文字列、`$VAR_NAME` のような環境変数の参照を受け付ける。

**continuo が渡しているのはファイルのパスの形である。**

### 4-3. `--settings` は上から2段目。プロジェクトの設定より強い

https://code.claude.com/docs/en/settings の優先順位の原文（高い順）。

> 1. **Managed settings** … 2. **Command line arguments**: flags you pass when you start `claude` from a terminal, for one session … Claude Code merges JSON you pass with `--settings <file-or-json>` with your settings files by the same rules as the other levels: it takes a key you set here over the same key in local, project, or user settings, and keeps the lower-level value for a key you omit. 3. **Project local settings** (`.claude/settings.local.json`) 4. **Shared project settings** (`.claude/settings.json`) 5. **User settings** (`~/.claude/settings.json`)

**訳（2段目だけ）。**
> **コマンドライン引数**: 端末から `claude` を起動するときに渡すフラグ。1つのセッションにだけ効く。
> `--settings <ファイルか JSON>` で渡した JSON は、ほかの段と同じ規則で設定ファイルと併合される。
> **ここで設定したキーは、ローカル・プロジェクト・利用者の設定の同じキーより優先される。**
> **書かなかったキーは、下の段の値がそのまま残る。**

同じページの別の箇所。

> **`--settings`**: pass a key as JSON, inline or as a path to a file. Claude Code applies it above your user, project, and local files and below managed settings. **It can set any key your user settings file can set**; it can't set `Managed` or `Global config` keys.

**訳。**
> **`--settings`**: キーを JSON で渡す。インラインでも、ファイルのパスでもよい。
> Claude Code はそれを利用者・プロジェクト・ローカルのファイルより上、managed settings より下に当てる。
> **利用者の設定ファイルが設定できるキーなら、何でも設定できる。**

**この1文が、下の 6-3（プラグインを1つ切る）を成り立たせている。**

### 4-4. hook は出どころをまたいで全部走る。上書きではない

https://code.claude.com/docs/en/hooks-guide の原文2箇所。

> When an event fires, Claude Code runs all matching hooks in parallel

> When multiple hooks match the same event, every hook's command runs to completion before Claude Code merges the results. One hook returning `deny` doesn't stop sibling hooks from executing.

**訳。**
> イベントが発火すると、Claude Code は**一致する hook を全部、並列で走らせる。**

> 同じイベントに複数の hook が一致したとき、**Claude Code が結果を併合する前に、どの hook のコマンドも最後まで走る。**
> 1つの hook が `deny` を返しても、兄弟の hook の実行は止まらない。

同じページの優先順位の節が、リストのキーについてこう書いている。

> When you set the same list key, such as `permissions.allow`, in more than one file, Claude Code combines the lists instead of picking one

**訳。**
> `permissions.allow` のような同じリストのキーを複数のファイルに書いたとき、
> Claude Code はどれか1つを選ぶのではなく、**リストを結合する。**

**帰結。continuo が `--settings` で渡す `hooks.Stop` は、リポジトリの `.claude/settings.json` の `hooks.Stop` を上書きしない。**
**両方が走る。**だからいま、continuo のセッションでも返答検査の3本が動いている。
**そして「`hooks` に空配列を書いて打ち消す」こともできない。**結合されるだけである。

### 4-5. hook が受け取る JSON に、無人かどうかを名乗るフィールドは無い

https://code.claude.com/docs/en/hooks の共通フィールドの表から、判定に使えそうなものだけ引く。

| フィールド | 原文の説明（抜粋） | 判定に使えるか |
| --- | --- | --- |
| `session_id` | "Current session identifier" | **使えない。**continuo が採番した UUID かどうかを hook 側が知る手段が無い |
| `cwd` | "Current working directory when the hook is invoked" | **条件つき。**ディレクトリが違うときだけ。同じディレクトリでは区別できない |
| `transcript_path` | "Path to conversation JSON." | **使えない。**保存先はプロジェクトごとで、起動元では分かれない |
| `permission_mode` | "`"default"`, `"plan"`, `"acceptEdits"`, `"auto"`, `"dontAsk"`, or `"bypassPermissions"`" | **条件つき。**下の 6-8 |
| `agent_id` / `agent_type` | "Present only when the hook fires inside a subagent call." | **使えない。**メインと subagent の区別であって、起動元の区別ではない |

**`source` というフィールドは、共通フィールドの表に無い。**
（`SessionStart` の個別のイベントには在るが、`Stop` の共通フィールドには無い。
**検索パターン**: `source`。**対象**: https://code.claude.com/docs/en/hooks の "Common Input Fields" の表）

### 4-6. 設定ファイルの `env` は、hook のようなサブプロセスにも届く

https://code.claude.com/docs/en/settings-reference の `env` の項。

> Set environment variables for every session **and its subprocesses**

**訳。**
> すべてのセッション**と、そのサブプロセス**に環境変数を設定する。

https://code.claude.com/docs/en/hooks の hook のコマンドに渡る環境の説明。

> A hook process inherits the parent environment, apart from the `OTEL_*` exporter variables that Claude Code removes from every subprocess it spawns and, when `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` is set to `1`, the variables it strips.

**訳。**
> **hook のプロセスは親の環境を継ぐ。**ただし Claude Code が起動するすべてのサブプロセスから取り除く `OTEL_*` の書き出し用の変数と、
> `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` が `1` のときに削る変数は除く。

**この2つで、`claude.env` → 設定ファイルの `env` → hook スクリプトの `os.environ` という経路が繋がる。**

### 4-7. `enabledPlugins` と `skillOverrides` は実在する

| キー | 公式の説明（原文） | 訳 |
| --- | --- | --- |
| `enabledPlugins` | "Turn individual plugins on or off per scope" | **スコープごとに、個々のプラグインを on / off にする** |
| `skillOverrides` | "Hide or collapse a skill without editing its SKILL.md" | **SKILL.md を編集せずに、スキルを隠す / 畳む** |

どちらも Scope は **"Any file"**（利用者・プロジェクト・ローカル・managed のどれにでも書ける）。
4-3 のとおり `--settings` は「利用者の設定ファイルが設定できるキーなら何でも設定できる」ので、
**continuo の設定ファイルにも書ける。**

`skillOverrides` の値は4つ（https://code.claude.com/docs/en/skills）。

| 値 | Claude への一覧 | `/` のメニュー |
| --- | --- | --- |
| `"on"` | 名前と説明 | 出る |
| `"name-only"` | 名前だけ | 出る |
| `"user-invocable-only"` | 隠す | 出る |
| `"off"` | 隠す | 隠す |

**ただし `skillOverrides` は hook を止めない。**
スキルが frontmatter に書いた hook は、そのスキルが呼ばれたときに登録されるものであり
（https://code.claude.com/docs/en/hooks の設置場所の表: "The rest of the session once the skill is invoked"）、
**今回の3本はどれもスキルの frontmatter に書かれていない**（3-1 のとおり、プラグインの `hooks.json` と `.claude/settings.json` である）。
**つまり「スキル単位で無効化」は、この3本には効かない。**

### 4-8. プラグインの hook だけを止めることはできない。プラグインごと切るしかない

https://code.claude.com/docs/en/discover-plugins の原文。

> When you uninstall a plugin that a project's `.claude/settings.json` enables, Claude Code asks which scope you mean: disable it for you alone, **which writes an override to your `.claude/settings.local.json`** and leaves the plugin installed for the project, or uninstall it for everyone

**訳。**
> プロジェクトの `.claude/settings.json` が有効にしているプラグインをアンインストールすると、
> Claude Code はどのスコープのことかを訊く。**自分だけ無効にする**なら
> **`.claude/settings.local.json` へ打ち消しを書き**、プロジェクトにはプラグインを残す。全員のぶんを消すなら…

**プラグインの構成要素（skills / agents / hooks / MCP）を選んで切る仕組みは、文書に無い。**
**検索パターン**: `disable` + `hooks` の組み合わせ。**対象**: https://code.claude.com/docs/en/plugins ・ /discover-plugins ・ /settings-reference。
**プラグイン単位の on / off が最小の粒度である。**

いま maimuzo の環境で使われている書き方は、`~/Sources/github/continuo/.claude/settings.local.json` の
`enabledPlugins` にある形である（17個が `true`。そのうち1つが `"maimuzo-chat-response@maimuzo-marketplace": true`）。

---

## 5. パターンの比較

**8つ挙げた。**上の4つが実用の候補、下の4つは却下の根拠つきである。

### 5-1. 何をするか と、同じディレクトリで区別できるか

**この表がいちばん重い。**人間の要求「並行して同じディレクトリ上で」に答えるのはこの列である。

| どうするか | 何をするか | 同じディレクトリで2つのセッションを区別できるか |
| --- | --- | --- |
| **無人の印を環境変数で渡し、hook スクリプトが先頭で抜ける** | WORKFLOW.md の `claude.env` に印を1行足す。3本の hook スクリプトの先頭に「その印が立っていたら 0 で抜ける」を足す | **できる。**環境変数はセッションごとに渡る。continuo が `--settings` で渡したセッションにだけ入り、人間が直接起動した `claude` には入らない |
| **issue ごとの設定ファイルで、プラグインを1つ切る** | continuo が書く設定ファイルへ `"enabledPlugins": {"maimuzo-chat-response@maimuzo-marketplace": false}` を足す | **できる。**`--settings` は「そのセッションだけ」に効く（4-2 の原文 "for this session only"） |
| **hook の入力の `permission_mode` を見る** | hook スクリプトが `permission_mode` を読み、`auto` なら抜ける | **できない。**人間も `auto` を使える。下の 6-8 |
| **worktree の `.continuo.json` を hook スクリプトが見て抜ける** | hook スクリプトが `cwd` の直下に身元ファイルが在るかを見る | **できない。**同じディレクトリなら人間のセッションでも在る |
| **worktree に `.claude/settings.local.json` を置いてプラグインを切る** | continuo が worktree の中へ打ち消しの設定を書く | **できない。**ディレクトリに紐づくので、同じ場所の人間のセッションにも効く |
| **リポジトリの2本をプラグインへ移し、プラグイン単位で切る** | `.claude/settings.json` から hook を外し、新しいプラグインへ入れる。continuo の設定ファイルで2つとも切る | **できる。**上の「プラグインを1つ切る」と同じ経路になる |
| **issue ごとの設定ファイルで hook を全部切る**（`disableAllHooks`） | continuo が書く設定ファイルへ `"disableAllHooks": true` を足す | **できる。だが continuo 自身の hook も死ぬ。**下の 6-4 |
| **起動オプションで拡張を全部切る**（`--safe-mode` / `--bare`） | continuo が起動フラグを1本足す | **できる。だが continuo 自身の hook も死ぬ。**下の 6-7 |

### 5-2. 変えるものと、影響範囲

**「maimuzo の環境だけか、continuo の利用者（不特定多数）にも及ぶか」を必ず見ること**
（[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「不特定多数の環境と、maimuzo の環境を混同しない」）。

| どうするか | 変えるもの | 影響範囲 |
| --- | --- | --- |
| **無人の印を環境変数で渡し、hook スクリプトが先頭で抜ける** | **WORKFLOW.md 1行 ＋ hook スクリプト3本。continuo の Go のコードは0行** | **maimuzo の環境だけ。**continuo の利用者は hook を持っていないので、何も変わらない |
| **issue ごとの設定ファイルで、プラグインを1つ切る** | **WORKFLOW.md に新しい設定キーが要る**（`claude.enabled_plugins` のようなもの）。**continuo の Go のコードに、そのキーを設定ファイルへ載せる処理を足す** | **不特定多数に及ぶ。**新しい設定キーは全利用者の WORKFLOW.md の語彙になる |
| **hook の入力の `permission_mode` を見る** | hook スクリプト3本 | maimuzo の環境だけ |
| **worktree の `.continuo.json` を hook スクリプトが見て抜ける** | hook スクリプト3本 | maimuzo の環境だけ |
| **worktree に `.claude/settings.local.json` を置いてプラグインを切る** | **continuo の Go のコード。**置き場所・`info/exclude` への登録・片付けの3つが新しく要る | **不特定多数に及ぶ** |
| **リポジトリの2本をプラグインへ移し、プラグイン単位で切る** | **新しいプラグインを1つ作る ＋ `.claude/settings.json` から hook を外す ＋ 上の「プラグインを1つ切る」の全部** | **不特定多数に及ぶ**（continuo 側の変更を含むため） |
| **issue ごとの設定ファイルで hook を全部切る** | continuo の Go のコード | **不特定多数に及ぶ。全利用者の continuo が壊れる** |
| **起動オプションで拡張を全部切る** | continuo の Go のコード | **同上** |

### 5-3. 確かめ方（人間が動作を確認する手順）

| どうするか | 人間がどう確かめるか |
| --- | --- |
| **無人の印を環境変数で渡し、hook スクリプトが先頭で抜ける** | 1. 人間の `claude` で200文字以上の崩れた返答を出し、**差し戻されることを見る。**2. 同じ worktree で continuo に issue を1件流し、issue のコメントに**差し戻しが1回も出ないこと**を見る。3. `printenv <印の名前>` を両方のセッションの Bash で叩き、片方にだけ在ることを見る |
| **issue ごとの設定ファイルで、プラグインを1つ切る** | 1. continuo のセッションで `/plugin list --disabled` を叩き、そのプラグインが出ることを見る。2. 人間のセッションで `/plugin list --enabled` に出ることを見る。3. 両方で `/hooks` を開き、`Stop` の件数が違うことを見る |
| **hook の入力の `permission_mode` を見る** | 人間が `claude --permission-mode auto` で起動し、**検査が効かなくなってしまうこと**を見る（これが落とし穴の実演になる） |
| **worktree の `.continuo.json` を hook スクリプトが見て抜ける** | continuo の worktree へ `cd` して人間の `claude` を起動し、**人間の側でも検査が効かなくなること**を見る |
| **どれでも共通** | `/hooks` を開く。公式が「`/hooks` を叩くと、イベントごとに設定済みの hook を全部見られる」と書いている。**ただしこのメニューは読み取り専用である**（"The `/hooks` menu is read-only."） |

### 5-4. 落とし穴

| どうするか | 落とし穴 |
| --- | --- |
| **無人の印を環境変数で渡し、hook スクリプトが先頭で抜ける** | **印の名前を継いでしまう経路がある。**人間が continuo を起動した端末からそのまま `claude` を叩くと、その shell に印が export されていれば継がれる。**`claude.env` は設定ファイル経由なので shell には出ないが、人間が手で export したら継がれる。**／ **`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` が `1` のとき、変数が削られる可能性がある**（4-6 の原文）。削られる変数の一覧は確かめていない |
| **issue ごとの設定ファイルで、プラグインを1つ切る** | **プラグイン単位でしか切れない**（4-8）。`maimuzo-chat-response` は hook だけのプラグインなので今は問題が無いが、**あとでスキルを足すと、そのスキルも一緒に消える。**／ **リポジトリの2本には効かない。**3本のうち1本しか止まらない |
| **hook の入力の `permission_mode` を見る** | **人間の `auto` を巻き込む。**continuo の既定は `auto` である（[internal/config/types.go:452-454](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/types.go#L452-L454)）。**判定として弱い** |
| **worktree の `.continuo.json` を hook スクリプトが見て抜ける** | **人間が continuo の worktree で作業すると検査が消える。**continuo に自分自身の issue をやらせている以上、人間がその worktree を開く場面は実際にある |
| **worktree に `.claude/settings.local.json` を置いてプラグインを切る** | **設計 3-12 が却下済み**（6-5）。worktree が汚れ、`info/exclude` の手当てと片付けが要る |
| **リポジトリの2本をプラグインへ移し、プラグイン単位で切る** | **プラグインは信頼していないフォルダでは黙って無効になる**（[docs/plans/continuo_design.md:1898](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L1898) が「信頼していないフォルダでは、subagent の frontmatter に書いた hook、プラグイン、追加のマーケットプレースが**ダイアログも出さずに無効化される**」と実測を記録している）。**人間のセッションでも黙って検査が消えうる** |
| **issue ごとの設定ファイルで hook を全部切る** | **continuo が turn の終わりを永久に受け取れなくなる**（6-4） |
| **起動オプションで拡張を全部切る** | **同上。加えて CLAUDE.md も skills も MCP も消える** |

---

## 6. 各パターンの根拠

### 6-1. 無人の印を環境変数で渡し、hook スクリプトが先頭で抜ける

**これがいちばん効く。**理由は3つ。

| なぜ | 根拠 |
| --- | --- |
| **経路が既に在り、実績がある** | `claude.env` → 設定ファイルの `env` → サブプロセスの環境、という経路を continuo は既に使っている（`CLAUDE_CODE_RETRY_WATCHDOG: "1"`。3-3）。**新しい仕組みを1つも作らない** |
| **continuo の Go のコードを1行も変えない** | 利用者が WORKFLOW.md へ1行足すだけで届く。**`.claude/settings.json` の hook の張り方も変えない** |
| **セッション単位なので、同じディレクトリでも割れる** | 環境変数は `--settings` で渡したセッションにだけ入る（4-3 の "for one session"） |

**足す1行のサンプル。**WORKFLOW.md の `claude.env` の下へ。

```yaml
  env:
    CLAUDE_CODE_RETRY_WATCHDOG: "1"
    CONTINUO_UNATTENDED: "1"                # 無人で走っていることを hook スクリプトへ伝える
```

**hook スクリプト側に足す形のサンプル**（3本とも、`stop_hook_active` を見ている行の直前へ）。

```python
def unattended() -> bool:
    """continuo が起動した無人のセッションなら真。

    無人のセッションには返答を読む人間が居ないので、書き直させても枠を使うだけである。
    印は WORKFLOW.md の claude.env から設定ファイルの env を経由して渡る。
    """
    return os.environ.get("CONTINUO_UNATTENDED", "") not in ("", "0", "false", "no")
```

```python
    if unattended():
        return 0
```

**名前を `CONTINUO_` で始めるべきか。**
continuo は既に `CONTINUO_RUNTIME_DIR` と `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` を使っている
（[internal/daemon/daemon.go:65-68](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/daemon/daemon.go#L65-L68)）ので、名前空間としては揃う。
**ただし continuo のコードはこの変数を読まない。**読むのは maimuzo の hook スクリプトだけである。
**「continuo が定めた変数」と誤読されうる**ので、人間が名前を決めること。

**この変更は、CLAUDE.md の「hook の挙動が変化する変更」に当たるか。当たらない。**
[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) が挙げる4つの定義は、
**hook が受け取る引数 / hook の宛先 / hook と本体の約束 / hook が Claude Code へ返すもの**であり、
どれも `continuo hook` サブコマンドについてのものである。
このパターンが触るのは `.claude/hooks/*.py`（返答を検査する側）と WORKFLOW.md だけで、
**同 CLAUDE.md の検知スクリプトの grep のパターンにも1本も掛からない**
（掛かるのは `internal/socketpath/` `internal/hookclient/` `internal/hookserver/` `internal/lock/`
`internal/orchestrator/settings.go` ほか。`.claude/hooks/` は入っていない）。
**とはいえ、実装の前に人間へこの判断を見せること。**

### 6-2. リポジトリの2本と、プラグインの1本は、止め方が違う

**ここを1つの方法で片付けようとすると、必ずどちらかが残る。**

| 何 | プラグインを切る方法で止まるか | 環境変数で抜ける方法で止まるか |
| --- | --- | --- |
| **返答の5段構成を検査する**（プラグイン） | **止まる** | **止まる**（スクリプトへ足せば） |
| **引用80文字などを検査する**（リポジトリ） | **止まらない** | **止まる** |
| **検証していないコマンドの報告を検査する**（リポジトリ） | **止まらない** | **止まる** |

**環境変数の経路だけが、3本ともを1つの仕組みで止められる。**

### 6-3. issue ごとの設定ファイルで、プラグインを1つ切る

**成り立つ。**4-3 の「利用者の設定ファイルが設定できるキーなら何でも設定できる」と、
4-7 の「`enabledPlugins` の Scope は Any file」の2つによる。

**書く形のサンプル**（continuo が書く `<実行時ディレクトリ>/issues/<スラグ>/settings.json`）。

```json
{
  "hooks": { "Stop": [ … continuo 自身の hook … ] },
  "permissions": { "allow": ["Bash","Read"], "deny": ["AskUserQuestion"] },
  "env": { "CLAUDE_CODE_RETRY_WATCHDOG": "1" },
  "enabledPlugins": { "maimuzo-chat-response@maimuzo-marketplace": false }
}
```

**続けるには WORKFLOW.md に新しい設定キーが要る。**
いまの `ClaudeConfig`（[internal/config/types.go:448-470](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/types.go#L448-L470)）に
プラグインを指すキーは無いので、**不特定多数の利用者の語彙が1つ増える。**

**測っていないこと。**`--settings` のファイル経由で `enabledPlugins` の `false` が実際に効くかは、**実測していない。**
文書の記述からは効くはずだが、プラグインの読み込みはセッションの開始時に起きるので、確かめてから採ること。

### 6-4. issue ごとの設定ファイルで hook を全部切る — 採ってはならない

**continuo 自身が turn の終わりを受け取れなくなる。**

| 順 | 何が起きるか |
| --- | --- |
| 1 | continuo が `--settings` で渡すファイルに `"disableAllHooks": true` が入る |
| 2 | 4-1 のとおり、**個別に残す手段は無い**ので、同じファイルに書いた continuo 自身の `Stop` hook も止まる |
| 3 | continuo は turn の終わりを永久に検知できない（3-4） |
| 4 | **人間から見える症状は「エージェントが喋り終わっているのに continuo が次を送らない」である。**[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) が同じ症状をこう書いている: 「hook が1つも届かないことと、Claude Code がまだ喋っている最中であることは、本体からは区別できない」 |

**測っていないこと。**`disableAllHooks` が「同じ `--settings` ファイルに書いた hook」まで止めるかどうかは、
**文書に例外の記述が無い**だけで、実測はしていない。
**だが「例外が無いほうに賭ける」のは、壊れたときの症状が重すぎる。**

### 6-5. worktree に `.claude/settings.local.json` を置く — 設計が却下済み

[docs/plans/continuo_design.md:2054-2062](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L2054-L2062) の 3-12 が、
2つの経路を並べて比べたうえで `--settings` に決めている。

| | worktree に `.claude/settings.local.json` を置く | **`--settings` で外部のファイルを指す** |
| --- | --- | --- |
| worktree が汚れるか | **汚れる。**`.gitignore` の手当てと、削除前に消す手間が要る | **汚れない** |

さらに [internal/config/types.go:439-443](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/types.go#L439-L443) が、実装しない理由を書いている。

> **届け方は `--settings` で外部の設定ファイルを指す経路に固定である**（設計 3-12）。
> 届け方を選ぶ設定キーは持たない。"worktree_local"（worktree に
> .claude/settings.local.json を置く）は、置き場所・.git/info/exclude への登録・
> 片付けの仕様がどこにも無いので実装していない。

**この案を復活させるなら、却下の根拠を否定するところから始めること**
（[.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) の「合理的根拠を否定できるなら、直さない」の裏である）。

### 6-6. 目印のファイル（`.continuo.json`）を hook が見る — 人間の要求を満たさない

**身元ファイルは実在する。**`<worktree>/.continuo.json` で、中身は
[docs/plans/continuo_design.md:2551](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L2551) の 3-18 にサンプルがある
（`issue_url` / `branch` / `session_uuid` / `settings_path` など）。

**だが、これはディレクトリに紐づく。**
人間が同じ worktree へ `cd` して `claude` を起動すると、その hook も同じファイルを見つけて抜ける。
**人間の要求「並行して同じディレクトリ上で」を満たさない。**

**session_uuid で割る案も成り立たない。**身元ファイルの `session_uuid` と
hook の入力の `session_id` を突き合わせれば、理屈の上ではセッション単位に割れる。
**しかし hook スクリプトが毎 turn ファイルを読むことになり、環境変数を1つ読むより高い。**
**環境変数で足りるものに、ファイルの読み取りを足す理由が無い。**

### 6-7. `--safe-mode` / `--bare` を渡す — 採ってはならない

公式の説明（https://code.claude.com/docs/en/cli-reference）。

> `--safe-mode`: Start with all customizations disabled to troubleshoot a broken configuration: CLAUDE.md, skills, plugins, **hooks**, MCP servers, custom commands and agents, output styles, workflows, custom themes, custom keybindings, status line and file-suggestion commands, LSP servers, and auto memory do not load.

> `--bare`: Minimal mode: skip auto-discovery of **hooks**, skills, custom commands, subagents, plugins, MCP servers, auto memory, and CLAUDE.md so scripted calls start faster.

**訳（要点だけ）。**
> `--safe-mode`: 壊れた設定を切り分けるために、**すべての作り込みを無効にして起動する。**
> CLAUDE.md・skills・プラグイン・**hook**・MCP サーバ…が読み込まれない。

> `--bare`: 最小モード。**hook**・skills・…・CLAUDE.md の自動検出を飛ばす。

**continuo 自身の hook も、組み込みの指示書も、CLAUDE.md も全部消える。**
6-4 と同じ理由で採れない。

### 6-8. hook の入力の `permission_mode` を見る — 判定として弱い

continuo は `--permission-mode` を毎回渡している
（[internal/orchestrator/settings.go:431-433](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/orchestrator/settings.go#L431-L433)）。

```go
	if mode := o.cfg.Claude.PermissionMode; mode != "" {
		args = append(args, "--permission-mode", mode)
	}
```

**既定は `auto` である**（[internal/config/types.go:452-454](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/config/types.go#L452-L454) の
「**既定は "auto"。**"dontAsk" を選べば、いままでどおり入力を待たない」）。

**だが `auto` は人間も使える。**[.claude/rules/](https://github.com/maimuzo/continuo/tree/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules) にも、
このセッション自身が auto mode で走っていることを示す記述がある。
**「無人かどうか」ではなく「権限モードが何か」を見ているだけなので、判定として弱い。**

---

## 7. 推奨

**推奨は、環境変数の印を1本立て、3本の hook スクリプトの先頭で抜けさせることである。**

| なぜ | 根拠の在りか |
| --- | --- |
| **3本ともを1つの仕組みで止められる**（プラグインを切る方法は1本しか止まらない） | 6-2 の表 |
| **同じディレクトリで並行して動く2つのセッションを区別できる** | 4-3 の "for one session" |
| **continuo の Go のコードを1行も変えない。不特定多数の利用者に何も及ばない** | 3-3（`claude.env` が既にある） |
| **既に実績のある経路を使う**（新しい仕組みを作らない） | `CLAUDE_CODE_RETRY_WATCHDOG` が同じ経路で届いている |

**ただし、次の2つは人間が決めること。**

| 決めること | 選択肢 |
| --- | --- |
| **印の名前** | `CONTINUO_UNATTENDED` は continuo が定めた変数と誤読されうる（6-1）。`MAIMUZO_HOOK_UNATTENDED` のように、読む側の持ち物だと分かる名前にする手もある |
| **continuo 側にも既定で書かせるか** | 書かせると不特定多数に及ぶ（5-2）。**書かせないなら、WORKFLOW.md へ1行足すのは利用者の仕事になる。**いまの maimuzo の環境では、それで足りる |

---

## 8. 測っていないこと

**この節を飛ばして実装しないこと。**

| 何 | なぜ測れていないか |
| --- | --- |
| **`--settings` のファイル経由で `enabledPlugins` の `false` が効くか** | 依頼が「設定の切り替えを実際に試さない（読むだけ）」と定めているため、試していない |
| **`disableAllHooks: true` が、同じ `--settings` ファイルに書いた hook まで止めるか** | 同上。**文書に例外の記述は無い** |
| **`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` が `1` のとき、どの変数が削られるか** | 変数の一覧が https://code.claude.com/docs/en/env-vars の取得できた範囲に出なかった |
| **`claude.env` に書いた変数が、実際に hook スクリプトの `os.environ` に届くか** | 文書の2つの記述（4-6）を繋いだ推論である。**実機で `printenv` を1回叩けば確かめられる** |
| **hook 1本あたりの実時間の費用** | 差し戻しが枠をどれだけ使うかは、この調査では測っていない |
