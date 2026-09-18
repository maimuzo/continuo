# Claude Codeを主に、Codexを併用する運用案

Claude側を正本として維持し、Codexには公式の互換機能とsymlinkで接続する。
推奨は「公式連携プラグイン＋CLAUDE.mdのfallback読み込み＋既存marketplaceの再利用」。
高度なhooks制御と、このリポジトリ固有の移植設計は対象外とする。

調査基準日: 2026-09-18。導入確認日: 2026-09-14、実行検証日: 2026-09-15。Codex CLI 0.154.0・Herdr 0.8.2を使用。基本設定と選択した資産の導入は適用済み。構成は14節、復元方法は15節、実行検証と残件は16・17節、未対応範囲の比較調査と選定は18〜21節に示す。

## 1. 採用する構成

指示・スキル本文の編集先を増やさず、実行環境ごとの設定だけを最小限持つ。
プラグインは同じ配布元から両者に入れる。配布時のキャッシュが別でも、本文の二重保守にはならない。

| 対象 | 推奨方法 | 人が保守するもの |
| --- | --- | --- |
| ClaudeからCodexへの委任・レビュー | OpenAI公式 `codex-plugin-cc` | 通常は追加実装なし |
| プロジェクトの基本指示 | Codexのfallbackに `CLAUDE.md` を指定 | 既存のCLAUDE.mdのみ |
| 個人共通の基本指示 | CodexのグローバルAGENTS.mdから既存CLAUDE.mdへsymlink | 既存の個人CLAUDE.mdのみ |
| `.claude/rules/` | CLAUDE.mdにCodex向け読取手順を短く追記 | 既存rulesと短い接続指示 |
| `.claude/skills/` | 共有対象のskillディレクトリを `.agents/skills/` へsymlink | 既存のSKILL.mdと付属ファイル |
| 指定のClaudeプラグイン群 | 既存marketplaceをCodexにも登録し、必要なpluginを選択 | 既存の配布元・本文 |
| Claude固有の実行機能 | Claude側に残す。必要なものだけ後日対応 | 当面は変更なし |
| Claude停止時の継続 | HerdrからCodexを起動し、session resumeまたは公式import | 通常は追加実装なし |

OpenAI公式には、Claude互換manifestとmarketplaceの受け入れ、Codex CLIのplugin管理、会話を移送するimportがある。「Codex用manifestを全件自作する」ことを出発点にしない。設定本文のimportは今回の正本共有には使わない。[プラグイン仕様](https://developers.openai.com/plugins/build/plugins)、[CodexのPlugins](https://learn.chatgpt.com/docs/plugins)、[Import](https://learn.chatgpt.com/docs/import)

## 2. 普段は公式プラグインからCodexを呼ぶ

Claudeが作業の窓口を担い、Codexにまとまった調査・実装・レビューを渡す。
独自MCPサーバーやCLI実行ラッパーは初期構成に加えない。

採用するのは [openai/codex-plugin-cc](https://github.com/openai/codex-plugin-cc)。公式READMEはClaudeを使い続けながらCodexへ委任する用途を明示している。ローカルのCodex app-serverを使い、Codex直接起動と同じ認証・設定を参照する。

未導入の環境では、Claude Code内で次を実行する。導入済みなら再インストールせず `/codex:setup` から確認する。

```text
/plugin marketplace add openai/codex-plugin-cc
/plugin install codex@openai-codex
/reload-plugins
/codex:setup
```

| 用途 | Claude Code内での操作 |
| --- | --- |
| 差分の通常レビュー | `/codex:review --background` |
| 方針・設計を含む批判的レビュー | `/codex:adversarial-review --background 検証してほしい観点` |
| 実装・調査の委任 | `/codex:rescue --background 目的・対象・完了条件` |
| 進捗・結果の確認 | `/codex:status`、`/codex:result` |
| Claudeの会話をCodexへ引き継ぐ | `/codex:transfer` |

通常レビューはread-onlyで、任意の観点を渡す用途にはadversarial-reviewを使う。親の会話・skills・権限がすべて自動継承されるとは考えず、対象ディレクトリ、目的、変更範囲、検証方法は依頼に含める。Codex側の資産共有設定は別途必要である。

Codex側はChatGPTアカウントでログインして利用できる。APIキー利用も可能だが、契約枠で運用する場合は既存認証を確認し、APIキー経路へ切り替えない。料金・利用枠は [公式の認証説明](https://learn.chatgpt.com/docs/auth) と契約に従う。

## 3. CLAUDE.mdは移動せず読み込ませる

最も変更が少ないのはCodexの `project_doc_fallback_filenames` を使う方法である。
各リポジトリにAGENTS.mdを新設する必要がなく、Claude側の探索・読み込みは変わらない。

初期設定時に利用者またはセットアップ担当が、既存の `~/.codex/config.toml` のトップレベルへ次を追加する。ファイル全体を上書きせず、既存値があれば統合する。

```toml
project_doc_fallback_filenames = ["CLAUDE.md"]
project_doc_max_bytes = 131072
```

`131072` は本案の初期値であり公式推奨値ではない。指示が短ければ既定値でよい。Codexの既定上限は32KiBなので、長いCLAUDE.mdを共有するときは実サイズを測って調整する。上限引き上げは切り捨てを避ける対策であり、長文への遵守精度を保証しない。

探索は各ディレクトリで `AGENTS.override.md` → `AGENTS.md` → fallbackの順で、原則1ファイルを選ぶ。既存AGENTS.mdがあるとCLAUDE.mdは追加されない。開始ディレクトリまでの階層を読むため、Claudeの「後から触った下位ディレクトリの指示を読む」動作との完全一致も前提にしない。[公式のAGENTS.md仕様](https://learn.chatgpt.com/docs/agent-configuration/agents-md)

他のマシンにも設定なしで入口を配布したい場合だけ、リポジトリのルートに次の相対symlinkを置く。

```sh
ln -s CLAUDE.md AGENTS.md
```

実行前に同名ファイルがないことを確認する。既存AGENTS.mdへ強制上書きしない。fallbackとsymlinkを両方採る必要はない。CLAUDE.mdを `.claude/CLAUDE.md` に置くプロジェクトでは、symlinkの参照先をその実位置に合わせる。

## 4. グローバル指示とrulesの扱い

グローバル指示はsymlinkで共有し、rulesは既存ファイルを読む手順をCLAUDE.mdに足す。
rulesのパス条件がCodexで自動再現されるとは扱わない。

既存の個人CLAUDE.mdを共用でき、Codex側にAGENTS.mdがない場合の初回設定例:

```sh
ln -s ../.claude/CLAUDE.md ~/.codex/AGENTS.md
```

Codexのグローバル探索はプロジェクトのfallbackとは別である。すでにCodex固有の指示がある場合は上書きせず、そのAGENTS.mdを短い入口にして既存CLAUDE.mdを読むよう指定する。これは入口の差分管理であり、規則本文を複製する必要はない。[公式のグローバル指示](https://learn.chatgpt.com/docs/agent-configuration/agents-md)

Claudeは `.claude/rules/` 内のMarkdownを読み、`paths` による条件付き読み込みも行う。Codexの `.rules` はコマンド実行ポリシーであり、Claudeの自然言語rulesをそこへsymlinkしても代替にならない。[Claudeのmemory/rules](https://code.claude.com/docs/en/memory)、[CodexのRules](https://learn.chatgpt.com/docs/agent-configuration/rules)

接続指示は個人のCLAUDE.mdに一度だけ置けば、各プロジェクトへの重複追記を避けられる。今回はこちらを採用し、Codexにのみ適用すると明記する。プロジェクト単独で配布する場合の接続指示の例:

```markdown
## Codexで作業する場合

- 作業前に、このCLAUDE.mdに対応する .claude/rules/ 内のMarkdownを
  サブディレクトリも含めて列挙する。
- paths指定のない規則はすべて読み、paths指定のある規則は対象ファイルが
  条件に合う作業を始める前に読む。対象範囲が増えたら再確認する。
- このファイルから参照する文書は、参照元ファイルを基準に解決して読む。
- Claude専用ツール・実行機能を要する手順は、同等機能が利用できるか
  確認する。利用できない手順を実施済みとして扱わない。
```

これはモデルへの読取指示であり、Claudeと同じ自動注入機構ではない。個人の `~/.claude/rules/` も共有する場合は、同じ読取指示を個人CLAUDE.mdに置く。`@path` のimportは、Codexには参照先を明示的に読むよう指示する必要があり、symlinkだけでClaudeの展開機構まで移植できるわけではない。Codex側へrules本文を複製・変換する方式は採らず、Claudeでの通常の読み込みを維持するため、rules全件をCLAUDE.mdへ連結しない。

## 5. 単体skillsはディレクトリ単位でsymlinkする

SKILL.mdだけでなくscripts・referencesもまとめて共有する。
既存の `.agents/skills/` を残せる、skillごとのsymlinkを標準にする。

初回セットアップで、プロジェクトルートから実行する例。`example-skill` は実在する共有対象の名前に置き換える。リンク先の実在と、リンクを置くパスが未使用であることを確認し、既存ファイル・ディレクトリがあれば上書きせず整理する。

```sh
mkdir -p .agents/skills
ln -s ../../.claude/skills/example-skill .agents/skills/example-skill
```

```text
project/
├── CLAUDE.md                       # 指示の正本
├── .claude/
│   ├── rules/                      # 規則の正本
│   └── skills/example-skill/       # SKILL.mdと付属ファイルの正本
└── .agents/skills/example-skill -> ../../.claude/skills/example-skill
```

全skillsが共有可能で `.agents/skills` が未作成なら、`.agents/skills -> ../.claude/skills` というディレクトリ全体のリンクでもよい。両方式を重ねない。個人skillsも同様に `~/.agents/skills/<name>` から `~/.claude/skills/<name>` へリンクできる。

Codexは `.agents/skills/` と `~/.agents/skills/` を探索し、symlinkされたskillディレクトリをサポートする。初期コンテキストは名前・説明を中心に、本文は必要時に読む方式である。[公式Skills仕様](https://learn.chatgpt.com/docs/build-skills)

Claude向け本文はそのまま残す。コードや資料の相対参照はskillディレクトリ基準に整理し、`$CLAUDE_PLUGIN_ROOT` などが必須なら、単体skillとしての共有対象からいったん外す。グローバルな絶対symlinkをリポジトリへコミットしない。

## 6. 指定のプラグイン群は同じmarketplaceから導入する

既存のClaude形式をCodexが読める経路を先に使う。
Claudeのインストールキャッシュを直接リンクする方式は、標準運用にはしない。

対象はユーザー指定の [maimuzo-claude-plugins](https://github.com/maimuzo/maimuzo-claude-plugins)。手元の同リポジトリのcleanなcheckout、commit `01f58ad6fa19bdbe2a6c2687c788c2737f05e873` を確認した。marketplaceは14プラグイン、SKILL.mdは54件。最新版の全件実行結果ではなく、構成を判断するための確認値である。

Codex CLI 0.154.0のヘルプで確認できた導入構文は次のとおり。初期セットアップで実行し、Codexの新規セッションから確認する。

```sh
codex plugin marketplace add maimuzo/maimuzo-claude-plugins
codex plugin add maimuzo-go@maimuzo-marketplace
```

これは指定された配布元を再利用する具体例であり、全14件を無条件に導入する指示ではない。Codexの `/plugins` から必要なものを選ぶ運用でもよい。

根拠は、公式仕様のClaude互換manifest受け入れと既存marketplace互換、および公式実装の `.claude-plugin/plugin.json` 対応である。ただしmanifestを読めることと、個々のskill・agentの実行意味が一致することは別である。[公式パッケージ仕様](https://developers.openai.com/plugins/build/plugins)、[公式manifest実装](https://github.com/openai/codex/blob/main/codex-rs/core-plugins/src/manifest.rs)

更新時はClaude側を通常どおり更新し、Codex側でもmarketplaceを更新する。

```sh
codex plugin marketplace upgrade maimuzo-marketplace
```

このコマンドはGit marketplaceのsnapshot更新用である。インストール済みpluginの反映状況は `/plugins` と新規セッションで確認する。Claudeの更新だけでCodexのcacheまで即座に変わるとは扱わない。双方の更新確認を一つの運用手順にすれば、本文の二重保守は発生しない。

## 7. プラグイン内のどこまで共有するか

共有可否はプラグイン名ではなく、中の手順が必要とする機能で判断する。
Claudeの機能を削って全skillを最低共通機能へ落とす方法は採らない。

| 資産の種類 | 指定プラグイン内の例 | 判断 |
| --- | --- | --- |
| コーディング規約・検討手順 | Go/TSガイド、命名・設計根拠など | 本文を共有する候補。対象プロジェクトとの適合は別に確認 |
| CLIやMCPを使うワークフロー | issue操作、Pencil、RUCMなど | 依存コマンド・MCP接続・実行手順が揃うものを選択 |
| Claudeの環境操作・チーム制御 | usage確認、model切替、cosper-team | Claude専用として残す |
| 隔離実行に依存するレビュー | co-reviewの `context: fork` / `Skill` 呼び出し | 同等の隔離実行を検証するまでは移植対象外 |
| Claudeのブラウザに依存 | auto-debugのClaude in Chrome呼び出し | Codexに同じツールがあるとは扱わない |
| hook・session内部形式に依存 | completion・応答検査など | 13節の別課題 |

たとえばGo/TSプラグインには規約だけでなく、Claudeのhookやagent利用を説明するskillもある。プラグイン単位で「全部共通」と判定しない。Codex側で不要なskillを無効化するか、少数だけ必要なら5節の選択リンクを使う。[skillの無効化設定](https://learn.chatgpt.com/docs/build-skills#enable-or-disable-local-codex-skills)

`user-invocable`、`disable-model-invocation`、`allowed-tools`、`context: fork`、`agent`、`model`、`$ARGUMENTS`、動的シェル展開はClaudeの拡張と実行契約を含む。SKILL.mdが読めるだけで同じ効果があると判断しない。Claude側のfrontmatterは保持し、必要な差分だけ同じskillの本文または追加メタデータで扱う。[ClaudeのSkills仕様](https://code.claude.com/docs/en/skills)

Codexの暗黙呼び出しを止める場合は `agents/openai.yaml` の `policy.allow_implicit_invocation: false` が使える。ただし、これは `user-invocable: false` の置き換えではない。前者は自動選択、後者はClaudeの利用者による呼び出し可否に関係し、意味が異なる。

## 8. キャッシュへのsymlinkを第一候補にしない理由

symlinkで同じ実体を読むことはできても、pluginの有効化・更新・名前空間までは共有されない。
既存の配布機構を使うほうが、今回の運用全体では保守量が少ない。

Claudeのplugin cacheは通常 `~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/` のようにバージョンを含む。そこへリンクを固定すると更新に追従せず、残った旧版を読み続ける可能性がある。インストール登録と有効化も同一ではない。[Claudeのプラグイン仕様](https://code.claude.com/docs/en/plugins-reference)

また、指定プラグインではGoとTSの両方に `ecc-testing` など同名skillが存在する。単体skillsとして平坦化すると衝突しやすく、リンク名だけ変えてもSKILL.mdの `name` は変わらない。pluginの単位と名前空間を保つことに利点がある。

それでも「Claudeがいま導入している版と必ず同じ実体を読む」が必須なら、小さなリンク管理機能を追加する。責務は有効なインストール先の解決、選択対象のリンク更新、名前衝突・参照切れの検査に限定する。実行機能のエミュレーターにはしない。

この場合はuser/project/localの有効範囲、更新後のリンク差し替え、アンインストール後の扱いまで必要になる。初期構成では自作しない。native plugin導入と単体symlinkの両方から同じskillを重複登録することも避ける。

## 9. Claudeが止まったらHerdrからCodexを使う

事前にCodexの認証と共有設定を済ませておけば、切り替えのためにClaudeの復帰を待つ必要はない。
「Codexに委任済みの仕事」と「Claudeだけが持つ会話」は別の経路で引き継ぐ。

| 状態 | 切り替え方法 |
| --- | --- |
| Codexへ委任済み | `/codex:result` 等で得たsession IDを `codex resume <session-id>` へ渡す |
| Claudeがまだ応答できる | `/codex:transfer` が返すresumeコマンドをHerdrのshellで実行 |
| Claudeが既にレートリミット | Codexを直接起動し `/import` からClaudeの会話を選ぶ |
| import非対応版・対象会話なし | 新規Codexへ目的・現状・未完了事項を渡し、作業ツリーと既存記録を確認させる |

`/codex:transfer` はClaudeの会話から継続可能なCodex threadを作る公式機能である。Codexの `/import` はローカルセッションで利用できるが、実行中タスク・remote session・ローカルapp-server daemon接続中には利用できない。CLIの会話importは直近30日・最大50件という範囲がある。[公式連携README](https://github.com/openai/codex-plugin-cc)、[公式Import仕様](https://learn.chatgpt.com/docs/import)

Herdrの既存shell paneで `codex` または `codex resume <session-id>` を実行すればよい。別の空いているpaneをエージェントとして起動する場合の構文例:

```text
herdr agent start codex-worker --kind codex --pane <pane-id>
herdr agent start codex-worker --kind codex --pane <pane-id> -- resume <session-id>
```

後者は前者との選択であり、同じpaneへ続けて実行しない。pane IDはHerdrが返した実値を使う。構文はHerdr 0.8.2の `herdr agent` で確認した。[Herdr](https://github.com/herdrdev/herdr)

同じ作業ツリーで書き込み担当を交代するときは、前の担当の書き込みが止まったことを確認する。同時に別の実装を進める場合はworktreeを分ける。引き継ぎは目的・判断・変更・テスト状態の再確認を含み、会話importが完全な実行状態移送になるとは扱わない。

## 10. 公式importの使いどころ

importは「同じファイルを読む」以外の公式選択肢であり、特に会話の救出に使う。
設定の継続共有は、importとsymlinkが同じ出力先を管理しないようにする。

公式importは指示・skills・plugins・MCP・commands・subagents・hooksなどを対象にし、元のClaude設定を変更しない。ただし、Codex側の生成物を作るため、正本をsymlinkで参照する今回の運用には使わない。会話移送だけは設定本文の複製を伴わないため利用できる。[公式Import仕様](https://learn.chatgpt.com/docs/import)

ただし、CLI中心のHerdr運用で、自動更新がどの資産・衝突・削除まで扱うかを公式の概要だけから保証できない。Claudeの任意の拡張手順も完全変換されるとは書かれていない。本案では次のように使い分ける。

- 指示と単体skillsは3〜5節の直接参照を基本にする。
- pluginsは6節の同じmarketplaceからの導入を基本にする。
- 会話移送にはimportを利用する。MCP・subagent・hooksの初期移行には使わない。
- デスクトップアプリの自動同期を採る場合も、同じ資産のsymlinkと生成物を重ねず、本文共有を優先する。

## 11. 既存ツールと実践記事の比較

必要な部品は既に存在する。今回は公式機能を中心にし、共通形式への変換はしない。
rulesの管理が複雑になっても、正本本文の複製を増やさず、Codex側の入口と実行時参照だけを再検討する。

| 候補 | 役割 | 今回の判断 |
| --- | --- | --- |
| [OpenAI codex-plugin-cc](https://github.com/openai/codex-plugin-cc) | ClaudeからCodexへのレビュー・委任・会話移送 | 採用 |
| [公式Import](https://learn.chatgpt.com/docs/import) | 設定・会話の移行、desktopの自動更新 | 会話移送と選択的な初期移行に採用 |
| [Vercel Skills CLI](https://github.com/vercel-labs/skills) | 複数エージェントへのskills配布、symlink、更新 | 新たに取得する共通skill向け。既存Claude plugin全体の代替にはしない |
| [Rulesync](https://github.com/dyoshikawa/rulesync) | rules等のimport・生成・直接変換 | 不採用。生成物の保守が条件に反する |
| [Ruler](https://github.com/intellectronica/ruler) | 共通指示を各エージェントへ配布 | `.ruler/` 中心への移行は今回のClaude主軸に対して変更が大きい |
| [codex-in-claude](https://github.com/briandconnelly/codex-in-claude) | MCP経由のCodex委任・レビュー | 公式連携と重複するため初期構成には追加しない |

Rulesyncには共通 `.rulesync/` を正本にする方式と直接変換があるが、どちらもCodex側の生成物または別の正本を持つ。生成後の同期タイミング・出力先の所有・条件付きrulesの意味を別途保守することになるため、今回の小さな接続指示より管理対象が増え、不採用とする。

Vercel Skills CLIのsymlinkはcanonical copyを各エージェントから参照する仕組みであり、Claudeが導入した任意のplugin cacheをそのまま継続追跡する機能と混同しない。[作者のREADME](https://github.com/vercel-labs/skills)

実践例として、John Davenportの [One Skill, Three Platforms](https://dev.codemyspec.com/blog/skill-portability) は `.claude/skills/` を正本にしてCodexへsymlinkする同種の運用を紹介している。方向性は採るが、記事の全frontmatter互換表やコマンド例を仕様として採用せず、公式文書と相対パス計算で確認する。記事中の「共通機能だけに絞る」という方向は、Claudeの機能を落としたくない今回の条件にはそのまま採らない。

## 12. 導入順序と合格条件

小さな共有設定から始め、ClaudeとCodexの両方で同じ資産を使えることを確認する。
確認が終わるまで、plugin全件移植や同期ツールの開発へ広げない。

1. Codexの認証と公式連携プラグインを確認する。
2. CLAUDE.mdのfallbackと必要な読み込み上限を設定する。
3. rulesの読取指示を既存CLAUDE.mdに一度追加する。
4. 単体skillを一つsymlinkし、同じmarketplaceから小さなpluginを一つ導入する。
5. Claude内の委任と、Herdr上のCodex直接起動の両方で使う。
6. 確認済みの対象だけ増やす。Claude専用資産はそのまま残す。

| 確認 | 合格条件 |
| --- | --- |
| 指示 | CLAUDE.md先頭・末尾の規則と参照先を実際に確認でき、切り捨てがない |
| rules | 無条件rulesを読み、対象を変えたとき条件付きrulesも確認する |
| skills | 明示呼び出しと適切な暗黙選択が働き、付属scripts/referencesを解決できる |
| 更新 | 正本の一箇所の編集を、各方式の反映タイミング後に両者が読む |
| plugin | 必要skill・依存ツールが利用でき、同名skillの混同がない |
| 引き継ぎ | Claudeが応答しなくてもCodexを起動でき、対象の仕事を継続できる |
| Claude側の維持 | 既存の呼び出し・frontmatter・段階的読み込みが変わらない |

ロードしたというモデルの自己申告だけでなく、実ファイルの参照、具体的な規則の適用、意図したskill選択まで確認する。symlinkは即時に実体を共有するが、起動時に読む指示は新規セッションで確認する。pluginはcache更新後の新規セッションで確認する。

## 13. 今回のスコープから外す課題

高度なhooks制御の同等性は、今回の導入条件にしない。
ただし「共有ができたから同じ機械的制約も効く」とは扱わない。

| 課題 | 後日確認する点 |
| --- | --- |
| hooks・権限の同等性 | 発火点、入力・出力、環境変数、trust、失敗時の挙動 |
| transcript依存の制御 | ClaudeとCodexの記録形式、安定した代替イベントの有無 |
| fork・agent・Workflow依存 | 隔離・役割・継続・ツール権限を維持できるか |
| 厳密なpath条件の適用 | rulesの読取指示で不足する場合の公式機能・Rulesync等の評価 |
| インストール版の完全同期 | native更新で十分か、cache参照を管理する小さな補助が必要か |

Codexにもhooksはあり、plugin互換用の環境変数なども存在する。「hooksはCodexにない」という前提は置かない。互換性を評価する際は [現行の公式Hooks仕様](https://learn.chatgpt.com/docs/hooks) を基準とする。公式連携プラグインの追加review gateも今回の基本構成には含めない。

## 14. 適用済みの構成と確認範囲

Claude側の資産本文を移動・複製せず、Codexの入口と標準plugin導入で接続している。
基本設定の読み込みは確認済み。12節の実運用上の合格条件を、すべて試験済みとは扱わない。

| 対象 | 適用内容・確認結果 |
| --- | --- |
| プロジェクト指示 | 3節のfallbackと上限を設定。再開後のセッションにCLAUDE.mdの先頭から末尾のcommit規則まで渡されたことを確認 |
| 個人指示 | 新設した `~/.claude/CLAUDE.md` にCodex限定のrules・import読取手順を集約。`~/.codex/AGENTS.md` は `../.claude/CLAUDE.md` へのsymlink |
| 単体skill | `.agents/skills/worker-briefing` は `../../.claude/skills/worker-briefing` へのsymlink。Codexの `skills/list` が正本のパスへ解決し、利用可能と返すことを確認 |
| 既存共有skill | 個人のHerdr skillは既に共有配置だったため変更なし。`skills/list` でも利用可能 |
| marketplace | 指定の既存配布元をCodex CLIで登録し、Goガイドのpluginを一つ導入。本文の変換・独自manifest追加なし |
| 選択的な有効化 | Goガイド・coding・git・patterns・security・testingの6 skillsが有効。Claude固有のagents・hooks・performanceの3 skillsはCodex設定で無効化 |
| 公式連携 | 導入済みの公式Claude連携plugin 1.0.6のsetup診断が `ready: true`。ChatGPT認証の検証とapp-server接続を確認。追加review gateは無効のまま |
| 既存設定 | 導入前後のconfig差分を確認。モデル・権限設定・既存hooksを変更していない |

`skills/list` の結果は読み込みエラーなし。これは発見・名前空間・有効状態の検証であり、暗黙選択や各skill内の全手順の互換性を保証しない。
通常の開発作業では本文と必要な付属資料を読み、Claude専用のWorkflow・ツール等は実行済みと扱わない。

Codex側で無効化したskillの設定はplugin cacheのバージョン付きパスを参照する。plugin更新後は、新しいパスで無効化が維持されているか `skills/list` と新規セッションで再確認する。設定の実値は個人バックアップ側に保持し、公開リポジトリへ個人のconfigを複製しない。

実タスク委任、Goガイド6種の明示指定、複数領域の暗黙選択、正本との同版一致、相対参照、会話移送とCodexでの継続、正本更新後の両製品の再読込は確認済み。今回の共有対象に付属scriptsはない。確認方法は16節に示す。
共有対象7 skillsのfrontmatter確認と、更新後の名前なし自動選択も実施した。指定marketplaceの54 skills全体には公式validatorも適用し、Codexの形式上の受入範囲を測定した。残るのはClaude側の既存開発フロー全般と、frontmatterキーの実行意味の完全同等性であり、外部状態を変更しない今回の検証では保留する。詳細は17節に示す。高度なhooks制御は13節の課題のままとする。

## 15. 復元方法

リポジトリの変更はcommitで戻し、ホーム配下の設定は個人バックアップで戻す。
Gitの取り消しだけでは個人設定は復元されない。後から加えた変更がある場合は丸ごと上書きしない。

導入前のcommitは `1355524d`。導入完了のcommitは、この文書と `.agents/skills/worker-briefing` の履歴から確認できる。
リポジトリの未コミット変更を保護したうえで、完了commitだけを `git revert <完了commit>` で取り消す。履歴や無関係の変更を消す `git reset --hard` は使わない。

個人設定のバックアップ先は `~/.codex/backups/claude-coexistence-20260914/`。
セットアップ担当が導入前に `config.before.toml`、導入後に `config.after.toml` と `CLAUDE.after.md`、復元用の `RESTORE.md` を保存している。
これらは個人設定を含むため、この公開リポジトリにはコミットしない。

復元時は `RESTORE.md` に従い、現在値と導入完了時のコピーを比較してから、今回のplugin・marketplaceだけを取り消す。configは同じディレクトリの一時ファイル経由で戻し、新設した個人CLAUDE.mdとAGENTS.mdは削除せずバックアップ先へ退避する。
導入前からあった公式Claude連携plugin・hooks・認証データは取り消し対象ではない。復元後は新しいセッションで開始する。

## 16. 実行検証の結果

ClaudeからCodexへの読み取り専用タスク委任と、Codexでのスキル選択・本文読取は動作した。
合格は下表の限定した観測結果を指す。コード実装時の全手順や、すべての依頼での自動選択までは保証しない。

| 検査 | 合格条件 | 観測結果 |
| --- | --- | --- |
| Claudeからの委任 | 公式コマンドがCodexを実行し、指定した規則と合言葉を返す | 合格。Herdrの新規対話型Claudeから `/codex:rescue --fresh --wait` を送信。公式helperの `task` がthreadを作成し `Turn completed`。合言葉 `COBALT-ORBIT-7391` が一致 |
| 会話移送と継続 | Claudeの検証会話を移送し、Codex単独で履歴中の合言葉を復元する | 合格。承認を受けて公式helperの `transfer --source <検証会話JSONL> --json` を実行。移送先を `codex exec --sandbox read-only resume --json <移送先thread>` で再開し、合言葉を再提示せず `COBALT-ORBIT-7391` と回答。両処理とも300秒以内に終了、再開側は `turn.completed` |
| 委任先の指示・相対参照 | CLAUDE.mdの規則とworker-briefingの参照文書を実際に読む | 合格。helperログに読取コマンドとexit 0。禁止起動方法・commit形式が実ファイルと一致。主担当も該当箇所を再照合 |
| 暗黙のスキル選択 | 名前を渡さず、5領域の問いに対応する本文を読んで答える | 合格。関数コメント・エラー処理・セキュリティ・テスト・パターンの問いから対応する5種を選択。JSONLに各SKILL.mdの読取と回答を記録 |
| 明示的なスキル指定 | 導入済みGo6種の名前を渡して各本文の規則を返す | 合格。同じ検証threadをresumeし `$<plugin>:<skill>` の形式で6種を指定。既読5種を再利用し、未読のgitガイドを読んで6行の回答を返す |
| Claude側のskill回帰 | 既存Claudeから共有対象の本文を読める | 合格。Herdr上のClaude検証paneでGo6種を明示指定し、各SKILL.mdから1規則ずつ返す処理がexit 0で完了 |
| 更新後の暗黙選択 | skill名を渡さず、更新後も対象領域に合うskillを選ぶ | 合格。名前なしのcommit前セキュリティ確認依頼で `ecc-security` を選択し、選択理由とテスト併用方針を返した。別試験ではgitガイド単独の依頼から `ecc-git` を選択 |
| 共有対象frontmatter | Claude由来のキーをCodexが保証する範囲を区別する | 合格。共有対象7 skillsを静的監査し、`user-invocable` は7件、`allowed-tools` はworker-briefingの1件のみ。いずれもCodexでClaudeと同じ意味になるとは扱わない |
| marketplace全体の形式検査 | 指定配布元のskillがCodex validatorで受け入れられる | 限定結果。54件中5件がvalid、49件が失敗。失敗は `user-invocable` 43件、`argument-hint` 3件、`context` と `user-invocable` の併用1件、YAML不正1件などで、Claude側本文は変更していない |
| marketplace付属資産の分類 | scripts・MCP・外部CLI依存を把握し、無条件共有しない | 合格。skill配下に9個の実行scriptがあり、`mcp__claude`・`claude -p`・`gh`・Claude専用agent/fork参照を静的に特定。外部状態を変更するscriptは実行していない |
| frontmatter runtime | Claude固有キーを含むskillのCodex発見・本文読取を確認する | 合格。隔離fixtureの `github-project-update`・`naming-brainstorming`・`co-reviewer` は3件とも発見・本文読取に成功したが、`allowed-tools`・`argument-hint`・`context: fork`・`user-invocable` の意味は確認できなかった |
| Claude clean runtime | 起動前からfixture内symlinkがあるClaudeで通常skillを読む | 合格。新規Claude sessionで `naming-brainstorming` を発見・明示実行し、他2件もReadできた。外部アクセス・gh・スクリプト・ファイル変更・実レビュー起動は行っていない |
| Claude/Codexの本文一致 | Go6種の同一バージョンがバイト単位で一致 | 合格。両cacheの `0.1.0/skills/<skill>/SKILL.md` を `cmp -s`。6種すべてexit 0 |
| 相対参照の解決 | worker-briefingの参照先が正本基準で存在する | 合格。symlinkの実体を確認し、参照先6種類を存在確認 |
| 付属scriptsの実行 | 共有対象にscriptsがある場合のみ実行を確認 | 該当なし。Go6種・worker-briefing・Herdrの8ディレクトリを隠しファイル込みで列挙し、実行scriptsは0件 |

暗黙選択は、利用可能なスキルから選ぶよう依頼した限定試験である。実装中に追加の案内なしで必ず選ばれることの証明ではない。validator検査は54件すべてに行ったが、runtimeの本文読取は3件、frontmatter意味の検証は行っていない。
明示指定はCLIのプロンプト内の名前指定であり、対話画面の `$` 候補一覧・slashコマンドの挙動までは試していない。

Codex単独の試験は `codex exec --sandbox read-only --json <検証依頼>`、継続は `codex exec --sandbox read-only resume --json <検証thread> <明示指定の依頼>` を使用。どちらも終了コード0と `turn.completed` を確認した。
検証ログは個人環境の `~/.codex/backups/claude-coexistence-20260914/verification-20260915/` に保持し、個人パスや読取内容を含むJSONL全体は公開しない。Claude側の証拠は、今回の検証専用セッションの公式helper実行結果にある。

`--wait` を指定したが、Claudeは実際にはsubagentをbackgroundで起動し、結果の前後にも説明を付けた。委任と結果受領は成功したものの、公式コマンド本文の「foreground実行」「結果だけをそのまま返す」という指示の完全遵守は確認できなかった。依頼文による制御を機械的な保証と扱わない。

以降の検証処理には300秒の実行上限を付ける。Herdrの状態待ちだけでなく実処理にも期限を設定し、超過時は今回起動した検証プロセス群だけを停止して報告する。`idle` は入力待ちであり、処理が継続していると扱わない。今回のluna実行でもこの上限を適用し、処理は上限前に完了した。

## 17. 検証で残った制約と次の確認

スキル本文を共用できても、Claude固有のagent・設定・実行環境まで共用されたわけではない。
移送は確認済み、設定更新や全般的な回帰試験は未実施として明確に区別する。

| 項目 | 状態・扱い |
| --- | --- |
| 会話移送の範囲 | 今回確認したのは承認された検証会話1件だけ。JSONL全体には設定等が含まれ得るため、他の既存会話を無断で移送しない。slashコマンドの画面操作ではなく、同コマンドが使う公式helperから試験した。移送先を300秒上限で再開し、履歴から合言葉を復元できた |
| 有効スキル内のClaude固有agent | git・security・testingの本文7行にplanner / tdd-guide / code-reviewer / security-reviewer / e2e-runnerの使用指示がある。今回のGo pluginにはそのagent定義がない。Codexでの同等手順を確認せず、実施済みにしない |
| Claude固有設定 | gitガイドに `~/.claude/settings.json` のattribution設定への依存がある。Codexへ同じ設定が継承されるとは扱わない。明示指定試験のCodex回答もこの非互換を指摘 |
| プロジェクト前提 | Goガイドの `go run ./cmd/manager/` と `bash unit_and_e2e_test.sh` はplugin同梱資産ではない。対象プロジェクトで存在を確かめる。共有設定の検証を製品固有の修正へ広げない |
| 正本更新の反映 | 隔離fixtureのCLAUDE.md・symlink先skill・相対参照の3値を更新前の値から更新後の値へ変更。Codexの新規sessionとClaudeの新規Herdr sessionが、`PROJECT-R2-9136`・`SKILL-R2-2748`・`REFERENCE-R2-6502` を同じく返した。運用中の正本を編集する試験は未実施 |
| 残りの自動選択・回帰 | 共有対象の更新後暗黙選択は `ecc-git` と `ecc-security` で確認済み。Claude側の既存開発フロー全般と、各frontmatterキーの実行意味の完全同等性は保留。issue操作・auto-debug・co-review等を実行すると外部書込みや編集が発生するため、読み取り専用の今回スコープでは試験しない。今回の限定試験を全互換と一般化しない |
| 既存hooksの警告 | Codex単独試験で `hooks.json` と `config.toml` の二重読込、およびSessionEndのtimeoutを3秒へ制限する警告が出た。試験自体は完了。hooks変更は今回の範囲外で、既存設定を変更していない |

Claudeのレートリミットを意図的に発生させる試験はしていない。代わりの合格条件である「移送済みの会話をClaudeに依存せずCodexで継続できること」は16節の試験で満たした。次に残る確認を行う場合も、lunaを明示し、300秒上限を付けてから開始する。

## 18. 未対応範囲の比較調査

本文・配布・実行意味を分けると、既存資産を壊さずに対応できる範囲が明確になる。
結論は「標準のSKILL.mdはsymlink、Claude拡張も本文をそのまま参照し実行意味は保証しない、agentsとhooksはClaude側に残す」である。

### 18.1 公式仕様で確認した境界

Claude CodeはAgent Skills標準を採用しているが、`user-invocable`、`context: fork`、`agent`、動的注入などを拡張している。[Claude Skills](https://code.claude.com/docs/en/skills) は、Claude外では標準の `name` / `description` / `license` / `compatibility` / `metadata` / `allowed-tools` だけを使うよう明記している。

OpenAIのClaude plugin移植仕様は、SKILL.md・scripts・references・resourcesを保持しつつ、Claude固有パラメータを除去・適応し、`userConfig`を自動実行しない。Codexのskillは `.agents/skills` のsymlinkを読める。[plugin移植](https://developers.openai.com/fr-FR/plugins/guides/submit-claude-plugin)、[skills](https://developers.openai.com/es-419/docs/build-skills)

Codexのcustom subagentは `.codex/agents/*.toml` の独自形式であり、ClaudeのMarkdown agentをそのままsymlinkできない。[subagents](https://developers.openai.com/es-419/docs/agent-configuration/subagents)

Claudeのhooksはprompt/agent hookと終了コード2によるイベント制御を持つ。一方、Codexのhooksは現行仕様ではcommandとmcp_toolが中心で、prompt/agent handlerは互換実行されない。[Claude hooks](https://code.claude.com/docs/en/hooks)、[Codex hooks](https://developers.openai.com/fr-FR/docs/hooks)

### 18.2 候補の比較

| 候補 | 共有できる範囲 | 保守負担 | Claude性能へのリスク | 判断 |
| --- | --- | --- | --- | --- |
| Claudeを正本にして標準SKILL.mdをsymlink | 本文・scripts・references・resources | 最小 | 共有対象を選べばなし | **採用** |
| [OpenAI migrate-to-codex](https://github.com/openai/skills/tree/main/skills/.curated/migrate-to-codex) | Claudeのinstructions・skills・MCP・subagentsをCodex形式へ投影 | 小〜中 | Codex側の生成物と再変換が必要。実行意味もlossy | **不採用** |
| [Vercel Skills CLI](https://github.com/vercel-labs/skills) | Agent Skills対応のskill配布。symlinkが推奨 | 小 | Claude plugin/rules/agents/hooksは対象外 | 補助採用候補 |
| [sync-claude-skills-to-codex](https://github.com/ariccb/sync-claude-skills-to-codex) | Claude plugin cacheと個人skillをCodexへsymlink | 小 | plugin更新でリンク再作成が必要。frontmatter意味は未変換 | 参考採用。全面導入しない |
| [Rulesync](https://github.com/dyoshikawa/rulesync) | rules・commands・subagents・skills・hooksをimport/生成/convert | 中〜大 | 生成物と変換差分がClaude側へ影響し得る | 不採用。生成物の保守が条件に反する |
| [Ruler](https://github.com/intellectronica/ruler) | `.ruler`から各agentの設定を生成 | 中 | CLAUDE.mdを生成物にするため正本変更が大きい | 不採用 |
| [agentsync](https://github.com/spxrogers/agentsync) / [aitoolsync](https://github.com/EvanL1/aitoolsync) | 多agentへの投影と差分報告 | 中〜大 | subagent・command・hooksがlossy | 不採用。将来の比較対象 |
| 自作の全面変換器 | 全拡張を個別変換 | 最大 | Claude仕様を変換都合に合わせる危険 | 不採用 |
| [openai/codex-plugin-cc](https://github.com/openai/codex-plugin-cc) | Claude内のCodex review・rescue・transfer | 小 | 既存Claude資産を変更しない | **採用済み** |

OpenAIのmigrate-to-codexは公式の変換器だが、Claude agentをCodex TOMLへ生成するため、Claude側の更新ごとに再変換・再検証が必要になる。さらに`tools`などはprompt guidanceへ落ち、実行意味も同一にならない。今回の「Claudeを正本にし、Codex用の別ファイルを保守しない」という条件と合わないため不採用とする。Vercel CLIは単体skillのsymlinkには適するがagentを扱わず、Rulesyncも共通の正本を別に持つ運用になるため採用しない。

## 19. 未対応項目への選定結果

全資産を同じ意味で共有することはできない。Claude側の正本をそのまま参照できるものだけを共有し、Codex固有形式が必要なものは変換せずClaude専用として残す。

### 19.1 採用する段階的アプローチ

1. `name`・`description`を中心とする標準SKILL.mdと、skillディレクトリ内の相対scripts/references/resourcesだけをsymlinkする。
2. Claude拡張frontmatterを含むskillも本文は同じsymlinkを読む。`user-invocable`、`argument-hint`、`context: fork`、`agent`、`background`、動的注入、Claude専用tool名はCodexで同じ意味になると扱わず、変換もしない。
3. Claude agentはClaude専用として残す。Codexで同じ役割が必要なときは、標準の`worker`/`explorer`へ依頼文で正本Markdownのパスを指定して読ませる。`.codex/agents/*.toml`のsidecarや変換ファイルは作らない。
4. 同じmarketplaceをCodexへ登録し、必要pluginだけをnative導入する。ローカルskillはsymlinkし、plugin本文は同じ配布元から各CLIへ導入する。Claude cacheのバージョン付きパスをCodexへ直結しない。
5. rulesはCLAUDE.mdのCodex向け読取指示から正本を参照する。hooks、Claudeの権限・Workflow・agent teamは今回共有しない。

この方法なら、Claudeの既存frontmatter・plugin・agents・hooksを変更せず、共有できる本文だけをCodexから参照できる。Codexのcustom agent機能をClaude agentと同一視しない代わりに、Codex用の変換ファイルを保守しない。

### 19.2 残件ごとの実現性

| 残件 | 実現性 | 今回の扱い |
| --- | --- | --- |
| 標準skill本文・付属資料 | 高い | symlinkを継続 |
| Claude pluginのCodex導入 | 高い | 既存marketplaceをnative導入 |
| Claudeの基本指示・個人指示 | 高い | fallbackとAGENTS.md symlink |
| 条件付きrulesの自動適用 | 部分的 | CLAUDE.mdのCodex読取手順。厳密化はRulesyncを再評価 |
| Claude agentの同一実行 | 低い | Codex側へ変換・sidecar化しない。必要時は標準agentに正本Markdownを明示的に読ませる |
| frontmatterの実行意味 | 部分的 | standardと拡張を分類し、無理に共用しない |
| Claude hooksの同一挙動 | 低い | 今回は対象外。別経路で再設計 |
| Claude内からのCodex委任・会話移送 | 高い | 公式pluginを採用済み |

「未対応」は一括移植を意味しない。高い項目だけを本文共有で使い、Codex固有形式が必要なagentとhooksはClaude側に残す。

## 20. 未完了項目と判定

残件を低リスク範囲だけで打ち切らず、Codex公式仕様とOSSの共有方式を比較した。
変換器はメンテナンス条件に反するため採用せず、symlinkと実行時参照だけを残す。

| 項目 | 判定 | 根拠・次の条件 |
| --- | --- | --- |
| skillの3群分類 | 完了 | 54件のfrontmatterキー・Claude専用参照・付属scriptを静的計測し、標準・Claude拡張・外部tool依存を判定できる材料を揃えた。結果は16・18節に記録 |
| 拡張frontmatter代表3件 | 完了 | `github-project-update`・`naming-brainstorming`・`co-reviewer`をfixtureで発見・本文読取。実行意味の同等性は保証しない |
| Claude agentのCodex共有 | 不採用 | Codex custom agentは`name`・`description`・`developer_instructions`必須のTOMLで、Claude Markdownをsymlinkできない。変換すると更新のたびに再生成・再検証が必要になるため、Claude専用として残す |
| Codexでのagent役割利用 | 限定対応 | Codexの標準`worker`/`explorer`へ、作業時だけ正本Markdownのパスを明示して読ませる。自動spawn・frontmatter・Claude専用toolsの同等性は保証しない |
| 条件付きrules | 保留（追加調査済み） | Codexの指示探索には`.claude/rules/`の`paths`条件と同じ自動適用機構がない。現行のCodex向け読取指示を正本へ残し、実タスクで漏れが観測されても本文変換は行わず、Codex側の入口だけを再検討する |
| hooksの同等化 | 対象外 | ユーザー指定どおり高度なhooks制御は今回実装しない。Claude hooksをsymlinkせず、別設計として残す |

本文共有・plugin導入・Claudeからの委任・HerdrからのCodex直接利用・代表frontmatter検証に加え、agentを変換しない境界まで確認した。未対応なのは、Codex固有形式が避けられないagentの自動実行、条件付きrulesの完全自動化、hooksであり、Claude側の性能と正本を守るため隔離する。

## 21. 変換なしで残件を扱う手順

Claudeの正本を変更せず、Codexが必要とする範囲だけ実行時に参照する。
Codex用のagent TOMLや変換済み本文は作らない。

1. ローカルskillは`.agents/skills/<name>`からClaude側のskillディレクトリへsymlinkする。Codex公式もskillディレクトリのsymlink探索をサポートしている。
2. plugin skillはClaudeとCodexの両方で同じmarketplaceからnative導入する。Claude cacheの版付きディレクトリを直接symlinkしない。
3. Claude agentが本文で参照されたとき、Codexでは実行済みと扱わず、必要なら依頼文に正本Markdownのパスを渡して標準agentに読ませる。
4. rulesはCLAUDE.mdのCodex向け読取指示に従って必要なMarkdownを読む。`paths`条件が自動適用されたとは扱わない。
5. 初めて使う組み合わせだけread-only fixtureで確認し、処理には300秒上限を付ける。hooksの同等化は別課題のままにする。

この運用なら、Claudeの更新時にCodex側の再変換を要求しない。CodexでClaude agentの自動選択・権限・lifecycleまで同じになるとは扱わず、必要なときだけ正本を読ませる。
