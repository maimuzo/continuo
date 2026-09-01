# continuo

[![ci](https://github.com/maimuzo/continuo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/maimuzo/continuo/actions/workflows/ci.yml)

**[English](README.md)**

**GitHub Projects v2 をカンバンとして使い、そこに載せた issue をコーディングエージェントに片付けさせるオーケストレーターです。**複数のリポジトリの issue を1枚のカンバンに載せ、`Ready` へ動かすだけで、continuo が git worktree を用意し、[herdr](https://github.com/herdrdev/herdr) の pane で Claude Code を動かします。結果は Status の変化としてカンバンへ返ります。

Go で書いており、OpenAI の symphony の仕様を実装しています。**Claude Code は対話モードのまま動くので、何をしているかを見ながら進められます。**いつもの定額プランのまま使えます。

---

## 想定しているカンバンの運用方法

**タスクを issue にまとめ、`Ready` へ置くだけです。**あとは continuo が進めます。`Blocked` へ移っていたら、エージェントが行き詰まっています。issue のコメントで指示してください。`In Review` へ移っていたら作業は終わっています。内容を確認して、よければ `Done` へ動かしてください。

![continuo が動かすカンバン](docs/images/board.png)

| Status | 誰が動かすか | そのとき何が起きているか |
| --- | --- | --- |
| `Ready` | **あなた** | continuo がカンバンの上から順に拾い、herdr で動かします |
| `In Progress` | continuo | 着手すると自動で移ります。feature branch を作り、git worktree の上で作業を始めます |
| `In Review` | continuo | エージェントが作業を終えると自動で移ります。issue を開いて結果を確認してください。**`Done` へ動かすかどうかは、あなたが決めます** |
| `Blocked` | continuo | エージェントが行き詰まったときと、あなたの回答を待っているときに自動で移ります。issue のコメントに対応方法を書いて、`Ready` へ戻してください |
| `Done` | **あなた** | ここへ動かすと、continuo が worktree と branch を消します |

**Status の名前は、いま使っているものそのままで構いません。**`Ready` でも `Todo` でも、`continuo setup` で5つの役割に一度だけ対応づけます。

## エージェントの状況を確認可能

**Claude Code は、herdr の pane で対話モードのまま動きます。**見えないままバックグラウンドで動くのではありません。herdr の画面を開けば様子が分かりますし、別の端末から読むこともできます。

```bash
herdr agent read continuo-hello-world-188 --source recent-unwrapped --lines 40
```

**従量課金にはなりません。**`claude -p` も Agent SDK も **API の直叩き**も使いません。定額プランのまま、issue ごとに対話セッションを1つ保ち、そこへ続きの指示を送ります。**同じセッションの中で文脈が積み上がります。**

**`continuo --port 8080` で起動すると、進行中の issue の一覧を `http://localhost:8080` で見られます。**

## カンバン1枚で複数のリポジトリを回せる

**1枚のカンバンに、リポジトリをいくつ混ぜても構いません。**continuo は issue ごとに、そのリポジトリの下へ worktree を作ります。

```
~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188/
~/worktrees/github.com/octocat/sample-app/continuo-octocat-sample-app-42/
```

**同時に動かす issue の数は設定で決めます**（既定2件）。

## 始める前に知っておくこと

**エージェントは、あなたのリポジトリを実際に編集し、commit して push します。**continuo は Claude Code を「人間に確認を出さないモード」で起動し、引数を制限せずに `Bash` を許可します。**確認のダイアログは出ません。**

**issue の本文とコメントは、そのままエージェントへの指示になります。**既定の指示書には、
本文とコメントを **JSON で**読むように書いてあります。GitHub が付けた投稿者の立場（`authorAssociation`）が、本文と混ざらずに届きます。
**命令として従うのは `OWNER` / `MEMBER` / `COLLABORATOR` が書いたものだけ**で、それ以外は報告として読みます。
**これは穴を狭めるだけで、塞ぎはしません。**エージェントは確認なしで `Bash` を実行できます。

**public なリポジトリを載せるときは、その文を書いたのが他人であることを忘れないでください。**
issue もコメントも第三者が書けます。**「このリポジトリを消せ」と書かれていたら、そのとおりに動きます。**

| 抑え方 | どうするか |
| --- | --- |
| **自分が書いた issue だけを進める** | `Ready` へ動かすのはあなたです。**中身を読んでいない issue を動かさない** |
| **ラベルで絞る** | `tracker.required_labels` に目印のラベルを書き、**それが付いた issue だけ**を対象にする |
| **隔離して動かす** | 専用のアカウントを使うか、捨ててよい機械・コンテナで動かす |

**最初は使い捨てにできるリポジトリで試してください。**本業のリポジトリをいきなり指定しないこと。

**agent teams には対応していません。**Claude Code の実験的な機能で、既定では無効です。
**有効にしていると、issue が失敗することがあります**（[docs/FAQ.md](docs/FAQ.md) の
「「作業の途中で確認の画面に止まりました」と出る（agent teams が有効な場合）」を参照）。

## 必要なもの

| | |
| --- | --- |
| OS | macOS / Linux。**Windows ネイティブは非対応**（WSL2 を使う） |
| [herdr](https://github.com/herdrdev/herdr) | **pane と worktree を束ねる常駐プロセス。**continuo は herdr を通して Claude Code を動かす。**0.8.0 で動作を確認**（socket の protocol が食い違うと、continuo は起動しない） |
| [Claude Code](https://claude.com/claude-code) | **定額プランで使う。**2.1.233 で動作を確認 |
| [`gh`](https://cli.github.com/) | `gh auth login -s project` でログイン済みであること。2.97.0 で動作を確認 |
| [`git`](https://git-scm.com/) / [`ghq`](https://github.com/x-motemen/ghq) | worktree の作成と、clone の場所の解決に使う |
| [Go](https://go.dev/dl/) 1.26+ | ビルドにだけ必要 |

**カンバンには Status の選択肢が5つ要ります。**GitHub の既定は `Todo` / `In Progress` / `Done` の3つなので、**足りない2つは GitHub の画面から足してください** — カンバンの `Settings` を開き、左の `Custom fields` の `Status` を選び、`Options` の下の `Add option...` に名前を入れて `Add`。名前は何でも構いません。役割との対応は `continuo setup` で決めます。

**`continuo doctor` は17の項目を検査します** — 設定ファイル / 片付けの状態 / **未記入の項目** / claude / **hook の置き場所** / **ロックの場所** / **カンバンロック** / Claude の設定 / worktree の場所 / herdr / gh の認証 / カンバン / Status の名前 / 対応表のキー / clone / 信頼登録 / 資格情報（定額プランの枠を読むためのもの）。**OS と Go の版は調べないので、そこは自分で確認してください。**

**`✗` が1つでもあれば終了コードは 1、`!` だけなら 0 です。**
**ただし「終了コードが 0」は「continuo が起動する」という意味ではありません。**
**カンバンを読めなかったこと**（レートリミットや、検査が時間切れになったとき）**も `!` で出ます。**
continuo は起動のたびに同じ読み取りを行うので、**その `!` が出ているあいだは起動しません。**
時間をおいてから `continuo doctor` をやり直してください。

## インストール

```bash
curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh
```

**インストーラーがすること。**OS と命令セットを見分け、[release](https://github.com/maimuzo/continuo/releases) から実行ファイルを取り、**チェックサムを照合してから** `~/.local/bin/continuo` へ置きます。**足りない道具があれば1つずつ尋ねます**（`git` / `gh` / `ghq`）。**`herdr` と `claude` は入れません** — どちらも独自の配布経路と認証があるため、案内するだけです。

| オプション | 何をするか |
| --- | --- |
| `--yes` | すべての確認に「はい」と答える（**パッケージの導入も含む**） |
| `--no-deps` | 道具を1つも入れず、足りないものを並べるだけ |
| `--dir DIR` | 置き先を変える（既定 `~/.local/bin`） |
| `--version V` | 版を指定する（既定は最新の release） |
| `--repo O/R` | fork から入れる。**使うと警告が出る** |

**チェックサムを照合できなければ止まります。**承知のうえで続けるなら `--insecure-no-checksum` を付けてください。

**チェックサムだけでは改竄を検知できません。**チェックサムは実行ファイルと同じ release に置いてあるので、
release ごと差し替えられれば、両方とも差し替わります。**GitHub が署名した出所の証明（provenance）のほうが強く**、
`gh` が入っていればインストーラーが自動で確かめます。手で確かめるなら次を叩いてください。

```bash
gh attestation verify continuo_darwin_arm64.tar.gz --repo maimuzo/continuo
```

**ソースからビルドすることもできます。**Go 1.26 が要ります。

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
mise trust && mise install                       # mise で Go を入れているなら1回だけ
go build -o ~/.local/bin/continuo ./cmd/continuo
sh scripts/test-like-ci.sh                       # テストを走らせる（約3分。任意）
```

## 使い方

**continuo はカンバンを作りません。**いま使っているカンバンを、そのまま使います。

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init      # WORKFLOW.md を置く。owner とカンバンの番号は gh から引く
```

**ここで一度 `WORKFLOW.md` を開いてください。**`trust.repositories` に、カンバンに載っていたリポジトリが全部並んでいます。**要らない行を消さないと、無関係なリポジトリまで Claude Code に信頼登録されます。**

```bash
continuo setup                    # カンバンの Status を continuo の5つの役割に対応づける（対話）
continuo trust --dry-run          # 何を信頼登録するかを、実行せずに表示する
continuo trust                    # 対象リポジトリを信頼登録する。clone が無ければ取ってくる
continuo allow-keychain-access    # macOS だけ。定額プランの枠を読むために1回
continuo doctor                   # 前提が揃っているか調べる

continuo                          # 常駐を始める
```

**あとは issue を `Ready` に置くだけです。**

**うまく動かないときは [docs/FAQ.md](docs/FAQ.md) を見てください。**画面に出たメッセージから引ける一覧です。

**止めるときは `Ctrl+C`。**巡回を止め、turn ループを畳んで抜けます。**pane は閉じません。**Claude Code はそのまま動き続けるので、次に起動したとき、その pane を引き継いで続きから進めます。

**フラグは位置引数の前でも後ろでも構いません。**`git` や `gh` と同じで、`continuo trust ~/continuo-work --dry-run` と `continuo trust --dry-run ~/continuo-work` は同じ意味です。**`--` より後ろは、`-` で始まっていても位置引数として扱います。**continuo が知らないフラグは、どこに書いてもエラーです。

### 間違えて着手したとき

**`Ready` に置く issue を間違えたら、`continuo abandon` で着手する前の状態へ戻します。**worktree・pane・herdr の workspace・branch をまとめて消します。

```bash
continuo abandon --dry-run https://github.com/octocat/hello-world/issues/42   # 何が消えるかだけ見る
continuo abandon https://github.com/octocat/hello-world/issues/42              # 消す
```

**先に `--dry-run` を叩いてください。**消す前に、その issue の Status・worktree のパス・branch・herdr の pane・**コミットされていない変更のファイル数**・**push されていない commit の件数**を並べます。

**`--dry-run` はカンバンに1文字も書きません。**Status を書き換えず、continuo に手を離させることもしません。実行したらどの Status へ動かすかを、その場で1行お知らせするだけです。

**書けない Status は、何かを消す前に弾きます。**`--to` と手を離させる先（`--park`）は、先にカンバンの Status の選択肢と突き合わせます。**`--park` に作業中の Status を渡すと、その場で止まります**（そこへ動かしても continuo は手を離さず、pane も閉じないためです）。**一致する worktree が無いときは `--to` を使いません。**黙って捨てず、動かしていないことをお知らせします（URL の打ち間違いだと、別の issue の Status を動かすことになるためです）。

**失うものがあると、何も消さずに止まります。**それでも消すなら `--force` を付けてください。

**worktree が無くても、その issue の branch が残っていれば片付けます。**着手が途中で失敗すると、
**branch だけが残る**ことがあります。`abandon` は `herdr.worktree.branch_template` と渡された URL から
branch 名を組み立てて探し、残っていれば名前・リポジトリ・先頭の commit と、
**どの remote にも載っていない commit の件数**をお知らせします。
**消すには `--force` が要ります**（worktree が無いので、コミットしていない編集が残っていたかは調べようがないためです）。
消したときは、戻すためのコマンド（`git -C <clone> branch <名前> <SHA>`）を1行でお知らせします。

**git が「worktree が使っている」と断ったら、そこで止まります。**continuo は
`git worktree prune` を代わりに叩きません。**その登録が指すディレクトリは、消えたのではなく
移されただけかもしれない**からです。断られたときは、登録の在りかと、
`git -C <clone> worktree prune` を叩いてからやり直す手順をお知らせします。

**元から無かった branch を「残っています」とは言いません。**着手が失敗し続けて branch が
1度も作られなかったときは、「消す対象がありませんでした」とだけお知らせします。
**リポジトリを名指しできず、実在するかを確かめられないときだけ**、いままでどおり残ったものとしてお知らせします。

**worktree の `.git` が壊れていても片付けられます。**worktree の `.git` は1行だけのファイルで、空になったり書き換えられたり消えたりすると、その中では `git` が1つも通らなくなります。**`abandon` はそこで止まりません**（まさにそれを片付けるための道具だからです）。見られる範囲を見せ、**調べられなかったことと git が返した理由を並べ、「失うものはありません」とは決して言いません。**中身が分からない以上、**消す実行では `--force` を要求します。**付ければ、worktree のディレクトリ・branch・herdr の workspace を git に頼らずに消し切ります。

**herdr が答えないときも同じ扱いです。**`--force` が無ければ、pane の生死を確かめられないまま消すことはしません。付いていれば、確かめずに消したことをその場でお知らせします。**唯一そのまま止まるのは、worktree の `.git` が「別のリポジトリ」を指していたときです。**壊れているのではなく書き換えられた痕跡なので、1バイトも消しません。

**continuo が動いていても、そのまま叩けます。**その issue から手を離させてから消します。**まだ作業中の Status なら**、一時的に `tracker.failure_state`（既定では `Blocked`）へ動かし、pane が閉じるのを待ちます。**動かす先は `--park` で変えられます。閉じなければ、何も消さずに止まります。**

**手を離させたあとで止まった場合、Status はその値のまま残ります。**continuo は元へ戻しません（戻す先は作業中の Status なので、戻した瞬間に continuo がその issue を拾い直しかねないためです）。**そのことを1行でお知らせします。**戻すかどうかはカンバンで決めてください。

**continuo が動いていないと判定したときも、消す前に pane を確かめます。**ロックファイルは `~/.continuo/continuo.lock` の1本に固定されているので、launchd から起動した continuo と端末で叩いた `abandon` が別の場所を見ることはありません。**`--id <名前>` を付けて動かしているときは、`abandon` にも同じ名前を渡してください。****その worktree の pane が生きていれば、ロックが何と言っていても消しません。**

**片付けたあとの Status は動かしません。**「もう要らない」のか「書き直して出し直す」のかは continuo には分からないので、**カンバンで決めてください。**決まっているなら `--to "Ice Box"` のように渡せます。

**カンバンの操作だけでは取り消せません。**これが `continuo abandon` を作った理由です。

| やりたくなること | 実際に起きること |
| --- | --- |
| `Ready` へ戻す | **止まりません。**`Ready` は作業中の Status の1つなので、continuo はそのまま続けます。むしろ着手待ちの Status でもあるので、**もう一度着手されることがあります** |
| `Done` へ動かす | **Claude Code が起動し直されます。**continuo は片付ける前に「この作業のコメントが issue にあるか」を確かめ、無ければセッションを復元して書かせようとします。**間違えて着手した issue には、書かせる成果がありません** |

### 設定

`continuo init` が `WORKFLOW.md` を置きます。**この1枚が設定ファイルであり、エージェントへ送る指示書でもあります。**

**先頭の front matter が設定です。**よく触るのは次の4つ。

```yaml
tracker:
  provider:
    owner: octocat                # GitHub のアカウント名
    project_number: 3             # カンバンの番号
agent:
  max_concurrent_agents: 2        # 同時に動かす issue の数
claude:
  turn_timeout_ms: 3600000        # 画面が変わらないまま何ミリ秒たったら打ち切るか
```

**`turn_timeout_ms` は turn の総時間ではありません。**herdr が見ている画面が変わり続けている限り、1つの指示に何時間かかっても打ち切りません。

**front matter より下が、Claude Code へ送る1回目のプロンプトそのものです。**「終わったら `CONTINUO-STATUS: review` と書け」「その前に commit して push しろ」といった依頼が書いてあります。**プロジェクトの流儀に合わせて書き換えてください。**

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

## 仕組み

**turn の終わりは、herdr が pane を見て判定します。**continuo は herdr の socket へ「指示を送って待て」と頼み、エージェントが `idle` / `done` / `blocked` になるまで待ちます。**herdr のコマンドを起動するのではなく、socket を直接開いて JSON をやり取りします。**

1. continuo が issue に着手し、Status を `In Progress` にする
2. worktree を作り、herdr の pane で Claude Code を起動して、指示を送る
3. **herdr が「エージェントは止まった」と返す**
4. **Claude Code の `Stop` hook で裏を取る。**`background_tasks` が空になり、そのあと数秒たっても新しい仕事が始まらなければ、turn が終わったと確定する
5. transcript を読み、`CONTINUO-STATUS:` の行があれば Status を動かす
6. issue がまだ作業中の Status なら、続きの指示を送る（`agent.max_dispatch_turns` 回まで。既定20回）

**信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

## 開発の状況

**実機で issue を1件、着手から片付けまで通しました。**`Ready` から拾い、herdr の pane で作業し、`Done` へ動き、worktree と branch も消えました。自分で試す手順は [docs/trying_it_out.md](docs/trying_it_out.md) にあります。

**v0.x のうちは、設定の形を変えることがあります。**`WORKFLOW.md` の front matter は未知のキーを弾くので、
**キーを消したり改名したりすると、古い設定ファイルは起動しなくなります。**その変更は release notes に書きます。

## もっと詳しく

| | |
| --- | --- |
| **なぜそう作ったか**（読むならこちら） | [docs/plans/continuo_design_slim.md](docs/plans/continuo_design_slim.md)（634行） |
| 判断の根拠・実測値・比較した案 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md)（4800行近い） |
| ユースケース記述（RUCM） | [docs/spec/usecases/](docs/spec/usecases/) |
| **作りの形からくる問題**（コードを直す前に読む） | [docs/bug_details.md](docs/bug_details.md)（繰り返し噛みつく7つと、触るときの注意） |
| **issue 1件が着手から片付けまでどう進むか** | [docs/agent_life_cycle.md](docs/agent_life_cycle.md)（Status の移り変わり・会話の引き継ぎ・自動化に横取りされた Status の戻し方。図つき） |
| **うまく動かないとき** | [docs/FAQ.md](docs/FAQ.md)（画面のメッセージから引く） |
| **新しい版に上げたあと** | [docs/upgrading.md](docs/upgrading.md)（増えた設定・書かないとどうなるか・確かめ方） |
| **開発とテスト** | [CONTRIBUTING.md](CONTRIBUTING.md) |
| 実行ファイルに含まれる第三者のソフトウェア | [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) |

## 準拠する仕様

**symphony は、コーディングエージェントを束ねるオーケストレーターの仕様です**（OpenAI が公開。Apache-2.0）。continuo はこれを実装しています。仕様の本文は [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) にあります（**このリポジトリには同梱していません**）。

**仕様は Codex の app-server と stdio でやり取りする前提です。**continuo はそこを、herdr の pane で動く Claude Code に読み替えました。守れない `MUST` と、代わりに何をするかは [docs/plans/continuo_design.md](docs/plans/continuo_design.md) の「8. symphony の仕様と異なるところ」にあります。

## ライセンス

**MIT** — [LICENSE](LICENSE)
