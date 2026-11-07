package scaffold_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: `continuo init` が置く WORKFLOW.local.md.template の名前が、continuo が重ねるファイルの名前の
// 末尾に `.template` を足したものであることを確かめる（issue #325・設計 5-1a）。
//
// **利用者は `cp -n` で写すだけで使える。**名前がずれると、写した先を continuo が読まない。
//
// 与える情報: WORKFLOW.md のパス。
// 成功条件: config.LocalPathFor が返す名前に `.template` を足すと、雛形の名前になること。
func TestLocalTemplate_名前は重ねるファイルの名前にtemplateを足したもの(t *testing.T) {
	workflow := filepath.Join(t.TempDir(), scaffold.WorkflowFileName())
	want := filepath.Base(config.LocalPathFor(workflow)) + ".template"
	if got := scaffold.LocalTemplateFileName(); got != want {
		t.Errorf("雛形の名前が %q です（%q であるべきです）", got, want)
	}
}

// 目的: 雛形の名前を WORKFLOW.local.md へ変えて WORKFLOW.md の雛形の隣に置くと、
// continuo がそのまま読み、残り日数ごとの1週間のマージンの表が効くことを確かめる（issue #325 のコメント 6699736243）。
//
// **人間の依頼は「WORKFLOW.local.md.template 側には1日ごとの週間レートリミットを上書きする設定を入れておいて」である。**
// 写しただけで効く形（注釈にしない）にしてある。
//
// **WORKFLOW.md は owner とカンバンの番号を埋める。**埋めないとプレースホルダで落ちる（config.Load の検査）。
//
// 与える情報: 値を埋めた WORKFLOW.md の雛形と、WORKFLOW.local.md へ写した雛形。
// 成功条件: config.Load が通り、表の7日ぶんが雛形の値で読め、ほかの値は WORKFLOW.md のまま、
// 送る本文は WORKFLOW.md の本文であること。
func TestLocalTemplate_写すと表が効く(t *testing.T) {
	dir := t.TempDir()
	workflow := filepath.Join(dir, scaffold.WorkflowFileName())
	if err := os.WriteFile(workflow, []byte(scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	local := config.LocalPathFor(workflow)
	if err := os.WriteFile(local, []byte(scaffold.LocalTemplate()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.local.md を書けません: %v", err)
	}

	loaded, err := config.Load(workflow)
	if err != nil {
		t.Fatalf("雛形を写した WORKFLOW.local.md を読めません: %v", err)
	}
	if loaded.LocalPath != local {
		t.Errorf("WORKFLOW.local.md を重ねていません: LocalPath=%q", loaded.LocalPath)
	}
	got := loaded.Config.Tracker.Provider.Handoff.WeeklyMarginPercentByDaysUntilReset
	want := map[int]int{1: 0, 2: 5, 3: 10, 4: 10, 5: 10, 6: 10, 7: 10}
	if len(got) != len(want) {
		t.Errorf("読めた日が %d 個です（%d 個であるべきです）: %v", len(got), len(want), got)
	}
	for day, v := range want {
		if got[day] != v {
			t.Errorf("残り %d 日の値が %d です（%d であるべきです）", day, got[day], v)
		}
	}
	// **書いた分だけが上書きされる。**WORKFLOW.md で埋めた値は残る。
	if loaded.Config.Tracker.Provider.Owner != "octocat" {
		t.Errorf("WORKFLOW.md の owner が消えています: %q", loaded.Config.Tracker.Provider.Owner)
	}
	// **本文は空なので、WORKFLOW.md の本文が送られる。**
	if _, from := prompt.BodyOf(loaded); from != workflow {
		t.Errorf("送る本文の出どころが %q です（WORKFLOW.md であるべきです）", from)
	}
}

// 目的: 雛形を `.template` の名前のまま置いても、continuo は読まないことを確かめる（issue #325）。
//
// **`continuo init` を叩いただけで、チームの WORKFLOW.md の値が変わってはならない。**
//
// 与える情報: `continuo init` が置いた3枚（WORKFLOW.md は値を埋めたもの）。
// 成功条件: config.Load が WORKFLOW.local.md を重ねず、表が空であること。
func TestLocalTemplate_写さなければ読まない(t *testing.T) {
	dir := t.TempDir()
	res := scaffold.WriteAll(dir, false, scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	if res.WorkflowErr != nil || res.CIErr != nil || res.LocalErr != nil {
		t.Fatalf("3枚を置けません: workflow=%v ci=%v local=%v", res.WorkflowErr, res.CIErr, res.LocalErr)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.LocalTemplateFileName())); err != nil {
		t.Fatalf("%s が置かれていません: %v", scaffold.LocalTemplateFileName(), err)
	}

	loaded, err := config.Load(filepath.Join(dir, scaffold.WorkflowFileName()))
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if loaded.LocalPath != "" {
		t.Errorf("写していない雛形を重ねています: %q", loaded.LocalPath)
	}
	if got := loaded.Config.Tracker.Provider.Handoff.WeeklyMarginPercentByDaysUntilReset; len(got) != 0 {
		t.Errorf("写していない雛形の表が効いています: %v", got)
	}
}

// 目的: 雛形の表の値が、WORKFLOW.md の雛形の同じ表の見本（注釈の行）と同じであることを確かめる（issue #325）。
//
// **片方だけ直すと、利用者はどちらの値が勧められているのかを読めなくなる。**
//
// 与える情報: 2つの雛形の全文。
// 成功条件: 1〜7 の各日の値が両方で同じであること。
func TestLocalTemplate_表の値はWORKFLOWmdの雛形の見本と同じ(t *testing.T) {
	workflowRow := regexp.MustCompile(`(?m)^      #   ([1-7]): ([0-9]+) `)
	localRow := regexp.MustCompile(`(?m)^        ([1-7]): ([0-9]+) `)
	parse := func(re *regexp.Regexp, s string) map[int]int {
		out := map[int]int{}
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			day, _ := strconv.Atoi(m[1])
			v, _ := strconv.Atoi(m[2])
			out[day] = v
		}
		return out
	}
	w := parse(workflowRow, scaffold.Template())
	l := parse(localRow, scaffold.LocalTemplate())
	if len(w) != 7 || len(l) != 7 {
		t.Fatalf("表の行を7つずつ読めません: WORKFLOW.md=%v WORKFLOW.local.md.template=%v", w, l)
	}
	for day := 1; day <= 7; day++ {
		if w[day] != l[day] {
			t.Errorf("残り %d 日の値が違います: WORKFLOW.md の見本=%d 雛形=%d", day, w[day], l[day])
		}
	}
}

// 目的: 雛形の頭書きが、利用者が使う前に知るべきことを書いていることを確かめる（issue #325 の設計レビュー1周目）。
//
// **写して使うこと・立て直しが要ること・要らなければ表ごと消すこと・.gitignore に入れることの4つである。**
// 落とすと、効いたと思い込む（立て直し）・写しただけで残り1日に使い切る（表ごと消す）、が起きる。
//
// 与える情報: 雛形の全文。
// 成功条件: 4つの文が全部あること。
func TestLocalTemplate_頭書きに使う前に知ることを書く(t *testing.T) {
	body := scaffold.LocalTemplate()
	for _, want := range []string{
		"cp -n WORKFLOW.local.md.template WORKFLOW.local.md を叩いて写し",
		"書き換えたら continuo を立て直してください（動いている continuo には効きません）",
		"表が要らなければ表ごと消してください",
		"WORKFLOW.local.md はチームと共有しないので、.gitignore に入れてください",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("雛形の頭書きに「%s」がありません", want)
		}
	}
}

// 目的: continuo-ci.yaml が「既にある」以外の理由で落ちても、WORKFLOW.local.md.template は書くことを確かめる
// （設計 5-3o の「3枚目」。issue #325 の実装レビュー1周目）。
//
// **2枚とも continuo が読まない見本で、互いに関係が無い。**片方の失敗でもう片方まで置けなくなると、利用者には理由が読めない。
//
// 与える情報: continuo-ci.yaml の名前でリンク先の無い symlink を置いたディレクトリ（symlink は辿らずに止まる）。
// 成功条件: CI が失敗として返り、雛形は書けていること。
func TestWriteAll_CIが落ちても雛形は書く(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "nowhere.yaml"), filepath.Join(dir, scaffold.CIFileName())); err != nil {
		t.Fatalf("symlink を作れません: %v", err)
	}

	res := scaffold.WriteAll(dir, false, scaffold.Values{})
	if res.WorkflowErr != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", res.WorkflowErr)
	}
	if !res.CIFailed() {
		t.Errorf("continuo-ci.yaml が失敗として返っていません: %v", res.CIErr)
	}
	if res.LocalErr != nil {
		t.Errorf("continuo-ci.yaml が落ちたので、雛形まで書いていません: %v", res.LocalErr)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.LocalTemplateFileName())); err != nil {
		t.Errorf("雛形が置かれていません: %v", err)
	}
}

// 目的: WORKFLOW.local.md が既にあっても雛形を置き、WORKFLOW.local.md には触らないことを確かめる
// （人間の依頼「continuo init する時に WORKFLOW.md と WORKFLOW.local.md.template を出力し」。issue #325 の実装レビュー2周目）。
//
// **既にある WORKFLOW.local.md を守るのは、写し方の案内（cp -n）である。**雛形は別の名前なので、置いても壊さない。
//
// 与える情報: WORKFLOW.md・continuo-ci.yaml・WORKFLOW.local.md を置き、雛形だけが無いディレクトリ。
// 成功条件: 雛形が置かれ、--force でも WORKFLOW.local.md が1バイトも変わらないこと。
func TestWriteAll_WORKFLOWlocalmdが在っても雛形は置き触らない(t *testing.T) {
	dir := t.TempDir()
	if res := scaffold.WriteAll(dir, false, scaffold.Values{}); res.WorkflowErr != nil || res.CIErr != nil || res.LocalErr != nil {
		t.Fatalf("3枚を置けません: workflow=%v ci=%v local=%v", res.WorkflowErr, res.CIErr, res.LocalErr)
	}
	template := filepath.Join(dir, scaffold.LocalTemplateFileName())
	local := filepath.Join(dir, "WORKFLOW.local.md")
	// **雛形を WORKFLOW.local.md へ動かす。**雛形が無く、WORKFLOW.local.md が在る状態を作る。
	if err := os.Rename(template, local); err != nil {
		t.Fatalf("雛形を WORKFLOW.local.md へ動かせません: %v", err)
	}
	const mine = "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 30\n---\n"
	if err := os.WriteFile(local, []byte(mine), 0o600); err != nil {
		t.Fatalf("WORKFLOW.local.md を書けません: %v", err)
	}

	res := scaffold.WriteAll(dir, false, scaffold.Values{})
	if res.LocalErr != nil {
		t.Errorf("WORKFLOW.local.md が在るのに雛形を置いていません: %v", res.LocalErr)
	}
	if res.AllExisted() {
		t.Error("雛形が無かったのに3枚とも在る扱いになっています")
	}
	if _, err := os.Stat(template); err != nil {
		t.Errorf("雛形が置かれていません: %v", err)
	}

	if forced := scaffold.WriteAll(dir, true, scaffold.Values{}); forced.LocalErr != nil || !forced.Local.Overwritten {
		t.Errorf("--force で雛形を置き換えていません: err=%v overwritten=%v", forced.LocalErr, forced.Local.Overwritten)
	}
	got, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("WORKFLOW.local.md を読めません: %v", err)
	}
	if string(got) != mine {
		t.Errorf("WORKFLOW.local.md が変わっています: %q", got)
	}
}
