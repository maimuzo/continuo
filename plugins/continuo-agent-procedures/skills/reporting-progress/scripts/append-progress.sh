#!/bin/sh
# 途中経過のコメントを書く（「reporting-progress」の「段2a. その1件に書き足す」と「段2b. 新しく1件投稿する」の見本）。
#
# 使い方:
#   sh append-progress.sh append <issue のリポジトリ（owner/repo）> <段1が返したコメントの ID> <<'NOW'
#   <いま何をしていて、あと何が残っているか>
#   NOW
#
#   sh append-progress.sh new <issue の URL> <<'NOW'
#   <いま何をしていて、あと何が残っているか>
#   NOW
#
# 出力:
#   append  書き換えたときは gh api が返した JSON。
#           本文を読めなかったときは「本文を読めませんでした。段2b で新しく1件投稿します」（何も書き込まない）
#   new     gh issue comment が返したコメントの URL
#   引数か標準入力が足りないときは、何も書き込まずに使い方を標準エラーへ出し、終了コード 2 で終わる。
#
# mktemp で作ったファイルは消さない（見本と同じ）。

usage() {
  echo "使い方: sh append-progress.sh append <issue のリポジトリ> <コメントの ID> <<'NOW' … NOW" >&2
  echo "        sh append-progress.sh new <issue の URL> <<'NOW' … NOW" >&2
  exit 2
}

# 置き換え忘れ（<issue の URL> のような山括弧の語のまま）の引数を断る。
for A in "$@"; do
  case "$A" in
    '' | *'<'* | *'>'*) usage ;;
  esac
done

MODE=$1
case "$MODE" in
  append)
    [ $# -eq 3 ] || usage
    REPO=$2
    ID=$3
    case "$REPO" in
      */*) ;;
      *) usage ;;
    esac
    case "$ID" in
      *[!0-9]*) usage ;;
    esac
    ;;
  new)
    [ $# -eq 2 ] || usage
    URL=$2
    ;;
  *)
    usage
    ;;
esac

NOWF=$(mktemp)
cat > "$NOWF"
[ -s "$NOWF" ] || usage

ADD=$(mktemp)
printf -- '- %s いま ' "$(date '+%Y-%m-%d %H:%M (%Z)')" > "$ADD"
cat "$NOWF" >> "$ADD"

if [ "$MODE" = append ]; then
  OLD=$(gh api "repos/${REPO}/issues/comments/${ID}" --jq .body)
  case "$OLD" in
    *"<!-- continuo:progress -->"*)
      F=$(mktemp)
      { printf '%s\n' "$OLD"; cat "$ADD"; } > "$F"
      gh api --method PATCH "repos/${REPO}/issues/comments/${ID}" \
        -F body=@"$F"
      ;;
    *)
      echo "本文を読めませんでした。段2b で新しく1件投稿します"
      ;;
  esac
else
  F=$(mktemp)
  cat > "$F" <<'PROGRESS'
<!-- continuo:agent -->
<!-- continuo:progress -->
まだ作業中です。

PROGRESS
  cat "$ADD" >> "$F"
  gh issue comment "$URL" --body-file "$F"
fi
