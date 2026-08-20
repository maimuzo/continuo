<!-- 目的: 人間が continuo を実際に動かして確かめるための手順 -->

# 試してみる / Trying it out

**English:** This walks `continuo` through one issue end to end **on a board you already have** —
no new board is created. Build it, check that the existing `Status` field already carries the five
options `continuo` needs, generate `WORKFLOW.md` with `continuo init` (owner, project number and the
list of repositories are filled in automatically from `gh`), register folder trust for the
repositories you kept with `continuo trust`, check the prerequisites with `continuo doctor`, put one
issue into `Ready`, and watch `continuo` open a worktree and drive Claude Code until the board says
`Done`. Every command block states which directory to run it in and works on its own. The table
below says which steps were actually executed while writing this document and which were not.

**言いたいこと。**1件の issue が `Ready` から `Done` まで通るのを、実際に目で見るための手順である。
**ボードは新しく作らない。いま使っているボードにそのまま足して使う。**
**すべてのコマンドは「どこで実行するか」を書いてあり、そのブロックだけで動く。**

## この文書のどこを実際に叩いたか

**叩いていない段・叩けない段は、そう明記してある。**

| 段 | 叩いたか | 補足 |
| --- | --- | --- |
| 段1 ビルドする | **叩いた** | `go build` と、5つのサブコマンドの `--help` |
| 段2 使うボードを確かめる | **叩いた（読むだけ）** | `gh project list` と `gh project field-list`。**本番のボードには読み取りしか行っていない** |
| 段3 設定を置く | **叩いた** | 自動で埋まるとき・`--owner` / `--project` を渡すとき・`gh` が無いとき・既にあるときの4通り |
| 段4 Status の割り当てを合わせる | **叩いた** | `continuo setup` を本番のボードに対して実行した（読み取りのみ） |
| 段5 clone して信頼を登録する | **叩いた** | `continuo trust --dry-run` は実物の `~/.claude.json` に対して（読むだけ）。書き込みは偽のホームディレクトリで確かめた |
| 段6 前提を検査する | **叩いた** | 揃っているとき・フィールド名が違うとき・設定が未記入のときの3通り |
| 段7 issue を1件用意する | **偽物一式で叩いた** | 偽の `gh` と偽の GitHub GraphQL に対して。**本番のボードには1リクエストも送っていない** |
| 段8 動かす | **両方叩いた** | **本物で起動し、巡回が始まるところまで確かめた**（`Ready` が0件なので Claude Code は起動していない）。1件を `Ready` から `Done` まで通したのは偽物一式のほう |
| 段9 止める・片付ける | **両方叩いた** | 本物にも偽物にも `Ctrl+C` 相当の `SIGINT` を送り、終了コード 0 で終わることを確かめた |

**本物に対して確かめていないのは「実際に issue を1件通すこと」だけである**（段7 の issue 作成と、
段8 で Claude Code が動く部分）。**そこから枠を消費するので、人間が判断すること。**

**段7〜段9 の「偽物一式」は [test/e2e](../test/e2e) にある。**偽の `gh` / 偽の GitHub GraphQL /
偽の herdr / 偽の Claude Code / 隔離したホームディレクトリの5つと、本物の git を組み合わせてある。
**実物の Claude Code は1回も起動していないので、枠は消費していない。**
実際に人間が本物に対して叩くと、段8 から枠を消費する。

> **出力例は実際に叩いた結果である。**ただし個人のパス・アカウント名・リポジトリ名・ボードの名前だけを
> `~` と `<ACCOUNT>` / `<PROJECT>` / `<REPO-1>` / `<ボードの名前>` に置き換えてある。

---

## 先に決めること

**この手順では、次の3つを自分の値に置き換える。**先に決めて手元に控えておく。

| 記号 | 何を入れるか | 例 |
| --- | --- | --- |
| `<ACCOUNT>` | あなたの GitHub アカウント名 | `octocat` |
| `<PROJECT>` | **いま使っているボードの番号**（段2 で確かめる） | `3` |
| `<REPO>` | **試す対象のリポジトリ**（issue を置く場所） | `octocat/sandbox` |

> **`<REPO>` は使い捨てにできるものを選ぶ。**エージェントがそのリポジトリを実際に編集し、
> commit して push する。**本業のリポジトリを指定しない。**

## 先に知っておくこと

| 何を | なぜ |
| --- | --- |
| **ボードは作らない** | continuo は**既にあるボードに後から足して使う。**足りない選択肢があるときだけ画面で足す（段2） |
| **段8 から枠を消費する** | 実際に Claude Code が起動し、issue を実装しようとする |
| **止めるのは `Ctrl+C`** | 巡回を止め、hook の受け口を閉じ、turn の終わりを待ってから抜ける。**pane は閉じない**（次の起動で引き継ぐ） |

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
    	tracker.provider.project_number に書くボードの番号（省略すると gh から引く）
```

`setup --help` の出力。**`--force` は無い**（段4 で説明する）。

```text
Usage of continuo setup:
  -owner string
    	Status の選択肢を読むボードの GitHub の user / organization 名（省略すると gh から引く）
  -project int
    	Status の選択肢を読むボードの番号（省略すると gh から引く）
  -status-field string
    	Status を読み書きする single-select フィールドの名前（既定 Status） (default "Status")
```

`trust --help` の出力。

```text
Usage of continuo trust:
  -dry-run
    	何が要求されているかを表示するだけで、~/.claude.json を書き換えない
```

**サブコマンドは5つある。**`init` / `setup` / `trust` / `doctor` / `hook` で、
引数に何も渡さなければ常駐する。

> **実装を更新したら、必ずここへ戻ってビルドし直すこと。**
> `/tmp/continuo` は前に建てたものが残る。**新しいサブコマンドを叩くと
> 「設定ファイルが見つからない」という分かりにくいエラーになる**
> （知らない引数は設定ファイルのパスとして扱われるため）。

---

## 段2. 使うボードを確かめる（作らない）

**言いたいこと。**continuo は**既にあるボードに後から足して使うものである。**
**要るのは `Status` フィールドに5つの選択肢が揃っていることだけで、
揃っていれば `WORKFLOW.md` は既定のままでよい。**

**実行する場所: どこでもよい**（GitHub に対する操作である）

```bash
gh project list --owner <ACCOUNT>
```

実際に叩いた出力。左端の数字が `<PROJECT>` である。

```text
3	<ボードの名前>	open	PVT_...
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

**`Status` に必要な5つが全部ある。**このボードは**何も足さずにそのまま使える。**

| 選択肢 | 何に使うか | `WORKFLOW.md` のどのキーか |
| --- | --- | --- |
| `Ready` | **ここに置いた issue を continuo が取る** | `dispatch_state` |
| `In Progress` | 取ったときに continuo が書き込む | `running_state` |
| `In Review` | エージェントが `CONTINUO-STATUS: review` を出すと入る | `status_signal_map.review` |
| `Blocked` | 判断を仰ぐとき、または打ち切ったときに入る | `failure_state` |
| `Done` | **人間がここへ動かすと、continuo が worktree と branch を片付ける** | `terminal_states` |

`Ice Box` のように**continuo が知らない選択肢があっても構わない。**
`active_states`（既定は `Ready` と `In Progress`）に書いていない選択肢は、ただ無視される。

### 足りない選択肢があったとき

| 道 | 何をするか | 画面の作業 |
| --- | --- | --- |
| **選択肢を足す** | ボードを開く → 右上の `⋯` → `Settings` → 左の `Status` → `+ Add option` | 足す数だけ |
| **設定を縮める** | `Status` に実在する名前だけで回るように `WORKFLOW.md` を書き換える（段4） | 無し |

> **選択肢を API で足してはならない。**足す API は `updateProjectV2Field` しか無く、
> **選択肢の指定を全件置き換えとして扱う。**GitHub が全部の選択肢に ID を採番し直すので、
> **設定済みの Status の値が全部消える。**必ず GitHub の画面から追加する。

### 専用のフィールドを使ってもよい

**`status_field` に書いた名前のフィールドが、絞り込み・読み取り・書き込みのすべてで使われる。**
`continuo Status` のように**空白を含む名前でもよい**（設計 3-34。絞り込みのキーは引用符で囲んで組み立てている）。
組み込みの `Status` を別の目的で使っているボードでは、この道を採る。

**確かめていないこと。**この文書を書くにあたって、専用フィールドを実際には作っていない。
**本番のボードへ書き込まないためである。**空白を含むフィールド名が絞り込みのキーとして解決されることは、
設計 3-34 に読み取り専用クエリでの実測が載っている。

**綴りが1文字でも違うとボードを読めない。**実際に、存在しない `continuo Status` を指定して
`continuo doctor` を叩いた出力（段6 に全体を載せてある）。

```text
✗ ボード          ボードを読めません: tracker エラー [tracker_response]: GraphQL がエラーを返しました: [NOT_FOUND] Could not resolve to a Unions::ProjectV2FieldConfiguration with the name continuo Status
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

`~/continuo-try/WORKFLOW.md` ができる。**`owner` と `project_number` と `trust.repositories` は
`gh` から引いて自動で入る。**入った値はその場に出る。実際に叩いた出力。

```text
WORKFLOW.md を作成しました: ~/continuo-try/WORKFLOW.md
✓ tracker.provider.owner: <ACCOUNT>（`gh api user` が返した GitHub のログイン名です）
✓ tracker.provider.project_number: <PROJECT>（`gh project list` の候補が1件だけでした: #<PROJECT> <ボードの名前>）
✓ trust.repositories: <ACCOUNT>/<REPO-1>, <ACCOUNT>/<REPO-2>, <ACCOUNT>/<REPO-3>, <ACCOUNT>/<REPO-4>, <ACCOUNT>/<REPO-5>（ボード #<PROJECT> に載っている 5 個のリポジトリを並べました）
  → **要らない行は WORKFLOW.md から消してください。**残ったものだけが `continuo trust` の対象になります
  → 何を許すことになるかは `continuo trust --dry-run` で確かめられます
```

**`trust.repositories` は「並べただけ」である。**continuo はここから勝手に信頼を登録しない。
**要らない行を消すのは人間の仕事で、消し終えてから段5 を叩く。**

### 自動で入らないとき

**ボードが2枚以上あると `project_number` は自動で決まらない。**候補が番号・名前・URL で並ぶので、
段2 で確かめた番号を指定して置き直す。

> **organization のボードを使うなら `--owner <組織名>` を必ず渡す。**
> `continuo init` は `gh api user` に聞くので、**渡さないと個人のログイン名が入る。**
> しかもその値は `✓` として報告されるので、**間違っていることが分からない。**
> 段4 の `continuo setup` も同じ経路でボードを決めるので、そちらにも渡す。

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
✓ trust.repositories: …（ボード #<PROJECT> に載っている 5 個のリポジトリを並べました）
```

**`gh` が無い・認証が無いときも `WORKFLOW.md` は作られ、終了コードは 0 である。**
その場合だけプレースホルダ（`__FILL_ME__` と `0`）が残るので、案内どおりに手で埋める。
実際に `gh` を PATH から外して叩いた出力。
**`maimuzo` は文言に埋め込んである固定の例であり、あなたのアカウント名ではない。**

```text
WORKFLOW.md を作成しました: ~/continuo-try/WORKFLOW.md
! tracker.provider.owner: 埋められませんでした（gh コマンドが見つかりませんでした）
  → gh を入れて `gh auth login -s project` でログインしてください
  → または `continuo init --owner <名前>` でもう一度実行してください
  → https://github.com/maimuzo なら maimuzo の位置が owner です
! tracker.provider.project_number: 埋められませんでした（owner が決まらないので、ボードの候補を引けませんでした）
  → 先に owner を決めてから、もう一度 `continuo init` を実行してください
  → または `continuo init --project <番号>` でボードの番号を直接指定してください
! trust.repositories: 埋められませんでした（owner とボードの番号が決まらないので、ボードに載っているリポジトリを引けませんでした）
  → owner とボードの番号を決めてから、もう一度 `continuo init` を実行してください
  → `continuo trust` の対象は WORKFLOW.md の trust.repositories に手で書いても構いません
埋まらなかった値は WORKFLOW.md の中でプレースホルダのままです。上の案内どおりに書いてください
```

**既に `WORKFLOW.md` があると `init` は上書きせずに止まる**（終了コード 1）。

```text
~/continuo-try/WORKFLOW.md は既にあります。上書きするなら --force を付けてください
```

**`workspace.root` も見ておく。**worktree を置く場所である（既定は `~/worktrees`）。

---

## 段4. Status の割り当てを合わせる

**言いたいこと。**`continuo setup` が、ボードの選択肢を continuo の5つの役割へ割り当てる。
**役割の説明が出るので、それを読んで番号で選ぶ。**
**書き換わるのは `Status` に関する7行だけで、段3 で手を入れた行はそのまま残る。**

**段3 で作った `WORKFLOW.md` に対して実行する。**`continuo setup` は雛形を作らないので、
`WORKFLOW.md` が無いときは段3 をやり直すよう案内して止まる（終了コード 1）。

> **`continuo setup` は、どのボードを読むかを `WORKFLOW.md` から決めない。**
> `gh` に聞き直す。だから次の3つの場合はフラグで指定する。
>
> ```bash
> cd ~/continuo-try
> /tmp/continuo setup --owner <ACCOUNT> --project <PROJECT> --status-field Status
> ```
>
> | いつフラグが要るか | 指定しないとどうなるか |
> | --- | --- |
> | **ボードが2枚以上ある** | ボードを決められず、終了コード 1 で止まる（段3 と同じ） |
> | **organization のボードを使う** | `gh api user` が返す個人のログイン名で探すので、**別のボードの選択肢を読む** |
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

**実際の出力**（このボードの選択肢は段2 で確かめた6つ）。

```text
ボードの Status フィールドには次の選択肢があります。
  1  Ice Box
  2  Ready
  3  In Progress
  4  Blocked
  5  In Review
  6  Done

これから 5 個の役割について、それぞれどの選択肢を使うかを尋ねます。番号で答えてください。
その役割に使える選択肢がボードに無い場合は 0 を入力してください。
Ctrl+C で中断できます。中断したときは WORKFLOW.md を書き換えません。

[1/5] dispatch_state: continuo が自動的に処理を開始する State は何番ですか?
番号> 2
  → dispatch_state に "Ready" を割り当てました

[2/5] running_state: continuo が処理を開始したときに移動する State は何番ですか?
番号> 3
  → running_state に "In Progress" を割り当てました

[3/5] status_signal_map.review: エージェントが作業を完了したときに移動する State は何番ですか?
番号> 5
  → status_signal_map.review に "In Review" を割り当てました

[4/5] status_signal_map.blocked / failure_state: エージェントが判断を仰ぐとき・打ち切ったときに移動する State は何番ですか?
番号> 4
  → status_signal_map.blocked / failure_state に "Blocked" を割り当てました

[5/5] terminal_states: 人間がここへissueを移動したら作業完了とみなしgit worktreeを削除する State は何番ですか?
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
| 選択肢が5個未満のボード | **尋ねる前に止める。**足す手順を出す |
| `Ctrl+C` | 中断する。`WORKFLOW.md` は書き換えない |

### 手で書き換えることもできる

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
${EDITOR:-vi} WORKFLOW.md
```

**書き換えるのは front matter の次のキーだけである。**すべて**ボードに実在する選択肢名**を書く。

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

**選択肢を足せない（足したくない）ボードでの縮め方の例。**
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
level=ERROR msg="continuo を起動できません" error="起動できませんでした: 起動時の検査に落ちました（生きている pane は閉じずに残します）: ボードの Status の選択肢名が設定と一致しません（対象0件が無言で続くのを防ぎます）: … : Readyyy"
```

**無言で回り続けることはない。**ただし段6 の `doctor` なら起動する前に気づける。

---

## 段5. 対象リポジトリを clone して信頼を登録する

**言いたいこと。**continuo は `ghq` で clone の場所を引く。**手元に無いリポジトリは動かせない。**
**信頼の登録は `continuo trust` が行う。**手で `claude` を起動して承認する必要は無い。

### clone を用意する

**実行する場所: どこでもよい**（`ghq` が置き場所を決める）

```bash
ghq get <REPO>                   # continuo は ghq で clone の場所を引く
ghq list -p -e <REPO>            # clone の絶対パスが出る。0行なら失敗している
```

### `<REPO>` を信頼の対象に書き足す

**言いたいこと。**段3 の `continuo init` が `trust.repositories` に並べたのは、
**そのときボードに載っていたリポジトリだけ**である。
**`<REPO>` は段7 で初めてボードに載るので、ここには入っていない。**手で書き足す。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
grep -n -A 9 "^  repositories:" WORKFLOW.md    # いま並んでいるものを見る
```

`<REPO>` の行が無ければ、`${EDITOR:-vi} WORKFLOW.md` で足す。

```yaml
trust:
  repositories:                             # continuo trust が信頼を登録してよいリポジトリ。ボードから拾って並べた。
                                            # **要らない行は消すこと。**ここに残ったものだけが登録の対象になる
                                            # **これから issue を作るリポジトリは、まだボードに無いので入っていない。**手で足すこと
    - "<REPO>"                                # ← 段7 で issue を置くリポジトリ。手で足す
```

#### 何が「信頼済み」を決めるのか

**巡回は `trust.repositories` を読まない。**読むのは `continuo trust` だけである。

| 誰が | 何を見るか |
| --- | --- |
| **巡回のループ** | **`~/.claude.json` の `projects["<clone の絶対パス>"].hasTrustDialogAccepted`。**これが唯一の門番 |
| `continuo trust` | `WORKFLOW.md` の `trust.repositories`。**そこに書かれたものだけ**を `~/.claude.json` へ登録する |
| `continuo doctor` の `信頼登録` | **ボードに載っている issue のリポジトリ**について `~/.claude.json` を見る |

**だから2つのことが起きうる。**

- **`trust.repositories` に書かなくても取ることがある。**その clone で以前 Claude Code を
  起動していれば、`~/.claude.json` には既に登録されている
- **書いてあっても `continuo trust` を実行していなければ取らない。**書いただけでは
  `~/.claude.json` は変わらない

#### 信頼が無いまま段8 へ進むとどうなるか

**continuo はその issue を取らない。worktree も pane も作らない。**
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

**未信頼のリポジトリがあるときの出力。**偽のホームディレクトリと偽の `ghq` の置き場所を作って
叩いたものである（実物の `~/.claude.json` を書き換えないため）。読みやすさのためにパスを `~` へ置き換えてある。

```text
信頼を登録すると、次の設定が Claude Code に効くようになります。
**そのリポジトリで動くエージェントが、ここに書かれた操作を確認なしで実行できます。**
書き込む先: ~/.claude.json

! demo/sample-a（未信頼。登録の対象）
    信頼の鍵: ~/ghq/github.com/demo/sample-a
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
    信頼の鍵: ~/ghq/github.com/demo/sample-b
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
    信頼の鍵: ~/Sources/github/<REPO-1>
    .claude/settings.json:
      permissions.allow: なし
      permissions.additionalDirectories: なし
    .mcp.json: ありません

すべて信頼済みです。書き込むものはありません。

--dry-run なので何も書き換えていません。登録するなら --dry-run を外して実行してください。
```

> **`信頼の鍵` は `ghq list -p -e` が返したパスそのものとは限らない。**
> `git rev-parse --show-toplevel` の出力を鍵にするので、`ghq` の置き場所が symlink なら
> 実体側のパスが出る。**登録されるのはこの鍵である。**

### 登録する

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo trust
```

**書き込む前に、`--dry-run` と同じ要求内容をもう一度出す。**そのうえで書き込む。
実際に叩いた出力の末尾（偽のホームディレクトリに対して）。

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

> **`trust.repositories` に書いたものだけが対象である。**ボードから自動で集めない。
> **issue を足せる人はボードに載るリポジトリを変えられる**ので、そこから自動で登録すると
> 信頼させる先を他人が増やせてしまう（設計 3-33）。

---

## 段6. 前提が揃っているかを検査する

**実行する場所: `~/continuo-try`**（`WORKFLOW.md` を置いた場所）

```bash
cd ~/continuo-try
/tmp/continuo doctor
```

7項目を検査して、足りないものと直し方を出す。**`✗` が1つでもあれば終了コードは 1。**
**既存のボードを既定の設定のまま使って**実際に叩いた出力。

```text
✓ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めました（front matter の検証も通りました）
✓ herdr           protocol 19（設定と一致）／herdr 0.8.0／socket ~/.config/herdr/herdr.sock
✓ gh の認証       scope に project が含まれる（github.com の有効なアカウント）
✓ ボード          <ACCOUNT> の project #<PROJECT> を読めました（Status の選択肢は設定と一致。active_states の issue 0件／対象リポジトリ 0件）
! clone           active_states の issue が0件なので、検査する対象がありません
! 信頼登録        active_states の issue が0件なので、検査する対象がありません
! 資格情報        ~/.claude/.credentials.json がありません（macOS では Keychain に入っています）
                  → 判定を飛ばしました。continuo の起動には影響しません（Keychain は読みません）

3件を確かめられませんでした（✗ 0件 / ! 3件）。足りないものはありません
```

**`!` は「確かめられなかった」であって、足りないという意味ではない。**
`資格情報` は macOS では常に `!` になる（**Keychain を読むと確認の画面が出て無人運用が止まる**ので読まない）。
`clone` と `信頼登録` は、段7 で issue を `Ready` に置くと `✓` か `✗` に変わる。

**別のディレクトリから叩くなら、`WORKFLOW.md` のパスを1つだけ渡せる。**

**実行する場所: どこでもよい**

```bash
/tmp/continuo doctor ~/continuo-try/WORKFLOW.md
```

### ここで詰まりやすいところ（`continuo doctor` が出すもの）

| 症状 | 直し方 |
| --- | --- |
| `Could not resolve to a Unions::ProjectV2FieldConfiguration with the name …` | `status_field` に書いた名前のフィールドがボードに無い。段2 で確かめた綴りに合わせる |
| `ボードの Status の選択肢名が設定と一致しません` | 段4 の書き換えが足りない。**この状態では段8 の起動時検査が止めるので、無言で進むことはない** |
| `gh の scope に "project" がありません` | `gh auth refresh -h github.com -s project` を実行する |
| `✗ clone  ghq が PATH にありません` | `ghq` か `git` が入っていない。**この2つは巡回が worktree を作るときに起動する**ので、無いと段8 で必ず落ちる。入れて PATH を通す |

### `doctor` を通っても段8 の起動で落ちるもの

**次の2つは `continuo doctor` が検査していない。**`doctor` が「前提はすべて揃っています」と
言っても、段8 で `continuo` を起動した瞬間にこの2つで落ちることがある。
**どちらも hook を受ける socket の置き場所の話で、置き場所は `CONTINUO_RUNTIME_DIR` で変えられる。**

| 起動時に出るエラー | 直し方 |
| --- | --- |
| `hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）` | **macOS の Unix domain socket は絶対パス103バイトまで。**`CONTINUO_RUNTIME_DIR=/tmp/continuo-run /tmp/continuo` のように短い場所を指定して起動する |
| `既にある hook を受ける socket のディレクトリ … の権限が 0755 です` | continuo は**自分が作っていないディレクトリの権限を書き換えない。**`chmod 700 <その場所>` してから起動する |

**`status_field` に実在しない名前を書いたときの出力**（実際に `continuo Status` と書いて叩いた）。

```text
✓ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めました（front matter の検証も通りました）
✓ herdr           protocol 19（設定と一致）／herdr 0.8.0／socket ~/.config/herdr/herdr.sock
✓ gh の認証       scope に project が含まれる（github.com の有効なアカウント）
✗ ボード          ボードを読めません: tracker エラー [tracker_response]: GraphQL がエラーを返しました: [NOT_FOUND] Could not resolve to a Unions::ProjectV2FieldConfiguration with the name continuo Status
                  → WORKFLOW.md の tracker.provider（owner / project_number / status_field）を確認してください
! clone           ボードを読めなかったため、対象のリポジトリを特定できませんでした
! 信頼登録        ボードを読めなかったため、対象のリポジトリを特定できませんでした
! 資格情報        ~/.claude/.credentials.json がありません（macOS では Keychain に入っています）
                  → 判定を飛ばしました。continuo の起動には影響しません（Keychain は読みません）

4件に問題があります（✗ 1件 / ! 3件）
```

**設定が未記入のままだと、設定ファイルが `✗` になり、他の項目はすべて `!` になる。**
実際にプレースホルダを残したまま叩いた出力。

```text
✗ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めません: … 埋めていない設定が 2 件あります。値を埋めてください: tracker.provider.owner がプレースホルダ（__FILL_ME__）のままです / tracker.provider.project_number がプレースホルダ（0）のままです
                  → `continuo init` で雛形を置けます（既にある場合は front matter を直してください）
! herdr           設定ファイルを読めなかったため、照合する herdr.protocol が決まりません
! gh の認証       設定ファイルを読めなかったため、gh の認証を検査しませんでした
                  → WORKFLOW.md を直してから `continuo doctor` をもう一度実行してください
! ボード          設定ファイルを読めなかったため、どの project を見るか決まりません
! clone           ボードを読めなかったため、対象のリポジトリを特定できませんでした
! 信頼登録        ボードを読めなかったため、対象のリポジトリを特定できませんでした
! 資格情報        rate_limit の設定が読めないので、何を見るべきか決まりません
                  → 設定を直してからもう一度実行してください

7件に問題があります（✗ 1件 / ! 6件）
```

---

## 段7. issue を1件用意する

**ここから先は偽物一式（[test/e2e](../test/e2e)）で通してある。**
本物に対して叩くと、段8 で実際に Claude Code が動き、枠を消費する。

**実行する場所: どこでもよい**

```bash
gh issue create --repo <REPO> \
  --title "README に使い方を1行足す" \
  --body "README.md の先頭に、このリポジトリが何かを1行で書いてください。"
```

返ってきた issue の URL を控える。**ボードに載せる。**

**実行する場所: どこでもよい**

```bash
gh project item-add <PROJECT> --owner <ACCOUNT> --url <issue の URL>
```

**`Ready` にするのは画面での作業である。**ボードを開き、その issue の `Status`
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
| 3 | `Ready` の issue を取り、**Status を `In Progress` へ書く** | **ボードで見える** |
| 4 | worktree を作り、herdr の pane で Claude Code を起動する | **herdr の画面で見える** |
| 5 | エージェントが作業し、`CONTINUO-STATUS:` の行を出す | herdr の pane |
| 6 | continuo がその行を読み、**Status を `In Review` へ動かす** | **ボードで見える** |
| 7 | 人間が確認して **`Done` へ動かす** | 人間の操作 |
| 8 | continuo が worktree と branch を片付ける | 置き場所から消える |

**巡回の間隔は既定30秒である。**すぐには動かない。

**起動に成功したときのログ**（`Ready` が0件の状態で実際に叩いたもの。
**この状態では Claude Code は起動しないので、枠を消費しない**）。

```text
continuo を起動します（設定ファイル: ~/continuo-try/WORKFLOW.md）
level=INFO msg=設定ファイルを読み込みました path=~/continuo-try/WORKFLOW.md
level=INFO msg="hook を受ける socket の場所を決めました" socket=/tmp/cnt-run/hooks.sock
level=INFO msg=二重起動防止のロックを獲得しました lock_file=/tmp/cnt-run/continuo.lock
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

> **最後の `WARN` は macOS では必ず出る。**段6 の `資格情報` が `!` になるのと同じ理由で、
> **Keychain を読まない**ためである。**起動は止まらない。**

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

**`ls ~/worktrees` に出るのは `github.com` の1行だけである。**`workspace.layout` が `gwq` なので、
worktree はその下の `~/worktrees/<ホスト>/<owner>/<repo>/<branch の / を - にしたもの>` に掘られる。
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
**書き換えたら continuo を再起動する**（設定の読み直しは未実装。最後の節を見よ）。

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

---

## 段9. 止める・片付ける

**continuo を動かしている端末で** `Ctrl+C` を押す。

**巡回を止め、hook の受け口を閉じ、走行中の turn の終わりを待ってから抜ける。**
**pane は閉じない。**次に起動したとき、その pane を引き継ぐ。

実際に叩いた出力（終了コードは 0）。

```text
level=INFO msg="巡回を止めました（hook の受け口を閉じて turn ループの終了を待ちます）"
level=INFO msg="走行中の turn ループが終わりました（pane は閉じていません）"
level=INFO msg=continuo を終了しました
```

### 試したあとの片付け

| 何を | どうするか |
| --- | --- |
| worktree と branch | **Status を `Done` にすれば continuo が片付ける。**残っていれば `~/worktrees` の下を見て `git worktree remove` と `git branch -D` |
| push した branch | **GitHub には残る。**continuo が消すのは手元の branch だけである（`cleanup.require_pushed` で push を確かめてから消しているので、成果は GitHub 側に残る）。要らなければ GitHub の画面か `git push origin --delete <branch>` で消す |
| ボードの item | **ボードは消さない。**試した issue だけを画面から外すか、`Done` に置いたままにする |
| 信頼の登録 | `~/.claude.json.continuo-backup-<日時>` から戻すか、`projects` の該当キーを消す。**バックアップを消すのは人間である** |
| 作業ディレクトリ | `~/continuo-try` を消す |
| 実行ファイル | `/tmp/continuo` を消す |
| 実行時ディレクトリ | **上から順に最初に見つかった場所。**`CONTINUO_RUNTIME_DIR` → `$XDG_RUNTIME_DIR/continuo` → macOS なら `$TMPDIR/continuo` → `~/.continuo/run`。**`XDG_RUNTIME_DIR` は macOS でも先に見る。**中に socket が残っていれば消す |

---

## うまく動かないとき

| 症状 | 見るところ |
| --- | --- |
| **issue を取ってくれない** | Status が `Ready` か。`doctor` の `ボード` と `信頼登録` が `✓` か。**信頼の門番は `~/.claude.json` である**（`trust.repositories` に書くだけでは足りない。`continuo trust` の実行が要る。段5） |
| **別のフィールドを書き換えている** | `status_field` が段2 で確かめた名前になっているか。continuo は `status_field` に書いた名前のフィールドしか読み書きしない |
| **信頼していないリポジトリの issue を飛ばしている** | `doctor` の `信頼登録` が `✓` か。**未信頼だと worktree も pane も作られない。**そのリポジトリにつき1回、**issue にコメントが投稿される**（直し方もそこに書いてある） |
| **`In Review` にならない** | エージェントが `CONTINUO-STATUS: review` を出しているか。herdr の pane で応答を見る |
| **片付かない** | **未コミットの変更が残っている**か、**push していない commit がある**と消さない（成果を失わないため）。ログに理由が出る |
| 枠を使い切った | continuo は待って再開する。Claude Code 2.1.234 以降は Claude Code 自身も継続するので、continuo は `agent_status` を見て二重投入を避ける |
| **同じ issue に Claude Code が2つ立った** | 起きてはならない。**再現手順を添えて issue を立ててほしい** |

---

## いま無いもの

**設定の読み直しが実装されていない**（`SPEC.md` 6.2）。
`WORKFLOW.md` を書き換えても、**continuo を再起動するまで反映されない。**

詳しくは [plans/impl/tasks.md](plans/impl/tasks.md) の「未実装として残っているもの」を見ること。
