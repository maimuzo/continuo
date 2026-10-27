#!/bin/sh
# 前の判断票を探し、今回の周の番号を決める（「posting-review-verdict」の「判断票の貼り方」の段0 の見本）。
#
# 使い方:
#   sh find-verdict.sh KIND=計画 PR= PRREPO=<owner/repo> ISSUE_URL=<issue の URL> ISSUE_REPO=<owner/repo>
#
#   KIND=        計画 か 実装
#   PR=          実装の判断票では、実装レビューを回している pull request の番号。計画では空のまま
#   PRREPO=      その pull request があるリポジトリ。省くか空なら ISSUE_REPO と同じ
#   ISSUE_URL=   この issue の URL
#   ISSUE_REPO=  この issue のリポジトリ（owner/repo）
#
# 出力は次のどれか1行。終了コードは見ない（どの場合も 0 で終わる）。
#   前の判断票はありません。今回は1周目です。…
#   前の判断票があります。今回は N 周目です。…
#   前の判断票はありますが、遷移表を読めません。…
#   判断票を探せませんでした（…）。1周目として貼らないでください
# 出た文ごとにすることは、posting-review-verdict の SKILL.md の段0 の表にある。
# 引数が足りない・知らない引数があるときも「判断票を探せませんでした」を出す。

KIND=
PR=
PRREPO=
ISSUE_URL=
ISSUE_REPO=
ERR=
for a in "$@"; do
  case "$a" in
    KIND=*) KIND=${a#KIND=} ;;
    PR=*) PR=${a#PR=} ;;
    PRREPO=*) PRREPO=${a#PRREPO=} ;;
    ISSUE_URL=*) ISSUE_URL=${a#ISSUE_URL=} ;;
    ISSUE_REPO=*) ISSUE_REPO=${a#ISSUE_REPO=} ;;
    *) ERR="知らない引数があります: $a" ;;
  esac
done
[ -n "$PRREPO" ] || PRREPO=$ISSUE_REPO
if [ -z "$ERR" ]; then
  case "$KIND" in
    計画 | 実装) ;;
    *) ERR="KIND= は 計画 か 実装 です" ;;
  esac
fi
if [ -z "$ERR" ]; then
  case "$ISSUE_URL|$ISSUE_REPO|$PRREPO" in
    *'<'* | *'>'*) ERR="置き換え語（<…>）が残っています。システムプロンプトの値で埋めてください" ;;
  esac
fi
if [ -z "$ERR" ] && { [ -z "$ISSUE_URL" ] || [ -z "$ISSUE_REPO" ]; }; then
  ERR="ISSUE_URL= と ISSUE_REPO= が要ります"
fi

REPO=$ISSUE_REPO
ID=
if [ -z "$ERR" ]; then
  if [ "$KIND" = 実装 ]; then
    REPO=$PRREPO
    if [ -z "$PR" ]; then
      ERR="PR= が空です"
    else
      ID=$(gh pr view "$PR" --repo "$PRREPO" --json comments \
        --jq '[.comments[] | select(.viewerDidAuthor and ((.body // "") | test("^[ \t\r\n]*<!-- code-review-result -->[ \t\r\n]*<!-- continuo:agent -->")))] | .[-1:][] | .url | split("#issuecomment-")[1]') \
        || ERR="gh pr view が失敗しました"
    fi
  else
    ID=$(gh issue view "$ISSUE_URL" --json comments \
      --jq '[.comments[] | select(.viewerDidAuthor and ((.body // "") | test("^[ \t\r\n]*<!-- continuo:agent -->[ \t\r\n]*<!-- design-review-result -->")))] | .[-1:][] | .url | split("#issuecomment-")[1]') \
      || ERR="gh issue view が失敗しました"
  fi
fi
if [ -n "$ERR" ]; then
  echo "判断票を探せませんでした（${ERR}）。1周目として貼らないでください"
else
  case "$ID" in
    '' | *[!0-9]*)
      echo "前の判断票はありません。今回は1周目です。段5 は ID= を空のまま、NO=1 で叩きます"
      ;;
    *)
      LAST=$(gh api "repos/$REPO/issues/comments/$ID" --jq .body | tr -d '\r' \
        | awk '/^<details>/ { exit } /^[|] *[0-9]+周目 *[|]/ { n = $0; sub(/^[|] */, "", n); sub(/周目.*/, "", n); last = n + 0 } END { print last + 0 }')
      if [ "$LAST" -gt 0 ]; then
        echo "前の判断票があります。今回は $((LAST + 1)) 周目です。段5 は ID=$ID NO=$((LAST + 1)) で叩きます"
      else
        echo "前の判断票はありますが、遷移表を読めません。段5 は ID=$ID と、あなたが数えている今回の周の番号で叩きます"
      fi
      ;;
  esac
fi
