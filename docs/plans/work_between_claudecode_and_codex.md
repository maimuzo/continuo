# Claude Codeを主に、Codexを併用する運用案

Claude側を正本として維持し、Codexには公式の互換機能とsymlinkで接続する。
推奨は「公式連携プラグイン＋CLAUDE.mdのfallback読み込み＋既存marketplaceの再利用」。
先に保留したhooks・agents・rules・permissions・Workflowも、変換なしで共有できる境界を再調査して判断する。

調査基準日: 2026-09-18。導入確認日: 2026-09-14、実行検証日: 2026-09-15。Codex CLI 0.154.0・Herdr 0.8.2を使用。基本設定と選択した資産の導入は適用済み。構成は14節、復元方法は15節、実行検証と残件は16・17節、対象外解除後の比較調査と追加採用候補は18〜22節に示す。

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
| Claude固有の実行機能 | Codex native機能へ直接接続できるものを個別採用 | 本文・script正本はClaude側、宣言だけCodex側 |
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
| Claudeの環境操作・チーム制御 | usage確認、model切替、cosper-team | Codexのnative機能へ同じ本文を参照させる。Claude固有の自動制御はClaude側に残す |
| 隔離実行に依存するレビュー | co-reviewの `context: fork` / `Skill` 呼び出し | Codexのworker/explorer・subagent設定で近似できる範囲を検証する。自動同一性は保証しない |
| Claudeのブラウザに依存 | auto-debugのClaude in Chrome呼び出し | Codexに同じツールがあるとは扱わない |
| hook・session内部形式に依存 | completion・応答検査など | Codexのnative hooksへ同じscriptを接続できるものと、transcript依存で接続できないものを分ける |

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

## 13. 対象外解除後の調査対象

先に保留した機能を一括移植するのではなく、Claudeの正本を変えずにCodexへ接続できる最小経路を調べる。
「接続できる」と「同じ強制力・同じ失敗時挙動になる」は別の判定にする。

| 課題 | 今回確認する境界 |
| --- | --- |
| hooks・権限 | event、入力、出力、trust、timeout、失敗時挙動をClaude/Codex公式仕様で照合 |
| transcript依存 | `Stop`の共通payloadで足りるか、Codex transcriptを読む実装が必要かを分離 |
| fork・agent・Workflow | Codexのworker/explorer/subagentで正本Markdownを参照させる経路と、同一lifecycle不可の境界 |
| 厳密なpath条件 | CLAUDE.mdからの実行時参照、AGENTS入口、Rulesync等の生成方式を比較 |
| frontmatter・commands | 標準キーの共用、Claude拡張の無視、command本文のskill参照可否を確認 |
| permissions・MCP | Codexのapproval/sandbox/hooks/plugin MCPを使い、Claude設定を変換せずに同じ危険操作を止められるか確認 |
| plugin・Workflow | Claude pluginをnative導入し、Codex plugin manifestとshared hooksの追加が必要か確認 |
| インストール版同期 | native marketplace更新、symlink、薄い宣言ファイルのどれが正本更新に追随するか比較 |

Codexの現行Hooks仕様は `PreToolUse`・`Stop`・`SessionStart`・`PostToolUse` などを持ち、Claude互換の `hookSpecificOutput` も受け付ける。ただし `prompt`/`agent` handlerは実行されず、transcript形式は安定したAPIではない。従って、hooksは「全面対象外」ではなく、native eventへ直接接続できるscriptから段階的に採用する。[Codex Hooks](https://developers.openai.com/codex/hooks)

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
共有対象7 skillsのfrontmatter確認と、更新後の名前なし自動選択も実施した。指定marketplaceの54 skills全体には公式validatorも適用し、Codexの形式上の受入範囲を測定した。残るClaude側の既存開発フロー全般とfrontmatterキーの実行意味は、18〜22節のnative機能・実行時参照の条件付き採用として整理する。外部状態を変更する検証は、fixtureの採用条件を満たすまで実施済みとは扱わない。

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
| 既存hooksの警告 | Codex単独試験で `hooks.json` と `config.toml` の二重読込、およびSessionEndのtimeoutを3秒へ制限する警告が出た。試験自体は完了。重複読込を避ける薄い宣言と、Codex側のtrustを別途検証する |

Claudeのレートリミットを意図的に発生させる試験はしていない。代わりの合格条件である「移送済みの会話をClaudeに依存せずCodexで継続できること」は16節の試験で満たした。次に残る確認を行う場合も、lunaを明示し、300秒上限を付けてから開始する。

## 18. 未対応範囲の比較調査

本文・配布・実行意味を分けると、既存資産を壊さずに対応できる範囲が明確になる。
結論は「標準本文はsymlink、実行宣言はnative形式、hooksは互換payloadのscriptだけ同じ実体を接続する」である。

### 18.1 公式仕様で確認した境界

Claude CodeはAgent Skills標準を採用しているが、`user-invocable`、`context: fork`、`agent`、動的注入などを拡張している。[Claude Skills](https://code.claude.com/docs/en/skills) は、Claude外では標準の `name` / `description` / `license` / `compatibility` / `metadata` / `allowed-tools` だけを使うよう明記している。

OpenAIのClaude plugin移植仕様は、SKILL.md・scripts・references・resourcesを保持しつつ、Claude固有パラメータを除去・適応し、`userConfig`を自動実行しない。Codexのskillは `.agents/skills` のsymlinkを読める。[plugin移植](https://developers.openai.com/fr-FR/plugins/guides/submit-claude-plugin)、[skills](https://developers.openai.com/es-419/docs/build-skills)

Codexのcustom subagentは `.codex/agents/*.toml` の独自形式であり、ClaudeのMarkdown agentをそのままsymlinkできない。[subagents](https://developers.openai.com/es-419/docs/agent-configuration/subagents)

Claudeのhooksはcommand・HTTP・MCP・prompt・agentと終了コード2によるイベント制御を持つ。Codexも `PreToolUse`・`Stop`・`SessionStart`・`PostToolUse` などのcommand/MCP hooksを持ち、`hookSpecificOutput.permissionDecision` と `decision:block` を受け付ける。一方、Codexの `prompt`/`agent` handlerは現在実行されず、transcript形式は安定したAPIではないため、同じscriptでもeventごとの適合性を検査する。[Claude hooks](https://code.claude.com/docs/en/hooks)、[Codex hooks](https://developers.openai.com/codex/hooks)

### 18.2 候補の比較

| 候補 | 共有できる範囲 | 保守負担 | Claude性能へのリスク | 判断 |
| --- | --- | --- | --- | --- |
| Claudeを正本にして標準SKILL.mdをsymlink | 本文・scripts・references・resources | 最小 | 共有対象を選べばなし | **採用** |
| [OpenAI migrate-to-codex](https://github.com/openai/skills/tree/main/skills/.curated/migrate-to-codex) | Claudeのinstructions・skills・MCP・subagents・command hooksをCodex形式へ投影 | 小〜中 | hooksはcommand/event/timeoutを機械生成するが、未対応event・matcher・async・prompt/agentを落としてレポートする。設定変更時は再生成が必要 | **hooks設定の一回生成に採用** |
| [Vercel Skills CLI](https://github.com/vercel-labs/skills) | Agent Skills対応のskill配布。symlinkが推奨 | 小 | Claude plugin/rules/agents/hooksのnative宣言までは扱わない | 補助採用候補 |
| [sync-claude-skills-to-codex](https://github.com/ariccb/sync-claude-skills-to-codex) | Claude plugin cacheと個人skillをCodexへsymlink | 小 | plugin更新でリンク再作成が必要。frontmatter意味は未変換 | 参考採用。全面導入しない |
| [Rulesync](https://github.com/dyoshikawa/rulesync) | rules・commands・subagents・skills・hooksをimport/生成/convert | 中〜大 | 生成物と変換差分がClaude側へ影響し得る | 不採用。生成物の保守が条件に反する |
| [Ruler](https://github.com/intellectronica/ruler) | `.ruler`から各agentの設定を生成 | 中 | CLAUDE.mdを生成物にするため正本変更が大きい | 不採用 |
| [agentsync](https://github.com/spxrogers/agentsync) / [aitoolsync](https://github.com/EvanL1/aitoolsync) | 多agentへの投影と差分報告 | 中〜大 | subagent・command・hooksがlossy | 不採用。将来の比較対象 |
| [codex-hooks](https://github.com/hatayama/codex-hooks) | Claude settingsを読み、Codex session JSONLから通知・完了・中断へcommand hookを再生 | 小 | Claudeのblock/decision出力を解釈せず、独自wrapperが必要。native hooksの方が強い | 通知など副作用限定の補助候補 |
| Codex native hooks + 共有script | `.codex/hooks.json`またはplugin `hooks/hooks.json`だけを薄く追加し、既存scriptを直接呼ぶ | 小 | ClaudeとCodexで同じstdin/outputになるeventだけ採用。Codexのtrustとmanifestを別管理 | **追加採用候補** |
| Codex native hooks + dual-protocol core | 既存scriptをprovider検出型にし、Claude/Codexのpayload差を同じ正本で処理 | 中 | script変更がClaudeへも届くため、両方のfixtureと回帰試験が必須 | transcript依存hookの次段候補 |
| 自作の全面変換器 | 全拡張を個別変換 | 最大 | Claude仕様を変換都合に合わせる危険 | 不採用 |
| [openai/codex-plugin-cc](https://github.com/openai/codex-plugin-cc) | Claude内のCodex review・rescue・transfer | 小 | 既存Claude資産を変更しない | **採用済み** |

OpenAIのmigrate-to-codexは、hooksだけに限定すれば`.claude/settings.json`から`.codex/hooks.json`を機械生成できる。生成物はClaude側の正本ではなくCodex側の派生設定なので、Claude設定を変更したときだけ再実行・再検証する。command文字列は原文のまま写すため、`$CLAUDE_PROJECT_DIR`のようなClaude専用環境変数は、両製品で使えるgit root基準のcommandへ先に直す。agent・skill・MCPの全面変換には使わない。Vercel CLIは単体skillのsymlinkには適するがagentを扱わず、Rulesyncも共通の正本を別に持つ運用になるため採用しない。

## 19. 未対応項目への選定結果

全資産を同じ意味で共有することはできない。Claude側の正本をそのまま参照できるものだけを共有し、Codex固有形式が必要なものは変換せずClaude専用として残す。

### 19.1 採用する段階的アプローチ

1. `name`・`description`を中心とする標準SKILL.mdと、skillディレクトリ内の相対scripts/references/resourcesだけをsymlinkする。
2. Claude拡張frontmatterを含むskillも本文は同じsymlinkを読む。`user-invocable`、`argument-hint`、`context: fork`、`agent`、`background`、動的注入、Claude専用tool名はCodexで同じ意味になると扱わず、変換もしない。
3. Claude agentはClaude専用として残す。Codexで同じ役割が必要なときは、標準の`worker`/`explorer`へ依頼文で正本Markdownのパスを指定して読ませる。`.codex/agents/*.toml`のsidecarや変換ファイルは作らない。
4. 同じmarketplaceをCodexへ登録し、必要pluginだけをnative導入する。ローカルskillはsymlinkし、plugin本文は同じ配布元から各CLIへ導入する。Claude cacheのバージョン付きパスをCodexへ直結しない。
5. rulesはCLAUDE.mdのCodex向け読取指示から正本を参照する。hooksはClaude設定のcommandを両製品共通のpath表現へ直してから、公式migrate-to-codexで`.codex/hooks.json`を生成する。agents・Workflow・agent teamは標準worker/explorerへ正本Markdownを明示して近似する。
6. ClaudeのpermissionsはCodexのapproval/sandboxへ機械変換しない。危険操作の同じ拒否が必要なものだけ、正本設定を読み取るCodex PreToolUse/PermissionRequest hookとして追加検証する。

この方法なら、Claudeの既存frontmatter・plugin・hook script本文を共通実装として維持し、Codex側には公式変換で生成した宣言だけを置ける。Codexのcustom agent機能をClaude agentと同一視せず、Codex用の大きな実装ファイルを保守しない。設定変更時は変換を再実行する。

### 19.2 残件ごとの実現性

| 残件 | 実現性 | 今回の扱い |
| --- | --- | --- |
| 標準skill本文・付属資料 | 高い | symlinkを継続 |
| Claude pluginのCodex導入 | 高い | 既存marketplaceをnative導入 |
| Claudeの基本指示・個人指示 | 高い | fallbackとAGENTS.md symlink |
| 条件付きrulesの自動適用 | 部分的 | CLAUDE.mdのCodex読取手順。厳密化はRulesyncを再評価 |
| Claude agentの同一実行 | 低い | Codex側へ変換・sidecar化しない。必要時は標準agentに正本Markdownを明示的に読ませる |
| frontmatterの実行意味 | 部分的 | standardはそのまま、Claude拡張はCodexで保証しない。呼出制御はCodex native設定で必要時だけ補う |
| Claude hooksの同一挙動 | 中（event別） | command設定は公式migrate-to-codexで生成する。`block-merge` と `check-reply-clarity` は同じscriptを接続候補。`check-verified-commands` はClaude側もtranscriptをやめ、両製品のPre/PostToolUseで共通journalを作る再実装を検証 |
| permissions・MCP | 部分的 | Claude設定を変換せず、Codex native approval/sandbox/plugin MCPを使う。denyの同一強制はhookでfixture検証後に採用 |
| Workflow・agent team | 部分的 | Codexのsubagent/worker/explorerへ同じ正本Markdownを渡す。lifecycle・自動spawnの同一性は保証しない |
| Claude内からのCodex委任・会話移送 | 高い | 公式pluginを採用済み |

「未採用」は一括移植を意味しない。本文はsymlinkで共有し、Codex固有形式が必要なagent・hook宣言・permissionsは最小のnative接続だけを追加する。実行意味が一致しない部分は、Claude側の正本を守るため無理に近似しない。

## 20. 未完了項目と判定

残件を低リスク範囲だけで打ち切らず、Codex公式仕様とOSSの共有方式を比較した。
本文・実装の変換は行わず、hookの宣言だけは公式変換器で生成し、scriptの共通化はClaude側にも適用する。

| 項目 | 判定 | 根拠・次の条件 |
| --- | --- | --- |
| skillの3群分類 | 完了 | 54件のfrontmatterキー・Claude専用参照・付属scriptを静的計測し、標準・Claude拡張・外部tool依存を判定できる材料を揃えた。結果は16・18節に記録 |
| 拡張frontmatter代表3件 | 完了 | `github-project-update`・`naming-brainstorming`・`co-reviewer`をfixtureで発見・本文読取。実行意味の同等性は保証しない |
| Claude agentのCodex共有 | 不採用 | Codex custom agentは`name`・`description`・`developer_instructions`必須のTOMLで、Claude Markdownをsymlinkできない。変換すると更新のたびに再生成・再検証が必要になるため、Claude専用として残す |
| Codexでのagent役割利用 | 限定対応 | Codexの標準`worker`/`explorer`へ、作業時だけ正本Markdownのパスを明示して読ませる。自動spawn・frontmatter・Claude専用toolsの同等性は保証しない |
| 条件付きrules | 保留（追加調査済み） | Codexの指示探索には`.claude/rules/`の`paths`条件と同じ自動適用機構がない。現行のCodex向け読取指示を正本へ残し、実タスクで漏れが観測されても本文変換は行わず、Codex側の入口だけを再検討する |
| hooksの段階接続 | 追加採用候補を選定 | 公式migrate-to-codexでcommand hook設定を生成できる。既存 `block-merge-without-review.py` と `check-reply-clarity.py` は同じscriptを接続できる見込み。`check-verified-commands.py` はClaude側もtranscript依存をやめ、両製品の共通Pre/PostToolUse journal方式へ再実装するか、採用を見送る |
| permissions | 追加検証 | Claude `permissions.allow/deny` とCodex `approval_policy`/`sandbox_mode` は別契約。危険操作のdenyだけをCodex native hookへ移し、通常の承認は既存のCodex設定を正本とする |
| commands・MCP | 部分採用 | command本文は標準skillへsymlinkできるものだけ選ぶ。MCPはClaude `.mcp.json`を変換せず、plugin native MCPまたはCodex側の既存接続を使う |
| Workflow・agent team | 限定採用 | Codexのworker/explorer/subagentへ正本Markdownを読ませる。Claudeの自動spawn・fork・権限lifecycleは同一化しない |

本文共有・plugin導入・Claudeからの委任・HerdrからのCodex直接利用・代表frontmatter検証に加え、保留していたhooks・permissions・Workflowまで比較した。追加採用は、既存scriptを直接呼ぶnative hook宣言、Codex標準agentへの正本参照、native approval/sandboxの併用に限定する。Claude側の本文・frontmatter・hook挙動を弱める変換は採らない。

## 21. 変換なしで残件を扱う手順

Claudeの正本を変更せず、Codexが必要とする範囲だけnative設定から実行時に参照する。
Codex用のagent TOMLや変換済み本文は作らない。薄いhook/plugin宣言は本文を持たない接続として許容する。

1. ローカルskillは`.agents/skills/<name>`からClaude側のskillディレクトリへsymlinkする。Codex公式もskillディレクトリのsymlink探索をサポートしている。
2. plugin skillはClaudeとCodexの両方で同じmarketplaceからnative導入する。Claude cacheの版付きディレクトリを直接symlinkしない。
3. Claude agentが本文で参照されたとき、Codexでは実行済みと扱わず、必要なら依頼文に正本Markdownのパスを渡して標準agentに読ませる。
4. rulesはCLAUDE.mdのCodex向け読取指示に従って必要なMarkdownを読む。`paths`条件が自動適用されたとは扱わない。
5. hooksはCodexの `.codex/hooks.json` またはplugin `hooks/hooks.json` から、git root基準で既存scriptを呼ぶ。設定と `~/.codex/config.toml` の二重登録は避け、Codexのtrust確認を完了する。
6. `PreToolUse`/`Stop`の入出力をClaude fixtureとCodex fixtureで比較する。transcript依存hookは両製品のsession JSONLを正本と見なさず、両製品のPre/PostToolUseから同じイベントjournalを作る共通実装として検証する。
7. permissionsは `approval_policy = "on-request"`・`sandbox_mode = "workspace-write"` をCodexのnative設定として維持し、Claude `allow/deny` は同じ設定へ機械変換しない。危険操作の追加denyだけhookで測定する。
8. 初めて使う組み合わせだけread-only fixtureで確認し、処理には300秒上限を付ける。Claude側の既存hookテストも再実行し、Codexでの不成立を理由にClaude本文を弱めない。

この運用なら、Claudeの更新時にCodex側の本文再変換を要求しない。CodexでClaude agentの自動選択・権限・lifecycleまで同じになるとは扱わず、必要なときだけ正本を読ませる。hook宣言やplugin manifestの変更は、本文の複製ではないが、Codex仕様変更時に再検証する。

## 22. 追加採用候補と優先順位

追加採用の第一候補は、同じscriptをCodex native hookから呼ぶことだ。
Claude専用payloadを前提にしたscriptは、そのまま共有せず、差分を測ってから次段へ進める。

| 優先 | 追加するもの | 方法 | 採用条件 |
| --- | --- | --- | --- |
| 1 | `block-merge-without-review.py` | Codex `PreToolUse` matcher `Bash` から同じscriptを呼ぶ薄い宣言 | `hookSpecificOutput.permissionDecision=deny`、`tool_input.command`、`gh` timeout、fail-openをfixtureで確認 |
| 2 | `check-reply-clarity.py` | Codex `Stop` から同じscriptを呼ぶ | `last_assistant_message`・`stop_hook_active`と`decision:block`が一致。`gh`題名取得失敗時もClaudeと同じfail-openを確認 |
| 3 | pluginの通知・session hooks | Codex pluginの`hooks/hooks.json`と`.codex-plugin/plugin.json`を薄く追加し、`CLAUDE_PLUGIN_ROOT`互換を利用 | plugin sourceの同じscriptがClaudeで変わらず、Codex trust・timeout・環境変数をfixtureで確認 |
| 4 | Claude `permissions.deny`の危険操作 | Codex `PreToolUse`/`PermissionRequest`へ正本設定を読み取るhookを追加 | `rm`等のdenyをworkspace-write下でも止め、allow/approvalの誤ブロックがないことを測定 |
| 5 | `check-verified-commands.py` | Claude/Codex双方の`PreToolUse`/`PostToolUse`から同じjournal writerを呼び、`Stop`で同じ判定coreを呼ぶ | transcriptをどちらの製品の内部JSONLとしても読まない。`session_id`・`tool_use_id`・`tool_input.command`を共通recordへ正規化し、拒否・失敗・並列実行・cleanupを両製品fixtureで確認。共通化できなければCodexでは採用しない |
| 6 | Claude agent / Workflow / commands | 変換せず、Codex標準worker/explorerへ正本Markdownを依頼文で渡す。commandは標準SKILL.mdへリンク可能なものだけ選ぶ | 手動引き継ぎで目的・制約・検証が再現し、Claude側の自動spawn/forkを壊さない |

従って、先に実施した内容へ直ちに足すのは、公式migrate-to-codexで生成したhook宣言と、共通payloadで動く既存scriptの範囲である。`check-verified-commands.py`はClaude側も含めた共通journalへの再実装を先に検証し、共通化できなければCodexでは採用しない。agent・Workflow・commands・MCPは、正本本文のsymlinkと実行時参照までを追加採用し、Codex固有の大きな変換ファイルは作らない。

追加調査のローカル測定では、3本の既存Python hook scriptを`py_compile`で検査し、Codex形式の`Stop` payloadを`check-reply-clarity.py`へ渡すとClaudeと同じ`decision:block` JSONが返り、`PreToolUse`の非対象Bashは無出力・終了0だった。一方、実機Codexのsession JSONLは`event_msg`・`response_item`等を持ち、`check-verified-commands.py`が読むClaude形式の`user`・`tool_use`列とは一致しない。従って問題はhook stdin/stdout全体ではなく、scriptが製品固有の内部transcriptを直接読んでいる部分である。共通journalへ再実装できるかを先に検証し、できない場合はこのhookだけCodex採用を諦める。
