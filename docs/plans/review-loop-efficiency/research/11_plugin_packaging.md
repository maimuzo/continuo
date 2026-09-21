<!-- 目的: レビューループの定義をプラグインとしてまとめるための材料。公式文書で「何が配れるか」を確かめ、いまの定義を移す/残す/消すに仕分ける -->

# レビューループをプラグインへまとめるための材料

**この文書は材料であり、作る指示ではない。**作るのは人間の確認を得てからである。

**調べた時点。**2026-09-16。

| 何 | 値 |
| --- | --- |
| リポジトリの HEAD | `df36f9d7ec971e82ee0b680053a29ac3f9d35eb7`（`git rev-parse HEAD`） |
| 作業した場所 | `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/` |
| プラグインの編集用 clone | `~/Sources/github/maimuzo-claude-plugins/`。HEAD は `01f58ad6fa19bdbe2a6c2687c788c2737f05e873`（2026-08-26） |
| 導入済みの写し | `~/.claude/plugins/marketplaces/maimuzo-marketplace/`。HEAD は `540a15221978120226828d4c4ca7845f8a06e573`（2026-08-31） |
| 書いたもの | このファイルだけ。git と gh の書き込みはしていない |

---

## 0. worker-briefing の問いへの答え

**1. この作業に当てはまる規則。**

- worker-briefing の 2-4（指示に名前が出ていないものも探して読む）。呼ぶ側が挙げた範囲に加え、`~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/` の
  `.claude/settings.json`・`.claude/hooks/`・`.github/workflows/review-gate.yml`・`scripts/check-release-ready.sh`・`CONTRIBUTING.md`・`internal/prompt/builtin.md` と、
  `~/Sources/github/maimuzo-claude-plugins/README.md`・`SETUP_NEW_PROJECT.md`・`.github/workflows/test.yaml` を自分で見つけて読んだ
- worker-briefing の 2-5（同じものが他に無いかを数える）。件数・検索パターン・範囲を「8」に書いた
- [~/Sources/github/continuo/.claude/rules/reporting.md:554-562](../../../../.claude/rules/reporting.md#L554-L562) の根拠の付け方。公式文書は URL と原文と訳、手元のファイルはパスと行番号、「無い」は検索パターン・対象パス・対象コミットを付けた
- [~/Sources/github/continuo/CLAUDE.md:95-118](../../../../CLAUDE.md#L95-L118)（公開してよい情報かを常に判断する）と [~/Sources/github/continuo/CLAUDE.md:286-291](../../../../CLAUDE.md#L286-L291)（不特定多数の環境と maimuzo の環境を混同しない）。後者は、この仕分けの判断そのものに効いた

**2. 飛ばしてよい段。**[~/Sources/github/continuo/.claude/rules/design-review.md:5-17](../../../../.claude/rules/design-review.md#L5-L17) の9段は issue の実装作業の手順である。この作業は調査で、設計も実装もしないので通らない。
[~/Sources/github/continuo/CLAUDE.md:120-284](../../../../CLAUDE.md#L120-L284) の hook の検討も、コードを1行も変えないので当たらない。

**3. 公開してよくない情報を書きうる場面。**このファイルは PUBLIC のリポジトリに置かれる。

- worktree と個人のディレクトリの絶対パスを書きうるので、全部 `~/` から書いた
- `~/.claude/plugins/` の中の個人の設定値は、レビューに関係する範囲（版と HEAD）だけ書いた
- トークン・tailnet のホスト名は1つも書いていない
- **プラグイン名は実在のものをそのまま書いた。**この文書自体が「どのプラグインへ移すか」を決める材料で、架空名にすると材料にならないためである

**4. どう探し、何を読んだか。**

- **公式文書。**https://code.claude.com/docs/en/plugins ・ plugins-reference ・ skills ・ memory ・ sub-agents ・ code-review ・ plugin-marketplaces ・ discover-plugins の8本を WebFetch で取り、原文で引いた
- **手元のプラグイン。**`~/Sources/github/maimuzo-claude-plugins/` を `find` で全ファイル列挙し、marketplace.json・plugin.json 3本・hooks.json・skills の frontmatter を読んだ。導入済みの写しとは `diff -rq --exclude=.git` で比べた
- **リポジトリ側。**01 と 02 を Read で全文読み、そのうえで行数・バイト数・参照元を自分で測り直した
- **読めなかったもの。**無い。**ただし `/code-review` は実行していない。**3章の結論は公式文書の記述からの推論で、実測ではない

---

## 1. 言いたいこと

**言いたいこと。**プラグインは rules を配れず、skill の本文は起動時に読まれない。
だから「起動時に必ず読ませたい決まり」と「`/code-review` へ届けたい決まり」はリポジトリに残すしかない。
移せるのは、メインエージェントが手順として辿る部分（412行）と、worker への前置き（490行）である。

| いちばん重い発見 | どこに書いたか |
| --- | --- |
| **プラグインに rules の置き場所は無い。**公式文書が「指示を読み込ませたいなら skill に置け」と明記している | 2-2 |
| **skill の本文は起動時に読まれない。**読まれるのは description だけである | 2-3 |
| **`/code-review` には CLAUDE.md と `.claude/rules/` が届き、skill の本文は届かない。**重大度の基準をレビュワーへ渡したいなら、置き場所は rules しかない | 3 |
| **外部の貢献者にプラグインは効かない。**project scope で commit しても、その人が install するまで読み込まれない | 5-1 |
| **プラグイン化だけでは、公式の目安（CLAUDE.md 200行未満）には届かない。**774行が546行になるだけである | 6-3 |

**決めてほしいことは無い。**この文書は材料である。

---

## 2. プラグインで配れるもの・配れないもの

### 2-1. 配れる部品

**言いたいこと。**skills・agents・hooks・commands・MCP・LSP・monitors・bin・settings.json の9種類である。
rules と CLAUDE.md は入っていない。

https://code.claude.com/docs/en/plugins の「Plugin structure overview」の表が、プラグインのルートに置ける
ディレクトリを列挙している。`.claude-plugin/`（`plugin.json` だけ）・`skills/`・`commands/`・`agents/`・`hooks/`・
`.mcp.json`・`.lsp.json`・`monitors/`・`bin/`・`settings.json` の9つで、**rules は1つも無い。**

https://code.claude.com/docs/en/plugins-reference の manifest のスキーマも同じで、部品のパスを指す欄は
`skills` / `commands` / `agents` / `workflows` / `hooks` / `mcpServers` / `outputStyles` / `lspServers` と
`experimental.themes` / `experimental.monitors` / `experimental.evals` である。**`rules` の欄は無い。**

`settings.json` は配れるが、中身は限られる。同じ plugins の文書が書いている。

> "Plugins can include a `settings.json` file at the plugin root to apply default configuration when the plugin is enabled. Currently, only the `agent` and `subagentStatusLine` keys are supported."
>
> （訳: **プラグインは、有効化されたときに既定の設定を当てるための `settings.json` をプラグインのルートに置ける。
> いまのところ、対応しているキーは `agent` と `subagentStatusLine` の2つだけである。**）

**つまり `settings.json` 経由で `claudeMd` を配ることもできない。**`claudeMd` は
https://code.claude.com/docs/en/memory が「managed と policy の設定でしか効かない」と書いている欄で、
組織の管理者が配るものである。

### 2-2. rules は配れない（決定的な1文）

**言いたいこと。**公式文書が名指しで否定している。
プラグインのルートに CLAUDE.md を置いても読まれない。指示は skill に置け、と書いてある。

https://code.claude.com/docs/en/plugins-reference の「Standard plugin layout」にある。

> "A `CLAUDE.md` file at the plugin root is not loaded as project context. Plugins contribute context through skills, agents, and hooks rather than CLAUDE.md. To ship instructions that load into Claude's context, put them in a skill."
>
> （訳: **プラグインのルートに置いた `CLAUDE.md` は、プロジェクトの文脈としては読み込まれない。
> プラグインは `CLAUDE.md` ではなく skills・agents・hooks を通じて文脈を与える。
> Claude の文脈へ読み込ませたい指示は、skill に置くこと。**）

**手元のプラグインも、1つも rules を持っていない。**
`find ~/Sources/github/maimuzo-claude-plugins/plugins -type d -name rules` の出力は0行だった
（対象は編集用 clone の `01f58ad6`）。

**ただし例外が1つある。**`~/.claude/plugins/cache/everything-claude-code/.../rules/` にはファイルが在る。
02 が [同じことを書いている](02_inventory_memory_plugins.md)（3-5 の `ecc/rules/common/code-review.md` の行に
「plugin.json に rules のキーは無い（キーは agents・skills・commands）」とある）。
**置いてあるだけで、Claude Code は読み込まない。**

### 2-3. skill の本文は、起動時に読まれない

**言いたいこと。**起動時に読まれるのは description だけである。
本文は呼ばれたときに入る。これがプラグイン化の効き目そのものであり、同時に最大の落とし穴である。

https://code.claude.com/docs/en/skills が2箇所で書いている。

> "Unlike CLAUDE.md content, a skill's body loads only when it's used, so long reference material costs almost nothing until you need it."
>
> （訳: **CLAUDE.md の内容と違って、skill の本文は使われたときにだけ読み込まれる。
> だから長い参照用の資料は、必要になるまでほとんど費用がかからない。**）

> "In a regular session, skill descriptions are loaded into context so Claude knows what's available, but full skill content only loads when invoked."
>
> （訳: **通常のセッションでは、何が使えるかを Claude が知るために skill の description は文脈へ読み込まれるが、
> skill の本文は呼び出されたときにだけ読み込まれる。**）

**反対に、rules は起動時に読まれる。**https://code.claude.com/docs/en/memory にある。

> "Rules without `paths` frontmatter are loaded at launch with the same priority as `.claude/CLAUDE.md`."
>
> （訳: **`paths` の frontmatter を持たない rule は、`.claude/CLAUDE.md` と同じ優先度で起動時に読み込まれる。**）

**いまの `.claude/rules/` は8本とも `paths` を持っていない。**
`grep -l '^paths:' ~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/.claude/rules/*.md` は
0件を返した（対象コミット `df36f9d7`）。**つまり8本とも毎回読み込まれている。**

**同じ文書が、この作業そのものを推奨している。**

> "Rules load into context every session or when matching files are opened. For task-specific instructions that don't need to be in context all the time, use skills instead, which only load when you invoke them or when Claude determines they're relevant to your prompt."
>
> （訳: **rules は毎セッション、または一致するファイルが開かれたときに文脈へ読み込まれる。
> 常に文脈にある必要のない、作業ごとの指示には代わりに skills を使うこと。
> skills は、呼び出したとき、または Claude がその prompt に関係すると判断したときにだけ読み込まれる。**）

### 2-4. 起動時に必ず読ませたいものを、プラグインでどう配るか

**言いたいこと。**手段は3つあるが、どれも rules の代わりにならない。
確実なのは hook で毎回注入する形だけで、それは rules と同じ量を毎回消費する。

| 手段 | できること | 何が足りないか |
| --- | --- | --- |
| **skill に置く**（公式の推奨） | description が起動時に入り、本文は呼ばれたときに入る | **呼ばれる保証が無い。**Claude が関係すると判断したときだけ入る |
| **`SessionStart` hook の標準出力** | 毎セッションの頭に文字列を文脈へ足せる | **rules と同じ量を毎回消費する。**短くする目的には効かない |
| **`agents/` に置く** | その agent を立てたときに system prompt として入る | メインエージェントには効かない |

`SessionStart` の挙動は https://code.claude.com/docs/en/hooks にある。

> "For most events, Claude Code writes stdout to the debug log and doesn't show it in the transcript. The exceptions are `UserPromptSubmit`, `UserPromptExpansion`, `SessionStart`, and `PostModelSwitch`, where Claude Code adds plain-text stdout as context that Claude can see and act on."
>
> （訳: **たいていの event では、Claude Code は標準出力を debug のログへ書き、transcript には見せない。
> 例外は `UserPromptSubmit`・`UserPromptExpansion`・`SessionStart`・`PostModelSwitch` で、
> そこでは Claude Code が平文の標準出力を、Claude が見て動ける文脈として足す。**）

**プラグインが hook を配れることは、手元で動いている実物で確かめられる。**
~/Sources/github/maimuzo-claude-plugins/plugins/maimuzo-chat-response/hooks/hooks.json が
`"command": "python3 \"${CLAUDE_PLUGIN_ROOT}/hooks/check-reply-structure.py\""` を `Stop` に張っている。

---

## 3. `/code-review` に何が届くか

**言いたいこと。**CLAUDE.md と `.claude/rules/` は届く。skill の本文は届かない。
だから重大度の基準のような「レビュワーへ届けたい決まり」は、rules に置くしかない。

**まず `/code-review` の走り方。**https://code.claude.com/docs/en/code-review が3つ書いている。

> "The review follows your `CLAUDE.md` like any Claude Code session, but it doesn't read `REVIEW.md`."
>
> （訳: **このレビューは、ほかの Claude Code のセッションと同じくあなたの `CLAUDE.md` に従うが、`REVIEW.md` は読まない。**）

> "In a terminal session, where `/code-review` runs the review as a forked subagent"
>
> （訳: **terminal のセッションでは、`/code-review` はレビューを forked subagent として走らせる**）

**次に、その subagent に何が入るか。**https://code.claude.com/docs/en/sub-agents の「What loads at startup」にある。

> "**CLAUDE.md files**: every level of the CLAUDE.md hierarchy the main conversation loads, including `~/.claude/CLAUDE.md`, project rules, `CLAUDE.local.md`, and managed policy files."
>
> （訳: **CLAUDE.md のファイル群: メインの会話が読み込む CLAUDE.md の階層の全段。
> `~/.claude/CLAUDE.md`・project rules・`CLAUDE.local.md`・管理ポリシーのファイルを含む。**）

**`project rules` がこの一覧に入っている。**`.claude/rules/` のことである。

> "**Preloaded skills**: full content of any skill named in the agent's `skills` field. Built-in agents don't preload skills."
>
> （訳: **先読みした skills: agent の `skills` の欄に名前を書いた skill の本文全体。
> 組み込みの agent は skill を先読みしない。**）

**`/code-review` は組み込みである。**だから、この経路で skill の本文が先読みされることは無い。

**skill の側から入り込む経路も無い。**https://code.claude.com/docs/en/skills が
`context: fork` の skill について挙げているのは「Also loads: CLAUDE.md, per the agent's startup context」
（訳: **併せて読み込むもの: CLAUDE.md。その agent の startup context に従う**）だけで、
**ほかの skill の本文は1つも挙げていない。**

**実行中に呼ぶ経路だけは残る。**

> "without it, the subagent can still discover and invoke project, user, and plugin skills through the Skill tool during execution."
>
> （訳: **それが無くても、subagent は実行中に Skill ツールを通じて、project・user・plugin の skill を
> 見つけて呼び出すことはできる。**）

**だが呼ぶ保証は無いうえ、こちらから頼む手段も無い。**
[~/Sources/github/continuo/.claude/skills/pr-review-and-merge/SKILL.md:99](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L99) が
「`/code-review` は自由なプロンプトを足せない。」と書いている（この行は 01 が挙げたもので、私も原文を開いて確かめた）。

**結論。**レビュワーへ届けたい決まり（重大度の基準・1回で全部挙げさせる指示・前の周の対応表）は、
**プラグインの skill へ移すと届かなくなる。**届かせるには CLAUDE.md か `.claude/rules/` に置くしかない。

**この結論は実測ではない。**`/code-review` を実行して、何が文脈に入ったかを観測してはいない。

---

## 4. 手元のプラグインの作り

### 4-1. リポジトリ構成

**言いたいこと。**marketplace の定義1本と、`plugins/` の下に14個のディレクトリが並ぶだけの形である。

```
~/Sources/github/maimuzo-claude-plugins/
├── .claude-plugin/marketplace.json   ← マーケットプレイスの定義（1本）
├── .github/workflows/test.yaml       ← 同梱スクリプトの unit テスト
├── README.md / SETUP_NEW_PROJECT.md
└── plugins/<プラグイン名>/
    ├── .claude-plugin/plugin.json
    ├── skills/<名前>/SKILL.md
    ├── agents/<名前>.md
    └── hooks/hooks.json
```

`marketplace.json` は `name`（`maimuzo-marketplace`）・`owner`・`plugins`（14件）・`renames` を持つ。
各 entry は `name` と `source`（`./plugins/<名前>` の相対パス）と `description` の3つだけである。
公式のスキーマ（https://code.claude.com/docs/en/plugin-marketplaces）で必須なのは `name` / `owner` / `plugins` の3つで、
entry の必須は `name` と `source` の2つなので、**いまの書き方は必須だけを埋めた最小の形である。**

`plugin.json` も最小である。実物は4行しかない。

```json
{
  "name": "maimuzo-chat-response",
  "version": "0.2.0",
  "description": "チャット返答の5段構成（…）が、この順番で中身を伴って揃っているかを Stop hook で機械的に検査する。…",
  "author": { "name": "maimuzo" }
}
```

**部品のパスを指す欄（`skills` / `agents` / `hooks`）は1つも書いていない。**既定の場所に置いているためである。

### 4-2. 参考にする2つ

**言いたいこと。**hooks を持つ最小の例が `maimuzo-chat-response`、skills 中心の例が `maimuzo-go` である。
どちらも `plugin.json` は4行で、中身はディレクトリの置き方だけで決まっている。

**hooks を持つ例。**`~/Sources/github/maimuzo-claude-plugins/plugins/maimuzo-chat-response/` は6ファイルしかない。

| ファイル | 役割 |
| --- | --- |
| `.claude-plugin/plugin.json` | 上の4行 |
| `hooks/hooks.json` | `Stop` に1本張る。`${CLAUDE_PLUGIN_ROOT}` でスクリプトを指す |
| `hooks/check-reply-structure.py` | 検査の実体 |
| `tests/test_check_reply_structure.py` | unit テスト。`.github/workflows/test.yaml` が CI で回す |
| `README.md` | 検査の中身と切り方 |

**このリポジトリのレビューループを持ち出すなら、いちばん近い形がこれである。**
検査を機械で持ち、テストを同梱し、CI で守っている。

**skills 中心の例。**`~/Sources/github/maimuzo-claude-plugins/plugins/maimuzo-go/` は
`skills/` の下に9個の SKILL.md を持つ。frontmatter は3行で揃っている。

```yaml
---
name: coding-guide-go
description: GoプロジェクトのCLAUDE.md固有コーディング・テストルール。…コード記述・テスト作成時に参照。
user-invocable: false
---
```

**`user-invocable: false` が9本とも付いている。**`plugin.json` の description も
「すべて user-invocable:false の context 節約型スキル」と名乗っている。
**description に「いつ参照するか」を書いてあるのは、2-3 のとおり起動時に読まれるのが description だけだからである。**

`~/Sources/github/maimuzo-claude-plugins/plugins/maimuzo-co-review/skills/co-reviewer/SKILL.md` は
`context: fork` と `allowed-tools: Read, Grep, Glob, Bash` を持つ。**別コンテキストのレビュワーを立てる形の実物である。**

### 4-3. 編集用 clone は、導入済みの写しより古い

**言いたいこと。**編集用 clone の HEAD は 2026-08-26 で、導入済みの写しは 2026-08-31 である。
**編集を始める前に `git pull` が要る。**そうしないと5日ぶんを巻き戻す変更を作る。

| 何 | HEAD | 日付 |
| --- | --- | --- |
| `~/Sources/github/maimuzo-claude-plugins/`（編集用 clone） | `01f58ad6` | 2026-08-26 22:07 |
| `~/.claude/plugins/marketplaces/maimuzo-marketplace/`（導入済みの写し） | `540a1522` | 2026-08-31 09:50 |

**編集用 clone は `540a1522` を持っている**（`git cat-file -t 540a152` が `commit` を返した）。
fetch は済んでいて、checkout している branch が古いだけである。

**導入済みの写しは depth 1 の shallow clone である**（`git rev-parse --is-shallow-repository` が `true`、
`git log --oneline | wc -l` が `1`）。**だからそちらで履歴は追えない。**

`diff -rq --exclude=.git` の差は5件で、どれも編集用 clone が古いことによる。

| 差 | 中身 |
| --- | --- |
| `README.md` | 内容が違う |
| `plugins/maimuzo-dev-core/.claude-plugin/plugin.json` | 版が違う（編集用 clone は 0.3.2、導入済みは 0.4.0） |
| `plugins/maimuzo-dev-core/skills/detect-chatgpt-usage-from-webapi/` | **導入済みの写しにしか無い** |
| `plugins/maimuzo-dev-core/skills/detect-usage-from-webapi/SKILL.md` | 内容が違う |
| `plugins/maimuzo-issue-manage/skills/bug-tickets-priority-analysis/SKILL.md` | 内容が違う |

### 4-4. 更新の流れ

**言いたいこと。**編集用 clone で PR → main へマージ → 各プロジェクトが `/plugin marketplace update` で取り込む。
この流れはリポジトリの README に書いてある。

~/Sources/github/maimuzo-claude-plugins/README.md:5 の原文。

> **プロジェクトに submodule として取り込む運用は廃止した。** プラグインの実体は Claude Code が `~/.claude/plugins/` 配下へ取得する。編集はこのリポジトリの専用 clone（例: `~/Sources/github/maimuzo-claude-plugins`）で行い、PR 経由でマージする。各プロジェクトへは `/plugin marketplace update maimuzo-marketplace` で反映される（通常は自動）。

**「通常は自動」には条件がある。**https://code.claude.com/docs/en/discover-plugins にある。

> "Third-party and local development marketplaces have auto-update disabled by default."
>
> （訳: **第三者のマーケットプレイスと、手元の開発用のマーケットプレイスは、既定で auto-update が無効である。**）

> "Claude Code checks for marketplace and plugin updates after your session starts, with a random delay of up to ten minutes, so the running session keeps using the versions it loaded at launch."
>
> （訳: **Claude Code はセッションが始まったあと、最大10分のランダムな遅れを置いて、マーケットプレイスと
> プラグインの更新を確認する。走っているセッションは、起動時に読み込んだ版を使い続ける。**）

**`~/Sources/github/maimuzo-claude-plugins/SETUP_NEW_PROJECT.md:234` も、同じことを別の理由で書いている。**

> **プライベートリポジトリのためバックグラウンド更新が失敗することがある**（上記「プライベートリポジトリ前提」を参照）。マーケットプレイスの HEAD が GitHub の `main` より古いままなら、`/plugin marketplace update` を手動実行する。

**版を上げないと届かない。**https://code.claude.com/docs/en/plugins の manifest の表に
「If set, users only receive updates when you bump this field」（訳: **設定してある場合、利用者は
この欄を上げたときにだけ更新を受け取る**）とある。**いまの `plugin.json` は全部 `version` を持っている。**

---

## 5. 何を移し、何を残すか

### 5-0. 仕分けの決め手

**言いたいこと。**呼ぶ側が挙げた4つの観点を、2章と3章の事実へ当てると、3つが「残す」に倒れる。
移せるのは「メインエージェントが手順として辿る部分」だけである。

| 観点 | 当てた結果 |
| --- | --- |
| **誰に効く必要があるか** | **外部の貢献者にプラグインは効かない。**下の引用のとおり、project scope で commit しても install するまで読み込まれない。しかも [~/Sources/github/continuo/CLAUDE.md:107](../../../../CLAUDE.md#L107) が、プラグインの有効化とマーケットプレイスの登録を「絶対にコミットしないもの」として `.claude/settings.local.json`（`.gitignore` 済み）へ置けと定めている |
| **機械が見ているか** | **目印の決まりはリポジトリ固有である。**`code-review-result` は規則の側に15件あり、CI と hook とリリース前の検査の3箇所が同じ正規表現を持つ |
| **`/code-review` に届くか** | **rules は届き、skill の本文は届かない**（3章） |
| **利用者向けか** | [~/Sources/github/continuo/internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) は製品の一部である。`git grep -n -E '\.claude/(rules\|skills)\|worker-briefing\|maimuzo' -- internal/` は、Go の import 以外に1件も返さなかった。**境界は既に切れている** |

外部の貢献者の根拠は https://code.claude.com/docs/en/discover-plugins にある。

> "A plugin that only the project's `.claude/settings.json` enables, and that comes from an external source such as a GitHub repository or npm package, doesn't load until the team member installs it."
>
> （訳: **project の `.claude/settings.json` だけが有効にしていて、GitHub のリポジトリや npm のパッケージのような
> 外部の source から来るプラグインは、そのチームの人が install するまで読み込まれない。**）

### 5-1. 仕分けの表

**言いたいこと。**移すのが4件（902行）、残すのが5件、消すのが2種類である。
移す4件のうち2件は、そもそも maimuzo の環境の名前が OSS のリポジトリに書いてあるものの引き取りである。

**移す（プラグインへ）。**

| 何（行数） | なぜ移すか |
| --- | --- |
| **[~/Sources/github/continuo/.claude/skills/worker-briefing/SKILL.md](../../../../.claude/skills/worker-briefing/SKILL.md) の全部**（490行・35,210バイト） | **既に skill で、起動時には読まれていない。**そのファイル自身の9行目が「中身は continuo に固有ではないので、動けばプラグインへ移す。検討は issue #165（コードレビューの記録と敵対的レビューの手順を、プラグインへ移せるか検討する）」と書いている |
| **[~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md)**（228行・16,638バイト。収まっている定義／最大1周／3・6・9／連続10回／敵対的レビュー判定後） | **continuo 固有の語が1つも要らない手順である。**メインエージェントが辿るもので、`/code-review` へ届ける必要が無い |
| **[~/Sources/github/continuo/.claude/rules/design-review.md:57-82](../../../../.claude/rules/design-review.md#L57-L82) と [同:124-240](../../../../.claude/rules/design-review.md#L124-L240)**（143行） | 設計を疑う段・根拠を否定できるなら直さない・回す回数・3回で収まらなかったとき。**同じく汎用である** |
| **[~/Sources/github/continuo/.claude/rules/design-review.md:83-123](../../../../.claude/rules/design-review.md#L83-L123)**（41行。誰にレビューさせるか） | **`maimuzo-from-ecc:architect` を名指ししている**（85行と115行）。**OSS のリポジトリに個人のプラグイン名が書いてある状態そのものなので、プラグイン側へ引き取るのが正しい置き場所である** |

**残す（リポジトリ）。**

| 何（行数） | なぜ残すか |
| --- | --- |
| **[~/Sources/github/continuo/CLAUDE.md:394-560](../../../../CLAUDE.md#L394-L560)**（149行。PR の手順・3箇所の機械・数える条件・対応表の列） | **目印の決まりそのものである。**`CONTINUO_ALLOW_UNREVIEWED_MERGE`（**2026-09-21 に廃止**）も `check-release-ready` もここにしか無い |
| **[~/Sources/github/continuo/.claude/rules/design-review.md:1-56](../../../../.claude/rules/design-review.md#L1-L56)**（56行） | **`<!-- continuo:agent -->` を先頭にする順序**（2件）**と、CI の `design-review-result` の条件がある。**continuo が起動したエージェントの挙動に直結する |
| **[~/Sources/github/continuo/.claude/skills/pr-review-and-merge/SKILL.md](../../../../.claude/skills/pr-review-and-merge/SKILL.md)**（310行） | `block-merge-without-review.py`（**2026-09-21 に廃止**）と `review-gate.yml` と目印を名指しする。**`code-review-result` を含む行が多く、目印を引数にしない限り持ち出せない** |
| **[~/Sources/github/continuo/CONTRIBUTING.md:133-241](../../../../CONTRIBUTING.md#L133-L241)**（109行） | **外部の貢献者だけが読む。**プラグインは効かない（5-0） |
| **[~/Sources/github/continuo/internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) の 3-2 と 3-6** | **製品の一部である。**利用者の手元で動く |

**消す（プラグイン化が終わってから）。**

| 何 | 中身 |
| --- | --- |
| **リポジトリ内の写し** | 01 の4章が挙げた重複。「最大1周」の写しが3箇所、「3・6・9回目で6段、10回で止まる」の写しが7箇所ある。**正がプラグインの skill 1本になれば、案内の1行だけ残して全部消せる** |
| **memory の6ファイルと索引1行** | 02 の4章が挙げた食い違い。`~/.claude/projects/<continuo のディレクトリ>/memory/` の下にある。**上限3回・5列の対応表・人間の返事を待つ・消さず人間に確認 の4つが、現行の規則と逆を言っている** |

### 5-2. 移すと壊れる参照

**言いたいこと。**worker-briefing を指す行が11本ある。移すと全部が「無い」になる。
呼び出し方も変わる（`/plugin 名:worker-briefing`）。

| 指している側 | 件数 |
| --- | --- |
| [~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md) | 4 |
| [~/Sources/github/continuo/.claude/rules/reporting.md](../../../../.claude/rules/reporting.md) | 5 |
| [~/Sources/github/continuo/.claude/rules/design-review.md](../../../../.claude/rules/design-review.md) | 1 |
| [~/Sources/github/continuo/.claude/skills/pr-review-and-merge/SKILL.md](../../../../.claude/skills/pr-review-and-merge/SKILL.md) | 1 |

**そのうえで、worker-briefing 自身が「絶対パスで渡せ」という手順を持っている**
（[~/Sources/github/continuo/.claude/skills/worker-briefing/SKILL.md:43-55](../../../../.claude/skills/worker-briefing/SKILL.md#L43-L55) の、
在るほうのパスを1行で出すスクリプト）。**プラグインへ移すと、そのスクリプトは `.claude/skills/` を探すので当たらない。**
置き場所は `~/.claude/plugins/cache/maimuzo-marketplace/<プラグイン名>/<版>/skills/worker-briefing/SKILL.md` になり、
**版の文字列が入るので、パスを固定で書けなくなる。**

**`user-invocable: false` のままでよいかも変わる。**いまは人間の `/` から呼べない設定だが、
プラグインの skill は `/plugin-name:skill-name` で名前空間が付く
（https://code.claude.com/docs/en/skills の precedence の表）。

---

## 6. 長さ

### 6-1. いま起動時に読まれる量

**言いたいこと。**2,468行・159,837バイトが毎セッション読み込まれている。
そのうちレビューループが673行（27%）である。

| 何 | 行数 | バイト数 |
| --- | --- | --- |
| [~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md) | 774 | 57,633 |
| `.claude/rules/` の8本 | 1,694 | 102,204 |
| **合計** | **2,468** | **159,837** |

**そのうちレビューループの部分。**

| 何 | 行数 | バイト数 |
| --- | --- | --- |
| [~/Sources/github/continuo/CLAUDE.md:310-318](../../../../CLAUDE.md#L310-L318)（設計のレビューの要約） | 9 | 1,104 |
| [~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md) | 377 | 28,326 |
| [~/Sources/github/continuo/.claude/rules/design-review.md](../../../../.claude/rules/design-review.md) の全部 | 248 | 14,561 |
| [~/Sources/github/continuo/.claude/rules/reporting.md:554-592](../../../../.claude/rules/reporting.md#L554-L592) | 39 | 3,035 |
| **合計** | **673** | **47,026** |

測ったコマンドは `sed -n '<開始>,<終了>p' <ファイル> | wc -l` と `| wc -c`、全体は `wc -l` / `wc -c`。

**skills は、この数に入っていない**（2-3）。
[~/Sources/github/continuo/.claude/skills/worker-briefing/SKILL.md](../../../../.claude/skills/worker-briefing/SKILL.md) の490行と
[~/Sources/github/continuo/.claude/skills/pr-review-and-merge/SKILL.md](../../../../.claude/skills/pr-review-and-merge/SKILL.md) の310行は、
**呼ばれたときにだけ入る。**ただし worker-briefing は worker ごとに毎回 Read されるので、
**worker の側では毎回490行が入っている。**

### 6-2. 公式文書が言う目安

**言いたいこと。**CLAUDE.md 1枚あたり200行未満である。いまは774行で、3.9倍ある。

https://code.claude.com/docs/en/memory の「Write effective instructions」にある。

> "**Size**: target under 200 lines per CLAUDE.md file. Longer files consume more context and reduce adherence."
>
> （訳: **大きさ: CLAUDE.md 1枚あたり200行未満を目標にする。
> 長いファイルは文脈を多く消費し、遵守の度合いを下げる。**）

同じ文書の「My CLAUDE.md is too large」にも、対になる記述がある。

> "Files over 200 lines consume more context and may reduce adherence. … Splitting into `@path` imports helps organization but doesn't reduce context, since imported files load at launch."
>
> （訳: **200行を超えるファイルは文脈を多く消費し、遵守の度合いを下げうる。…
> `@path` の import へ分けるのは整理には役立つが、import したファイルも起動時に読み込まれるので、文脈は減らない。**）

**`.claude/rules/` の合計に対する目安は、公式文書に無い。**
上の8本の文書を読んだ範囲で、rules の行数やバイト数の上限・目標を書いた文は1つも無かった。
書いてあるのは「常に文脈にある必要のないものは skills にせよ」（2-3 の引用）という向きだけである。

**CLAUDE.md には上限が別に在る。**同じ文書が「Claude Code loads a CLAUDE.md file of up to 4 MiB in full and skips a larger file.」
（訳: **Claude Code は 4 MiB までの CLAUDE.md を全部読み込み、それより大きいものは飛ばす。**）と書いている。
**57,633バイトなので、この線には遠い。**効いているのは200行の目安のほうである。

### 6-3. 移したときに、起動時の量がどれだけ減るか

**言いたいこと。**412行が出ていき、案内が10行ほど戻るので、約2,066行になる。
CLAUDE.md 単体は774行から546行になる。**それでも200行の目安には届かない。**

| 何を移すか | 減る行数 |
| --- | --- |
| [~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md) | 228 |
| [~/Sources/github/continuo/.claude/rules/design-review.md:57-123](../../../../.claude/rules/design-review.md#L57-L123) | 67 |
| [~/Sources/github/continuo/.claude/rules/design-review.md:124-240](../../../../.claude/rules/design-review.md#L124-L240) | 117 |
| **合計** | **412** |

**案内の行が戻る。**移した先を指す1行は残さなければならない
（[~/Sources/github/continuo/CLAUDE.md:308](../../../../CLAUDE.md#L308) が既に
「`.claude/rules/` と `.claude/skills/` の下のファイルは自動では読まれない。**ここから辿る。**」と書いている形）。
**4箇所ぶんで10行ほどと見積もる。**これは見積もりであって、測った値ではない。

| 何 | いま | 移したあと（見積もり） |
| --- | --- | --- |
| 起動時に読まれる合計 | 2,468行 | **約2,066行**（-16%） |
| [~/Sources/github/continuo/CLAUDE.md](../../../../CLAUDE.md) 単体 | 774行 | **約546行** |
| 公式の目安（200行）との差 | 3.9倍 | **2.7倍** |

**つまり、レビューループのプラグイン化だけでは目安に届かない。**
届かせるには、レビューループ以外（[~/Sources/github/continuo/CLAUDE.md:120-284](../../../../CLAUDE.md#L120-L284) の
hook の検討の165行など）にも手を入れる必要がある。**それはこの調査の範囲外である。**

---

## 7. 測っていないこと

| 何を | なぜ |
| --- | --- |
| **`/code-review` の文脈に実際に何が入るか** | 実行していない。3章は公式文書の記述からの推論である |
| **プラグインの skill が、実際にどれだけ自動で呼ばれるか** | 測っていない。`/plugin` の Stats タブに「what each of your skills costs in context and how often it gets used」があると公式文書は書くが、開いていない |
| **`SessionStart` hook で注入したときの実際の消費量** | 試していない |
| **移したあとの見積もり（6-3）の案内10行** | 見積もりであって実測ではない。実際に書いてみないと決まらない |
| **`~/.claude/plugins/cache/` に展開された版と、編集用 clone の全ファイルの差** | 導入済みの marketplace の写しとしか比べていない |

---

## 8. 数えた記録（worker-briefing 2-5）

**範囲。**`~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/` の追跡ファイル。対象コミット `df36f9d7`。
`git grep` で数えた（`.gitignore` 済みの写しを数えないため）。

| 検索パターン | 件数 | どこか |
| --- | --- | --- |
| `code-review-result` | 15 | CLAUDE.md・design-review.md・pr-review-and-merge・worker-briefing |
| `design-review-result` | 4 | CLAUDE.md・design-review.md |
| `review-gate\.yml` | 4 | CLAUDE.md・design-review.md・pr-review-and-merge |
| `continuo:agent` | 2 | design-review.md のみ |
| `block-merge-without-review`（**2026-09-21 に廃止。**いまは0件） | 2 | CLAUDE.md・pr-review-and-merge |
| `CONTINUO_ALLOW_UNREVIEWED_MERGE`（**2026-09-21 に廃止**） | 1 | CLAUDE.md のみ |
| `check-release-ready` | 1 | CLAUDE.md のみ |
| `skills/worker-briefing` | 14 | **指している側が11**（CLAUDE.md 4・reporting.md 5・design-review.md 1・pr-review-and-merge 1）。ほかに worker-briefing 自身が2、docs/plans/release_v0114.md が1 |
| `worker-briefing`（素の文字列） | 17 | 上と同じ並びで、pr-review-and-merge が2、worker-briefing 自身が4になる。**5-2 の表は `skills/worker-briefing` のほうで数えている** |
| `rules/design-review\.md` | 9 | issue.md・worker-briefing・review-gate.yml・CLAUDE.md |
| `maimuzo-from-ecc\|maimuzo-marketplace\|maimuzo-dev-core\|maimuzo-chat-response` | 7 | design-review.md 2・issue.md 1・plugins.md 3・reporting.md 1 |

範囲は `-- CLAUDE.md .claude/rules .claude/skills`（最後の1行だけ）、ほかは同じ範囲。

**「無い」ことの主張。**

| 何が無いか | 検索パターン | 対象パス | 対象コミット |
| --- | --- | --- | --- |
| **プラグインに rules のディレクトリは無い** | `find … -type d -name rules` | `~/Sources/github/maimuzo-claude-plugins/plugins` | `01f58ad6` |
| **`paths` frontmatter を持つ rule は無い** | `grep -l '^paths:'` | `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/.claude/rules/*.md` | `df36f9d7` |
| **製品側が `.claude/` を参照していない** | `\.claude/(rules\|skills)\|worker-briefing\|maimuzo` | `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/internal/` | `df36f9d7` |

**3つ目は、Go の import（`github.com/maimuzo/continuo/internal/…`）に当たって多数出る。**
`maimuzo` のパターンがモジュール名に一致するためである。
**import 行を除くと0件で、`.claude/rules`・`.claude/skills`・`worker-briefing` はどれも1件も出なかった。**

**文字列では数えられないもの。**「この節は汎用か continuo 固有か」の判定は、
文字列一致では数えられない。**5-1 の仕分けは、節ごとに全文を読んで判断した。**
判断の根拠になった固有名の件数は、上の表に出している。
