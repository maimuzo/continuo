package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// designDocPath は設計文書の場所である。Go のテストはパッケージのディレクトリを
// 作業ディレクトリとして走るので、test/internal/config からの相対パスで指す。
const designDocPath = "../../../docs/plans/continuo_design.md"

// designSectionHeading は front matter の設定例が載っている節の見出しである。
const designSectionHeading = "### 5-2. front matter（設定）"

// readDesignFrontMatterExample は設計文書 5-2 の設定例（```yaml で囲まれたブロック）を
// そのまま取り出す。testdata へ写しを置くのではなく設計文書そのものを読むことで、
// 設計と実装がずれた瞬間にこのテストが落ちるようにする。
//
// t: テストコンテキスト。
// 戻り値: ```yaml と ``` に挟まれた中身の文字列（前後の区切り行 "---" を含む。
// つまり WORKFLOW.md の front matter 部分そのもの）。
// 見出しが見つからない、または直後に ```yaml のブロックが無い場合はテストを失敗させる。
func readDesignFrontMatterExample(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(designDocPath)
	if err != nil {
		t.Fatalf("設計文書を読み込めません（%s）: %v", designDocPath, err)
	}
	lines := strings.Split(string(raw), "\n")

	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == designSectionHeading {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		t.Fatalf("設計文書に見出し %q が見つかりません。見出しを変えたならこのテストも直すこと", designSectionHeading)
	}

	fenceStart := -1
	for i := headingIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "### ") {
			break
		}
		if strings.TrimSpace(lines[i]) == "```yaml" {
			fenceStart = i
			break
		}
	}
	if fenceStart < 0 {
		t.Fatalf("見出し %q の直後に ```yaml のブロックが見つかりません", designSectionHeading)
	}

	fenceEnd := -1
	for i := fenceStart + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```" {
			fenceEnd = i
			break
		}
	}
	if fenceEnd < 0 {
		t.Fatalf("見出し %q の ```yaml のブロックが閉じられていません", designSectionHeading)
	}

	return strings.Join(lines[fenceStart+1:fenceEnd], "\n") + "\n"
}

// 目的: 設計 5-2 に載っている front matter の設定例が、そのまま WORKFLOW.md として
// 読み込めることを確認する。設計に書かれているのに Go の構造体に無いキーがあると、
// yaml.Strict() が未知のキーとして弾くため、このテストが落ちる。
// 与える情報: 設計文書 5-2 の ```yaml ブロックをそのまま書き出したファイル。
// 成功条件: config.Load がエラーを返さず、front matter に書かれた値が反映されていること。
func TestLoad_設計5_2の設定例がそのまま読み込める(t *testing.T) {
	front := readDesignFrontMatterExample(t)

	// 設計の設定例は front matter の区切り行 "---" を含んでいるので、writeWorkflow の
	// ように区切り行を足さず、そのままファイルへ書く。
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(front+"本文のテンプレート\n"), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("設計 5-2 の設定例を読み込めませんでした（設計に載っているキーが構造体に無い可能性がある）: %v", err)
	}

	if loaded.Config.Tracker.Provider.Owner != "octocat" {
		t.Errorf("tracker.provider.owner が反映されていない: got %q, want %q", loaded.Config.Tracker.Provider.Owner, "octocat")
	}
	if loaded.Config.Tracker.Provider.ProjectNumber != 3 {
		t.Errorf("tracker.provider.project_number が反映されていない: got %d, want %d", loaded.Config.Tracker.Provider.ProjectNumber, 3)
	}
	if loaded.Config.Tracker.StatusSignalPrefix != "CONTINUO-STATUS:" {
		t.Errorf("tracker.status_signal_prefix が反映されていない: got %q, want %q", loaded.Config.Tracker.StatusSignalPrefix, "CONTINUO-STATUS:")
	}
	review, ok := loaded.Config.Tracker.StatusSignalMap["review"]
	if !ok || review == nil || *review != "In Review" {
		t.Errorf("tracker.status_signal_map.review が反映されていない: %s", renderJSON(t, loaded.Config.Tracker.StatusSignalMap))
	}
	working, ok := loaded.Config.Tracker.StatusSignalMap["working"]
	if !ok {
		t.Errorf("tracker.status_signal_map に working が無い: %s", renderJSON(t, loaded.Config.Tracker.StatusSignalMap))
	} else if working != nil {
		t.Errorf("tracker.status_signal_map.working は null（Status を動かさない）であるべきなのに %q が入っている", *working)
	}
}

// 目的: 設計 5-2 の設定例と DefaultConfig() の既定値が一致していることを確認する。
// default.go 自身が「既定値は 5-2 の設定例をそのまま Go の値にしたもの」と宣言しているので、
// 片方だけを直したときにここで落とす。
// 与える情報: 設計文書 5-2 の設定例をそのまま書き出したファイルと、config.DefaultConfig() の値。
// 成功条件: 読み込んだ Config が、5-5 の展開（チルダ・環境変数）を反映した DefaultConfig() と
// 全区分で一致すること。
func TestLoad_設計5_2の設定例と既定値が一致する(t *testing.T) {
	front := readDesignFrontMatterExample(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(front), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.md を書き込めません: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("設計 5-2 の設定例を読み込めませんでした: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("ホームディレクトリを取得できないためスキップする: %v", err)
	}

	want := config.DefaultConfig()
	// **rate_limit.token_source の既定だけは OS で分かれる**（macOS は keychain）。
	// 設計 5-2 の設定例は、どの OS でも読める claude_credentials を書いてあるので、
	// 比較する側もその値へそろえる。**ここで既定値そのものを検査しているわけではない。**
	// OS ごとの既定値は TestDefaultConfig_rate_limitのtoken_sourceの既定がOSで分かれる が見る。
	want.RateLimit.TokenSource = config.RateLimitTokenSourceClaudeCredentials
	// 既定値を持たない必須キー（設定例が値を与えているもの）を埋める。
	want.Tracker.Provider.Owner = "octocat"
	want.Tracker.Provider.ProjectNumber = 3
	want.Tracker.Provider.StatusField = "Status"
	// 5-5 の展開を通ったあとの値にそろえる。
	// 設計 5-2 の socket は素の既定パス（~/.config/herdr/herdr.sock）なので、
	// チルダの展開だけを当てる。環境変数は参照しない（設計 5-2 のコメント参照）。
	want.Workspace.Root = home + "/worktrees"
	want.Herdr.Socket = home + "/.config/herdr/herdr.sock"

	sections := []struct {
		name string
		got  any
		want any
	}{
		{"tracker", loaded.Config.Tracker, want.Tracker},
		{"polling", loaded.Config.Polling, want.Polling},
		{"workspace", loaded.Config.Workspace, want.Workspace},
		{"workspace_hooks", loaded.Config.WorkspaceHooks, want.WorkspaceHooks},
		{"agent", loaded.Config.Agent, want.Agent},
		{"claude", loaded.Config.Claude, want.Claude},
		{"herdr", loaded.Config.Herdr, want.Herdr},
		{"naming", loaded.Config.Naming, want.Naming},
		{"cleanup", loaded.Config.Cleanup, want.Cleanup},
		{"rate_limit", loaded.Config.RateLimit, want.RateLimit},
		{"trust", loaded.Config.Trust, want.Trust},
		{"restart", loaded.Config.Restart, want.Restart},
		{"server", loaded.Config.Server, want.Server},
	}
	for _, s := range sections {
		if !reflect.DeepEqual(s.got, s.want) {
			t.Errorf("設定区分 %s が設計 5-2 の設定例と既定値でずれている\n設定例から読んだ値:\n%s\n既定値:\n%s",
				s.name, renderJSON(t, s.got), renderJSON(t, s.want))
		}
	}
}

// renderJSON はテストの失敗メッセージ用に、設定の値を読める形の JSON へ整形する。
// ポインタで持つフィールド（null を区別するフィールド）がアドレスで表示されてしまう
// %+v の代わりに使う。
//
// t: テストコンテキスト。
// v: 整形したい値。
// 戻り値: インデント付きの JSON 文字列。整形に失敗した場合はテストを失敗させる。
func renderJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("テストの失敗メッセージを組み立てられません: %v", err)
	}
	return string(b)
}

// 目的: テストやコードの注釈が指している設計文書の節が、実在することを確かめる。
// 節を並べ替えたり番号を振り直したりしたときに、注釈だけが古い番号を指したまま残るのを防ぐ。
// 実例として、展開規則を指す注釈が 5-4（「2回目以降のプロンプト」）を指したまま残っていた。
// 与える情報: internal/config と test/internal/config の注釈が参照している節の見出しの一覧。
// 成功条件: 一覧のすべてが設計文書の中に見出しとして存在すること。
func TestDesignDoc_注釈が参照している節が実在する(t *testing.T) {
	raw, err := os.ReadFile(designDocPath)
	if err != nil {
		t.Fatalf("設計文書を読み込めません（%s）: %v", designDocPath, err)
	}
	doc := string(raw)

	headings := []string{
		"### 3-7.",  // 識別子の正規化
		"### 3-9.",  // worktree と branch の後始末
		"### 3-10.", // 実行中の Status も作業中に含める
		"### 3-12.", // hook をどう届けるか
		"### 3-17.", // 二重起動は flock で防ぐ
		"### 3-21.", // stall の時計
		"### 3-23.", // hook を受ける socket の置き場所
		"### 3-27.", // レートリミット
		"### 4-1.",  // Status の構成
		"### 5-1.",  // ファイルの名前と探し方（相対パスの基準）
		"### 5-2.",  // front matter（設定）
		"### 5-5.",  // 設定値の展開規則
		"### 8-1.",  // 意図的に外している仕様
	}
	for _, heading := range headings {
		if !strings.Contains(doc, heading) {
			t.Errorf("設計文書に %q が無い。節を並べ替えたなら、参照している注釈も直すこと", heading)
		}
	}
}
