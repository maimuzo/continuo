<!-- 目的: continuo doctor（前提の検査）を実装するタスク -->

# 08. continuo doctor

**言いたいこと。**前提が6つあり、**どれが欠けても静かに失敗する。**
**機械的に検査して、足りないものと直し方を人間に出す。**

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 3-32 | **検査する6項目と、それぞれの検査方法・落とし穴** |
| 3-6 | 起動時の検査（doctor と重なる部分） |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `cmd/continuo`（`doctor` サブコマンド） | **7項目**を検査して結果を出す（下記の見出し語で出す） |
| `internal/doctor` | 各検査の実装 |

**検査する7項目と、出力に出す見出し語。**この語で固定する（設計 3-32）。

| 見出し語 | 何を検査するか |
| --- | --- |
| `設定ファイル` | `WORKFLOW.md` が読めて、front matter が検証を通るか |
| `herdr` | socket の `ping` の応答の `protocol` が `herdr.protocol` と一致するか |
| `gh の認証` | `gh auth status` の `Token scopes:` に `project` が単独で並んでいるか |
| `ボード` | `Bootstrap` が通り、`active_states` の選択肢名が全部あるか |
| `clone` | 対象リポジトリが `ghq list --exact` で見つかるか |
| `信頼登録` | 対象リポジトリの clone のパスが `~/.claude.json` で承認済みか |
| `資格情報` | `rate_limit` の設定に応じて、環境変数かファイルがあるか |

## 対象のリポジトリはボードを読んで決める

**`<owner>/<repo>` は設定に無い。**doctor が**ボードを1回読んで**、返ってきた issue の `nameWithOwner` を集める（設計 3-32）。

| 何を | どうするか |
| --- | --- |
| 検査の順序 | **`gh` の認証を先に検査する。**落ちたら clone と信頼は「確かめられなかった」として飛ばす |
| 設定の読み込みに失敗したら | **設定に依存する検査を全部「確かめられなかった」にする。**打ち切らない |
| 結果の種類 | **3値**（`✓` 通った / `✗` 足りない / `!` 確かめられなかった） |
| 終了コード | **`✗` が1つでもあれば 1。**`!` だけなら 0 |

## 受け入れの基準

- [ ] **herdr が動いているか。**socket の `ping` を呼び、応答の `protocol` が `herdr.protocol` と一致するか
  - **`herdr status` の CLI は使わない**（設計 2-1 / 3-32）。`internal/herdr` の実装を呼ぶ
- [ ] **`gh` の認証と scope。**`gh auth status` の `Token scopes:` の行を読み、
      **`'project'` が単独の scope として並んでいるか**を見る（`read:project` だけでは書き込めない）
  - **`--show-scopes` というフラグは存在しない**（gh 2.97.0 で確認）。既定の出力に scope が入っている
  - **対象のホストは `github.com` に固定する**（設定から引かない）
  - **読むのは `Active account: true` の行を持つブロックだけ**（同じホストに複数のアカウントを持てる）
  - **カンマで区切り、各要素の前後の空白と引用符を落としてから照合する**
  - **該当ブロックが1つも無ければ `✗`**（未ログイン）。「`gh auth login -s project` を実行してください」と出す
- [ ] **資格情報。**`rate_limit.source` と `token_source` に応じて記号を分ける（設計 3-32 の表）
  - **`source` が `none` なら `✓`**（`token_source` は見ない）
  - **`token_source` が `env` で環境変数が無ければ `✗`**、`claude_credentials` でファイルが無ければ `!`
  - **設定そのものが読めないときは `!`**（何を見るべきか決まらない）
- [ ] **対象リポジトリが0件のとき、`clone` と `信頼登録` は `!` にする。**終了コードには影響しない
  - **ボードが空なのは設定の誤りではない**（設計 3-32）
- [ ] **リポジトリの信頼登録。**`~/.claude.json` の `hasTrustDialogAccepted` が `true` か（**読むだけ**）
- [ ] **ローカルの clone。**`ghq list --exact <owner>/<repo>` の**出力が空でないか**
  - **exit code は存在の有無にかかわらず 0 を返す**（実測）。出力の有無で判定する
- [ ] **設定ファイル。**`WORKFLOW.md` が読めて、front matter が検証を通るか
- [ ] **ボードを読めるか。**`Bootstrap` を呼び、`active_states` の選択肢名が全部あるかを照合する
  - **選択肢名の不一致は `✗`。**巡回が無言で0件を返す原因になる
- [ ] **Claude の資格情報。**`~/.claude/.credentials.json` があれば `✓`、無ければ `!`
  - **Keychain を触らない**（設計 3-32）。読むと確認の画面が出て固まりうる
  - `!` のときは「macOS では Keychain に入っています。起動には影響しません」と出す
- [ ] **1つ失敗しても残りを全部検査する。**最初の失敗で止めない
- [ ] **足りないものごとに「どう直すか」を出す**
- [ ] **出力の形と終了コードが設計 3-32 のとおりである**（`✓` / `✗` / `!` と、`✗` があれば終了コード 1）
- [ ] **信頼の検査は `internal/workspace` の関数を呼ぶ**（第5段階で作るもの。二重に実装しない）
- [ ] **`rate_limit.token_source` が `env` なら、`token_env` の環境変数を見る。**無ければ `✗`（設計 3-32）
- [ ] **ボードを読めなかったときの記号は落ち方で分ける**（設計 3-32）。**レートリミットだけ `!`、他は `✗`**
- [ ] **上流が `✗` か `!` なら、下流を `!` にして理由を出す**（設計 3-32 の依存の表）
- [ ] **`ghq list -p -e <owner>/<repo>` を使う**（`-p` でパスを出す。設計 3-6 の3段と同じ呼び方）
- [ ] **draft issue は対象から外す**（`Owner` / `Repo` が空。リポジトリを持たない）

## 実装の記録

**言いたいこと。**`continuo doctor` は `internal/doctor` にあり、7項目を全部検査してから
記号つきで並べる。**検査の中身は既存の関数を呼ぶだけで、判定を書き直していない。**

### 置いたもの

| ファイル | 何が入っているか |
| --- | --- |
| [internal/doctor/doctor.go](../../../internal/doctor/doctor.go) | `Options` / `Run`（7項目の呼び出し順と依存の適用）／ボードから対象リポジトリを集める処理 |
| [internal/doctor/checks.go](../../../internal/doctor/checks.go) | 7項目それぞれの検査 |
| [internal/doctor/report.go](../../../internal/doctor/report.go) | 見出し語の定数・3値の記号・出力の形・終了コード |
| [cmd/continuo/main.go](../../../cmd/continuo/main.go) | `doctor` サブコマンド（引数の受け取りと終了コードだけ） |
| [internal/workspace/trust.go](../../../internal/workspace/trust.go) | `CheckTrustForClonePath` を切り出した（下記） |
| [README.md](../../../README.md) | 前提の表の下に `continuo doctor` の案内を置いた（**個々の検査手順は書かない。**3-32） |

### 検査の実体は既にあるものを呼ぶ

| 見出し語 | 呼ぶもの |
| --- | --- |
| 設定ファイル | `config.Load` |
| herdr | `herdr.Client.CheckProtocol`（**`herdr status` の CLI は使わない**） |
| gh の認証 | `tracker.CheckGHAvailable` / `tracker.CheckGHProjectScope` |
| ボード | `tracker.ResolveToken` → `tracker.Adapter.Bootstrap` → `FetchIssuesByStates` |
| clone | `workspace.RunGhqList`（`ghq list -p -e <owner>/<repo>`） |
| 信頼登録 | `workspace.CheckTrustForClonePath` |
| 資格情報 | `ratelimit` の定数（`SourceNone` / `TokenSourceEnv` / `CredentialsRelPath`） |

**`internal/daemon` の起動時検査（3-6）と同じ関数を呼ぶ。**違うのは落ち方だけで、
起動時検査は最初の失敗で起動を止め、doctor は全部調べて記号で並べる。

### 信頼の検査を共有するために切り出したもの

`Manager.CheckTrust` は「ghq で clone のパスを引く」と「そのパスを鍵に `~/.claude.json` を
読む」の2つを続けて行う。**doctor は clone の検査で既にパスを持っている**ので、後半だけを
`workspace.CheckTrustForClonePath(clonePath, homeDir)` として切り出し、`Manager.CheckTrust`
はそれを呼ぶ形にした。**ghq を2回起動しないためであり、判定は1箇所にしかない。**
**`~/.claude.json` のパスを組み立てるのもこの関数の1箇所だけである。**

### 設計に無く、実装で決めたこと

| 決めたこと | なぜ |
| --- | --- |
| **`ボード` の検査は Bootstrap と候補の取得の両方を含む** | 対象リポジトリはボードを読まないと決まらない（3-32）。どちらで落ちても「ボードを読めなかった」であり、下流は同じく `!` になる |
| **`clone` と `信頼登録` は対象ごとの内訳を出し、記号は重いほうを採る**（`✗` > `!` > `✓`） | 1件でも欠けていれば「足りない」と報告する（3-32）。どのリポジトリが欠けたかは内訳の行で示す |
| **記号が `!` のときは件数の見出しを出さない**（「0件が未承認です」と書かない） | 見出しの行だけを読むと「未承認は0件＝問題なし」に見える。記号と説明が食い違う |
| **集計の行は `✗` が0件なら「問題があります」と書かない** | 対象リポジトリが0件（ボードが空）でも `!` が2件出る。**ボードが空なのは設定の誤りではない**（3-32）ので、最後の1行を読んで問題があると読めてはならない |
| **doctor はアダプタに信頼の判定関数を渡さない** | 渡すと候補の取得のたびに issue ごとの ghq と git が走る。doctor はリポジトリ単位で1回ずつ検査する |
| **出力の桁揃えは端末の表示幅で行う**（CJK を2桁と数える） | 文字数で揃えると `設定ファイル` と `clone` の説明の開始位置がずれる |

### 依存の線は設計 3-32 の図のとおりに引く

**言いたいこと。**`gh の認証` は**設定ファイルの下流**である。設定ファイルが `✗` か `!` なら
`gh の認証` も `!` になる。読む値が設定に無くても、依存の図を実装で曲げない。

```text
設定ファイル ─┬─ herdr（設定の protocol と照合する）
              └─ gh の認証 ── ボードを読める ─┬─ clone（対象リポジトリが決まる）
                                              └─ 信頼登録（clone のパスが要る）
資格情報（設定が読めたかどうかだけを見る。飛ばさない）
```

**`資格情報` だけが上流の失敗で飛ばされない。**設計 3-32 がそれだけを図の外に置き、
「飛ばさない」と明記しているためである。

### `gh auth status` の単独ブロックの読み方

**言いたいこと。**`Active account: false` と**書いてあるブロック**は、1つだけでも受理しない。
受理するのは **`Active account:` の行そのものが無い**版の gh で、ブロックが1つだけのときに限る。

`gh` の有効なアカウントが別のホストにあると、`gh auth status --hostname github.com` は
`Active account: false` のブロックを1つだけ出しうる。**これを受理すると、Status を書けない
認証に `✓` を出す。**判定は
[internal/tracker/ghstatus.go](../../../internal/tracker/ghstatus.go) の `activeAccountScopes`
にあり、`hasActiveLine`（`Active account:` の行が在ったか）と `active`（`true` だったか）を
分けて持つ。

### テスト

[test/internal/doctor/](../../../test/internal/doctor/) に置いた。
**本番のボードへは1リクエストも送らない**（`httptest.Server` の偽 GraphQL）。
**実 herdr にも繋がない**（Unix domain socket の偽サーバ）。
**本物の `gh` / `ghq` / ホームディレクトリも使わない**（PATH の先頭へ偽物、`HOME` は一時ディレクトリ）。
`cli_test.go` は**ビルドしたバイナリを起動して**出力と終了コードを確かめる。

### `doctor` が検査していない前提が2つある

**言いたいこと。**`doctor` は hook を受ける socket の**置き場所**を1つも検査しない。
そのため `前提はすべて揃っています（✓ 7件）` の直後に、常駐プロセスの起動が落ちることがある。
**設計 3-32 が検査の項目を7つに固定しているため、増やすかどうかは設計の判断である。**

| 起動時だけ落ちる条件 | 出るエラー（原文） | 実装の場所 |
| --- | --- | --- |
| socket の絶対パスが 103 バイトを超える | `hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）` | [internal/socketpath/socketpath.go:104](../../../internal/socketpath/socketpath.go#L104) |
| 置き場所のディレクトリが group / other に開いている | `既にある hook を受ける socket のディレクトリ … の権限が 0755 です` | [internal/socketpath/socketpath.go:214](../../../internal/socketpath/socketpath.go#L214) |

**なぜ `doctor` に無いか。**どちらも
[internal/daemon/daemon.go:160-164](../../../internal/daemon/daemon.go#L160-L164) が
`socketpath.ResolveHookSocketPath` と `socketpath.EnsureDir` を呼ぶところにしか無く、
`internal/doctor` はこの2つを1回も呼んでいない。

**実測**（2026-08-20、[test/e2e](../../../test/e2e) の一式に対して）。
`CONTINUO_RUNTIME_DIR` に 215 バイトのパスと権限 0755 のディレクトリをそれぞれ渡すと、
**`continuo doctor` は両方とも終了コード 0 で `✓ 7件` を返し**、
同じ環境変数で `continuo <WORKFLOW.md>` を起動すると**終了コード 1 で上の文面を出して落ちる。**

**足すなら何を足すか。**8つ目の項目（見出し語の候補: `socket の置き場所`）として、
`ResolveHookSocketPath` の結果の長さと、その親ディレクトリの権限だけを見る。
**`EnsureDir` を呼んではならない。**`doctor` は検査の道具であり、ディレクトリを作らない。

**いまの扱い。**[docs/trying_it_out.md](../../trying_it_out.md) の段6 で、この2つを
「`doctor` を通っても段8 の起動で落ちるもの」として `doctor` が出すものと分けて書いてある。
