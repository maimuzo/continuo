<!-- 目的: worktree の用意・身元ファイル・信頼の検査・後始末を実装するタスク -->

# 05. workspace の管理

**言いたいこと。**issue ごとに worktree を用意し、**それが誰のものかディスクだけから分かるようにし**、
終わったら worktree と branch を片付ける。**この身元ファイルが無いと、第7段階（復元）が実装できない。**

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 3-22 | **置き場所の規則（gwq に合わせる）と、用意する手順7段** |
| 3-18 | **身元ファイルのパス・記録する項目・中身のサンプル・`.git/info/exclude` への登録** |
| 3-20 | **封じ込め検査。**worktree が置き場所の内側にあることを確かめる |
| 3-6 | dispatch の直前に issue ごとに行う検査（信頼・封じ込め） |
| 3-9 | **後始末の手順。**未コミットの変更と未 push の commit の検査 |
| 3-7 | 識別子の正規化を型で強制する |

## 作るもの

| パッケージ | 何を |
| --- | --- |
| `internal/workspace` | worktree の用意・再利用・身元ファイルの読み書き・封じ込め検査・後始末 |
| `internal/workspace` | 信頼の検査（`~/.claude.json` を**読むだけ**） |
| `internal/workspace` | **置き場所の走査**（第7段階の復元が使う。設計 3-4 の段2） |
| `internal/workspace` | **後始末。**worktree・branch・**issue ごとの設定ファイル**（身元ファイルの `settings_path`）を消す |

**`internal/workspace` はトラッカーを知らない。**issue へのコメントは**投稿しない。**
**「コメントすべきこと」を戻り値で返し、orchestrator が投稿する**（第6段階）。
**トラッカーへの投稿口を注入しない。**注入すると、この層のテストのたびにテスト用トラッカー mockが要る。

```go
// CleanupResult は片付けを試みた結果である（設計 3-9）。
type CleanupResult struct {
    Removed  bool     // worktree と branch を消したか
    Deferred bool     // 消さずに見送ったか
    Reasons  []string // 見送った理由（"コミットされていない変更が残っている" など。人間が読む文）

    // ShouldComment は、この見送りを issue へコメントすべきかである。
    // 身元ファイルの CleanupDeferredAt がゼロ値のときだけ true になる（設計 3-9 の手順2c）。
    ShouldComment bool
}

// MarkCleanupDeferred は身元ファイルの cleanup_deferred_at に時刻を書く。
// orchestrator が「コメントの投稿に成功したあと」に呼ぶ。
// 投稿の前に書くと、投稿が失敗したときコメントが永久に出ない。
func MarkCleanupDeferred(worktreePath string, at time.Time) error
```

**書き込みの順序を固定する。**

| 順 | 誰が | 何をするか |
| --- | --- | --- |
| 1 | `internal/workspace` | 片付けを試み、`CleanupResult` を返す（`cleanup_deferred_at` は書かない） |
| 2 | orchestrator | `ShouldComment` が true なら issue へコメントする |
| 3 | orchestrator | **コメントに成功したときだけ** `MarkCleanupDeferred` を呼ぶ |

**投稿に失敗したら書かない。**次の巡回でもう一度試みる。

**身元ファイルの型。**

```go
// Identity は worktree の直下に置く .continuo.json の中身である（設計 3-18）。
// これが復元の主キーになる。ディレクトリ名から issue へは戻れない。
type Identity struct {
    IssueURL         string    `json:"issue_url"`
    IssueIdentifier  string    `json:"issue_identifier"`
    ProjectItemID    string    `json:"project_item_id"`
    Branch           string    `json:"branch"`
    HerdrWorkspaceID string    `json:"herdr_workspace_id"`
    SocketPath       string    `json:"socket_path"`
    SettingsPath     string    `json:"settings_path"`
    AgentName        string    `json:"agent_name"`      // agent.prompt / agent.wait の宛先
    SessionUUID      string    `json:"session_uuid"`
    CreatedAt        time.Time `json:"created_at"`
    TakeoverCount    int       `json:"takeover_count"`
    // CleanupDeferredAt は、未コミットや未 push で片付けを見送った時刻である。
    // ゼロ値でなければ、issue へのコメントは既に書いてある（2回目以降はログにのみ残す）。
    CleanupDeferredAt time.Time `json:"cleanup_deferred_at,omitempty"`
}
```

## 受け入れの基準

- [x] **置き場所が `<root>/<host>/<owner>/<repo>/<スラグ>` になる**（スラグはスラッシュをハイフンに置換）
- [x] **`<host>` は issue の URL のホスト部から取る。**URL が空なら `github.com`（設計 3-22）
- [x] **用意の手順を7段そのままの順で実行する**（3-22）。`git worktree prune` が最初
  - **段7 は `worktree.open`。**`create` ではない（実体は git が作り終えている）
  - **その workspace の中に pane が1つできる。`pane.split` も `tab.create` も呼ばない**（設計 4-5）
- [x] **herdr workspace の label に `owner/repo/issues/N` を書く**（設計 3-3。組み立ては `herdr.IssueLabel`）
  - **`worktree.open` の `label` は、既に開かれている workspace には効かない。**直後に `workspace.rename` で書き直す
  - **失敗しても致命にしない。**label は表示名であり、復元の照合には使わない
- [x] **実体はあるが登録が無い worktree は、乗っ取らずエラーにする**
- [x] **`git worktree add -b` の失敗時に、先に作られた孤児 branch を消す**
- [x] **`workspace.root` を起動時に作る**（`os.MkdirAll` / `0700`）。**検査より前に行う**
- [x] **封じ込め検査が、`root` だけシンボリックリンクを解決して比較する**（設計 3-20）
  - **作る直前は worktree のパスがまだ無いので解決できない。**作ったあとにもう一度比較する
- [x] **身元ファイルを、着手の段6（3-16）で書く。`agent_name` だけは段9 のあとに追記する**
  - **段6 の時点では agent 名が確定していない**（重複したら連番が付く。設計 3-3）
- [x] **`workspace_hooks.after_run` を、run が終わったとき（worker を止める直前）に1回だけ実行する**（設計 3-9 の段0）
- [x] **worktree を再利用するときは、既存の身元ファイルを先に読む**（設計 3-18）
  - `takeover_count` は1つ増やす。`created_at` は保つ。それ以外は書き直す
  - **壊れていたら新規として扱う**
- [x] **`herdr.worktree.base` が null なら `Issue.NativeRef["default_branch"]` を読む。**
      そのキーも無ければ、その issue を失敗として扱う（base を推測しない）
- [x] **`.git/info/exclude` に登録する。`.gitignore` は触らない**（利用者のリポジトリを汚さない）
  - **登録先は共通ディレクトリ側の1本である**（`git rev-parse --git-common-dir` で引く）。worktree ごとには無い
- [x] **引き継ぐたびに `takeover_count` を増やして書き戻せる**
- [x] **信頼を引く鍵を、設計 3-6 の3段で作る**（`ghq list -p -e` → `git rev-parse --show-toplevel` → `~/.claude.json`）
- [x] **`~/.claude.json` は読むだけ。**書き換えない
- [x] **理由まで返す関数と、真偽値だけ返す関数の2つを持つ**
  - `CheckTrust(owner, repo) (trusted bool, reason string, err error)` — doctor が使う（理由を出したい）
  - `tracker.RepoTrustFunc` に合う `func(owner, repo string) bool` — 既存の呼び出し口に渡す薄い包み
  - **「clone が無い」と「未信頼」は `reason` で区別する。**真偽値だけの側はどちらも `false`
- [x] **後始末が `cleanup.on_states` に入った時点で走る**（「active でなくなった時点」ではない）
- [x] **未コミットの変更があれば消さない**（`require_clean_worktree`）
- [x] **push されていない成果が残っていれば消さない**（`require_pushed`）。**判定は upstream の有無で分ける**（設計 3-9）
  - **upstream がある**: `git rev-list --count @{u}..HEAD` が 0 なら消してよい
  - **upstream が無い**: `git diff --quiet <base>...HEAD` が真（差分なし）なら消してよい。`<base>` は worktree を作ったときの base
  - **commit の有無で判定しない。**commit していなくても編集したファイルが残っていれば成果はある（それは1つ上の `require_clean_worktree` で拾う）
  - **エージェントに push させる前提である**（プロンプトに指示がある。設計 5-3）。
    push しないと、この検査で永久に消えない
- [x] **`workspace_hooks.before_remove` を、消す前の worktree を cwd にして実行する**（設計 3-9 の段2d）
  - **失敗しても記録して続ける**（片付けを止めない）
- [x] **封じ込め検査の段4 で食い違ったら、worktree を消さずに残して失敗として扱う**（設計 3-20）
- [x] **`worktree.remove` は branch を消さないので、`git branch -D` を自分で叩く**
- [x] **`worktree.remove` のあとに `workspace.close` を呼ばない。**応答に `workspace` が入り、workspace ごと閉じる（設計 3-9）
- [x] 消さなかった worktree について、**issue へのコメントは1回だけ。**以後はログにのみ残す
  - **「1回だけ」の記録は身元ファイルに持つ**（メモリだと再起動で消えて毎回コメントする）

## 落とし穴（実測で分かっている）

- **信頼は worktree 単位ではなくリポジトリ単位で記録される。**worktree のパスで引くと必ず「未承認」になる
- **`.git` が worktree ではファイルである**（ディレクトリではない）。パスの扱いで前提にしない
- **ghq に worktree を作る機能は無い**（サブコマンドは6つだけ）。作るのは gwq か git 自身

## 実装の記録

**言いたいこと。**`internal/workspace` を実装し、受け入れの基準はすべて満たした。
**設計に書かれておらず自分で決めたことが10件ある**（下の表）。テストは67件で、
git は本物・herdr はテスト用socket mockで通している。

**作ったもの。**

| ファイル | 何を |
| --- | --- |
| [internal/workspace/workspace.go](../../../internal/workspace/workspace.go) | `Manager` / `Options`（`SettingsRoot` を含む）/ `New`（起動時に置き場所を 0700 で作り、`identity_file` を検査する）/ `HerdrClient` / `IssueRef` |
| [internal/workspace/layout.go](../../../internal/workspace/layout.go) | 置き場所の組み立て（`Locate`）・封じ込め検査・branch 名の描画・`BranchPrefix` |
| [internal/workspace/git.go](../../../internal/workspace/git.go) | git と ghq の実行（prune / worktree list / add / branch -D / status / rev-list / diff / git-common-dir / show-toplevel / 現在の branch） |
| [internal/workspace/identity.go](../../../internal/workspace/identity.go) | 身元ファイルの読み書き・`info/exclude` への登録・`MergeForReuse` / `SetAgentName` / `IncrementTakeover` / `MarkCleanupDeferred` |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | 用意の手順7段（`Prepare`） |
| [internal/workspace/scan.go](../../../internal/workspace/scan.go) | 置き場所の走査（固定4階層） |
| [internal/workspace/trust.go](../../../internal/workspace/trust.go) | `CheckTrust` と `TrustFunc`（`~/.claude.json` は読むだけ） |
| [internal/workspace/cleanup.go](../../../internal/workspace/cleanup.go) | `ShouldCleanup` / `Cleanup` / `CleanupResult` |
| [internal/workspace/hooks.go](../../../internal/workspace/hooks.go) | `RunHook` / `RunAfterRunOnce` / `BeginRun`（run の切り替わりで after_run の印を消す） |

テストは [test/internal/workspace/](../../../test/internal/workspace/) に9ファイル。

**設計に無く、実装で決めたこと。**

| 決めたこと | なぜそうしたか |
| --- | --- |
| `cleanup_deferred_at` のタグを `omitzero` にした | **`omitempty` は `time.Time` に効かない。**ゼロ値でも `"0001-01-01T00:00:00Z"` が出てしまい、「書いていないなら出さない」が成立しない |
| base を `CleanupRequest.Base` で受け取る | 片付けの手順2b が要る base は、身元ファイルの項目に無い（3-18 の一覧が確定している）。**base が空なら「判定できないので消さない」**として見送る |
| `worktree.remove` に `force: true` を渡す | 消してよいかの判定は手順2 と 2b で済ませてある。herdr 側の未コミット検査で二重に止まると、判定が2箇所に分かれる |
| branch を消すためのリポジトリを `git rev-parse --git-common-dir` で引く | ghq を引き直すより確実で、**worktree を消す前**に1回だけ引けばよい。`.git` で終われば親を、そうでなければ共通ディレクトリ自身を使う |
| `host` / `owner` / `repo` の各階層でスラッシュをハイフンに置き換える | `normalize.Normalize` は branch 名のためにスラッシュを許す。そのまま階層名にすると1つの値が2階層に割れ、**固定4階層の走査（3-4 の段2）と食い違う** |
| after_run の「1回だけ」の印を `Prepare` が消す（`BeginRun`） | 3-9 の段0 の「1回だけ」は **run 単位**である。3-18 は「再利用するということは、その issue が再び dispatch されたということであり、そこから先は別の run である」と定めている。worktree 単位の印にすると、**2回目の run で after_run が二度と実行されない** |
| `Manager` を goroutine から同時に呼んでよい型にした（`afterRunMu`） | turn ループは run ごとの goroutine で動き（3-8）、`agent.max_concurrent_agents` の既定は 2 である。`Manager` は1つを共有するので、**排他が無いと2つの run が同時に終わったとき concurrent map write で落ちる** |
| `git branch -D` に渡す branch を3つの条件で検算する（`deletableBranch`） | 身元ファイルは worktree の直下にあり、その worktree ではエージェントが `--permission-mode dontAsk` で動く（3-16 の段9）。**branch の値は書き換えられる。**そのまま渡すと利用者の `main` を消させられる。通すのは「正規化で変わらない」「`branch_template` の接頭辞で始まる」「**worktree が実際にチェックアウトしている branch と一致する**」の全部を満たす場合だけ |
| `settings_path` を消すのは `Options.SettingsRoot` の内側にあるときだけにした | 同じ理由で `settings_path` も書き換えられる。3-12 が置き場所を `<実行時ディレクトリ>/issues/<issue>/settings.json` と定めているので、**その置き場所を `Options.SettingsRoot` で受け取り、内側かどうかを字句で確かめる**（`..` は `filepath.Clean` が畳む）。**渡されていなければ消さない** |
| `workspace.identity_file` がファイルの名前かを `New` が確かめる（`ValidateIdentityFileName`） | 3-18 はこの値を「ファイルの名前」と定めている。`../secret.json` のような値だと**身元ファイルが worktree の外へ書かれ、`info/exclude` に書く行も `/../secret.json` になる。**`normalize.Normalize` はドットもスラッシュも通すので、別に弾く必要がある |

**`CleanupResult` は「設定で無効」も見送りとして返す。**`cleanup.enabled` が false のときは
`Deferred` を真・`ShouldComment` を偽にし、`Reasons` に理由を入れる。
**理由だけが入って `Deferred` が偽、という値は作らない**（呼び出し側が「消した」「見送った」
「無効」を区別できなくなる）。issue へのコメントは出さない（人間が自分で無効にしたのだから知っている）。

**`TrustFunc` の戻り値は素の関数型 `func(owner, repo string) bool` である。**
`tracker.RepoTrustFunc` はこれを基底型に持つ名前付き型なのでそのまま代入でき、
**`internal/workspace` は `internal/tracker` を import しない**（このパッケージの境界）。
代入できることの検査は `test/internal/workspace/trust_test.go` が tracker 側から行う。

**`Scan` は重複の解消をしない。**同じ project item の ID が2つ出たときにどちらを採るかは
復元（3-4 の段2）の判断であり、pane の一覧と突き合わせる段4 と対で決まる。第7段階で行う。

**時間に依存する処理が無いので `testing/synctest` は使っていない。**
workspace_hooks の時間切れは外部コマンドの実行時間であり、`synctest` の仮想時計では進まない。
