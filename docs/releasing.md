# リリースの作り方

**言いたいこと。**タグを打つのは人間である。**タグを push した瞬間に CI が動き、release ができる。**
**この文書がリリースの手順の正である。**上から順に行い、1つも飛ばさない。

---

## 絶対条件：文書を直してから出す

**リリースノートに書いただけでは足りない。**
**ノートは1回きりで、あとから困った人が引けない。**

**新しい設定・新しい `doctor` の見出し語・変わった振る舞いは、次の2つに入れてから出す。**

| 文書 | 何を書くか |
| --- | --- |
| [docs/FAQ.md](FAQ.md) | **症状 → 原因 → 直し方。**症状から引く場所である |
| [docs/upgrading.md](upgrading.md) | **`WORKFLOW.md` へ何を足すか・足さないと何が起きるか・確かめ方。**版から引く場所である。書き方は「5. 版ごとの節を書く」 |

**確かめ方。**次の出力に `0` が1つでもあれば、まだ出してはいけない。

```bash
# continuo リポジトリの root で実行する
for k in "<新しい設定キー>" "<新しい doctor の見出し語>"; do
  for f in docs/FAQ.md docs/upgrading.md; do
    n=$(grep -c -- "$k" "$f" 2>/dev/null)
    printf '%s\t%s\t%s\n' "$k" "$f" "${n:-0}"
  done
done
```

**[README.md](../README.md) と [README.ja.md](../README.ja.md) は数えない。**
README に1行あるだけで通ってしまうと、**症状から引ける場所と版から引ける場所が空のまま出る。**

**雛形（[internal/scaffold/template.go](../internal/scaffold/template.go)）にしか書いていない設定は、
`continuo init` を新しく走らせた人にしか届かない。**既に設定を持っている人には届かない。

---

## 1. 打つ前に確かめる

**この9つを通してから打つ。**どれか1つでも落ちていたら、タグを打ってはならない。

| 何を | どう確かめるか |
| --- | --- |
| **CI が緑である** | `gh run list --workflow ci.yml --branch main --limit 1` |
| **CI と同じ状況で手元も通る** | `sh scripts/test-like-ci.sh` |
| **仕様とテストの連鎖が揃っている** | `sh scripts/check-rucm.sh --strict` |
| **実機で issue を1件通した** | 下の「実機で issue を1件通す」 |
| **`docs/upgrading.md` にこの版の節がある** | 下の「5. 版ごとの節を書く」 |
| **`docs/FAQ.md` に新しい設定と見出し語がある** | 上の「絶対条件」の数え方 |
| **PR にレビュー結果が貼ってある** | `sh scripts/check-release-ready.sh` |
| **対の issue に説明が書いてある** | 同上 |
| **管理者にも検査を課す設定が有効である** | 同上。**外れていると、レビュー結果を貼ってあってもマージを止められない** |

**実機で issue を1件通すのがいちばん重い。**mock だけで通しても、実機で初めて出る欠陥がある。
実際、`interactive_ready` を見ていなかった欠陥は、テストが全部通っている状態で残っていた。
**やり方は下の「実機で issue を1件通す」にある。**

```bash
# continuo リポジトリの root で実行する
git fetch origin
REL_SHA=$(git rev-parse origin/main)    # この commit を確かめ、この commit にタグを打つ
echo "REL_SHA=$REL_SHA"                 # 控える（「7. タグを打つ」で使う）
git rev-parse HEAD                      # REL_SHA と同じであること。違えば、その commit を checkout してから下を叩く
# **`--workflow` で絞る。**絞らないと、直前に走った release の run を掴んで、
# ci が赤でも `success` が返る。
gh run list --workflow ci.yml --branch main --limit 1 --json headSha,status,conclusion \
  --jq '.[] | "\(.headSha) \(.status)/\(.conclusion)"'
sh scripts/test-like-ci.sh
sh scripts/check-rucm.sh --strict
sh scripts/check-release-ready.sh
```

**CI の行の SHA が `REL_SHA` と同じで、`completed/success` でなければ止まる。**
`test-like-ci.sh` と `check-rucm.sh` は、いまいるディレクトリのファイルを測るので、`REL_SHA` を checkout した場所で叩く。

**タグを打たずに CI を試せる。**

```bash
gh workflow run release.yml --ref main
```

**test と build までが走り、release は作られない**（publish はタグのときだけ動く）。

### 実機で issue を1件通す

**言いたいこと。**動いている continuo は止めなくてよい。
**テスト用の設定ファイルで worktree と socket を分け、`--id <名前>` を付けて起動すれば、
2つ目の continuo を並べて動かせる。**
**AI だけで最後まで回せる。**人間の判断を待つ段ではない。

**ただし Claude Code が実際に動くので、定額プランの枠を消費する。**続けて何度も回さない。
**本番のカンバン（project #3）には触れない。**使うのは検証用のカンバンだけである。
**その番号・リポジトリ・issue・Status の識別子は [docs/test_environment.md](test_environment.md) にある。**

**出そうとしている版で回す。**下の段三〜段五と片付けの `continuo` を PATH の名前のまま叩くと、
**手元に入っている前の版を確かめることになる。**出そうとしている commit を一時の worktree へ取り出してビルドし、
**段三〜段五と片付けの `continuo` を、すべてそのパスへ置き換えて叩く。**

```bash
# continuo リポジトリの root で実行する
git fetch origin
SHA=$(git rev-parse origin/main)          # 出そうとしている commit
TMP=$(mktemp -d)
git worktree add --detach "$TMP/src" "$SHA"
go build -C "$TMP/src" \
  -ldflags "-X github.com/maimuzo/continuo/internal/cli.version=e2e-$(git rev-parse --short "$SHA")" \
  -o "$TMP/bin/continuo" ./cmd/continuo
"$TMP/bin/continuo" version              # e2e-<短い SHA> が返ること
echo "SHA=$SHA BIN=$TMP/bin/continuo SRC=$TMP/src"    # この3つを控える
```

**表示した `BIN=` の絶対パスを控え、段三〜段五と片付けでは `continuo` をその絶対パスへ書き換えて叩く。**
段三以降は別のシェルで叩くことがあり、そこでは `$TMP` も `$SHA` も空になる。空のまま `"$TMP/bin/continuo"` を叩くと
`/bin/continuo` になって見つからない。`continuo` と名前だけで叩くと、PATH の前の版になる。
控えた `SHA=` は、「7. タグを打つ」で「実機の確認を回した commit」として比べるのに使う。

**`-ldflags` を落とさない。**付けないと `version` は `dev` しか返さず、どの commit を確かめているのかが分からない。
**`~/.local/bin` へは置かない。**動いている continuo は、走っている run の hook と statusline に
そのパスを書いている。release の前に差し替えると、確かめていない実行ファイルを走っている run が exec する。

**取り出した worktree の中でスクリプト（`sh scripts/test-like-ci.sh` など）を叩くなら、mise を使っている人は先に信頼する**
（`(cd "$TMP/src" && mise trust)`。CONTRIBUTING.md と同じ前提）。上の `go build -C` は root を cwd にして叩くので、信頼しなくても通る。

**片付けは、[docs/test_environment.md](test_environment.md) の「終わったあとの片付け」で検証用の continuo を止め、
`abandon --id e2e` を済ませてから、控えた `SRC=` のパスを `git worktree remove <SRC>` で片付ける。**
**`$TMP/bin` は消さない。**検証用の run の hook と statusline は控えた `BIN=` を exec するので、消すと、
残った pane の hook が無いパスを叩く。一時のディレクトリの下にあるので、残しておいてかまわない。

**一、テスト用の `WORKFLOW.md` で、隔離する設定を2つ変える。**

**本番とは別のディレクトリに `WORKFLOW.md` を1枚置き、そちらだけを書き換える。**
**本番の `WORKFLOW.md` には触れない。**

```yaml
workspace:
  root: ~/continuo-e2e-worktrees            # worktree の置き場所を本番と分ける
claude:
  hook_bridge:
    listen: /tmp/continuo-e2e/hooks.sock    # socket を分ける
```

**二重起動を止めるロックは、この2つでは分かれない。**
**`~/.continuo/continuo.lock` の1本に機械で固定されているので、`--id <名前>` で分ける**
（下の段四）。**`runtime` の節は無くなった。**書いてあると front matter の検査で弾かれる。

**二、socket を置くディレクトリの権限を 0700 にする。**

```bash
mkdir -p /tmp/continuo-e2e ~/continuo-e2e-worktrees
chmod 0700 /tmp/continuo-e2e
```

**忘れると `doctor` がここで落ちる。**

```
✗ hook の置き場所 hook を受ける socket を用意できません: 既にある hook を受ける socket の
                  ディレクトリ /tmp/continuo-e2e の権限が 0755 です。0700 にしてから起動してください
                  （continuo は自分が作っていないディレクトリの権限を書き換えません）
```

**パスは短くする。**macOS の Unix domain socket は104バイト以上で bind に失敗するので、
**深いディレクトリを指すと、権限とは別の理由でここが `✗` になる。**

**三、検証用のカンバンで設定を作り、検査を通す。**

```bash
# どこで実行してもよい
OWNER="$(gh repo view --json owner --jq .owner.login)"
WORK=~/continuo-e2e-work        # 置き場所は好きにしてよい。本番の作業ディレクトリと分けること
mkdir -p "$WORK"
continuo init --project 10 --owner "$OWNER" --force "$WORK"
# ここで、一の2つの設定を WORKFLOW.md へ書き込む
continuo doctor "$WORK"
```

**`✗` が0件になること。**
**カンバンに着手待ちの issue が無いうちは、`clone` と `信頼登録` が `!` のまま残る。**それでよい。

**四、`--id <名前>` を付けて起動し、issue を1件通す。**

```bash
continuo --id e2e "$WORK"
```

**`--id` を落とすと、本番の continuo が握っている `~/.continuo/continuo.lock` に当たり、
「二重起動を検出しました」で起動できない。**

**`--id` が分けるのは、二重起動を止めるロック1本だけである。**

| 分ける対象 | どうやって分けるか |
| --- | --- |
| **ロック** | **`--id e2e` を付ける。**`~/.continuo/id/e2e/continuo.lock` になる |
| **worktree の置き場所** | **テスト用の `WORKFLOW.md` の `workspace.root`**（段一） |
| **socket** | **テスト用の `WORKFLOW.md` の `claude.hook_bridge.listen`**（段一） |

**名前に書けるのは、小文字の英数字とハイフンだけである。**先頭は英数字、32文字まで。
**大文字・空白・`..`・`/` は起動する前に弾かれる。**

**段一の2つを分け忘れても、continuo は止めない。**`--id` が分けるのはロックだけで、
**分け忘れは検知できない。****分け忘れると、2本が同じ worktree に Claude Code を二重に立て、
片方の成果が黙って消える。****さらに、2本目が1本目の worktree を「自分の前の run のもの」と見て、
走行中の pane を巡回のたびに閉じる**（既定30秒ごと）。

**着手待ちの issue が拾われ、`In Progress` になり、Claude Code が起動する。**
**前の回の作業が残っているときは、着手待ちへ動かす前に片付ける。**残っていると、エージェントは前の回の続きとして扱い、新しく確かめずに終えることがある。次の順で行う。

1. 前の回の pull request が開いていれば閉じる（マージはしない）
2. 手元の worktree と branch を、控えた絶対パスの `abandon --id e2e` で片付ける
3. そのあとで、その branch を origin から消す（[docs/test_environment.md](test_environment.md) の「終わったあとの片付け」の `push origin --delete` の1行）。
   **先に消すと、手元のリモート追跡 ref も消えて、2 の `abandon` が「失うものがある」と見て止まる**
4. **検証用の issue に**、前の回の作業を片付けたので新しく作業してほしい、とコメントする（エージェントは閉じた pull request のコメントも読むので、いちばん新しい指示として issue に書く）

**エージェントは計画を書くと、了承を訊いて `Blocked` で止まる。**計画を読み、検証用の issue に了承のコメントを書いてから、Status を着手待ちへ戻す。
レビューの途中で質問して、また止まることもある。そのときは回答を書いて戻す。
**了承と回答のコメントの先頭に、AI の印（`<!-- continuo:` で始まる HTML コメント）を付けない。**エージェントは印の付いたコメントを人間の指示として数えないので、また止まる。
**`In Review` になれば成功である。**
**issue を着手待ちへ動かす手順と、進み方の見方は [docs/test_environment.md](test_environment.md) にある。**
**各段が何をしているのかは [docs/trying_it_out.md](trying_it_out.md) の段7〜段9 に書いてある。**

**本番の continuo は動いたままでよい。**socket もロックも worktree も分かれているので、互いに触らない。

**片付けるときは、`continuo abandon` にも同じ名前を渡すこと。**渡さないと、空いている既定のロックを見て
「continuo は動いていません」と判定し、**生きている worktree を消しにいく。**

```bash
continuo abandon --id e2e <issue の URL> "$WORK"
```

**五、詰まりやすいところ。**前の検証の残り物で止まることがある。

```
level=WARN msg="目的の worktree をそのまま使えません（Status を書かずにこの issue を飛ばします）"
  error="目的の branch を別の場所の worktree が使っています…
  【対処】continuo abandon <issue の URL> を実行してください。"
```

**案内に出たコマンドへ `--id e2e` を足して叩く**（`continuo abandon --id e2e <issue の URL> <ディレクトリ>`）。
**案内の文面には `--id` が入らない。**足さないと、空いている既定のロックを見て
「continuo は動いていません」と判定する。
**それでも消えないなら、git の登録だけが残っている。**

```bash
# どこで実行してもよい。<リポジトリ> は clone してある場所
git -C <リポジトリ> worktree remove <残っている worktree>
git -C <リポジトリ> worktree prune
git -C <リポジトリ> branch -D <残っている branch>
```

**どこが branch を掴んでいるかは `git -C <リポジトリ> worktree list` で並ぶ。**
**片付け方の全体は [docs/test_environment.md](test_environment.md) にある。**

## 2. PR とその issue の検査の結果を読む

**[scripts/check-release-ready.sh](../scripts/check-release-ready.sh) は、
マージ済みの PR と、それが閉じた issue を並べる。**

```
起点: v0.1.8 → origin/main
レビューの規則が入った c9f4a50 以降の PR だけを見る

管理者にも検査を課す設定 (enforce_admins) = 有効
PR #71  レビュー結果=有り（1件）
          issue #65 CLOSED 説明=有り
PR #69  レビュー結果=有り（1件）
          対の issue=無し（issue から生まれた PR ではない）

直すもの 0件。マージした PR とその issue は揃っています。
```

| 出たもの | どうするか |
| --- | --- |
| **`レビュー結果=無し`** | **`/code-review` を回し直し、結果をその PR のコメントへ貼る。**貼っていないものは、レビューを実施していないものとして扱う（[CLAUDE.md](../CLAUDE.md) の「PR を出すときの絶対条件」） |
| **`説明=無し`** / `説明=無し（閉じる前のコメントだけ）` | **その issue へ説明を書く**（下の表のとおり） |
| **`説明=閉じていない`** | 直したのに開いたままなら閉じる。**まだ直っていないなら、リリースノートに書かない** |
| **`対の issue=無し`** | **異常ではない。**issue から生まれない PR はある |
| **`enforce_admins) = **無効**`** | **[CONTRIBUTING.md](../CONTRIBUTING.md) の「管理者にも検査を課す」を実施する。**外れている間は、管理者が赤いままマージできる |
| **`enforce_admins) = 読めませんでした`** | **管理権限が要る。**branch protection そのものが無いときも読めない。**読めないまま通すと、外れていても気づけない** |

**レビュー結果の目印は `<!-- code-review-result -->` である。数える条件は2つある。**

| 条件 | なぜ |
| --- | --- |
| **目印がコメントの本文の先頭にある**（前に空白文字があってもよい） | 途中に書いたものまで数えると、**「レビューの話をしただけ」の PR が通ってしまう** |
| **投稿者が `OWNER` / `MEMBER` / `COLLABORATOR` のいずれかである** | **誰でもコメントできる。**外部の人が目印を貼れば通る状態にしない |

**同じ条件で、CI も PR を落とす。**[.github/workflows/review-gate.yml](../.github/workflows/review-gate.yml) が
`pull_request` のたびに走り、**貼られていなければ `code-review-result` の検査が赤になる。**

**必須の検査に入っている**（2026-09-21 に確認）。**赤いあいだはマージできない。**

> **この job は `review-result` から `code-review-result` へ改名し、必須の検査の入れ替えも済んでいる。**
> **fork して新しく設定する人は、[CONTRIBUTING.md](../CONTRIBUTING.md) の「この検査をマージの条件にする」を通ること。**

```
$ gh api repos/<owner>/continuo/branches/main/protection/required_status_checks --jq '.checks[].context'
test (ubuntu-latest)
test (macos-latest)
build (darwin, arm64)
build (darwin, amd64)
build (linux, amd64)
build (linux, arm64)
code-review-result
design-review-result
```

**8本である。**`gh api` の出力がそのまま必須の検査の全部で、他に隠れているものは無い。

**それでも、ここでもう一度数える。**必須の検査は**その PR がマージされる前**しか見ない。
**タグを打つ時点で見ているのは、既に main へ入ったあとの PR である。**
**必須の検査に入る前にマージされたものは、CI では拾えない。**
（**入れ直す手順**は [CONTRIBUTING.md](../CONTRIBUTING.md) の「この検査をマージの条件にする」にある。
`checks` は全件置き換えなので、いまの分を読んでから足すこと。）

**条件は2箇所で同じにしてある。**片方だけ緩いと、緩いほうが実質の規則になる。

| どこ | 何を止めるか |
| --- | --- |
| [.github/workflows/review-gate.yml](../.github/workflows/review-gate.yml) | PR のマージ |
| [scripts/check-release-ready.sh](../scripts/check-release-ready.sh) | タグを打つこと |

**AI の手元で `gh pr merge` / `gh pr ready` を止める hook も在ったが、2026-09-21 に廃止した。**
**branch の保護設定で `enforce_admins` を有効にし、admin も `code-review-result` を素通りできなくしたためである。**

**「前の空白文字」に何を含めるかも、2箇所で同じにしてある。**
**半角空白・タブ・CR・LF の4つだけである**（`[ \t\r\n]*`）。全角空白 U+3000 や NBSP U+00A0 は含めない。
`\s` は使わない。**engine によって当たる範囲が違う**ので、どちらに寄せてももう一方とずれる
（実測: 2026-09-02。全角空白を前に置いたコメントを、jq は数え、当時あった Python の実装は数えなかった。
**その Python の実装は 2026-09-21 に廃止したので、いまはどちらも jq である**）。
**2箇所が同じであることは
[.claude/hooks/tests/test_marker_pattern_parity.py](../.claude/hooks/tests/test_marker_pattern_parity.py)
が CI で押さえる。**

**対の issue は、`Closes` / `Fixes` / `Resolves` の後ろの `#N` だけを拾う。**
本文にただ出てくる `#N` は拾わない。「足すのは issue #53 で扱う」のような参照まで数えてしまうためである。
**1つの PR に複数あってよい。**検査は全部並べる。

**規則が入る前の PR は見ない。**「PR を出すときは code-review を通す」は `c9f4a50` で main に入った。
それ以前の PR は、いま直しようがない。**全部見たいときは第2引数に空文字を渡す。**

**issue へ書く内容。**

| 何を | 中身 |
| --- | --- |
| **何が起きていたか** | 症状。報告者の言葉に寄せる |
| **どう直したか** | 実際のコード片や出力を添える |
| **振る舞いが変わったところ** | **設定を変えなくても動きが変わる箇所は、必ず書く** |
| **直していないもの** | 別 issue に切り出したなら、その番号 |

## 3. 前の版からの差分を読む

**設定のキーが増えたかは「5. 版ごとの節を書く」で調べる。**ここで見るのは別の2つである。

```bash
# continuo リポジトリの root で実行する
prev=$(git describe --tags --abbrev=0 origin/main)   # 前の版
git log --oneline "$prev"..origin/main
git diff --stat "$prev"..origin/main -- internal/scaffold/template.go
git diff "$prev"..origin/main -- internal/config/ \
  | grep '^+' | grep -E "invalidValueError|requiredValueError|i18n\.Errorf"
```

| 何を見ているか | 何が分かるか |
| --- | --- |
| **雛形が変わったか** | 新しく `continuo init` する人と、既に設定を持つ人とで、見えるものが違ってくる |
| **弾く検査が増えたか** | **既存の設定が起動しなくなる。**利用者に必ず伝える必要がある |

**設定を弾く検査は、`invalidValueError` か `requiredValueError` か `i18n.Errorf` のどれかで誤りを返す。**
**[internal/config/validate.go](../internal/config/validate.go) の外にも在る**ので、`internal/config/` の全体を見る。
v0.2.1 では、`claude.env` の `GH_HOST` の食い違いで起動を断る検査が
[internal/config/host.go](../internal/config/host.go) に入り、`validate.go` だけを見るコマンドは0行を返した（実測: 2026-10-03）。
`internal/config/` の中で誤りを返しているのは、この3つの語だけである（同じ日の実測で、`fmt.Errorf` と `errors.New` は0件）。
**探す語を `required` の1語へ縮めないこと。**必須かどうかを見る検査にしか当たらず、値の形で弾く検査を全部落とす。

**出た行を1行ずつ読み、既存の設定が起動しなくなるものかを判定する。**新しいキーの値の形を見るだけの検査も、同じ語で当たる。

**0行でも、「起動しなくなる変更は無い」とは言えない。**このコマンドが拾わないものが4つある。

| 拾わないもの | 例 |
| --- | --- |
| **弾く条件だけを狭めた変更。**誤りを返す行は変わらないので、`^+` の行に出ない | v0.2.0 で、マージンに `100` を書けなくなった。差分は `> 100` を `>= 100` へ変えた2行（キー2つ）で、このコマンドには出ない（`v0.1.15..v0.2.0` へ当てて実測: 2026-10-03） |
| **`internal/config/` の外で、設定の値を見て起動を断る検査** | `herdr.protocol` の照合（`internal/herdr/`）、起動時の検査（`internal/daemon/checks.go`） |
| **設定を変えなくても動きが変わる箇所** | v0.2.1 の clone の引き方の変更（`internal/workspace/git.go`） |
| **キーを消した・名前を変えた変更。**設定の読み込みは知らないキーを誤りにするので、そのキーを書いている設定は起動しなくなる。誤りを返す行は増えないので、このコマンドには出ない | v0.2.0 で `rate_limit.pause_above_percent` が無くなった |

**上の3つは、上の `git log` に並んだ pull request の本文と、対の issue の説明から拾う。**
**4つ目は、「5. 版ごとの節を書く」の `diff` が拾う。**増えたキーだけでなく、消えたキーも出る。

## 4. 版を決める

**このリポジトリは `v0.x.y` である。**v0.1.15 の次は、破壊的変更が重なったので人間が v0.2.0 と決めた。

| 何が入ったか | どう上げるか |
| --- | --- |
| 直しと、省略できる設定の追加だけ | **末尾を1つ上げる** |
| **利用者の設定を書き換えないと動かない変更** | **人間に確認する。**勝手に上げない |

## 5. 版ごとの節を書く

**[docs/upgrading.md](upgrading.md) は版ごとに積む文書である。**打ってから書くと、
**その版を入れた人が、何を足せばよいか分からないまま最初の起動を迎える。**

**利用者は `WORKFLOW.md` を作り直せない。**`continuo init --force` は `continuo setup` で
決めた Status の割り当てと、下半分のプロンプトを雛形で潰す。**だから、足す行はこちらが書いて渡すしかない。**

**設定のキーの増減は機械で調べる。**

```bash
# continuo リポジトリの root で実行する
prev=$(git fetch origin --tags -q; git describe --tags --abbrev=0 origin/main)
diff <(git show "$prev":internal/config/types.go     | grep -o 'yaml:"[^"]*"' | sort -u) \
     <(git show origin/main:internal/config/types.go | grep -o 'yaml:"[^"]*"' | sort -u)
```

**`main` ではなく `origin/main` を見る。**手元の `main` は取り込んでいないことがあり、
**そのときは「増えたキーは無い」と嘘の答えが返る**（実測で踏んだ）。

**節に置くのは4つである。**

| 何を書くか | 例 |
| --- | --- |
| **増えたキー・消えたキー・改名したキー** | 「増えたのは `tracker.automated_state_rewrite` の1つだけ」 |
| **書かないと何が起きるか** | 「壊れない。いままでどおり猶予を置いて止まるだけ」 |
| **書き足す場所の図と、その見つけ方** | **`grep` で場所を出させ、yaml には `# 既にある行` / `# ← これを足す` の注記を付ける。****「そのまま貼れる yaml」と書かない。**`continuo init` の雛形には親キーが既にあるので、塊ごと貼ると front matter が重複キーになって起動しなくなる（issue #209） |
| **足したかどうかの確かめ方** | `continuo doctor` のどの行が何と出れば足せているか |

**1つも増えていなければ、「増えたキーはありません」とだけ書く。**節そのものは作る。
**「何も無い」と書いてあることが、読む人には要る。**

**実行ファイルを入れ替える1行を書くときは、`--version vX.Y.Z` を付ける**（`vX.Y.Z` はその節の版）。
**インストーラーは既定で、公開から 168 時間（7日）経った版だけを入れる**（クールダウン。設計 3-36）。
数えるのは release の最終更新の時刻（更新フィードの `<updated>`）で、公開より少し遅い側にずれる。
**付けないと、出してから 168 時間のあいだは1つ前の版が入り、**節のとおりに直した `WORKFLOW.md` を古い実行ファイルが読めないことがある。
[docs/FAQ.md](FAQ.md) の版ごとの節に書く1行も同じである。

```bash
curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh -s -- --version vX.Y.Z
```

## 6. リリースノートを書く

**利用者のための文書であって、作業の報告ではない。**

| 書く | 書かない |
| --- | --- |
| **何ができるようになったか** | 内部の作業量・手法 |
| **何をすれば使えるか**（設定の例） | PR 番号・issue 番号の羅列 |
| **振る舞いが変わったところ**（見出しを分ける） | 未修正の件数 |
| **入れ方** | 「直しました」という報告調 |

**振る舞いが変わったところは、必ず独立した見出しで書く。**
**設定を変えなくても動きが変わる箇所は、利用者が踏むまで気づけない。**

**破壊的変更があるときは、見出し `## 破壊的変更` の直下に、箇条書きで書く。**

**インストーラーは release の更新フィード（`https://github.com/<owner>/<repo>/releases.atom`）の本文から、
この見出しの下の箇条書きを読み、上げようとしている利用者に名指しで警告する**
（設計 3-36 の「破壊的変更のある版へ上げるときは、入れたうえで警告する」）。
**認証なしの GitHub の API は呼ばない。**更新フィードでは HTML のコメントが消えるので、以前の印（`<!-- breaking:start -->` と `<!-- breaking:end -->`）は読まない。**印は要らない。過去の本文は触らない。**

````markdown
## 破壊的変更

- `WORKFLOW.md` の `tracker.dispatch_state` が必須になりました。無いと起動しません
- `claude.model` の既定が `sonnet` から `opus` に変わりました
````

| 守ること | なぜ |
| --- | --- |
| **見出しは `## 破壊的変更` ちょうどにする**（後ろに言葉を足さない。英語なら `## Breaking changes`） | 見出しの文字を完全一致で探す。`## 破壊的変更（設定）` のように足すと、**その版の破壊的変更は黙って「なし」になる**。ほかの言語の見出しは読まない |
| **見出しの直下に、段落を挟まず箇条書きで書く**（空の行は挟んでよい） | 見出しの直後が箇条書きでないと、インストーラーは「確かめられなかった」と出す |
| **1件を1行の箇条書きで書く。折り返さない** | **その1件がそのまま利用者の画面に1行で出る** |
| **箇条書きを入れ子にしない** | 親の文に子の文が混ざるので、インストーラーは「確かめられなかった」と出す |
| **箇条書きのあとは、次の見出しまで何も置かない** | 読むのは見出しの直下の最初の箇条書きだけである。あとに足した文は読まれない |
| **破壊的変更が無い版には、この見出しを置かない** | 見出しが無ければ「なし」である。空の見出しは「確かめられなかった」になる |

**更新フィードには新しい10件しか載らない。**それより古い版から上げる人には、載っている範囲の破壊的変更を出したうえで、
「古すぎるため、ほかにも破壊的変更がある可能性があります」と releases の画面を案内する。**release を出すたびに、載る範囲は進む。**
また、古い系列へ修正版を出したあとは、いま入っている版が更新フィードに在っても、あいだの版が押し出されていることがあり、インストーラーにはそれが分からない（設計 3-36 の「限界」）。

**試験版（rc）は、いまは出さない。**インストーラーは版を比べるとき `-` から後ろを落とすので、`v2.98.0-rc1` から `v2.98.0` へ上げた人に警告が出ない。
**rc を出すなら、先に install.sh の `vcmp` を直すこと**（設計 3-36 の「版の比べ方」）。
インストーラーがタグの名前に `-` を含む版を試験版とみなして選ばないのは、fork が試験版を出すときのためである。

**個人の絶対パス・トークン・実在のリポジトリ名を書かない。**例は `<owner>/<repo>` を使う。

**置き場所は `${TMPDIR:-/tmp}/continuo-release-<新しい版>.md` とする**
（例: `/tmp/continuo-release-v0.1.9.md`）。**「9. リリースノートを差し替える」がこのパスを渡す。**
**リポジトリの中に置かない。**正はリリースの本文であり、同じ文章を2箇所に持つと片方が古くなる。

## 7. タグを打つ

```bash
# continuo リポジトリの root で実行する
next=v0.1.9                 # 「4. 版を決める」で決めた版に書き換える
REL_SHA=                    # 「1.」で控えた full の SHA を = の右に書く
git fetch origin
if [ -z "$REL_SHA" ]; then
  echo "REL_SHA が空です。「1.」で控えた full の SHA を書いてください"
elif [ "$(git rev-parse origin/main)" != "$REL_SHA" ]; then
  echo "origin/main が確かめた commit から進んでいます。打たずに、新しい commit で「1.」を回し直してください"
else
  git tag "$next" "$REL_SHA" && git rev-parse "$next" && git push origin "refs/tags/$next"
fi
```

**タグは main の先頭、それも「1.」で確かめた commit に打つ。**CI はタグが指す commit をビルドするので、
別の commit に打つと、その中身が配られる。**打つ時点の `origin/main` や手元の `main` から取り直さない。**
あいだに別の pull request が入ると、実機の確認も `check-release-ready.sh` も通っていない commit にタグを打つことになる。
手元の `main` は古いことがあり、別の worktree が checkout していると `git checkout main` も断られるので、SHA を名指しして打つ。

**`origin/main` が進んでいたら、新しい commit で「1.」を回し直す。**ただし、「実機で issue を1件通す」で控えた commit との差が
`docs/` の下だけなら（`git diff --name-only <実機で控えた SHA> <新しい commit>`）、実機の確認だけは回し直さなくてよい。

## 8. CI が作るもの

`.github/workflows/release.yml` が動き、次を release へ載せる。

```text
continuo_darwin_arm64.tar.gz   （macOS / Apple Silicon）
continuo_darwin_amd64.tar.gz   （macOS / Intel）
continuo_linux_amd64.tar.gz    （Linux / x86-64）
continuo_linux_arm64.tar.gz    （Linux / arm64）
checksums.txt                  （install.sh が照合する）
```

**それぞれの書庫には、実行ファイルと `LICENSE` と README 2つが入る。**
配布物だけを受け取った人が読めるようにするためである。

**provenance も作る。**「どの workflow の、どの commit から作られたか」を GitHub が証明する。
**release の資産としては載らない。**GitHub の attestation として別に記録される。
利用者は次で確かめられる。

```bash
gh attestation verify continuo_darwin_arm64.tar.gz --repo <owner>/continuo
```

**`checksums.txt` は改竄の検知には効かない。**書庫と同じ場所から配るので、
どちらも差し替えられる。**壊れていないことしか分からない。**provenance のほうが強い。

**できるまで待つ。**`gh run list --limit 1` は無関係な ci の run を掴むので、
**workflow とタグで絞る。**

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 打った版に書き換える
run=$(gh run list --workflow release.yml --branch "$next" --limit 1 \
  --json databaseId --jq '.[0].databaseId')
gh run watch "$run" --exit-status
```

**`run` が空なら、まだ run が登録されていない。**少し置いてから叩き直す。

## 9. リリースノートを差し替える

**workflow は `--generate-notes` で作るので、commit の一覧が載る。**
**「6. リリースノートを書く」で書いたもので上書きする。**

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 打った版に書き換える
gh release edit "$next" --notes-file "${TMPDIR:-/tmp}/continuo-release-${next}.md"
```

**差し替えは、公開の直後に済ませる。**インストーラーがクールダウンで数えるのは、release の**最終更新の時刻**である（更新フィードに公開の時刻が無いため）。
**公開のあとにノートを直すと、その版のクールダウンが直した時刻から数え直しになる。**そのあいだは1つ小さい版が選ばれ、
いまその版が入っている人には「版が下がるので入れ替えない」が出る。

**破壊的変更を書いたなら、見出しと箇条書きが本文に入ったかを確かめる。**
**見出しはここで初めて本文に入る。**`--generate-notes` が作った文面には無い。

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 打った版に書き換える
body=$(gh release view "$next" --json body --jq .body | tr -d '\r')
printf '%s\n' "$body" | grep -c '^## 破壊的変更$'    # 1 なら見出しがある（破壊的変更の無い版は 0）
printf '%s\n' "$body" | grep -c '^## 破壊的変更.'    # 0 でなければ、見出しに言葉を足している（直す）
printf '%s\n' "$body" | awk '/^## 破壊的変更$/ { h = 1; next } h && NF { print; exit }'   # 見出しの直下の最初の行
```

**破壊的変更を書いたのに1つ目が `0` なら、インストーラーは1件も警告できない。**
**3つ目は `- ` で始まる箇条書きの1行でなければならない。**段落が出たら、見出しと箇条書きのあいだの文を消す。
書き方は「6. リリースノートを書く」にある。**無いときは1つ目も2つ目も `0` でよい。**

## 10. 入るかを確かめる

**release ができたら、実際に入れてみる。**

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/continuo/main/install.sh \
  | sh -s -- --no-deps --dir /tmp/continuo-check --version <打ったタグ>
/tmp/continuo-check/continuo version
```

**`--version` を落とさない。**インストーラーは既定で公開から 168 時間経った版だけを入れる（クールダウン。設計 3-36）ので、
**付けないと、出した直後の版は見送られ、1つ前の版が入る。**それでは打ったタグを確かめたことにならない。

**打ったタグと同じ版が返れば成功である。**
**`continuo --version` は無い。**`flag provided but not defined: -version` で終了コード 2 になる。

**`continuo doctor` まで通す。**

```bash
# WORKFLOW.md を置いてあるディレクトリを、引数で渡す
/tmp/continuo-check/continuo doctor ~/continuo-work
```

**continuo リポジトリの root で `doctor` を叩かない。**そこに `WORKFLOW.md` は無いので、
**必ず `✗ 設定ファイル` が出る。**引数を省くと、いまいるディレクトリを見る。

**テストが全部通っても、実機で起動するまで見つからない不具合がある。**

## 11. 動いている continuo を入れ替える

**入れ替えは自動では起きない。**動いているものは古いバイナリのままである。

```bash
pgrep -lx continuo
```

**止めて起動し直すのは人間の判断である。**勝手に止めない。

## 失敗したとき

**CI が落ちたら、release は作られない。**タグだけが残る。

```bash
git tag -d v0.1.0                    # 手元のタグを消す
git push origin :refs/tags/v0.1.0    # リモートのタグを消す
```

**直してから同じタグを打ち直してよい。**release がまだ無いので、誰も掴んでいない。

**タグだけが残っていると、release の更新フィードに release の無いタグとして載る。**版を指定しないインストーラーは書庫の在りかを確かめるので選ばないが、
「書庫の無い版」として1行出し続け、`--version` でその版を名指しした人は書庫の 404 で止まる。だから、打ち直さないならタグを消しておく。

**release ができてしまった後に問題が見つかったら、タグは消さない。**
既に入れた人がいるかもしれない。**次の版を出す。**

## やってはいけないこと

| 何 | なぜ |
| --- | --- |
| **実機で1件通さずに出す** | **mock だけでは出ない欠陥がある。**実際に1度そのまま出した |
| **レビュー結果が貼っていない PR を含めて出す** | 貼ってあることが、レビューを実施したことの唯一の証拠である |
| **issue に説明を書かずに出す** | **自動で閉じた issue にはリンクしか残らない。**報告した人に伝わらない |
| **文書を直さずに出す** | ノートは1回きり。あとから困った人が引けない |
| **release ができた後にタグを打ち直す** | 既に取った人と中身が食い違う。**次の版を出す** |
| **CI が赤のまま出す** | 壊れたものを配ることになる |
| **`--generate-notes` のまま放置する** | commit の一覧は利用者に読めない |
| **破壊的変更を見出し `## 破壊的変更` の直下の箇条書きに書かずに出す** | **インストーラーが警告できない。**上げた人は次の起動で初めて気づく |
| **公開のあとに日を置いてリリースノートを直す** | その版のクールダウンが直した時刻から数え直しになり、そのあいだ1つ小さい版が選ばれる |

## 打つ前に落ちるようにしてあること

**タグを打ってから気づくのを避けるため、CI の test の段で先に落とす。**

| 何 | なぜ先に見るか |
| --- | --- |
| `LICENSE` / `README.md` / `README.ja.md` が git に入っているか | **build の段で `cp` する。**追跡されていないと4つ全部が落ちる |
| `install.sh` が sh と dash で走るか | 利用者が最初に叩くものである |
| 変数の直後に全角文字が続いていないか | `set -u` の下で落ちるが、**dash では起きないので構文検査では出ない** |
| 4つの組み合わせがビルドできるか | 手元が macOS でも、Linux 向けが通るとは限らない |
| `continuo version` が版を答えるか | **`-X` の左辺が誤っていても Go は警告を出さない** |
