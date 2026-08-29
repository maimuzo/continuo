# `git push -u origin HEAD` が continuo の worktree で足りるか

**何を確かめたか。**continuo が作る worktree の中で `git push -u origin HEAD` を叩くと、
**その issue のために作られた branch と同じ名前の branch が remote にでき、upstream もそこへ張られる。**
だからエージェントは branch 名を自分で決めなくてよい（設計 3-9 の「— その前提」、5-3 の本文）。

**何を確かめていないか。**

| 確かめた | 確かめていない |
| --- | --- |
| **git そのものの振る舞い。**remote はローカルの bare repository | **GitHub 側。**認証・branch protection・組織の規則 |

**GitHub 側は人間が実機で1度通してください。**この記録はそこまでを保証しません。

## 再現の手順

**この記録は 2026-08-29 に macOS 15（Darwin 25.6.0）/ git で取りました。**
下のスクリプトはどこにも書き込みを残さないので、そのまま貼って再実行できます。

```bash
#!/bin/bash
# ローカルの bare repository を origin に見立てて、continuo と同じ作り方の worktree から
# `git push -u origin HEAD` が通るかを確かめる。GitHub へは一切触らない。
set -u
SCR="${TMPDIR:-/tmp}/continuo-pushcheck"
rm -rf "$SCR"; mkdir -p "$SCR"
export GIT_CONFIG_GLOBAL="$SCR/gitconfig"
export GIT_CONFIG_SYSTEM=/dev/null
git config --global user.email "octocat@example.com"
git config --global user.name "octocat"
git config --global init.defaultBranch main

git init --bare -q "$SCR/origin.git"
git init -q "$SCR/repo"
cd "$SCR/repo" || exit 1
echo hello > README.md
git add README.md
git commit -q -m "first"
git remote add origin "$SCR/origin.git"
git -c push.default=simple push -q -u origin main
echo "=== 本体の準備おわり ==="

# continuo と同じ作り方（internal/workspace/git.go の gitWorktreeAddOnce）
BR="continuo/octocat/hello-world/64"
git worktree add -q -b "$BR" "$SCR/wt" main
cd "$SCR/wt" || exit 1
echo "--- HEAD ---"; git symbolic-ref --short HEAD
echo "--- upstream があるか ---"
git rev-parse --abbrev-ref '@{upstream}' 2>&1 || echo "(upstream なし)"
echo work > work.txt; git add work.txt; git commit -q -m "agent の作業"
echo "--- git push -u origin HEAD ---"
git push -u origin HEAD 2>&1; echo "exit=$?"
echo "--- origin の branch ---"; git --git-dir="$SCR/origin.git" branch --list
echo "--- upstream ---"; git rev-parse --abbrev-ref '@{upstream}'
echo "--- 2回目（-u なし）---"
echo more >> work.txt; git commit -q -am "2回目"; git push 2>&1; echo "exit=$?"
echo "--- commit するものが無い状態で git commit ---"
git commit -m "何も無い" 2>&1; echo "exit=$?"
```

## 出力（原文のまま）

```text
=== 本体の準備おわり ===
--- HEAD ---
continuo/octocat/hello-world/64
--- upstream があるか ---
fatal: no upstream configured for branch 'continuo/octocat/hello-world/64'
(upstream なし)
--- git push -u origin HEAD ---
To <検証用ディレクトリ>/origin.git
 * [new branch]      HEAD -> continuo/octocat/hello-world/64
branch 'continuo/octocat/hello-world/64' set up to track 'origin/continuo/octocat/hello-world/64'.
exit=0
--- origin の branch ---
  continuo/octocat/hello-world/64
* main
--- upstream ---
origin/continuo/octocat/hello-world/64
--- 2回目（-u なし）---
To <検証用ディレクトリ>/origin.git
   f08b89e..6e35ca5  continuo/octocat/hello-world/64 -> continuo/octocat/hello-world/64
exit=0
--- commit するものが無い状態で git commit ---
On branch continuo/octocat/hello-world/64
Your branch is up to date with 'origin/continuo/octocat/hello-world/64'.

nothing to commit, working tree clean
exit=1
```

**置換したのは検証用ディレクトリの絶対パスだけです**（このリポジトリは公開されているため）。
それ以外は git が出したままです。

## 読み取れること

| 何 | 結果 |
| --- | --- |
| **worktree の HEAD** | **detached ではない。**`git worktree add -b` で切った branch にそのまま乗っている |
| **push する前の upstream** | **無い。**だから片付けは「upstream が無い」経路に落ちる（設計 3-9 の手順2b） |
| **`git push -u origin HEAD`** | **通る。**`continuo/octocat/hello-world/64` が remote にでき、upstream が張られる |
| **2回目以降** | **`git push` だけで足りる。**upstream が残っているため |
| **commit するものが無いとき** | **`git commit` は exit 1 で落ちる**（`nothing to commit, working tree clean`） |

**最後の行は、まだ何も書いていない段階で `blocked` を出す場面に効きます。**
雛形は例外なく「必ず commit して push」と求めるので、その場面ではエージェントが必ず1回失敗します。
**この扱いは未決です**（issue #64 の「決めること」）。
