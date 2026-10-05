package scaffold

// ciTemplate は continuo init が書き出す continuo-ci.yaml の雛形である（設計 5-3o）。
//
// **これは設定ではない。**continuo は起動時にこのファイルを1バイトも読まない。
// **利用者が中身を確かめてから .github/workflows/ へ移すための見本である。**
// そのため、書けなくても continuo init は成功で終える（設計 5-3o）。
//
// **目印の文字列と job の名前を、設定から変えられる形にしてはならない。**
// 目印は internal/prompt/builtin.md がエージェントに書かせる文字列と対でしか意味を持たず、
// 組み込みは実行ファイルの中にあって利用者が変えられない。片方だけ変えられる口を開けると、
// 「CI が探す目印」と「エージェントが書く目印」が食い違う状態を、誰にも気づけない形で作れる。
//
// **プレースホルダを埋める口だけは、WORKFLOW.md の雛形と同じ経路に載せてある**
// （TemplateWithValues と同じく CITemplateWithValues を通す）。
// **いま埋める値は0個である。**上の理由で目印と job 名を渡さないため、埋めるものが無い。
// 値を足すときは、組み込みの側にも同じ値が届く形にしてから足すこと。
//
// 文字列リテラルとして持つのは WORKFLOW.md の雛形と同じ理由である。
// 構造体から yaml.Marshal すると YAML のコメントが全部消え、雛形として役に立たなくなる。
//
// **この雛形には backtick を1文字も書かない。**Go の raw string には backtick を置けないので、
// 書こうとすると文字列を何度も連結することになり、雛形そのものが読めなくなる。
// **markdown のコード表記は使わず、地の文で書く。**GITHUB_STEP_SUMMARY はそれでも読める。
//
// **中身が YAML として読めることは test/internal/scaffold/ci_template_test.go が押さえる。**
// 壊れた YAML でも Go のビルドは通り、テストも通り、そのまま配られてしまう。
// **そして GitHub Actions は、読めない workflow を「検査が無い」として扱う。**
// 画面では「まだ走っていない」と見分けが付かない。
const ciTemplate = `# レビュー結果が貼られていない pull request を落とす。
#
# **continuo init が置いた見本です。**ここに在るだけでは何も起きません。
# **中身を確かめてから .github/workflows/ へ移してください。**
# 既に CI がある場合は、この中身を既存の CI へ組み込むよう、お使いの AI に頼んでください。
#
# **2つの検査を持ちます。**
#
#   design-review-result   設計のレビュー結果が、紐づく issue のコメントに貼られているか
#   code-review-result     実装のレビュー結果が、この pull request のコメントに貼られているか
#
# **どちらも「レビューを飛ばして先へ進む」ことを止めるためのものです。**
# **文書に書くだけでは守られません。**continuo の開発で実際に起きました
# （2026-08-29、12本をレビューせずにマージし、あとから回し直すことになりました）。
#
# **目印の文字列を変えないでください。**continuo がエージェントへ送る指示書と対になっています。
# 送られる文面は continuo prompt --show で読めます。
# **片方だけ変えると、CI が探す目印とエージェントが書く目印が食い違い、誰にも気づけません。**
#
# **job の名前も変えないでください。**branch protection の必須の検査は、この名前で登録します。
# **名前を変えると設定が宙に浮き、検査が無いのにマージできる状態になります。**
#
# **赤いだけではマージを止められません。**必須の検査への登録は、人間が1回だけ行います。
# 手順は continuo の CONTRIBUTING.md の「この検査をマージの条件にする」にあります。

name: continuo-review-gate

on:
  pull_request:
    # **ready_for_review を入れます。**gh pr ready がこのイベントを起こすので、
    # 「結果を貼る → ready にする → 検査が回り直して緑」が人手なしでつながります。
    #
    # **edited は入れません。**逃がす断りは pull request の**コメント**に貼るので、
    # edited（題名・本文・base の変更）では回り直しません。
    # **断りを後から貼ったときは gh run rerun で回し直してください。**
    types: [opened, synchronize, reopened, ready_for_review]

# **読むだけでよい。**
# **叩く先は /issues/{番号}/comments ですが、相手は pull request です。**
# 紐づく issue を引く gh pr view は GraphQL を使うので、pull-requests: read が要ります。
#
# **投稿者が push できるかの照会（/repos/{owner}/{repo}/collaborators/{名前}/permission）も、
# この2つのままで通ります**（個人が持つ private のリポジトリで実測。2026-10-04）。
# **権限を組み替えるときは、この照会が通るかも確かめてください。**通らなくなっても、
# 立場が OWNER / MEMBER / COLLABORATOR と見える投稿者のコメントは、いままでどおり数えます。
permissions:
  issues: read
  pull-requests: read

# **concurrency を置きません。**
# 打ち切られた run は success / skipped / neutral のどれでもないので、
# **必須の検査にしたときマージを塞ぎます。**

jobs:
  # **設計のレビュー結果が、紐づく issue のコメントに貼られているか。**
  #
  # **実装のレビュー（下の job）と分けています。**1つにまとめると、
  # 赤の理由をログまで見に行くことになります。
  design-review-result:
    runs-on: ubuntu-latest
    steps:
      - name: 設計のレビュー結果が貼ってあるか
        # **checkout しません。**この job はリポジトリの中身を1つも読みません。
        # **GH_TOKEN と GH_ENTERPRISE_TOKEN の両方を渡します。**
        # gh は github.com と <名前>.ghe.com では GH_TOKEN を、GitHub Enterprise Server では
        # GH_ENTERPRISE_TOKEN を読みます。片方だけだと、どちらかの環境で認証に落ちます。
        env:
          GH_TOKEN: ${{ github.token }}
          GH_ENTERPRISE_TOKEN: ${{ github.token }}
          REPO: ${{ github.repository }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          IS_DRAFT: ${{ github.event.pull_request.draft }}
        run: |
          set -eu

          # **gh の宛先を、この検査が走っている GitHub にします。**
          # GITHUB_SERVER_URL は runner が必ず置く環境変数で、github.com なら
          # https://github.com、GitHub Enterprise ならそのホストの URL です。
          # **置かないと、GitHub Enterprise でも gh は github.com へ問い合わせます。**
          GH_HOST="${GITHUB_SERVER_URL#*://}"
          export GH_HOST

          # **数える条件は2つです。**
          #   一、目印が本文の**先頭**か、continuo:agent の印の**直後**にある。
          #       **直後を許すのは、continuo が「エージェントが書いたか」を
          #       本文の先頭ちょうどで見ているためです。**
          #   二、投稿者の立場（author_association）が OWNER / MEMBER / COLLABORATOR である。
          #       **どれでもないときは、その投稿者がこのリポジトリへ push できる。**
          #
          # **二の後ろ半分を足した理由。**この検査が使う GITHUB_TOKEN からは、
          # 書き込める人の立場が MEMBER に見えないことがあります（organization が持つ
          # リポジトリでの報告。そのコメントを人間のトークンで読むと MEMBER と出ます。
          # **原因は確定していません**）。立場だけで数えると、レビュー結果を貼っても、
          # 断りを貼っても、検査が緑になりません。
          # **立場が当たる投稿者が1人でも居れば、権限の照会は叩きません。**
          # いままで緑だった pull request は、同じ道筋で緑になります。
          #
          # **バックスラッシュ s を使いません。**engine によって当たる範囲が違い、
          # 全角空白 U+3000 を前に置いた本文が片方だけ通ります。
          # **当たる文字を並べて書きます。**半角空白・タブ・CR・LF の4文字だけです。

          # **タブ1文字。**下の関数が、行を「名前」と「立場」に分けるのに使います。
          TAB="$(printf '\t')"

          # **権限の照会の結果を控えるファイル。**落ちるときの案内に出します。
          #   perm_denied.txt   push できなかった投稿者（名前と、この検査から見えた立場）
          #   perm_missing.txt  照会が 404 だった投稿者
          #   perm_errors.txt   照会がそれ以外で失敗した投稿者（確かめられなかった）
          : > perm_denied.txt
          : > perm_missing.txt
          : > perm_errors.txt

          # has_pusher は、push できる投稿者が1人でも居れば 0 を、居なければ 1 を返します。
          #
          # 引数: 「名前<タブ>立場」を1行に1つ並べたファイル。
          #
          # **if の条件として呼びます。**人数を標準出力へ出して整数で比べる形にはしません。
          # 途中で落ちたときに空文字の比較になり、黙って偽へ倒れるためです。
          #
          # **標準出力と標準エラーを混ぜません。**混ぜると、gh が成功しつつ警告を1行出した
          # だけで値が汚れ、push できる人を数えられなくなります。
          #
          # **404 とそれ以外の失敗を分けます。**まとめて「権限が無い」に倒すと、
          # rate limit や一時的な失敗のときに「貼られていません」と嘘の案内を出します。
          # **404 の原因は言い切りません。**存在しない名前を叩くと 404 になることは
          # 確かめてありますが、この検査のトークンから見えないときにも 404 が返りえます。
          #
          # **true / false 以外の値は、黙って数え落とさずに控えます。**
          # 応答に .user.permissions.push が無い環境（GitHub Enterprise Server では
          # 確かめていません）では、ここへ来ます。
          #
          # **下の job にも同じ関数があります。**YAML の job をまたいで共有できないためです。
          # **中身を変えるときは、両方を直してください。**
          has_pusher() {
            local login assoc can_push rc
            while IFS="${TAB}" read -r login assoc || [ -n "${login}" ]; do
              [ -n "${login}" ] || continue
              rc=0
              can_push=$(gh api "repos/${REPO}/collaborators/${login}/permission" \
                --jq '.user.permissions.push' 2>gh_stderr.txt) || rc=$?
              if [ "${rc}" -ne 0 ]; then
                if grep -q "HTTP 404" gh_stderr.txt; then
                  printf '%s\n' "${login}" >> perm_missing.txt
                else
                  printf '%s\t%s\n' "${login}" "$(tr '\n' ' ' < gh_stderr.txt | cut -c1-200)" >> perm_errors.txt
                fi
                continue
              fi
              case "${can_push}" in
                true) return 0 ;;
                false) printf '%s\t%s\n' "${login}" "${assoc}" >> perm_denied.txt ;;
                *) printf '%s\t権限を表す値が true / false ではありませんでした: %s\n' \
                     "${login}" "${can_push}" >> perm_errors.txt ;;
              esac
            done < "$1"
            return 1
          }

          # poster_counts は、数える条件の二に当たる投稿者が居れば 0 を返します。
          #
          # 引数: 目印に当たったコメントを1行ずつ並べたファイル。行は次の2通りです。
          #   ok                        立場が OWNER / MEMBER / COLLABORATOR
          #   login<タブ>名前<タブ>立場  立場が外れた。push できるかを照会する
          #
          # **ok が1行でもあれば、照会は叩きません。**
          # **同じ投稿者を2回叩きません**（sort -u）。
          # **本文はこのファイルに入りません。**入るのは GitHub が付けた名前と立場だけなので、
          # コメントの本文に何を書いても、ok の行は作れません。
          poster_counts() {
            if grep -q '^ok$' "$1"; then
              return 0
            fi
            awk -F'\t' '$1 == "login" && $2 != "" { print $2 "\t" $3 }' "$1" | sort -u > logins.txt
            has_pusher logins.txt
          }

          # poster_notes は、数えなかった投稿者を案内に出します。
          #
          # 引数: 何の投稿者か（「断り」「設計のレビュー結果」など）。
          #
          # **この検査から見えた立場を出します。**同じコメントを人間のトークンで読むと
          # 別の立場に見えることがあり、画面を見ても原因に気づけないためです。
          poster_notes() {
            if [ -s perm_denied.txt ]; then
              echo ""
              echo "**${1}の目印で始まるコメントは在りますが、次の投稿者のものは数えていません。**"
              echo "立場が OWNER / MEMBER / COLLABORATOR と見えず、このリポジトリへ push もできません。"
              sort -u perm_denied.txt | awk -F'\t' '{ print "- " $1 "（この検査からは " $2 " と見えています）" }'
            fi
            if [ -s perm_missing.txt ]; then
              echo ""
              echo "**${1}の投稿者のうち、次の人は権限の照会が 404 でした。**"
              echo "その名前が存在しないか、この検査のトークンからは見えません。"
              sort -u perm_missing.txt | sed 's/^/- /'
            fi
            if [ -s perm_errors.txt ]; then
              echo ""
              echo "**${1}の投稿者のうち、次の人は push できるかを確かめられませんでした。**"
              echo "**「権限が無い」ではなく「確かめられなかった」です。**"
              echo "一時的な失敗なら、回し直すと直ります。"
              echo "**回し直しても同じなら、この検査のトークンでは権限を照会できません**"
              echo "（fork から来た pull request など）。そのときは、立場が"
              echo "OWNER / MEMBER / COLLABORATOR と見える人に貼ってもらってください。"
              sort -u perm_errors.txt | sed 's/^/- /'
            fi
          }

          # 段1. 設計のレビューを飛ばす断りが貼られているか。
          #
          # **pull request の本文ではなく、コメントを見ます。**上の2つの条件をそのまま掛けられて、
          # 数え方を1組で済ませられるためです。
          # **エージェントから守れるわけではありません。**エージェントが、数える条件の二に
          # 当たる資格情報で叩けば、その投稿も数えます。**この逃がし口は自己申告です。**
          #
          # **理由は目印の次の行に書かせます。**目印の中へ書かせると、
          # 閉じの2文字を理由と読んでしまい、理由が空でも通ります。
          #
          # **取得の失敗を守ります。**守らないと step ごと落ち、案内が空のまま赤になります。
          # **守りの中で必ず終わらせます。**gh は失敗したとき、誤りの JSON を標準出力へ
          # 1行出します。先へ進めると、その1行を「目印に当たったコメント」と数えかねません。
          if ! gh api --paginate "repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100" --jq '
            .[]
            | select((.body // "") | test("^[ \\t\\r\\n]*<!-- design-review-skipped -->[ \\t\\r\\n]*[^ \\t\\r\\n]"))
            | if (.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
              then "ok"
              else "login\t" + (.user.login // "") + "\t" + (.author_association // "")
              end' > skipped.txt; then
            echo "この pull request のコメントを読めませんでした（gh run rerun で回し直してください）" \
              | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 1
          fi
          if poster_counts skipped.txt; then
            echo "設計のレビューを飛ばす断りが貼られています" | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 0
          fi

          # **断りの照合で控えた分を、落ちるときの案内のために取っておきます。**
          # 下の照合も同じ3つのファイルへ控えるので、混ざらないよう先に書き出します。
          poster_notes "断り（design-review-skipped）" > skipped_notes.txt
          : > perm_denied.txt
          : > perm_missing.txt
          : > perm_errors.txt

          # 段2. 紐づく issue を引く。
          #
          # **取れなかったことと、0件だったことを分けます。**混ぜると、引き方を間違えた日から
          # 全部の pull request が「issue が無い」に落ち、**断りを書くのが正しい手順になります。**
          #
          # **closingIssuesReferences は REST では返りません。**gh pr view（GraphQL）で引きます。
          #
          # **本文の Closes / Fixes / Resolves は走査しません。**
          # **走査すると、コードの囲みや表の中の文字列まで拾います。**
          # 下のループは目印が1件見つかった時点で通すので、
          # **無関係の issue に目印があると、設計のレビューを1度もせずに緑になります。**
          if ! gh pr view "${PR_NUMBER}" --repo "${REPO}" --json closingIssuesReferences > pr.json; then
            echo "紐づく issue を引けませんでした（権限か gh の版を確かめてください）" \
              | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 1
          fi

          # **このリポジトリの issue だけを残します。**別のリポジトリの issue は、この job の
          # 権限では読めません（private なら 404、public でも投稿者の立場が変わります）。
          #
          # **URL の頭は GITHUB_SERVER_URL から取ります。**https://github.com/ と決め打ちすると、
          # GitHub Enterprise では1件も当たらず、紐づく issue が0件になって必ず落ちます。
          # **一重引用符の中ではシェルの変数が展開されないので、--arg で渡します。**
          jq -r --arg repo "${REPO}" --arg server "${GITHUB_SERVER_URL}" '
            [ .closingIssuesReferences[]
              | select(.url | startswith($server + "/" + $repo + "/issues/"))
              | .number
            ] | unique | .[]' pr.json > issues.txt
          jq -r --arg repo "${REPO}" --arg server "${GITHUB_SERVER_URL}" '
            .closingIssuesReferences[]
            | select(.url | startswith($server + "/" + $repo + "/issues/") | not)
            | .url' pr.json > outside.txt

          # 段3. その issue のどれか1件に、設計のレビュー結果が貼られているか。
          #
          # **1件でよい。**グループでまとめて直す pull request では、代表の issue にだけ
          # 設計が書かれます。全部に求めると、代表以外へ同じものを貼ることになります。
          # **1件が読めなくても、残りを見に行きます。**set -e の下で gh api が落ちると
          # step ごと終わるので、後ろの issue に目印があっても数えません。
          # **読めなかったことは数えておき、落ちるときの案内に出します**
          # （「無かった」と「読めなかった」を人間が見分けられるようにするためです）。
          found=0
          unreadable=0
          marked=0
          while read -r n; do
            [ -n "${n}" ] || continue
            if ! gh api --paginate "repos/${REPO}/issues/${n}/comments?per_page=100" --jq '
              .[]
              | select((.body // "") | test("^[ \\t\\r\\n]*(<!-- continuo:agent -->[ \\t\\r\\n]*)?<!-- design-review-result -->"))
              | if (.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
                then "ok"
                else "login\t" + (.user.login // "") + "\t" + (.author_association // "")
                end' > matched.txt; then
              echo "issue #${n} のコメントを読めませんでした（飛ばします）"
              unreadable=$((unreadable + 1))
              continue
            fi
            # **目印で始まるコメントが在ったかを控えます。**在ったのに数えなかったときは、
            # 「1件もありません」ではなく「投稿者が条件に当たらない」と案内します。
            if [ -s matched.txt ]; then
              marked=$((marked + 1))
            fi
            if poster_counts matched.txt; then
              echo "設計のレビュー結果=有り（issue #${n}）"
              echo "設計のレビュー結果=有り（issue #${n}）" >> "${GITHUB_STEP_SUMMARY}"
              found=1
              break
            fi
          done < issues.txt
          if [ "${found}" -eq 1 ]; then
            exit 0
          fi

          # **落ちたときは、どうすれば通るかを全部書きます。**
          {
            echo "## 設計のレビュー結果が貼られていません"
            echo ""
            if [ "$(wc -l < issues.txt | tr -d ' ')" -eq 0 ]; then
              echo "**この pull request に紐づく issue が1件もありません。**"
              echo ""
              if [ "$(wc -l < outside.txt | tr -d ' ')" -gt 0 ]; then
                echo "別のリポジトリの issue は見つかりましたが、この検査は読めません。"
                sed 's/^/- /' outside.txt
                echo ""
              fi
              echo "本文へ Closes #<番号> を書いてください。"
              echo "設計のレビューが要らない変更のときは、下の断りの段を読んでください。"
            else
              if [ "${marked}" -gt 0 ]; then
                echo "紐づく issue に、次の目印で始まるコメントは在りますが、"
                echo "**数える条件に当たる投稿者のものが1件もありません**（下の「数える条件」）。"
              else
                echo "紐づく issue に、次の目印で始まるコメントが1件もありません。"
              fi
              echo ""
              echo "    <!-- design-review-result -->"
              echo ""
              sed 's/^/- issue #/' issues.txt
              if [ "${unreadable}" -gt 0 ]; then
                echo ""
                echo "**このうち ${unreadable} 件は、コメントを読めませんでした。**"
                echo "**「貼られていない」ではなく「確かめられなかった」です。**"
              fi
            fi
            echo ""
            echo "**通し方。**"
            echo ""
            echo "1. 設計をサブエージェントにレビューさせる"
            echo "2. 指摘ごとに「直すか / 直さないか」と理由を書いた判断票を作る"
            echo "3. それを **issue のコメント**として貼る。**1行目をこの目印にする**"
            echo ""
            echo "    <!-- design-review-result -->"
            echo ""
            echo "   **continuo が起動したエージェントだけは、1行目を <!-- continuo:agent -->、2行目をこの目印にする。**"
            echo "   それ以外（人間や、人間が自分で起動した Claude Code）は <!-- continuo:agent --> を付けない。"
            echo "   付けると、continuo がそのコメントを、走っている run の成果として数えます"
            echo ""
            echo "**設計のレビューが要らない変更のとき**（文書だけの変更、他に影響しない1行の修正）**は、"
            echo "人間が、この pull request のコメントに断りを貼ります。**"
            echo ""
            echo "    <!-- design-review-skipped -->"
            echo "    文書だけの変更のため"
            echo ""
            echo "**2行目の理由を落とさないでください。**目印だけでは通りません。"
            echo ""
            echo "**continuo が起動したエージェントは、この断りを自分で貼りません。**"
            echo "設計のレビューが要らないと人間が判断したときに、人間が貼るものです。"
            echo "continuo がエージェントへ送る指示書が、そう決めています（continuo prompt --show で読めます）。"
            echo ""
            echo "**数える条件。**"
            echo ""
            echo "- 目印が**本文の先頭**にあること。途中に書いたものは数えません"
            echo "  （設計のレビュー結果の目印だけは、<!-- continuo:agent --> の直後でも数えます）"
            echo "- 投稿者の立場が **OWNER / MEMBER / COLLABORATOR** であること。"
            echo "  **どれでもないときは、その投稿者がこのリポジトリへ push できること**"
            echo "- **立場は、この検査のトークンから見えた値で決まります。**同じコメントを"
            echo "  自分のトークンで読むと、別の立場に見えることがあります"
            cat skipped_notes.txt
            poster_notes "設計のレビュー結果（design-review-result）"
            echo ""
            if [ "${IS_DRAFT}" = "true" ]; then
              echo "**この pull request は draft です。**draft のうちは赤のままでかまいません。"
              echo "結果を貼ってから gh pr ready ${PR_NUMBER} を打つと、この検査が回り直します。"
            else
              echo "**この pull request は draft ではありません。**"
              echo "**結果や断りを貼っても、本文へ Closes #<番号> を書き足しても、この検査は回り直しません。**"
              echo "gh run rerun を使うか、commit を1つ push してください。"
            fi
          } | tee -a "${GITHUB_STEP_SUMMARY}"

          exit 1

  # **実装のレビュー結果が、この pull request のコメントに貼られているか。**
  code-review-result:
    runs-on: ubuntu-latest
    steps:
      - name: 実装のレビュー結果が貼ってあるか
        env:
          GH_TOKEN: ${{ github.token }}
          GH_ENTERPRISE_TOKEN: ${{ github.token }}
          REPO: ${{ github.repository }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          IS_DRAFT: ${{ github.event.pull_request.draft }}
        run: |
          set -eu

          # **gh の宛先を、この検査が走っている GitHub にします**（上の job と同じ）。
          GH_HOST="${GITHUB_SERVER_URL#*://}"
          export GH_HOST

          # **数える条件は上の job と同じ2つです。**
          #   一、目印が本文の**先頭**にある（こちらは continuo:agent の直後を許しません）。
          #   二、投稿者の立場が OWNER / MEMBER / COLLABORATOR である。
          #       **どれでもないときは、その投稿者がこのリポジトリへ push できる。**
          # 二の後ろ半分を足した理由は、上の job のコメントに書いてあります。

          # **上の job と同じ3つの関数です。**YAML の job をまたいで共有できないので、
          # 同じものを置いてあります。**中身を変えるときは、両方を直してください。**
          # 理由は上の job のコメントに書いてあります。
          TAB="$(printf '\t')"
          : > perm_denied.txt
          : > perm_missing.txt
          : > perm_errors.txt

          has_pusher() {
            local login assoc can_push rc
            while IFS="${TAB}" read -r login assoc || [ -n "${login}" ]; do
              [ -n "${login}" ] || continue
              rc=0
              can_push=$(gh api "repos/${REPO}/collaborators/${login}/permission" \
                --jq '.user.permissions.push' 2>gh_stderr.txt) || rc=$?
              if [ "${rc}" -ne 0 ]; then
                if grep -q "HTTP 404" gh_stderr.txt; then
                  printf '%s\n' "${login}" >> perm_missing.txt
                else
                  printf '%s\t%s\n' "${login}" "$(tr '\n' ' ' < gh_stderr.txt | cut -c1-200)" >> perm_errors.txt
                fi
                continue
              fi
              case "${can_push}" in
                true) return 0 ;;
                false) printf '%s\t%s\n' "${login}" "${assoc}" >> perm_denied.txt ;;
                *) printf '%s\t権限を表す値が true / false ではありませんでした: %s\n' \
                     "${login}" "${can_push}" >> perm_errors.txt ;;
              esac
            done < "$1"
            return 1
          }

          poster_counts() {
            if grep -q '^ok$' "$1"; then
              return 0
            fi
            awk -F'\t' '$1 == "login" && $2 != "" { print $2 "\t" $3 }' "$1" | sort -u > logins.txt
            has_pusher logins.txt
          }

          poster_notes() {
            if [ -s perm_denied.txt ]; then
              echo ""
              echo "**${1}の目印で始まるコメントは在りますが、次の投稿者のものは数えていません。**"
              echo "立場が OWNER / MEMBER / COLLABORATOR と見えず、このリポジトリへ push もできません。"
              sort -u perm_denied.txt | awk -F'\t' '{ print "- " $1 "（この検査からは " $2 " と見えています）" }'
            fi
            if [ -s perm_missing.txt ]; then
              echo ""
              echo "**${1}の投稿者のうち、次の人は権限の照会が 404 でした。**"
              echo "その名前が存在しないか、この検査のトークンからは見えません。"
              sort -u perm_missing.txt | sed 's/^/- /'
            fi
            if [ -s perm_errors.txt ]; then
              echo ""
              echo "**${1}の投稿者のうち、次の人は push できるかを確かめられませんでした。**"
              echo "**「権限が無い」ではなく「確かめられなかった」です。**"
              echo "一時的な失敗なら、回し直すと直ります。"
              echo "**回し直しても同じなら、この検査のトークンでは権限を照会できません**"
              echo "（fork から来た pull request など）。そのときは、立場が"
              echo "OWNER / MEMBER / COLLABORATOR と見える人に貼ってもらってください。"
              sort -u perm_errors.txt | sed 's/^/- /'
            fi
          }

          # **取得の失敗を守ります。**守らないと step ごと落ち、案内が空のまま赤になります。
          # **守りの中で必ず終わらせます**（理由は上の job の段1に書いてあります）。
          # **数えるのを gh と同じパイプラインに置きません。**gh が落ちても後ろの終了状態が
          # 返るので、**取れなかったのか無かったのかを区別できなくなります。**
          if ! gh api --paginate "repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100" --jq '
            .[]
            | select((.body // "") | test("^[ \\t\\r\\n]*<!-- code-review-result -->"))
            | if (.author_association == "OWNER" or .author_association == "MEMBER" or .author_association == "COLLABORATOR")
              then "ok"
              else "login\t" + (.user.login // "") + "\t" + (.author_association // "")
              end' > matched.txt; then
            echo "この pull request のコメントを読めませんでした（gh run rerun で回し直してください）" \
              | tee -a "${GITHUB_STEP_SUMMARY}"
            exit 1
          fi

          if poster_counts matched.txt; then
            echo "実装のレビュー結果=有り"
            echo "実装のレビュー結果=有り" >> "${GITHUB_STEP_SUMMARY}"
            exit 0
          fi

          {
            echo "## 実装のレビュー結果が貼られていません"
            echo ""
            if [ -s matched.txt ]; then
              echo "この pull request のコメントに、次の目印で始まるものは在りますが、"
              echo "**数える条件に当たる投稿者のものが1件もありません**（下の「数える条件」）。"
            else
              echo "この pull request のコメントに、次の目印で始まるものが1件もありません。"
            fi
            echo ""
            echo "    <!-- code-review-result -->"
            echo ""
            echo "**通し方。**"
            echo ""
            echo "1. コードのレビューを回す"
            echo "2. その結果を、この pull request のコメントとして貼る"
            echo "3. **1行目をこの目印にする**"
            echo ""
            echo "    <!-- code-review-result -->"
            echo ""
            echo "   **continuo が起動したエージェントは、2行目に <!-- continuo:agent --> を置く。**"
            echo "   **設計のレビュー結果（issue に貼るもの）とは、順番が逆です。**"
            echo "   こちらは <!-- continuo:agent --> を1行目に置くと数えません"
            echo ""
            echo "**issue のコメントではありません。**この検査は、この pull request のコメントだけを読みます。"
            echo ""
            echo "**数える条件。**"
            echo ""
            echo "- 目印が**本文の先頭**にあること。途中に書いたものは数えません"
            echo "- 投稿者の立場が **OWNER / MEMBER / COLLABORATOR** であること。"
            echo "  **どれでもないときは、その投稿者がこのリポジトリへ push できること**"
            echo "- **立場は、この検査のトークンから見えた値で決まります。**同じコメントを"
            echo "  自分のトークンで読むと、別の立場に見えることがあります"
            poster_notes "実装のレビュー結果（code-review-result）"
            if [ "${IS_DRAFT}" = "true" ]; then
              echo ""
              echo "**この pull request は draft です。**draft のうちは赤のままでかまいません。"
              echo "結果を貼ってから gh pr ready ${PR_NUMBER} を打ってください。"
            else
              echo ""
              echo "**この pull request は draft ではありません。**"
              echo "gh pr ready を打っても、この検査は回り直しません。"
              echo "gh run rerun を使うか、commit を1つ push してください。"
            fi
          } | tee -a "${GITHUB_STEP_SUMMARY}"

          exit 1
`

// CITemplate は書き出す continuo-ci.yaml の中身を、プレースホルダを埋めずにそのまま返す。
//
// **埋める値はいま0個である**（上の ciTemplate の説明）。
// それでも WORKFLOW.md の雛形と同じ形の口を置いてあるのは、
// **「必要に応じて WORKFLOW.md の内容で置き換えられる形にしておく」と決めたためである。**
// 値を足すときは CITemplateWithValues の側へ書く。
//
// 戻り値: continuo-ci.yaml の全文。
func CITemplate() string {
	return ciTemplate
}

// CITemplateWithValues は、continuo-ci.yaml の雛形に values を埋めて返す。
//
// **いま埋める値は0個なので、values は使わない。**
// 引数を受け取る形にしてあるのは、WORKFLOW.md の雛形（TemplateWithValues）と
// 同じ経路に載せるためである。**片方だけ別の形にすると、値を足すときに
// 呼び出し側を全部書き換えることになる。**
//
// values: WORKFLOW.md の front matter へ埋める値と同じもの。いまは参照しない。
// 戻り値: continuo-ci.yaml の全文。
func CITemplateWithValues(values Values) string {
	_ = values
	return ciTemplate
}
