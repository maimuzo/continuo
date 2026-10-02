# 判断ログ: はじめて continuo を動かせるようにする

- 対象: `docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.rucm.md`
- 作成日 / 作成モデル: 2026-08-20 / Claude Opus 5 (1M context)。2026-10-02 に Claude Opus 5.5 が、いまの文書の段の順番と実装（常駐の起動）に合わせて書き直した
- 参照した根拠資料: `README.ja.md`（必要なもの / インストール / 使い方）、`docs/trying_it_out.md`（段2〜段9）、`docs/plans/continuo_design.md`（3-6 / 3-17 / 3-32）、`internal/cli/cli.go`、`internal/daemon/daemon.go`、`internal/daemon/checks.go`、`internal/doctor/doctor.go`、`internal/i18n/messages/ja.json`、取り込んだ7本の記述の名前

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | USE CASE NAME | はじめて continuo を動かせるようにする | 依頼で指定された名前をそのまま使う | 依頼文 | 100% |
| 2 | 配置先ディレクトリ | `scenario/` | 複数の記述を時系列に跨ぐ | - | 100% |
| 3 | 「システム」の実体 | 利用者の手元のコマンド環境と continuo の実行ファイル | `gh`・`mkdir`・`continuo` の各サブコマンド・常駐の本体が、同じ端末で動く。BRIEF DESCRIPTION の2文目で固定した | `README.ja.md` の「使い方」 | 85% |
| 4 | 根拠の使い分け | 人間が行う段は文書、continuo が行う段は実装 | 基本フロー 1〜22 は人間がコマンドを叩く段と結果の検証で、順番を決めているのは文書である。基本フロー 23〜33 は常駐の起動で、順番を決めているのは `Run` である | `README.ja.md` の「使い方」、`internal/daemon/daemon.go` の `Run` | 95% |
| 5 | 段の順番 | 入れる → ボードを確かめる → 設定を置く → Status の割り当て → 信頼の登録 → Keychain → 前提の検査 → issue を用意する → 動かす | `README.ja.md` の「使い方」のコマンドの並びと、`docs/trying_it_out.md` の段2〜段8 の並びである。前の版は issue を作る段を信頼の登録より前に置いていた | `README.ja.md` の「使い方」、`docs/trying_it_out.md` の段の見出し | 95% |
| 6 | 取り込んだ記述の引き方 | 名前だけで引く。取り込んだ先の段の番号と代替フローの名前を書かない | `continuo を入れる`・`対象リポジトリを信頼登録する`・`前提が揃っているかを検査する` は、同じ時期に別に書き直されている。中身に依存すると、片方だけが古くなる | 依頼文 | 100% |
| 7 | 基本フロー 1 | `INCLUDE USE CASE continuo を入れる` | `README.ja.md` の「インストール」の主な道は `install.sh` である。前の版は `go build` と Go の版の検証を段に書いていた。ソースからビルドする道は、本文に書いた | `README.ja.md` の「インストール」 | 85% |
| 8 | 代替フロー `Goの版が古い` を消したこと | 消す | ビルドの段を消したので、分岐元が無い。Go の版を確かめるのは Go の道具で、continuo ではない | `README.ja.md` の「インストール」 | 90% |
| 9 | 基本フロー 2 | `INCLUDE USE CASE ボードを新規に用意する` を、分岐なしで1回通す | 文書の道は「いま使っているカンバンをそのまま使う」の1本である。前の版は「ボードを持っていない」枝と「持っている」枝を IF で分けていた。持っていない利用者の道は、文書に無い | `README.ja.md` の「使い方」、`docs/trying_it_out.md` の段2 | 90% |
| 10 | 前の版の段7（`status_field` に `continuo Status` を書く）を消したこと | 消す | 専用のフィールドを作る段が `ボードを新規に用意する` から無くなった。文書は専用のフィールドを「使ってもよい」と書くだけである | `docs/trying_it_out.md` の「専用のフィールドを使ってもよい」 | 90% |
| 11 | 基本フロー 3〜4 | 設定を置く空のディレクトリを作る | 文書は `mkdir -p` で作ってから `continuo init` を叩く。`continuo init` はディレクトリを作らない | `README.ja.md` の「使い方」、`docs/trying_it_out.md` の段3 | 90% |
| 12 | 基本フロー 8 | 利用者が `trust.repositories` から要らない行を消す | 文書は、`continuo init` のあとで一度 WORKFLOW.md を開き、要らない行を消すよう書いている。消さないと、無関係なリポジトリまで信頼登録される。前の版には無かった | `README.ja.md` の「使い方」 | 95% |
| 13 | 利用者が画面やエディタで直接行う操作の書き方 | 利用者の直接の操作として書く（基本フロー 8・21） | 人間の決定（2026-08-20）。continuo も利用者が叩くコマンドも関与しない操作は、システムを介さずに書く | 人間の決定（2026-08-20） | 100% |
| 14 | 前の版の段12（利用者が clone を作る）を消したこと | 消す | `continuo trust` は、clone が無ければ `ghq get` で取ってくる。文書は「手で `ghq get` を叩く必要も無い」と書いている | `docs/trying_it_out.md` の段5、`README.ja.md` の「使い方」 | 95% |
| 15 | 基本フロー 9〜10（`continuo allow-keychain-access`） | 分岐なしの2段で書く | 対応する記述が無い。macOS でだけ Keychain を読み、ほかの OS では何もせずに終了コード 0 を返す。OS の違いを IF にすると、起動の段の経路が全部2倍になる（12本が24本になることを確かめた）。掛け合わせた経路に新しい情報は無い | `internal/cli/cli.go` の `runAllowKeychainAccess`、`docs/trying_it_out.md` の段5b | 80% |
| 16 | `continuo allow-keychain-access` の失敗を分岐にしないこと | 本文に書く | 読めない場合は終了コード 1 を返すが、continuo は Keychain を読めなくても起動する。シナリオを止める分岐ではない | `internal/cli/cli.go` の `runAllowKeychainAccess` | 75% |
| 17 | 基本フロー 16 の見出し語の数 | 18件 | `doctor.label.*` は18個である。`README.ja.md` も「18の項目」と書いている。前の版は7件だった | `internal/i18n/messages/ja.json` の `doctor.label.*`、`README.ja.md` の「必要なもの」 | 100% |
| 18 | 代替フロー `前提の不足` の終わり方 | `RESUME STEP 11`（検査をもう一度行う） | 直してから `continuo doctor` をやり直すのは人間で、文書に在る。CFG では cycle になる | `docs/trying_it_out.md` の段6、`README.ja.md` の「必要なもの」 | 90% |
| 19 | `前提の不足` の段 | 利用者が直し方に従って前提を揃える、の1段 | ✗ の見出し語と直し方を出すのは `前提が揃っているかを検査する` の中である。前の版はシナリオの側にも応答の段を書いていた | - | 85% |
| 20 | 基本フロー 13〜17（issue を用意する） | `gh issue create` → `gh project item-add` → 画面で Status を動かす | 文書の段7 の順である。Status を動かすのは画面での作業である | `docs/trying_it_out.md` の段7 | 95% |
| 21 | 基本フロー 23〜31 の検証の順番 | 設定 → プロンプト → 接続先 → socket の置き場所 → ロック → 部品の組み立て → 起動時の検査 → 使用率を受ける socket → 復元 | `Run` が上から順に行い、どれで止まっても起動を止める。前の版は「選択肢名の不一致」の1つだけだった | `internal/daemon/daemon.go` の `Run` | 95% |
| 22 | 代替フロー `起動できない` を1本にまとめたこと | BOUNDED ALTERNATIVE FLOW。分岐元は 19〜27 の9つ | どの段で止まっても、`runMain` は同じ見出し（continuo を起動できません）と理由を標準エラーのログへ出して 1 を返す。違うのは理由の文言だけである | `internal/cli/cli.go` の `runMain`、`internal/daemon/daemon.go` の `Run` | 90% |
| 23 | `起動できない` の終わり方 | ABORT | プロセスが終わる。直して起動し直すのは人間である | `internal/cli/cli.go` の `runMain` | 95% |
| 24 | `起動できない` の POSTCONDITION | 生きている pane を閉じていない。終了コード 1 | 起動時の検査で落ちたとき、実装は生きている pane を閉じずに残す。それより前の段では、pane に触っていない | `internal/daemon/daemon.go` の `Run` | 95% |
| 25 | 起動時の検査の中身を1つの検証にしたこと | 「起動時の検査がすべて通る」 | 中身は5つ（書ける場所・gh・gh の scope・herdr の protocol・Status の選択肢名）で、どれも同じ終わり方をする。中身は本文の表に書いた | `internal/daemon/checks.go` の `runStartupChecks` | 90% |
| 26 | 基本フロー 30 の条件 | 使用率を受ける socket を開けるか、`rate_limit.source` が `statusline` でない | 実装が起動を止めるのは、socket を開こうとして開けず、かつ `rate_limit.source` が `oauth_usage_api` でないときである。`none` のときは socket を作らないので、開こうとせずに進む。`oauth_usage_api` のときは、開けなくても警告を出して続ける。止まるのは `statusline` のときだけである。前は「`oauth_usage_api` である」と書いていて、`none` を覆っていなかった | `internal/daemon/daemon.go` の `Run` と `build` | 95% |
| 27 | 起動を止めない警告を段にしないこと | 本文に書く | 片付けを始める Status の食い違い・ダッシュボードを開けないこと は、警告を出して先へ進む。分岐にすると経路が掛け算で増える | `internal/daemon/daemon.go` の `Run` / `WarnCleanupStates` | 85% |
| 28 | 基本フロー 30 | `INCLUDE USE CASE issue を1件処理する` | 着手してから先の段（空きスロット・入札・worktree・pane・1回目の turn・表明）は、別の記述が持つ。前の版はシナリオの中に着手の段を13個書いていた。同じ段を2か所に持つと、片方だけが古くなる | `docs/trying_it_out.md` の段8、依頼文 | 85% |
| 29 | 前の版の代替フロー `未信頼のリポジトリ`・`確認の画面が出ている`・`選択肢名の不一致` を消したこと | 消す | 前の2本は着手の段から分かれるフローで、`issue を1件処理する` の側に在る。`選択肢名の不一致` は `起動できない`（分岐元 25）に含まれる | `internal/daemon/checks.go` の `runStartupChecks` | 90% |
| 30 | `常駐の中断` の分岐元 | `BRANCH FROM BASIC FLOW 34` | 巡回が始まったあとで、最初の issue を処理している最中である。pane と worktree が残る、影響がいちばん大きい時点である | `internal/daemon/daemon.go` の `Run` | 80% |
| 31 | `常駐の中断` の段の順番 | 巡回を止める → ダッシュボードを閉じる → 使用率を受ける socket を閉じる → hook を受ける socket を閉じる → turn ループの終了を待つ → pane を閉じずに終了する | 実装の後始末の順である。使用率を受ける socket は、ダッシュボードを閉じたあと、hook の受け口を閉じる前に閉じる。前はこの段が無かった。閉じられなくても WARN を出して先へ進むので、分岐にはしていない | `internal/daemon/daemon.go` の `Run` と `deps` の `close` | 95% |
| 32 | `常駐の中断` の終了コード | 0 | `Run` は、コンテキストの取り消しで終わったときに nil を返す。`runMain` は 0 を返す | `internal/cli/cli.go` の `runMain`、`docs/trying_it_out.md` の「この文書のどこを実際に叩いたか」（段9） | 95% |
| 33 | 基本フローの POSTCONDITION | 常駐している。巡回は続いている。設定の2枚が在る。信頼登録されている。最初の issue は1回処理されている | 取り込んだ記述の事後条件の細部（Status の値など）は書かない。取り込んだ先の中身に依存しないためである | - | 80% |
| 34 | PRECONDITION | GitHub のアカウント、OS、herdr と Claude Code の導入、ボードを1枚以上持っている、`gh auth login -s project` 済み | `install.sh` は herdr と claude を入れない。文書は、いま使っているカンバンをそのまま使うと書いている | `README.ja.md` の「必要なもの」「インストール」「使い方」 | 85% |
| 35 | SECONDARY ACTORS | GitHub、herdr、Claude Code、gh、Keychain | issue の作成とボードは GitHub、起動時の検査は gh と herdr、処理は Claude Code、基本フロー 13〜14 は Keychain である。前の版の ghq は、利用者が clone を作る段と一緒に消した | `internal/daemon/checks.go` の `runStartupChecks` | 85% |
| 36 | この記述からテストを作るか | 作らない。理由を記述の冒頭に書く | 基本フロー 1〜22 は人間の操作とその結果の検証で、23〜33 は常駐の起動である。段を通すテスト `TestE2E_手順書の段1から段9までをmockだけで通す` は、もう1本のシナリオ（`issue を着手から片付けまで見届ける`）の段も1本で通している。テストのファイルは `SOURCE` を1つしか持てないので、どちらの記述の印も付けない。常駐の起動の段を確かめるテストは、引いた先の `再起動して実行中の issue を引き継ぐ` に付いている | `test/e2e/walkthrough_test.go`、`docs/plans/rucm_realign_plan.md` の 5節 | 90% |
| 37 | 取り込んだコマンドが止まった場合の受け方 | 基本フロー 2・7・10・12 に検証を置き、偽の側を1本の BOUNDED ALTERNATIVE FLOW `用意のコマンドが止まる`（ABORT）で受けた | `continuo を入れる`・`設定ファイルを作る`・`既存のボードの Status を割り当てる`・`対象リポジトリを信頼登録する` は、途中で打ち切る代替フローを持つ。打ち切られると、利用者は次の段へ進めない。前は「止まった場合の扱いは取り込んだ先に在る」と断るだけで、止まった実行が次の段を通る形になっていた。終わり方はどれも「残りの段へ進まない」の1つなので、1本にまとめた。検証は、取り込んだ先が終えたあとに成り立つ状態（実行ファイルが在る・WORKFLOW.md が在る・書き換え終えている・信頼登録されている）で書いた。`ボードを新規に用意する` は利用者が gh と GitHub の画面で行う段で、指摘に無かったので足していない | `docs/spec/usecases/particular_case/continuo を入れる.rucm.md`、`設定ファイルを作る.rucm.md`、`既存のボードの Status を割り当てる.rucm.md`、`対象リポジトリを信頼登録する.rucm.md` | 80% |
| 38 | 経路が12本から16本へ増えたこと | 分けずに1本のままにした | 増えた4本は、取り込んだコマンドが止まる4箇所である。どれも ABORT で、そのあとの段と掛け合わさらない | `docs/spec/usecases/scenario/はじめて continuo を動かせるようにする.cfg.json` | 90% |
