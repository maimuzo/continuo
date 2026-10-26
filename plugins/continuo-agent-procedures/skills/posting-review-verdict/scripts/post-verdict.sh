#!/bin/sh
# 判断票を貼る（「posting-review-verdict」の「判断票の貼り方」の段5 の見本）。
#
# 使い方:
#   sh post-verdict.sh KIND=計画 PR= PRREPO=<owner/repo> ISSUE_URL=<issue の URL> ISSUE_REPO=<owner/repo> \
#     ID=<段0 が出した ID> NO=<段0 が出した周の番号> COUNTS=<ファイル> NOTE=<ファイル> ROUND=<ファイル>
#
#   KIND=        計画 か 実装
#   PR=          実装の判断票では、実装レビューを回している pull request の番号。計画では空のまま
#   PRREPO=      その pull request があるリポジトリ。省くか空なら ISSUE_REPO と同じ
#   ISSUE_URL=   この issue の URL
#   ISSUE_REPO=  この issue のリポジトリ（owner/repo）
#   ID=          段0 が出した ID。前の判断票が無いときと、数え直すときは空のまま
#   NO=          段0 が出した今回の周の番号。数え直すときは 1
#   COUNTS=      今回の周の件数を1行で書いたファイル（| C | H | M | L |）
#   NOTE=        削った内容を1行で書いたファイル。削らなかった周は空のファイルか、NOTE= を空のまま
#   ROUND=       今回の周の中身（<details> の中と同じもの）を書いたファイル
#
# 前の判断票へ今回の行・削除の行・<details> を足し、先頭が2行の印のままで、遷移表の行がちょうど1つ増え、
# 60,000 文字以下であることを確かめてから、同じ呼び出しの中で書き換える。書き換えられないときは、
# 前の周の <details> を落とした本文で新しく1件貼り、前の判断票が無い・読めない・遷移表を持たないときは、
# NO= の行だけの遷移表で新しく1件貼る。
#
# 出力は次のどれか1行（gh が出すものを除く）。出た文ごとにすることは、posting-review-verdict の SKILL.md の段5 の表にある。
#   前の判断票へ書き足しました（N 文字）                       終了コード 0
#   前の判断票へ書き足せないので、遷移表を写して新しく1件貼りました  終了コード 0
#   新しく1件貼りました（N周目から）                            終了コード 0
#   NO= に今回の周の番号を書いてから、もう一度叩いてください       終了コード 1
#   貼れませんでした                                            終了コード 1
#   引数を直して、もう一度叩いてください（…）                    終了コード 2（何も読まず、何も書いていない）

KIND=
PR=
PRREPO=
ISSUE_URL=
ISSUE_REPO=
ID=
NO=
COUNTS=
NOTE=
ROUND=
for a in "$@"; do
  case "$a" in
    KIND=*) KIND=${a#KIND=} ;;
    PR=*) PR=${a#PR=} ;;
    PRREPO=*) PRREPO=${a#PRREPO=} ;;
    ISSUE_URL=*) ISSUE_URL=${a#ISSUE_URL=} ;;
    ISSUE_REPO=*) ISSUE_REPO=${a#ISSUE_REPO=} ;;
    ID=*) ID=${a#ID=} ;;
    NO=*) NO=${a#NO=} ;;
    COUNTS=*) COUNTS=${a#COUNTS=} ;;
    NOTE=*) NOTE=${a#NOTE=} ;;
    ROUND=*) ROUND=${a#ROUND=} ;;
    *) echo "引数を直して、もう一度叩いてください（知らない引数があります: $a）"; exit 2 ;;
  esac
done
[ -n "$PRREPO" ] || PRREPO=$ISSUE_REPO
[ -n "$NOTE" ] || NOTE=/dev/null
case "$KIND" in
  計画 | 実装) ;;
  *) echo "引数を直して、もう一度叩いてください（KIND= は 計画 か 実装 です）"; exit 2 ;;
esac
case "$ISSUE_URL|$ISSUE_REPO|$PRREPO|$ID|$NO" in
  *'<'* | *'>'*) echo "引数を直して、もう一度叩いてください（置き換え語（<…>）が残っています）"; exit 2 ;;
esac
if [ -z "$ISSUE_URL" ] || [ -z "$ISSUE_REPO" ]; then
  echo "引数を直して、もう一度叩いてください（ISSUE_URL= と ISSUE_REPO= が要ります）"; exit 2
fi
if [ "$KIND" = 実装 ] && [ -z "$PR" ]; then
  echo "引数を直して、もう一度叩いてください（実装の判断票では PR= が要ります）"; exit 2
fi
for f in "$COUNTS" "$NOTE" "$ROUND"; do
  if [ -z "$f" ] || [ ! -r "$f" ]; then
    echo "引数を直して、もう一度叩いてください（COUNTS= と ROUND= には、読めるファイルを渡します: ${f:-空}）"; exit 2
  fi
done

REPO=$ISSUE_REPO
if [ "$KIND" = 実装 ]; then
  REPO=$PRREPO
  M1='<!-- code-review-result -->'; M2='<!-- continuo:agent -->'; TITLE='# レビューの判断票（実装）'
else
  M1='<!-- continuo:agent -->'; M2='<!-- design-review-result -->'; TITLE='# レビューの判断票（計画）'
fi
HEAD_RE="^[[:space:]]*${M1}[[:space:]]*${M2}"
OLD=$(mktemp)
NEW=$(mktemp)
rows() { awk '/^<details>/ { exit } /^[|] *[0-9]+周目 *[|]/ { n++ } END { print n + 0 }' "$1"; }
build() {
  awk -v keep="$1" -v first="$2" -v countsf="$COUNTS" -v notef="$NOTE" -v roundf="$ROUND" '
    { l[NR] = $0 }
    !fd && /^<details>/ { fd = NR }
    !fd && /^[|] *[0-9]+周目 *[|]/ { lt = NR; n = $0; sub(/^[|] */, "", n); sub(/周目.*/, "", n); last = n + 0 }
    !fd && /^[|] *---/ { sep = NR }
    END {
      if (first) { lt = sep; last = first - 1; fd = NR + 1 }
      if (!fd || !lt) exit 3
      if ((getline c < countsf) <= 0 || c == "") exit 4
      no = last + 1
      for (i = 1; i <= lt; i++) print l[i]
      print "| " no "周目 " c
      e = fd - 1
      while (e > lt && l[e] ~ /^[ \t]*$/) e--
      for (i = lt + 1; i <= e; i++) print l[i]
      k = 0
      while ((getline r < notef) > 0) if (r != "") { if (!k && e == lt) print ""; print "- " no "周目で削除: " r; k = 1 }
      print ""
      if (keep) { for (i = fd; i <= NR; i++) print l[i]; print "" }
      print "<details>"; print "<summary>" no "周目</summary>"; print ""
      while ((getline r < roundf) > 0) print r
      print ""; print "</details>"
    }' "$OLD" > "$NEW"
}
ok() {
  N=$(LC_ALL=C.UTF-8 wc -m < "$NEW" | tr -d ' ')
  [ "$N" -gt 0 ] && [ "$N" -le 60000 ] && head -n 3 "$NEW" | tr '\n' ' ' | grep -Eq "$HEAD_RE" \
    && [ "$(rows "$NEW")" -eq $(( $(rows "$OLD") + 1 )) ]
}
post() {
  if [ "$KIND" = 実装 ]; then
    gh pr comment "$PR" --repo "$REPO" --body-file "$NEW"
  else
    gh issue comment "$ISSUE_URL" --body-file "$NEW"
  fi
}
DONE=
case "$ID" in
  '' | *[!0-9]*)
    ;;
  *)
    gh api "repos/$REPO/issues/comments/$ID" --jq .body | tr -d '\r' | sed '/[^[:space:]]/,$!d' > "$OLD"
    if head -n 3 "$OLD" | tr '\n' ' ' | grep -Eq "$HEAD_RE"; then
      if build 1 0 && ok && gh api --method PATCH "repos/$REPO/issues/comments/$ID" -F body=@"$NEW" > /dev/null; then
        DONE=1; echo "前の判断票へ書き足しました（$N 文字）"
      elif build 0 0 && ok && post; then
        DONE=1; echo "前の判断票へ書き足せないので、遷移表を写して新しく1件貼りました"
      fi
    fi
    ;;
esac
if [ -z "$DONE" ]; then
  case "$NO" in
    '' | *[!0-9]* | 0)
      echo "NO= に今回の周の番号を書いてから、もう一度叩いてください"
      exit 1
      ;;
    *)
      printf '%s\n%s\n%s\n\n| 周 | CRITICAL | HIGH | MEDIUM | LOW |\n| --- | --- | --- | --- | --- |\n\n' "$M1" "$M2" "$TITLE" > "$OLD"
      if build 0 "$NO" && ok && post; then
        echo "新しく1件貼りました（${NO}周目から）"
      else
        echo "貼れませんでした"
        exit 1
      fi
      ;;
  esac
fi
