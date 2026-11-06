#!/bin/sh
# 報告のコメントに置く「レビューの結果と、pull request の状態」の節を出す（「finishing-run」の「runの終了を宣言する」の段4 の段A の見本）。
#
# 使い方:
#   sh review-status.sh ISSUE_URL=<issue の URL> ISSUE_REPO=<owner/repo> PR=<PR番号> PRREPO=<owner/repo>
#
#   ISSUE_URL=   この issue の URL
#   ISSUE_REPO=  この issue のリポジトリ（owner/repo）
#   PR=          この branch の開いた pull request の番号
#   PRREPO=      その pull request があるリポジトリ。省くか空なら ISSUE_REPO と同じ
#
# 出すのは事実だけで、残りの手順は出さない（残りの手順は run の終わり方で番が変わるので、スキルの表で決める）。
# 判断票は posting-review-verdict の find-verdict.sh で探す（探す条件を1か所に保つため）。
#
# 標準出力には節の中身だけを出し、読めなかった理由は標準エラーへ出す。
# どちらにも手元の絶対パスを入れない（節は公開のコメントへそのまま貼られる）。
# 読めなかった項目は「読めませんでした」と出し、推測で埋めない。
#
# 終了コード:
#   0  節を出した（読めなかった項目があるときも）
#   2  引数を直して、もう一度叩く（何も読まず、何も出していない）

ISSUE_URL=
ISSUE_REPO=
PR=
PRREPO=
for a in "$@"; do
  case "$a" in
    ISSUE_URL=*) ISSUE_URL=${a#ISSUE_URL=} ;;
    ISSUE_REPO=*) ISSUE_REPO=${a#ISSUE_REPO=} ;;
    PR=*) PR=${a#PR=} ;;
    PRREPO=*) PRREPO=${a#PRREPO=} ;;
    *) echo "引数を直して、もう一度叩いてください（知らない引数があります: ${a%%=*}）" >&2; exit 2 ;;
  esac
done
[ -n "$PRREPO" ] || PRREPO=$ISSUE_REPO
case "$ISSUE_URL|$ISSUE_REPO|$PR|$PRREPO" in
  *'<'* | *'>'*) echo "引数を直して、もう一度叩いてください（置き換え語（<…>）が残っています）" >&2; exit 2 ;;
esac
if [ -z "$ISSUE_URL" ] || [ -z "$ISSUE_REPO" ]; then
  echo "引数を直して、もう一度叩いてください（ISSUE_URL= と ISSUE_REPO= が要ります）" >&2; exit 2
fi
case "$PR" in
  '' | *[!0-9]*) echo "引数を直して、もう一度叩いてください（PR= には pull request の番号を書きます）" >&2; exit 2 ;;
esac

# **find-verdict.sh は、このスクリプトと同じ plugin の中にある。**場所は自分の在りかから解く。
# このパスは標準出力にも標準エラーにも出さない（手元の絶対パスになるため）。
FIND="$(dirname "$0")/../../posting-review-verdict/scripts/find-verdict.sh"

# verdict は、判断票を1つ読んで、判定の行と遷移表（と削除の行）を出す。
#
# $1: 計画 か 実装
# $2: 見出しに出す名前（設計レビュー / 実装レビュー）
verdict() {
  kind=$1
  label=$2
  repo=$ISSUE_REPO
  [ "$kind" = 実装 ] && repo=$PRREPO
  found=$(sh "$FIND" KIND="$kind" PR="$PR" PRREPO="$PRREPO" ISSUE_URL="$ISSUE_URL" ISSUE_REPO="$ISSUE_REPO" 2>/dev/null)
  id=
  case "$found" in
    前の判断票はありません。*)
      if [ "$kind" = 計画 ] && skipped=$(skipped_url) && [ -n "$skipped" ]; then
        echo "- ${label}: 飛ばした（断り: ${skipped}）"
      else
        echo "- ${label}: 判断票がありません"
      fi
      return
      ;;
    前の判断票があります。* | 前の判断票はありますが、遷移表を読めません。*)
      id=$(printf '%s\n' "$found" | sed -n 's/.*ID=\([0-9][0-9]*\).*/\1/p')
      ;;
  esac
  if [ -z "$id" ]; then
    echo "読めませんでした: ${label}の判断票を探せませんでした" >&2
    echo "- ${label}: 判断票を読めませんでした"
    return
  fi
  # 1行目に URL、2行目から本文を出させる。gh api は失敗すると誤りの JSON を標準出力へ出すので、終了コードで分ける。
  got=$(gh api "repos/$repo/issues/comments/$id" --jq '.html_url + "\n" + (.body // "")' 2>/dev/null) || got=
  url=$(printf '%s\n' "$got" | sed -n '1p')
  case "$url" in
    https://*) ;;
    *)
      echo "読めませんでした: ${label}の判断票の本文を読めませんでした" >&2
      echo "- ${label}: 判断票を読めませんでした"
      return
      ;;
  esac
  # 遷移表の行（| N周目 | C | H | M | L |）と削除の行（- N周目で削除: …）を、<details> より前から写す。
  table=$(printf '%s\n' "$got" | sed '1d' | tr -d '\r' \
    | awk '/^<details>/ { exit } /^[|] *[0-9]+周目 *[|]/ || /^- [0-9]+周目で削除:/ { print }')
  rows=$(printf '%s\n' "$table" | grep -c '^[|]')
  if [ "$rows" -eq 0 ]; then
    echo "- ${label}: 判断票はありますが、遷移表を読めませんでした（${url}）"
    return
  fi
  # 最後の周の番号・CRITICAL・HIGH と、最初の周の番号を取る。
  last=$(printf '%s\n' "$table" | grep '^[|]' | tail -n 1)
  no=$(printf '%s\n' "$last" | awk -F'|' '{ n = $2; gsub(/[^0-9]/, "", n); print n }')
  c=$(printf '%s\n' "$last" | awk -F'|' '{ v = $3; gsub(/[^0-9]/, "", v); print v }')
  h=$(printf '%s\n' "$last" | awk -F'|' '{ v = $4; gsub(/[^0-9]/, "", v); print v }')
  first=$(printf '%s\n' "$table" | grep '^[|]' | sed -n '1p' | awk -F'|' '{ n = $2; gsub(/[^0-9]/, "", n); print n }')
  if printf '%s\n' "$table" | grep -q "^- ${no}周目で削除:"; then
    del=あり
  else
    del=なし
  fi
  if [ "$c" = 0 ] && [ "$h" = 0 ] && [ "$del" = なし ]; then
    echo "- ${label}: 収まった（${no}周目）"
  else
    echo "- ${label}: 収まっていない（最後は ${no}周目。CRITICAL ${c}・HIGH ${h}・削除${del}）"
  fi
  if [ "$first" != 1 ]; then
    echo "- ${label}の前の周は、別の判断票にあります（この判断票: ${url}）"
  fi
  echo
  echo '| 周 | CRITICAL | HIGH | MEDIUM | LOW |'
  echo '| --- | --- | --- | --- | --- |'
  printf '%s\n' "$table" | grep '^[|]'
  dels=$(printf '%s\n' "$table" | grep '^- ')
  if [ -n "$dels" ]; then
    echo
    printf '%s\n' "$dels"
  fi
}

# skipped_url は、pull request に貼られた、設計レビューを飛ばす断りの URL を出す（無ければ空）。
#
# CI（review-gate.yml）と同じく、目印の後ろに理由のあるものだけを数える。投稿者は OWNER / MEMBER / COLLABORATOR。
skipped_url() {
  gh pr view "$PR" --repo "$PRREPO" --json comments \
    --jq '[.comments[] | select((.authorAssociation == "OWNER" or .authorAssociation == "MEMBER" or .authorAssociation == "COLLABORATOR") and ((.body // "") | test("^[ \t\r\n]*<!-- design-review-skipped -->[ \t\r\n]*[^ \t\r\n]")))] | .[-1:][] | .url' 2>/dev/null
}

# checks は、検査の件数を区分ごとに1行で出す。
#
# **件数は終了コードではなく、出力の行（タブ区切りの2列目の区分）から数える。**
# gh は pull request が見つからないときも、検査に fail があるときと同じ終了コード 1 を返す（gh 2.103.3 で実測）。
#
# $1: 見出しに出す名前（必須の検査 / 全部の検査）
# $2: --required か空
checks() {
  label=$1
  if [ -n "$2" ]; then
    got=$(gh pr checks "$PR" --repo "$PRREPO" --required 2>&1)
  else
    got=$(gh pr checks "$PR" --repo "$PRREPO" 2>&1)
  fi
  tab=$(printf '\t')
  lines=$(printf '%s\n' "$got" | grep "$tab")
  if [ -z "$lines" ]; then
    case "$got" in
      *'no required checks reported'*) echo "- ${label}: 必須の検査なし" ;;
      *'no checks reported'*) echo "- ${label}: 検査なし" ;;
      *)
        echo "読めませんでした: ${label}を読めませんでした" >&2
        echo "- ${label}: 読めませんでした"
        ;;
    esac
    return
  fi
  printf '%s\n' "$lines" | awk -F'\t' -v label="$label" '
    { n[$2]++; if ($2 == "fail" || $2 == "cancel") bad = bad (bad == "" ? "" : "・") $1 }
    END {
      printf "- %s: pass %d・fail %d・pending %d・skipping %d・cancel %d", label, n["pass"], n["fail"], n["pending"], n["skipping"], n["cancel"]
      if (bad != "") printf "（落ちたもの: %s）", bad
      printf "\n"
    }'
}

echo '## 報告: レビューの結果と、pull request の状態'
echo
verdict 計画 設計レビュー
echo
verdict 実装 実装レビュー
echo

state=$(gh pr view "$PR" --repo "$PRREPO" --json url,isDraft,mergeable,mergeStateStatus,reviewDecision \
  --jq '[.url, (.isDraft | tostring), .mergeable, .mergeStateStatus, (.reviewDecision // "")] | @tsv' 2>/dev/null) || state=
url=$(printf '%s\n' "$state" | awk -F'\t' '{ print $1 }')
case "$url" in
  https://*)
    draft=$(printf '%s\n' "$state" | awk -F'\t' '{ print $2 }')
    mergeable=$(printf '%s\n' "$state" | awk -F'\t' '{ print $3 }')
    mss=$(printf '%s\n' "$state" | awk -F'\t' '{ print $4 }')
    rd=$(printf '%s\n' "$state" | awk -F'\t' '{ print $5 }')
    echo "- pull request: ${url}"
    case "$draft" in
      true) echo "- draft: はい" ;;
      false) echo "- draft: いいえ" ;;
      *) echo "- draft: 読めませんでした" ;;
    esac
    ;;
  *)
    echo "読めませんでした: pull request の状態を読めませんでした" >&2
    echo "- pull request: 読めませんでした"
    echo "- draft: 読めませんでした"
    mergeable=
    mss=
    rd=READ_FAILED
    ;;
esac
checks 必須の検査 --required
checks 全部の検査 ''
case "$rd" in
  '') echo "- 承認（reviewDecision）: 求められていない" ;;
  APPROVED) echo "- 承認（reviewDecision）: APPROVED（承認済み）" ;;
  CHANGES_REQUESTED) echo "- 承認（reviewDecision）: CHANGES_REQUESTED（変更を求められている）" ;;
  REVIEW_REQUIRED) echo "- 承認（reviewDecision）: REVIEW_REQUIRED（承認が要る）" ;;
  *) echo "- 承認（reviewDecision）: 読めませんでした" ;;
esac
case "$mergeable" in
  MERGEABLE | CONFLICTING | UNKNOWN) echo "- 競合（mergeable）: $mergeable" ;;
  *) echo "- 競合（mergeable）: 読めませんでした" ;;
esac
case "$mss" in
  CLEAN) echo "- mergeStateStatus: CLEAN（マージできる）" ;;
  UNSTABLE) echo "- mergeStateStatus: UNSTABLE（マージできるが、通っていない検査がある）" ;;
  HAS_HOOKS) echo "- mergeStateStatus: HAS_HOOKS（マージできる）" ;;
  BLOCKED) echo "- mergeStateStatus: BLOCKED（branch の保護の条件（必須の検査・必須の承認など）を満たしていない）" ;;
  BEHIND) echo "- mergeStateStatus: BEHIND（向け先より古い）" ;;
  DIRTY) echo "- mergeStateStatus: DIRTY（競合がある）" ;;
  DRAFT) echo "- mergeStateStatus: DRAFT（draft なのでマージできない）" ;;
  UNKNOWN) echo "- mergeStateStatus: UNKNOWN（GitHub が計算中）" ;;
  *) echo "- mergeStateStatus: 読めませんでした" ;;
esac
exit 0
