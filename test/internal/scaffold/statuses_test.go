package scaffold_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// jaStatuses は日本語の選択肢名で埋めた割り当てである。
//
// **ボードの選択肢名は英語とは限らない。**引用符の付け方が壊れていないかを、
// 空白を含む名前（`レビュー 待ち`）で確かめる。
var jaStatuses = scaffold.Statuses{
	Dispatch: "着手待ち",
	Running:  "作業中",
	Review:   "レビュー 待ち",
	Blocked:  "保留",
	Done:     "完了",
}

// 目的: `continuo setup` が決めた割り当てが WORKFLOW.md の7つのキーへ書かれることを確認する。
// 与える情報: 本番のボードの選択肢名で埋めた Statuses。
// 成功条件: dispatch_state / running_state / failure_state / active_states / terminal_states /
// status_signal_map.review / status_signal_map.blocked に、割り当てた選択肢名が入ること。
// 雛形の既定値（`Ready` など）が残っていないこと。
func TestTemplateWithValues_割り当てた選択肢が7つのキーへ書かれる(t *testing.T) {
	out := scaffold.TemplateWithValues(scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
		Statuses: scaffold.Statuses{
			Dispatch: "Ready",
			Running:  "In Progress",
			Review:   "In Review",
			Blocked:  "Blocked",
			Done:     "Done",
		},
	})

	wants := []string{
		`  dispatch_state: "Ready"`,
		`  running_state: "In Progress"`,
		`  failure_state: "Blocked"`,
		`  active_states: ["Ready", "In Progress"]`,
		`  terminal_states: ["Done"]`,
		`    review: "In Review"`,
		`    blocked: "Blocked"`,
	}
	for _, w := range wants {
		if !containsLineWithPrefix(out, w) {
			t.Errorf("書き出しに %q で始まる行が無い", w)
		}
	}
}

// 目的: 日本語で空白を含む選択肢名でも、書き出した WORKFLOW.md が読み込めることを確認する。
// 与える情報: 日本語の選択肢名で埋めた Statuses と、埋めた owner / project_number。
// 成功条件: config.Load が通り、5つの役割の値が割り当てたとおりに読めること。
func TestWriteTemplateWithValues_日本語の選択肢名でも読み込める(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
		Statuses:      jaStatuses,
	})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("割り当てを書いた雛形を読み込めなかった: %v", err)
	}
	p := loaded.Config.Tracker
	if p.DispatchState != jaStatuses.Dispatch {
		t.Errorf("dispatch_state が違う: %q（期待 %q）", p.DispatchState, jaStatuses.Dispatch)
	}
	if p.RunningState != jaStatuses.Running {
		t.Errorf("running_state が違う: %q（期待 %q）", p.RunningState, jaStatuses.Running)
	}
	if p.FailureState != jaStatuses.Blocked {
		t.Errorf("failure_state が違う: %q（期待 %q）", p.FailureState, jaStatuses.Blocked)
	}
	if strings.Join(p.ActiveStates, "|") != jaStatuses.Dispatch+"|"+jaStatuses.Running {
		t.Errorf("active_states が違う: %v", p.ActiveStates)
	}
	if strings.Join(p.TerminalStates, "|") != jaStatuses.Done {
		t.Errorf("terminal_states が違う: %v", p.TerminalStates)
	}
	if got := p.StatusSignalMap["review"]; got == nil || *got != jaStatuses.Review {
		t.Errorf("status_signal_map.review が違う: %v（期待 %q）", got, jaStatuses.Review)
	}
	if got := p.StatusSignalMap["blocked"]; got == nil || *got != jaStatuses.Blocked {
		t.Errorf("status_signal_map.blocked が違う: %v（期待 %q）", got, jaStatuses.Blocked)
	}
}

// 目的: 5つの役割のうち1つでも欠けたら、雛形の既定値をそのまま残すことを確認する。
//
// **一部だけ差し替えると、割り当てた Status と既定値のままの Status が混ざる。**
// その WORKFLOW.md は起動時の照合で「ボードに無い Status」として落ちる。
//
// 与える情報: 完了だけ空にした Statuses。
// 成功条件: 雛形の既定値（`Ready` / `In Progress` / `Done`）がそのまま残ること。
func TestTemplateWithValues_役割が1つでも欠けたら既定値を残す(t *testing.T) {
	incomplete := jaStatuses
	incomplete.Done = ""

	out := scaffold.TemplateWithValues(scaffold.Values{Statuses: incomplete})
	wants := []string{
		`  dispatch_state: "Ready"`,
		`  running_state: "In Progress"`,
		`  terminal_states: ["Done"]`,
	}
	for _, w := range wants {
		if !containsLineWithPrefix(out, w) {
			t.Errorf("既定値の行 %q が残っていない", w)
		}
	}
	// 雛形のコメントにも「着手待ち」という語が出るので、**キーの行だけを見る。**
	if containsLineWithPrefix(out, `  dispatch_state: "`+jaStatuses.Dispatch+`"`) {
		t.Errorf("役割が欠けているのに割り当てを書き込んでいる")
	}
}

// containsLineWithPrefix は、prefix で始まる行が全文の中にあるかを返す。
//
// **行の途中に現れただけでは真にしない。**コメントの中の同じ語を拾わないためである。
//
// s: 探す対象の全文。
// prefix: 行の先頭から一致させる文字列。
// 戻り値: そのような行が1つでもあれば真。
func containsLineWithPrefix(s, prefix string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// 目的: `continuo setup` が既にある WORKFLOW.md の Status の7行だけを書き換え、
// 利用者が手で直した行を消さないことを確認する。
//
// **これが壊れると、段3 で消した trust.repositories の行や、書き換えた workspace.root が
// 段4 で元に戻る。**利用者から見ると「setup が設定を消した」ことになる。
//
// 与える情報: 雛形を書き出したあと、workspace.root と max_concurrent_agents を手で書き換え、
// trust.repositories の行を1つ足したファイル。
// 成功条件: 手で直した3箇所がそのまま残り、Status の7行だけが割り当てのとおりに変わること。
func TestUpdateStatuses_手で直した行を消さない(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
	})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}

	before, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("雛形を読み込めなかった: %v", err)
	}
	edited := strings.Replace(string(before),
		"  root: ~/worktrees ", "  root: ~/my-worktrees ", 1)
	edited = strings.Replace(edited,
		"  max_concurrent_agents: 2 ", "  max_concurrent_agents: 7 ", 1)
	edited = strings.Replace(edited,
		"  repositories: []", "  repositories:\n    - \"acme/only-this-one\"", 1)
	if edited == string(before) {
		t.Fatalf("手で直す対象の行が雛形に無い")
	}
	if err := os.WriteFile(result.Path, []byte(edited), 0o644); err != nil {
		t.Fatalf("手で直した内容を書き戻せなかった: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("Status の割り当てを書き換えられなかった: %v", err)
	}

	after, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き換えたファイルを読み込めなかった: %v", err)
	}
	for _, want := range []string{
		"  root: ~/my-worktrees ",
		"  max_concurrent_agents: 7 ",
		"    - \"acme/only-this-one\"",
		"    owner: acme ",
	} {
		if !strings.Contains(string(after), want) {
			t.Errorf("手で直した行 %q が残っていない", want)
		}
	}
	for _, want := range []string{
		`  dispatch_state: "着手待ち"`,
		`  running_state: "作業中"`,
		`  failure_state: "保留"`,
		`  active_states: ["着手待ち", "作業中"]`,
		`  terminal_states: ["完了"]`,
		`    review: "レビュー 待ち"`,
		`    blocked: "保留"`,
	} {
		if !containsLineWithPrefix(string(after), want) {
			t.Errorf("書き換えた行 %q が無い", want)
		}
	}
}

// 目的: 書き換えたあとも行の右側のコメントが残り、行数が増えも減りもしないことを確認する。
//
// 与える情報: 雛形をそのまま書き出したファイル。
// 成功条件: 7つのキーの行にコメントが残り、行数が書き換えの前後で同じであること。
func TestUpdateStatuses_コメントと行数を保つ(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	before, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("雛形を読み込めなかった: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("Status の割り当てを書き換えられなかった: %v", err)
	}
	after, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き換えたファイルを読み込めなかった: %v", err)
	}

	if got, want := strings.Count(string(after), "\n"), strings.Count(string(before), "\n"); got != want {
		t.Errorf("行数が変わった: %d 行（元は %d 行）", got, want)
	}
	for _, want := range []string{
		"# 着手待ちの Status。取り残された issue はここへ戻す",
		"# エージェントを起動したときに書き込む Status",
		"# 打ち切ったとき・失敗したときに落とす Status",
		"# 対象にする Status。下の running_state と dispatch_state を必ず含めること",
		"# 終わったとみなす Status。ここへ移った issue の worktree を片付ける",
		"# 作業が終わり、人間のレビューに回してよいとき",
		"# 判断を仰ぎたいとき、または失敗したとき",
	} {
		if !strings.Contains(string(after), want) {
			t.Errorf("コメント %q が消えている", want)
		}
	}
}

// 目的: 書き換えたあとの WORKFLOW.md が config.Load を通ることを確認する。
//
// **本文（プロンプト）を壊していないことも同時に確かめる。**本文には
// `CONTINUO-STATUS: review` のように front matter と似た形の行があるので、
// 範囲を切り違えるとそこを書き換えてしまう。
//
// 与える情報: 雛形を書き出して Status を書き換えたファイル。
// 成功条件: config.Load が通り、本文の表明の行がそのまま残っていること。
func TestUpdateStatuses_書き換えたあとも読み込める(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{
		Owner:         "acme",
		ProjectNumber: 3,
	})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("Status の割り当てを書き換えられなかった: %v", err)
	}

	loaded, err := config.Load(result.Path)
	if err != nil {
		t.Fatalf("書き換えたファイルを読み込めなかった: %v", err)
	}
	if loaded.Config.Tracker.DispatchState != jaStatuses.Dispatch {
		t.Errorf("dispatch_state が違う: %q", loaded.Config.Tracker.DispatchState)
	}
	if !strings.Contains(loaded.PromptTemplate, "CONTINUO-STATUS: review") {
		t.Errorf("本文の表明の行が壊れている")
	}
}

// 目的: WORKFLOW.md が無いときに ErrNotFound で止まることを確認する。
//
// **雛形を新規に作らない。**作るのは `continuo init` の仕事である。
//
// 与える情報: 空のディレクトリ。
// 成功条件: ErrNotFound が返り、ファイルが作られていないこと。
func TestUpdateStatuses_WORKFLOWが無ければ作らずに止まる(t *testing.T) {
	dir := t.TempDir()

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); !errors.Is(err, scaffold.ErrNotFound) {
		t.Fatalf("ErrNotFound が返らなかった: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "WORKFLOW.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("WORKFLOW.md を作ってしまっている: %v", err)
	}
	// 対話を始める前の検査も同じ理由で止まること。
	if _, err := scaffold.CheckUpdatable(dir); !errors.Is(err, scaffold.ErrNotFound) {
		t.Errorf("CheckUpdatable が ErrNotFound を返さなかった: %v", err)
	}
}

// 目的: 書き換える対象のキーが消されていたら ErrKeysNotFound で止まり、ファイルを変えないことを確認する。
//
// **黙って何もしないより落とす。**書き込んだつもりで進むと、巡回が無言で「対象0件」を返し続ける。
//
// 与える情報: dispatch_state の行を消した WORKFLOW.md。
// 成功条件: ErrKeysNotFound が返り、ファイルの中身が1バイトも変わっていないこと。
func TestUpdateStatuses_キーが無ければ書き換えずに止まる(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{})
	if err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("雛形を読み込めなかった: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "  dispatch_state:") {
			continue
		}
		kept = append(kept, line)
	}
	broken := strings.Join(kept, "\n")
	if broken == string(raw) {
		t.Fatalf("消す対象の行が雛形に無い")
	}
	if err := os.WriteFile(result.Path, []byte(broken), 0o644); err != nil {
		t.Fatalf("キーを消した内容を書き戻せなかった: %v", err)
	}

	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); !errors.Is(err, scaffold.ErrKeysNotFound) {
		t.Fatalf("ErrKeysNotFound が返らなかった: %v", err)
	}
	after, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("書き換えたファイルを読み込めなかった: %v", err)
	}
	if string(after) != broken {
		t.Errorf("止まったのにファイルを書き換えている")
	}
}

// 目的: 5つの役割が1つでも欠けていたら ErrStatusesIncomplete で止まることを確認する。
//
// 与える情報: 完了だけ空にした Statuses。
// 成功条件: ErrStatusesIncomplete が返ること。
func TestUpdateStatuses_役割が欠けていたら止まる(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	incomplete := jaStatuses
	incomplete.Done = ""

	if _, err := scaffold.UpdateStatuses(dir, incomplete); !errors.Is(err, scaffold.ErrStatusesIncomplete) {
		t.Fatalf("ErrStatusesIncomplete が返らなかった: %v", err)
	}
}

// 目的: StatusKeyNames が7つのキーを返し、それが実際に書き換えられるキーであることを確認する。
//
// **雛形からどれか1つでもキーが消えると UpdateStatuses が ErrKeysNotFound で落ちる。**
// TemplateWithValues は見つからなかったキーを報告しないので、ここで雛形の側を押さえる。
//
// 与える情報: 雛形をそのまま書き出したファイル。
// 成功条件: StatusKeyNames が7件返り、UpdateStatuses がエラー無しで通ること。
func TestStatusKeyNames_雛形に7つのキーが全部ある(t *testing.T) {
	names := scaffold.StatusKeyNames()
	if len(names) != 7 {
		t.Fatalf("書き換えるキーが %d 件（期待 7 件）: %v", len(names), names)
	}

	dir := t.TempDir()
	if _, err := scaffold.WriteTemplateWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("雛形を書き出せなかった: %v", err)
	}
	if _, err := scaffold.UpdateStatuses(dir, jaStatuses); err != nil {
		t.Fatalf("雛形に7つのキーが揃っていない: %v", err)
	}
}
