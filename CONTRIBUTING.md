# 開発と貢献について / Contributing

> **Docs, code comments and test names are in Japanese.** Issues and pull requests are welcome in English or Japanese — write in whichever you are comfortable with.

---

## 手元で動かすまで

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo

mise trust && mise install     # mise で Go を入れている場合。これが無いと go build が
                               # 「No version is set for shim: go」で止まります
go build -o /tmp/continuo ./cmd/continuo
```

**Go 1.26 以上が要ります。**[mise.toml](mise.toml) が版を固定しています。

## テストの走らせ方

**`sh scripts/test-like-ci.sh` を使ってください。**素の `go test ./...` は使わないでください。

```bash
sh scripts/test-like-ci.sh     # 約3分
```

**なぜ。**素の `go test` は、あなたの機械にある `claude` や `herdr` を見てしまいます。
**手元で通って CI で落ちます。**このスクリプトは PATH からそれらを隠し、`LANG` も外します。
実際にそれで3件の欠陥を見つけました（[docs/plans/continuo_design.md](docs/plans/continuo_design.md) の 6-7）。

**個別に走らせるとき。**

```bash
go test -p 1 -count=1 ./test/internal/orchestrator/
```

**`-p 1` を外さないでください。**並行に走らせるとカバレッジのプロファイルが互いを上書きし、
値が再現しません（同じコードが 83% にも 65% にも見えます）。

## 本物の herdr を叩くテスト

```bash
sh scripts/test-live.sh        # 数秒。herdr が無ければ静かに skip します
```

**これは何か。**[test/live/](test/live/) は、**本物の herdr の socket を叩く**テストです。
偽の herdr は「continuo が正しいと思っている振る舞い」しか返しません。そのため
`worktree.open` に `path` と `branch` を両方渡していたずれがテストを素通りし、
**実機の着手が全件落ちました**（2026-08-20）。ここはその手のずれを捕まえる唯一の経路です。

**いつ回すか。**

| きっかけ | なぜ |
| --- | --- |
| `internal/herdr` の引数や応答の形を変えたとき | 名前のずれは本物にしか分かりません |
| [test/e2e/fakeherdr_test.go](test/e2e/fakeherdr_test.go) の台本を変えたとき | 偽と本物が離れていないかを確かめます |
| herdr 本体を更新したとき | protocol 版と応答の形が変わることがあります |
| pull request を出す前 | `scripts/test-like-ci.sh` は herdr を隠すので、これは走りません |

**叩くのは herdr だけです。**Claude Code は起動しません（枠を消費するため）。
GitHub の GraphQL も `gh` も叩きません（認証と本番のボードが要るため）。

**herdr が居なければ静かに skip します。**PATH に `herdr` が無い・socket が無い・
socket へ繋がらないのいずれかで飛びます。CI では必ず飛びます（runner に herdr は居ません）。

**後始末はテストが自分で行います。**worktree は `t.TempDir()` の下に作り、
`worktree.remove` と `workspace.close` で必ず閉じます。**後始末に失敗したらテストが落ちます。**
既に開いている pane / workspace には触りません（`t.TempDir()` の下を指すものだけを閉じます）。

## コードの置き場所

| 何 | どこ | なぜ |
| --- | --- | --- |
| **テスト** | `test/internal/<パッケージ名>/` | **`internal/` の隣ではありません。**`internal/` と `cmd/` の下にテストのファイルは1つも置きません |
| **本物の herdr を叩くテスト** | `test/live/` | 手元でだけ走ります。CI では skip します |
| CLI の実体 | `internal/cli/` | `cmd/continuo/main.go` は `os.Exit(cli.Run(...))` の1行だけです |
| 画面に出す文言 | `internal/i18n/messages/ja.json` | **日本語が正です** |

**`cmd/continuo` に実装を書かないでください。**`package main` の関数は `test/` から呼べず、
引数の受け取り方も終了コードも検査できません。

## 書き方

- **コメントとテスト名は日本語で書いてください。**既存のコードに合わせてください
- **テストの doc コメントに「目的 / 与える情報 / 成功条件」の3つを書いてください。**
  見本: [test/internal/config/validate_values_test.go](test/internal/config/validate_values_test.go)
- **画面に出す文言を足したら、`internal/i18n/messages/ja.json` にキーを足し、
  `internal/i18n/keys.go` の定数と `allKeys` の両方に足してください。**
  どれか1つでも欠けるとテストが落ちます（実際に落としました）
- **文言を確かめるテストは、日本語の原文を相手に書いてください。**
  その package に `TestMain` を1つ置けば、テストが日本語で走ります
  （見本: [test/internal/doctor/lang_test.go](test/internal/doctor/lang_test.go)）。
  **既定の言語は英語なので、置かないと訳文が返ってきて検査が空振りします**
- **文言を足したら、`internal/i18n/messages/en.json` にも英語を足してください。**
  片方だけだと1つの画面に英語と日本語が混ざり、全部日本語であるより読みにくくなります
- **英語を書くときは [docs/spec/translation-glossary.md](docs/spec/translation-glossary.md) に従ってください。**
  どの日本語をどの英語にするかと、句点・大文字・書式の verb の決めごとが書いてあります。
  **そこに無い語を使ったときは、その語を訳語集へ足してください**
- **`ja.json` の文言を直したときは、`en.json` の先頭の `_source_sha256` を入れ直してください。**

  ```bash
  shasum -a 256 internal/i18n/messages/ja.json
  ```

  この値が実物と食い違うと、「日本語だけ直して英語を直し忘れた」としてテストが落ちます

## 触らないもの

**テストの先頭と各テストの上にある、この形のコメントを書き換えないでください。**

```go
// {"RUCM-CFG-SHA256": "8b8ade…", "SOURCE": "docs/spec/usecases/particular_case/…cfg.json"}
// {"RUCM-PATH": "P003"}
```

**仕様（RUCM）とテストの対応を機械で照合するための印です。**手で書き換えると照合が壊れます。

**この照合は、外部からの PR では CI が飛ばします**（検査のスクリプトが非公開のプラグインに
同梱されているため）。**テストパスが増える変更なら、PR の本文にその旨を書いてください。**
仕様の再生成はメンテナが行います。

## 出す前に

```bash
gofmt -l ./cmd ./internal ./test    # 何も出ないこと
go vet ./...
sh scripts/test-like-ci.sh
```

**commit メッセージは `{何を実装したか} {作業内容を簡潔に表現}` の形にしてください。**

## PR にはレビュー結果を貼る

**レビュー結果が貼られていない PR は、CI が落とします。**
[.github/workflows/review-gate.yml](.github/workflows/review-gate.yml) の `review-result` がそれです。

**数える条件は2つです。**

| 条件 | なぜ |
| --- | --- |
| 目印 `<!-- code-review-result -->` が**コメントの本文の先頭**にある | 途中に書いたものまで数えると、**「レビューの話をしただけ」で通ってしまいます** |
| 投稿者が `OWNER` / `MEMBER` / `COLLABORATOR` のいずれか | **誰でもコメントできます。**外部の人が目印を貼れば通る状態にしません |

**通し方。**

1. レビューを回す
2. その結果を、その PR のコメントとして貼る（**1行目を `<!-- code-review-result -->` にする**）
3. **PR が draft なら** `gh pr ready <番号>` を打つ。`ready_for_review` で検査が回り直します
4. **PR が draft でないなら `gh pr ready` は効きません。**`ready_for_review` は draft を ready に
   したときにしか起きないからです。`gh run rerun <run の id>` で回し直してください

**draft のあいだは赤のままでかまいません。**検査は draft でも走り、**job を飛ばしません。**
**飛ばした job は「成功」として報告され、必須の検査であってもマージを止められないからです。**

**fork から出す PR では、あなた自身が貼っても数えられません**（投稿者が `CONTRIBUTOR` か `NONE` になるため）。
**メンテナがレビューし、結果を貼ります。**赤いままで待っていてかまいません。

### この検査をマージの条件にする（メンテナ向け・1回だけ）

**赤いだけではマージを止められません。**branch protection の必須の検査へ入れて、はじめて止まります。
**リポジトリの管理権限が要ります。**

```bash
OWNER=<owner>   # 自分のアカウント名に書き換える
BR=repos/$OWNER/continuo/branches/main/protection/required_status_checks

# 一、いまの必須の検査を読む
gh api "$BR" --jq '.checks[].context'

# 二、review-result を足した JSON を作る（いまの設定はそのまま持ち越す）
gh api "$BR" --jq '{
  strict: .strict,
  checks: ((.checks | map({context, app_id}))
           + [{context: "review-result", app_id: (.checks[0].app_id)}]
           | unique_by(.context))
}' > "${TMPDIR:-/tmp}/required-status-checks.json"

# 三、入れ替える
gh api --method PATCH "$BR" --input "${TMPDIR:-/tmp}/required-status-checks.json"

# 四、入ったかを確かめる
gh api "$BR" --jq '.checks[].context'
```

**四で `review-result` を含む7つが並べば入っています。**足す前は次の6つです。

```text
test (ubuntu-latest)
test (macos-latest)
build (darwin, arm64)
build (darwin, amd64)
build (linux, amd64)
build (linux, arm64)
```

| 気をつけること | なぜ |
| --- | --- |
| **`checks` は全件置き換えである** | 一部だけ渡すと、**渡さなかった検査が必須から外れます。**二のように、いまの分を読んでから足すこと |
| **`app_id` を落とさない** | `null` にすると、**どのアプリが報告した検査でも合格として扱われます** |
| **job の名前を変えない** | 必須の検査は `review-result` という名前で登録されます。名前を変えると設定が宙に浮き、**検査が無いのにマージできる状態になります** |

## 設計を読む

| 何を知りたいか | どこ |
| --- | --- |
| **なぜそう作ったか**（人間が読む用） | [docs/plans/continuo_design_slim.md](docs/plans/continuo_design_slim.md)（634行） |
| 判断の根拠・実測値・比較した案 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md)（4800行近い） |
| ユースケース記述（RUCM） | [docs/spec/usecases/](docs/spec/usecases/) |
| 実機で試す手順 | [docs/trying_it_out.md](docs/trying_it_out.md) |

**準拠する仕様（openai/symphony の SPEC.md）は同梱していません。**必要なら各自で置いてください。

```bash
mkdir -p docs/spec/symphony
curl -sL https://raw.githubusercontent.com/openai/symphony/main/SPEC.md -o docs/spec/symphony/SPEC.md
```

## 最初の一歩に向く仕事

**英語の文言の直し**（`internal/i18n/messages/en.json`）。全キーの訳が入っていますが、
**機械的に訳した箇所が残っています。**読みにくい英語を見つけたら直してください。
`ja.json` を見ながら1件ずつ直すだけで、設計を読む必要も RUCM に触る必要もありません。

**直すときの約束。**キーを消さないこと。`%d` や `%s` の並び順を訳文でも保つこと
（並び順が違うと `%!d(string=…)` のような壊れた表示になります。テストが落とします）。
**言い換えは [docs/spec/translation-glossary.md](docs/spec/translation-glossary.md) の語に揃えること。**

**日本語のまま出てしまう箇所を見つけたときも歓迎します。**
**次の3つは、まだ資源へ移していません。**

| どこ | 画面のどこに出るか |
| --- | --- |
| ログ | 常駐して動かしているあいだの出力 |
| [internal/config/validate.go](internal/config/validate.go) の要件の文（`0より大きい整数にすること` など） | `continuo doctor` の `config` の行。設定に不正な値を書いたとき |
| [internal/tracker](internal/tracker) のエラーの本文 | `continuo doctor` の `board` の行。ボードを読めなかったとき |

**番兵エラー**（`errors.New` で package の変数として持つエラー）**を資源へ移すときは
`i18n.Sentinel` を使ってください。**`errors.New` に文言を直接書くと、
**その文字列は package の初期化の時点で固まり、言語を決める前なので英語を選んでも日本語のまま出ます。**

## 脆弱性を見つけたら

**公開の issue に書かないでください。**[SECURITY.md](SECURITY.md) を読んでください。
