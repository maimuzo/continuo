package scaffold

// localTemplateFileName は `continuo init` が置く3枚目のファイルの名前である（設計 5-1a / 5-3o）。
//
// **continuo はこの名前のままでは読まない。**重ねるのは `WORKFLOW.local.md` だけである
// （config.LocalPathFor）。**置いた瞬間に効かせないための名前である。**
// 効かせると、`continuo init` を叩いただけで、チームの WORKFLOW.md の値が利用者の知らないうちに変わる。
//
// **名前は「重ねるファイルの名前 + `.template`」に揃える。**利用者は `cp -n` で写すだけで使える（`mv` を勧めない。既にある WORKFLOW.local.md を黙って上書きするため）。
// 揃っていることは test/internal/scaffold/local_template_test.go が config.LocalPathFor と突き合わせる。
const localTemplateFileName = "WORKFLOW.local.md.template"

// localTemplate は `continuo init` が置く WORKFLOW.local.md の雛形である（issue #325）。
//
// **中身は、1週間の枠のリセットまでの残り日数ごとのマージンの表だけである**
// （`tracker.provider.handoff.weekly_margin_percent_by_days_until_reset`。issue #324 / 設計 3-27a）。
// 週のリセットの時刻は人ごとに違うので、チームで共有する WORKFLOW.md ではなく、
// 自分の機械だけの WORKFLOW.local.md に書くのが向いている（人間の依頼）。
//
// **表は注釈にせず、有効な形で書く。**写しただけで効くようにするためである（人間の依頼「上書きする設定を入れておいて」）。
// 写すだけで残り1日のマージンが 0% になるので、頭書きで「要らなければ表ごと消す」を知らせる。
//
// **値（0・5・10）は WORKFLOW.md の雛形の同じ表の見本と揃える**（template.go）。
// 揃っていることは test/internal/scaffold/local_template_test.go が確かめる。
//
// **本文は空にする。**空なら WORKFLOW.md の本文がそのまま送られる（設計 5-1a の「本文」。prompt.BodyOf）。
// 本文を書くと WORKFLOW.md の本文と丸ごと差し替わり、チームが足した決まりが届かなくなる。
//
// **プレースホルダを持たない。**`continuo init` が gh から引いた値は WORKFLOW.md へだけ埋める
// （ここに owner を書くと、チームの値を個人の値で上書きしてしまう）。
//
// **説明は WORKFLOW.md の雛形の注釈より短くする。**残り日数の数え方などの細かい説明は、
// WORKFLOW.md の雛形の同じ表の注釈と docs/FAQ.md にある。ここに書き写すと、片方だけ直って食い違う。
const localTemplate = `---
# WORKFLOW.local.md の雛形です。continuo は、この名前（末尾が .template）のままでは読みません。
# 使うときは、同じディレクトリで cp -n WORKFLOW.local.md.template WORKFLOW.local.md を叩いて写し、値を自分の値へ直してください。
#   mv で名前を変えると、既にある WORKFLOW.local.md を黙って上書きします。cp -n は既にあれば写しません。
# WORKFLOW.local.md は、自分の機械だけの上書きです。ここに書いたキーだけが、隣の WORKFLOW.md の値を上書きします。
#   書き方は WORKFLOW.md と同じです。WORKFLOW.local.md はチームと共有しないので、.gitignore に入れてください。
# 写しただけで、下の表がそのまま効きます。要らない日の行は消し、表が要らなければ表ごと消してください。
# 書き換えたら continuo を立て直してください（動いている continuo には効きません）。
# 表の細かい説明（残り日数の数え方など）は、continuo の docs/FAQ.md の「1週間のマージンを、リセットの直前だけ小さくしたい」にあります。
tracker:
  provider:
    handoff:
      weekly_margin_percent_by_days_until_reset:  # 1週間の枠のリセットまでの残り日数（1〜7）ごとのマージン（単位は %。% を付けずに 0以上100未満の整数）。
                                                  # 書かない日は weekly_margin_percent を使う。
                                                  # ここに表を書くと、WORKFLOW.md の表とは日ごとに混ざらず、丸ごと置き換わる。
        1: 0      # 残り1日（リセットの24時間前からリセットまで）はマージン 0%。使用率100%まで使う
        2: 5      # 残り2日（リセットの48時間前から24時間前まで）はマージン 5%
        3: 10     # 残り3日はマージン 10%
        4: 10     # 残り4日はマージン 10%
        5: 10     # 残り5日はマージン 10%
        6: 10     # 残り6日はマージン 10%
        7: 10     # 残り7日（リセットした直後から、次のリセットの144時間前まで）はマージン 10%
---
`

// LocalTemplate は `continuo init` が置く WORKFLOW.local.md の雛形の中身を返す。
//
// **テストが雛形を WORKFLOW.local.md として置き、config.Load に読ませるために要る。**
//
// 戻り値: 雛形の全文。
func LocalTemplate() string { return localTemplate }

// LocalTemplateFileName は `continuo init` が置く3枚目のファイル名である。
//
// **CLI が案内と失敗の文言で名乗るために要る。**
//
// 戻り値: WORKFLOW.local.md.template。
func LocalTemplateFileName() string { return localTemplateFileName }
