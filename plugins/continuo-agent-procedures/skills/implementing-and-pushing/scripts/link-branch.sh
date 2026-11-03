#!/bin/sh
# いまの branch を issue に結びつけて remote に作る（「implementing-and-pushing」の「pull request を出す」の段2 の見本）。
#
# 使い方:
#   sh link-branch.sh <issue の URL>
#
# 結びつけた branch から pull request を作ると、GitHub はその pull request を issue の Development にリンクし、
# branch のリンクを外す（2026-11-03、向け先が既定の branch の PR で測った）。本文の `Closes #<番号>` は
# 向け先が既定の branch でないと読まれないので、これで補う。
#
# 結びつけるのは、次の4つがそろうときだけである。そろわなければ何も書き込まずに、そう出す。
#   1. origin が issue のリポジトリである（別のリポジトリへ出すときは結びつけない）
#   2. remote にいまの branch がまだ無い（既に在る branch を結びつけたときの動きは測っていない。
#      確かめられなかったときも「在る」と扱う）
#   3. issue にリンクされた branch が1本も無い（continuo はリンクがちょうど1本なら worktree の起点にするので、
#      人間が起点にリンクした branch の上に足すと2本になり、起点が既定の branch へ倒れる）
#   4. 起点の commit が決まる（HEAD と origin/<既定の branch> の merge-base。HEAD の祖先で remote に在るので、
#      そのあとの `git push -u origin HEAD` が fast-forward になる）
#
# 出力は次のどれか1行（gh と git が出すものを除く）。どの場合も終了コード 0 で終わる。
#   結びつけました（<branch 名>）
#   結びつけません（<理由>）
#   結びつけられませんでした（<理由>）
#   引数が足りないときは、使い方を標準エラーへ出し、終了コード 2 で終わる。
#
# 道具は gh 内蔵の --jq とシェルの case だけを使う（単体の jq は使わない）。

usage() {
  echo "使い方: sh link-branch.sh <issue の URL>" >&2
  exit 2
}

[ $# -eq 1 ] || usage
URL=$1
case "$URL" in
  '' | *'<'* | *'>'*) usage ;;
  https://github.com/*/*/issues/*) ;;
  *) usage ;;
esac

# https://github.com/<owner>/<repo>/issues/<番号> を分ける。
REST=${URL#https://github.com/}
OWNER=${REST%%/*}
REST=${REST#*/}
REPO=${REST%%/*}
NUM=${REST#*/issues/}
NUM=${NUM%%[!0-9]*}
case "$NUM" in
  '') usage ;;
esac

BRANCH=$(git branch --show-current 2>/dev/null)
if [ -z "$BRANCH" ]; then
  echo "結びつけません（いまの branch が分かりません）"
  exit 0
fi

ORIGIN=$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null)
if [ "$ORIGIN" != "$OWNER/$REPO" ]; then
  echo "結びつけません（origin が issue のリポジトリではありません）"
  exit 0
fi

# 終了コードは、在れば 0、無ければ 2（--exit-code）。それ以外は確かめられなかったとして「在る」と扱う。
git ls-remote --exit-code --heads origin "refs/heads/$BRANCH" >/dev/null 2>&1
case $? in
  2) ;;
  0)
    echo "結びつけません（remote に ${BRANCH} が既に在ります）"
    exit 0
    ;;
  *)
    echo "結びつけません（remote に branch が在るかを確かめられませんでした）"
    exit 0
    ;;
esac

IDS=$(gh api graphql \
  -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){id issue(number:$n){id linkedBranches(first:1){totalCount}}}}' \
  -f o="$OWNER" -f r="$REPO" -F n="$NUM" \
  --jq '[.data.repository.id, .data.repository.issue.id, (.data.repository.issue.linkedBranches.totalCount | tostring)] | join(" ")' 2>/dev/null)
set -- $IDS
REPO_ID=$1
ISSUE_ID=$2
COUNT=$3
if [ -z "$REPO_ID" ] || [ -z "$ISSUE_ID" ] || [ -z "$COUNT" ]; then
  echo "結びつけられませんでした（issue を読めませんでした）"
  exit 0
fi
if [ "$COUNT" != 0 ]; then
  echo "結びつけません（issue にリンクされた branch が既に ${COUNT} 本あります）"
  exit 0
fi

DEFAULT=$(gh repo view "$OWNER/$REPO" --json defaultBranchRef --jq .defaultBranchRef.name 2>/dev/null)
if [ -z "$DEFAULT" ]; then
  echo "結びつけられませんでした（既定の branch の名前を読めませんでした）"
  exit 0
fi
if ! git fetch -q origin "$DEFAULT" >/dev/null 2>&1; then
  echo "結びつけられませんでした（origin/${DEFAULT} を取ってこられませんでした）"
  exit 0
fi
OID=$(git merge-base HEAD FETCH_HEAD 2>/dev/null)
if [ -z "$OID" ]; then
  echo "結びつけられませんでした（起点の commit が決まりませんでした）"
  exit 0
fi

GOT=$(gh api graphql \
  -f query='mutation($i:ID!,$r:ID!,$b:String!,$c:GitObjectID!){createLinkedBranch(input:{issueId:$i,repositoryId:$r,name:$b,oid:$c}){linkedBranch{ref{name}}}}' \
  -f i="$ISSUE_ID" -f r="$REPO_ID" -f b="$BRANCH" -f c="$OID" \
  --jq '.data.createLinkedBranch.linkedBranch.ref.name' 2>/dev/null)
if [ "$GOT" = "$BRANCH" ]; then
  echo "結びつけました（${BRANCH}）"
else
  echo "結びつけられませんでした（GitHub が branch を作りませんでした）"
fi
exit 0
