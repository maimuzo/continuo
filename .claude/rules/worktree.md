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

## 消す前に4つを確認する

**「たぶん使っていない」で消してはならない。**

```bash
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

## ツールが作った worktree も見る

**エージェントの並列実行が作った worktree は `.claude/worktrees/` に溜まる。**

**`git worktree list` が唯一の一覧である。**
**自分が作った場所だけを見て「片付いた」と言ってはならない。**

---

## stash は worktree を消しても残る

**stash は `.git` の中にあるので、worktree を消しても失われない。**
**逆に、放置すると誰にも気づかれない。**

**worktree を片付けるときに `git stash list` も見る。**古いものが残っていたら、報告する。
