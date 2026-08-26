# 01. 設定・ログ・CLI

**言いたいこと。この段階は全部実装済みで、テストも通っている。**
**二重起動の防止を最初に入れたのは、あとから足すと検証で2つ目を立てて壊すからである。**

## 読むもの（設計）

| 節 | 何が書いてあるか |
| --- | --- |
| 5-1 | 設定ファイルの名前と探し方 |
| 5-2 | **front matter の全キーと既定値** |
| 5-3 | 本文（プロンプトのテンプレート） |
| 5-5 | **展開規則**（`${VAR}` と `~`） |
| 3-7 | 識別子の正規化を型で強制する |
| 3-17 | **二重起動は `flock` で防ぐ。`ps` は使わない** |
| 3-24 | 設定を読み直して失敗しても落ちない。**作らないと決めた**（[tasks.md](tasks.md) の「仕様のうち、作らないと決めたもの」） |
| 3-32 | **`continuo init` が置くもの**（`WORKFLOW.md` 1つだけ） |

## `continuo init`

**どこに作るか。**雛形を書き出す実体は **`internal/scaffold`** に置き、`cmd/continuo` はそれを呼ぶだけにする。
**公開する形。**

```go
// Result は WriteTemplate が何をしたかを返す。
type Result struct {
    Path        string // 書いたファイルの絶対パス。symlink は辿った先（実体）で返す
    Overwritten bool   // 既存を上書きしたか（--force のとき真）
}

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て終了コードを決める。
var (
    ErrAlreadyExists = errors.New("WORKFLOW.md が既にあります")
    ErrDirNotFound   = errors.New("指定されたディレクトリがありません")
    ErrNotADirectory = errors.New("指定されたパスはディレクトリではありません")
    ErrSymlink       = errors.New("WORKFLOW.md が symlink です")
)

// WriteTemplate は dir の直下に WORKFLOW.md の雛形を書く。
// dir が空文字なら、いまいるディレクトリ（os.Getwd）に書く。
// Result.Path は必ず絶対パスに正規化して返す（dir が相対でも空文字でも）。
// dir 自身やその親が symlink なら、辿った先（実体）のパスを基準に組み立てる。
// 書き出す先の WORKFLOW.md が symlink なら、--force でも辿らずに ErrSymlink で止める。
func WriteTemplate(dir string, force bool) (Result, error)

// Values は雛形のプレースホルダに書き込む値である。ゼロ値は「決まらなかった」を表す。
type Values struct {
    Owner         string // tracker.provider.owner
    ProjectNumber int    // tracker.provider.project_number
}

// WriteTemplateWithValues は values で埋めてから書く。それ以外は WriteTemplate と同じ。
func WriteTemplateWithValues(dir string, force bool, values Values) (Result, error)

// Detect は gh から owner と project_number を引く。失敗しても error を返さない
// （雛形そのものは書けるため）。決まらなかった理由と直し方は Field に入れて返す。
func Detect(ctx context.Context, opts DetectOptions) Detection
```

> **`Result.Path` は symlink を解決した実体のパスにする。**`os.Stat` も `os.OpenFile` も
> ディレクトリの symlink は辿るので、書き込みは必ずリンク先へ落ちる。リンク側のパスを報告すると
> **「壊した場所」と「報告した場所」が食い違う。**存在を確かめたあとに `filepath.EvalSymlinks` を通す
> （対象が存在しないとエラーになるので、存在の検査より後に呼ぶ）。

> **`dir` が空文字のときの意味を1つに決める。**「いまいるディレクトリ」である。
> **`cmd/continuo` 側で `os.Getwd` を呼んで渡す形にはしない。**
> そうすると `test/` から `WriteTemplate("")` を呼んだときの挙動が決まらず、
> 「位置引数を省いたら、いまいるディレクトリに書く」をテストで確かめられない
> （`cmd/continuo` の `run` は `package main` の非公開関数なので `test/` から呼べない）。

**エラーの文言と終了コードの対応は `cmd/continuo` 側で決める。**

> **CLI の入出力を検証する受け入れ基準は、`internal/scaffold` の戻り値で確かめる。**
> `cmd/continuo` の `run(args, stdout, stderr) int` は `package main` の非公開関数なので `test/` から呼べない。
> **「終了コード 1」「標準エラーへ出す」は、`WriteTemplate` が返すエラーの種類で判定する。**
**`package main` の非公開関数は `test/` から呼べない**ので、テストが書けなくなる。

**雛形の中身は 5-2 と 5-3 をそのまま使う。**新しく考えない。
**コメント付きの YAML をそのまま持つ必要があるので、Go の文字列リテラルとして埋め込む**
（構造体から `yaml.Marshal` するとコメントが消える）。

**利用者に手で埋めさせない**（設計 3-32）。**gh から引いて雛形に書き込む。**

| キー | 何から決めるか | 引けなかったとき |
| --- | --- | --- |
| `tracker.provider.owner` | `gh api user --jq .login` | `__FILL_ME__` を残す（文字列） |
| `tracker.provider.project_number` | `gh project list` の**候補が1件のときだけ** | `0` を残す（**数値。**文字列を入れると YAML の読み込みで落ちる） |
| `herdr.socket` | 既定のパス `~/.config/herdr/herdr.sock` | — |
| `workspace.root` | 既定のパス `~/worktrees` | — |

**雛形のコメントに設計の節番号を書かない。**WORKFLOW.md を開く人は設計文書を持っていない。
front matter のコメントは設計 5-2 の設定例と同じ文面にそろえる（違うのは上の2行の値だけである）。

## 受け入れの基準

- [x] **`WORKFLOW.md` の雛形を置く。**置くのはこの1ファイルだけである
- [x] **書き出した `WORKFLOW.md` を `config.Load` に通すと、
      「`tracker.provider.owner` がプレースホルダのままです」というエラーで落ちる**
  - **プレースホルダの検出を `internal/config` の検証に足す。**キー名を名指しする
  - **検出は他の検証より先に走らせる。**`project_number: 0` は既存の検証でも落ちるので、順序が逆だと
    「プレースホルダのままです」ではなく「0以上にすること」が出てしまう
  - **`project_number: 0` は「プレースホルダのまま」として報告する**（「0より大きい整数にすること」ではない）
    - **雛形が 0 を置いているので、0 は「まだ埋めていない」という意味である**
  - **キー自体が書かれていない場合も同じ文言にする。**読み込むと 0 になるので、区別できない
    （区別するには `*int` にする必要があるが、そこまでする価値が無い）
  - **複数のプレースホルダが残っていたら、まとめて1つのエラーに並べる**（1つずつ直させない）
- [x] **プレースホルダを埋めた `WORKFLOW.md` は `config.Load` を通る**
- [x] **位置引数はディレクトリを取る。**その直下に `WORKFLOW.md` を書く
- [x] **位置引数を省いたら、いまいるディレクトリに書く**
- [x] **既にファイルがあれば上書きせずに、終了コード 1 と
      「`<パス>` は既にあります。上書きするなら --force を付けてください」を標準エラーへ出す**
- [x] **`--force` を付けたときだけ上書きする。**上書きしたことを標準出力に出す
- [x] **雛形に `__FILL_ME__` と `# ここを埋めること` の両方が含まれる**（gh から引けなかったときに残る形）
- [x] **`continuo init` が gh から owner と project_number を引いて雛形に書き込む**
  - **`--owner` / `--project` が渡されたら gh を1回も起動しない**
  - **ボードの候補が複数なら選ばない。**候補を番号・名前・URL で並べ、`--project <番号>` を案内する
  - **候補が0件・gh が無い・認証が無いときも失敗させない。**プレースホルダを残し、直し方を出す
  - **対話で選ばせない**（標準入力を握らない）
  - **埋めた行はコメントごと差し替える**（「ここを埋めること」を残さない）
  - **owner は英数字で始まり英数字とハイフンだけの39文字以内に限る。**外れた値は書き込まない
- [x] **プレースホルダが2つとも残っていたら、1つのエラーに2件とも並べて出す**
- [x] **新規に作成できたら、書いたパスを標準出力に出す**（`WORKFLOW.md を作成しました: <パス>`）
- [x] **位置引数のディレクトリが無ければ作らずにエラーにする**（`--force` でも作らない）
- [x] **位置引数がファイルのパスだったらエラーにする**（ディレクトリだけを受ける）
- [x] **書き出す先の `WORKFLOW.md` が symlink なら、`--force` でも辿らずに止める**
      （`ErrSymlink`。リンク先の中身は1バイトも変えない）
- [x] **位置引数のディレクトリ自身が symlink でも、`Result.Path` は実体側のパスを返す**
      （書き込む場所と報告が食い違わないようにする）
- [x] **`--help` / `-h` は終了コード 0 を返す**（引数の指定の誤りの 2 と区別する）

## 実装の記録

**実装済み**（2026-08-18〜19）。

| パッケージ | 何を |
| --- | --- |
| `internal/config` | front matter と本文の読み込み・展開・検証・既定値 |
| `internal/normalize` | 識別子の正規化（型で強制する） |
| `internal/logging` | 構造化ログ（`log/slog`） |
| `internal/lock` | `flock` による二重起動の防止 |
| `internal/socketpath` | socket の置き場所の探索と長さの検査 |
| `internal/scaffold` | `WORKFLOW.md` の雛形の書き出し（`continuo init` の実体） |
| `cmd/continuo` | CLI（本体と `init` / `hook` サブコマンド。**`hook` は骨組みだけ**） |

**設計文書の設定例をそのまま読み込めるかを検証するテストを持っている**
（`test/internal/config/design_example_test.go`）。**設計に足したキーが実装に無いと、その場で落ちる。**
このテストのおかげで、設計と実装のずれが4回とも即座に見つかった。

**雛形は、設計 5-2 の設定例とキーの集合を直接突き合わせている**
（`test/internal/scaffold/design_template_test.go`）。**比べるのは値ではなくキー構成である**
（雛形の値はプレースホルダなので、設計の設定例とは違って当然）。
入れ子を `.` でつないだキーのパス（例 `cleanup.require_pushed`）の集合が、
設計 5-2 の YAML ブロックと雛形の front matter で完全に一致することを求める。

**本文は、設計 5-3 の ```markdown ブロックと一字一句そのまま突き合わせる**（同じテストファイル）。
雛形の本文は設計 5-3 を機械的に写したものでプレースホルダの差し替えが無い（差し替えるのは
front matter の値だけである）ので、完全一致を求めてよい。**テスト側に書いた文字列
（`{{.issue.identifier}}` など）との照合では、本文が設計から離れても落ちない。**
食い違ったときは、最初にずれた行の番号と設計側・雛形側の中身を出す。

**`config.Load` に通すだけでは足りない。**雛形からキーや節を丸ごと削っても
`DefaultConfig()` が既定値で埋めるため、`config.Load` は素通りする
（`cleanup:` を丸ごと削っても起動が通ることを実測した）。
`config.Load` が弾けるのは「設計に無いキーを雛形に足したとき」だけである。

**プレースホルダの検出は `internal/config/placeholder.go` に置き、`validate` より先に呼ぶ。**
呼ぶ場所は `parseFrontMatter`（unmarshal の直後）である。

**引っかかった点。**

- **`ps` で二重起動を判定してはならない。**`continuo hook` が本体と同じ実行ファイル名なので、
  hook が走っているだけで「2つ目が立っている」と誤判定する
- **socket のパスは 103 バイトが上限**（macOS の実測値）。設定を読んだ時点で落とす
- **`MkdirAll` は既存ディレクトリの権限を直さない。**そのあとに `Chmod` を必ず呼ぶ
- **雛形の本文（設計 5-3）はバッククォートを含む。**raw string literal 1本に入らないので、
  バッククォートの位置で切って `"` + "`" + `"` を挟んで連結している（`internal/scaffold/template.go`）
- **`--force` が無いときの書き出しは `O_EXCL` で行う。**先に `os.Stat` で存在を見てから書くと、
  その隙間に別のプロセスが作ったファイルを黙って壊しうる
- **書き出しは必ず `syscall.O_NOFOLLOW` を付けて開く。**`os.WriteFile` も素の `os.OpenFile` も
  symlink を辿るので、`<dir>/WORKFLOW.md` が symlink だと `--force` が指定ディレクトリの外に
  あるリンク先を雛形で潰す。辿った場合は `ELOOP` が返るので、`scaffold.ErrSymlink` に包んで
  「WORKFLOW.md が symlink です」と名指しで止める。`--force` でも辿らない
  （辿らせないことが目的なので、`--force` を勧める文言も出さない）
- **`WORKFLOW.md` というファイル名の定数は `config.DefaultFileName` の1つだけにする。**
  `internal/scaffold` はそれを参照する（`const fileName = config.DefaultFileName`）。
  同じ値の定数を2つ持つと、片方だけ直したときに init が置いたファイルを本体が読まなくなるのに、
  ビルドもテストも通ってしまう
- **`--help` / `-h` は終了コード 0 で返す。**`flag` は `-h` で `flag.ErrHelp` を返すので、
  `errors.Is(err, flag.ErrHelp)` で判定して 0 にする（`cmd/continuo` の `parseErrorExitCode`）。
  利用者が意図して求めたものなので、引数の指定の誤り（2）と同じ扱いにしない
- **`t.TempDir()` は macOS では `/var/...`（`/private/var` への symlink）を返す。**
  `os.Getwd` も `WriteTemplate` の `Result.Path` も解決後のパスを返すので、
  テストの期待値は `filepath.EvalSymlinks` を通してからそろえる
  （`test/internal/scaffold/scaffold_test.go` の `wantWorkflowPath`）
