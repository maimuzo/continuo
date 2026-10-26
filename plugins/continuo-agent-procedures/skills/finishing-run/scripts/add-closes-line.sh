#!/bin/sh
# まとめて直した issue の Closes の行を、pull request の本文へ足す
# （「finishing-run」の「まとめて直したとき」の「段0. 表明と pull request の本文を、issue ごとに1行ずつ書く」の見本）。
#
# 使い方:
#   sh add-closes-line.sh <issue のリポジトリ（owner/repo）> <PR番号> <その issue の番号> [<pull request のリポジトリ（owner/repo）>]
#
# pull request を issue と別のリポジトリへ出したときだけ、4つ目に出した先のリポジトリを渡す。
# そのときは出した先で本文を読み書きし、足す行を `Closes <issue のリポジトリ>#<番号>` にする
# （「finishing-pull-request」の「別のリポジトリへ pull request を出すとき」）。
#
# 出力は次のどれか。
#   もう入っています
#   <gh pr edit が出した pull request の URL>     … 足して書き戻した
#   PR の本文を読めませんでした。書き換えていません
# 引数が足りないか、番号が数字でないときは、何も書き込まずに使い方を標準エラーへ出し、終了コード 2 で終わる。
#
# 本文を読めたこと（gh が成功し、中身が空でないこと）を確かめずに gh pr edit を叩かない。
# 叩くと、本文が足した1行だけになり、その pull request の Closes の行と説明が消える。
# mktemp で作ったファイルは消さない（見本と同じ）。

usage() {
  echo "使い方: sh add-closes-line.sh <issue のリポジトリ> <PR番号> <その issue の番号> [<pull request のリポジトリ>]" >&2
  exit 2
}

[ $# -eq 3 ] || [ $# -eq 4 ] || usage
REPO=$1
PR=$2
NUM=$3
PRREPO=${4:-$1}
for R in "$REPO" "$PRREPO"; do
  case "$R" in
    '' | *'<'* | *'>'*) usage ;;
    */*) ;;
    *) usage ;;
  esac
done
case "$PR" in
  '' | *[!0-9]*) usage ;;
esac
case "$NUM" in
  '' | *[!0-9]*) usage ;;
esac

if [ "$PRREPO" = "$REPO" ]; then
  LINE="Closes #${NUM}"
else
  LINE="Closes ${REPO}#${NUM}"
fi

F=$(mktemp)
if gh pr view "$PR" --repo "$PRREPO" --json body --jq .body > "$F" && [ -s "$F" ]; then
  # 行末の CR を外して比べる（Windows の改行で書かれた本文でも、同じ行を重ねて足さない）。
  if tr -d '\r' < "$F" | grep -qxF "$LINE"; then
    echo "もう入っています"
  else
    printf '\n%s\n' "$LINE" >> "$F"
    gh pr edit "$PR" --repo "$PRREPO" --body-file "$F"
  fi
else
  echo "PR の本文を読めませんでした。書き換えていません"
fi
