#!/bin/sh
# 出す前に、マージ済みの PR と、それと対になる issue を検査する。
#
# **タグを打ってから気づいても遅い。**release は消せるが、取った人の手元には残る。
#
# 見るもの:
#   レビュー結果 … PR に code-review の結果が貼ってあるか
#                  （CLAUDE.md「PR を出すときの絶対条件」。貼ってあることが実施の唯一の証拠）
#   対の issue   … PR が閉じた issue に、閉じたあとの説明のコメントがあるか
#                  （自動で閉じた issue にはリンクしか残らず、報告した人に伝わらない）
#
# 使い方:
#   sh scripts/check-release-ready.sh                  … 直近のタグ → origin/main
#   sh scripts/check-release-ready.sh v0.1.7           … 起点の版を指定する
#   sh scripts/check-release-ready.sh v0.1.7 <commit>  … レビューの規則が入った commit を指定する
#   sh scripts/check-release-ready.sh v0.1.7 ""        … 区間の PR を全部見る（絞らない）
#
# 直すものが1件でも残れば 1 で終わる。
#
# **gh の認証が要る。**ネットワークにも出るので CI では回さない。

set -eu

if ! command -v gh > /dev/null 2>&1; then
	echo "gh が要ります: https://cli.github.com/" >&2
	exit 1
fi

prev="${1:-}"
if [ -z "${prev}" ]; then
	prev="$(git describe --tags --abbrev=0 origin/main)"
fi

# **レビューの規則が main に入った commit より前の PR は見ない。**
# その規則は c9f4a50（PR #63）で入った。**それ以前の PR は、規則が無い状態でマージされている。**
# 見に行くと、いま直しようのないものが毎回並び、検査そのものが読まれなくなる。
#
# **既定を「規則が入った commit」にしたのは、起点を人が覚えなくてよいからである。**
# 起点を渡す形だけにすると、渡し忘れたときに黙って全部を見てしまう。
# 全部を見たいときは、第2引数に空文字を渡す。
gate="${2-c9f4a50}"

# **起点を読めなければ、そこで止める。**
# `git log` が落ちてもパイプラインの終了状態には現れないので、`set -e` では捕まらない。
# **捕まえないと、版の名前を打ち間違えただけで「見る対象の PR はありません」と出て
# exit 0 を返す。**リリースの門番が、何も見ずに合格を出すことになる。
for ref in "${prev}" "${gate}"; do
	if [ -n "${ref}" ] && ! git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null; then
		echo "起点を読めません: ${ref}" >&2
		echo "  → 版の名前が正しいかを確かめてください（例: sh $0 v0.1.8）" >&2
		exit 1
	fi
done

range_log() {
	if [ -n "${gate}" ]; then
		git log --oneline "${prev}"..origin/main --not "${gate}"
	else
		git log --oneline "${prev}"..origin/main
	fi
}

echo "起点: ${prev} → origin/main"
if [ -n "${gate}" ]; then
	echo "レビューの規則が入った ${gate} 以降の PR だけを見る"
fi
echo ""

# **PR 番号は merge commit の題名からだけ拾う。**
# commit の本文から `#N` を拾うと、issue 番号と混ざる。
#
# **rebase merge は merge commit を作らないので、この方法では拾えない。**
# このリポジトリは merge commit を使う。**rebase merge に切り替えるなら、
# 拾い方を `gh pr list --state merged` から引く形に変えること。**
prs="$(range_log | grep -oE 'Merge pull request #[0-9]+' | grep -oE '[0-9]+' | sort -un || true)"

if [ -z "${prs}" ]; then
	echo "この区間に、見る対象の PR はありません。"
	exit 0
fi

ng=0

# レビュー結果が貼ってあるかを見る。
# **目印は `<!-- code-review-result -->` である。**本文に "code-review" と書いただけの
# コメントを数えると、実施していないものが通ってしまう。
# **目印はコメントの先頭にあるものだけを数える**（CLAUDE.md「コメントの先頭に
# `<!-- code-review-result -->` を置く」）。**本文の途中で目印を引用しただけの
# 説明コメント（例:「◯◯のコメントに目印が無い」という指摘そのもの）を contains() で
# 数えると、その PR は恒久的にレビュー済み扱いになってしまう。**
# **信頼できる投稿者（OWNER / MEMBER / COLLABORATOR）が貼ったものだけを数える。**
# このリポジトリは PUBLIC なので、通りがかりの投稿者が貼った目印まで数えると、
# その PR は恒久的にレビュー済み扱いになってしまう。
# **同じ目印・同じ判定基準が `.claude/hooks/block-merge-without-review.py` の
# `count_trusted_reviews()` にもある。片方だけ直すと、リリース前の検査とマージの検査が食い違う。
# 両方直すこと。**（`.claude/hooks/tests/test_block_merge_without_review.py` が、
# この一覧と Python 側の `TRUSTED_ASSOCIATIONS` が揃っていることを確かめる）
review_of() {
	# shellcheck disable=SC2016  # $a は jq の変数である。シェルに展開させない。
	gh pr view "$1" --json comments \
		--jq '[.comments[] | select(
			(.body // "" | test("^\\s*<!-- code-review-result -->"))
			and (.authorAssociation as $a | (["OWNER", "MEMBER", "COLLABORATOR"] | index($a)) != null)
		)] | length'
}

# 対になる issue の番号を並べる。
# **拾うのは、GitHub が紐付けたものと、本文の `Closes` / `Fixes` / `Resolves` の後ろだけである。**
# **ただ本文に出てくる `#N` は拾わない。**「足すのは issue #53 で扱う」のような参照まで
# 対の issue として数えてしまい、まだ開いている issue が毎回並ぶ（実測: PR #63）。
# **1つの PR に複数あってよい。**全部出す。
issues_of() {
	# shellcheck disable=SC2016  # jq の中の記法である。シェルに展開させない。
	gh pr view "$1" --json body,closingIssuesReferences --jq '
		[ .closingIssuesReferences[].number,
		  (.body // "" | scan("(?i)(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[ :]*#([0-9]+)") | .[0] | tonumber)
		] | unique | .[] | tostring'
}

# issue の状態と、閉じたあとに説明のコメントが付いているかを見る。
# **PR は弾く。**issue と PR は番号を共有しており、`gh issue view` は PR も返す。
issue_line() {
	# shellcheck disable=SC2016  # $closed / $last は jq の変数である。シェルに展開させない。
	gh issue view "$1" --json url,number,state,closedAt,comments --jq '
		select(.url | contains("/pull/") | not)
		| (.closedAt // "") as $closed
		| (.comments[-1].createdAt // "") as $last
		| "issue #\(.number) \(.state) 説明="
		  + (if .state != "CLOSED" then "閉じていない"
		     elif $last == "" then "無し"
		     elif $last >= $closed then "有り"
		     else "無し（閉じる前のコメントだけ）" end)' 2> /dev/null
}

for p in ${prs}; do
	r="$(review_of "${p}" || echo 0)"
	if [ "${r}" = "0" ]; then
		echo "PR #${p}  レビュー結果=無し  ← レビューを回し直し、結果を PR へ貼ること"
		ng=$((ng + 1))
	else
		echo "PR #${p}  レビュー結果=有り（${r}件）"
	fi

	found=0
	for n in $(issues_of "${p}" || true); do
		line="$(issue_line "${n}" || true)"
		[ -n "${line}" ] || continue
		found=1
		echo "          ${line}"
		case "${line}" in
			*"説明=有り") ;;
			*) ng=$((ng + 1)) ;;
		esac
	done
	if [ "${found}" = "0" ]; then
		# **issue から生まれない PR はある。**文書だけの直し・CI の直しなどである。
		# **無いことを異常として数えない。**数えると、毎回ここで止まる。
		echo "          対の issue=無し（issue から生まれた PR ではない）"
	fi
done

echo ""
if [ "${ng}" -gt 0 ]; then
	echo "直すもの ${ng}件"
	echo "**直すものが残っている間は、タグを打たないこと。**"
	exit 1
fi
echo "直すもの 0件。マージした PR とその issue は揃っています。"
