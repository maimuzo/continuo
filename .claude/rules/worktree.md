# worktree の片付け

## 絶対条件：使い終わった worktree は、その作業を終える前に消す

**「あとで片付ける」と引き継ぎに書いて、次のセッションへ渡してはならない。**

**実例。**17個まで溜まった。引き継ぎには「`git worktree prune` を1回叩けば済む」と
書いてあったが、**prune では1つも消えなかった。**

**消すのは、次のどれかになった時点である。**

| いつ | 例 |
| --- | --- |
| **PR がマージされた** | `gh pr merge` が通った直後 |
| **PR を閉じた** | 別の形に作り直したとき |
| **作業を中断した** | 方針が変わった。**branch を push してから消す** |

---

## `git worktree prune` は片付けの手段ではない

| コマンド | 何をするか |
| --- | --- |
| **`git worktree remove <パス>`** | **実体のディレクトリを消し、登録も消す。**片付けはこちら |
| `git worktree prune` | **登録はあるが実体が無いものの、登録だけを消す。**実体があるものには触れない |

**prune は、実体が先に消えたあとの掃除にすぎない。**

---

## 絶対条件：確認の前に `git fetch origin` を打つ

**打たないと、`origin/main` を見る検査が全部、嘘の答えを返す。**

**手元の `origin/main` は、最後に fetch した時点で止まっている。**
**その先でマージされたものは、手元から見ると「まだマージされていない」に見える。**

**2026-09-02 に実際に踏んだ。**マージ済みの branch を消そうとしたところ、
`git branch --merged origin/main` が**その branch を1行も返さなかった。**
手元の `origin/main` が28分前の commit を指したままで、
**マージ commit そのものが手元に存在していなかった**（`git cat-file -t` が落ちた）。

**いちばん危ないのは、嘘が「安全側の顔」で出ることである。**
**返らなかったときの見た目は「まだマージされていないので消さないでおこう」と全く同じで、区別が付かない。**
**マージした直後の branch ほど当たりやすく、片付けのたびに1本ずつ取り残される。**

```bash
git fetch origin -q   # 検査の前に必ず打つ
```

**同じ理由で、[CLAUDE.md](../../CLAUDE.md) の「6. hook の経路」の検知スクリプトも、
先頭で `git fetch origin -q` を打っている。**あちらは「手元の `main` ではなく `origin/main` を見る」だが、
**`origin/main` に切り替えても、fetch していなければ同じことが起きる。**だから2つで1組である。
[docs/releasing.md](../../docs/releasing.md) の版の比較も、先に fetch してから `origin/main` を見る。

**`--merged` は commit の祖先関係で判定する。**squash merge や rebase merge で入った branch は、
**何度 fetch しても永久に出ない。**そのときは
`git -C <パス> log --oneline HEAD --not --remotes`（どの remote にも無い commit が残っていないか）で確かめる。

---

## 消す前に4つを確認する

**「たぶん使っていない」で消してはならない。**

```bash
git fetch origin -q   # 上の絶対条件。これを先に打つ

# 未コミットの変更（untracked も数える）
git -C <パス> status --porcelain --untracked-files=all

# 未マージの commit。0件でなければ、その commit が origin にあるか確かめる
git -C <パス> log --oneline origin/main..HEAD

# 開いている PR が使っていないか
gh pr list --state open --json number,title,headRefName

# 一覧（ツールが作った worktree も出る）
git worktree list
```

**走っている background のエージェントが、そのディレクトリで動いていないかも見る。**

---

## branch を消してよいかは、`--merged origin/main` だけが決める

**`git branch -d` は判定していない。安全網だと思ってはならない。**
**判定は fetch のあとの `git branch --merged origin/main` が1つで行う。**

**`-d` が比べる相手は `origin/main` ではない。**git の man がこう書いている。

> The branch must be fully merged in its upstream branch, or in HEAD if no upstream was set
> with `--track` or `--set-upstream-to`.
>
> （訳: **その branch は upstream へ完全にマージされていなければならない。
> `--track` や `--set-upstream-to` で upstream を設定していない場合は、`HEAD` へマージされていなければならない。**）

| 何 | 何と比べているか |
| --- | --- |
| **`git branch --merged origin/main`**（fetch のあと） | **`origin/main`。判定はこれだけが行う** |
| `git branch -d` | **upstream があれば upstream、無ければ `HEAD`。`origin/main` は見ていない** |
| `git branch -D` | **何とも比べない** |

**だから `-d` は2通りに外れる**（git 2.50.1 で実測）。

| いつ | 何が起きるか |
| --- | --- |
| **`-u` を付けて push した branch**（[docs/upgrading.md](../../docs/upgrading.md) がそう指示している。つまりほぼ全部） | **upstream は branch 自身と同じ commit なので、main へ入っていなくても素通りする。**`warning:` を1行出して消える |
| **手元の `main` が `origin/main` より遅れている** | **`--merged origin/main` に出ていても `error: the branch '…' is not fully merged` で断る。**git は `-D` を使えと案内する |

**手順。**

```bash
git fetch origin -q
git branch --merged origin/main                          # ここに出ていなければ、消さない
git worktree remove <その branch を出している worktree>   # 出しているなら先に消す
git branch -d <branch 名>                                # 断られたら、上の一覧に出ていることを確かめて -D
```

**worktree を先に消す。**branch を出している worktree が在るあいだは、
**`-d` でも `-D` でも `error: cannot delete branch '…' used by worktree at '…'` で通らない。**
`git branch --merged` の一覧では、その branch の先頭に `+` が付いている。

**`--merged origin/main` に出ていない branch を `-D` で消さない。**
**そこだけは、どんな理由があっても曲げない。**

---

## ツールが作った worktree も見る

**エージェントの並列実行が作った worktree は `.claude/worktrees/` に溜まる。**

**`git worktree list` が唯一の一覧である。**
**自分が作った場所だけを見て「片付いた」と言ってはならない。**

---

## stash は worktree を消しても残る

**stash は `.git` の中にあるので、worktree を消しても失われない。**
**逆に、放置すると誰にも気づかれない。**

**worktree を片付けるときに `git stash list` も見る。**古いものが残っていたら、報告する。
