# CLAUDE.md

## 絶対条件：発言前の確認（最優先ルール）

**時間がかかってもいいから、すべての発言前に自分の発言に批判的にレビューし、不明瞭な点があれば追加調査して、全て確認できてから答えること。絶対条件。**

- ファイルの存在・内容・パスについて発言する前に、必ず Read / Glob / Grep / Bash で実態を確認する
- 「〜が未作成」「〜が壊れている」「〜が必要」などの問題指摘は、実際にファイルを確認してから行う
- 確認せずに推測で発言することは禁止
- **「〜してよいですか？」と確認した場合は、必ず返答を待ってから実行すること。返答なしに勝手に進めることは禁止**
- **指示の意図が不明瞭な場合は、「〇〇という理解でよいですか？」と自分の解釈を先に述べてから作業すること。**解釈が正しいか確認せずに作業を始めることは禁止

---

## このプロジェクトは何か

**`continuo` は、GitHub Projects v2 のボード1枚を見張り、issue ごとに git worktree を用意して、[herdr](https://github.com/herdrdev/herdr) の pane で Claude Code を対話モードで起動し、完了までを面倒見る常駐プロセスである。**Go で書く。

**名前は通奏低音（basso continuo）に由来する。**バロック音楽で、曲の最初から最後まで途切れず鳴り続け、全体の和声を支える低音パート。

**準拠する仕様は [openai/symphony](https://github.com/openai/symphony) の [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md)**（Apache-2.0）。

> **仕様はこのリポジトリに同梱しない。**再配布になり、ライセンスの扱いが増えるためである。
> **作業に使うときは各自が手元に置く**（`docs/spec/symphony/` は `.gitignore` 済み）。
>
> ```bash
> mkdir -p docs/spec/symphony
> curl -sL https://raw.githubusercontent.com/openai/symphony/main/SPEC.md -o docs/spec/symphony/SPEC.md
> ```
>
> **文書から参照するときは節番号を使う**（例: `SPEC.md` 6.2）。**行番号は使わない**（upstream で動く）。

**設計は [docs/plans/continuo_design.md](docs/plans/continuo_design.md) が正である。**設計の判断はすべてここに記録する。指示を待たない。

---

## 絶対に守る制約

### 1. `claude -p` は使用禁止

**従量課金になるため禁止。提案もしないこと。**Claude Agent SDK と Anthropic API の直叩きも同じ理由で対象外である。

別のコンテキストで作業させたい場合は **herdr の別 pane を開く**（このマシンでは herdr が常駐。`HERDR_ENV=1`）。

```bash
herdr pane split --current --direction down --cwd <ディレクトリ> --no-focus
# 返る JSON の result.pane.pane_id を使う
herdr agent start <名前> --kind claude --pane <pane_id>           # 検知されるまで待つ（既定30秒）
herdr agent prompt <名前> "<プロンプト>" --wait --timeout 120000  # idle/done/blocked まで待つ
herdr agent read <名前> --source recent-unwrapped --lines 50
```

**`herdr wait agent-status …` は存在しない**（herdr 0.8.0 で確認）。待機は `herdr agent wait <名前> --until <status>`。
**`pane run "claude"` で起動する経路も避ける。**`agent start` と違って起動完了を待たないため、直後に `agent wait` を呼ぶと `agent_not_found` で失敗する。

### 2. GitHub Projects v2 の project #3 は本番のボードである

104件の実データが入っている。**検証で書き込まない。**

**実機で確かめるための専用の環境がある。**ボードもリポジトリも issue もラベルも用意済みで、
**Status は API で動かせる。**在りかと使い方は [docs/test_environment.md](docs/test_environment.md) にある。
**この環境は消さない。**セッションをまたいで再利用する。

**とくに `updateProjectV2Field` を本番のボードで呼んではならない。**選択肢の指定は全件置き換えとして扱われ、**設定済みの Status の値が全部消える。**

**テスト用のボード（project #10）に対してだけは呼んでよい。**そこは実データを持たないので、選択肢を作り直しても失うものが無い。
**それ以外のボードでは、選択肢の追加は人間が GitHub の画面から行う。**

### 3. `~/.claude/projects/` 配下を消さない

調査を subagent に依頼するときは「調査結果の書き込み以外、変更・削除を一切禁止」と明示すること。パスの許可リスト／禁止リストは必ず穴が開く。

### 4. ファイルの書き換えは「一時ファイルへ書いてから差し替える」

**書き込む先をその場で開いて中身を空にしてから書いてはならない。**
**途中で落ちると、元の内容が失われる。**

```go
// してはいけない（O_TRUNC で中身を消してから書く）
os.WriteFile(path, data, perm)
os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)

// こうする（同じディレクトリの一時ファイルへ書き切ってから差し替える）
tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
// … 書く / Sync / Close / 権限を元に戻す …
os.Rename(tmp.Name(), path)
```

**一時ファイルは必ず書き込む先と同じディレクトリに作る。**
`os.Rename` が不可分なのは同じファイルシステムの中だけである。

**揃えられない箇所があるなら、その理由をコードのコメントに書く。**黙って例外にしない。
例: ロックファイル（差し替えるとロックが切れる）、追記だけのファイル。

### 5. 公開してよい情報かを常に判断する

**このリポジトリは PUBLIC であり、OSS として公開する予定である。**
**何かを書く前に「これは公開してよいか」を判断すること。**判断せずに書かない。

**絶対にコミットしないもの。**

| 何 | 例 |
| --- | --- |
| API キー・トークン | `ghp_…` / `sk-ant-…` / `github_pat_…` |
| tailnet のホスト名 | `*.ts.net` |
| 個人の絶対パス | `/Users/<名前>/…`（`~/` に直すこと） |
| 個人の環境に依存する設定 | プラグインの有効化・マーケットプレイスの登録（`.claude/settings.local.json` へ。`.gitignore` 済み） |

**例を書くときは架空の名前を使う。**

- **リポジトリ名**: `<owner>/<repo>` か `octocat/hello-world`。**自分の実在のリポジトリ名を書かない**
- **アカウント名**: `<ACCOUNT>` か `octocat`
- **そもそも特定の名前を書かずに済む書き方を先に探す**

> **実在のリポジトリ名やプラグイン名は、漏れても実害は無い**（公開情報である）。
> **だが読む人には「なぜこの人の名前が?」と映る。**架空の名前にしておけば、その疑問が生じない。
>
> **既に履歴へ入ってしまったものは、そのままでよい。**書き換えのために履歴を作り直さない。

### 6. continuo で continuo 自身を直すとき、hook の経路に触れたら人間に確認する

**この項目は、continuo に continuo 自身の issue をやらせるときにしか効かない。**
他のプロジェクトを continuo に任せている人には関係が無い。

**何が起きるか。**continuo は常駐プロセスである。`go build -o ~/.local/bin/continuo ./cmd/continuo` は
**rename で実行ファイルを差し替える**ので inode が変わり、**動いている continuo は、開いたままの古い実体で最後まで走り切る。**
**ところが Claude Code の hook は、turn ごとにそのパスを exec する**
（[internal/orchestrator/settings.go:352](internal/orchestrator/settings.go#L352) が
`<continuo のパス> hook --socket <パス> --pending-dir <パス>` を組み立て、issue ごとの設定ファイルへ書く）。
**つまり「本体は古い・hook は新しい」という混ざった状態が、ビルドするたびに必ず起きる。**

**何が壊れるか。**`continuo hook` のフラグ名を変える変更を入れた瞬間、
新しい実行ファイルの hook は**引数を受け取れずに exit 1 で落ちる**
（[internal/cli/cli.go:1280-1298](internal/cli/cli.go#L1280-L1298) が
`--socket` と `--pending-dir` の欠落と相対パスを、それぞれ exit 1 にしている）。
**古い本体は turn の終わりを永久に受け取れなくなる。**
**しかも本体には、自分が黙らされたことが分からない。**hook が1つも届かないことと、
Claude Code がまだ喋っている最中であることは、本体からは区別できない。

**やること。**

> **この状態を検知したら、人間に必ず確認すること。**
> **人間が問題ないので進めてと答えたら進めて良い。**
> **明示的に返答しないケースを含め、それ以外は決して進めないこと。**
> **（AI が勝手に hook 周りも仕様に含めた場合を含む）**

**最後の括弧が本体である。**issue に hook のことが1行も書いていなくても、
**作業中に手が hook の経路へ伸びたら、その時点で止まる。**「ついでに直した」を通さない。

**検知のしかた。**判定は変更したファイルのパスだけで行う。中身を読んで迷わない。

```bash
git fetch origin -q
{ git diff --name-only origin/main...HEAD   # commit 済みのもの
  git diff --name-only HEAD                 # まだ commit していないもの（staged / unstaged）
  git ls-files --others --exclude-standard  # 新しく足して、まだ追跡させていないもの
} | sort -u | grep -E '^(internal/socketpath/|internal/hookclient/|internal/hookserver/|internal/lock/|internal/orchestrator/settings\.go|internal/orchestrator/orchestrator\.go|internal/cli/cli\.go)'
```

**3つとも見るのは、`git diff --name-only origin/main...HEAD` だけでは素通りするからである。**
三点の `...` は commit 済みの履歴しか読まないので、**hook の引数を書き換えて、まだ commit していない状態では1行も返らない。**
**止まるべき場面で「触っていない」と読めてしまう**（実測で確認済み）。

**`main` ではなく `origin/main` を見る。**手元の `main` は取り込んでいないことがあり、
**そもそも手元に `main` が無い checkout では `fatal: ambiguous argument` になって、grep には何も渡らない。**
これも「触っていない」と見分けが付かない（[docs/releasing.md:284](docs/releasing.md#L284) と同じ理由である）。

**1行でも返ったら止まる。**それぞれ、なぜ止まるかは次のとおり。

| 触った場所 | なぜ止まるか |
| --- | --- |
| [internal/cli/cli.go](internal/cli/cli.go) の `hook` の引数 | `--socket` / `--pending-dir` が変わると、新しい hook が古い本体へ届かなくなる |
| [internal/orchestrator/settings.go](internal/orchestrator/settings.go) | hook のコマンド行を組み立てている場所そのもの |
| [internal/socketpath/](internal/socketpath/) | socket のパスの決め方。ずれると hook の宛先が消える |
| [internal/orchestrator/orchestrator.go:1148](internal/orchestrator/orchestrator.go#L1148) の `pendingDir` | continuo が落ちている間の hook の逃がし先の置き場所 |
| [internal/hookclient/](internal/hookclient/) と [internal/hookserver/](internal/hookserver/) | hook を送る側と受ける側の約束 |
| [internal/lock/](internal/lock/) | ロックファイルの扱い。新旧が同じ鍵を取り合う |

**人間に見せるもの。**次の4つを揃える。1つでも欠けたら、人間は可否を判断できない。

| 何を見せるか | 具体的に何を書くか |
| --- | --- |
| **どのファイルのどこを触るか** | 上の `git diff --name-only` の出力と、変える関数名・フラグ名 |
| **hook のどの経路に効くか** | 上の表のどの行に当たるか。issue ごとの設定ファイルのどこが変わるか |
| **止まったまま何もしないと何が起きるか** | **その issue が進まないだけである。**動いている continuo は壊れない |
| **進めて壊れたときの戻し方** | 下の3行。**古い実行ファイルへ戻すところまで書く** |

**壊れたときの戻し方。**

```bash
# 1. 動いている continuo を止める。hook が届かないので1回目の Ctrl+C は待たされる。
#    待たずに終わらせたいときは、もう一度 Ctrl+C を押す
# 2. hook が動いていた頃の commit へ戻し、同じパスへ入れ直す
git switch --detach <hook が動いていた commit> && go build -o ~/.local/bin/continuo ./cmd/continuo
# 3. 立て直す（pane と Claude Code は生きているので、次の起動が引き継ぐ）
continuo
```

---

## 共通ガイドライン

**コーディングとテストの共通ルールは、このリポジトリの外で管理している。**
**どこから読むかは [.claude/local-guidelines.md](.claude/local-guidelines.md) に書いてある**
（`.gitignore` 済み。環境ごとに違うため共有しない）。

**そのファイルが無い環境では、この節は読み飛ばしてよい。**
このリポジトリの規則は、この CLAUDE.md と [.claude/rules/](.claude/rules/) だけで完結している。

---

## プランファイルの書き方

[.claude/rules/plan-file.md](.claude/rules/plan-file.md) に従うこと。とくに次の3点。

- **節の冒頭に「言いたいこと」を3行以内で置く。**そのあとに結論、最後に詳細
- **修正の履歴を書かない。**置くのは最新の仕様・選定根拠・比較した案の否定根拠だけ
- **1つの節は50行以内。**長くなったら要約版を別ファイルに作る（元は残す）

## issue の扱い

[.claude/rules/issue.md](.claude/rules/issue.md) に従うこと。とくに次の4点。

- **issue を作ることと、着手することは別。**作ったらグループ化して着手順序を出し、**人間の指示を待つ。**指示が出たら、その issue が `Ready` へ上がったことを確かめてから着手する（`Ice Box` のままだと continuo は拾わない）
- **ボードの Status を動かすのは人間である。**ボードへ載せて `Ice Box` を付けるのも、`Ready` へ上げるのも、**グループの代表以外を `Ice Box` へ落とす**のも、人間が GitHub の画面から行う。**AI は対象を名指しして人間へ渡すだけ**（落とさないと continuo が代表とは別に dispatch する）
- **閉じられるものを先に外す。**issue の題名だけで「未修正」と判断せず、現行コードと突き合わせる
- **同時に進める issue は2か3まで。**これは continuo の設定 `agent.max_concurrent_agents` とは別物である

## リリースの手順

[.claude/rules/release.md](.claude/rules/release.md) に従うこと。とくに次の3点。

- **実機で issue を1件通してから出す。**テストが全部通っていても、実機で初めて出る欠陥がある
- **文書を直してから出す。**[docs/FAQ.md](docs/FAQ.md) と [docs/upgrading.md](docs/upgrading.md) の両方に入れる。ノートは1回きりで、あとから困った人が引けない
- **`--generate-notes` のまま放置しない。**commit の一覧は利用者に読めない

## 報告のルール

[.claude/rules/reporting.md](.claude/rules/reporting.md) に従うこと。とくに次の5点。

- **初見の人が分かる形で、問題の定義から書く。例外は無い。**「何が起きているか / なぜそれが困るか / いま何を決めるのか」の3つを毎回書く
- **返答の冒頭に「何が言いたいのか」を置く**
- **番号だけのラベルを人間に見せない**
- **issue と PR は対で書く。**PR だけを名指ししない
- **英語の技術用語（worktree / pane / hook / branch / commit）を日本語に直訳しない**

---

## PR を出すときの絶対条件

**PR を出すときは、必ず `/code-review` でレビューする。**
**レビューを通していないものを、マージ可能な状態にしてはならない。**

| 状態 | レビュー |
| --- | --- |
| **draft の PR** | **通していなくてよい** |
| **draft を外した PR**（マージ可能な状態） | **必ず通してあること** |

**手順。**

1. **まず draft で作る**（`gh pr create --draft`）
2. **`/code-review` を通す**
3. **レビュー結果と、指摘ごとの対応表を、その PR のコメントに貼る。**
   **コメントの先頭に `<!-- code-review-result -->` を置く**（CI とリリース前の検査がこの目印を数える）。
   **対応表の中身は下の「コードレビュー記録フロー」にある**
4. **指摘に対応する**（下の「コードレビュー記録フロー」に従う）
5. **`gh pr ready` で draft を外す**

**3 を飛ばしたものは、レビューを実施していないものとして扱う。**
**貼ってあることが、実施したことの唯一の証拠である。**

**既に draft を外してしまったものは、`gh pr ready --undo` で戻してからレビューする。**

**この規則は機械で止める。3箇所で止まる。**

| どこ | いつ止まるか |
| --- | --- |
| [.claude/hooks/block-merge-without-review.py](.claude/hooks/block-merge-without-review.py) | `gh pr merge <番号>` と `gh pr ready <番号>` を**実行する前** |
| [.github/workflows/review-gate.yml](.github/workflows/review-gate.yml) | **PR が作られたとき・push したとき・draft を ready にしたとき。**`review-result` の検査が赤になる |
| [scripts/check-release-ready.sh](scripts/check-release-ready.sh) | **タグを打つ前** |

**3つとも数える条件は同じである。**

- **目印がコメントの本文の先頭にあること**（前に空白文字があってもよい）。**途中に書いたものは数えない**
- **投稿者が `OWNER` / `MEMBER` / `COLLABORATOR` のいずれかであること**

**CI は hook より確かである。**hook はコマンドの文字列から PR 番号を当てているが、
**CI は `github.event.pull_request.number` で受け取る。**書き方を変えても外れない。

**結果を貼ったら `gh pr ready <番号>` を打つ。**`ready_for_review` が飛んで CI の検査が回り直し、緑になる。

**既に draft を外してある PR では、これは効かない。**`ready_for_review` は
**draft を ready にしたときにしか起きない**ので、`gh pr ready <番号>` を打っても何も回らない。
**その場合は `gh run rerun` で回し直す**（手順は [.claude/skills/pr-review-and-merge/SKILL.md](.claude/skills/pr-review-and-merge/SKILL.md) の段5）。

**規則に書くだけでは守られなかった**（2026-08-29。12本をレビューせずにマージし、あとから回し直すことになった）。
**人間が明示的に許すときだけ、環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` を置いて通す。**
**AI が自分でその環境変数を置いてはならない。**
**この逃がし口は hook にしか効かない。**CI は環境変数を見ないので、**貼るまで赤のままである。**

**エージェントが作る PR にも同じ規則を当てる。**continuo が作った PR も、
レビューを通すまで draft のままにする。

## コードレビュー記録フロー

**言いたいこと。**指摘を盲目的に直さない。**1件ごとに「直すか / 直さないか」と「その合理的理由」を先に書き、
PR のコメントへ残してから直す。**掛け直した回数は数える。3回で通らなかったら、下の段へ入る。

**`/code-review` / `code-reviewer` / `security-reviewer` のどれで受けたときも同じである。**

### 報告は issue と PR をセットで

**レビューの話をするときは、必ず対で書く。**
「PR #<番号> の話」ではなく
「**issue #<番号>（issue の題名）に対する PR #<番号>（PR の題名）**」と書く。
**片方だけでは、何を求められていて、どこまで直ったのかを突き合わせられない。**
番号への題名の添え方は
[.claude/rules/reporting.md](.claude/rules/reporting.md) の「issue と PR は対で書く」に従う。

### 手順

| 順 | 何をするか |
| --- | --- |
| **1** | **指摘1件ごとに対応表を書く**（列は下）。**書く前に直さない** |
| **2** | **対応表を、レビュー結果と同じ PR のコメントへ貼る**（先頭の目印は `<!-- code-review-result -->`） |
| **3** | **表のとおりに直す。**表に無いものを直さない |
| **4** | **同じ表をそのまま人間へ報告し、追加の指示を待つ** |
| **5** | **設計そのものが変わったときだけ、プランファイルへ書く** |

### 対応表の列

| 列 | 中身 |
| --- | --- |
| **短縮名** | 指摘に付ける短い名前。**番号だけのラベルを使わない**（[.claude/rules/reporting.md](.claude/rules/reporting.md)） |
| **レベル** | Critical / High / Medium / Low / Info |
| **指摘内容** | 1〜2行 |
| **直す / 直さない** | どちらか。**保留は置かない** |
| **合理的理由** | **なぜ直すか / なぜ直さなくてよいか。**「レビュワーが言ったから」は理由ではない |

**Critical と High は直す。**それ以下は、簡単に直るなら直し、設計に触るなら
follow-up の issue へ切り出す。**切り出した issue を「直さない」の理由欄に書く。**

### なぜ PR のコメントへ残すか

| 置き場所 | 採るか |
| --- | --- |
| **PR のコメント** | **採る。**レビュー結果を貼るコメントは3箇所の機械が数えていて省けない。**同じコメントに入れれば、判断だけが抜け落ちることが無い。**diff の隣にあるので、理由が正しいかを人間がその場で当てられる |
| プランファイル | **採らない。**[.claude/rules/plan-file.md](.claude/rules/plan-file.md) が「**修正の履歴を書かない**」と決めている。指摘ごとの可否は修正の履歴そのものである |
| チャットだけ | **採らない。**セッションが終わると消える |

### 回数を数える

**掛け直した回数を数え、対応表と同じコメントに「何周目か」を書く。上限は3回。**

| 回 | どうするか |
| --- | --- |
| 1回目のあと | **直して2回目を回す。**人間に訊かない |
| 2回目のあと | **直して3回目を回す。**人間に訊かない |
| **3回目のあと** | **収まっていれば進む。収まっていなければ下の段へ入る** |

**回数そのものが目的ではない。**人間が見たいのは「方向がずれていないか」である。
**それでも数える。**書いていないと、ずれ始めた地点が分からず、次のセッションが数え直せない。

### 3回で通らなかったとき

**指摘を1件ずつ潰すのをやめ、この4段へ入る。**
**狙いは、変な方向に作業が進むことを抑制することである。**

| 順 | 誰が | 何をするか |
| --- | --- | --- |
| **1** | **サブエージェント（目的の確認役）** | **対になる issue を読み、何を求めているかを取り出す。**PR の diff を見せない。**diff を見せると、出来上がったものに引きずられる** |
| **2** | **実装担当者** | **PR がいまの内容になるまでの経緯と、その構造にした合理的根拠を、レビュワーへ説明する** |
| **3** | **レビュワー** | **敵対的にレビューする。**説明された合理的理由が正しいと思えなければ**否定する。**通すために納得しない |
| **4** | **レビュワー** | **段1で取り出した目的の外にあるものを、削除の候補として挙げる** |

### 敵対的レビューが判定したあと

**上の4段は判定までである。判定は「いる」か「いらない」の2つで、保留は置かない。**
**判定したら、人間の判断を待たずに下へ進む。**「削りますか」と訊かない。

| 敵対的レビューの判定 | そのあと何をするか |
| --- | --- |
| **目的に照らして、いる** | **1. 実装を止める → 2. 設計内容を敵対的レビューする → 3. 再度実装する → 4. 再度レビューする** |
| **目的に照らして、いらない** | **1. 元の issue を満たせる内容で、どの仕様を削除すべきかを検討する → 2. 削除内容を issue のコメントへまとめる → 3. 実装を止める → 4. 仕様削除を含めた設計内容を敵対的レビューする → 5. 再度実装する → 6. 再度レビューする** |

**どちらも「実装を止める → 設計をレビューする → 実装し直す → もう一度レビューする」である。**
**違うのは、いらないと判定したときだけ、削除する仕様を先に issue のコメントへ残す点である。**
**残すのは、何が消えたかが人間の目に触れないまま進むのを防ぐためである。**報告して待つのではなく、記録して進む。

**2026-09-02 のユーザー指摘。**

> **求めているのは、いるか、いらないか、ではない。**
> **いると判定したなら、一度実装を止め、設計内容を敵対的レビューしてから再度実装し、再度レビューしろ。**
> **いらないと判定したなら、元のissueの内容を満たせる内容で、どの仕様を削除すべきか検討し
> issueコメントに削除内容をまとめ、一度実装を止め、仕様削除を含めた設計内容を敵対的レビューしてから
> 再度実装し、再度レビューしろ。**
> **これで人間の判断を待たずに、方向調整してレビューを終えれるはずだ。**

**結果として「いまのまま」になることもある。それでよい。**

---

## コミットメッセージ形式

`"{何を実装したか} {作業内容を簡潔に表現}"` とする。
