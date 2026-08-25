# 外部との通信の6件（fix29 / 担当: external）

**言いたいこと。**`docs/plans/review/2026-08-25_full_audit.json` の「外部との通信」6件を直す。
触る範囲は internal/herdr / internal/tracker / internal/ratelimit / internal/daemon の4つだけである。

**足したテストは1本ずつ変異（守りを1箇所だけ潰した版）を作って FAIL することを確かめる。**

## 進捗

| 指摘 | 状態 |
| --- | --- |
| turn の待ちが herdr より先に切れる | 直した |
| 取り直しの一括失敗 | 直した |
| 一時的な失敗の判定が使われていない | **直していない**（直し場所が担当の範囲外。最後の節を参照） |
| 枠の判定が一度の失敗で永久に切れる | 直した |
| gh auth token に期限が無い | 直した |
| gh auth status の失敗を未ログインと言い切る | 直した |

## turn の待ちが herdr より先に切れる

**直した内容。**待ちを伴う呼び出しの socket の期限を、herdr へ渡す待ち受けより必ず長くした。
`waitReadBudget`（internal/herdr/client.go）が `max(Timeouts.Turn, 待ち受けの上限) + Timeouts.Read` を返し、
`AgentPrompt`（待機あり）と `AgentWait` がそれを使う。ミリ秒→Duration の変換は溢れを潰してある
（溢れると符号が反転し、期限が「もう過ぎている」ことになる）。

**足したテスト。**test/internal/herdr/wait_budget_test.go の3本。
偽サーバが「herdr へ渡した待ち受けより遅れて `timeout` を返す」様子を再現し、
返るエラーが `ErrCodeReadTimeout` ではなく `ErrCodeTimeout` であることを確かめる。

## 取り直しの一括失敗

**直した内容。**「候補の集合にもう居ない item」と「provider 側の異常」を分けた。
Status 未設定と、Issue でも DraftIssue でもない content（PullRequest 等）を
`mapItemResult.Gone` にして、ID 指定の取り直しでは省く扱いにした（internal/tracker/query.go）。
content が空・repository が空・nameWithOwner の形が不正、は今までどおりエラーである
（SPEC.md 11.1 が要求する malformed）。

**なぜこの直し方か。**候補の取得（`items(...)`）は Status で絞るので、Status 未設定の item は
最初から返らない。PullRequest も同じく候補に出てこない。**どちらもボードとしては正常であり、
malformed ではない。**「取れたぶん＋取れなかった ID」を返す形も考えたが、
呼び出し側（internal/orchestrator）は担当の範囲外で `err != nil` なら `issues` を捨てるので、
足しても動かないコードになる。

**足したテスト。**test/internal/tracker/fetch_ids_test.go に3本
（Status 未設定が混ざっても残りを返す / PullRequest が混ざっても残りを返す /
provider の異常はエラーにする）。

## 枠の判定が一度の失敗で永久に切れる

**直した内容。**資格情報の失敗を3つに分けた（internal/ratelimit/ratelimit.go の `Fetch`）。

| 種類 | 何をするか |
| --- | --- |
| 打ち切り（ctx の cancel / deadline） | エラーを返すだけ。回数にも数えない |
| 一時的（`security` が期限内に返らない） | エラーを返す。`Enabled` は真のまま。連続 5 回で諦める |
| 恒久的（PATH に無い / 項目が無い / 中身が壊れている / 401・403） | 今までどおり1回で諦める |

打ち切りを `security` の失敗と混ぜないよう、`runSecurity` に `ErrKeychainCanceled` の経路を足した。

**足したテスト。**test/internal/ratelimit/keychain_test.go に3本
（1回返ってこなくても諦めない / 続けば最後には諦める / 打ち切りでは諦めない）。
**偽の `security` に呼ばれた回数を数えさせてはならない。**期限で殺されると数える前に死ぬ
（実測: 期限 300ms では1回目の子プロセスの起動が間に合わず、スクリプトが1行も走らなかった）。
目印のファイルをテスト側が置いて切り替える形にしてある。

## gh auth token に期限が無い

**直した内容。**依存の組み立て（`build`）に `tokenTimeout` を渡し、`tracker.ResolveToken` の
呼び出しを `context.WithTimeout` で包んだ（internal/daemon/daemon.go）。値は起動時検査と同じ
`Options.StartupCheckTimeout`（0 なら `DefaultStartupCheckTimeout` = 60秒）である。
あわせて `RunGHAuthToken` と `RunGHAuthStatus` に `cmd.WaitDelay` を置いた。置かないと、
`gh` が孫プロセスへ標準出力を渡していた場合に殺したあとの `Output` が返らず、期限が効かない。
パッケージ doc の起動の順序にも段2b（依存の組み立て＝外部プロセスを起こす段）を書き足した。

**足したテスト。**test/internal/daemon/wiring_test.go の
`TestRun_ghauthtokenが返らなければ起動を止める`。
`gh auth token` だけが返ってこない偽の `gh` を PATH の先頭へ置き、
`daemon.Run` が30秒以内に「トークンを取得できません」で返ることを確かめる。

## gh auth status の失敗を未ログインと言い切る

**直した内容。**`gh auth status` の読み方を2点変えた（internal/tracker/ghstatus.go）。

1. **`Failed to log in to ` もブロックの始まりとして扱う。**扱わないと、その下の
   `Active account: true` がどのブロックにも属さず、出力全体が「未ログイン」に見える
2. **落ちたときは gh の出力をエラー文へ添える。**添えないと gh が書いた本当の理由
   （`The token in keyring is invalid.`）が1文字も画面に出ない

トークンを検証できていないときの文言は新しい key（`tracker.gh_scope.token_unverified`）で、
案内するのは `gh auth login` ではなく `gh auth refresh -h github.com` である。

**足したテスト。**test/internal/tracker/ghstatus_test.go に2本
（トークンを検証できないだけなら未ログインと言わない / 未ログインのときも gh の出力を隠さない）。
入力は実測の出力そのままである（gh 2.97.0、プロキシで塞いだ状態、終了コード 1）。

## 一時的な失敗の判定が使われていない — 直していない

**直せない理由。**この指摘の直し場所は `internal/orchestrator/turn.go` の `sendTurn` と
`afterWaitTimeout` であり、**この担当の触ってよい範囲の外である**
（範囲は internal/herdr / internal/tracker / internal/ratelimit / internal/daemon の4つ）。

**当てるべき変更。**`sendTurn` の失敗経路（`herdr.IsCode(err, herdr.ErrCodeTimeout)` の分岐の直後）に

```go
if herdr.IsTransient(err) {
    o.logger.Warn("herdr へ届かなかったので、この run は次の巡回へ持ち越します",
        "identifier", rs.issue().Identifier, "error", err)
    return turnAborted
}
```

を足す。`afterWaitTimeout` の `else if !herdr.IsCode(err, herdr.ErrCodeTimeout)` の枝も同じ。
`turnAborted` は run を諦めずにそのまま返る既存の結果である
（`o.abandonRun` を呼ばず、リトライも消費しない）。

**herdr の側で吸収する案は採らない。**待ち受け中に接続が切れた場合、プロンプトが届いたか
どうかを client は知りようがない。再送すれば二重に投入され、`agent.wait` へ切り替えると
届いていなかったときに「turn が終わった」と誤読する。
また `internal/herdr/errors.go` は「herdr パッケージ自身はリトライしない」と決めている。

## 検査の通し方

**`scripts/test-like-ci.sh` は mise の shim 経由で `go` を引く。**worktree の `mise.toml` が
信頼されていないと shim が終了コード 1 で落ち、テストが1本も走らないまま「失敗」になる。
worktree で走らせるときは信頼するパスを環境変数で渡す。

```sh
MISE_TRUSTED_CONFIG_PATHS=<この worktree の絶対パス> sh scripts/test-like-ci.sh
```

## 設計への記録

`docs/plans/continuo_design.md` に3節を足した（既存の節は書き換えていない）。

| 節 | 何を決めたか |
| --- | --- |
| 3-38 | 待ちを伴う herdr の呼び出しは、herdr の待ち受けより長く待つ |
| 3-39 | 外部の失敗は「もう見えない」「一時的」「恒久的」で切り分ける |
| 3-40 | 外部プロセスを起こす段には、必ず期限を掛ける |

## RUCM は変えていない

**分岐が増えていないためである。**「前提が揃っているかを検査する」は
`continuo doctor` のユースケースで、`gh の認証` の落ち方は
「前提が揃っていない → 落ち方に応じた直し方を添える」という既存の分岐に収まる。
「レートリミットで待って再開する」の `SPECIFIC ALTERNATIVE FLOW 枠を読めない` も、
その巡回で枠を読めないという意味は変わらない（諦めるかどうかを RUCM は書いていない）。
`sh scripts/check-rucm.sh --strict` は通っている。

## `runSecurity` が期限切れと打ち切りを見る順番

**期限切れ（`runCtx.Err()` が `DeadlineExceeded`）を先に見る。**`runCtx` は呼び出し側の `ctx` から
派生しているので、`ctx` 自身の期限で切れたときも両方の条件が成り立つ。

**先に打ち切りとして扱ってはならない。**`continuo doctor` は検査ごとに期限を付けて
`ProbeKeychain` を呼び、期限切れを「確かめられなかった」として `!` を付ける
（`TestDoctor_資格情報_keychainが返ってこなければ確かめられなかったと出す`）。
打ち切り扱いにすると、この検査が別の記号になる。

**`ErrKeychainCanceled` が要るのは期限の無い打ち切りだけである。**終了処理で `ctx` を
cancel したときがこれに当たり、そこで枠の判定を諦めてはならない。
