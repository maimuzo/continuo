# Claude Codeを正本にしたCodex併用セットアップ手順

Claude Codeの既存 `CLAUDE.md`・`.claude/rules/`・選択したskills・hook scriptを正本のまま使い、Codexの入口とnative宣言だけを追加する。
本文やscriptはsymlink・直接参照で共有し、Codex固有の宣言が必要な部分だけ薄い接続を置く。
この手順は、既に検証した範囲と、追加検証が必要なhooks・permissions・agent連携を分けて別のmacOS/Linux環境へ反映するものである。

## 1. 対象と前提

基本セットアップの対象は次の範囲で、追加候補は12節の条件を満たしたものだけ有効化する。

- Codexが `CLAUDE.md` をプロジェクト指示として読む設定
- 個人指示の `~/.codex/AGENTS.md` から `~/.claude/CLAUDE.md` へのsymlink
- Claudeの標準的なskillディレクトリを、プロジェクトの `.agents/skills/` または `~/.agents/skills/` へskill単位でsymlink
- 同じmarketplaceからCodexへ必要なpluginだけを導入
- Claude内からCodexを呼ぶ公式pluginと、HerdrからCodexを直接起動する経路
- Codex native hooksから、互換性を確認したClaude hook scriptを直接呼ぶ経路

Claudeのhook設定JSONをCodexへsymlinkしない。共有候補は、Codexのevent payloadとoutput契約が一致するhook script、`name`・`description`を持つskill、依存コマンドと参照資料が対象環境にあるpluginである。transcript依存hook、`context: fork`、Claude agent、MCP secretは追加検証が終わるまで有効化しない。

## 2. 作業前の確認とバックアップ

作業対象を確認し、既存ファイルを上書きしない。すでにCodex固有の `AGENTS.md` やskillがある場合は、symlinkへ置き換えず、その入口からClaudeの正本を読む短い指示を追加する。

```sh
test -d ~/.claude
test -d ~/.codex
test -f ~/.codex/config.toml
mkdir -p ~/.codex/backups/claude-coexistence-$(date +%Y%m%d)
cp -p ~/.codex/config.toml ~/.codex/backups/claude-coexistence-$(date +%Y%m%d)/config.before.toml
test -f ~/.claude/CLAUDE.md && cp -p ~/.claude/CLAUDE.md ~/.codex/backups/claude-coexistence-$(date +%Y%m%d)/CLAUDE.before.md
```

設定を編集するときは、同じディレクトリの一時ファイルへ書いてからrenameする。エディタの安全保存機能を使い、ファイル全体の上書きや既存キーの重複を避ける。

## 3. Codexの基本設定

既存の `~/.codex/config.toml` を残したまま、未設定のトップレベルキーだけを追加する。

```toml
approval_policy = "on-request"
approvals_reviewer = "auto_review"
sandbox_mode = "workspace-write"
project_doc_fallback_filenames = ["CLAUDE.md"]
project_doc_max_bytes = 131072
```

`approvals_reviewer = "auto_review"` は承認要求のレビュー担当を自動レビューにする設定であり、`approval_policy = "on-request"` の承認要求そのものを無条件に消す設定ではない。上限 `131072` は長い `CLAUDE.md` の切り捨てを避けるための例で、実ファイルサイズ以上に必要な場合だけ設定する。

設定後は新しいCodexセッションで確認する。

```sh
codex --version
codex exec --sandbox read-only "CLAUDE.mdの先頭と末尾にある規則を読み、末尾のcommit規則を一文で返す"
```

既存の `AGENTS.md` がある階層ではfallbackが選ばれない。Codex固有の入口が必要な場合は、そのファイルから対象の `CLAUDE.md` と参照rulesを読む手順を明示する。

## 4. 個人指示をsymlinkする

個人のClaude指示をCodexにも読む場合だけ実行する。既存のCodex `AGENTS.md` が無いことを確認してから作る。

```sh
test ! -e ~/.codex/AGENTS.md && test ! -L ~/.codex/AGENTS.md
ln -s ../.claude/CLAUDE.md ~/.codex/AGENTS.md
readlink ~/.codex/AGENTS.md
```

既存ファイルがある場合は削除・上書きせず、`~/.codex/AGENTS.md` に次のような短い接続指示を手で追加する。

```markdown
個人のClaude指示を正本とする。`~/.claude/CLAUDE.md` と、そこから参照されるrules・文書を作業前に読む。
Claude専用のtool・hook・Workflowは、Codexに同等機能があることを確認するまで実施済みと扱わない。
```

## 5. プロジェクトのskillを選択してsymlinkする

Claudeの全skillを平坦化せず、Codexで使うものだけをskillディレクトリ単位でリンクする。`SKILL.md` だけでなく、同じディレクトリの `scripts/`・`references/`・`assets/` も一緒に参照できる。

```sh
cd /path/to/project
mkdir -p .agents/skills
skill=example-skill
test -d ".claude/skills/$skill"
test ! -e ".agents/skills/$skill" && test ! -L ".agents/skills/$skill"
ln -s "../../.claude/skills/$skill" ".agents/skills/$skill"
readlink ".agents/skills/$skill"
```

個人skillは同じ考え方で `~/.agents/skills/<skill>` から `~/.claude/skills/<skill>` へリンクする。`.agents/skills` 全体をリンクする方法は、そこにある全skillがCodexで安全に読めると確認できた環境だけで使う。既存のplugin cacheを直接symlinkして更新を追跡する方法は採らない。

次のfrontmatter・実行機能を含むskillは、まずClaude側の正本をリンクし、Codexで意味が保証されない機能だけを実行しない。Codex用sidecarや本文変換ファイルは作らない。

- `user-invocable`、`argument-hint`、`context: fork`、`agent`、`background`
- `!` による動的シェル注入、`${CLAUDE_PLUGIN_ROOT}`、`mcp__claude` などのClaude専用参照
- Claudeのagent team、Workflow、transcript依存hook、外部状態を書き換えるscript

## 6. 必要なpluginだけをCodexへ導入する

Claudeの導入状態を全件複製せず、同じ公開marketplaceをCodexへ登録して、必要なpluginだけを有効にする。

```sh
codex plugin marketplace add maimuzo/maimuzo-claude-plugins
codex plugin add maimuzo-go@maimuzo-marketplace
codex plugin marketplace list
codex plugin list
```

実際のplugin名はmarketplaceの一覧で確認して置き換える。Claude側のplugin cacheをCodexの探索先へリンクしない。plugin更新後はCodex側のmarketplaceも更新し、新しいセッションでskill一覧を確認する。

```sh
codex plugin marketplace upgrade maimuzo-marketplace
```

CLIの版によって更新サブコマンドが異なる場合は `codex plugin marketplace --help` を先に確認する。pluginの導入成功はmanifestが読めたことを示すだけで、Claudeのfrontmatterやagentの実行意味まで一致したことを示さない。

## 7. ClaudeからCodexを呼ぶ経路を用意する

Claude Code内で公式pluginを導入し、Codexの認証を確認する。

```text
/plugin marketplace add openai/codex-plugin-cc
/plugin install codex@openai-codex
/reload-plugins
/codex:setup
```

用途に応じて `/codex:review`、`/codex:rescue`、`/codex:transfer` を使う。依頼には対象ディレクトリ、目的、変更範囲、検証方法、書き込み可否を明記する。親Claudeの会話・権限・skillがCodexへ完全継承されるとは扱わない。

## 8. HerdrからCodexへ直接切り替える

Claudeがレートリミットや停止状態でも、同じworktreeの書き込み担当が一つになるよう確認してからCodexを起動する。

```sh
herdr pane split --current --direction down --cwd /path/to/project --no-focus
# 返った pane_id を使う
herdr agent start codex-worker --kind codex --pane <pane_id>
herdr agent prompt codex-worker "目的、現状、未完了事項、検証方法を確認してから続行" --wait --timeout 300000
```

Claudeから `/codex:transfer` が返した `codex resume <session-id>` は、HerdrのCodex paneで実行する。移送できない場合は、新規Codexに作業ツリー・issue・既存ログから目的と完了条件を渡す。別の実装を同時に進めるときはworktreeを分ける。

## 9. 最低限の検証

外部issue・PR・本番データを変更しないread-only依頼で、入口・skill・連携を順に確認する。

```sh
test -L ~/.codex/AGENTS.md || test -f ~/.codex/AGENTS.md
test -L .agents/skills/<skill>
test -f .agents/skills/<skill>/SKILL.md
codex exec --sandbox read-only "利用可能なskillから、指定した領域に対応する本文を読み、選んだskill名と根拠だけ返す"
```

Claude側では `/skills` と `/codex:setup` を確認し、代表skillを明示指定して既存の本文が読めることを確認する。正本を更新した場合は、ClaudeとCodexの新しいセッションをそれぞれ起動して同じ更新値を返すことを確認する。Codex validatorで失敗するskillを、Claude側のfrontmatterを削って通す変更はしない。

## 10. 更新と復元

更新はClaude側の正本、Claude plugin、Codex pluginの順に確認する。skillのsymlinkは正本更新を即時に参照するが、Codexの指示・pluginの一覧は新しいセッションまたはreloadが必要になる。

復元時は作業中の追加変更を確認し、バックアップの `config.before.toml` とsymlink作成前の記録を使って個別に戻す。既存のClaude plugin・hooks・認証データを、この手順の復元対象として削除しない。復元後にClaudeとCodexを再起動し、`codex plugin list` と代表skillのread-only検証を再実行する。

## 11. 失敗時の判断

| 症状 | 対応 |
| --- | --- |
| `CLAUDE.md` が読まれない | 階層内の既存 `AGENTS.md`、fallback設定、上限値を確認し、新規セッションで再試行 |
| skillが一覧に出ない | symlinkのリンク先、`SKILL.md` の存在、対象scopeを確認。plugin cacheを直接編集しない |
| skillは出るが実行意味が違う | Claude拡張frontmatter・tool名・動的注入を分類し、Codex側では無理に実行しない |
| plugin導入後にClaudeの挙動が変わった | Claude側のpluginを戻さず、まずCodex側のplugin有効化を解除して切り分ける |
| hookを共有したくなった | Claude settingsをsymlinkせず、12節のCodex native宣言から同じscriptを呼ぶ。payload/outputが一致しないhookやtranscript依存hookは有効化しない |

この手順で解決しないものは「未対応」として記録する。Claudeの性能を保つため、共通化のためにClaude側の正本・frontmatter・既存hooksを弱めない。

## 12. hooks・permissionsを追加する場合

Codexの設定JSONとClaudeの設定JSONはsymlinkしない。Codex native hookの薄い宣言から、正本のscriptをgit root基準で直接呼ぶ。

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^Bash$",
        "hooks": [
          {
            "type": "command",
            "command": "python3 \"$(git rev-parse --show-toplevel)/.claude/hooks/block-merge-without-review.py\"",
            "timeout": 30
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 \"$(git rev-parse --show-toplevel)/.claude/hooks/check-reply-clarity.py\"",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

この例は候補であり、`~/.codex/config.toml` と `.codex/hooks.json` の両方へ同じhookを登録しない。Codexのhooks trustを確認し、`PreToolUse`のdeny、`Stop`の`decision:block`、timeout/fail-openをread-only fixtureで確認してから有効化する。Codexは`CLAUDE_PLUGIN_ROOT`をplugin hookへ設定するが、project hookでは`CLAUDE_PROJECT_DIR`が保証されないため、git rootを使う。

`check-verified-commands.py`のようにClaude transcriptの内部形状を読むhookは、このまま有効化しない。Codexのtranscriptは安定したhook APIではないため、必要なら`PostToolUse`で署名journalを作り、`Stop`で同じ判定coreを呼ぶ別設計をfixtureで検証する。Claude側の既存設定・script・テストは変更しない。

Claudeの`permissions.allow/deny`はCodexの`approval_policy`・`sandbox_mode`へ変換しない。通常の確認はCodex native設定を使い、危険操作を追加で止める必要がある場合だけ`PreToolUse`または`PermissionRequest`でdenyする。allowが広すぎる・workspace-writeで実行できる操作をdenyできない場合は、Claude側の性能を守るためCodex hookを有効化しない。

Codex pluginへhookを同梱する場合は、共有scriptと`hooks/hooks.json`をplugin sourceに置き、`.codex-plugin/plugin.json`の`hooks`エントリから参照する。Claude側の`.claude-plugin/plugin.json`は変更せず、Codex manifestは薄い宣言に限定する。plugin hookはインストール後もtrustが必要である。
