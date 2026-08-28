# リリースの手順

## この規則と [docs/releasing.md](../../docs/releasing.md) の分担

**言いたいこと。**同じ工程を2つの文書が持つと、緩いほうへ流れる。
**手順の正はこの規則である。**[docs/releasing.md](../../docs/releasing.md) は配布物の仕組みだけを持つ。

| 文書 | 何を持つか |
| --- | --- |
| **この規則** | **通す検査と、飛ばしてはいけない順番。**出す担当が上から読む |
| [docs/releasing.md](../../docs/releasing.md) | **仕組み。**CI が何を作るか、provenance をどう確かめるか、タグを消してよいのはいつか |

**この分担にした理由。**[docs/releasing.md](../../docs/releasing.md) は
[SECURITY.md](../../SECURITY.md) から辿れる公開の文書であり、
**配られたものをどう検証するかを、外部の人が読む場所である。**
検査の手順をそこにも置くと、同じことを2箇所に持つことになる。
**検査はこの規則へ集め、[docs/releasing.md](../../docs/releasing.md) からは外した。**

---

## 絶対条件：文書を直してから出す

**リリースノートに書いただけでは足りない。**
**ノートは1回きりで、あとから困った人が引けない。**

**新しい設定・新しい `doctor` の見出し語・変わった振る舞いは、次の2つに入れてから出す。**

| 文書 | 何を書くか |
| --- | --- |
| [docs/FAQ.md](../../docs/FAQ.md) | **症状 → 原因 → 直し方。**症状から引く場所である |
| [docs/upgrading.md](../../docs/upgrading.md) | **`WORKFLOW.md` へ何を足すか・足さないと何が起きるか・足したあとの確かめ方。**版から引く場所であり、版ごとにここへ積み上げる |

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

**[README.md](../../README.md) と [README.ja.md](../../README.ja.md) は数えない。**
README に1行あるだけで通ってしまうと、**症状から引ける場所と版から引ける場所が空のまま出る。**

**雛形（[internal/scaffold/template.go](../../internal/scaffold/template.go)）にしか書いていない設定は、
`continuo init` を新しく走らせた人にしか届かない。**既に設定を持っている人には届かない。

---

## 手順

**上から順に行う。1つでも飛ばさない。**

### 1. 出す前の6つの検査を通す

**言いたいこと。****「実機で issue を1件通す」がいちばん重い。**
mock だけで通しても、実機で初めて出る欠陥がある。
実際、`interactive_ready` を見ていなかった欠陥は、テストが全部通っている状態で残っていた。

| 短縮名 | 何を見るか |
| --- | --- |
| **main が緑** | 最新の main で CI が通っている |
| **手元も CI と同じ状況で通る** | PATH から claude / herdr / gh を隠し、LANG も外して走らせる |
| **仕様とテストの連鎖** | RUCM → CFG → テストのマーカーが揃っている |
| **実機で issue を1件通す** | 本物のボードと本物の Claude Code で、`Ready` から `Done` まで通す |
| **PR にレビュー結果が貼ってある** | 貼ってあることが、レビューを実施したことの唯一の証拠である |
| **対の issue が閉じていて、説明が書いてある** | 自動で閉じた issue にはリンクしか残らない |

```bash
# continuo リポジトリの root で実行する
git fetch origin
gh run list --branch main --limit 1 --json headSha,status,conclusion \
  --jq '.[] | "\(.headSha[0:7]) \(.status)/\(.conclusion)"'
sh scripts/test-like-ci.sh
sh scripts/check-rucm.sh --strict
sh scripts/check-release-ready.sh
```

**「main が緑」は `completed/success` でなければ止まる。**

**「実機で issue を1件通す」の手順は [docs/trying_it_out.md](../../docs/trying_it_out.md) にある。**
その文書の段7〜段9 を、本物のボードに対して行う。**上の block には入っていない。**
**ここから利用の枠を消費するので、人間の判断で行う。**

**「PR にレビュー結果が貼ってある」と「対の issue」は
[scripts/check-release-ready.sh](../../scripts/check-release-ready.sh) が見る。**
次の段で、その読み方を書く。

### 2. PR とその issue の検査の結果を読む

**言いたいこと。**[scripts/check-release-ready.sh](../../scripts/check-release-ready.sh) は
**直すもの**と**見て判断するもの**を分けて数える。**直すものが残っている間は、タグを打たない。**

```
PR #72  レビュー結果=有り（1件）
          issue #66 CLOSED 説明=有り
PR #56  レビュー結果=無し  ← レビューを回し直し、結果を PR へ貼ること
          対になる issue が本文に出てきません ← 対が無いか、書き忘れかを確かめること

直すもの 7件 ／ 見て判断するもの 3件
```

| 出たもの | どうするか |
| --- | --- |
| **`レビュー結果=無し`** | **`/code-review` を回し直し、結果をその PR のコメントへ貼る。**貼っていないものは、レビューを実施していないものとして扱う（[CLAUDE.md](../../CLAUDE.md) の「PR を出すときの絶対条件」） |
| **`説明=無し`** / `説明=無し（閉じる前のコメントだけ）` | **その issue へ説明を書く**（下の表のとおり） |
| **`説明=閉じていない`** | 直したのに開いたままなら閉じる。**まだ直っていないなら、リリースノートに書かない** |
| **`対になる issue が本文に出てきません`** | **対が本当に無いなら、それでよい。**書き忘れなら、PR の本文へ `Closes #N` を足す |

**レビュー結果の目印は `<!-- code-review-result -->` である。**
検査はこの目印を探す。**貼るときは、この行をコメントの先頭に置く。**
本文に `code-review` と書いただけのコメントは数えない。それを数えると、
「レビューの話をしただけ」の PR が通ってしまう。

**1つの PR に対の issue が複数あることがある。**検査は全部並べる。
**PR 番号は merge commit の題名からだけ拾い、issue 番号は PR の側から拾う。**
commit の本文から `#N` を拾うと、PR 番号と issue 番号が混ざる。

**issue へ書く内容。**

| 何を | 中身 |
| --- | --- |
| **何が起きていたか** | 症状。報告者の言葉に寄せる |
| **どう直したか** | 実際のコード片や出力を添える |
| **振る舞いが変わったところ** | **設定を変えなくても動きが変わる箇所は、必ず書く** |
| **直していないもの** | 別 issue に切り出したなら、その番号 |

### 3. 前の版からの差分を読む

```bash
# continuo リポジトリの root で実行する
prev=$(git describe --tags --abbrev=0 origin/main)   # 前の版
git log --oneline "$prev"..origin/main
git diff --stat "$prev"..origin/main -- internal/scaffold/template.go
```

**雛形が変わっていたら、利用者が `WORKFLOW.md` を更新する必要があるかを判断する。**

| 何 | 更新が要るか |
| --- | --- |
| **キーを足しただけで、省略できる**（既定値がある） | **要らない** |
| キーの名前を変えた・消した | **要る** |
| 検査が厳しくなり、既存の設定が弾かれる | **要る** |

**「要らない」と判断したら、その根拠を確かめる。**

```bash
# continuo リポジトリの root で実行する
prev=$(git describe --tags --abbrev=0 origin/main)
grep -n "<新しいキーの Go の名前>" internal/config/default.go   # 既定値があるか
git diff "$prev"..origin/main -- internal/config/validate.go \
  | grep '^+' | grep -E "invalidValueError|requiredValueError"
```

**設定を弾くのは、この2つの関数だけである**（[internal/config/validate.go](../../internal/config/validate.go)）。
**`required` という語では当たらない。**弾く条件の側にその語が出ないためである。

### 4. 文書を直す

**上の「絶対条件」のとおり。**直し終えてから次へ進む。

### 5. 版を決める

**このリポジトリは `v0.1.x` である。**

| 何が入ったか | どう上げるか |
| --- | --- |
| 直しと、省略できる設定の追加だけ | **末尾を1つ上げる** |
| **利用者の設定を書き換えないと動かない変更** | **人間に確認する。**勝手に上げない |

### 6. リリースノートを書く

**[.claude/rules/reporting.md](reporting.md) と同じ考え方で書く。**
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
（例: `/tmp/continuo-release-v0.1.9.md`）。**段9 がこのパスを `--notes-file` に渡す。**
**リポジトリの中に置かない。**正はリリースの本文であり、同じ文章を2箇所に持つと片方が古くなる。

### 7. タグを打つ

**打つ前に、タグ無しで CI を試せる。**

```bash
# continuo リポジトリの root で実行する
gh workflow run release.yml --ref main
```

**test と build までが走り、release は作られない。**publish はタグのときだけ動く
（[.github/workflows/release.yml](../../.github/workflows/release.yml) の
`if: startsWith(github.ref, 'refs/tags/v')`）。

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 段5 で決めた版に書き換える
git tag "$next" origin/main
git push origin "$next"
```

**タグを打つと [.github/workflows/release.yml](../../.github/workflows/release.yml) が走る。**
**それまで release は1つも作られない。**

### 8. 実行ファイルができるのを待つ

**`gh run list --limit 1` では、無関係な ci の run を掴む。**
**workflow とタグで絞り、`gh run watch` で終わるまで待つ。**

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 段5 で決めた版に書き換える
run=$(gh run list --workflow release.yml --branch "$next" --limit 1 \
  --json databaseId --jq '.[0].databaseId')
gh run watch "$run" --exit-status
```

**`run` が空なら、まだ run が登録されていない。**少し置いてから叩き直す。

**release に載るのは、この5つだけである。**

```
continuo_darwin_arm64.tar.gz   continuo_darwin_amd64.tar.gz
continuo_linux_amd64.tar.gz    continuo_linux_arm64.tar.gz
checksums.txt
```

**provenance は release には載らない。**GitHub の attestation として別に記録される。
確かめ方は [docs/releasing.md](../../docs/releasing.md) にある。

### 9. リリースノートを差し替える

**workflow は `--generate-notes` で作るので、commit の一覧が載る。**
**段6 で書いたノートで上書きする。**

```bash
# continuo リポジトリの root で実行する
next=v0.1.9   # 段5 で決めた版に書き換える
gh release edit "$next" --notes-file "${TMPDIR:-/tmp}/continuo-release-${next}.md"
```

### 10. 手元へ入れて、起動するところまで確かめる

```bash
# どこで実行してもよい
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/install.sh \
  | sh -s -- --no-deps --dir /tmp/continuo-relcheck
/tmp/continuo-relcheck/continuo version
```

**打った版が返れば、配布物は取れている。**
**`continuo --version` は無い。**`flag provided but not defined: -version` で終了コード 2 になる。

```bash
# WORKFLOW.md を置いてあるディレクトリを、引数で渡す
/tmp/continuo-relcheck/continuo doctor ~/continuo-work
```

**continuo リポジトリの root で `doctor` を叩かない。**そこに `WORKFLOW.md` は無いので、
**必ず `✗ 設定ファイル` が出る。**引数を省くと、いまいるディレクトリを見る。

**`continuo doctor` まで通すこと。**
**テストが全部通っても、実機で起動するまで見つからない不具合がある。**

### 11. 動いている continuo を入れ替える

**入れ替えは自動では起きない。**動いているものは古いバイナリのままである。

```bash
# どこで実行してもよい
pgrep -fl continuo
```

**止めて起動し直すのは人間の判断である。**勝手に止めない。

---

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

**CI が落ちて release ができていないなら、タグを消して打ち直してよい。**
**誰も掴んでいないためである。**消し方は [docs/releasing.md](../../docs/releasing.md) にある。
