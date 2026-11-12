package scaffold_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: continuo-ci.yaml の見本の、どの pull request を検査するかの既定値を固定する（issue #345）。
//
// **既定は「全部の pull request を検査する」（all）である。人間の決定である**
// （https://github.com/maimuzo/continuo/issues/345#issuecomment-6824122886）。
// 既定が continuo に変わると、全部を検査したい利用者が見本を置き直したときに、
// 人間の pull request の検査が緑で素通りする。**赤くならないので気づけない。**
//
// **切り替えの行は、見本の2行目に置く。**人間は「見本の先頭の行を書き換えて .github/workflows/ へ置く」と
// 理解して了承した（https://github.com/maimuzo/continuo/issues/345#issuecomment-6824579497）。
//
// 与える情報: scaffold.CITemplate() の全文。
// 成功条件: 先頭の3行が env: と2つの値で、CONTINUO_REVIEW_GATE_SCOPE が all であること。
func TestCITemplate_検査する範囲の既定は全部で先頭に在る(t *testing.T) {
	lines := strings.SplitN(scaffold.CITemplate(), "\n", 4)
	want := []string{"env:", "  CONTINUO_REVIEW_GATE_SCOPE: all", "  CONTINUO_BRANCH_PREFIX: continuo/"}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("見本の %d 行目 = %q, want %q", i+1, lines[i], w)
		}
	}
	env := envMap(workflowEnv(t, scaffold.CITemplate()))
	if env["CONTINUO_REVIEW_GATE_SCOPE"] != "all" {
		t.Errorf("CONTINUO_REVIEW_GATE_SCOPE の既定 = %q, want all", env["CONTINUO_REVIEW_GATE_SCOPE"])
	}
}

// 目的: 見本の branch 名の頭の既定値が、continuo が既定で作る branch 名の頭と同じであることを確かめる（issue #345）。
//
// **ずれると、continuo の pull request も「continuo の branch ではない」として緑で通る。**
// branch_template の既定値を変えた日に、見本だけが古いまま残るのをここで止める。
//
// 与える情報: config.DefaultConfig() の branch_template と、見本の env:。
// 成功条件: workspace.BranchPrefix で作った頭と、CONTINUO_BRANCH_PREFIX が一致すること。
func TestCITemplate_branch名の頭の既定が設定の既定と同じ(t *testing.T) {
	want := workspace.BranchPrefix(config.DefaultConfig().Herdr.Worktree.BranchTemplate)
	if want == "" {
		t.Fatal("既定の branch_template から頭を作れません")
	}
	got := envMap(workflowEnv(t, scaffold.CITemplate()))["CONTINUO_BRANCH_PREFIX"]
	if got != want {
		t.Errorf("CONTINUO_BRANCH_PREFIX = %q, want %q（既定の branch_template の頭）", got, want)
	}
}

// 目的: 検査する範囲の切り替えが、2つの job で同じように効くことを確かめる（issue #345）。
//
// **2つの運用を支える。**
//   - all: 全部の pull request に判断票を求める（開発をすべて continuo で進めるリポジトリ）
//   - continuo: continuo の branch から出た pull request にだけ求め、人間の pull request は緑で終える
//
// **書き間違いの値と空は赤にする。**黙って全部を通す側にも、黙って全部を止める側にも倒さず、直し方を出す。
//
// 与える情報: 判断票の無い pull request（どちらの job も、数えれば赤になる状態）と、
// CONTINUO_REVIEW_GATE_SCOPE・CONTINUO_BRANCH_PREFIX・HEAD_REF の組み合わせ。
// 成功条件: 下の表のとおりの終了コードと、画面の案内になること。
func TestCITemplate_検査する範囲を切り替えられる(t *testing.T) {
	cases := []struct {
		name     string
		env      []string
		wantExit int
		want     []string
		ng       []string
	}{
		{
			name:     "all では人間の pull request も数える",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=all", "HEAD_REF=feature/login"},
			wantExit: 1,
			want:     []string{"貼られていません"},
			ng:       []string{"判断票を数えません"},
		},
		{
			name:     "continuo では人間の pull request を数えずに緑",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=continuo", "HEAD_REF=feature/login"},
			wantExit: 0,
			want:     []string{"判断票を数えません", "feature/login", "continuo/"},
			ng:       []string{"貼られていません"},
		},
		{
			name:     "continuo でも continuo の pull request は数える",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=continuo", "HEAD_REF=continuo/octocat/hello-world/12"},
			wantExit: 1,
			want:     []string{"貼られていません"},
			ng:       []string{"判断票を数えません"},
		},
		{
			name:     "頭を変えたら、その頭の pull request を数える",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=continuo", "CONTINUO_BRANCH_PREFIX=agent/", "HEAD_REF=agent/octocat/hello-world/12"},
			wantExit: 1,
			want:     []string{"貼られていません"},
			ng:       []string{"判断票を数えません"},
		},
		{
			name:     "頭が空なら全部を数える側に倒れる",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=continuo", "CONTINUO_BRANCH_PREFIX=", "HEAD_REF=feature/login"},
			wantExit: 1,
			want:     []string{"貼られていません"},
			ng:       []string{"判断票を数えません"},
		},
		{
			name:     "頭の * は文字どおりに比べる",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=continuo", "CONTINUO_BRANCH_PREFIX=*", "HEAD_REF=feature/login"},
			wantExit: 0,
			want:     []string{"判断票を数えません"},
		},
		{
			name:     "書き間違いの値は赤で直し方を出す",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=Continuo", "HEAD_REF=feature/login"},
			wantExit: 1,
			want:     []string{"どの pull request を検査するかが読めません", "Continuo", "CONTINUO_REVIEW_GATE_SCOPE: all"},
			ng:       []string{"貼られていません", "判断票を数えません"},
		},
		{
			name:     "空の値は赤で直し方を出す",
			env:      []string{"CONTINUO_REVIEW_GATE_SCOPE=", "HEAD_REF=feature/login"},
			wantExit: 1,
			want:     []string{"どの pull request を検査するかが読めません"},
			ng:       []string{"貼られていません", "判断票を数えません"},
		},
	}
	for _, job := range []string{"design-review-result", "code-review-result"} {
		script := templateRun(t, job)
		for _, c := range cases {
			t.Run(job+"/"+c.name, func(t *testing.T) {
				r := newCIRun(t)
				// **コメントは0件にしておく。**数えに進めば必ず「貼られていません」で赤になる。
				r.comments(prComments)
				r.env = c.env
				res := r.run(script, false)
				wantExit(t, res, c.wantExit)
				wantSummary(t, res, c.want...)
				wantNoSummary(t, res, c.ng...)
			})
		}
	}
}

// 目的: 既存の CI へ job だけを写して env: が落ちても、set -u で黙って落ちずに直し方を出すことを確かめる（issue #345）。
//
// **見本は「既存の CI へ組み込む」道も案内している。**job だけを写すと最上位の env: が落ちる。
// そのとき unbound variable で落ちると、画面に何も出ず、何を直せばよいか分からない。
//
// 与える情報: CONTINUO_REVIEW_GATE_SCOPE と CONTINUO_BRANCH_PREFIX の無い環境。
// 成功条件: 2つの job とも、終了コードが 1 で、env: の2行を置くよう案内すること。
func TestCITemplate_envが無くても直し方を出す(t *testing.T) {
	for _, job := range []string{"design-review-result", "code-review-result"} {
		t.Run(job, func(t *testing.T) {
			script := "unset CONTINUO_REVIEW_GATE_SCOPE CONTINUO_BRANCH_PREFIX\n" + templateRun(t, job)
			r := newCIRun(t)
			res := r.run(script, false)
			wantExit(t, res, 1)
			wantSummary(t, res, "どの pull request を検査するかが読めません", "CONTINUO_REVIEW_GATE_SCOPE: all")
			if strings.Contains(res.output, "unbound variable") {
				t.Errorf("unbound variable で落ちています\n%s", res.output)
			}
		})
	}
}

// envMap は "名前=値" の並びを map にする。
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}
