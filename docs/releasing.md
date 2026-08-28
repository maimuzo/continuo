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

**この8つを通してから打つ。**どれか1つでも落ちていたら、タグを打ってはならない。

| 何を | どう確かめるか |
| --- | --- |
| **CI が緑である** | `gh run list --branch main --limit 1` |
| **CI と同じ状況で手元も通る** | `sh scripts/test-like-ci.sh` |
| **仕様とテストの連鎖が揃っている** | `sh scripts/check-rucm.sh --strict` |
| **実機で issue を1件通した** | [docs/trying_it_out.md](trying_it_out.md) の手順 |
| **`docs/upgrading.md` にこの版の節がある** | 下の「5. 版ごとの節を書く」 |
| **`docs/FAQ.md` に新しい設定と見出し語がある** | 上の「絶対条件」の数え方 |
| **PR にレビュー結果が貼ってある** | `sh scripts/check-release-ready.sh` |
| **対の issue に説明が書いてある** | 同上 |

**実機で issue を1件通すのがいちばん重い。**mock だけで通しても、実機で初めて出る欠陥がある。
実際、`interactive_ready` を見ていなかった欠陥は、テストが全部通っている状態で残っていた。
**その手順は [docs/trying_it_out.md](trying_it_out.md) の段7〜段9 である。**
**ここから利用の枠を消費するので、人間の判断で行う。**

```bash
# continuo リポジトリの root で実行する
git fetch origin
gh run list --branch main --limit 1 --json headSha,status,conclusion \
  --jq '.[] | "\(.headSha[0:7]) \(.status)/\(.conclusion)"'
sh scripts/test-like-ci.sh
sh scripts/check-rucm.sh --strict
sh scripts/check-release-ready.sh
```

**`completed/success` でなければ止まる。**

**タグを打たずに CI を試せる。**

```bash
gh workflow run release.yml --ref main
```

**test と build までが走り、release は作られない**（publish はタグのときだけ動く）。

## 2. PR とその issue の検査の結果を読む

**[scripts/check-release-ready.sh](../scripts/check-release-ready.sh) は、
マージ済みの PR と、それが閉じた issue を並べる。**

```
起点: v0.1.8 → origin/main
レビューの規則が入った c9f4a50 以降の PR だけを見る

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

**レビュー結果の目印は `<!-- code-review-result -->` である。**
**貼るときは、この行をコメントの先頭に置く。**本文に `code-review` と書いただけのコメントは数えない。
それを数えると、「レビューの話をしただけ」の PR が通ってしまう。

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
git diff "$prev"..origin/main -- internal/config/validate.go \
  | grep '^+' | grep -E "invalidValueError|requiredValueError"
```

| 何を見ているか | 何が分かるか |
| --- | --- |
| **雛形が変わったか** | 新しく `continuo init` する人と、既に設定を持つ人とで、見えるものが違ってくる |
| **弾く検査が増えたか** | **既存の設定が起動しなくなる。**利用者に必ず伝える必要がある |

**設定を弾くのは `invalidValueError` と `requiredValueError` の2つだけである**
（[internal/config/validate.go](../internal/config/validate.go)）。
**`required` という語では当たらない。**弾く条件の側にその語が出ないためである。

## 4. 版を決める

**このリポジトリは `v0.1.x` である。**

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
| **そのまま貼れる yaml** | **雛形の値のままで起動すること**を、手元で1度確かめてから載せる |
| **足したかどうかの確かめ方** | `continuo doctor` のどの行が何と出れば足せているか |

**1つも増えていなければ、「増えたキーはありません」とだけ書く。**節そのものは作る。
**「何も無い」と書いてあることが、読む人には要る。**

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

**個人の絶対パス・トークン・実在のリポジトリ名を書かない。**例は `<owner>/<repo>` を使う。

**置き場所は `${TMPDIR:-/tmp}/continuo-release-<新しい版>.md` とする**
（例: `/tmp/continuo-release-v0.1.9.md`）。**「9. リリースノートを差し替える」がこのパスを渡す。**
**リポジトリの中に置かない。**正はリリースの本文であり、同じ文章を2箇所に持つと片方が古くなる。

## 7. タグを打つ

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 「4. 版を決める」で決めた版に書き換える
git checkout main
git pull --ff-only
git tag "$next"
git push origin "$next"
```

**タグは main の先頭に打つ。**CI はタグが指す commit をビルドするので、
別の commit に打つと、その中身が配られる。

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

## 10. 入るかを確かめる

**release ができたら、実際に入れてみる。**

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/continuo/main/install.sh \
  | sh -s -- --no-deps --dir /tmp/continuo-check
/tmp/continuo-check/continuo version
```

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
pgrep -fl continuo
```

**止めて起動し直すのは人間の判断である。**勝手に止めない。

## 失敗したとき

**CI が落ちたら、release は作られない。**タグだけが残る。

```bash
git tag -d v0.1.0                    # 手元のタグを消す
git push origin :refs/tags/v0.1.0    # リモートのタグを消す
```

**直してから同じタグを打ち直してよい。**release がまだ無いので、誰も掴んでいない。

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

## 打つ前に落ちるようにしてあること

**タグを打ってから気づくのを避けるため、CI の test の段で先に落とす。**

| 何 | なぜ先に見るか |
| --- | --- |
| `LICENSE` / `README.md` / `README.ja.md` が git に入っているか | **build の段で `cp` する。**追跡されていないと4つ全部が落ちる |
| `install.sh` が sh と dash で走るか | 利用者が最初に叩くものである |
| 変数の直後に全角文字が続いていないか | `set -u` の下で落ちるが、**dash では起きないので構文検査では出ない** |
| 4つの組み合わせがビルドできるか | 手元が macOS でも、Linux 向けが通るとは限らない |
| `continuo version` が版を答えるか | **`-X` の左辺が誤っていても Go は警告を出さない** |
