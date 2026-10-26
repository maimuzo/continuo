#!/bin/sh
# まとめて直した同じグループの別の issue へ、何をしたかを書く
# （「finishing-run」の「まとめて直したとき」の「段2a. その1件へ書き足す」と「段2b. 新しく1件投稿する」の見本）。
#
# 使い方:
#   sh append-group-report.sh read   <issue のリポジトリ（owner/repo）> <段1が返した URL>
#   sh append-group-report.sh append <issue のリポジトリ（owner/repo）> <段1が返した URL> <<'LINE'
#   <足す行（行頭の "- " はこのスクリプトが付ける）>
#   LINE
#   sh append-group-report.sh new    <issue のリポジトリ（owner/repo）> <その issue の番号> <issue の識別子（owner/repo#N）> <<'GROUP'
#   <先頭の1文より後ろの本文（ふつうは空行から始める）>
#   GROUP
#
# 出力:
#   read    印（<!-- continuo:group -->）のある本文が読めたら、その本文
#   append  書き換えたときは gh api が返した JSON
#   read / append で ID を取れないか本文を読めないときは、次のどちらか（何も書き込まない）
#           コメントの ID を取れませんでした。段2b で新しく1件投稿します
#           本文を読めませんでした。段2b で新しく1件投稿します
#   new     gh issue comment が返したコメントの URL
#   引数か標準入力が足りないときは、何も書き込まずに使い方を標準エラーへ出し、終了コード 2 で終わる。
#
# 書き換えは、印を確かめる case の中だけで行う。外で行うと、読み取りに失敗したときに
# 印ごと本文が消え、段1 がその報告を二度と見つけられない。
# mktemp で作ったファイルは消さない（見本と同じ）。

usage() {
  echo "使い方: sh append-group-report.sh read <issue のリポジトリ> <段1が返した URL>" >&2
  echo "        sh append-group-report.sh append <issue のリポジトリ> <段1が返した URL> <<'LINE' … LINE" >&2
  echo "        sh append-group-report.sh new <issue のリポジトリ> <その issue の番号> <issue の識別子> <<'GROUP' … GROUP" >&2
  exit 2
}

# 置き換え忘れ（<issue のリポジトリ> のような山括弧の語のまま）の引数を断る。
for A in "$@"; do
  case "$A" in
    '' | *'<'* | *'>'*) usage ;;
  esac
done

MODE=$1
case "$MODE" in
  read | append)
    [ $# -eq 3 ] || usage
    ;;
  new)
    [ $# -eq 4 ] || usage
    ;;
  *)
    usage
    ;;
esac
REPO=$2
case "$REPO" in
  */*) ;;
  *) usage ;;
esac

if [ "$MODE" = new ]; then
  NUM=$3
  IDENT=$4
  case "$NUM" in
    *[!0-9]*) usage ;;
  esac
  BODY=$(mktemp)
  cat > "$BODY"
  [ -s "$BODY" ] || usage
  F=$(mktemp)
  printf '%s\n%s と一緒に見ました。この issue の分は次のとおりです。\n' '<!-- continuo:group -->' "${IDENT}" > "$F"
  cat "$BODY" >> "$F"
  gh issue comment "$NUM" --repo "$REPO" --body-file "$F"
  exit
fi

URL=$3
if [ "$MODE" = append ]; then
  LINEF=$(mktemp)
  cat > "$LINEF"
  [ -s "$LINEF" ] || usage
  ADD=$(mktemp)
  printf -- '- ' > "$ADD"
  cat "$LINEF" >> "$ADD"
fi

ID=${URL##*#issuecomment-}
case "$ID" in
  '' | *[!0-9]*)
    echo "コメントの ID を取れませんでした。段2b で新しく1件投稿します"
    ;;
  *)
    OLD=$(gh api "repos/${REPO}/issues/comments/${ID}" --jq .body)
    case "$OLD" in
      *"<!-- continuo:group -->"*)
        if [ "$MODE" = read ]; then
          printf '%s\n' "$OLD"
        else
          F=$(mktemp)
          { printf '%s\n' "$OLD"; cat "$ADD"; } > "$F"
          gh api --method PATCH "repos/${REPO}/issues/comments/${ID}" \
            -F body=@"$F"
        fi
        ;;
      *)
        echo "本文を読めませんでした。段2b で新しく1件投稿します"
        ;;
    esac
    ;;
esac
