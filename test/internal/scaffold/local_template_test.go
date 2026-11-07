package scaffold_test

import (
	"errors"
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
// **利用者は末尾の `.template` を消すだけで使える。**名前がずれると、消しても continuo が読まない。
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
// 名前を変えただけで効く形（注釈にしない）にしてある。
//
// **WORKFLOW.md は owner とカンバンの番号を埋める。**埋めないとプレースホルダで落ちる（config.Load の検査）。
//
// 与える情報: 値を埋めた WORKFLOW.md の雛形と、名前を変えた WORKFLOW.local.md の雛形。
// 成功条件: config.Load が通り、表の7日ぶんが雛形の値で読め、ほかの値は WORKFLOW.md のまま、
// 送る本文は WORKFLOW.md の本文であること。
func TestLocalTemplate_名前を変えると表が効く(t *testing.T) {
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
		t.Fatalf("雛形の名前を変えた WORKFLOW.local.md を読めません: %v", err)
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
func TestLocalTemplate_名前を変えなければ読まない(t *testing.T) {
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
		t.Errorf("名前を変えていない雛形を重ねています: %q", loaded.LocalPath)
	}
	if got := loaded.Config.Tracker.Provider.Handoff.WeeklyMarginPercentByDaysUntilReset; len(got) != 0 {
		t.Errorf("名前を変えていない雛形の表が効いています: %v", got)
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
// **名前を変えて使うこと・立て直しが要ること・要らなければ表ごと消すこと・.gitignore に入れることの4つである。**
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

// 目的: WORKFLOW.local.md が既にあるときは、--force でも雛形を置かず ErrLocalInUse を返し、
// 3枚とも在る扱いにすることを確かめる（issue #325 の実装レビュー1周目）。
//
// 与える情報: WORKFLOW.md・continuo-ci.yaml・WORKFLOW.local.md（リンク先の無い symlink も試す）を置いたディレクトリ。
// 成功条件: LocalErr が ErrLocalInUse で、LocalFailed が偽、force 無しなら AllExisted が真、
// どちらでも雛形が置かれていないこと。
func TestWriteAll_WORKFLOWlocalmdが在れば雛形を置かない(t *testing.T) {
	for _, kind := range []string{"file", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if res := scaffold.WriteAll(dir, false, scaffold.Values{}); res.WorkflowErr != nil || res.CIErr != nil {
				t.Fatalf("1枚目と2枚目を置けません: workflow=%v ci=%v", res.WorkflowErr, res.CIErr)
			}
			template := filepath.Join(dir, scaffold.LocalTemplateFileName())
			if err := os.Remove(template); err != nil {
				t.Fatalf("雛形を消せません: %v", err)
			}
			local := filepath.Join(dir, "WORKFLOW.local.md")
			switch kind {
			case "file":
				if err := os.WriteFile(local, []byte("---\n---\n"), 0o600); err != nil {
					t.Fatalf("WORKFLOW.local.md を書けません: %v", err)
				}
			case "dangling-symlink":
				if err := os.Symlink(filepath.Join(dir, "nowhere.md"), local); err != nil {
					t.Fatalf("symlink を作れません: %v", err)
				}
			}

			res := scaffold.WriteAll(dir, false, scaffold.Values{})
			if !errors.Is(res.LocalErr, scaffold.ErrLocalInUse) {
				t.Errorf("LocalErr が ErrLocalInUse ではありません: %v", res.LocalErr)
			}
			if res.LocalFailed() {
				t.Error("WORKFLOW.local.md が在ることを失敗として扱っています")
			}
			if !res.AllExisted() {
				t.Errorf("3枚とも在る扱いになっていません: workflow=%v ci=%v local=%v", res.WorkflowErr, res.CIErr, res.LocalErr)
			}
			// **名前で比べる。**macOS の一時ディレクトリは symlink の先（/private/var）で返るので、全体では比べられない。
			if filepath.Base(res.Local.Path) != filepath.Base(local) {
				t.Errorf("Local.Path が %q です（WORKFLOW.local.md であるべきです）", res.Local.Path)
			}

			forced := scaffold.WriteAll(dir, true, scaffold.Values{})
			if !errors.Is(forced.LocalErr, scaffold.ErrLocalInUse) {
				t.Errorf("--force で LocalErr が ErrLocalInUse ではありません: %v", forced.LocalErr)
			}
			if _, err := os.Lstat(template); !os.IsNotExist(err) {
				t.Errorf("WORKFLOW.local.md が在るのに雛形を置いています: %v", err)
			}
		})
	}
}
