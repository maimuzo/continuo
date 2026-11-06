package config_test

// WORKFLOW.local.md を WORKFLOW.md の上に重ねる振る舞い（設計 5-1a / issue #325）を確かめる。

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// writeLocal は WORKFLOW.md と同じディレクトリに WORKFLOW.local.md を書く。
//
// t: テストコンテキスト。
// workflowPath: WORKFLOW.md の絶対パス。
// content: WORKFLOW.local.md の全文（front matter の区切りも含めて渡す）。
// 戻り値: 書き込んだ WORKFLOW.local.md の絶対パス。
func writeLocal(t *testing.T, workflowPath, content string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(workflowPath), "WORKFLOW.local.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用の WORKFLOW.local.md を書き込めません: %v", err)
	}
	return path
}

// teamFrontMatter は、チームで共有する WORKFLOW.md に見立てた front matter である。
// マージンと入札の締め切りを既定値と違う値にしてあり、上書きされなかった値が残るかを見分けられる。
const teamFrontMatter = validFrontMatter +
	"    handoff:\n" +
	"      bid_window_ms: 120000\n" +
	"      five_hour_margin_percent: 15\n" +
	"      weekly_margin_percent: 20\n" +
	"claude:\n" +
	"  env:\n" +
	"    TEAM_ONLY: \"1\"\n" +
	"trust:\n" +
	"  repositories:\n" +
	"    - acme/team-a\n" +
	"    - acme/team-b\n"

// mustLoad は Load を呼び、誤りなら止める。
func mustLoad(t *testing.T, path string) *config.Loaded {
	t.Helper()
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load が誤りを返した: %v", err)
	}
	return loaded
}

// 目的: WORKFLOW.local.md が無ければ、いままでと同じに読めることを確かめる（issue の「無くても動く」）。
// 与える情報: チームの WORKFLOW.md だけを置いたディレクトリ。
// 成功条件: 値が WORKFLOW.md のとおりで、LocalPath と LocalPromptTemplate が空であること。
func TestLoadLocal_無ければいままでと同じ(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "チームの本文\n")
	loaded := mustLoad(t, path)
	if loaded.LocalPath != "" || loaded.LocalPromptTemplate != "" {
		t.Errorf("WORKFLOW.local.md が無いのに重ねた扱いになった: %q / %q", loaded.LocalPath, loaded.LocalPromptTemplate)
	}
	if got := loaded.Config.Tracker.Provider.Handoff.WeeklyMarginPercent; got != 20 {
		t.Errorf("weekly_margin_percent = %d、want 20", got)
	}
	if loaded.SourceLabel() != path {
		t.Errorf("SourceLabel = %q、want %q", loaded.SourceLabel(), path)
	}
}

// 目的: WORKFLOW.local.md に書いた1キーだけが上書きされ、兄弟のキーはチームの値のまま残ることを確かめる。
// 与える情報: WORKFLOW.local.md に weekly_margin_percent だけを書く。
// 成功条件: weekly_margin_percent が 30、five_hour_margin_percent と bid_window_ms と owner がチームの値のままであること。
func TestLoadLocal_書いた1キーだけが上書きされる(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "チームの本文\n")
	localPath := writeLocal(t, path, "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 30\n---\n")
	loaded := mustLoad(t, path)

	h := loaded.Config.Tracker.Provider.Handoff
	if h.WeeklyMarginPercent != 30 {
		t.Errorf("weekly_margin_percent = %d、want 30", h.WeeklyMarginPercent)
	}
	if h.FiveHourMarginPercent != 15 || h.BidWindowMs != 120000 {
		t.Errorf("兄弟のキーが変わった: five_hour=%d bid_window=%d", h.FiveHourMarginPercent, h.BidWindowMs)
	}
	if loaded.Config.Tracker.Provider.Owner != "acme" {
		t.Errorf("owner = %q、want acme", loaded.Config.Tracker.Provider.Owner)
	}
	if loaded.LocalPath != localPath {
		t.Errorf("LocalPath = %q、want %q", loaded.LocalPath, localPath)
	}
	if loaded.Path != path {
		t.Errorf("Path が WORKFLOW.md でなくなった: %q", loaded.Path)
	}
}

// 目的: 値を書かない行（null）が、チームの値を既定値へ戻さないことを確かめる（設計レビュー1周目の HIGH）。
// 与える情報: WORKFLOW.local.md に `handoff:` だけ（子は全部注釈）を書く。
// 成功条件: handoff の値がチームの値のまま残ること。
func TestLoadLocal_値を書かない行はチームの値を残す(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	writeLocal(t, path, "---\ntracker:\n  provider:\n    handoff:\n      # weekly_margin_percent: 30\n---\n")
	loaded := mustLoad(t, path)

	h := loaded.Config.Tracker.Provider.Handoff
	if h.WeeklyMarginPercent != 20 || h.FiveHourMarginPercent != 15 || h.BidWindowMs != 120000 {
		t.Errorf("チームの値が消えた: %+v", h)
	}
}

// 目的: front matter が空・空行・注釈だけの WORKFLOW.local.md が、設定を何も変えないことを確かめる。
// goccy/go-yaml は空の文書で構造体を全部ゼロにするので、読まずに飛ばす必要がある。
// 与える情報: 4通りの空の front matter。
// 成功条件: どれも WORKFLOW.md だけを読んだときと同じ Config になること。
func TestLoadLocal_空のfront_matterは何も変えない(t *testing.T) {
	base := mustLoad(t, writeWorkflow(t, teamFrontMatter, "本文\n"))
	for name, fm := range map[string]string{
		"空":     "",
		"空行":    "\n\n",
		"注釈だけ":  "# 個人の上書きはまだ無い\n",
		"空白と注釈": "  \n# a\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeWorkflow(t, teamFrontMatter, "本文\n")
			writeLocal(t, path, "---\n"+fm+"---\n")
			loaded := mustLoad(t, path)
			if !reflect.DeepEqual(loaded.Config, base.Config) {
				t.Errorf("空の front matter で設定が変わった")
			}
		})
	}
}

// 目的: map とリストは、WORKFLOW.md に同じ文字を書いたときと同じく丸ごと置き換わることを確かめる。
// 与える情報: WORKFLOW.local.md に claude.env（map）と trust.repositories（リスト）を書く。
// 成功条件: どちらもチームの要素が残らず、WORKFLOW.local.md の要素だけになること。
func TestLoadLocal_mapとリストは丸ごと置き換わる(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	writeLocal(t, path, "---\nclaude:\n  env:\n    MINE: \"2\"\ntrust:\n  repositories:\n    - acme/mine\n---\n")
	loaded := mustLoad(t, path)

	if want := map[string]string{"MINE": "2"}; !reflect.DeepEqual(loaded.Config.Claude.Env, want) {
		t.Errorf("claude.env = %v、want %v", loaded.Config.Claude.Env, want)
	}
	if want := []string{"acme/mine"}; !reflect.DeepEqual(loaded.Config.Trust.Repositories, want) {
		t.Errorf("trust.repositories = %v、want %v", loaded.Config.Trust.Repositories, want)
	}
}

// 目的: WORKFLOW.local.md の未知のキーは、WORKFLOW.local.md のパスを名指して止まることを確かめる。
// 与える情報: WORKFLOW.local.md に存在しないキー。
// 成功条件: 誤りが返り、その文言に WORKFLOW.local.md のパスと「front matter が不正」が入ること。
func TestLoadLocal_未知のキーはWORKFLOW_local_mdを名指す(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	localPath := writeLocal(t, path, "---\nno_such_key: 1\n---\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("未知のキーがあるのに誤りが返らなかった")
	}
	if !strings.Contains(err.Error(), localPath) || !strings.Contains(err.Error(), "front matter が不正です") {
		t.Errorf("誤りが WORKFLOW.local.md を名指していない: %v", err)
	}
}

// 目的: 重ねたあとの値の誤りは、両方のファイルを名指すことを確かめる。
// 与える情報: WORKFLOW.local.md に範囲外の weekly_margin_percent。
// 成功条件: 誤りの文言に、WORKFLOW.md と WORKFLOW.local.md の両方のパスが入ること。
func TestLoadLocal_値の誤りは両方を名指す(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	localPath := writeLocal(t, path, "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 150\n---\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("範囲外の値なのに誤りが返らなかった")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), localPath) {
		t.Errorf("誤りが両方のファイルを名指していない: %v", err)
	}
}

// 目的: WORKFLOW.local.md も WORKFLOW.md と同じ文法（front matter の区切りが必須）であることを確かめる。
// 与える情報: 区切りの無い WORKFLOW.local.md。
// 成功条件: 誤りが返り、WORKFLOW.local.md のパスを名指すこと。
func TestLoadLocal_区切りが無ければ止まる(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	localPath := writeLocal(t, path, "tracker:\n  provider:\n    owner: other\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("区切りが無いのに誤りが返らなかった")
	}
	if !strings.Contains(err.Error(), localPath) {
		t.Errorf("誤りが WORKFLOW.local.md を名指していない: %v", err)
	}
}

// 目的: WORKFLOW.local.md が在るのに読めないときは、黙って飛ばさずに止まることを確かめる。
// 与える情報: 権限を外した WORKFLOW.local.md。
// 成功条件: 誤りが返り、WORKFLOW.local.md のパスを名指すこと。
func TestLoadLocal_在るのに読めなければ止まる(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("権限で読めなくする手が使えない環境")
	}
	path := writeWorkflow(t, teamFrontMatter, "")
	localPath := writeLocal(t, path, "---\n---\n")
	if err := os.Chmod(localPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(localPath, 0o600) })
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("読めないのに誤りが返らなかった")
	}
	if !strings.Contains(err.Error(), localPath) {
		t.Errorf("誤りが WORKFLOW.local.md を名指していない: %v", err)
	}
}

// 目的: 本文は重ねずに両方を返すことを確かめる（どちらを送るかは prompt.BodyOf が決める）。
// 与える情報: 両方に本文を書く。
// 成功条件: PromptTemplate が WORKFLOW.md の本文、LocalPromptTemplate が WORKFLOW.local.md の本文であること。
func TestLoadLocal_本文は両方を返す(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "チームの本文\n")
	writeLocal(t, path, "---\n---\n個人の本文\n")
	loaded := mustLoad(t, path)
	if loaded.PromptTemplate != "チームの本文\n" || loaded.LocalPromptTemplate != "個人の本文\n" {
		t.Errorf("本文 = %q / %q", loaded.PromptTemplate, loaded.LocalPromptTemplate)
	}
}

// 目的: WORKFLOW.local.md のパスの決め方を確かめる（設計 5-1a の表）。
// 与える情報: いろいろな名前の WORKFLOW のパス。
// 成功条件: 表のとおりのパス（重ねるものが無いときは空文字）が返ること。
func TestLocalPathFor_名前の決め方(t *testing.T) {
	for in, want := range map[string]string{
		"/w/WORKFLOW.md":       "/w/WORKFLOW.local.md",
		"/w/team.md":           "/w/team.local.md",
		"/w/WORKFLOW":          "/w/WORKFLOW.local",
		"/w/WORKFLOW.markdown": "/w/WORKFLOW.local.markdown",
		"/w/WORKFLOW.MD":       "/w/WORKFLOW.local.MD",
		"/w/WORKFLOW.local.md": "",
		"/w/team.local.md":     "",
		"/w/WORKFLOW.local":    "",
	} {
		if got := config.LocalPathFor(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("LocalPathFor(%q) = %q、want %q", in, got, want)
		}
	}
}

// 目的: 読み直しの印が、WORKFLOW.local.md を置いた・直した・消したときに変わることを確かめる（設計 5-1a / 3-24）。
// 与える情報: WORKFLOW.md だけ → WORKFLOW.local.md を置く → 中身を変える → 消す。
// 成功条件: 段ごとに印が前の段と違い、どの段でも StampOf が誤りを返さないこと。
func TestStampOf_WORKFLOW_local_mdの変化で印が変わる(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	stamp := func() config.FileStamp {
		t.Helper()
		s, err := config.StampOf(path)
		if err != nil {
			t.Fatalf("StampOf が誤りを返した: %v", err)
		}
		return s
	}
	s0 := stamp()
	localPath := writeLocal(t, path, "---\n---\n")
	s1 := stamp()
	if s0.Same(s1) {
		t.Error("WORKFLOW.local.md を置いても印が変わらない")
	}
	writeLocal(t, path, "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 30\n---\n")
	s2 := stamp()
	if s1.Same(s2) {
		t.Error("WORKFLOW.local.md を直しても印が変わらない")
	}
	if err := os.Remove(localPath); err != nil {
		t.Fatal(err)
	}
	s3 := stamp()
	if s2.Same(s3) {
		t.Error("WORKFLOW.local.md を消しても印が変わらない")
	}
	if !s0.Same(s3) {
		t.Error("WORKFLOW.local.md を消したのに、置く前と違う印になった")
	}
}

// 目的: WORKFLOW.local.md が読めないときも StampOf が誤りを返さず、印が変わることを確かめる。
// 誤りを返すと読み直しが黙って止まり、Load の「読めません」が出なくなる。
// 与える情報: 権限を外した WORKFLOW.local.md。
// 成功条件: StampOf が誤りを返さず、読めるときの印と違う印を返すこと。
func TestStampOf_読めないWORKFLOW_local_mdでも誤りを返さない(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("権限で読めなくする手が使えない環境")
	}
	path := writeWorkflow(t, teamFrontMatter, "")
	localPath := writeLocal(t, path, "---\n---\n")
	readable, err := config.StampOf(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(localPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(localPath, 0o600) })
	unreadable, err := config.StampOf(path)
	if err != nil {
		t.Fatalf("StampOf が誤りを返した: %v", err)
	}
	if readable.Same(unreadable) {
		t.Error("読めなくなっても印が変わらない")
	}
}

// 目的: doctor の direct chat の重なりの検査が、WORKFLOW.local.md も重ねて見ることを確かめる。
// 与える情報: WORKFLOW.local.md で direct_chat_state を running_state（In Progress）と同じにする。
// 成功条件: DirectChatConflictsInFile が tracker.running_state との重なりを返すこと。
func TestDirectChatConflictsInFile_WORKFLOW_local_mdも見る(t *testing.T) {
	path := writeWorkflow(t, teamFrontMatter, "")
	writeLocal(t, path, "---\ntracker:\n  direct_chat_state: In Progress\n---\n")
	conflicts, state, err := config.DirectChatConflictsInFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if state != "In Progress" {
		t.Errorf("direct_chat_state = %q、want In Progress", state)
	}
	found := false
	for _, k := range conflicts {
		if k == "tracker.running_state" {
			found = true
		}
	}
	if !found {
		t.Errorf("重なりに tracker.running_state が無い: %v", conflicts)
	}
}

// 目的: リンク先の無い symlink の WORKFLOW.local.md を「無い」と取り違えず、止まることを確かめる
// （実装レビュー1周目の HIGH。黙って飛ばすと個人の上書きが外れたことに気づけない）。
// 与える情報: 存在しない先を指す symlink を WORKFLOW.local.md として置く。
// 成功条件: Load が WORKFLOW.local.md のパスを名指す誤りを返し、StampOf は誤りを返さずに「無い」とは違う印を返すこと。
func TestLoadLocal_リンク先の無いsymlinkは止まる(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink を作れない環境がある")
	}
	path := writeWorkflow(t, teamFrontMatter, "")
	absent, err := config.StampOf(path)
	if err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(filepath.Dir(path), "WORKFLOW.local.md")
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere.md"), localPath); err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(path)
	if err == nil {
		t.Fatal("リンク先が無いのに誤りが返らなかった")
	}
	if !strings.Contains(err.Error(), localPath) {
		t.Errorf("誤りが WORKFLOW.local.md を名指していない: %v", err)
	}
	dangling, err := config.StampOf(path)
	if err != nil {
		t.Fatalf("StampOf が誤りを返した: %v", err)
	}
	if absent.Same(dangling) {
		t.Error("リンク先の無い symlink を置いても、無いときと同じ印になった")
	}
}

// 目的: チームの WORKFLOW.md の残り日数の表（`weekly_margin_percent_by_days_until_reset`）が、WORKFLOW.local.md で
// どう扱われるかを確かめる（issue #324。FAQ の「1週間のマージンを、リセットの直前だけ小さくしたい」と雛形のコメントに書いたこと）。
//
// 与える情報: チームの WORKFLOW.md に表 {1: 0, 2: 5}。WORKFLOW.local.md は3通り（`weekly_margin_percent` だけ・表を {1: 3}・表を {}）。
// 成功条件: `weekly_margin_percent` だけなら表は残る。表を書けば丸ごと置き換わる（2 は残らない）。{} なら表が外れる。
func TestLoadLocal_残り日数の表は丸ごと置き換わり空にすれば外れる(t *testing.T) {
	team := strings.Replace(teamFrontMatter, "      weekly_margin_percent: 20\n",
		"      weekly_margin_percent: 20\n      weekly_margin_percent_by_days_until_reset:\n        1: 0\n        2: 5\n", 1)
	if team == teamFrontMatter {
		t.Fatal("チームの front matter に表を足せない")
	}
	for _, c := range []struct {
		name  string
		local string
		want  map[int]int
	}{
		{"weekly_margin_percent だけなら表は残る", "      weekly_margin_percent: 30\n", map[int]int{1: 0, 2: 5}},
		{"表を書けば丸ごと置き換わる", "      weekly_margin_percent_by_days_until_reset:\n        1: 3\n", map[int]int{1: 3}},
		{"{} なら表が外れる", "      weekly_margin_percent_by_days_until_reset: {}\n", map[int]int{}},
		// **キーだけ書いて値を書かない行は、チームの表を残す**（雛形と FAQ に書いたこと。値の無い日の行を弾く型を置いても変わらない）。
		{"キーだけ書けば表は残る", "      weekly_margin_percent_by_days_until_reset:\n", map[int]int{1: 0, 2: 5}},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeWorkflow(t, team, "")
			writeLocal(t, path, "---\ntracker:\n  provider:\n    handoff:\n"+c.local+"---\n")
			got := mustLoad(t, path).Config.Tracker.Provider.Handoff.WeeklyMarginPercentByDaysUntilReset
			if len(got) != len(c.want) || !reflect.DeepEqual(map[int]int(got), c.want) && len(c.want) > 0 {
				t.Errorf("表 = %v、want %v", got, c.want)
			}
		})
	}
}
