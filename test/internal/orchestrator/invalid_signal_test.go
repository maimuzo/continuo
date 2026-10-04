package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/statussignal"
)

// statusSignalPathOf は、issue #188 の取り得る値のファイルの置き場所を返す。
func statusSignalPathOf(fx *fixture) string {
	return filepath.Join(fx.RuntimeDir, "issues", "octocat-hello-world-188", statussignal.FileName)
}

// writeFileAt は、親ディレクトリを作ってからファイルを置く。
//
// t: 呼び出し元のテスト。
// path: 置く先の絶対パス。
// data: 置く中身。
func writeFileAt(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("ファイルを書けない: %v", err)
	}
}

// TestDispatch_着手のとき取り得る値のファイルを書く は、hook が読むファイルを本体が置くことを確かめる
// （issue #274 の経路1）。
//
// **このファイルが在る run でだけ、`continuo hook` は表明を調べる。**
//
// 目的: 着手のとき、issue ごとのディレクトリへ、識別子・印・取り得る値を書くこと。
// **逃がし先（`pending/`）の中には書かないこと**（本体が hook の1件として読んでしまう）。
// 与える情報: `Ready` の issue が1件。
// 成功条件: `<実行時ディレクトリ>/issues/<スラグ>/status-signal.json` に、識別子・印・
// 既定の3つの値が入っている。**`settings.json` の hook のコマンド行に書いた `--pending-dir` から
// hook が引く場所と、本体が書いた場所が同じである**（逃がし先の場所を変えたときに、
// hook がファイルを見つけられなくなるのを、ここで止める）。
func TestDispatch_着手のとき取り得る値のファイルを書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	got, err := statussignal.Read(statusSignalPathOf(fx))
	if err != nil {
		t.Fatalf("取り得る値のファイルを読めない: %v", err)
	}
	if got.Identifier != "octocat/hello-world#188" || got.Prefix != signalPrefix {
		t.Errorf("識別子か印が違う: %+v", got)
	}
	for _, key := range []string{"review", "blocked", "working"} {
		if _, ok := got.Values[key]; !ok {
			t.Errorf("取り得る値に %q が無い: %+v", key, got.Values)
		}
	}
	if dest := got.Values["working"]; dest != nil {
		t.Errorf("working の行き先が null でない: %v", *dest)
	}
	if !got.Usable() {
		t.Errorf("hook が使えない中身を書いている: %+v", got)
	}

	settings, err := os.ReadFile(filepath.Join(fx.RuntimeDir, "issues", "octocat-hello-world-188", "settings.json"))
	if err != nil {
		t.Fatalf("設定ファイルを読めない: %v", err)
	}
	m := regexp.MustCompile(`--pending-dir '([^']+)'`).FindSubmatch(settings)
	if m == nil {
		t.Fatalf("設定ファイルの hook のコマンド行に --pending-dir が無い:\n%s", settings)
	}
	if fromHook := statussignal.PathFromPendingDir(string(m[1])); fromHook != statusSignalPathOf(fx) {
		t.Errorf("hook が引く場所と本体が書いた場所が違う: hook=%q 本体=%q", fromHook, statusSignalPathOf(fx))
	}
}

// TestAdopt_引き継ぐとき取り得る値のファイルを書き直す は、立て直しのあとも hook と本体が
// 同じ対応表を見ることを確かめる（issue #274）。
//
// **立て直しは `settings.json` を書き直さない。**このファイルも書き直さないと、
// `tracker.status_signal_map` を書き換えて立て直した利用者の run で、hook が古い一覧で
// 差し戻し、本体が新しい一覧で返す。
//
// 目的: run を引き継ぐとき、前の起動が置いた古いファイルを、いまの設定で書き直すこと。
// 与える情報: 古い対応表（`done` だけ）のファイルが残っている issue を引き継ぐ。
// 成功条件: 引き継いだあとのファイルが、いまの対応表（`review` を持つ）になっている。
func TestAdopt_引き継ぐとき取り得る値のファイルを書き直す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "In Progress")
	fx.Tracker.AddIssue(issue)

	stale, err := statussignal.Encode(statussignal.File{
		Identifier: issue.Identifier,
		Prefix:     "OLD-STATUS:",
		Values:     map[string]*string{"done": nil},
	})
	if err != nil {
		t.Fatalf("古いファイルを組み立てられない: %v", err)
	}
	writeFileAt(t, statusSignalPathOf(fx), stale)

	if !fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		SessionUUID:  "session-1",
		WorktreePath: t.TempDir(),
	}, false) {
		t.Fatalf("検査用の run を印の集合へ入れられません")
	}

	got, err := statussignal.Read(statusSignalPathOf(fx))
	if err != nil {
		t.Fatalf("取り得る値のファイルを読めない: %v", err)
	}
	if got.Prefix != signalPrefix {
		t.Errorf("印がいまの設定で書き直されていない: %q", got.Prefix)
	}
	if _, ok := got.Values["review"]; !ok {
		t.Errorf("対応表がいまの設定で書き直されていない: %+v", got.Values)
	}
	if _, ok := got.Values["done"]; ok {
		t.Errorf("古い対応表が残っている: %+v", got.Values)
	}
}
