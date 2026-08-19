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
**トラッカーへの投稿口を注入しない。**注入すると、この層のテストのたびに偽のトラッカーが要る。

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

- [ ] **置き場所が `<root>/<host>/<owner>/<repo>/<スラグ>` になる**（スラグはスラッシュをハイフンに置換）
- [ ] **`<host>` は issue の URL のホスト部から取る。**URL が空なら `github.com`（設計 3-22）
- [ ] **用意の手順を7段そのままの順で実行する**（3-22）。`git worktree prune` が最初
  - **段7 は `worktree.open`。**`create` ではない（実体は git が作り終えている）
  - **その workspace の中に pane が1つできる。`pane.split` も `tab.create` も呼ばない**（設計 4-5）
- [ ] **実体はあるが登録が無い worktree は、乗っ取らずエラーにする**
- [ ] **`git worktree add -b` の失敗時に、先に作られた孤児 branch を消す**
- [ ] **`workspace.root` を起動時に作る**（`os.MkdirAll` / `0700`）。**検査より前に行う**
- [ ] **封じ込め検査が、`root` だけシンボリックリンクを解決して比較する**（設計 3-20）
  - **作る直前は worktree のパスがまだ無いので解決できない。**作ったあとにもう一度比較する
- [ ] **身元ファイルを、着手の段6（3-16）で書く。`agent_name` だけは段9 のあとに追記する**
  - **段6 の時点では agent 名が確定していない**（重複したら連番が付く。設計 3-3）
- [ ] **`workspace_hooks.after_run` を、run が終わったとき（worker を止める直前）に1回だけ実行する**（設計 3-9 の段0）
- [ ] **worktree を再利用するときは、既存の身元ファイルを先に読む**（設計 3-18）
  - `takeover_count` は1つ増やす。`created_at` は保つ。それ以外は書き直す
  - **壊れていたら新規として扱う**
- [ ] **`herdr.worktree.base` が null なら `Issue.NativeRef["default_branch"]` を読む。**
      そのキーも無ければ、その issue を失敗として扱う（base を推測しない）
- [ ] **`.git/info/exclude` に登録する。`.gitignore` は触らない**（利用者のリポジトリを汚さない）
  - **登録先は共通ディレクトリ側の1本である**（`git rev-parse --git-common-dir` で引く）。worktree ごとには無い
- [ ] **引き継ぐたびに `takeover_count` を増やして書き戻せる**
- [ ] **信頼を引く鍵を、設計 3-6 の3段で作る**（`ghq list -p -e` → `git rev-parse --show-toplevel` → `~/.claude.json`）
- [ ] **`~/.claude.json` は読むだけ。**書き換えない
- [ ] **理由まで返す関数と、真偽値だけ返す関数の2つを持つ**
  - `CheckTrust(owner, repo) (trusted bool, reason string, err error)` — doctor が使う（理由を出したい）
  - `tracker.RepoTrustFunc` に合う `func(owner, repo string) bool` — 既存の呼び出し口に渡す薄い包み
  - **「clone が無い」と「未信頼」は `reason` で区別する。**真偽値だけの側はどちらも `false`
- [ ] **後始末が `cleanup.on_states` に入った時点で走る**（「active でなくなった時点」ではない）
- [ ] **未コミットの変更があれば消さない**（`require_clean_worktree`）
- [ ] **push されていない成果が残っていれば消さない**（`require_pushed`）。**判定は upstream の有無で分ける**（設計 3-9）
  - **upstream がある**: `git rev-list --count @{u}..HEAD` が 0 なら消してよい
  - **upstream が無い**: `git diff --quiet <base>...HEAD` が真（差分なし）なら消してよい。`<base>` は worktree を作ったときの base
  - **commit の有無で判定しない。**commit していなくても編集したファイルが残っていれば成果はある（それは1つ上の `require_clean_worktree` で拾う）
  - **エージェントに push させる前提である**（プロンプトに指示がある。設計 5-3）。
    push しないと、この検査で永久に消えない
- [ ] **`workspace_hooks.before_remove` を、消す前の worktree を cwd にして実行する**（設計 3-9 の段2d）
  - **失敗しても記録して続ける**（片付けを止めない）
- [ ] **封じ込め検査の段4 で食い違ったら、worktree を消さずに残して失敗として扱う**（設計 3-20）
- [ ] **`worktree.remove` は branch を消さないので、`git branch -D` を自分で叩く**
- [ ] **`worktree.remove` のあとに `workspace.close` を呼ばない。**応答に `workspace` が入り、workspace ごと閉じる（設計 3-9）
- [ ] 消さなかった worktree について、**issue へのコメントは1回だけ。**以後はログにのみ残す
  - **「1回だけ」の記録は身元ファイルに持つ**（メモリだと再起動で消えて毎回コメントする）

## 落とし穴（実測で分かっている）

- **信頼は worktree 単位ではなくリポジトリ単位で記録される。**worktree のパスで引くと必ず「未承認」になる
- **`.git` が worktree ではファイルである**（ディレクトリではない）。パスの扱いで前提にしない
- **ghq に worktree を作る機能は無い**（サブコマンドは6つだけ）。作るのは gwq か git 自身

## 実装の記録

（着手したら書く）
