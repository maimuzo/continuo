// {"RUCM-CFG-SHA256": "762f90189ab19708c063eb0bb16a544257768ec0f393e6a6ea44614891b171da", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **`continuo setup` がどのボードを読むかを決める経路の検査である。**
//
// **WORKFLOW.md に答えが書いてあるのに `--project` を要求してはならない**（設計 6-2）。
// さらに悪い場合として、ログイン名のボードがちょうど1件だけあると、
// **WORKFLOW.md に書かれたボードではない別のボードの Status を読み、その名前を書き込む。**
// project_number はそのままなので、起動時の照合まで誰も気づけない。
package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
)

// recordingDetect は、検出へ渡された値を記録する Deps を組み立てる。
//
// **記録するのは「どのボードを読むと決めたか」である。**
//
// got: 渡された値を書き込む先。
// 戻り値: 検出だけを差し替えた Deps（ボードの読み取りは必ず失敗させ、対話へ入らない）。
func recordingDetect(got *scaffold.DetectOptions) cli.Deps {
	return cli.Deps{
		ScaffoldDetect: func(_ context.Context, opts scaffold.DetectOptions) scaffold.Detection {
			*got = opts
			// **渡された値をそのまま返す。**検出は「決まらなかったぶんだけ gh を叩く」ので、
			// 決まっていれば同じ値が返る。
			return scaffold.Detection{
				Values: scaffold.Values{Owner: opts.Owner, ProjectNumber: opts.ProjectNumber},
				Fields: []scaffold.Field{
					{Key: scaffold.OwnerKey, Filled: opts.Owner != "", Reason: "検査用に固定した値です"},
					{Key: scaffold.ProjectKey, Filled: opts.ProjectNumber > 0, Reason: "検査用に固定した値です"},
				},
			}
		},
		SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
			// **ここから先は、このテストの関心ではない。**どのボードを読むと決めたかは
			// もう記録し終えているので、対話に入らずに止める。
			return setup.StatusField{}, errors.New("検査ではボードを読みません")
		},
	}
}

// writeWorkflowWith は、owner とボードの番号を書いた WORKFLOW.md を1つ置く。
//
// t: 呼び出し元のテスト。
// owner: 書き込む owner。
// number: 書き込むボードの番号。
// 戻り値: 置いたディレクトリ。
func writeWorkflowWith(t *testing.T, owner string, number int) string {
	t.Helper()
	dir := t.TempDir()
	out := scaffold.TemplateWithValues(scaffold.Values{Owner: owner, ProjectNumber: number})
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return dir
}

// TestRunSetup_WORKFLOWmdに書かれたボードを使う は、**書いてあるのに聞き直す**のを落とす。
//
// 目的: フラグが無いとき、WORKFLOW.md の owner とボードの番号を検出へ渡すこと。
// 与える情報: `owner: octocat` / `project_number: 42` を書いた WORKFLOW.md。
// 成功条件: 検出がその2つを受け取り、使うボードが画面に出ること。
func TestRunSetup_WORKFLOWmdに書かれたボードを使う(t *testing.T) {
	var got scaffold.DetectOptions
	dir := writeWorkflowWith(t, "octocat", 42)

	_, stdout, _ := runCLIWith(recordingDetect(&got), []string{"setup", dir}, "")

	if got.Owner != "octocat" || got.ProjectNumber != 42 {
		t.Fatalf("WORKFLOW.md に書かれたボードを使っていない: %+v", got)
	}
	if !strings.Contains(stdout, "42") {
		t.Errorf("どのボードを読むかが画面に出ていない:\n%s", stdout)
	}
}

// TestRunSetup_フラグはWORKFLOWmdより強い は、明示した指定が勝つことを確かめる。
//
// 目的: `--owner` と `--project` を渡したとき、WORKFLOW.md の値ではなくフラグを使うこと。
// 与える情報: `owner: octocat` / `project_number: 42` を書いた WORKFLOW.md と、別の値のフラグ。
// 成功条件: 検出がフラグの値を受け取ること。
func TestRunSetup_フラグはWORKFLOWmdより強い(t *testing.T) {
	var got scaffold.DetectOptions
	dir := writeWorkflowWith(t, "octocat", 42)

	runCLIWith(recordingDetect(&got), []string{"setup", "--owner", "acme", "--project", "7", dir}, "")

	if got.Owner != "acme" || got.ProjectNumber != 7 {
		t.Fatalf("フラグの指定が使われていない: %+v", got)
	}
}
