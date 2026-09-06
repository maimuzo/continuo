package doctor

import (
	"strconv"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
)

// automationsNoteLimit は内訳に並べる自動化の名前の上限である。
//
// **10 にするのは、見出し語 `未記入の項目` と同じ理由である**（missingKeysNoteLimit）。
// **この検査1つで検査結果の並びを埋めない。**GitHub の組み込みの自動化は
// 2026-09-05 時点で10個前後だが、**上限が無いと、増えたときに他の検査が画面の外へ出る。**
const automationsNoteLimit = 10

// checkAutomations は、カンバンの自動化が有効なのに書き戻しの対応表が空でないかを見る
// （見出し語 `自動化`。設計 3-32 / 3-54。issue #209）。
//
// **何が起きるのか。**カンバンの組み込みの自動化（`Pull request linked to issue` など）が
// Status を書くと、continuo はそれを「知らない Status」と読んで**走行中の run を止める**
// （internal/orchestrator の `handleUnknownState`）。**止めずに続けさせる唯一の設定が
// `tracker.automated_state_rewrite` である**（設計 3-54）。
// **これが空のままで自動化を有効にしていると、PR を issue へ紐づけた瞬間に run が止まる。**
//
// **利用者は、止まってから初めて気づく。**引き渡しのコメントには足す2行まで書いてあるが、
// **それが出るのは1件止まったあとである。**しかも設定は落ちる（`WORKFLOW.md` を作り直すと
// 消える）。**`continuo doctor` は起動する前に叩くものなので、ここが気づく時点を前へ出す
// 唯一の場所である。**
//
// **条件は2つだけである。**
//
//	1 有効な自動化が1つ以上ある
//	2 `tracker.automated_state_rewrite` が空である
//
// **雛形が同じことを書いている。**`internal/scaffold/template.go` の
// `automated_state_rewrite` の説明が「カンバンの Settings → Workflows で Status を書く
// 自動化を1つも有効にしていないなら、空のままでよい。**有効にしているなら書く。**」である。
// **この検査は、その1文を機械で確かめているだけである。**
//
// **「どの自動化が Status を書くか」で絞り込まない。**`ProjectV2Workflow` が公開している
// のは名前と有効かどうかだけで（2026-09-05 に introspection で確認）、
// **どの Status を書くかを返すフィールドは1つも無い。**組み込みの自動化の名前の一覧も
// 公式に固定されていないので、**名前で当たりを付けると、外れたときに静かに取りこぼす。**
// **有効な自動化の名前を全部内訳へ出し、判断の材料を人間へ渡す。**
//
// **例外は1つだけである。**item を載せるだけで Status を1文字も書かない自動化
// （`Auto-add …`）は数えない（`addOnlyWorkflowPrefix`）。**除く向きにしか名前を使わない。**
//
// **「カンバンの Status の選択肢のうち、設定に名前が無いもの」では判定しない。**
// その一覧は `Ice Box` や `Backlog` のような、自動化と何の関係も無い Status も拾う。
// **直す先の無い `!` を毎回出すことになる**（見出し語 `対応表のキー` が
// 「対応表が空なら注意を出さない」と決めたのと同じ判断である）。
//
// **この検査が見ていない範囲を、`✓` にも `!` にも1行添える。**
// 自動化が終端の Status（`tracker.terminal_states`）や引き渡しの Status
// （`tracker.status_signal_map` の遷移先）を書く場合、run は止まるが**書き戻しでは救えない**
// （設計 3-55 が「PR のマージで `Done` になる件（issue #35）は、この仕組みでは解かない」と
// 決めており、対応表のキーには設定の他のキーに出てくる Status を書けない）。
// **見ていないことを `✓` で断言しない。**
//
// **記号は `✗` ではなく `!` にする。**対応表が空でも continuo は起動し、走る。
// **`✗` にすると、自動化を有効にしたまま動かしている人の continuo が、版を上げた瞬間に
// 起動しなくなる**（見出し語 `片付けの状態` と `未記入の項目` と同じ理由である）。
//
// **自動化は、カンバンを読むのとは別のリクエストで取る**（`Adapter.FetchProjectWorkflows`）。
// **起動時の検査のクエリへ混ぜてはならない。**あちらは GraphQL が `errors` を1件でも
// 返した時点で落ちるので、`workflows` を読めない環境（権限の足りないトークン・
// この field を持たない GitHub Enterprise Server）では**常駐プロセスが起動しなくなる。**
//
// cfg: 読めた場合の設定。
// workflows: カンバンの自動化の一覧（`tracker.Adapter.FetchProjectWorkflows` の戻り値）。
// **nil は「読めなかった」である。**長さ0の「1件も無い」と取り違えてはならない。
// boardSymbol: 上流（カンバン）の記号。
// 戻り値: 検査結果。
func checkAutomations(cfg loadedConfig, workflows []tracker.ProjectWorkflow, boardSymbol Symbol) Result {
	if !cfg.OK {
		return Result{
			Label:  LabelAutomations,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorAutomationsConfigUnreadable),
		}
	}
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelAutomations,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorAutomationsBoardUnreadable),
		}
	}
	if workflows == nil {
		// **読めなかった。**リクエストそのものが落ちた場合と、応答に `workflows` が
		// 入っていなかった場合の両方がここへ来る（GitHub Enterprise Server や、
		// 権限が足りないトークン）。**`✓` にしてはならない。**確かめていないものを通したことになる。
		return Result{
			Label:  LabelAutomations,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorAutomationsUnreadable),
		}
	}

	enabled := enabledWorkflowNames(workflows)
	if len(enabled) == 0 {
		return Result{
			Label:  LabelAutomations,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorAutomationsNoneEnabled),
		}
	}

	if len(cfg.Config.Tracker.AutomatedStateRewrite) > 0 {
		// **書いてある行が正しいかは、ここでは見ない。**キーがカンバンの選択肢にあるかは
		// 見出し語 `対応表のキー` が、綴りの紛らわしさは見出し語 `Status の名前` が受け持つ。
		return Result{
			Label:  LabelAutomations,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorAutomationsOK,
				len(enabled), len(cfg.Config.Tracker.AutomatedStateRewrite)),
			Notes: []string{i18n.T(i18n.KeyDoctorAutomationsNoteScope)},
		}
	}

	return Result{
		Label:  LabelAutomations,
		Symbol: SymbolUnknown,
		Detail: i18n.T(i18n.KeyDoctorAutomationsEmptyTable, len(enabled)),
		Notes:  append(workflowNotes(enabled), i18n.T(i18n.KeyDoctorAutomationsNoteScope)),
		Remedies: []string{
			i18n.T(i18n.KeyDoctorAutomationsRemedyRewrite),
			i18n.T(i18n.KeyDoctorAutomationsRemedyDisable),
			i18n.T(i18n.KeyDoctorAutomationsRemedyIgnore),
		},
	}
}

// addOnlyWorkflowPrefix は、item をカンバンへ載せるだけで Status を1文字も書かない
// 組み込みの自動化の名前の頭である。
//
// **2026-09-05 時点で、この頭を持つ組み込みの自動化は2つある。**
//
//	Auto-add to project            … 条件に合う issue をカンバンへ載せる
//	Auto-add sub-issues to project … 親 issue の sub-issue をカンバンへ載せる
//
// **どちらも Status を書かない。**載せた item に Status を書くのは、
// **別の自動化 `Item added to project` である**（この頭を持たないので、除かれない）。
//
// **除く向きにしか使わない。**「これは Status を書く」と名前で当てにいくと、
// 外れたときに**静かに取りこぼす。**「これは書かない」を外すと、余分な `!` が1行出るだけである。
// **安いほうの誤りに倒してある。**
const addOnlyWorkflowPrefix = "Auto-add "

// enabledWorkflowNames は Status を書きうる有効な自動化の名前を、GitHub が返した順のまま返す。
//
// **並べ替えない。**GitHub の画面に出る順（自動化の番号順）と揃えておくと、
// 人間が画面と見比べやすい。
//
// **item を載せるだけの自動化は数えない**（addOnlyWorkflowPrefix）。
// **数えると、この検査はほとんどの利用者に直す先の無い `!` を出す。**
// カンバンへ issue を載せる標準の手段なので、**多くの人が有効にしている。**
// 実測: このリポジトリのカンバンで有効な自動化は
// `Auto-add sub-issues to project` と `Auto-add to project` の2件だけで、
// **どちらも Status を書かない**（2026-09-05）。
//
// workflows: カンバンの自動化の一覧。
// 戻り値: Status を書きうる有効なものの名前（GitHub の綴りのまま）。
func enabledWorkflowNames(workflows []tracker.ProjectWorkflow) []string {
	names := make([]string, 0, len(workflows))
	for _, w := range workflows {
		if !w.Enabled {
			continue
		}
		if strings.HasPrefix(w.Name, addOnlyWorkflowPrefix) {
			continue
		}
		names = append(names, w.Name)
	}
	return names
}

// workflowNotes は有効な自動化の名前を内訳の行にする。
//
// **上限で切る**（automationsNoteLimit）。切ったぶんは件数だけを1行で出す。
//
// names: 有効な自動化の名前。
// 戻り値: 内訳の行。
func workflowNotes(names []string) []string {
	shown := names
	var extra int
	if len(names) > automationsNoteLimit {
		shown = names[:automationsNoteLimit]
		extra = len(names) - automationsNoteLimit
	}
	notes := make([]string, 0, len(shown)+1)
	for _, name := range shown {
		notes = append(notes, i18n.T(i18n.KeyDoctorAutomationsNoteWorkflow, strconv.Quote(name)))
	}
	if extra > 0 {
		notes = append(notes, i18n.T(i18n.KeyDoctorAutomationsNoteMore, extra))
	}
	return notes
}
