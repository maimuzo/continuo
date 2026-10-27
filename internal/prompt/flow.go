package prompt

import (
	"fmt"
	"strings"

	"github.com/maimuzo/continuo/plugins"
)

// このファイルは、システムプロンプトの「フローチャート」「対応表」「操作の前に読むスキルの表」を
// **1つの定義から作る**（issue #288）。
//
// **3つを別々に書かない。**設計レビューの2周目で、段で呼ぶスキルを対応表とフローチャートに別々に書いた案が
// 6か所で食い違い、push と終了の宣言が要る箱で読み込まれなかった。
//
// **フローチャートの箱と矢印は、人間が確定させた指示書の流れ図（commit 68596d21 の builtin.md）をそのまま使う。**
// 箱の文の括弧書き（節の名前での参照）を、その箱の入口のスキルの名前「」に置き換えた。
// 途中経過の点線の箱だけを「どの段でも」の箱に置き換えた。

// スキルの名前である。**plugins/continuo-agent-procedures/skills/ の下のディレクトリの名前と同じである**
// （テストが、ここに出る名前が全部実在することを確かめる）。
const (
	SkillStartingRun              = "starting-run"
	SkillTrustingInstructions     = "trusting-instructions"
	SkillWritingPlan              = "writing-plan"
	SkillRunningReviewLoop        = "running-review-loop"
	SkillBriefingSubagents        = "briefing-subagents"
	SkillPostingReviewVerdict     = "posting-review-verdict"
	SkillImplementingAndPushing   = "implementing-and-pushing"
	SkillFinishingPullRequest     = "finishing-pull-request"
	SkillReportingProgress        = "reporting-progress"
	SkillWritingCommentsForHumans = "writing-comments-for-humans"
	SkillFinishingRun             = "finishing-run"
)

// Skills は、対応表に並べる順のスキルの名前である。
//
// **11本である。**入口の箱を持つ7本を流れの順に、操作の前と名前でたどられたときに読む4本をその後ろに並べる。
var Skills = []string{
	SkillStartingRun,
	SkillWritingPlan,
	SkillRunningReviewLoop,
	SkillImplementingAndPushing,
	SkillFinishingPullRequest,
	SkillFinishingRun,
	SkillReportingProgress,
	SkillTrustingInstructions,
	SkillBriefingSubagents,
	SkillPostingReviewVerdict,
	SkillWritingCommentsForHumans,
}

// FlowNode はフローチャートの箱1つである。
type FlowNode struct {
	// ID は mermaid の箱の名前である。対応表の「入口として使う箱」にも出る。
	ID string
	// Text は箱の文である（スキルの名前を除く）。
	Text string
	// Skill はその箱の入口のスキルである。**無い箱は空文字である。**
	Skill string
	// Decision は分かれ道（菱形）の箱なら真である。
	Decision bool
	// SkillInText は、スキルの名前が Text の中に既に書いてあるなら真である（末尾に足さない）。
	SkillInText bool
	// Anywhere は「どの段でも」の箱なら真である（対応表には「どの段でも」と出す）。
	Anywhere bool
}

// FlowEdge はフローチャートの矢印1つである。
type FlowEdge struct {
	// From と To は箱の ID である。
	From, To string
	// Label は矢印の文である。無ければ空文字。
	Label string
}

// OperationRule は「この操作をする前に、このスキルを呼ぶ」の1行である。
type OperationRule struct {
	// Before は、その操作である。
	Before string
	// Skills は呼ぶスキルの並びである。
	Skills []string
	// Notes は、スキルごとの添え書きである（スキルの名前 → 括弧の中の文）。無ければ添えない。
	Notes map[string]string
}

// anywhereText は「どの段でも」の箱の文である。
//
// **マージンの確かめは、必ず守らせる側（システムプロンプト）に置く**（2026-10-17 の人間の指示）。
// スキルの側へ置くと、エージェントがそのスキルを呼ばない限り、1度も確かめない。
// **マージンの1文だけを、`margin_status_path` があるときに出す**（いまの `{{if .margin_status_path}}` と同じ）。
const anywhereText = "どの段でも（着手のときに呼び、以後は時刻を確かめるたびに入る）: " +
	"長い作業の前と区切りごとに date で時刻を確かめ、{{.progress_interval_minutes}}分黙っていたら途中経過を書く。" +
	"{{if .margin_status_path}}時刻を確かめるとき・subagent を起動する前・長いコマンドを叩く前に " +
	"cat '{{.margin_status_path}}'; date +%s を見て、1行目が MARGIN: cut で始まり、その expires_at= の数が 2行目より大きければ切り上げる。{{end}}" +
	"「" + SkillReportingProgress + "」。判断に迷ったら止める（止めるときは操作の前の表のとおり）"

// reviewFixText は、レビューの指摘を直す箱の文である（設計と実装で同じ）。
const reviewFixText = "使い捨てのプランファイルに指摘をまとめ、関連処理への影響と一緒に直す箇所を並べてから直す"

// FlowNodes はフローチャートの箱である。**並びは、mermaid に書く順である。**
var FlowNodes = []FlowNode{
	{ID: "Y", Text: "担当が自分のままかを確かめる", Skill: SkillStartingRun},
	{ID: "Z", Text: "worktree の分岐元を取り込む", Skill: SkillStartingRun},
	{ID: "A", Text: "issue と、紐づく pull request を読む", Skill: SkillStartingRun},
	{ID: "B", Text: "関連するプランファイルと過去の issue を読む", Skill: SkillStartingRun},
	{ID: "C", Text: "設計: 計画を書き、計画のコメントとして issue へ書く", Skill: SkillWritingPlan},
	{ID: "HC", Text: "人間確認: 報告のコメントで訊き、blocked で止まる", Skill: SkillWritingPlan},
	{ID: "D", Text: "設計レビューループ", Skill: SkillRunningReviewLoop},
	{ID: "DP", Text: reviewFixText, Skill: SkillRunningReviewLoop},
	{ID: "F", Text: "実装する", Skill: SkillImplementingAndPushing},
	{ID: "G", Text: "commit して push する（最初の push で、draft の pull request を作る）", Skill: SkillImplementingAndPushing},
	{ID: "H", Text: "pull request の題名と本文を書き直す", Skill: SkillFinishingPullRequest},
	{ID: "I", Text: "実装レビューループ", Skill: SkillRunningReviewLoop},
	{ID: "IP", Text: reviewFixText, Skill: SkillRunningReviewLoop},
	{
		ID:          "P",
		Text:        "実装レビューを回した pull request が draft なら、必ず外す（外さない3つの場合は「" + SkillFinishingPullRequest + "」）",
		Skill:       SkillFinishingPullRequest,
		SkillInText: true,
	},
	{ID: "J2", Text: "まとめて直した issue ごとに、その issue へ書く", Skill: SkillFinishingRun},
	{ID: "K", Text: "何をしたかを issue へ書く", Skill: SkillFinishingRun},
	{ID: "L", Text: "CONTINUO-STATUS を1行書いて終わる", Skill: SkillFinishingRun},
	{ID: "X", Text: "人間へ方針を訊く報告のコメントを書き、blocked で止まる", Skill: SkillRunningReviewLoop},
	{ID: "Q", Text: "その周を打ち切り、報告のコメントで質問し、blocked で止まる", Skill: SkillRunningReviewLoop},
	{ID: "R", Text: "人間が次にすることを明示したか", Skill: SkillRunningReviewLoop, Decision: true},
	{ID: "R1", Text: "そのとおりに進める"},
	{ID: "R1S", Text: "指示された段へ"},
	{
		ID:    "R2",
		Text:  "設計だけ見直して新しい計画のコメントを書き、報告のコメントで設計レビューから回し直してよいかを訊き、blocked で止まる",
		Skill: SkillWritingPlan,
	},
	{ID: "ANY", Text: anywhereText, Skill: SkillReportingProgress, SkillInText: true, Anywhere: true},
}

// FlowEdges はフローチャートの矢印である。**並びは、人間が確定させた流れ図と同じである。**
var FlowEdges = []FlowEdge{
	{From: "Y", To: "Z"},
	{From: "Z", To: "A"},
	{From: "A", To: "B"},
	{From: "B", To: "C"},
	{From: "C", To: "HC"},
	{From: "HC", To: "D", Label: "はっきり了承された"},
	{From: "HC", To: "C", Label: "直しを求められた"},
	{From: "D", To: "DP", Label: "収まっていない"},
	{From: "DP", To: "D"},
	{From: "D", To: "F", Label: "収まった"},
	{From: "F", To: "G"},
	{From: "G", To: "H"},
	{From: "H", To: "I"},
	{From: "I", To: "IP", Label: "収まっていない"},
	{From: "IP", To: "I"},
	{From: "I", To: "P", Label: "収まった"},
	{From: "P", To: "J2"},
	{From: "J2", To: "K"},
	{From: "K", To: "L"},
	{From: "D", To: "X", Label: "10周で収まらない"},
	{From: "I", To: "X", Label: "10周で収まらない"},
	{From: "X", To: "R", Label: "人間が回答した"},
	{From: "D", To: "Q", Label: "人間に訊くことが出た"},
	{From: "I", To: "Q", Label: "人間に訊くことが出た"},
	{From: "Q", To: "R", Label: "人間が回答した"},
	{From: "R", To: "R1", Label: "明示した"},
	{From: "R1", To: "R1S"},
	{From: "R", To: "R2", Label: "明示していない"},
	{From: "R2", To: "D", Label: "はっきり了承された"},
	{From: "R2", To: "C", Label: "直しを求められた"},
}

// OperationRules は「操作の前に読むスキル」の表である。
//
// **ほとんどの箱で要る手順（止まり方・push・信頼の判定）は、箱ではなく操作に結びつける**
// （設計レビューの4周目。箱ごとに並べ尽くす作りは毎周漏れた）。
//
// **「ファイルを書き換える」は worktree の中のものに絞る**（設計レビューの6周目）。
// 計画のコメントを書くための一時ファイルにまで当てると、設計の段でも実装のスキルを呼び込む。
var OperationRules = []OperationRule{
	{
		Before: "止まる・終えると決めたら（報告のコメントを書く前に。`CONTINUO-STATUS:` の行はその最後）",
		Skills: []string{SkillFinishingRun, SkillImplementingAndPushing},
		Notes:  map[string]string{SkillImplementingAndPushing: "止まる前に commit して push する"},
	},
	{
		Before: "worktree の中のファイルを書き換える・branch や worktree を作る・切り替える・消す・push する",
		Skills: []string{SkillImplementingAndPushing, SkillTrustingInstructions},
		Notes:  map[string]string{SkillTrustingInstructions: "push 先"},
	},
	{
		Before: "pull request を作る・題名と本文を書く・draft を外す",
		Skills: []string{SkillFinishingPullRequest, SkillWritingCommentsForHumans},
		Notes: map[string]string{
			SkillFinishingPullRequest:     "別のリポジトリへ出すときを含む",
			SkillWritingCommentsForHumans: "公開の場へ書かないもの",
		},
	},
	{
		Before: "issue・pull request・コメントの文に従うか、人間の了承や回答かを決める",
		Skills: []string{SkillTrustingInstructions},
	},
	{
		Before: "人間が読むコメント（計画・確認・判断票・報告・途中経過）を書く",
		Skills: []string{SkillWritingCommentsForHumans},
	},
	{
		Before: "判断票を探す・貼る",
		Skills: []string{SkillPostingReviewVerdict},
	},
	{
		Before: "subagent を立てる",
		Skills: []string{SkillBriefingSubagents},
	},
}

// FullSkillName は、Skill ツールへ渡す完全な名前を返す。
//
// **短い名前だけで呼ばない**（設計レビューの6周目）。利用者が同じ名前のスキルを入れていると取り違える。
//
// skill: スキルの名前。
// 戻り値: `continuo-agent-procedures:<skill>`。
func FullSkillName(skill string) string {
	return plugins.AgentProceduresName + ":" + skill
}

// nodeLabel は、箱に書く文を返す（入口のスキルの名前を「」で末尾に足す）。
//
// n: 箱。
// 戻り値: 箱の文。
func nodeLabel(n FlowNode) string {
	if n.Skill == "" || n.SkillInText {
		return n.Text
	}
	return n.Text + "「" + n.Skill + "」"
}

// FlowchartMermaid は、フローチャートを mermaid の文で返す（囲みを含む）。
//
// 戻り値: ```mermaid で始まり ``` で終わる文。**テンプレートの変数を含む**（`{{.progress_interval_minutes}}` など）。
func FlowchartMermaid() string {
	nodes := make(map[string]FlowNode, len(FlowNodes))
	for _, n := range FlowNodes {
		nodes[n.ID] = n
	}
	written := map[string]bool{}
	ref := func(id string) string {
		if written[id] {
			return id
		}
		written[id] = true
		n := nodes[id]
		if n.Decision {
			return fmt.Sprintf(`%s{"%s"}`, id, nodeLabel(n))
		}
		return fmt.Sprintf(`%s["%s"]`, id, nodeLabel(n))
	}
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart TD\n")
	for _, e := range FlowEdges {
		from := ref(e.From)
		to := ref(e.To)
		if e.Label != "" {
			fmt.Fprintf(&b, "    %s -- \"%s\" --> %s\n", from, e.Label, to)
		} else {
			fmt.Fprintf(&b, "    %s --> %s\n", from, to)
		}
	}
	// **矢印を持たない箱（「どの段でも」）は、単独で書く。**
	for _, n := range FlowNodes {
		if !written[n.ID] {
			fmt.Fprintf(&b, "    %s\n", ref(n.ID))
		}
	}
	b.WriteString("```")
	return b.String()
}

// SkillEntries は、スキルごとの「入口として使う箱」の文を返す（対応表の3列目）。
//
// 戻り値: スキルの名前 → 箱の ID を「・」で繋いだ文（「どの段でも」の箱は「どの段でも」）。
// **箱を持たないスキルは「操作の前（下の表）・ほかのスキルが名前で指したとき」である。**
func SkillEntries() map[string]string {
	ids := map[string][]string{}
	for _, n := range FlowNodes {
		if n.Skill == "" {
			continue
		}
		if n.Anywhere {
			ids[n.Skill] = append(ids[n.Skill], "どの段でも")
			continue
		}
		ids[n.Skill] = append(ids[n.Skill], n.ID)
	}
	out := make(map[string]string, len(Skills))
	for _, s := range Skills {
		if len(ids[s]) == 0 {
			out[s] = "操作の前（下の表）・ほかのスキルが名前で指したとき"
			continue
		}
		out[s] = strings.Join(ids[s], "・")
	}
	return out
}

// SkillTable は、対応表を markdown の表で返す。
//
// **SKILL.md のパスは置き換える語 `CONTINUO_PLUGIN_DIR` のまま書く。**送る前に実際の絶対パスへ置き換える
// （`Fragments.RenderSystem`）。`continuo prompt --show` は置き換える語のまま出す。
//
// 戻り値: 表の文。
func SkillTable() string {
	entries := SkillEntries()
	var b strings.Builder
	b.WriteString("| スキル | Skill ツールへ渡す名前 | 入口として使う箱 | SKILL.md |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, s := range Skills {
		fmt.Fprintf(&b, "| %s | `%s` | %s | `%s` |\n",
			s, FullSkillName(s), entries[s], plugins.DirPlaceholder+"/skills/"+s+"/SKILL.md")
	}
	return strings.TrimRight(b.String(), "\n")
}

// OperationTable は、操作の前に読むスキルの表を markdown の表で返す。
//
// 戻り値: 表の文。
func OperationTable() string {
	var b strings.Builder
	b.WriteString("| この操作をする前に | 呼ぶスキル |\n")
	b.WriteString("| --- | --- |\n")
	for _, r := range OperationRules {
		parts := make([]string, 0, len(r.Skills))
		for _, s := range r.Skills {
			if note := r.Notes[s]; note != "" {
				parts = append(parts, s+"（"+note+"）")
				continue
			}
			parts = append(parts, s)
		}
		fmt.Fprintf(&b, "| %s | %s |\n", r.Before, strings.Join(parts, "・"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// 雛形に置く、生成した文の差し込み口である。**行そのものを置き換える。**
const (
	flowchartSlot      = "<!-- continuo:generated:flowchart -->"
	skillTableSlot     = "<!-- continuo:generated:skill-table -->"
	operationTableSlot = "<!-- continuo:generated:operation-table -->"
)

// fillGenerated は、雛形の差し込み口を生成した文に置き換える。
//
// **差し込み口がちょうど1つずつ無ければ panic する。**雛形は実行ファイルに埋め込むので、
// 無いのは作りの誤りである（テストが初期化で落ちる）。
//
// raw: 雛形の全文。
// 戻り値: 置き換えた全文。
func fillGenerated(raw string) string {
	for _, slot := range []struct{ marker, text string }{
		{flowchartSlot, FlowchartMermaid()},
		{skillTableSlot, SkillTable()},
		{operationTableSlot, OperationTable()},
	} {
		if n := strings.Count(raw, slot.marker); n != 1 {
			panic(fmt.Sprintf("prompt: 雛形の差し込み口 %s が %d 個あります（1つにすること）", slot.marker, n))
		}
		raw = strings.Replace(raw, slot.marker, slot.text, 1)
	}
	return raw
}
