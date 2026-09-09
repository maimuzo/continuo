package doctor

import (
	"strconv"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// checkRewriteKeys は `tracker.automated_state_rewrite` のキーがカンバンの Status の
// 選択肢にあるかを見る（見出し語 `対応表のキー`。設計 3-57。issue #67）。
//
// **判定は書き直さない。**internal/config の `RewriteKeysOutsideBoard` をそのまま呼ぶ。
// 起動時の警告（internal/tracker の `missingRewriteKeys`）も同じ関数を呼んでいる。
// **違うのは出し方だけである。**
//
// **記号は `✗` ではなく `!` にする。**キーはカンバンに実在しなくてよい。
// **`✗` にすると、カンバンの自動化をやめて選択肢を消した人が抜け出せなくなる**（issue #67）。
//
// **なぜこの見出し語が要るのか。**キーの綴りを打ち間違えると、対応表のその行は
// 一度も引かれないまま黙って死ぬ。起動時には警告が出るが、**`continuo doctor` は
// tracker のログを捨てる**ので（`Options.Logger` の既定は `io.Discard`）、
// **doctor に項目が無いと、人間はどこでも打ち間違いに気づけない。**
//
// **見出し語 `Status の名前` では拾えない。**あちらが拾うのは「区切りを落とすと同じ綴り」か
// 「一方が他方を語の並びとして丸ごと含む」だけで、`In Progres` と `In Progress` は
// **どちらにも当たらない。**
//
// **カンバンを読んだときの応答を使い回すので、リクエストは増えない。**
//
// cfg: 読めた場合の設定。
// boardOptions: カンバン側の Status の選択肢名（tracker.Adapter.StatusOptionNames の戻り値）。
// boardSymbol: 上流（カンバン）の記号。
// 戻り値: 検査結果。
func checkRewriteKeys(cfg loadedConfig, boardOptions []string, boardSymbol Symbol) Result {
	if !cfg.OK {
		return Result{
			Label:  LabelRewriteKeys,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorRewriteKeysConfigUnreadable),
		}
	}
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelRewriteKeys,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorRewriteKeysBoardUnreadable),
		}
	}
	// **対応表そのものが空なら、確かめるものが無い。**ここで注意を出すと、
	// 書き戻しを使っていない人が毎回読み飛ばす注意を1件抱える。
	if len(cfg.Config.Tracker.AutomatedStateRewrite) == 0 {
		return Result{
			Label:  LabelRewriteKeys,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorRewriteKeysEmpty),
		}
	}

	missing := config.RewriteKeysOutsideBoard(cfg.Config.Tracker, boardOptions)
	if len(missing) == 0 {
		return Result{
			Label:  LabelRewriteKeys,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorRewriteKeysOK, len(cfg.Config.Tracker.AutomatedStateRewrite)),
		}
	}

	// **どのキーかを必ず名前で出す。**件数だけでは、どの行を直せばよいか分からない
	// （見出し語 `片付けの状態` と `Status の名前` と同じ流儀である）。
	notes := make([]string, 0, len(missing))
	remedies := make([]string, 0, len(missing))
	for _, key := range missing {
		quoted := strconv.Quote(key)
		notes = append(notes, i18n.T(i18n.KeyDoctorRewriteKeysNote, quoted))
		remedies = append(remedies, i18n.T(i18n.KeyDoctorRewriteKeysRemedy, quoted, quoted))
	}
	return Result{
		Label:    LabelRewriteKeys,
		Symbol:   SymbolUnknown,
		Detail:   i18n.T(i18n.KeyDoctorRewriteKeysMissing, len(missing)),
		Notes:    notes,
		Remedies: remedies,
	}
}
