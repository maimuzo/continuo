package orchestrator

import (
	"context"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
)

// reloadConfig は WORKFLOW.md が変わっていれば読み直し、差し替えてよいキーだけを入れ替える
// （設計 3-24。`SPEC.md` 6.2）。
//
// **巡回の頭で1回だけ呼ぶ。**turn の途中では呼ばない。
//
// **失敗しても巡回は続ける。**`SPEC.md` 6.2 は
// *"Invalid reloads MUST NOT crash the service; keep operating with the last known good
// effective configuration and emit an operator-visible error."*
// （**訳:** 不正な読み直しでサービスを落としてはならない。最後に正常だった実効設定で動き続け、
// オペレータに見えるエラーを出すこと）と定めている。
//
// ctx: 呼び出しに適用するコンテキスト。**いまは使わないが、経路を揃えるために受け取る。**
func (o *Orchestrator) reloadConfig(_ context.Context) {
	if o.configPath == "" {
		return
	}

	stamp, err := config.StampOf(o.configPath)
	if err != nil {
		// **何も言わずに次の巡回へ回す。**エディタが書き換えている最中（rename の途中）は
		// 一時的に読めない。ここで WARN を出すと、保存のたびに1行流れる。
		return
	}

	o.reloadMu.Lock()
	unchanged := o.configStamp.Same(stamp)
	if unchanged {
		o.reloadMu.Unlock()
		return
	}
	// **印は先に進める。**壊れたファイルを保存したまま席を立ったとき、
	// **30秒ごとに同じ読み直しをやり直さない。**人が直せば印が変わるので、次の巡回で読み直す。
	o.configStamp = stamp
	o.reloadMu.Unlock()

	loaded, err := config.Load(o.configPath)
	if err != nil {
		o.noteReload("load-failed:"+err.Error(), func() {
			o.logger.Warn("WORKFLOW.md を読み直せませんでした（いまの設定のまま続けます）",
				"path", o.configPath, "error", err)
		})
		return
	}

	merged, err := config.MergeReloadable(o.cfg, loaded.Config)
	if err != nil {
		// **「あなたのファイルが不正です」と言ってはならない。**
		// 新しいファイル単体は検査を通っており、落ちたのは
		// **いま効いている凍結側の値と混ぜた組み合わせ**である。
		o.noteReload("merge-failed:"+err.Error(), func() {
			o.logger.Warn(
				"読み直した設定を、いま動いている設定と混ぜられませんでした（いまの設定のまま続けます）"+
					"。この組み合わせは読み直しでは作れません（continuo の再起動が要ります）",
				"path", o.configPath, "error", err)
		})
		return
	}

	frozen, err := config.FrozenChanges(merged, loaded.Config)
	if err != nil {
		// **差分を作れなくても差し替えは行う。**報告できないことは、効かせない理由にならない。
		o.logger.Warn("読み直しても効かない項目を数えられませんでした（差し替えは行います）",
			"path", o.configPath, "error", err)
	}
	if o.promptBodyChanged(loaded.PromptTemplate) {
		// **本文は Config の中に無い**ので、設定の差分には出てこない。
		// **出さないと、本文だけを直した人が「効いた」と読む。**
		frozen = append([]config.FrozenChange{{
			Key: config.PromptBodyKey, From: "いまの本文", To: "変わりました",
		}}, frozen...)
	}

	before := o.reloadableConfig()
	after := config.ExtractReloadable(merged)
	applied := diffReloadable(*before, after)

	// **案内を「書かない」と決めた記録を捨てる**（`on_assignee_gate` が変わったときだけ）。
	// **捨てないと、既に止まっている issue には二度と案内が書かれない。**
	// 再起動なら記録ごと消えて書かれるので、**読み直しが再起動より弱くなる。**
	if before.OnAssigneeGate != after.OnAssigneeGate {
		o.forgetGateNoticesOffByConfig()
	}

	o.reloadable.Store(&after)

	o.noteReload(reloadNoteOf(applied, frozen), func() {
		if len(applied) > 0 {
			o.logger.Info("WORKFLOW.md を読み直しました", "path", o.configPath,
				"効いた項目", strings.Join(applied, " / "))
		} else {
			o.logger.Info("WORKFLOW.md を読み直しました（走行中に効く項目の変更はありません）",
				"path", o.configPath)
		}
		if len(frozen) > 0 {
			lines := make([]string, 0, len(frozen))
			for _, c := range frozen {
				lines = append(lines, c.String())
			}
			o.logger.Warn(
				"次の項目は読み直しただけでは効きません（continuo の再起動が要ります）",
				"path", o.configPath, "項目", strings.Join(lines, " / "))
		}
	})
}

// promptBodyChanged は WORKFLOW.md の本文が変わったかを返す。
//
// **中身は持たない。**本文は長く、利用者が何を書いているか分からない。
// **変わったかどうかだけを持つ。**
//
// body: 読み直したファイルの本文。
// 戻り値: いま効いている本文と違えば真。
func (o *Orchestrator) promptBodyChanged(body string) bool {
	return o.promptFragments.BodyChanged(body)
}

// diffReloadable は、実際に効いた項目をドット区切りのキーで並べる。
//
// **キーの名前は front matter と揃える。**利用者が書いた名前で報告する。
//
// before: 差し替える前。
// after: 差し替えたあと。
// 戻り値: 変わった項目のキー（名前順）。
func diffReloadable(before, after config.Reloadable) []string {
	var out []string
	if before.OnAssigneeGate != after.OnAssigneeGate {
		out = append(out, "tracker.provider.handoff.on_assignee_gate")
	}
	if !sameStringMap(before.AutomatedStateRewrite, after.AutomatedStateRewrite) {
		out = append(out, "tracker.automated_state_rewrite")
	}
	if before.MaxConcurrentAgents != after.MaxConcurrentAgents {
		out = append(out, "agent.max_concurrent_agents")
	}
	if !sameIntMap(before.MaxConcurrentAgentsByState, after.MaxConcurrentAgentsByState) {
		out = append(out, "agent.max_concurrent_agents_by_state")
	}
	return out
}

// sameStringMap は2つの map が同じ中身かを返す。
func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// sameIntMap は2つの map が同じ中身かを返す。
func sameIntMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// reloadNoteOf は、この読み直しの知らせを1つの文字列にする（同じ知らせを出し続けないため）。
func reloadNoteOf(applied []string, frozen []config.FrozenChange) string {
	parts := make([]string, 0, len(applied)+len(frozen)+1)
	parts = append(parts, "ok")
	parts = append(parts, applied...)
	for _, c := range frozen {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, "\x00")
}

// noteReload は、前の巡回と違う知らせのときだけ出す。
//
// **同じ知らせを出し続けない。**巡回の間隔の既定は30秒であり、壊れたファイルを保存したまま
// 席を立つと、同じ WARN が永久に流れて他の行が埋もれる
// （`internal/tracker/adapter.go` の「10分に1回同じ行が流れると他の行が埋もれる」と同じ理由）。
//
// note: この巡回の知らせ（前と同じかを比べるためだけに使う）。
// emit: 実際に出す処理。
func (o *Orchestrator) noteReload(note string, emit func()) {
	o.reloadMu.Lock()
	same := o.reloadNote == note
	o.reloadNote = note
	o.reloadMu.Unlock()
	if same {
		return
	}
	emit()
}
