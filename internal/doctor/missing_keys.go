package doctor

import (
	"os"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// checkMissingKeys は、雛形にあって WORKFLOW.md に書かれていない設定項目を見る
// （見出し語 `未記入の項目`。設計 3-75。issue #85）。
//
// **判定は書き直さない。**internal/scaffold の `MissingKeys` をそのまま呼ぶ。
// `continuo doctor --missing-keys-patch` も同じ関数を呼んでいる。
// **違うのは出し方だけである。**
//
// **なぜこの見出し語が要るのか。**版を上げて設定項目が増えても、利用者の
// `WORKFLOW.md` にはその項目が無い。**Go が持つ既定値が黙って使われ、doctor は
// 何も言わない。**リリースノートは1回きりなので、**読み飛ばした人には、
// その項目があること自体を知る手段が1つも無い**（issue #85）。
//
// **記号は `✗` ではなく `!` にする。**書かれていなくても continuo は起動し、走る。
// **起動を止めると、いま動いている人の continuo が版を上げた瞬間に起動しなくなる。**
//
// **黙らせる手段は作らない。**「要らないから書いていない」と「知らないから書いていない」は
// 機械には区別できない。**足りないものは足りないものとして、直すまで毎回出す。**
//
// **内訳に差分そのものは出さない。**差分は長い（実測: 版を1つ上げて増えた3項目で30行、
// 手で書いた短い `WORKFLOW.md` で156行）。**そのまま並べると、他の17個の検査結果が
// 画面の外へ押し出される。**内訳には**足りない項目の名前だけ**を出し、
// **差分を読むコマンドと当てるコマンドを直し方に出す。**
//
// **名前も上限で切る**（missingKeysNoteLimit）。**名前だけなら1件1行なので短いが、
// `continuo init` を使わずに手で書いた人では実測で31件になる。**
//
// opts: 検査の入力（`ConfigPath` だけを使う）。
// cfg: 読めた場合の設定。**中身は見ない**（読めたかどうかだけを使う）。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkMissingKeys(opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK || !cfg.OK {
		return Result{
			Label:  LabelMissingKeys,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorMissingKeysConfigUnreadable),
		}
	}

	// **設定を読み直す。**`config.Load` が返すのは検証済みの構造体であり、
	// **書かれていないキーと、既定値で埋まったキーを区別できない。**
	// 区別できるのは原文だけである。
	raw, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return Result{
			Label:  LabelMissingKeys,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorMissingKeysUnreadable, err),
		}
	}

	res, err := scaffold.MissingKeys(opts.ConfigPath, string(raw))
	if err != nil {
		return Result{
			Label:  LabelMissingKeys,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorMissingKeysUnreadable, err),
		}
	}
	if len(res.Keys) == 0 {
		return Result{
			Label:  LabelMissingKeys,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorMissingKeysOK, res.Total),
		}
	}

	return Result{
		Label:  LabelMissingKeys,
		Symbol: SymbolUnknown,
		Detail: i18n.T(i18n.KeyDoctorMissingKeysMissing, len(res.Keys), res.Total),
		Notes:  missingKeyNotes(res.Keys),
		Remedies: []string{
			i18n.T(i18n.KeyDoctorMissingKeysRemedyShow, opts.ConfigPath),
			// **当てる相手を `patch` の引数でも名指しする。**差分の `---` / `+++` の行は
			// WORKFLOW.md の絶対パスなので、**GNU patch はそれを「いまいるディレクトリの外」
			// として捨てる**（設計 3-75c）。引数で名指しすれば、GNU patch でも
			// macOS の Apple patch でも当たる。
			i18n.T(i18n.KeyDoctorMissingKeysRemedyApply, opts.ConfigPath, opts.ConfigPath),
		},
	}
}

// missingKeysNoteLimit は内訳に並べる項目の名前の上限である。
//
// **10 にするのは、この検査1つで検査結果の並びを埋めないためである。**
// `continuo doctor` は18個の検査を出す。**内訳がそれと同じ高さを超えると、
// 何を見ている画面なのかが分からなくなる。**
//
// **版を1つ上げて増える項目は、実測で最大3件である**（v0.1.5 から v0.1.9 まで
// 上げたときの `tracker.unknown_state_grace_ms` / `tracker.automated_state_rewrite` /
// `workspace.on_broken_worktree`）。**上限に当たるのは、`continuo init` を使わずに
// 手で書いた `WORKFLOW.md` だけである**（実測31件）。
const missingKeysNoteLimit = 10

// missingKeyNotes は足りない項目の名前を、内訳として並べられる形にする。
//
// **差分は入れない。**差分は長すぎて他の検査結果を画面の外へ押し出す。
// **差分を読むコマンドは直し方に出してある。**
//
// keys: 足りない項目の名前（`tracker.automated_state_rewrite` のようなドット区切り）。
// 戻り値: 1行ずつに分けた内訳。**上限を超えたぶんは最後の1行にまとめる。**
func missingKeyNotes(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	if len(keys) <= missingKeysNoteLimit {
		return append([]string(nil), keys...)
	}
	notes := append([]string(nil), keys[:missingKeysNoteLimit]...)
	return append(notes, i18n.T(i18n.KeyDoctorMissingKeysMore, len(keys)-missingKeysNoteLimit))
}
