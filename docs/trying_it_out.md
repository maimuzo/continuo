# 試してみる / Trying it out

**English:** This walks `continuo` through one issue end to end **on a board you already have** —
no new board is created. Build it, check that the existing `Status` field already carries the five
options `continuo` needs, generate `WORKFLOW.md` with `continuo init` (owner, project number and the
list of repositories are filled in automatically from `gh`), register folder trust for the
repositories you kept with `continuo trust`, grant Keychain access once with
`continuo allow-keychain-access` (**macOS only**), check the prerequisites with `continuo doctor`, put one
issue into `Ready`, and watch `continuo` open a worktree and drive Claude Code until the board says
`Done`. Every command block states which directory to run it in and works on its own. The table
below says which steps were actually executed while writing this document and which were not.

**言いたいこと。**1件の issue が `Ready` から `Done` まで通るのを、実際に目で見るための手順である。
**カンバンは新しく作らない。いま使っているカンバンにそのまま足して使う。**
**すべてのコマンドは「どこで実行するか」を書いてあり、そのブロックだけで動く。**

## この文書のどこを実際に叩いたか

**叩いていない段・叩けない段は、そう明記してある。**

| 段 | 叩いたか | 補足 |
| --- | --- | --- |
| 段1 ビルドする | **叩いた** | `go build` と、各サブコマンドの `--help` |
| 段2 使うカンバンを確かめる | **叩いた（読むだけ）** | `gh project list` と `gh project field-list`。**本番のカンバンには読み取りしか行っていない** |
| 段3 設定を置く | **叩いた** | 自動で埋まるとき・`--owner` / `--project` を渡すとき・`gh` が無いとき・既にあるときの4通り |
| 段4 Status の割り当てを合わせる | **叩いた** | `continuo setup` を本番のカンバンに対して実行した（読み取りのみ） |
| 段5 clone して信頼を登録する | **叩いた** | `continuo trust --dry-run` は実物の `~/.claude.json` に対して（読むだけ）。書き込みはテスト用ホームディレクトリで確かめた |
| 段5b Keychain へのアクセスを許可する | **叩いた** | 実物の Keychain に対して（読むだけ）。**確認のダイアログは出なかった**（2026-08-21、macOS） |
| 段6 前提を検査する | **叩いた** | 揃っているとき・フィールド名が違うとき・設定が未記入のときの3通り |
| 段7 issue を1件用意する | **テスト用mock一式で叩いた** | 偽の `gh` とテスト用GitHub GraphQL mock に対して。**本番のカンバンには1リクエストも送っていない** |
| 段8 動かす | **両方叩いた** | **本物で起動し、巡回が始まるところまで確かめた**（`Ready` が0件なので Claude Code は起動していない）。1件を `Ready` から `Done` まで通したのはテスト用mock一式のほう |
| 段8b 着手を取り消す | **`--help` だけ叩いた** | 貼ってある `abandon --help` の出力は実際に叩いたものである（2026-08-24）。**消す実行は、本物にもテスト用mock一式にも叩いていない。**出力例は文言の資源（[internal/i18n/messages/ja.json](../internal/i18n/messages/ja.json)）から組み立てたものである |
| 段9 止める・片付ける | **両方叩いた** | 本物にもmockにも `Ctrl+C` 相当の `SIGINT` を送り、終了コード 0 で終わることを確かめた |

**本物に対して確かめていないのは「実際に issue を1件通すこと」だけである**（段7 の issue 作成と、
段8 で Claude Code が動く部分）。**そこから枠を消費するので、人間が判断すること。**

**段7〜段9 の「テスト用mock一式」は [test/e2e](../test/e2e) にある。**偽の `gh` / テスト用GitHub GraphQL mock /
テスト用herdr mock / テスト用Claude Code mock / 隔離したホームディレクトリの5つと、本物の git を組み合わせてある。
**実物の Claude Code は1回も起動していないので、枠は消費していない。**
実際に人間が本物に対して叩くと、段8 から枠を消費する。

> **出力例は実際に叩いた結果である**（例外は段8b の消す実行だけで、上の表にそう書いてある）。
> ただし個人のパス・アカウント名・リポジトリ名・カンバンの名前だけを
> `~` と `<ACCOUNT>` / `<PROJECT>` / `<REPO-1>` / `<カンバンの名前>` に置き換えてある。

---

## 先に決めること

**この手順では、次の3つを自分の値に置き換える。**先に決めて手元に控えておく。

| 記号 | 何を入れるか | 例 |
| --- | --- | --- |
| `<ACCOUNT>` | あなたの GitHub アカウント名 | `octocat` |
| `<PROJECT>` | **いま使っているカンバンの番号**（段2 で確かめる） | `3` |
| `<REPO>` | **試す対象のリポジトリ**（issue を置く場所） | `octocat/sandbox` |

> **`<REPO>` は使い捨てにできるものを選ぶ。**エージェントがそのリポジトリを実際に編集し、
> commit して push する。**本業のリポジトリを指定しない。**
>
> **`<REPO>` が既にカンバンに載っていれば、段5 の手作業は無くなる。**
> 段3 の `continuo init` が `trust.repositories` をカンバンから拾って埋めるので、
> **そこに `<REPO>` が入っていれば、段5 は `continuo trust` を1回叩くだけである。**
> 新しいリポジトリを使うなら、段5 で1行足す。

## 先に知っておくこと

| 何を | なぜ |
| --- | --- |
| **カンバンは作らない** | continuo は**既にあるカンバンに後から足して使う。**足りない選択肢があるときだけ画面で足す（段2） |
| **段8 から枠を消費する** | 実際に Claude Code が起動し、issue を実装しようとする |
| **draft item は動かない** | カンバンの draft item は**リポジトリを持たないので作業場所を決められない。**continuo は着手せず飛ばす。**リポジトリの issue を載せること** |
| **agent teams には対応していない** | Claude Code の実験的な機能で、**既定では無効。**有効にしていると issue が失敗することがある。[docs/FAQ.md](FAQ.md) の「「作業の途中で確認の画面に止まりました」と出る（agent teams が有効な場合）」を参照 |
| **止めるのは `Ctrl+C`** | 巡回を止め、hook の受け口を閉じ、turn ループを畳んで抜ける。**Claude Code はそのまま動き続ける。pane は閉じない**（次の起動で引き継ぐ） |

---

## 段1. ビルドする

**実行する場所: continuo のリポジトリの中**

```bash
cd ~/Sources/github/continuo     # continuo を clone した場所
go build -o /tmp/continuo ./cmd/continuo
/tmp/continuo init --help        # 動くことの確認。終了コード 0 が返る
```

**Go 1.26 以上が要る**（`testing/synctest` を使っている）。
**リポジトリの中にビルドの生成物を置かない**ため、実行ファイルは `/tmp/continuo` へ出している。
**以降はこの絶対パスで叩く**ので、どのディレクトリからでも実行できる。

**[mise](https://mise.jdx.dev/) で Go を入れているなら、clone した直後に1回だけ次を実行すること。**

```bash
cd ~/Sources/github/continuo
mise trust                       # 1回だけ。リポジトリの mise.toml を信頼する
```

**これを飛ばすと `go build` が次のエラーで止まる**（実測: 2026-08-20）。

```text
mise ERROR Config files in ~/Sources/github/continuo/mise.toml are not trusted.
Trust them with `mise trust`. See https://mise.jdx.dev/cli/trust.html for more information.
zsh: command not found: go
```

リポジトリの [mise.toml](../mise.toml) が `go = "1.26.2"` を指定している。
**`go.mod` の `go 1.26` は mise が読まないため、この指定が別に要る。**
mise を使っていない場合は、[go.dev/dl](https://go.dev/dl/) で入れた Go に PATH が通っていればよい。

`init --help` の出力。

```text
Usage of continuo init:
  -force
    	既に WORKFLOW.md があっても上書きする
  -owner string
    	tracker.provider.owner に書く GitHub の user / organization 名（省略すると gh から引く）
  -project int
    	tracker.provider.project_number に書くカンバンの番号（省略すると gh から引く）
```

`setup --help` の出力。**`--force` は無い**（段4 で説明する）。

```text
Usage of continuo setup:
  -owner string
    	Status の選択肢を読むカンバンの GitHub の user / organization 名（省略すると gh から引く）
  -project int
    	Status の選択肢を読むカンバンの番号（省略すると gh から引く）
  -status-field string
    	Status を読み書きする single-select フィールドの名前（既定 Status） (default "Status")
```

`prompt --help` の出力。**`--show` は必須である**（付けないと終了コード 2 で案内が出る）。

```text
Usage of continuo prompt:
  -builtin
    	WORKFLOW.md を読まず、組み込みのプロンプトだけを出す
  -show
    	送るプロンプトの全文を標準出力へ出す
```

`trust --help` の出力。

```text
Usage of continuo trust:
  -dry-run
    	何が要求されているかを表示するだけで、~/.claude.json を書き換えない
```

`abandon --help` の出力。**位置引数を自分で出している**（段8b で使う）。

```text
continuo abandon — 間違えて着手した issue を、着手する前の状態へ戻します。

使い方:
  continuo abandon <issue の URL> [ディレクトリ]

位置引数:
  <issue の URL>                 例 https://github.com/octocat/hello-world/issues/42
  [ディレクトリ]                 WORKFLOW.md のあるディレクトリ（省略すると、いまいるディレクトリ）

フラグ:
  -dry-run
    	何を消すかを見せるだけで、消さない
  -force
    	コミットされていない変更や push されていない commit があっても消す
  -park string
    	continuo が動いているときに、手を離させるため一時的に動かす先（省略すると tracker.failure_state）
  -to string
    	片付けたあとに Status をこの値へ動かす（省略すると動かさない）
```

**サブコマンドは次のとおりである。**`init` / `setup` / `trust` / `abandon` /
`allow-keychain-access` / `doctor` / `prompt` / `version` / `hook` で、
引数に何も渡さなければ常駐する。

**フラグは位置引数の前でも後ろでも書ける。**`git` / `docker` / `gh` と同じである。
`continuo abandon <issue の URL> --dry-run` と `continuo abandon --dry-run <issue の URL>` は
同じ意味になる。**`--` より後ろは、`-` で始まっていても
位置引数として扱う。**知らないフラグは、どこに書いてもエラーのままである。

`allow-keychain-access --help` の出力。**フラグは1つも無い**（段5b で使う。macOS 専用）。

```text
Usage of continuo allow-keychain-access:
```

> **実装を更新したら、必ずここへ戻ってビルドし直すこと。**
> `/tmp/continuo` は前に建てたものが残る。**新しいサブコマンドを叩くと
> 「設定ファイルが見つからない」という分かりにくいエラーになる**
> （知らない引数は設定ファイルのパスとして扱われるため）。

---

## 段2. 使うカンバンを確かめる（作らない）

**言いたいこと。**continuo は**既にあるカンバンに後から足して使うものである。**
**要るのは `Status` フィールドに5つの選択肢が揃っていることだけで、
揃っていれば `WORKFLOW.md` は既定のままでよい。**

**実行する場所: どこでもよい**（GitHub に対する操作である）

```bash
gh project list --owner <ACCOUNT>
```

実際に叩いた出力。左端の数字が `<PROJECT>` である。

```text
3	<カンバンの名前>	open	PVT_...
```

**実行する場所: どこでもよい**

```bash
gh project field-list <PROJECT> --owner <ACCOUNT> --format json \
  | jq -r '.fields[] | select(.type=="ProjectV2SingleSelectField") | "\(.name): \([.options[].name] | join(", "))"'
```

実際に叩いた出力。

```text
Status: Ice Box, Ready, In Progress, Blocked, In Review, Done
Priority: P0, P1, P2, P3
Size: XS, S, M, L, XL
```

**`Status` に必要な5つが全部ある。**このカンバンは**何も足さずにそのまま使える。**

| 選択肢 | 何に使うか | `WORKFLOW.md` のどのキーか |
| --- | --- | --- |
| `Ready` | **ここに置いた issue を continuo が取る** | `dispatch_state` |
| `In Progress` | 処理開始したときに continuo が書き込む | `running_state` |
| `In Review` | エージェントが `CONTINUO-STATUS: review` を出すと入る | `status_signal_map.review` |
| `Blocked` | 判断を仰ぐとき、または打ち切ったときに入る | `failure_state` |
| `Done` | **人間がここへ動かすと、continuo が worktree と branch を片付ける** | `terminal_states` |

`Ice Box` のように**continuo が知らない選択肢があっても構わない。**
`active_states`（既定は `Ready` と `In Progress`）に書いていない選択肢は、ただ無視される。

### 足りない選択肢があったとき

| 道 | 何をするか | 画面の作業 |
| --- | --- | --- |
| **選択肢を足す** | カンバンを開く → 右上の `⋯` → `Settings` → 左の `Status` → `+ Add option` | 足す数だけ |
| **設定を縮める** | `Status` に実在する名前だけで回るように `WORKFLOW.md` を書き換える（段4） | 無し |

> **選択肢を API で足してはならない。**足す API は `updateProjectV2Field` しか無く、
> **選択肢の指定を全件置き換えとして扱う。**GitHub が全部の選択肢に ID を採番し直すので、
> **設定済みの Status の値が全部消える。**必ず GitHub の画面から追加する。

### 専用のフィールドを使ってもよい

**`status_field` に書いた名前のフィールドが、絞り込み・読み取り・書き込みのすべてで使われる。**
`continuo Status` のように**空白を含む名前でもよい**（設計 3-34。絞り込みのキーは引用符で囲んで組み立てている）。
組み込みの `Status` を別の目的で使っているカンバンでは、この道を採る。

**確かめていないこと。**この文書を書くにあたって、専用フィールドを実際には作っていない。
**本番のカンバンへ書き込まないためである。**空白を含むフィールド名が絞り込みのキーとして解決されることは、
設計 3-34 に読み取り専用クエリでの実測が載っている。

**綴りが1文字でも違うとカンバンを読めない。**実際に、存在しない `continuo Status` を指定して
`continuo doctor` を叩いた出力（段6 に全体を載せてある）。

```text
✗ カンバン        カンバンを読めません: tracker エラー [tracker_response]: GraphQL がエラーを返しました: [NOT_FOUND] Could not resolve to a Unions::ProjectV2FieldConfiguration with the name continuo Status
                  → WORKFLOW.md の tracker.provider（owner / project_number / status_field）を確認してください
```

---

## 段3. 設定を置く

**言いたいこと。**continuo は**いまいるディレクトリの `WORKFLOW.md`** を読む。
だから**試用のための空のディレクトリを1つ作り、そこに設定を置く。**

### 出てくる2つの場所

| 場所 | 何か |
| --- | --- |
| `/tmp/continuo` | **段1 でビルドした実行ファイルそのもの。**リポジトリの中に生成物を残さないために `/tmp` へ出した。どこからでも絶対パスで叩ける |
| `~/continuo-try` | **continuo を動かす作業ディレクトリ。**ここに `WORKFLOW.md` を置く。試用のためだけの空のディレクトリでよく、終わったらまるごと消せば片付く |

**実行する場所: `~/continuo-try`**（新しく作る）

```bash
mkdir -p ~/continuo-try
cd ~/continuo-try
/tmp/continuo init
```

`~/continuo-try/WORKFLOW.md` が1枚できる。
**front matter（設定）と本文（その project でだけ効く指示）が、このファイルに入っている。**
**`owner` と `project_number` と `trust.repositories` は `gh` から引いて自動で入る。**
入った値はその場に出る。実際に叩いた出力。

```text
WORKFLOW.md を作成しました: ~/continuo-try/WORKFLOW.md
✓ tracker.provider.owner: <ACCOUNT>（`gh api user` が返した GitHub のログイン名です）
✓ tracker.provider.project_number: <PROJECT>（`gh project list` の候補が1件だけでした: #<PROJECT> <カンバンの名前>）
✓ trust.repositories: <ACCOUNT>/<REPO-1>, <ACCOUNT>/<REPO-2>, <ACCOUNT>/<REPO-3>, <ACCOUNT>/<REPO-4>, <ACCOUNT>/<REPO-5>（カンバン #<PROJECT> に載っている 5 個のリポジトリを並べました）
  → **要らない行は WORKFLOW.md から消してください。**残ったものだけが `continuo trust` の対象になります
  → 何を許すことになるかは `continuo trust --dry-run` で確かめられます
```

**`trust.repositories` は「並べただけ」である。**continuo はここから勝手に信頼を登録しない。
**要らない行を消すのは人間の仕事で、消し終えてから段5 を叩く。**

### 自動で入らないとき

**カンバンが2枚以上あると `project_number` は自動で決まらない。**候補が番号・名前・URL で並ぶので、
段2 で確かめた番号を指定して置き直す。

> **organization のカンバンを使うなら `--owner <組織名>` を必ず渡す。**
> `continuo init` は `gh api user` に聞くので、**渡さないと個人のログイン名が入る。**
> しかもその値は `✓` として報告されるので、**間違っていることが分からない。**
> 段4 の `continuo setup` も同じ経路でカンバンを決めるので、そちらにも渡す。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo init --owner <ACCOUNT> --project <PROJECT> --force
```

`--owner` と `--project` を渡すと**`gh` を叩かずにその値を書く。**実際に叩いた出力。
**`--force` で上書きしたときは1行目が「上書きしました」に変わる。**

```text
WORKFLOW.md を上書きしました: ~/continuo-try/WORKFLOW.md
✓ tracker.provider.owner: <ACCOUNT>（--owner で指定された値です）
✓ tracker.provider.project_number: <PROJECT>（--project で指定された値です）
✓ trust.repositories: …（カンバン #<PROJECT> に載っている 5 個のリポジトリを並べました）
```

**`gh` が無い・認証が無いときも `WORKFLOW.md` は作られ、終了コードは 0 である。**
その場合だけプレースホルダ（`__FILL_ME__` と `0`）が残るので、案内どおりに手で埋める。
実際に `gh` を PATH から外して叩いた出力。
**`octocat` は文言に埋め込んである固定の例であり、あなたのアカウント名ではない。**

```text
WORKFLOW.md を作成しました: ~/continuo-try/WORKFLOW.md
! tracker.provider.owner: 埋められませんでした（gh コマンドが見つかりませんでした）
  → gh を入れて `gh auth login -s project` でログインしてください
  → または `continuo init --owner <名前>` でもう一度実行してください
  → https://github.com/octocat なら octocat の位置が owner です
! tracker.provider.project_number: 埋められませんでした（owner が決まらないので、カンバンの候補を引けませんでした）
  → 先に owner を決めてから、もう一度 `continuo init` を実行してください
  → または `continuo init --project <番号>` でカンバンの番号を直接指定してください
! trust.repositories: 埋められませんでした（owner とカンバンの番号が決まらないので、カンバンに載っているリポジトリを引けませんでした）
  → owner とカンバンの番号を決めてから、もう一度 `continuo init` を実行してください
  → `continuo trust` の対象は WORKFLOW.md の trust.repositories に手で書いても構いません
埋まらなかった値は WORKFLOW.md の中でプレースホルダのままです。上の案内どおりに書いてください
```

**既にあると `init` は上書きせずに止まる**（終了コード 1）。

```text
~/continuo-try/WORKFLOW.md は既にあります。上書きするなら --force を付けてください
```

**`WORKFLOW.md` の本文が、エージェントへ送る指示書のうち人間が書く部分である。**
残りは continuo の実行ファイルの中にある。**送られる全文はこう読む。**

```bash
cd ~/continuo-try && /tmp/continuo prompt --show
```

**内訳は標準エラーへ出る。**実際に叩いた出力（`>` で捨てているのは標準出力の側である）。

```text
送る文面の内訳:
  組み込みのプロンプト（前半）  360 行
  WORKFLOW.md の本文  21 行  ~/continuo-try/WORKFLOW.md
  組み込みのプロンプト（後半）  438 行
```

**行数は版によって変わる。**上は `continuo init` が置いたままの `WORKFLOW.md` で実測した値である。
**本文へ書き足せば真ん中が増え、組み込みへ節が増えれば前半か後半が増える。**
**`language` の値では変わらない。**組み込みの指示書は、いまのところ日本語だけである。

**本文が真ん中に挟まる。**組み込みの締めくくり（表明の1行の説明）が必ず最後に来る。
**組み込みの側だけを読みたいときは `--builtin` を付ける。**

```bash
cd ~/continuo-try && /tmp/continuo prompt --show --builtin
```

**`workspace.root` も見ておく。**worktree を置く場所である（既定は `~/worktrees`）。

---

## 段4. Status の割り当てを合わせる

**言いたいこと。**`continuo setup` が、カンバンの選択肢を continuo の5つの役割へ割り当てる。
**役割の説明が出るので、それを読んで番号で選ぶ。**
**書き換わるのは `Status` に関する7行だけで、段3 で手を入れた行はそのまま残る。**

**段3 で作った `WORKFLOW.md` に対して実行する。**`continuo setup` は雛形を作らないので、
`WORKFLOW.md` が無いときは段3 をやり直すよう案内して止まる（終了コード 1）。

> **`continuo setup` は、どのカンバンを読むかを `WORKFLOW.md` から決めない。**
> `gh` に聞き直す。だから次の3つの場合はフラグで指定する。
>
> ```bash
> cd ~/continuo-try
> /tmp/continuo setup --owner <ACCOUNT> --project <PROJECT> --status-field Status
> ```
>
> | いつフラグが要るか | 指定しないとどうなるか |
> | --- | --- |
> | **カンバンが2枚以上ある** | カンバンを決められず、終了コード 1 で止まる（段3 と同じ） |
> | **organization のカンバンを使う** | `gh api user` が返す個人のログイン名で探すので、**別のカンバンの選択肢を読む** |
> | **`Status` 以外のフィールドを使う**（段2 の「専用のフィールド」） | 既定の `Status` を読む。`--status-field` に段2 で決めた名前を渡す |

```text
~/continuo-try/WORKFLOW.md がありません。continuo setup は既にある WORKFLOW.md の Status の割り当てだけを書き換えます（役割の割り当ては1つも尋ねていません）
→ 先に continuo init を実行して WORKFLOW.md を作ってください
```

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo setup
```

**実際の出力**（このカンバンの選択肢は段2 で確かめた6つ）。

```text
カンバンの Status フィールドには次の選択肢があります。
  1  Ice Box
  2  Ready
  3  In Progress
  4  Blocked
  5  In Review
  6  Done

これから 5 個の役割について、それぞれどの選択肢を使うかを尋ねます。番号で答えてください。
その役割に使える選択肢がカンバンに無い場合は 0 を入力してください。
Ctrl+C で中断できます。中断したときは WORKFLOW.md を書き換えません。

[1/5] dispatch_state: continuo が自動的に処理を開始する Status は何番ですか?
番号> 2
  → dispatch_state に "Ready" を割り当てました

[2/5] running_state: continuo が処理を開始したときに移動する Status は何番ですか?
番号> 3
  → running_state に "In Progress" を割り当てました

[3/5] status_signal_map.review: エージェントが作業を完了したときに移動する Status は何番ですか?
番号> 5
  → status_signal_map.review に "In Review" を割り当てました

[4/5] status_signal_map.blocked / failure_state: エージェントが判断を仰ぐとき・打ち切ったときに移動する Status は何番ですか?
番号> 4
  → status_signal_map.blocked / failure_state に "Blocked" を割り当てました

[5/5] terminal_states: 人間がここへissueを移動したら作業完了とみなしgit worktreeを削除する Status は何番ですか?
番号> 6
  → terminal_states に "Done" を割り当てました

5 個の役割の割り当ては次のとおりです。
  dispatch_state: "Ready"
  running_state: "In Progress"
  status_signal_map.review: "In Review"
  status_signal_map.blocked / failure_state: "Blocked"
  terminal_states: "Done"

WORKFLOW.md の Status の割り当てを書き換えました: ~/continuo-try/WORKFLOW.md
書き換えたキー:
  tracker.status_signal_map.review
  tracker.status_signal_map.blocked
  tracker.active_states
  tracker.terminal_states
  tracker.running_state
  tracker.dispatch_state
  tracker.failure_state
```

### 段3 で手を入れた内容は消えない

**`continuo setup` に `--force` は無い。**書き換えるのが上の7行だけなので、
**上書きから守るものが無くなった。**段3 で `trust.repositories` から消した行も、
`workspace.root` や `agent.max_concurrent_agents` を書き換えた値も、そのまま残る。
**行の右側のコメント・空行・並び順・インデントも変わらない。**

段3 で `workspace.root` と `agent.max_concurrent_agents` を書き換え、
`trust.repositories` を1行だけ残してから段4 を叩いて、`diff` を取った結果。
**変わったのは `Status` の行だけである**（この例では `dispatch_state` に `Ice Box` を選んだ）。

```diff
-  active_states: ["Ready", "In Progress"]   # 対象にする Status。下の running_state と dispatch_state を必ず含めること
+  active_states: ["Ice Box", "In Progress"] # 対象にする Status。下の running_state と dispatch_state を必ず含めること
   terminal_states: ["Done"]                 # 終わったとみなす Status。ここへ移った issue の worktree を片付ける
   running_state: "In Progress"              # エージェントを起動したときに書き込む Status
-  dispatch_state: "Ready"                   # 着手待ちの Status。取り残された issue はここへ戻す
+  dispatch_state: "Ice Box"                 # 着手待ちの Status。取り残された issue はここへ戻す
```

| 起きること | どうなるか |
| --- | --- |
| **`WORKFLOW.md` が無い** | **止める。**`continuo init` を先に実行するよう案内する（雛形は作らない） |
| **7つのキーのどれかが `WORKFLOW.md` から消されている** | **尋ねる前に止める。**消したキーを名指しする（5問答えさせてから捨てない） |
| 同じ選択肢を2つの役割に選ぶ | **拒否して同じ役割をもう一度尋ねる**（打ち切らない） |
| **番号 `0`**（その役割に使える選択肢が無い） | **打ち切る。**`WORKFLOW.md` は書き換えない |
| 選択肢が5個未満のカンバン | **尋ねる前に止める。**足す手順を出す |
| `Ctrl+C` | 中断する。`WORKFLOW.md` は書き換えない |

### 手で書き換えることもできる

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
${EDITOR:-vi} WORKFLOW.md
```

**書き換えるのは front matter の次のキーだけである。**すべて**カンバンに実在する選択肢名**を書く。

```yaml
tracker:
  provider:
    status_field: Status                     # 読み書きする single-select フィールドの名前
  status_signal_map:
    review: "In Review"                      # エージェントが review を出したときに書く Status
    blocked: "Blocked"                       # 判断を仰ぐとき・失敗したときに書く Status
  active_states: ["Ready", "In Progress"]    # 対象にする Status。dispatch_state と running_state を必ず含める
  terminal_states: ["Done"]                  # 終わったとみなす Status
  running_state: "In Progress"               # 着手したときに書き込む Status
  dispatch_state: "Ready"                    # 着手待ちの Status
  failure_state: "Blocked"                   # 打ち切った・失敗したときに書き込む Status
```

**選択肢を足せない（足したくない）カンバンでの縮め方の例。**
組み込みの `Status` が `Todo` / `In Progress` / `Done` の3つだけなら、次のようにする。

```yaml
  active_states: ["Todo", "In Progress"]
  terminal_states: ["Done"]
  running_state: "In Progress"
  dispatch_state: "Todo"
  failure_state: "Todo"                      # Blocked が無いので、着手待ちへ戻す
  status_signal_map:
    review: null                             # In Review が無いので Status を動かさない
    blocked: "Todo"
    working: null
```

**書き換えたら段6 の `continuo doctor` で照合する。**綴りが1文字でも違うと、
**段8 の起動時検査が起動を止める**（終了コード 1。実測した出力）。

```text
level=ERROR msg="continuo を起動できません" error="起動できませんでした: 起動時の検査に落ちました（生きている pane は閉じずに残します）: カンバンの Status の選択肢名が設定と一致しません（対象0件が無言で続くのを防ぎます）: … : Readyyy"
```

**無言で回り続けることはない。**ただし段6 の `doctor` なら起動する前に気づける。

---

## 段5. 対象リポジトリを信頼に登録する

**言いたいこと。****`continuo trust` を1回叩くだけである。**clone が無ければ `ghq get` で
取ってきて、`~/.claude.json` に信頼を書き込む。**手で `ghq get` を叩く必要も、`claude` を
起動して承認する必要も無い。**

**`trust.repositories` は段3 の `continuo init` がカンバンから拾って埋めている。**
**`<REPO>` がそこに入っていれば、この段でファイルを編集する必要は無い。**

### `<REPO>` が一覧に無ければ書き足す

**言いたいこと。****まず見る。入っていれば何もしなくてよい。**
段3 の `continuo init` が `trust.repositories` をカンバンから拾って埋めている。
**入っていないのは「そのときカンバンに載っていなかったリポジトリ」だけ**である。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
grep -n -A 9 "^  repositories:" WORKFLOW.md    # いま並んでいるものを見る
```

**`<REPO>` の行があれば、この節は飛ばして「何を許すことになるかを先に見る」へ進む。**

無いのは次の場合である。

| いつ無いか | どうするか |
| --- | --- |
| **これから issue を作るリポジトリで試す** | 段7 で初めてカンバンに載るので、いま手で足す |
| 段3 で自分で消した | 消した行を書き戻す |
| `gh` の認証が無い状態で `init` を叩いた | **手で書き足す**（下記）。**`continuo init --force` は使わない** |

> **`continuo init --force` を叩いてはならない。**雛形を全文で書き直すので、
> **段4 の `continuo setup` が書いた Status の割り当ても、段3 で直した
> `workspace.root` や `agent.max_concurrent_agents` も全部消える。**
> どうしても叩いたなら、**段4 からやり直すこと。**

足すときは `${EDITOR:-vi} WORKFLOW.md` を開く。

```yaml
trust:
  repositories:                             # continuo trust が信頼を登録してよいリポジトリ。カンバンから拾って並べた。
                                            # **要らない行は消すこと。**ここに残ったものだけが登録の対象になる
                                            # **これから issue を作るリポジトリは、まだカンバンに無いので入っていない。**手で足すこと
    - "<REPO>"                                # ← 段7 で issue を置くリポジトリ。手で足す
```

#### 何が「信頼済み」を決めるのか

**巡回は `trust.repositories` を読まない。**読むのは `continuo trust` だけである。

| 誰が | 何を見るか |
| --- | --- |
| **巡回のループ** | **`~/.claude.json` の `projects["<clone の絶対パス>"].hasTrustDialogAccepted`。**これが唯一の門番 |
| `continuo trust` | `WORKFLOW.md` の `trust.repositories`。**そこに書かれたものだけ**を `~/.claude.json` へ登録する |
| `continuo doctor` の `信頼登録` | **カンバンに載っている issue のリポジトリ**について `~/.claude.json` を見る |

**だから2つのことが起きうる。**

- **`trust.repositories` に書かなくても取ることがある。**その clone で以前 Claude Code を
  起動していれば、`~/.claude.json` には既に登録されている
- **書いてあっても `continuo trust` を実行していなければ取らない。**書いただけでは
  `~/.claude.json` は変わらない

#### 信頼が無いまま段8 へ進むとどうなるか

**continuo はその issue の処理を開始しない。worktree も pane も作らない。**
**そのリポジトリにつき1回、issue にコメントを投稿する**（`trust.on_untrusted` は
`skip_and_comment` のみ。他の値は設定として受け付けない）。

```text
このリポジトリ（<REPO>）は Claude Code に信頼登録されていないため、continuo は着手できません。

信頼していないフォルダでは Claude Code の hook が1つも動かず、turn の終わりを検知できません。

直し方。WORKFLOW.md の `trust.repositories` に `<REPO>` を書き足してから、`continuo trust` を実行してください。
何を許すことになるかは `continuo trust --dry-run` で先に見られます。
```

**ログは巡回のたびに出る**（issue ごと・30秒ごと）。**この行には直し方が書かれていない。**

```text
level=INFO msg="dispatch できない issue を候補に含めました" identifier=<REPO>#12 理由="リポジトリ … が Claude Code に信頼登録されていません（…設計 3-6 / 4-3）"
```

**直し方が書いてあるのは issue のコメントのほうである。**

### 何を許すことになるかを先に見る

> **`--dry-run` は clone を取りに行かない。**読むだけのつもりで叩いた人のディスクを
> 無断で使わないためである。clone が無ければ次のように出る。
>
> ```text
> ✗ <REPO>
>     clone がありません（`ghq list -p -e <REPO>` の出力が空。--dry-run では取りに行きません）
> ```
>
> **これは失敗ではない。**次の「登録する」で `continuo trust` を叩けば取ってくる。

> **`~/.claude.json` がまだ無い場合**（Claude Code を一度も起動していないマシン）、
> `continuo trust` は**終了コード 3** で止まる。**先に Claude Code を1回起動して、
> このファイルを作らせること。**continuo は `~/.claude.json` を新規には作らない。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo trust --dry-run
```

**`--dry-run` は Claude Code の信頼のダイアログの代わりである。**
信頼すると、そのリポジトリの `.claude/settings.json` の `permissions.allow` と
`permissions.additionalDirectories` が効き、`.mcp.json` の MCP サーバーが使えるようになる。
**`--dry-run` は 1バイトも書き換えない**（実物の `~/.claude.json` に対して叩き、
前後の SHA-256 が一致することを確かめた）。

**未信頼のリポジトリがあるときの出力。**テスト用ホームディレクトリと偽の `ghq` の置き場所を作って
叩いたものである（実物の `~/.claude.json` を書き換えないため）。読みやすさのためにパスを `~` へ置き換えてある。

```text
信頼を登録すると、次の設定が Claude Code に効くようになります。
**そのリポジトリで動くエージェントが、ここに書かれた操作を確認なしで実行できます。**
書き込む先: ~/.claude.json

! demo/sample-a（未信頼。登録の対象）
    登録するパス: ~/ghq/github.com/demo/sample-a
    .claude/settings.json:
      permissions.allow:
        - Bash(rm -rf:*)
        - Read
        - WebFetch(domain:example.invalid)
      permissions.additionalDirectories:
        - /etc
        - ~/.ssh
    .mcp.json の MCP サーバー:
      - docs  （https://example.invalid/mcp）
      - payments  （node server.js --live）

! demo/sample-b（未信頼。登録の対象）
    登録するパス: ~/ghq/github.com/demo/sample-b
    .claude/settings.json: ありません
    .mcp.json: ありません

信頼を登録する対象は 2 件です: demo/sample-a, demo/sample-b

--dry-run なので何も書き換えていません。登録するなら --dry-run を外して実行してください。
```

**登録の対象が残っていると終了コードは 1 である**（`--dry-run` のとき）。

**全部が信頼済みのときの出力。**実物の `~/.claude.json` に対して叩いたものである。
**登録するものが無いときも、先頭の3行は必ず出る。**

```text
信頼を登録すると、次の設定が Claude Code に効くようになります。
**そのリポジトリで動くエージェントが、ここに書かれた操作を確認なしで実行できます。**
書き込む先: ~/.claude.json

✓ <ACCOUNT>/<REPO-1>（既に信頼済み。触りません）
    登録するパス: ~/Sources/github/<REPO-1>
    .claude/settings.json:
      permissions.allow: なし
      permissions.additionalDirectories: なし
    .mcp.json: ありません

すべて信頼済みです。書き込むものはありません。

--dry-run なので何も書き換えていません。登録するなら --dry-run を外して実行してください。
```

> **`登録するパス` は、`ghq list -p -e` が返したものとは限らない。**
> `git rev-parse --show-toplevel` の出力を使うので、`ghq` の置き場所が symlink なら
> 実体側のパスが出る。**`~/.claude.json` に登録されるのはこのパスである。**

### 登録する

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo trust
```

**clone が無ければ、ここで `ghq get` が走る。**取ってきてから信頼を登録する。
実際に叩いた出力の1行目（隔離したホームディレクトリに対して）。
**リポジトリ名はこの節の他の出力と同じく `demo/sample-a` へ伏せてある。**

```text
demo/sample-a の clone がないので `ghq get` で取ってきます（時間がかかることがあります）
```

**書き込む前に、`--dry-run` と同じ要求内容をもう一度出す。**そのうえで書き込む。
実際に叩いた出力の末尾（テスト用ホームディレクトリに対して）。

```text
**Claude Code のセッションが動いていると、~/.claude.json を取り合って書き換えが失われることがあります。**
気になるなら Claude Code を全部終了してから、もう一度実行してください。

バックアップを取りました: ~/.claude.json.continuo-backup-2026-08-20T14:54:38+09:00
**このバックアップは消しません。要らなくなったら人間が消してください。**
~/.claude.json に信頼を登録しました。
  ✓ demo/sample-a → ~/ghq/github.com/demo/sample-a
  ✓ demo/sample-b → ~/ghq/github.com/demo/sample-b
```

**もう一度叩いても何も起きない。**

```text
すべて信頼済みです。書き込むものはありません。

~/.claude.json は書き換えていません（変えるものがありませんでした）。
既に信頼済み: demo/sample-a, demo/sample-b
```

| 守られていること | 確かめ方 |
| --- | --- |
| **`--dry-run` は書き換えない** | 叩く前後で `~/.claude.json` の SHA-256 が一致した |
| **書き換える前に写しを取る** | `~/.claude.json.continuo-backup-<日時>` ができる。**continuo は消さない** |
| **列挙していないリポジトリは登録しない** | 対象は `WORKFLOW.md` の `trust.repositories` に残した行だけである |
| **変えるものが無ければ書き込まない** | 2回目の実行ではバックアップも増えない |

> **`trust.repositories` に書いたものだけが対象である。**カンバンから自動で集めない。
> **issue を足せる人はカンバンに載るリポジトリを変えられる**ので、そこから自動で登録すると
> 信頼させる先を他人が増やせてしまう（設計 3-33）。

---

## 段5b. Keychain へのアクセスを1回許可する（**macOS だけ**）

**実行する場所: どこでもよい**（このコマンドは `WORKFLOW.md` を読まない）

```bash
/tmp/continuo allow-keychain-access
```

**macOS でだけ必要な段である。**Linux / WSL2 では何もせずに終わるので、飛ばしてよい。

**なぜ要るか。**macOS の Claude Code は OAuth トークンを Keychain に置いていて、
`~/.claude/.credentials.json` は無いのが普通である。continuo は枠（レートリミット）の残りを読むために
このトークンを使う。**Keychain は初めて読む実行ファイルに確認のダイアログを出すので、
無人で走る continuo がそれに当たると、答える人がいないまま枠の判定の期限が切れる。**
**人間が端末にいるうちに1回読ませて、「常に許可」で答えておく。**

**実際に叩いた出力**（2026-08-21、macOS。**このときダイアログは出なかった**）。

```text
Keychain の項目 "Claude Code-credentials" を読みます。
確認のダイアログが出たら「常に許可」を選んでください（「許可」だけを選ぶと、次に実行するときまた出ます）。
Keychain の項目 "Claude Code-credentials" を読めました。以後 continuo が枠を読めます
読めた項目: accessToken, expiresAt, rateLimitTier, refreshToken, refreshTokenExpiresAt, scopes, subscriptionType
```

> **確認のダイアログが出たら「常に許可」を選ぶ。**「許可」だけを選ぶと、次に実行するときまた出る。
> **無人運用中に出ると、答える人がいないまま10秒で打ち切られる。**

| 守られていること | 中身 |
| --- | --- |
| **トークンの値を出さない** | 出るのは**項目の名前だけ**である（`accessToken` という名前は出るが、値は出ない）。ログにもエラー文にも載せない |
| **設定ファイルを読まない** | `WORKFLOW.md` がまだ無くても叩ける。読む先は Keychain の1項目に決まっている |
| **待ちっぱなしにならない** | 60秒待っても `security` が返らなければ打ち切り、「ダイアログが出たままかもしれません」と出して終了コード 1 を返す |
| **macOS 以外では何もしない** | 「このコマンドは macOS でだけ意味があります（いまの OS: linux）。何もしませんでした」と出して終了コード 0 |

**読めなかったときは、原因と対処が出る。**

```text
Keychain の項目 "Claude Code-credentials" を読めませんでした: …
【確かめ方】security find-generic-password -s "Claude Code-credentials" -w
【よくある原因】claude でログインしていない / 別のユーザーのログイン Keychain に入っている / ログイン Keychain がロックされている / ダイアログで「許可しない」を選んだ
【対処】claude でログインし直してから、continuo allow-keychain-access をもう一度実行してください。読めないままにするなら、WORKFLOW.md の rate_limit.token_source を env にして環境変数からトークンを渡すか、rate_limit.source を none にして枠の判定を止めてください
```

> **段3 の `continuo init` は、macOS では `rate_limit.token_source: keychain` を書いている。**
> `WORKFLOW.md` の該当行はこうなっている（実際に書き出された行）。
>
> ```yaml
>   token_source: keychain                    # macOS の Keychain から読む。先に continuo allow-keychain-access を1回実行すること。claude_credentials なら ~/.claude/.credentials.json、env なら下の token_env から読む
> ```

---

## 段6. 前提が揃っているかを検査する

**実行する場所: `~/continuo-try`**（`WORKFLOW.md` を置いた場所）

```bash
cd ~/continuo-try
/tmp/continuo doctor
```

見出し語ごとに検査して、足りないものと直し方を出す。**`✗` が1つでもあれば終了コードは 1。**
**既存のカンバンを既定の設定のまま使って**実際に叩いた出力。

> **`資格情報` の行は、段5b を通した macOS で `rate_limit.token_source: keychain` にして
> 取り直したものである**（2026-08-21）。**`claude` から `worktree の場所` までの4行は、
> 同じ macOS で別に叩いて取ったものである**（2026-08-24）。**件数の行はそれに合わせて数え直してある。**
> **hook の socket の場所は、機械ごとに変わる文字列を `$TMPDIR` に置き換えてある。**
>
> **このあとに出てくる `continuo doctor` の出力は、どれも見出し語が7つ足りない。**
> `片付けの状態` と `未記入の項目` と `プロンプトの変数` と `Status の名前` と `対応表のキー` と
> `自動化` と `agent teams` は、この写しを取ったあとに足したものである。
> **並びの正は [internal/doctor/report.go](../internal/doctor/report.go) の `Label` 定数である。**

```text
✓ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めました（front matter の検証も通りました）
✓ claude          ~/.local/bin/claude
✓ hook の置き場所 $TMPDIR/continuo/hooks.sock に socket を作れます
✓ Claude の設定   ~/.claude/session-env に書けます
✓ worktree の場所 ~/worktrees に書けます（workspace.root）
✓ herdr           protocol 20（設定と一致）／herdr 0.8.2／socket ~/.config/herdr/herdr.sock
✓ gh の認証       scope に project が含まれる（github.com の有効なアカウント）
✓ カンバン        <ACCOUNT> の project #<PROJECT> を読めました（Status の選択肢は設定と一致。active_states の issue 0件／対象リポジトリ 0件）
! clone           active_states の issue が0件なので、検査する対象がありません
! 信頼登録        active_states の issue が0件なので、検査する対象がありません
✓ 資格情報        Keychain の項目 "Claude Code-credentials" から accessToken を読めます（rate_limit.token_source: keychain）

2件を確かめられませんでした（✗ 0件 / ! 2件）。足りないものはありません
```

**`Claude の設定` と `worktree の場所` は、その場所へ実際にディレクトリを作って消している。**
読めるだけで書けない場所（read-only で再マウントされたファイルシステムなど）は、
`os.Stat` で見ても分からない。**`Claude の設定` が `✗` なら、issue は1件も始まらない。**
Claude Code は SessionStart hook を走らせる前にそこへ書き、continuo はその hook を必ず張る。

**`!` は「確かめられなかった」であって、足りないという意味ではない。**
`clone` と `信頼登録` は、段7 で issue を `Ready` に置くと `✓` か `✗` に変わる。

**`資格情報` が `✓` になるのは、段5b を通してあるからである。**`doctor` はこの項目で実際に Keychain を読む。
**10秒の上限が掛かっているので、確認のダイアログが出たままでも `doctor` は固まらない**（`!` になって
「確認のダイアログが出ていないか確かめてください」と出る）。読めなかったときは `✗` になり、
直し方として `continuo allow-keychain-access` を案内する。

**別のディレクトリから叩くなら、`WORKFLOW.md` のパスを1つだけ渡せる。**

**実行する場所: どこでもよい**

```bash
/tmp/continuo doctor ~/continuo-try/WORKFLOW.md
```

### ここで詰まりやすいところ（`continuo doctor` が出すもの）

| 症状 | 直し方 |
| --- | --- |
| `Could not resolve to a Unions::ProjectV2FieldConfiguration with the name …` | `status_field` に書いた名前のフィールドがカンバンに無い。段2 で確かめた綴りに合わせる |
| `カンバンの Status の選択肢名が設定と一致しません` | 段4 の書き換えが足りない。**この状態では段8 の起動時検査が止めるので、無言で進むことはない** |
| `gh の scope に "project" がありません` | `gh auth refresh -h github.com -s project` を実行する |
| `front matter が不正です: unknown field "…"` | **設定のキーが増減したときに出る。**`continuo` を更新したら雛形も変わっている。出たキーの行を `WORKFLOW.md` から消す（**`continuo init --force` は使わない。**段4 の割り当てが消える） |
| `✗ clone  ghq が PATH にありません` | `ghq` か `git` が入っていない。**この2つは巡回が worktree を作るときに起動する**ので、無いと段8 で必ず落ちる。入れて PATH を通す |
| `read-only file system` / `input/output error` | **設定ではなくファイルシステムが壊れている。**下の「WSL でファイルシステムが壊れたとき」を見る |
| `✗ Claude の設定  … に書けません` | **ここが書けないと issue は1件も始まらない。**権限か、下の「WSL でファイルシステムが壊れたとき」を見る |

### WSL でファイルシステムが壊れたとき

**言いたいこと。**`EROFS: read-only file system` や `EIO` が出たら、**設定は壊れていない。**
**ファイルシステムが壊れている。**設定を作り直しても直らない。

**どう見えるか。**Claude Code が起動直後に止まる。

```text
EROFS: read-only file system, mkdir '/home/<ACCOUNT>/.claude/session-env/<session id>'
```

**なぜ continuo に関係するか。**continuo は issue ごとに SessionStart hook を張る。
**Claude Code は SessionStart hook を走らせる前に `~/.claude/session-env/<session id>/` を作る。**
だからホームが書けないと、**issue は1件も始まらない。**
`continuo doctor` は見出し語 `Claude の設定` でこの場所へ実際に書いてみるので、
**起動する前に気づける。**

**確かめる順番。**上から順に叩く。

| 何を | どう確かめるか |
| --- | --- |
| ルートが読み取り専用で再マウントされていないか | `mount \| grep ' / '` の出力に `ro` が無いか見る |
| カーネルが I/O エラーを出していないか | `dmesg \| grep -i ext4` |
| Windows 側のディスクに空きがあるか | エクスプローラで C ドライブの空き容量を見る（仮想ディスクを伸ばせないと書き込みが落ちる） |
| **再起動で直るか** | PowerShell で `wsl --shutdown` を実行し、**Windows を再起動してから開き直す** |

**この症状は再起動で直った実例がある。**先に設定やインストールを触らないこと。

### `doctor` を通っても段8 の起動で落ちるもの

**次の2つは `continuo doctor` が検査していない。**`doctor` が「前提はすべて揃っています」と
言っても、段8 で `continuo` を起動した瞬間にこの2つで落ちることがある。
**どちらも hook を受ける socket の置き場所の話で、置き場所は `CONTINUO_RUNTIME_DIR` で変えられる。**

| 起動時に出るエラー | 直し方 |
| --- | --- |
| `hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）` | **macOS の Unix domain socket は絶対パス103バイトまで。**`CONTINUO_RUNTIME_DIR=/tmp/continuo-run /tmp/continuo` のように短い場所を指定して起動する |
| `既にある hook を受ける socket のディレクトリ … の権限が 0755 です` | continuo は**自分が作っていないディレクトリの権限を書き換えない。**`chmod 700 <その場所>` してから起動する |

**`status_field` に実在しない名前を書いたときの出力**（実際に `continuo Status` と書いて叩いた。
hook の socket の場所だけ `$TMPDIR` に置き換えてある）。

```text
✓ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めました（front matter の検証も通りました）
✓ claude          ~/.local/bin/claude
✓ hook の置き場所 $TMPDIR/continuo/hooks.sock に socket を作れます
✓ Claude の設定   ~/.claude/session-env に書けます
✓ worktree の場所 ~/worktrees に書けます（workspace.root）
✓ herdr           protocol 20（設定と一致）／herdr 0.8.2／socket ~/.config/herdr/herdr.sock
✓ gh の認証       scope に project が含まれる（github.com の有効なアカウント）
✗ カンバン        カンバンを読めません: tracker エラー [tracker_response]: GraphQL がエラーを返しました: [NOT_FOUND] Could not resolve to a Unions::ProjectV2FieldConfiguration with the name continuo Status
                  → WORKFLOW.md の tracker.provider（owner / project_number / status_field）を確認してください
! clone           カンバンを読めなかったため、対象のリポジトリを特定できませんでした
! 信頼登録        カンバンを読めなかったため、対象のリポジトリを特定できませんでした
✓ 資格情報        Keychain の項目 "Claude Code-credentials" から accessToken を読めます（rate_limit.token_source: keychain）

3件に問題があります（✗ 1件 / ! 2件）
```

**設定が未記入のままだと、設定ファイルが `✗` になる。**
**それでも、既定値だけで確かめられるものは確かめる。**
`claude` と `hook の置き場所` は既定値で、`Claude の設定` は設定を読まずに走る。
**設定が読めないという理由で全部を `!` にすると、本当の原因を1つも指摘できない。**
実際にプレースホルダを残したまま叩いた出力（hook の socket の場所だけ `$TMPDIR` に置き換えてある）。

```text
✗ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めません: ~/continuo-try/WORKFLOW.md の front matter が不正です: 埋めていない設定が 2 件あります。値を埋めてください: tracker.provider.owner がプレースホルダ（__FILL_ME__）のままです / tracker.provider.project_number がプレースホルダ（0）のままです
                  → ファイルは読めています。front matter を直してください（`continuo init --force` は使わないでください。設定を雛形で潰します）
✓ claude          ~/.local/bin/claude
                  設定ファイルを読めなかったので、既定値で確かめました
✓ hook の置き場所 $TMPDIR/continuo/hooks.sock に socket を作れます
                  設定ファイルを読めなかったので、既定値で確かめました
✓ Claude の設定   ~/.claude/session-env に書けます
! worktree の場所 設定ファイルを読めないので、worktree の置き場所を決められません
! herdr           設定ファイルを読めなかったため、照合する herdr.protocol が決まりません
! gh の認証       設定ファイルを読めなかったため、gh の認証を検査しませんでした
                  → WORKFLOW.md を直してから `continuo doctor` をもう一度実行してください
! カンバン        設定ファイルを読めなかったため、どの project を見るか決まりません
! clone           カンバンを読めなかったため、対象のリポジトリを特定できませんでした
! 信頼登録        カンバンを読めなかったため、対象のリポジトリを特定できませんでした
! 資格情報        rate_limit の設定が読めないので、何を見るべきか決まりません
                  → 設定を直してからもう一度実行してください

8件に問題があります（✗ 1件 / ! 7件）
```

**`continuo init` を勧めるのは、設定ファイルが「無い」ときだけである。**
上の出力のようにファイルは読めていて中身が悪いだけなら、勧めるのは front matter の修正である。
**`continuo init --force` を使ってはならない。**段4 で書いた Status の割り当てが雛形で潰れる。

---

## 段7. issue を1件用意する

**ここから先はテスト用mock一式（[test/e2e](../test/e2e)）で通してある。**
本物に対して叩くと、段8 で実際に Claude Code が動き、枠を消費する。

**実行する場所: どこでもよい**

```bash
gh issue create --repo <REPO> \
  --title "README に使い方を1行足す" \
  --body "README.md の先頭に、このリポジトリが何かを1行で書いてください。"
```

返ってきた issue の URL を控える。**カンバンに載せる。**

**実行する場所: どこでもよい**

```bash
gh project item-add <PROJECT> --owner <ACCOUNT> --url <issue の URL>
```

**`Ready` にするのは画面での作業である。**カンバンを開き、その issue の `Status`
（段4 で `status_field` を変えたなら、そのフィールド）を `Ready` にする。

> **最初に試す issue は小さいものにする。**エージェントは実際にコードを書き、commit して push する。

**issue の載っているリポジトリが `~/.claude.json` に登録されているかを確かめる。**
**段5 で `continuo trust` を実行していなければ、ここが `✗` になる。**
`trust.repositories` に書き足してから `continuo trust` をもう一度叩く。

**ここでもう一度 `doctor` を叩くと、`clone` と `信頼登録` の判定が出る**（対象リポジトリが決まるため）。
段5 まで済んでいれば両方 `✓` になる。`✗ 信頼登録` が出たら、出てくる直し方のとおりに
`trust.repositories` へ書き足してから `continuo trust` を実行する。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo doctor
```

---

## 段8. 動かす

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo
```

**ここから枠を消費する。**起きることは次のとおり。

| 順 | 何が起きるか | どこで見えるか |
| --- | --- | --- |
| 1 | 設定を読み、`flock` を取り、前提を検査する | 標準エラーのログ |
| 2 | 置き場所を走査して、引き継ぐ run を探す（初回は0件） | 同上 |
| 3 | `Ready` の issue の処理を開始し、**Status を `In Progress` へ書く** | **カンバンで見える** |
| 4 | worktree を作り、herdr の pane で Claude Code を起動する | **herdr の画面で見える** |
| 5 | エージェントが作業し、push して **PR を作る** | **GitHub で見える** |
| 6 | エージェントが `CONTINUO-STATUS:` の行を出す | herdr の pane |
| 7 | continuo がその行を読み、**Status を `In Review` へ動かす** | **カンバンで見える** |
| 8 | 人間が PR を確認して **`Done` へ動かす** | 人間の操作 |
| 9 | continuo が worktree と branch を片付ける | 置き場所から消える |

**巡回の間隔は既定30秒である。**すぐには動かない。

**段5 で Status が勝手に動いたら、カンバンの自動化である。**
カンバンの `Settings` → `Workflows` に `Pull request linked to issue` という自動化があり、
**有効にしていると、PR が issue に紐づいた瞬間に Status を書き換える。**
continuo は知らない Status になった issue を、
`tracker.unknown_state_grace_ms`（既定10分）のあと止める。
**直し方は [FAQ.md](FAQ.md) の
「エージェントが PR を作った直後に止まる（automated_state_rewrite）」にある。**

**PR を作らせないようにはできない。**組み込みの指示書が本文に委ねるのは
**draft にするかどうか・base にする branch・成果がこの worktree の外にあるときの出し方**の3つだけである。
**「作らない」と書いても作られる。**

**起動に成功したときのログ**（`Ready` が0件の状態で実際に叩いたもの。
**この状態では Claude Code は起動しないので、枠を消費しない**）。

```text
continuo を起動します（設定ファイル: ~/continuo-try/WORKFLOW.md）
level=INFO msg=設定ファイルを読み込みました path=~/continuo-try/WORKFLOW.md
level=INFO msg="hook を受ける socket の場所を決めました" socket=/tmp/cnt-run/hooks.sock
level=INFO msg=二重起動防止のロックを獲得しました lock_file=~/.continuo/continuo.lock
level=INFO msg=リポジトリの信頼はこのファイルで判定します claude_config=~/.claude.json
level=INFO msg="gh の認証と scope を確かめました" scope=project
level=INFO msg="herdr の socket に到達しました" protocol=19
level=INFO msg="tracker アダプタの起動時検査が完了しました" owner=<ACCOUNT> project_number=<PROJECT> status_options=6
level=INFO msg="hook を受ける socket の listen を始めました" socket=/tmp/cnt-run/hooks.sock
level=INFO msg="hook の配送を始めました"
level=INFO msg=復元を終えました worktrees=0 adopted=0 closed_panes=0 cleaned=0
level=INFO msg="ダッシュボードは開きません（server.port が未設定）"
level=INFO msg=巡回を始めます poll_interval_ms=30000
level=WARN msg="枠の判定を諦めます（rate_limit.source: none と同じ動きになります。起動は止めません）" error="枠の判定に使う資格情報を取得できません: ~/.claude/.credentials.json を読めません（macOS では Keychain にあるのが普通です）: …"
```

> **最後の `WARN` は、枠の判定に使う資格情報を取れなかったときに1回だけ出る。**
> 枠の判定を諦めて `rate_limit.source: none` と同じ動きになるだけで、**起動は止まらない。**
>
> **この出力を叩いたときの設定は `rate_limit.token_source: claude_credentials` で、
> `~/.claude/.credentials.json` が無かった。**だからこの行が出ている。
> **macOS の既定は `keychain` である**（段3 の `continuo init` がそう書く）。
> `keychain` にして段5b を通すと、段6 の `資格情報` は `✓` になる。
> 巡回のループも `doctor` と同じ `security find-generic-password -s "Claude Code-credentials" -w` を
> 起動して読むので、**読める先が2つに割れることはない。**

**`Ready` の issue が0件なら、ここで止まったまま何も起きない。**
**Claude Code が起動するのは、`Ready` の issue を見つけたときだけである。**

### 別の端末から様子を見る

**実行する場所: どこでもよい**

```bash
find ~/worktrees -name .continuo.json           # worktree ができたか（workspace.root）
gh project item-list <PROJECT> --owner <ACCOUNT> --format json | jq -r '.items[] | "\(.status)\t\(.title)"'
herdr workspace list                            # worktree が herdr の workspace として開かれたか
```

> **2つ目の `jq` は `.status` を決め打ちで見ている。**これは `gh project item-list` が
> **組み込みの `Status` フィールドだけ**を `status` というキーで返すためである。
> 段2 の「専用のフィールド」を使っている場合、この行は空を返す。
> そのときは `--format json` の生の出力を見て、フィールド名に合わせて `jq` を書き換えること。

**`ls ~/worktrees` に出るのは `github.com` の1行だけである。**worktree の並べ方は gwq の規則に固定してあり
（設定では変えられない）、`~/worktrees/<ホスト>/<owner>/<repo>/<branch の / を - にしたもの>` に掘られる。
**どの issue のものかは、この階層を辿っても分からない。**
その中の `.continuo.json` に、どの issue の worktree かが書いてある。

```json
{
  "issue_url": "https://github.com/<REPO>/issues/12",
  "issue_identifier": "<REPO>#12",
  "project_item_id": "PVTI_...",
  "branch": "continuo/<REPO>/12",
  "base": "main",
  "herdr_workspace_id": "w1",
  "socket_path": "/tmp/continuo-run/hooks.sock",
  "settings_path": "/tmp/continuo-run/issues/<owner>-<repo>-12/settings.json",
  "agent_name": "continuo-<repo>-12",
  "session_uuid": "550a5f08-a837-4e00-aed2-7ed2f9ca9ecf",
  "created_at": "2026-08-20T17:01:43.775528+09:00",
  "takeover_count": 0
}
```

### ダッシュボードを見る

`WORKFLOW.md` の `server.port` に番号を書いておくと、**実行中の run の一覧を HTTP で見られる。**
**書き換えたら continuo を再起動する**（設定の読み直しは作らないと決めてある。最後の節を見よ）。

```yaml
server:
  port: 8787
```

**実行する場所: どこでもよい**

```bash
open http://127.0.0.1:8787/          # issue / Status / turn 数 / 最後に hook を受けた時刻 / トークン
curl -s http://127.0.0.1:8787/api/v1/state | jq .
```

**127.0.0.1 からしか繋がらない**（読むだけの窓であり、外へ晒すものではない）。

**トークンの表は2つあります。**上は**いま走っている run のぶん**で、
run が終わると画面から消えます。下の「run をまたぐ累計」（JSON では `cumulative_totals`）は、
**終わった run のぶんも残ります。**

**累計の意味は「この continuo が起動してから、turn の終わりに読み取った transcript の合計」です。**
**継続の指示を送って引き継いだ run では、continuo を起動する前に書かれたぶんも入ります。**
**走行中の turn のぶんは、まだ入っていません**（集計は turn の終わりに1回だけ走ります）。
**continuo を再起動すると 0 に戻ります**（メモリだけに持っているためです）。

---

## 段8b. 着手を取り消す（間違えて `Ready` に置いたとき）

**実行する場所: `~/continuo-try`**（`WORKFLOW.md` があるディレクトリ）

**`Ready` に置く issue を間違えたら、`continuo abandon` で着手する前の状態へ戻す。**
worktree・pane・herdr の workspace・branch がまとめて消える。

**先に `--dry-run` で下見する。**この段では何も消さない。

```bash
cd ~/continuo-try
/tmp/continuo abandon --dry-run <issue の URL>
```

**出るもの。**issue と Status、worktree のパス、branch と base、herdr の workspace と pane、
コミットされていない変更のファイル数、push されていない commit の件数。

```text
continuo は動いていません。
消すもの:
  issue          : <REPO>#<番号>（<issue の URL>）
  Status         : In Progress
  worktree       : <worktree の絶対パス>
  branch         : <branch 名>（base: main）
  herdr workspace: <herdr workspace の ID>
  herdr pane     : <pane の ID>
  コミットされていない変更: 0 ファイル
  push されていない commit: 0 件
--dry-run なので何も消していません。
```

**消すときは `--dry-run` を外す。**

```bash
cd ~/continuo-try
/tmp/continuo abandon <issue の URL>
```

| 何が起きるか | 補足 |
| --- | --- |
| **continuo が動いていれば、先に手を離させる** | **まだ作業中の Status なら**、`tracker.failure_state`（既定 `Blocked`）へ一時的に動かし、**その worktree の pane が閉じるのを待つ。**`--park <Status 名>` で動かす先を変えられる |
| **pane が閉じなければ、何も消さずに止まる** | 上限は `herdr.read_timeout_ms` の10倍（既定50秒）。**終了コードは 1** |
| **失うものがあれば、何も消さずに止まる** | コミットされていない変更・push されていない commit のこと。**それでも消すなら `--force`。**終了コードは 1 |
| **片付けたあとの Status は動かさない** | 「Status は動かしていません。カンバンで決めてください。」と出る。**動かす先が決まっているなら `--to "<Status 名>"`** |
| **その issue の worktree が無ければ、何もせずに終わる** | 「この issue の worktree はありません」と出る。**終了コードは 0** |

> **カンバンの操作だけでは取り消せない。**`Ready` へ戻しても continuo は止まらない（`Ready` は
> 作業中の Status の1つであり、着手待ちの Status でもあるので**もう一度着手されうる**）。
> `Done` へ動かすと、片付けの前に「この作業のコメントが issue にあるか」を確かめ、
> **無ければセッションを復元して Claude Code を起動し直す。**
> 詳しくは [plans/continuo_design.md](plans/continuo_design.md) の 3-37 を見ること。

---

## 段9. 止める・片付ける

**continuo を動かしている端末で** `Ctrl+C` を押す。

**巡回を止め、ダッシュボードと hook の受け口を閉じ、走行中の turn の終わりを待ってから抜ける。**
**pane は閉じない。**次に起動したとき、その pane を引き継ぐ。

**待たされるのが嫌なら、もう一度 `Ctrl+C` を押す。**後始末を待たずに、その場で終わる
（終了コードは 130）。**それでも終わらないときは `kill -QUIT <pid>` を実行する。**
全 goroutine のスタックが出るので、その出力を issue へ貼ってほしい。

実際に叩いた出力（終了コードは 0）。

```text
level=WARN msg="割り込みを受けました。走行中の turn ループを壊さないよう、順に閉じてから終わります" signal=interrupt max_wait=36s pid=88890
level=WARN msg="待ちたくない場合は、もう一度 Ctrl+C を押してください（同じ signal をもう一度送っても同じです）。後始末を待たずに即座に終了します" exit_code=130
level=WARN msg="それでも終わらない場合は、次のコマンドで全 goroutine のスタックを出して、その出力を issue へ貼ってください" command="kill -QUIT 88890"
level=INFO msg="巡回を止めました。後始末を始めます（ダッシュボード → hook の受け口 → turn ループの順に閉じます）" max_wait=36s pid=88890
level=INFO msg="後始末 1/3: ダッシュボードを閉じています（処理中の応答をこの時間だけ待ち、過ぎたら接続を切ります）" timeout=1s
level=INFO msg="後始末 2/3: hook の受け口を閉じています（受け取り済みの hook を印へ書き終えるまで待ちます）" timeout=5s
level=INFO msg="後始末 3/3: 走行中の turn ループの終了を待っています（送った指示が中途半端に切れないようにするためです）" timeout=30s
level=INFO msg="走行中の turn ループが終わりました（pane は閉じていません）"
level=INFO msg=continuo を終了しました
```

### 試したあとの片付け

| 何を | どうするか |
| --- | --- |
| worktree と branch | **Status を `Done` にすれば continuo が片付ける。**着手そのものを取り消したいなら段8b の `continuo abandon` を使う。それでも残っていれば `~/worktrees` の下を見て `git worktree remove` と `git branch -D` |
| push した branch | **GitHub には残る。**continuo が消すのは手元の branch だけである（`cleanup.require_pushed` で push を確かめてから消しているので、成果は GitHub 側に残る）。要らなければ GitHub の画面か `git push origin --delete <branch>` で消す |
| カンバンの item | **カンバンは消さない。**試した issue だけを画面から外すか、`Done` に置いたままにする |
| 信頼の登録 | `~/.claude.json.continuo-backup-<日時>` から戻すか、`projects` の該当キーを消す。**バックアップを消すのは人間である** |
| 作業ディレクトリ | `~/continuo-try` を消す |
| 実行ファイル | `/tmp/continuo` を消す |
| 実行時ディレクトリ | **上から順に最初に見つかった場所。**`CONTINUO_RUNTIME_DIR` → `$XDG_RUNTIME_DIR/continuo` → macOS なら `$TMPDIR/continuo` → `~/.continuo/run`。**`XDG_RUNTIME_DIR` は macOS でも先に見る。**中に socket が残っていれば消す |

---

## うまく動かないとき

| 症状 | 見るところ |
| --- | --- |
| **issue の処理が始まらない** | Status が `Ready` か。`doctor` の `カンバン` と `信頼登録` が `✓` か。**信頼の門番は `~/.claude.json` である**（`trust.repositories` に書くだけでは足りない。`continuo trust` の実行が要る。段5） |
| **別のフィールドを書き換えている** | `status_field` が段2 で確かめた名前になっているか。continuo は `status_field` に書いた名前のフィールドしか読み書きしない |
| **信頼していないリポジトリの issue を飛ばしている** | `doctor` の `信頼登録` が `✓` か。**未信頼だと worktree も pane も作られない。**そのリポジトリにつき1回、**issue にコメントが投稿される**（直し方もそこに書いてある） |
| **`In Review` にならない** | エージェントが `CONTINUO-STATUS: review` を出しているか。herdr の pane で応答を見る |
| **issue が急に `Blocked` になった** | **issue のコメントを開く。**そこに何が起きたか・どう確かめるか・どう直すかが書いてある。**画面が変わらないまま `claude.turn_timeout_ms` が過ぎると打ち切る**（既定1時間）。**これは turn の総実行時間の上限ではない。**画面が変わり続けている限り、1つの指示に何時間かかっても打ち切らない |
| **着手する issue を間違えた** | **段8b の `continuo abandon` で着手する前へ戻す。**カンバンで `Ready` へ戻しても止まらず、`Done` へ動かすと Claude Code が起動し直される |
| **片付かない** | **未コミットの変更が残っている**か、**push していない commit がある**と消さない（成果を失わないため）。ログに理由が出る |
| 枠を使い切った | continuo は待って再開する。Claude Code 2.1.234 以降は Claude Code 自身も継続するので、continuo は `agent_status` を見て二重投入を避ける |
| **同じ issue に Claude Code が2つ立った** | 起きてはならない。**再現手順を添えて issue を立ててほしい** |

---

## 作らないと決めたもの

**設定の読み直しは作らない**（`SPEC.md` 6.2 は REQUIRED としているが、実装しないと決めた）。
`WORKFLOW.md` を書き換えたら、**continuo を再起動すること。**

**理由。**再起動は安全に作ってある（`restart.orphan_running_action` が実行中の run を
引き継ぐか着手待ちへ戻す。**worktree も pane も残る**）。
無人で回している最中に設定を書き換える運用を想定しておらず、
実行中の run へ反映すると turn の途中で判断の基準が食い違う。

詳しくは [plans/impl/tasks.md](plans/impl/tasks.md) の「仕様のうち、作らないと決めたもの」を見ること。
