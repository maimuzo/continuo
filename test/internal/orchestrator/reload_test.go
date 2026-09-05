package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 読み直しの検査に使う WORKFLOW.md を書き、その絶対パスを返す。
//
// t: テストコンテキスト。
// dir: 置き場所（同じディレクトリへ書き直すと、印が変わって読み直しが走る）。
// front: front matter の YAML 本体。末尾は "\n" で終えること。
// body: 本文。
// 戻り値: 書き込んだファイルの絶対パス。
func 読み直し用のWORKFLOW(t *testing.T, dir, front, body string) string {
	t.Helper()
	path := filepath.Join(dir, "WORKFLOW.md")
	content := "---\n" + front + "---\n" + body
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書けません: %v", err)
	}
	return path
}

// 読み直しの検査に使う front matter を組み立てる。
//
// gate: tracker.provider.handoff.on_assignee_gate の値。
// maxAgents: agent.max_concurrent_agents の値（文字列で渡す）。
// pollingMs: polling.interval_ms の値（凍結側。読み直しても効かないことの確認に使う）。
func 読み直し用のfrontMatter(gate, maxAgents, pollingMs string) string {
	return "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		"    handoff:\n" +
		"      on_assignee_gate: " + gate + "\n" +
		"agent:\n" +
		"  max_concurrent_agents: " + maxAgents + "\n" +
		"polling:\n" +
		"  interval_ms: " + pollingMs + "\n"
}

// 読み直しの検査で使う設定の下ごしらえをする。
//
// **fixture の既定は雛形なので、`tracker.provider.owner` などが空である。**
// 読み直しは「混ぜた結果」へ検査を掛け直すので、土台の側も検査を通る値にしておく。
func 読み直し用の設定(cfg *config.Config) {
	cfg.Tracker.Provider.Owner = "acme"
	cfg.Tracker.Provider.ProjectNumber = 1
	cfg.Tracker.Provider.StatusField = "Status"
}

// Test読み直しで効くキーは巡回の頭で入れ替わる は、WORKFLOW.md を書き換えたあとの巡回で、
// 読み直せるキーが効き、凍結側は効かないと知らせることを確かめる。
//
// 目的: 受け入れ条件「常駐している continuo に設定を読み直させられる」と
// 「読み直しただけでは効かない項目を変えたとき、その項目名と前後の値が出る」を固定する。
// 与える情報: 読み直せるキーと凍結側のキーの両方を書き換えた WORKFLOW.md。
// 成功条件: 効いた項目に読み直せるキーが並び、効かない項目に凍結側のキーが前後の値つきで並ぶ。
func Test読み直しで効くキーは巡回の頭で入れ替わる(t *testing.T) {
	dir := t.TempDir()
	path := 読み直し用のWORKFLOW(t, dir,
		読み直し用のfrontMatter("warn_and_comment", "2", "30000"), "本文\n")

	fx := newFixture(t, fixtureOptions{
		ConfigPath: path,
		Mutate: func(cfg *config.Config) {
			読み直し用の設定(cfg)
			cfg.Tracker.Provider.Handoff.OnAssigneeGate = config.OnAssigneeGateWarnAndComment
			cfg.Agent.MaxConcurrentAgents = 2
			cfg.Polling.IntervalMs = 30000
		},
	})

	// **fixture の設定はファイルと別に組み立てているので、凍結側のキーが多数ちがう。**
	// 実機では同じファイルから来るので、ここまで並ばない。
	fx.AllowLog("読み直しただけでは効きません")

	// 1回目の巡回で、いまのファイルの印を覚える。
	fx.Orc.Tick(context.Background())

	// **読み直せるキーと凍結側のキーを、両方とも書き換える。**
	読み直し用のWORKFLOW(t, dir, 読み直し用のfrontMatter("warn_only", "5", "1000"), "本文\n")

	fx.Orc.Tick(context.Background())

	logs := fx.Logs.String()
	if !strings.Contains(logs, "WORKFLOW.md を読み直しました") {
		t.Fatalf("読み直しの記録が出ていない:\n%s", logs)
	}
	for _, want := range []string{
		"tracker.provider.handoff.on_assignee_gate",
		"agent.max_concurrent_agents",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("効いた項目に %q が並んでいない:\n%s", want, logs)
		}
	}
	if !strings.Contains(logs, "読み直しただけでは効きません") {
		t.Errorf("効かない項目の知らせが出ていない:\n%s", logs)
	}
	if !strings.Contains(logs, "polling.interval_ms: 30000 → 1000") {
		t.Errorf("効かない項目に、キーの道筋と前後の値が出ていない:\n%s", logs)
	}
}

// Test読み直しに失敗しても落ちず古い設定のまま続ける は、壊れた WORKFLOW.md を保存しても
// 巡回が止まらないことを確かめる。
//
// 目的: 受け入れ条件「読み直しに失敗したとき、古い設定のまま動き続ける。落ちない」を固定する。
// 与える情報: front matter が YAML として壊れている WORKFLOW.md。
// 成功条件: 巡回が返り、警告が出て、同じ警告が2回目の巡回では出し直されない。
func Test読み直しに失敗しても落ちず古い設定のまま続ける(t *testing.T) {
	dir := t.TempDir()
	path := 読み直し用のWORKFLOW(t, dir,
		読み直し用のfrontMatter("warn_and_comment", "2", "30000"), "本文\n")

	fx := newFixture(t, fixtureOptions{ConfigPath: path, Mutate: 読み直し用の設定})
	fx.AllowLog("WORKFLOW.md を読み直せませんでした")
	fx.AllowLog("読み直しただけでは効きません")
	fx.Orc.Tick(context.Background())

	// **YAML として壊す。**
	if err := os.WriteFile(path, []byte("---\ntracker:\n  provider:\n   : :\n---\n"), 0o600); err != nil {
		t.Fatalf("書けなかった: %v", err)
	}

	fx.Orc.Tick(context.Background())
	first := strings.Count(fx.Logs.String(), "WORKFLOW.md を読み直せませんでした")
	if first == 0 {
		t.Fatalf("読み直しの失敗が記録されていない:\n%s", fx.Logs.String())
	}

	// **同じ壊れたファイルのまま、もう1回巡回する。**同じ警告を出し直してはならない。
	fx.Orc.Tick(context.Background())
	if got := strings.Count(fx.Logs.String(), "WORKFLOW.md を読み直せませんでした"); got != first {
		t.Errorf("同じ警告が出し直された（%d回 → %d回）:\n%s", first, got, fx.Logs.String())
	}
}

// Test本文だけを直したら効かないと知らせる は、front matter を変えずに本文だけを直したときに、
// 「本文は効かない」が1行出ることを確かめる。
//
// 目的: 本文は Config の外にあるため、設定の差分だけでは1行も出ないことへの手当てを固定する。
// 与える情報: front matter が同じで、本文だけが違う WORKFLOW.md。
// 成功条件: 効かない項目に本文の行が出る。
func Test本文だけを直したら効かないと知らせる(t *testing.T) {
	dir := t.TempDir()
	front := 読み直し用のfrontMatter("warn_and_comment", "2", "30000")
	path := 読み直し用のWORKFLOW(t, dir, front, "はじめの本文\n")

	fx := newFixture(t, fixtureOptions{ConfigPath: path, PromptTemplate: "はじめの本文\n", Mutate: 読み直し用の設定})
	fx.AllowLog("読み直しただけでは効きません")
	fx.Orc.Tick(context.Background())

	読み直し用のWORKFLOW(t, dir, front, "書き換えた本文\n")
	fx.Orc.Tick(context.Background())

	logs := fx.Logs.String()
	if !strings.Contains(logs, config.PromptBodyKey) {
		t.Errorf("本文が効かないことの知らせが出ていない:\n%s", logs)
	}
}

// Test設定ファイルのパスを渡さなければ読み直さない は、渡していないテストの挙動が
// 変わっていないことを確かめる。
//
// 目的: 既存の47本のテストが、この変更で読み直しの経路へ入らないことを固定する。
// 与える情報: ConfigPath を渡さない fixture。
// 成功条件: 巡回しても読み直しの記録が1行も出ない。
func Test設定ファイルのパスを渡さなければ読み直さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Orc.Tick(context.Background())
	if strings.Contains(fx.Logs.String(), "WORKFLOW.md を読み直しました") {
		t.Errorf("パスを渡していないのに読み直しが走った:\n%s", fx.Logs.String())
	}
}

// Test値を元へ戻しても読み直しの記録が出る は、2 → 4 のあと 4 → 2 に戻したときに
// 記録が黙らないことを確かめる。
//
// 目的: 「読み直しが走らなかった」と利用者が読み違えるのを防ぐ。
// 与える情報: 同じキーを行き来させる WORKFLOW.md。
// 成功条件: 読み直しの記録が2回とも出る。
func Test値を元へ戻しても読み直しの記録が出る(t *testing.T) {
	dir := t.TempDir()
	path := 読み直し用のWORKFLOW(t, dir,
		読み直し用のfrontMatter("warn_and_comment", "2", "30000"), "本文\n")

	fx := newFixture(t, fixtureOptions{ConfigPath: path, Mutate: 読み直し用の設定})
	fx.AllowLog("読み直しただけでは効きません")
	fx.Orc.Tick(context.Background())

	読み直し用のWORKFLOW(t, dir, 読み直し用のfrontMatter("warn_and_comment", "4", "30000"), "本文\n")
	fx.Orc.Tick(context.Background())
	first := strings.Count(fx.Logs.String(), "WORKFLOW.md を読み直しました")

	// **元の値へ戻す。**知らせがキーの名前だけだと、ここで前回と同じ文字列になって黙る。
	読み直し用のWORKFLOW(t, dir, 読み直し用のfrontMatter("warn_and_comment", "2", "30000"), "本文\n")
	fx.Orc.Tick(context.Background())
	second := strings.Count(fx.Logs.String(), "WORKFLOW.md を読み直しました")

	if second <= first {
		t.Errorf("値を元へ戻したら記録が出なくなった（%d回 → %d回）:\n%s", first, second, fx.Logs.String())
	}
}

// Test触っていない項目は効かない一覧に出ない は、利用者が書き換えていないキーが
// 「効きません」の一覧へ出ないことを確かめる。
//
// 目的: CLI の上書き（--port）が混ざって、触っていない server.port が毎回出るのを防ぐ。
// 与える情報: 差し替えられるキーだけを書き換えた WORKFLOW.md と、Config 側だけ違う server.port。
// 成功条件: 「効きません」の一覧に server.port が出ない。
func Test触っていない項目は効かない一覧に出ない(t *testing.T) {
	dir := t.TempDir()
	front := 読み直し用のfrontMatter("warn_and_comment", "2", "30000")
	path := 読み直し用のWORKFLOW(t, dir, front, "本文\n")

	fileCfg := 読み直し用のファイルの設定(t, path)
	port := 9090
	fx := newFixture(t, fixtureOptions{
		ConfigPath: path,
		ConfigFile: &fileCfg,
		Mutate: func(cfg *config.Config) {
			読み直し用の設定(cfg)
			// **CLI の --port を渡して起動したのと同じ形にする。**
			cfg.Server.Port = &port
		},
	})
	fx.AllowLog("読み直しただけでは効きません")

	fx.Orc.Tick(context.Background())
	読み直し用のWORKFLOW(t, dir, 読み直し用のfrontMatter("warn_and_comment", "4", "30000"), "本文\n")
	fx.Orc.Tick(context.Background())

	if strings.Contains(fx.Logs.String(), "server.port") {
		t.Errorf("触っていない server.port が「効きません」に出た:\n%s", fx.Logs.String())
	}
}

// 読み直し用のファイルの設定 は、書いた WORKFLOW.md を読んだままの設定を返す。
func 読み直し用のファイルの設定(t *testing.T, path string) config.Config {
	t.Helper()
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を読めなかった: %v", err)
	}
	return loaded.Config
}
