package doctor

import (
	"os"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// checkMissingKeys は、雛形にあって WORKFLOW.md に書かれていない設定項目を見る
// （見出し語 `未記入の項目`。設計 3-73。issue #85）。
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
// **出すのは差分そのものと、それを当てるコマンドの2つである。**キーの名前だけを並べても、
// **何を書ける項目なのかが分からない。**差分には雛形のコメントがそのまま入っている。
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
		Label:    LabelMissingKeys,
		Symbol:   SymbolUnknown,
		Detail:   i18n.T(i18n.KeyDoctorMissingKeysMissing, len(res.Keys), res.Total),
		Notes:    patchNotes(res.Patch),
		Remedies: []string{i18n.T(i18n.KeyDoctorMissingKeysRemedy, opts.ConfigPath)},
	}
}

// patchNotes は unified diff を、内訳として1行ずつ並べられる形にする。
//
// **差分をそのまま見せる。**足すキーの名前も、雛形が持つ説明のコメントも、
// `+` の行に全部入っている。**別に一覧を並べると同じものを二度書くことになる。**
//
// patch: 組み立てた unified diff。
// 戻り値: 1行ずつに分けた差分（末尾の空行は落とす）。
func patchNotes(patch string) []string {
	trimmed := strings.TrimRight(patch, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
