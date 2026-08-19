<!-- 目的: 人間が continuo を実際に動かして確かめるための手順 -->

# 試してみる / Trying it out

**English:** This walks `continuo` through one issue end to end: build it, create a throwaway
board whose Status options are made by a single `gh` command (no clicking in the web UI),
clone the target repository and approve the folder trust, generate `WORKFLOW.md` with
`continuo init` (owner and project number are filled in automatically from `gh`), check the
prerequisites with `continuo doctor`, put one issue into `Ready`, and watch `continuo` open a
worktree and drive Claude Code until the board says `Done`. Every command block states which
directory to run it in and works on its own. The table below says which steps were actually
executed while writing this document and which were not.

**言いたいこと。**1件の issue が `Ready` から `Done` まで通るのを、実際に目で見るための手順である。
**ボードの Status は `gh` のコマンド1本で作るので、GitHub の画面での設定作業は要らない。**
**すべてのコマンドは「どこで実行するか」を書いてあり、そのブロックだけで動く。**

## この文書のどこを実際に叩いたか

**叩いていない段は、そう明記してある。**

| 段 | 叩いたか | 補足 |
| --- | --- | --- |
| 段1 ビルドする | **叩いた** | `go build` と `continuo init --help` |
| 段2 ボードを作る | **叩いていない** | **本番のボードを触らないため。**コマンドの構文は gh 2.97.0 で確認したもの |
| 段3 リポジトリを置く・信頼を承認する | **叩いていない** | Claude Code の信頼の確認は対話が要る |
| 段4 設定を置く | **叩いた** | `gh` がある場合と無い場合の両方 |
| 段5 前提を検査する | **叩いた** | 正常なとき・設定が未記入のとき・フィールド名が違うときの3通り |
| 段6〜段8 | **叩いていない** | **ここから先は実際に Claude Code が動き、枠を消費する** |

> **出力例は実際に叩いた結果である。**ただし個人のパスとアカウント名だけを
> `~` と `<ACCOUNT>` / `<PROJECT>` に置き換えてある。

---

## 先に決めること

**この手順では、次の3つを自分の値に置き換える。**先に決めて手元に控えておく。

| 記号 | 何を入れるか | 例 |
| --- | --- | --- |
| `<ACCOUNT>` | あなたの GitHub アカウント名 | `octocat` |
| `<REPO>` | **試す対象のリポジトリ**（issue を置く場所） | `octocat/sandbox` |
| `<PROJECT>` | 段2 で作るボードの番号 | `7` |

> **`<REPO>` は使い捨てにできるものを選ぶ。**エージェントがそのリポジトリを実際に編集し、
> commit して push する。**本業のリポジトリを指定しない。**

## 先に知っておくこと

| 何を | なぜ |
| --- | --- |
| **本番のボードで試さない** | continuo は Status を書き換える。**使い捨てのボードを作る**（段2） |
| **段7 から枠を消費する** | 実際に Claude Code が起動し、issue を実装しようとする |
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

---

## 段2. 使い捨てのボードを作る

**言いたいこと。**ボードを作り、**continuo 専用の Status フィールドを1本作る。**
**この2コマンドで終わり、GitHub の画面での作業は無い。**

**実行する場所: どこでもよい**（GitHub に対する操作である）

```bash
gh project create --owner <ACCOUNT> --title "continuo-試用" --format json --jq .number
```

出た数字が `<PROJECT>` である。控えておく。

**実行する場所: どこでもよい**

```bash
gh project field-create <PROJECT> --owner <ACCOUNT> \
  --name "continuo Status" \
  --data-type SINGLE_SELECT \
  --single-select-options "Ready,In Progress,In Review,Blocked,Done"
```

**GitHub が最初に作る `Status` には触らない。**別に `continuo Status` という
single-select フィールドを新しく作り、continuo にはそちらを読ませる（段4 で設定する）。

| 選択肢 | 何に使うか |
| --- | --- |
| `Ready` | **ここに置いた issue を continuo が取る** |
| `In Progress` | 取ったときに continuo が書き込む |
| `In Review` | エージェントが `CONTINUO-STATUS: review` を出すと入る。**人間のレビュー待ち** |
| `Blocked` | エージェントが判断を仰ぐとき、または打ち切ったときに入る |
| `Done` | **人間がここへ動かすと、continuo が worktree と branch を片付ける** |

**なぜ既定の `Status` に足さないのか。**既にある single-select フィールドに選択肢を足す API は
`updateProjectV2Field` しか無い。**この API は選択肢の指定を全件置き換えとして扱い、
GitHub が全部の選択肢に新しい ID を採番し直すので、設定済みの Status の値が全部消える。**
**`--single-select-options` はフィールドを新しく作るときにだけ効く**（gh 2.97.0 で確認）。
だから、足すのではなく作る。

### 別の道: 既定の `Status` を使う

**既存のボードで試したいなど、既定の `Status` を使いたい場合は画面から選択肢を足す。**
上の表の5つのうち、足りないものを追加する。

ボードを開く → 右上の `⋯` → `Settings` → 左の `Status` → `+ Add option`

その場合、段4 の `status_field` は既定の `Status` のままでよい。

> **API で選択肢を足してはならない。**理由は上のとおりである。必ず GitHub の画面から追加する。

---

## 段3. 対象リポジトリを手元に置く

**実行する場所: どこでもよい**（`ghq` が置き場所を決める）

```bash
ghq get <REPO>                   # continuo は ghq で clone の場所を引く
ghq list -p -e <REPO>            # clone の絶対パスが出る。0行なら失敗している
```

### フォルダの信頼を承認する（対話での作業）

**実行する場所: clone の中**

```bash
cd "$(ghq list -p -e <REPO>)"
claude
```

Claude Code が「このフォルダを信頼するか」を聞いてくるので**承認する。**
承認したら `/exit` で抜けてよい。

**信頼を承認していないと hook が1つも動かない。**turn の終わりを検知できないので、
continuo はその issue を飛ばす（段5 の `信頼登録` が `✗` になる）。

---

## 段4. 設定を置く

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

`~/continuo-try/WORKFLOW.md` ができる。**`owner` と `project_number` は `gh` から引いて自動で入る。**
入った値はその場に出る。

```text
WORKFLOW.md を作成しました: ~/continuo-try/WORKFLOW.md
✓ tracker.provider.owner: <ACCOUNT>（`gh api user` が返した GitHub のログイン名です）
✓ tracker.provider.project_number: <PROJECT>（`gh project list` の候補が1件だけでした: #<PROJECT> continuo-試用）
```

### 読ませるフィールド名を書く

**段2 で `continuo Status` を作った場合は、`WORKFLOW.md` の `status_field` を書き換える。**
既定は `Status` である。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
${EDITOR:-vi} WORKFLOW.md
```

front matter の8行目あたりを次のように直す。

```yaml
tracker:
  provider:
    status_field: continuo Status            # 段2 で作ったフィールドの名前
```

**「別の道」で既定の `Status` に選択肢を足した場合は、この書き換えは要らない。**

### 自動で入らないとき

**ボードが2枚以上あると `project_number` は自動で決まらない。**候補が番号・名前・URL で並ぶので、
段2 で控えた番号を指定して置き直す。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo init --project <PROJECT> --force
```

**`gh` が無い・認証が無いときも `WORKFLOW.md` は作られ、終了コードは 0 である。**
その場合だけプレースホルダ（`__FILL_ME__` と `0`）が残るので、案内どおりに手で埋める。
実際に `gh` を PATH から外して叩いた出力。

```text
WORKFLOW.md を上書きしました: ~/continuo-try/WORKFLOW.md
! tracker.provider.owner: 埋められませんでした（gh コマンドが見つかりませんでした）
  → gh を入れて `gh auth login -s project` でログインしてください
  → または `continuo init --owner <名前>` でもう一度実行してください
  → https://github.com/maimuzo なら maimuzo の位置が owner です
! tracker.provider.project_number: 埋められませんでした（owner が決まらないので、ボードの候補を引けませんでした）
  → 先に owner を決めてから、もう一度 `continuo init` を実行してください
  → または `continuo init --project <番号>` でボードの番号を直接指定してください
埋まらなかった値は WORKFLOW.md の中でプレースホルダのままです。上の案内どおりに書いてください
```

**既に `WORKFLOW.md` があると `init` は上書きせずに止まる**（終了コード 1）。

```text
~/continuo-try/WORKFLOW.md は既にあります。上書きするなら --force を付けてください
```

**`workspace.root` も見ておく。**worktree を置く場所である（既定は `~/worktrees`）。

---

## 段5. 前提が揃っているかを検査する

**実行する場所: `~/continuo-try`**（`WORKFLOW.md` を置いた場所）

```bash
cd ~/continuo-try
/tmp/continuo doctor
```

7項目を検査して、足りないものと直し方を出す。**`✗` が1つでもあれば終了コードは 1。**
実際に叩いた出力。

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
`clone` と `信頼登録` は、段6 で issue を `Ready` に置くと `✓` か `✗` に変わる。

**別のディレクトリから叩くなら、`WORKFLOW.md` のパスを1つだけ渡せる。**

**実行する場所: どこでもよい**

```bash
/tmp/continuo doctor ~/continuo-try/WORKFLOW.md
```

### ここで詰まりやすいところ

| 症状 | 直し方 |
| --- | --- |
| `Could not resolve to a Unions::ProjectV2FieldConfiguration with the name …` | `status_field` に書いた名前のフィールドがボードに無い。段2 で作った名前と綴りを合わせる |
| `ボードの Status の選択肢名が設定と一致しません` | 段2 の選択肢が足りない。**これを放置すると、巡回が無言で「対象0件」を返し続ける** |
| `hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）` | **macOS の Unix domain socket は絶対パス103バイトまで。**`CONTINUO_RUNTIME_DIR=/tmp/continuo-run /tmp/continuo doctor` のように短い場所を指定する |
| `実行時ディレクトリ … の権限が 755 です` | continuo は**自分が作っていないディレクトリの権限を書き換えない。**`chmod 700 <その場所>` してから起動する |
| `gh の scope に "project" がありません` | `gh auth refresh -h github.com -s project` を実行する |

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

## 段6. issue を1件用意する

**ここから先は叩いていない。段7 で実際に Claude Code が動き、枠を消費する。**

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

**`Ready` にするのは画面での作業である。**ボードを開き、その issue の
**`continuo Status`**（段2 で作ったフィールド。既定の `Status` ではない）を `Ready` にする。

> **最初に試す issue は小さいものにする。**エージェントは実際にコードを書き、commit して push する。

**ここでもう一度 `doctor` を叩くと、`clone` と `信頼登録` が `✓` になる**（対象リポジトリが決まるため）。

**実行する場所: `~/continuo-try`**

```bash
cd ~/continuo-try
/tmp/continuo doctor
```

---

## 段7. 動かす

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

### 別の端末から様子を見る

**実行する場所: どこでもよい**

```bash
ls ~/worktrees                                  # worktree ができたか（workspace.root）
gh project item-list <PROJECT> --owner <ACCOUNT> --format json | jq -r '.items[] | "\(.status)\t\(.title)"'
herdr workspace list                            # pane が立ったか
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

## 段8. 止める・片付ける

**continuo を動かしている端末で** `Ctrl+C` を押す。

**巡回を止め、hook の受け口を閉じ、走行中の turn の終わりを待ってから抜ける。**
**pane は閉じない。**次に起動したとき、その pane を引き継ぐ。

### 試したあとの片付け

| 何を | どうするか |
| --- | --- |
| worktree と branch | **Status を `Done` にすれば continuo が片付ける。**残っていれば `~/worktrees` の下を見て `git worktree remove` と `git branch -D` |
| 使い捨てのボード | GitHub の画面から消すか、`gh project delete` を使う（構文は `gh project delete --help` で確認すること。**番号を間違えると別のボードが消える**） |
| 作業ディレクトリ | `~/continuo-try` を消す |
| 実行ファイル | `/tmp/continuo` を消す |
| 実行時ディレクトリ | `CONTINUO_RUNTIME_DIR` を指定した場合、その場所を消す |

---

## うまく動かないとき

| 症状 | 見るところ |
| --- | --- |
| **issue を取ってくれない** | `continuo Status` が `Ready` か。`doctor` の `ボード` が `✓` か。**選択肢名が1文字でも違うと0件になる** |
| **既定の `Status` を書き換えている** | `status_field` の書き換えを忘れている（段4）。continuo は `status_field` に書いた名前のフィールドしか読み書きしない |
| **pane は立つが進まない** | 信頼の承認が済んでいるか（段3）。**信頼していないと hook が1つも動かない** |
| **`In Review` にならない** | エージェントが `CONTINUO-STATUS: review` を出しているか。herdr の pane で応答を見る |
| **片付かない** | **未コミットの変更が残っている**か、**push していない commit がある**と消さない（成果を失わないため）。ログに理由が出る |
| 枠を使い切った | continuo は待って再開する。Claude Code 2.1.234 以降は Claude Code 自身も継続するので、continuo は `agent_status` を見て二重投入を避ける |
| **同じ issue に Claude Code が2つ立った** | 起きてはならない。**再現手順を添えて issue を立ててほしい** |

---

## いま無いもの

**設定の読み直しが実装されていない**（`SPEC.md` 6.2）。
`WORKFLOW.md` を書き換えても、**continuo を再起動するまで反映されない。**

**信頼の一括登録は、意図的に作っていない**（設計 3-33）。
**信頼のダイアログは「リポジトリの中の設定が勝手に動かない」ための安全機構である。**
信頼すると、そのリポジトリの `.claude/settings.json` の `permissions.allow` が効くようになる。
**ボードに載っているだけのリポジトリを、中身を見ずにまとめて信頼するのは、この仕組みの目的を壊す。**
**段3 のとおり、リポジトリごとに1度だけ人間が承認する。**

詳しくは [plans/impl/tasks.md](plans/impl/tasks.md) の「未実装として残っているもの」を見ること。
