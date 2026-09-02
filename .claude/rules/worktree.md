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

**同じ落とし穴が [CLAUDE.md](../../CLAUDE.md) の「6. hook の経路」にもある。**
あちらは「手元の `main` ではなく `origin/main` を見る」だが、
**`origin/main` に切り替えても、fetch していなければ同じことが起きる。**

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

**branch を消すときも同じである。**

```bash
git fetch origin -q
git branch --merged origin/main       # ここに出たものだけ
git branch -d <branch 名>             # -D ではなく -d で消す
```

**`-D` を使わない。**`-d` は「マージされていない」と判断したときに断ってくれる。
**`-D` はその門を素通りするので、fetch を忘れた状態と組み合わさると、
本当にマージされていない branch を黙って消す。**

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
