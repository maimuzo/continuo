# worktree の片付け

## 絶対条件：使い終わった worktree は、その作業を終える前に消す

**「あとで片付ける」と引き継ぎに書いて、次のセッションへ渡してはならない。**

**2026-08-30、17個まで溜まった。**引き継ぎには「`git worktree prune` を1回叩けば済む」と
書いてあったが、**prune では1つも消えなかった。**

**消すのは、次のどれかになった時点である。**

| いつ | 例 |
| --- | --- |
| **PR がマージされた** | `gh pr merge` が通った直後 |
| **PR を閉じた** | 別の形に作り直したとき |
| **作業を中断した** | 枠が尽きた・方針が変わった。**branch を push してから消す** |

---

## `git worktree prune` は片付けの手段ではない

| コマンド | 何をするか |
| --- | --- |
| **`git worktree remove <パス>`** | **実体のディレクトリを消し、登録も消す。**片付けはこちら |
| `git worktree prune` | **登録はあるが実体が無いものの、登録だけを消す。**実体があるものには触れない |

**prune は、実体が先に消えたあとの掃除にすぎない。**
scratchpad の worktree は、セッションが終わると実体だけが消える。**そのとき初めて prune の出番になる。**

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

# 一覧（.claude/worktrees/ も出る）
git worktree list
```

**走っている background の agent / workflow が、そのディレクトリで動いていないかも見る。**

---

## `.claude/worktrees/` も見る

**workflow が作った worktree はここに溜まる。**
**scratchpad だけを見て「片付いた」と言ってはならない。**

**`git worktree list` が唯一の一覧である。**

---

## stash は worktree を消しても残る

**stash は `.git` の中にあるので、worktree を消しても失われない。**
**逆に、放置すると誰にも気づかれない。**

**worktree を片付けるときに `git stash list` も見る。**古いものが残っていたら、報告する。
