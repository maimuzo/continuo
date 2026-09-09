#!/bin/sh
# 本物の herdr を叩くテストだけを走らせる。
#
# **何のためのものか。**mock は「continuo が正しいと思っている振る舞い」しか返さない。
# そのため `worktree.open` の引数のずれ（path と branch を両方渡していた）が
# テストを素通りし、実機の着手が全件落ちた（2026-08-20）。
# **ここは、その手のずれを手元で捕まえるための唯一の経路である。**
#
# **いつ回すか。**
#
#   - internal/herdr の params / result の形を変えたとき
#   - test/e2e/fakeherdr_test.go の台本を変えたとき
#   - herdr 本体を更新したとき
#   - PR を出す前（`sh scripts/test-like-ci.sh` は herdr を隠すので、こちらは走らない）
#
# **叩くのは herdr だけである。**Claude Code は起動しない（枠を消費するため）。
# GitHub の GraphQL / gh も叩かない（認証と本番のカンバンが要るため）。
#
# **herdr が居なければ静かに飛ぶ。**PATH に herdr が無い・socket が無い・socket へ
# 繋がらないのいずれかなら t.Skip になる。CI では必ず飛ぶ（runner に herdr が居ない）。
#
# **後始末はテストが自分で行う。**作った worktree は t.TempDir() の下に置き、
# workspace は worktree.remove と workspace.close で閉じる。
# 後始末に失敗したらテストの失敗として出る。**黙ってゴミを残さない。**

set -eu

echo "本物の herdr を叩くテストを走らせます（test/live）。"
if command -v herdr > /dev/null 2>&1; then
	echo "  herdr: $(command -v herdr)"
else
	echo "  herdr が PATH にありません。テストは skip されます。"
fi
echo ""

# **-count=1 を外さない。**結果をキャッシュされると、herdr を更新しても走らなくなる。
# **-v を付ける。**skip の理由を読めるようにするためである（何が確かめられなかったかが残る）。
exec go test -count=1 -v "$@" ./test/live/
