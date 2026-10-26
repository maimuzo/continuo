#!/bin/sh
# まとめて直した issue の Closes の行を、pull request の本文へ足す
# （「finishing-run」の「まとめて直したとき」の「段0. 表明と pull request の本文を、issue ごとに1行ずつ書く」の見本）。
#
# 使い方:
#   sh add-closes-line.sh <issue のリポジトリ（owner/repo）> <PR番号> <その issue の番号>
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
  echo "使い方: sh add-closes-line.sh <issue のリポジトリ> <PR番号> <その issue の番号>" >&2
  exit 2
}

[ $# -eq 3 ] || usage
REPO=$1
PR=$2
NUM=$3
case "$REPO" in
  '' | *'<'* | *'>'*) usage ;;
  */*) ;;
  *) usage ;;
esac
case "$PR" in
  '' | *[!0-9]*) usage ;;
esac
case "$NUM" in
  '' | *[!0-9]*) usage ;;
esac

F=$(mktemp)
if gh pr view "$PR" --repo "$REPO" --json body --jq .body > "$F" && [ -s "$F" ]; then
  if grep -qx "Closes #${NUM}" "$F"; then
    echo "もう入っています"
  else
    printf '\nCloses #%s\n' "$NUM" >> "$F"
    gh pr edit "$PR" --repo "$REPO" --body-file "$F"
  fi
else
  echo "PR の本文を読めませんでした。書き換えていません"
fi
