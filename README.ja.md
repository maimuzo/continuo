# continuo

[![ci](https://github.com/maimuzo/continuo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/maimuzo/continuo/actions/workflows/ci.yml)

**[English](README.md)**

GitHub Project v2をカンバンとして使い、複数リポジトリ上のissueをカンバン上のReady stateに配置することで、continuoがそれをherdr上で実行し、処理結果をカンバンにフィードバックするオーケストレーターです。OpenAIのSymphonyをベースにclaudeとgo言語とherdrを使って処理内容を確認しながら実行することができます。

---

## カンバンの上での挙動

**タスクをissueにまとめ `Ready` に置くだけです。**あとは continuo が処理します。Blockedに移ったらAIが困っているのでissueコメントで指示出ししてください。In Reviewに移ったら処理を終えているので内容を確認し、OKならDoneに移してください

![continuo が動かすカンバン](docs/images/board.png)

| Status | 誰が動かすか | そのとき何が起きているか |
| --- | --- | --- |
| `Ready` | **あなた** | continuo が上から順に herdr 上で処理します |
| `In Progress` | continuo | continuoが処理を開始したらこのstateに自動的に移ります。今処理中のissueです。featureブランチを作りgit worktree上で作業を開始します |
| `In Review` | continuo | AIがうまく処理できたら自動的に移ります。人間確認待ちなのでissueを開いて内容を確認してください。**OKなら `Done` へ動かすのはあなた** |
| `Blocked` | continuo | うまく処理できなかった場合、もしくは人間からの質問の回答待ちになると自動的に移ります。issueコメントに対応方法を書いて `Ready` に移してください |
| `Done` | **あなた** | issueが完了した状態です。ここに移るとcontinuo が worktree と branch を消します |

**Status の名前はボードに合わせられます。**`Ready` でも `Todo` でも、`continuo setup` で対応づけます。

## 何をしているか、見える

**Claude Code は herdr の pane で、対話モードのまま動きます。**バックグラウンドで、見えないまま動くのではありません。herdr の画面を開けば動いている様子が見えますし、別の端末から読むこともできます。

```bash
herdr agent read continuo-hello-world-188 --source recent-unwrapped --lines 40
```

**従量課金の API は使いません。**`claude -p` も Agent SDK も **API の直叩き**も使わず、定額プランのまま、対話セッションを維持して続きの指示を送ります。**同じセッションの中で文脈が積み上がります。**

**`continuo --port 8080` オプションで起動すると、進行中の issue の一覧を `http://localhost:8080` で見られます。**

## 複数のリポジトリを1枚で回せる

**カンバン1枚に、複数のリポジトリの issue を載せられます。**continuo は issue ごとに、そのリポジトリの worktree を作ります。

```
~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188/
~/worktrees/github.com/octocat/sample-app/continuo-octocat-sample-app-42/
```

**同時に動かす issue の数は設定で決めます**（既定2件）。

## 始める前に知っておくこと

**エージェントは、あなたのリポジトリを実際に編集し、commit して push します。**continuo は Claude Code を「人間に確認を出さないモード」で起動し、`Bash` を引数の制限なしに許可します。**確認のダイアログは出ません。**

**issue の本文とコメントは、そのままエージェントへの指示になります。**既定の指示書は
本文とコメントを **JSON で**読ませ、GitHub が付けた投稿者の立場（`authorAssociation`）を本文と分けて渡します。
**命令として従わせるのは `OWNER` / `MEMBER` / `COLLABORATOR` が書いたものだけ**で、それ以外は報告として読ませます。
**これは穴を狭めるだけで、塞ぎはしません。**エージェントは確認なしで `Bash` を実行できます。

**public なリポジトリを載せるときは、これが他人の書いた文であることを忘れないでください。**
issue もコメントも第三者が書けます。**「このリポジトリを消せ」と書かれていたら、そのとおりに動きます。**

| 抑え方 | どうするか |
| --- | --- |
| **自分が書いた issue だけを進める** | `Ready` へ動かすのは人間である。**知らない issue を動かさない** |
| **ラベルで絞る** | `tracker.required_labels` に印を入れ、**それが付いた issue だけ**を対象にする |
| **隔離して動かす** | 専用のアカウントか、捨ててよい機械・コンテナで動かす |

**最初は使い捨てにできるリポジトリで試してください。**本業のリポジトリをいきなり指定しないこと。

**agent teams には対応していません。**Claude Code の実験的な機能で、既定では無効です。
**有効にしていると、issue が失敗することがあります**（[docs/FAQ.md](docs/FAQ.md) の
「「作業の途中で確認の画面に止まりました」と出る（agent teams が有効な場合）」を参照）。

## 前提

| | |
| --- | --- |
| OS | macOS / Linux。**Windows ネイティブは非対応**（WSL2 を使う） |
| [herdr](https://github.com/herdrdev/herdr) | **pane と worktree を束ねる常駐プロセス。**continuo はこれ越しに Claude Code を動かす。**0.8.0 で動作を確認**（socket の protocol が一致しないと起動を止める） |
| [Claude Code](https://claude.com/claude-code) | **定額プランで使う。**2.1.233 で動作を確認 |
| [`gh`](https://cli.github.com/) | `gh auth login -s project` でログイン済みであること。2.97.0 で動作を確認 |
| [`git`](https://git-scm.com/) / [`ghq`](https://github.com/x-motemen/ghq) | worktree の作成と、clone の場所の解決に使う |
| [Go](https://go.dev/dl/) 1.26+ | ビルドにだけ必要 |

**ボードには Status の選択肢が5つ要ります。**GitHub の既定は `Todo` / `In Progress` / `Done` の3つなので、**足りない2つは GitHub の画面から足してください** — ボードの `Settings` を開き、左の `Custom fields` の `Status` を選び、`Options` の下の `Add option...` に名前を入れて `Add`。名前は何でも構いません。役割との対応は `continuo setup` で決めます。

**`continuo doctor` が15の検査を通します** — 設定ファイル / 片付けの状態 / **未記入の項目** / claude / **hook の置き場所** / Claude の設定 / worktree の場所 / herdr / gh の認証 / ボード / Status の名前 / 対応表のキー / clone / 信頼登録 / 資格情報（定額プランの枠を読むためのもの）。**OS と Go の版は調べないので、そこは自分で確認してください。**

**`✗` が1つでもあれば終了コードは 1、`!` だけなら 0 です。**
**ただし「終了コードが 0」は「continuo が起動する」という意味ではありません。**
**ボードを読めなかったこと**（レートリミット、応答を待つ期限切れ）**も `!` で出ます。**
continuo は起動のたびに同じ読み取りを行うので、**その `!` が出ているあいだは起動しません。**
時間をおいてから `continuo doctor` をやり直してください。

## 入れる

```bash
curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh
```

**やること。**OS と命令セットを見分け、[release](https://github.com/maimuzo/continuo/releases) から実行ファイルを取り、**チェックサムを照合してから** `~/.local/bin/continuo` へ置きます。**足りない道具があれば1つずつ尋ねます**（`git` / `gh` / `ghq`）。**`herdr` と `claude` は入れません** — どちらも独自の配布経路と認証があるため、案内するだけです。

| オプション | 何をするか |
| --- | --- |
| `--yes` | すべての確認に「はい」と答える（**パッケージの導入も含む**） |
| `--no-deps` | 道具を1つも入れず、足りないものを並べるだけ |
| `--dir DIR` | 置き先を変える（既定 `~/.local/bin`） |
| `--version V` | 版を指定する（既定は最新の release） |
| `--repo O/R` | fork から入れる。**使うと警告が出ます** |

**チェックサムを照合できなければ止まります。**承知のうえで続けるなら `--insecure-no-checksum` を付けてください。

**チェックサムだけでは改竄を検知できません。**書庫と同じ release から配るので、release ごと
差し替えられれば一緒に差し替わります。**GitHub が署名した出所の証明（provenance）のほうが強く**、
`gh` が入っていればインストーラーが自動で確かめます。手で確かめるなら次を叩いてください。

```bash
gh attestation verify continuo_darwin_arm64.tar.gz --repo maimuzo/continuo
```

**ソースから作ることもできます。**Go 1.26 が要ります。

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
mise trust && mise install                       # mise で Go を入れているなら1回だけ
go build -o ~/.local/bin/continuo ./cmd/continuo
sh scripts/test-like-ci.sh                       # テストを走らせる（約3分。任意）
```

## 使う

**ボードは新しく作りません。**いま使っているカンバンにそのまま足します。

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init      # WORKFLOW.md を置く。owner とボード番号は gh から引く
```

**ここで一度 `WORKFLOW.md` を開いてください。**`trust.repositories` に、ボードに載っていたリポジトリが全部並んでいます。**要らない行を消さないと、無関係なリポジトリまで Claude Code に信頼登録されます。**

```bash
continuo setup                    # ボードの Status を continuo の5つの役割に対応づける（対話）
continuo trust --dry-run          # 何を信頼登録するかを、実行せずに表示する
continuo trust                    # 対象リポジトリを信頼登録する。clone が無ければ取ってくる
continuo allow-keychain-access    # macOS だけ。定額プランの枠を読むために1回
continuo doctor                   # 前提が揃っているか調べる

continuo                          # 常駐を始める
```

**あとは issue を `Ready` に置くだけです。**

**うまく動かないときは [docs/FAQ.md](docs/FAQ.md) を見てください。**画面に出たメッセージから引ける一覧です。

**止めるときは `Ctrl+C`。**巡回を止め、turn ループを畳んで抜けます。**pane は閉じません。**Claude Code はそのまま動き続けるので、次に起動したとき、その pane を引き継いで続きから進めます。

**フラグは位置引数の前でも後ろでも構いません。**`git` や `gh` と同じで、`continuo trust ~/continuo-work --dry-run` と `continuo trust --dry-run ~/continuo-work` は同じ意味です。**`--` より後ろは、`-` で始まっていても位置引数として扱います。**知らないフラグは、どこに書いてもエラーのままです。

### 間違えて着手したとき

**`Ready` に置く issue を間違えたら、`continuo abandon` で着手する前の状態へ戻します。**worktree・pane・herdr の workspace・branch をまとめて消します。

```bash
continuo abandon --dry-run https://github.com/octocat/hello-world/issues/42   # 何が消えるかだけ見る
continuo abandon https://github.com/octocat/hello-world/issues/42              # 消す
```

**先に `--dry-run` を叩いてください。**消す前に、その issue の Status・worktree のパス・branch・herdr の pane・**コミットされていない変更のファイル数**・**push されていない commit の件数**を並べます。

**`--dry-run` はボードに1文字も書きません。**Status を書き換えず、continuo に手を離させることもしません。実行したらどの Status へ動かすかを、その場で1行お知らせするだけです。

**書けない Status は、何かを消す前に弾きます。**`--to` と手を離させる先（`--park`）は、先にボードの Status の選択肢と突き合わせます。**`--park` に作業中の Status を渡すと、その場で止まります**（そこへ動かしても continuo は手を離さず、pane も閉じないためです）。**一致する worktree が無いときは `--to` を使いません。**黙って捨てず、動かしていないことをお知らせします（URL の打ち間違いだと、別の issue の Status を動かすことになるためです）。

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

**手を離させたあとで止まった場合、Status はその値のまま残ります。**continuo は元へ戻しません（戻す先は作業中の Status なので、戻した瞬間に continuo がその issue を拾い直しかねないためです）。**そのことを1行でお知らせします。**戻すかどうかはボードで決めてください。

**continuo が動いていないと判定したときも、消す前に pane を確かめます。**ロックファイルの置き場所は環境変数（`CONTINUO_RUNTIME_DIR` / `XDG_RUNTIME_DIR` / `TMPDIR`）で決まるので、launchd から起動した continuo と端末で叩いた `abandon` で食い違うことがあります。**その worktree の pane が生きていれば、ロックが何と言っていても消しません。**

**片付けたあとの Status は動かしません。**「もう要らない」のか「書き直して出し直す」のかは continuo には分からないので、**ボードで決めてください。**決まっているなら `--to "Ice Box"` のように渡せます。

**ボードの操作だけでは取り消せません。**これが `continuo abandon` を作った理由です。

| やりたくなること | 実際に起きること |
| --- | --- |
| `Ready` へ戻す | **止まりません。**`Ready` は作業中の Status の1つなので、continuo はそのまま続けます。むしろ着手待ちの Status でもあるので、**もう一度着手されることがあります** |
| `Done` へ動かす | **Claude Code が起動し直されます。**continuo は片付ける前に「この作業のコメントが issue にあるか」を確かめ、無ければセッションを復元して書かせようとします。**間違えて着手した issue には、書かせる成果がありません** |

### 設定

`continuo init` が `WORKFLOW.md` を置きます。**この1枚が設定ファイルであり、エージェントへ送る指示書でもあります。**

**上半分（front matter）が設定です。**よく触るのは次の4つ。

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

**`turn_timeout_ms` は turn の総時間ではありません。**herdr が見ている画面の版が変わり続けている限り、1つの指示に何時間かかっても打ち切りません。

**下半分が、Claude Code へ送る1回目のプロンプトそのものです。**「終わったら `CONTINUO-STATUS: review` と書け」「その前に commit して push しろ」といった依頼が書いてあります。**プロジェクトの流儀に合わせて書き換えてください。**

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

## 動く仕組み

**turn の終わりは、herdr が pane を見て判定します。**continuo は herdr の socket へ「指示を送って待て」と頼み、agent が `idle` / `done` / `blocked` になるまで返りません。**herdr のコマンドを起動するのではなく、socket を直接開いて JSON をやり取りします。**

1. continuo が issue の処理を開始し、Status を `In Progress` にする
2. worktree を作り、herdr の pane で Claude Code を起動して、指示を送る
3. **herdr が「agent は止まった」と返す**
4. **Claude Code の `Stop` hook で裏を取る。**`background_tasks` が空になり、そのあと数秒たっても新しい仕事が始まらなければ、turn が終わったと確定する
5. transcript を読み、`CONTINUO-STATUS:` の行があれば Status を動かす
6. issue がまだ作業中の状態なら、続きの指示を送る（`agent.max_dispatch_turns` 回まで。既定20回）

**信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

## いまの状態

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
| 実行ファイルに含まれる第三者 | [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) |

## 準拠する仕様

**symphony は、OpenAI が公開しているコーディングエージェントのオーケストレーターの仕様です**（Apache-2.0）。continuo はこれを実装しています。仕様の本文は [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) にあります（**このリポジトリには同梱していません**）。

**仕様は Codex の app-server と stdio でやり取りする前提です。**continuo はそこを、herdr の pane で動く Claude Code に読み替えました。守れない `MUST` と、代わりに何をするかは [docs/plans/continuo_design.md](docs/plans/continuo_design.md) の「8. symphony の仕様と異なるところ」にあります。

## ライセンス

**MIT** — [LICENSE](LICENSE)
