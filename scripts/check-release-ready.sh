#!/bin/sh
# 出す前に、マージ済みの PR と、それと対になる issue を検査する。
#
# **タグを打ってから気づいても遅い。**release は消せるが、取った人の手元には残る。
#
# 見るもの:
#   レビュー結果 … PR に code-review の結果が貼ってあるか
#                  （CLAUDE.md「PR を出すときの絶対条件」。貼ってあることが実施の唯一の証拠）
#   対の issue   … PR と対になる issue が閉じていて、閉じたあとに説明のコメントがあるか
#                  （自動で閉じた issue にはリンクしか残らず、報告した人に伝わらない）
#
# 使い方:
#   sh scripts/check-release-ready.sh          … 直近のタグから origin/main まで
#   sh scripts/check-release-ready.sh v0.1.7   … 起点の版を指定する
#
# 1件でも欠けていれば 1 で終わる。
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
echo "起点: ${prev} → origin/main"
echo ""

# **PR 番号は merge commit の題名からだけ拾う。**
# commit の本文から `#N` を拾うと、issue 番号と混ざる。
prs="$(git log --oneline "${prev}"..origin/main \
	| grep -oE 'Merge pull request #[0-9]+' \
	| grep -oE '[0-9]+' | sort -un)"

if [ -z "${prs}" ]; then
	echo "この区間にマージした PR はありません。"
	exit 0
fi

ng=0  # 直さないと出せないもの
chk=0 # 人が見て判断するもの

# レビュー結果が貼ってあるかを見る。
# **目印は `<!-- code-review-result -->` である。**本文に "code-review" と書いただけの
# コメントを数えると、実施していないものが通ってしまう。
review_of() {
	gh pr view "$1" --json comments \
		--jq '[.comments[] | select(.body | contains("<!-- code-review-result -->"))] | length'
}

# 対になる issue の番号を並べる。
# **まず GitHub が紐付けたもの（Closes / Fixes / Resolves を GitHub が解釈した結果）と、
# 本文に書いた同じ形の記述を採る。**どちらも無ければ、本文に出てくる `#N` を候補として出す。
# **1つの PR に複数あってよい。**全部出す。
issues_of() {
	# shellcheck disable=SC2016  # $named は jq の変数である。シェルに展開させない。
	gh pr view "$1" --json body,closingIssuesReferences --jq '
		([ .closingIssuesReferences[].number,
		   (.body // "" | scan("(?i)(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[ :]*#([0-9]+)") | .[0] | tonumber)
		 ] | unique) as $named
		| (if ($named | length) > 0 then $named
		   else ([ .body // "" | scan("#([0-9]+)") | .[0] | tonumber ] | unique) end)
		| .[] | tostring'
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
		echo "          対になる issue が本文に出てきません ← 対が無いか、書き忘れかを確かめること"
		chk=$((chk + 1))
	fi
done

echo ""
if [ "${ng}" -gt 0 ] || [ "${chk}" -gt 0 ]; then
	echo "直すもの ${ng}件 ／ 見て判断するもの ${chk}件"
	echo "**直すものが残っている間は、タグを打たないこと。**"
	exit 1
fi
echo "マージした PR とその issue は揃っています。"
