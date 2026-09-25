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

**`continuo` は、GitHub Projects v2 のカンバン1枚を見張り、issue ごとに git worktree を用意して、[herdr](https://github.com/herdrdev/herdr) の pane で Claude Code を対話モードで起動し、完了までを面倒見る常駐プロセスである。**Go で書く。

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

**`herdr wait agent-status …` は存在しない**（herdr 0.8.0 と 0.9.1 で確認）。待機は `herdr agent wait <名前> --until <status>`。
**`pane run "claude"` で起動する経路も避ける。**`agent start` と違って起動完了を待たないため、直後に `agent wait` を呼ぶと `agent_not_found` で失敗する。

### 2. GitHub Projects v2 の project #3 は本番のカンバンである

104件の実データが入っている。**検証で書き込まない。**

**実機で確かめるための専用の環境がある。**カンバンもリポジトリも issue もラベルも用意済みで、
**Status は API で動かせる。**在りかと使い方は [docs/test_environment.md](docs/test_environment.md) にある。
**この環境は消さない。**セッションをまたいで再利用する。

**とくに `updateProjectV2Field` を本番のカンバンで呼んではならない。**選択肢の指定は全件置き換えとして扱われ、**設定済みの Status の値が全部消える。**

**テスト用のカンバン（project #10）に対してだけは呼んでよい。**そこは実データを持たないので、選択肢を作り直しても失うものが無い。
**それ以外のカンバンでは、選択肢の追加は人間が GitHub の画面から行う。**

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

**絶対に commit しないもの。**

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

### 6. continuo で continuo 自身を直すとき、hook の挙動が変化する変更を実装する前に、その変更によりどんな影響があるかを深く検討し、実装してよいか人間に確認する

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
（[internal/cli/cli.go:1755-1775](internal/cli/cli.go#L1755-L1775) が
`--socket` と `--pending-dir` の欠落と相対パスを、それぞれ exit 1 にしている）。
**古い本体は turn の終わりを永久に受け取れなくなる。**
**しかも本体には、自分が黙らされたことが分からない。**hook が1つも届かないことと、
Claude Code がまだ喋っている最中であることは、本体からは区別できない。

**やること。**

> **hook の挙動が変わる変更は、実装する前に止まること。**
> **止まったら、その変更でどんな影響が出るかを深く検討すること。**
> **検討した結果を人間へ見せ、実装してよいかを確認すること。**
> **人間が問題ないので進めてと答えたら進めて良い。**
> **明示的に返答しないケースを含め、それ以外は決して進めないこと。**
> **（AI が勝手に hook 周りも仕様に含めた場合を含む）**

**最後の括弧が本体である。**issue に hook のことが1行も書いていなくても、
**作業中に手が hook の挙動を変えようとしたら、その時点で止まる。**「ついでに直した」を通さない。

**「挙動が変わる」とは何か。**次の4つのどれかである。

| 何が変わるか | 例 |
| --- | --- |
| **hook が受け取る引数** | `--socket` / `--pending-dir` の名前・必須かどうか・値の形 |
| **hook の宛先** | socket のパスの決め方、逃がし先のディレクトリの場所 |
| **hook と本体の約束** | 送る内容、受ける側の解釈、ロックの取り方 |
| **hook が Claude Code へ返すもの** | **サブコマンド名**（`continuo hook` の `hook`）、**終了コード**、標準出力へ返す JSON、張る hook の種類の一覧 |

**4つ目を落としてはならない。**上の3つは continuo の中の話で、**Claude Code との約束が1つも入っていない。**

**実測（2026-09-05）。**サブコマンド名を変えたのと同じ状態を作って叩くと、**終了コード 2 が返る。**

```
$ go run ./cmd/continuo hook-renamed --socket /tmp/x.sock --pending-dir /tmp/x
flag provided but not defined: -socket
continuo — turns a GitHub Projects v2 kanban board into a work queue and has Claude Code work through the issues.

Usage:
  continuo [flags]               start the daemon (…)
  continuo <subcommand> [args]
（Usage とサブコマンド一覧が続く。フラグは -id / -log-level / -port の3本）
exit status 2
```

**`switch args[0]` のどれにも当たらない引数は `runMain` へ落ち、`--socket` が未知のフラグとして 2 を返す。**
**Claude Code は hook の終了コード 2 を「その操作を止めろ」と解釈する。**
`Stop` hook で 2 が返ると、**エージェントが turn を終えられなくなる**
（[internal/cli/cli.go:1730-1731](internal/cli/cli.go#L1730-L1731) と
[docs/plans/impl/04_hook.md:197](docs/plans/impl/04_hook.md#L197)）。

**終了コードを「揃える」cleanup が、いちばん危ない。**
同じファイルの `parseErrorExitCode` は引数の誤りに 2 を返しており、
**`runHook` だけが 1 を返す例外である。**「ばらついているので揃える」は自然な思いつきで、
**上の3つの定義を全部すり抜ける。**

**逆に、これら4つが1つも変わらないなら、下のパスに触れていても止まらない。**
**例。**`internal/cli/cli.go` の別のサブコマンドへ処理を足す。ログの文言を直す。コメントを直す。
**そういう変更は、深く検討したうえで「挙動は変わらない」と判断できたなら、そのまま進めてよい。**

**判断した結果は、pull request の本文へ1段落で書くこと。**
**「触ったが挙動は変わらない」と書いておかないと、次に読む人が同じ検討をやり直す。**

**検知のしかた。**まずパスで拾う。**拾ったものを、上の4つに当てて判定する。**

**この網は、定義そのものではない。**下の grep は「受ける側の解釈」を実装している
[internal/orchestrator/hookinput.go](internal/orchestrator/hookinput.go)（届いた hook を捨てる判定）・
[internal/orchestrator/turn.go](internal/orchestrator/turn.go)（turn の終わりを決める場所）・
[internal/orchestrator/runstate.go](internal/orchestrator/runstate.go) も拾うが、
**拾えるのはファイル単位までである。**その中のどこを触ったかは見ていない。
**網に掛からないファイルでも、上の4つに当たると思ったら止まること。**

```bash
git fetch origin -q
R=$(git rev-parse --show-toplevel)          # cwd がどこでも同じ結果にする
{ git -C "$R" diff --name-only origin/main...HEAD   # commit 済みのもの
  git -C "$R" diff --name-only HEAD                 # まだ commit していないもの（staged / unstaged）
  git -C "$R" ls-files --others --exclude-standard -- :/  # 新しく足して、まだ追跡させていないもの
} | sort -u | grep -E '^(internal/socketpath/|internal/hookclient/|internal/hookserver/|internal/lock/|internal/orchestrator/settings\.go|internal/orchestrator/orchestrator\.go|internal/orchestrator/hookinput\.go|internal/orchestrator/turn\.go|internal/orchestrator/runstate\.go|internal/cli/cli\.go)'
```

**`git -C "$R"` から叩くのは、cwd の下しか見ない経路を塞ぐためである。**
`git ls-files --others --exclude-standard` は pathspec を省くと**いまいるディレクトリの下だけ**を列挙する
（`--full-name` はパスの前置きを直すだけで、走査の範囲は直らない）。
**`internal/cli/` を cwd にして叩くと、`internal/hookserver/` の下に置いた新しいファイルは1行も出ない。**
**「触っていない」と見分けが付かないまま門を通る。**

**3つとも見るのは、`git diff --name-only origin/main...HEAD` だけでは素通りするからである。**
三点の `...` は commit 済みの履歴しか読まないので、**hook の引数を書き換えて、まだ commit していない状態では1行も返らない。**
**止まるべき場面で「触っていない」と読めてしまう**（実測で確認済み）。

**`main` ではなく `origin/main` を見る。**手元の `main` は取り込んでいないことがあり、
**そもそも手元に `main` が無い checkout では `fatal: ambiguous argument` になって、grep には何も渡らない。**
これも「触っていない」と見分けが付かない（[docs/releasing.md:351](docs/releasing.md#L351) と同じ理由である）。

**1行でも返ったら、上の4つに当てて判定する。当たれば止まる。**
それぞれ、どの定義に当たりうるかは次のとおり。

| 触った場所 | どの定義に当たりうるか |
| --- | --- |
| [internal/cli/cli.go](internal/cli/cli.go) の `hook` の引数 | `--socket` / `--pending-dir` が変わると、新しい hook が古い本体へ届かなくなる |
| [internal/cli/cli.go:184-205](internal/cli/cli.go#L184-L205) の `switch args[0]` と [internal/cli/cli.go:1585-1590](internal/cli/cli.go#L1585-L1590) の `parseErrorExitCode` | **4つ目の定義そのものである。**サブコマンド名を変えると、`runMain` へ落ちて終了コード 2 が返る。`Stop` hook で 2 が返ると、エージェントが turn を終えられなくなる |
| [internal/orchestrator/settings.go](internal/orchestrator/settings.go) | hook のコマンド行を組み立てている場所そのもの |
| [internal/socketpath/](internal/socketpath/) | socket のパスの決め方。ずれると hook の宛先が消える |
| [internal/orchestrator/orchestrator.go:1213-1217](internal/orchestrator/orchestrator.go#L1213-L1217) の `pendingDir` | continuo が落ちている間の hook の逃がし先の置き場所 |
| [internal/hookclient/](internal/hookclient/) と [internal/hookserver/](internal/hookserver/) | hook を送る側と受ける側の約束 |
| [internal/lock/](internal/lock/) | ロックファイルの扱い。新旧が同じ鍵を取り合う |
| [internal/orchestrator/hookinput.go](internal/orchestrator/hookinput.go) | 届いた hook を捨てる判定。**受ける側の解釈そのもの** |
| [internal/orchestrator/turn.go](internal/orchestrator/turn.go) | turn の終わりを決める場所。**ここが変わると、本体が turn の終わりを受け取れなくなる** |
| [internal/orchestrator/runstate.go](internal/orchestrator/runstate.go) | hook を受けた run の状態。**新旧で持ち方が違うと、復元した run が hook を取りこぼす** |

**人間に見せるもの。**次の5つを揃える。1つでも欠けたら、人間は可否を判断できない。

| 何を見せるか | 具体的に何を書くか |
| --- | --- |
| **深く検討した影響** | **走っている run のどれが、いつ、どう壊れるか。**既に書かれている issue ごとの設定ファイルが、新しい実行ファイルで通るか。**壊れたときに人間が観測できる症状は何か** |
| **どのファイルのどこを触るか** | 上の `git diff --name-only` の出力と、変える関数名・フラグ名 |
| **hook のどの経路に効くか** | 上の表のどの行に当たるか。issue ごとの設定ファイルのどこが変わるか |
| **止まったまま何もしないと何が起きるか** | **その issue が進まないだけである。**動いている continuo は壊れない |
| **進めて壊れたときの戻し方** | 下の4段。**古い実行ファイルへ戻すところまで書く** |

**戻す先の commit の見つけ方。****いま入っている実行ファイルを作った時刻から引く。**
その時刻に HEAD が指していた commit が、hook が動いていた commit である。

```bash
stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S' ~/.local/bin/continuo   # 実行ファイルを作った時刻（macOS の stat）
git rev-parse "HEAD@{$(stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S' ~/.local/bin/continuo)}"
```

**reflog は90日で消える。**それより古い実行ファイルだったときは、`git log --oneline` から
hook の引数を触る前の commit を人が選ぶ。

**壊れたときの戻し方。****いまの作業ディレクトリは触らない。**
この手順が要る場面では**書きかけの変更が残っている**ので、`git switch` はそれを持ち越せずに拒否することがある。
**別の worktree を1つ作って、そこでビルドする。**

```bash
# 1. 動いている continuo を止める。hook が届かないので1回目の Ctrl+C は待たされる。
#    待たずに終わらせたいときは、もう一度 Ctrl+C を押す
# 2. hook が動いていた commit を、別の worktree として取り出す
OLD=$(git rev-parse "HEAD@{$(stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S' ~/.local/bin/continuo)}")
ROLLBACK="$(mktemp -d)/continuo-rollback"
git worktree add --detach "$ROLLBACK" "$OLD"
go build -C "$ROLLBACK" -o ~/.local/bin/continuo ./cmd/continuo
# 3. 立て直す（pane と Claude Code は生きているので、次の起動が引き継ぐ）
continuo
# 4. 使い終わった worktree を消す（prune では消えない）
git worktree remove "$ROLLBACK"
```

**どうしても作業ディレクトリごと切り替えるときは、戻る段を必ず付ける。**
`git stash` → `git switch --detach "$OLD"` → ビルド → **`git switch -` で元の branch へ戻る** → `git stash pop`。
**`git switch -` を書き忘れると、detached HEAD のまま次の作業を始めることになる。**

### 7. 不特定多数の環境と、maimuzo の環境を混同しない

このcontinuoプロジェクトはOSSである。不特定多数の開発環境でコードが利用され、continuoもまた不特定多数に利用される前提。
一方、開発にはmaimuzoのオリジナルプラグインが使用され、テスト環境もプライベート環境として用意してある。
この不特定多数の環境と、maimuzoの環境を混同してはならない。要件がある時、それは不特定多数向けなのか、maimuzoの環境向けなのかを常に意識すること。
何度もAIは混同し、判断を間違っているので、これは重要な前提であることを忘れないで。

---

## 共通ガイドライン

**コーディングとテストの共通ルールは、このリポジトリの外で管理している。**
**どこから読むかは [.claude/local-guidelines.md](.claude/local-guidelines.md) に書いてある**
（`.gitignore` 済み。環境ごとに違うため共有しない）。

**そのファイルが無い環境では、この節は読み飛ばしてよい。**
このリポジトリの規則は、この CLAUDE.md と、下の「作業の進め方」の表が指す先で完結している。
**ただし表の多くは maimuzo のプラグインを指すので、このリポジトリを clone しただけの人には届かない。**外部の貢献者が読むのは [CONTRIBUTING.md](CONTRIBUTING.md) である。

---

## 作業の進め方

**ここから下（作業の進め方・PR を出すときの絶対条件・コードレビュー記録フロー）は、人間と直接やりとりしている AI に当てる。**
**continuo が起動したエージェントは、ここから下と組み込みの指示書が食い違うときは、組み込みの指示書に従う。**

**規則は次の表の置き場にある。**`.claude/rules/` の2本のほかは、**自動では読まれない。ここから辿る。**

| 何の規則か | どこにあるか | どう読むか |
| --- | --- | --- |
| **設計 → 設計レビュー → 実装 → 実装レビューの段取り、issue と PR のコメントの書き方、レビューの回し方、subagent への渡し方** | [internal/prompt/builtin.md](internal/prompt/builtin.md)（continuo の組み込みの指示書。3-2・3-6・5-5・5-6・5-7） | **Read で開く** |
| **worktree・並列・プラグイン・issue の作り方と着手** | `maimuzo-dev-core` の `general-claude-md` スキル | **セッション開始時と compaction のあとに Skill で読む** |
| **カンバンの操作** | `maimuzo-dev-core` の `issue-management` スキル | カンバンに触る前に Skill で読む |
| **プランファイルの書き方** | `maimuzo-dev-core` の `docs-standard` スキル | プランファイルを書く前に Skill で読む |
| **報告・返答の書き方、worker へ渡すもの** | `maimuzo-chat-response` の `chat-response` スキル | **セッション開始時と compaction のあとに Skill で読む** |
| **chat での質問の訊き方と図の書き方**（上のスキルへ移していないもの） | [.claude/rules/reporting.md](.claude/rules/reporting.md) | 自動で読まれる |
| **プランファイルの補足**（`docs-standard` へ移していないもの） | [.claude/rules/plan-file.md](.claude/rules/plan-file.md) | 自動で読まれる |
| **リリースの手順** | [docs/releasing.md](docs/releasing.md) | Read で開く |

**プラグインの規則は開発者の環境向けである**（上の「不特定多数の環境と、maimuzo の環境を混同しない」）。**このリポジトリを clone した人には当てはまらない。**

### 組み込みの指示書を、自分の作業にも当てる

**この節は、人間と直接やりとりしている AI にだけ当てる。**
**continuo が起動したエージェント（continuo が continuo 自身の issue を回したとき）は、この節を読み飛ばし、組み込みの指示書どおりに動く。**下の読み替えの表を当ててはならない。`CONTINUO-STATUS:` も `<!-- continuo:agent -->` も、指示書どおりに書く。

**組み込みの指示書は、continuo が起動したエージェントへ渡す文書である。**
**人間と直接やりとりしている AI も、計画と pull request のレビュー（3-2・3-6）・issue と PR のコメントの書き方（5-5）・レビューの回し方（5-6）・subagent への渡し方（5-7）は、そこに従う。**
読み替えは次のとおりである。

| 指示書の書き方 | このリポジトリを直す AI は |
| --- | --- |
| 応答の最後に `CONTINUO-STATUS:` を書く | **書かない。**カンバンの操作は `issue-management` スキルに従う |
| 質問は issue のコメントへ書いて止まる | **人間へチャットで訊く。**訊き方は [.claude/rules/reporting.md](.claude/rules/reporting.md) |
| 4-4 の「このプロジェクトの決まり」 | **この CLAUDE.md が、それに当たる** |
| 計画と設計レビューの判断票の1行目に `<!-- continuo:agent -->` を置く | **置かない。**判断票の1行目は目印そのもの（下の「貼る先と目印」） |
| 設計レビューを飛ばす断りを、自分で書かない | **このリポジトリでは、作業している AI が貼る**（下の「貼る先と目印」） |
| マージは、あなたの仕事ではない | **メインエージェントが自分で行う**（下の「PR のマージは、メインエージェントが自分で行う」） |
| カンバンの Status は continuo が動かすので、subagent へ渡してよい | **カンバンへの書き込みは worker に渡さない** |

#### 食い違ったときにどれが勝つか

**上から順に勝つ。**

| 順 | どれ |
| --- | --- |
| 1 | この CLAUDE.md |
| 2 | `.claude/rules/` に残した2本 |
| 3 | 組み込みの指示書（3-2・3-6・5-5・5-6・5-7） |
| 4 | プラグインのスキル |
| 5 | メモリ（`~/.claude/projects/` の下。古い決まりが残っていることがある） |

**とくに次の2つは、プラグインのスキルと食い違っている。**
- **レビュワーへ渡すものは、組み込みの指示書の 5-6・5-7 に従う。**前の周の判断票は「直さないと決めた指摘とその理由」だけを渡す。差分を読む役へは、差分の外の場所と数える範囲を渡さない（`chat-response` の「worker へ渡すもの」の表は、全 worker へ同じものを渡す形のまま）
- **mid と low を、新しい issue へ切り出さない**（`general-claude-md` の「スコープ外として放置しない。bug-reporter で issue に登録する」より、下の「このリポジトリに固有の決まり」が勝つ）

**レビューを回すときは、毎周 5-6 を開き直す。**とくに次の2つを落とさない。

- **重さは、受けた側が 5-6 の4段の定義で付け直す。**レビュワーが付けた重さをそのまま数えない
- **直す前に、直す箇所と影響範囲を全部並べてから、一気に直す**（5-6 の「直す前に書くこと」）

**開き直さなかったために、害の無い指摘を high のまま数え、1本の pull request で31周回したことがある**（2026-09-22）。
**組み込みの指示書は自動では読まれないので、読みに行かない限り効かない。**

### このリポジトリに固有の決まり

- **continuo が本番のカンバン（project #3）を見張っているあいだ、次の2つを守る。**`issue-management` スキルの例外より、こちらが勝つ
  - **確認を待たずに着手してよい例外で直すときも、issue は `Ice Box` のまま直す。**`In Progress` へ動かすと、continuo が同じ issue にもう1つ Claude Code を起動する（`In Progress` は `tracker.active_states` の既定に入っている）
  - **`Ready` と `In Progress` の item の並び順を動かさない。**走っている continuo が次に dispatch する issue が変わる
- **AI が独断で issue を作らない。**人間の依頼か許可があるときだけ作る（`general-claude-md` の「bug-reporter で issue に登録する」より、こちらが勝つ）
- **設計を書く前に、そもそも対応するかを疑う。**非対応と文書に書くだけで済まないかを先に問う
- **設計を書く前に、対象のファイル名・関数名・エラーの文面で [docs/plans/continuo_design.md](docs/plans/continuo_design.md) を grep する。**既に決定が無いかを探す
- **レビュワーの割り当て。**組み込みの指示書の 5-6「誰に見せるか」の2つの役に、次を当てる

  | どのレビュー | 差分を読む役 | 関連処理まで見る役 |
  | --- | --- | --- |
  | **設計レビュー** | `maimuzo-from-ecc:architect`（設計をファイルへ書いて、そのパスを渡す） | Bash を持つエージェント（`general-purpose` など） |
  | **実装レビュー** | `/code-review <PR 番号>` | Bash を持つエージェント（`general-purpose` など） |

  **architect は `git` を叩けないので、実装レビューと、関連処理まで見る役には使わない**
- **設計レビューを飛ばしてよいのは、文書だけの変更と、1行の修正で他に影響しないことが明らかなもの（定数の値・typo）だけである。迷ったら飛ばさない**
- **mid と low は、簡単に直るならその issue の中で直す。設計に触るなら放置する。新しい issue へ切り出さない。**直さないと決めた理由は判断票に書く。**判断票に「保留」は置かない**（直すか直さないかのどちらか）
- **削除した内容は、対になる issue のコメントへ残す。対になる issue が無ければ、その pull request のコメントへ残す。**対になる issue が無いときは、人間がチャットで出した指示の原文を issue の代わりにして突き合わせる
- **毎周、判断票をそのまま人間へ報告する。返事は待たずに次を回す。**周の途中で「続けてよいか」を訊かない（止まるのは連続10回のときだけ）
- **突き合わせの結果が「いまのまま」になってもよい。**何かを変えるために変えない
- **削除が起きた周に回す設計の敵対的レビューは、設計レビューの側に数える。**設計レビューの回数は、pull request を作ったときの本文へ書き写す
- **カンバンの操作は AI が行う**（continuo が起動したエージェントは除く。そちらはカンバンの操作をしない。`In Progress` → `Blocked` を自分で `gh` から動かす経路だけは、[docs/plans/continuo_design.md:9223](docs/plans/continuo_design.md#L9223) が認めている）。**人間がやるのは、設計文書の 4-1 の遷移表で「誰が」の欄が「人間」だけの3つ**（`Ice Box` → `Ready` / `Blocked` → `Ready` / `In Review` → `Done`）。**`Ice Box` → `Ready` だけは、人間が名指しで依頼したときに AI が代行してよい。****代表以外の Status を外してはならない**（未設定の item は continuo から見えなくなり、グループの表明が1件も通らない）
- **worker へ渡す製品の説明は、次の段落をそのまま渡す。要約しない**

  > **continuo は、GitHub のカンバン（GitHub Projects v2）1枚を見張り、
  > issue ごとに git worktree を用意して、その中で Claude Code を対話モードで起動し、
  > 完了までを面倒見る常駐プロセスです。Go で書かれています。**
  >
  > **利用者は `continuo init` で WORKFLOW.md という1枚のファイルを受け取ります。
  > 上半分（`---` に挟まれた front matter）が continuo の設定、
  > 下半分（本文）が Claude Code へそのまま送られるプロンプトです。
  > continuo はこのファイルを勝手に書き換えません。**
- **同時に進める issue は2か3まで。**これは continuo の設定 `agent.max_concurrent_agents`（continuo が同時に走らせる Claude Code の数の上限）とは別物である。**この行を読んで、その設定に手を入れてはならない**
- **リリースは [docs/releasing.md](docs/releasing.md) のとおりに行う。**実機で issue を1件通してから出す。[docs/FAQ.md](docs/FAQ.md) と [docs/upgrading.md](docs/upgrading.md) の両方を直してから出す。`--generate-notes` のまま放置しない

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
   **判断票の中身は [internal/prompt/builtin.md](internal/prompt/builtin.md) の 3-2 と 5-6 にある**
4. **指摘に対応する。収まるまで 2〜4 を繰り返す。**「収まっている」の定義（Critical と High が0件）・収まったあと何周回すか・
   連続10回で完全に止まることは、[internal/prompt/builtin.md](internal/prompt/builtin.md) の 5-6「何周回すか」にある。
   **重さは、受けた側が同じ 5-6 の4段で付け直してから数える**
5. **`gh pr ready` で draft を外す**

**3 を飛ばしたものは、レビューを実施していないものとして扱う。**
**貼ってあることが、実施したことの唯一の証拠である。**

**既に draft を外してしまったものは、`gh pr ready --undo` で戻してからレビューする。**

**この規則は機械で止める。2箇所で止まる。**

| どこ | いつ止まるか |
| --- | --- |
| [.github/workflows/review-gate.yml](.github/workflows/review-gate.yml) | **PR が作られたとき・push したとき・draft を ready にしたとき。**`code-review-result` の検査が赤になる。**あわせて `design-review-result` が、その PR が閉じる issue のコメントに `<!-- design-review-result -->` が貼られているかを数える**（[internal/prompt/builtin.md](internal/prompt/builtin.md) の 3-2） |
| [scripts/check-release-ready.sh](scripts/check-release-ready.sh) | **タグを打つ前** |

**2つとも数える条件は同じである。**

- **目印がコメントの本文の先頭にあること**（前に空白文字があってもよい）。**途中に書いたものは数えない**
- **投稿者が `OWNER` / `MEMBER` / `COLLABORATOR` のいずれかであること**

**2026-09-21 まで、3枚目として `.claude/hooks/block-merge-without-review.py` が在った。**
`gh pr merge` と `gh pr ready` を、AI の手元で**実行する前に**止める hook である。**廃止した。**

**廃止できた理由は1つである。**branch の保護設定で **`enforce_admins` を有効にした**（2026-09-21）。
**repository の管理者も、必須の検査が緑にならないとマージできない。**
**それまでは管理者だけが赤いままマージできたので、CI は「最後の門」になれていなかった。**

**CI のほうが確かである。**hook はコマンドの文字列から PR 番号を当てていたが、
**CI は `github.event.pull_request.number` で受け取る。**書き方を変えても外れない。

**結果を貼ったら `gh pr ready <番号>` を打つ。**`ready_for_review` が飛んで CI の検査が回り直し、緑になる。

**既に draft を外してある PR では、これは効かない。**`ready_for_review` は
**draft を ready にしたときにしか起きない**ので、`gh pr ready <番号>` を打っても何も回らない。
**その場合は `gh run rerun` で回し直す**（どの run を選ぶかは [internal/prompt/builtin.md](internal/prompt/builtin.md) の 3-6「貼ったら、検査を回し直す」）。

**逃がし口は無い。**
**かつては環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` で hook を通せたが、その hook ごと廃止した。**
**CI は環境変数を見ない。**`enforce_admins` を有効にしたので、**管理者も素通りできない。**

**急いでいても、通す手は無い。**レビューを回して結果を貼ること。
**`enforce_admins` を外せば通るが、それは必須の検査8本すべての強制を外す操作である**
（`code-review-result` と `design-review-result` だけでなく、`test` 2本と `build` 4本も外れる）。
**しかも外したことは追跡ファイルに1文字も残らないので、次に開いた人には見分けが付かない。**
**AI がこの設定を外してはならない。**外れていないことは、タグを打つ前に
[scripts/check-release-ready.sh](scripts/check-release-ready.sh) が確かめる。

**`gh pr ready` を実行の前に止める機械は、もう無い。**
**draft を外すこと自体は誰も止めない。**止まるのはマージだけである
（`code-review-result` が赤いまま `enforce_admins` に当たる）。
**「draft を外した pull request は必ずレビューを通してある」は、規則としては生きているが、
実行の前に止める機械の裏付けは失った。**

**エージェントが作る PR にも同じ規則を当てる。**continuo が作った PR も、
レビューを通すまで draft のままにする。

### `/code-review` とマージの細部

- **`/code-review` には PR 番号を必ず渡す。**渡さないと、その worktree の `HEAD` からの差分がレビューされる。**出力の冒頭で、対象が PR になっていることを確かめる**
- **`/code-review ultra` は、人間が明示的に指示したときだけ使う。**レートリミットを大きく使う
- **`gh pr ready` を打ったら、新しく立った run の完了を待ってからマージへ進む。**直後は1つ前の結果が出る
- **合否は `gh pr view <番号> --json mergeable,mergeStateStatus` の1行だけで決める。**検査の一覧を自分で数えて決めない（必須の検査が1本も報告していないと、一覧にそもそも出てこない）

  | 返った値 | どうするか |
  | --- | --- |
  | `MERGEABLE` / `CLEAN` | マージしてよい |
  | `MERGEABLE` / `UNSTABLE` | **マージしない。**必須でない検査が落ちている。何が落ちたかを確かめ、人間へ報告する |
  | `MERGEABLE` / `BLOCKED` | **マージしない。**必須の検査が赤いか、まだ走っている。検査を回し直す |
  | `CONFLICTING` | **マージしない。**先に競合を解決する |

  **表に無い値（`UNKNOWN`・`BEHIND` など）も含め、`CLEAN` 以外はマージしない。**`UNKNOWN` は GitHub がまだ計算中なので、少し待って叩き直す
- **マージは `gh pr merge <番号> --merge`（merge commit）で行う。**squash で入れた branch は `git branch --merged origin/main` に出ず、worktree の片付けの判定が効かなくなる
- **`gh pr merge` が Claude Code の権限の判定で拒否されたら、別の経路を探さず、人間に押してもらう**
- **確かめのコマンドには `--fail-fast` を付けず、`set -e` も置かない。**赤いときこそ、判定の1行を出させる。**シェルの変数は Bash の呼び出しをまたげないので、塊は1回の呼び出しで丸ごと叩く**
- **目印の数え方は、[.github/workflows/review-gate.yml](.github/workflows/review-gate.yml) と [scripts/check-release-ready.sh](scripts/check-release-ready.sh) の2か所が持っている。変えるときは2つとも直す**

### 絶対条件：PR のマージは、メインエージェントが自分で行う

**worker（subagent / Workflow の agent）に `gh pr merge` を実行させてはならない。**
**マージできる状態かどうかの確認も、メインエージェントが自分で行う。**

**なぜか。**メインエージェントが渡す確認コマンドが、目印を**本文のどこかに含むか**で数えると、
**進捗のコメントの本文中に手順の説明として入った同じ文字列を1件と数え、レビュー未実施のまま通る。**

**数え方を自分で書き直してはならない。**数える条件は上の2箇所の実装が持っている。
**手で書いた jq は、投稿者の絞り込みか、ページ送りか、先頭の空白の扱いのどれかで必ずずれる。**
**代わりに `gh pr view <番号> --json mergeable,mergeStateStatus` を見る。**検査が全部緑なら `CLEAN` が返る。

### マージの条件は、なるべく機械で判定する

**AI の判断に頼る部分を減らす。**

| 何を確かめるか | どう確かめるか |
| --- | --- |
| **レビュー結果が貼ってあるか** | **GitHub Actions の `code-review-result`**（`main` の必須の検査） |
| ビルドとテスト | `build` 4本と `test` 2本（必須の検査） |
| 衝突が無いか | `gh pr view <番号> --json mergeable,mergeStateStatus` |

**必須の検査は `gh api repos/<owner>/<repo>/branches/main/protection/required_status_checks` で見られる。**

**機械で判定できないものだけを AI が見る。**
**判定できるようにできるなら、issue を立てて機械へ移す。**

## コードレビュー記録フロー

**言いたいこと。**回し方の正は [internal/prompt/builtin.md](internal/prompt/builtin.md) の 5-6 である。**ここに写さない。**
ここに置くのは、このリポジトリに固有の3つ（貼る先と目印・対で書くこと・人間へ見せるもの）だけである。

**`/code-review` / `code-reviewer` / `security-reviewer` のどれで受けたときも同じである。**

### 貼る先と目印

| 何を | どこへ | 1行目 |
| --- | --- | --- |
| **実装レビューの判断票** | **その pull request のコメント** | `<!-- code-review-result -->` |
| **設計レビューの判断票** | **その pull request が閉じる issue のコメント** | `<!-- design-review-result -->`（continuo が起動したエージェントは、1行目を `<!-- continuo:agent -->`、2行目をこの目印にする） |
| **設計レビューを飛ばしてよい変更**（文書だけ・1行の修正） | **その pull request のコメント** | `<!-- design-review-skipped -->`。**2行目に理由を書く** |

**CI が数えるので、1行目を変えない。**判断票の列と何周目かの書き方は組み込みの指示書の 3-2、直す前の計画は 5-6 の「直す前に書くこと」にある。
**判断票は、直す前に貼る。**貼らずに直したものは、レビューを実施していないものとして扱う。

**判断票をプランファイルへ書かない。**指摘ごとの可否は修正の履歴そのもので、プランファイルは修正の履歴を持たない（`docs-standard` スキル）。

### 報告は issue と PR をセットで

**レビューの話をするときは、必ず対で書く。**
「PR #<番号> の話」ではなく
「**issue #<番号>（issue の題名）に対する PR #<番号>（PR の題名）**」と書く。
**片方だけでは、何を求められていて、どこまで直ったのかを突き合わせられない。**

### 人間へ見せるもの

**連続10回で収まらずに止まったとき**（5-6 の「何周回すか」）は、次を揃えて人間へ見せる。

| 何を | 中身 |
| --- | --- |
| **何回回したか** | 各回の critical / high / mid / low の件数（**受けた側が付け直したあとの件数**） |
| **減っているか** | **減っていないなら、それがいちばん重い事実である** |
| **同じ指摘が繰り返し出ていないか** | 出ているなら、その指摘の中身 |
| **止まったまま何もしないと何が起きるか** | その pull request が進まないだけか、他が止まるか |

**人間が方針を変えたら、そこから数え直す。**AI が自分で方向を変えたときは数え直さない。

---

## commit メッセージの形式

`"{何を実装したか} {作業内容を簡潔に表現}"` とする。
