#!/bin/sh
# 担当が自分のままかを確かめる（「starting-run」の「段1. 担当が自分のままかを確かめる」の見本）。
#
# 使い方: sh check-assignee.sh <issue の URL>
#
# 出力は次のどれか1行。終了コードは見ない（どの場合も 0 で終わる）。
#   担当は自分です。進めます
#   担当者がいません。進めます
#   担当は <担当者> です。自分（<ログイン名>）は入っていません。止まります
#   確かめられませんでした（…）
# 出た文ごとにすることは、starting-run の SKILL.md の段1 の表にある。

ME=$(gh api user --jq .login) || ME=
WHO=$(gh issue view "$1" --json assignees --jq '[.assignees[].login] | join(",")') || WHO='?'
case "$ME" in
  '' | *[!A-Za-z0-9_.\[\]-]*)
    echo "確かめられませんでした（自分のログイン名を取れません）"
    ;;
  *)
    case ",${WHO}," in
      ",?,") echo "確かめられませんでした（担当者を取れません）" ;;
      ",,") echo "担当者がいません。進めます" ;;
      *",${ME},"*) echo "担当は自分です。進めます" ;;
      *) echo "担当は ${WHO} です。自分（${ME}）は入っていません。止まります" ;;
    esac
    ;;
esac
