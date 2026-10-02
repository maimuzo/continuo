// **WORKFLOW.md に答えが書いてあるのに `--project` を要求してはならない**（設計 6-2）。
// さらに悪い場合として、ログイン名のカンバンがちょうど1件だけあると、
// **WORKFLOW.md に書かれたカンバンではない別のカンバンの Status を読み、その名前を書き込む。**
// project_number はそのままなので、起動時の照合まで誰も気づけない。
package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
)

// recordingDetect は、検出へ渡された値を記録する Deps を組み立てる。
//
// **記録するのは「どのカンバンを読むと決めたか」である。**
//
// got: 渡された値を書き込む先。
// 戻り値: 検出だけを差し替えた Deps（カンバンの読み取りは必ず失敗させ、対話へ入らない）。
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
			// **ここから先は、このテストの関心ではない。**どのカンバンを読むと決めたかは
			// もう記録し終えているので、対話に入らずに止める。
			return setup.StatusField{}, errors.New("検査ではカンバンを読みません")
		},
	}
}

// writeWorkflowWith は、owner とカンバンの番号を書いた WORKFLOW.md を1つ置く。
//
// t: 呼び出し元のテスト。
// owner: 書き込む owner。
// number: 書き込むカンバンの番号。
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
