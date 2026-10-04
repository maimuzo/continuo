// Package statussignal_test は internal/statussignal（表明の読み方と、取り得る値の一覧の文面）を
// 検証する（issue #274。設計 3-25）。
//
// **行の拾い方そのものは、`test/internal/orchestrator/signal_test.go` が `ParseSignals` を通して
// 検査している**（`ParseSignals` は `statussignal.Parse` を呼ぶだけである）。
// ここでは、issue #274 で足したもの（取り得る値に在るかの判定・文面・ファイル）を検査する。
package statussignal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/statussignal"
)

const (
	prefix  = "CONTINUO-STATUS:"
	current = "octocat/hello-world#274"
)

// strPtr は文字列のポインタを返す（対応表の行き先を書くため）。
func strPtr(s string) *string { return &s }

// defaultValues は既定の対応表と同じ形を返す（`working` は Status を動かさない）。
func defaultValues() map[string]*string {
	return map[string]*string{
		"review":  strPtr("In Review"),
		"blocked": strPtr("Blocked"),
		"working": nil,
	}
}

// TestFindInvalid_取り得る値に無い表明だけを集める は、決まり以外の値の見つけ方を確かめる。
//
// 目的: 取り得る値に在る表明は数えず、無い表明だけを、対象の名前順で返すこと。
// 与える情報: 自分の issue は `done`、別の issue #45 は `review`、#47 は `finished`。
// 成功条件: `done` と `finished` の2件が、対象の名前順に返る。`review` は返らない。
func TestFindInvalid_取り得る値に無い表明だけを集める(t *testing.T) {
	signals := statussignal.Parse([]string{
		"終わりました。\nCONTINUO-STATUS: done\nCONTINUO-STATUS: #45 review\nCONTINUO-STATUS: #47 finished",
	}, prefix, current)

	got := statussignal.FindInvalid(signals, defaultValues())

	if len(got) != 2 {
		t.Fatalf("決まり以外の値は2件のはず: %+v", got)
	}
	if got[0].Target != current || got[0].Value != "done" {
		t.Errorf("1件目が自分の issue の done でない: %+v", got[0])
	}
	if got[1].Target != "octocat/hello-world#47" || got[1].Value != "finished" {
		t.Errorf("2件目が #47 の finished でない: %+v", got[1])
	}
}

// TestFindInvalid_全部が取り得る値なら1件も返さない は、正しい表明を決まり以外と数えないことを確かめる。
//
// 目的: 決まりどおりの値を書いた応答を、hook が差し戻さず、本体も書き直しを求めないこと。
// 与える情報: `review` と `#45 blocked` と、何も書いていない応答。
// 成功条件: どちらも0件。
func TestFindInvalid_全部が取り得る値なら1件も返さない(t *testing.T) {
	for _, text := range []string{
		"CONTINUO-STATUS: review\nCONTINUO-STATUS: #45 blocked",
		"表明を書いていない応答です。",
	} {
		signals := statussignal.Parse([]string{text}, prefix, current)
		if got := statussignal.FindInvalid(signals, defaultValues()); len(got) != 0 {
			t.Errorf("決まり以外の値として数えている: %q → %+v", text, got)
		}
	}
}

// TestFindInvalid_同じ対象は最後の行だけを見る は、応答の途中の引用で差し戻さないことを確かめる。
//
// 目的: 同じ対象の行が複数あるとき、最後の行だけで判定すること（本体の読み方と同じ）。
// 与える情報: `done` のあとに `review` を書いた応答と、その逆順。
// 成功条件: 前者は0件、後者は `done` の1件。
func TestFindInvalid_同じ対象は最後の行だけを見る(t *testing.T) {
	ok := statussignal.Parse([]string{"CONTINUO-STATUS: done\nCONTINUO-STATUS: review"}, prefix, current)
	if got := statussignal.FindInvalid(ok, defaultValues()); len(got) != 0 {
		t.Errorf("最後の行が正しいのに、決まり以外と数えている: %+v", got)
	}
	ng := statussignal.Parse([]string{"CONTINUO-STATUS: review\nCONTINUO-STATUS: done"}, prefix, current)
	if got := statussignal.FindInvalid(ng, defaultValues()); len(got) != 1 || got[0].Value != "done" {
		t.Errorf("最後の行が決まり以外なのに、見つけていない: %+v", got)
	}
}

// TestFindInvalid_対象なしの行と自分の番号を書いた行は同じ対象である は、対象の解決を確かめる。
//
// **hook がいま作業している issue を知らないと、ここが本体とずれる。**`#274 done` と
// 対象なしの `review` を別の対象と読むと、最後の行が正しいのに差し戻す。
//
// 目的: 自分の番号を書いた行と対象なしの行を、同じ対象として最後の行で判定すること。
// 与える情報: `#274 done` のあとに対象なしの `review`。
// 成功条件: 0件。
func TestFindInvalid_対象なしの行と自分の番号を書いた行は同じ対象である(t *testing.T) {
	signals := statussignal.Parse([]string{"CONTINUO-STATUS: #274 done\nCONTINUO-STATUS: review"}, prefix, current)
	if got := statussignal.FindInvalid(signals, defaultValues()); len(got) != 0 {
		t.Errorf("同じ対象の最後の行が正しいのに、決まり以外と数えている: %+v", got)
	}
}

// TestLookup_大文字と前後の空白を区別しない は、hook と本体が同じ比べ方をすることを確かめる。
//
// **設定のキーの検査は「空でない」だけである。**`Review` と書いた利用者でも、
// `Parse` が小文字にした値 `review` と一致しなければならない。一致しないと、
// Status を動かした値を「決まり以外」と返すことになる。
//
// 目的: キーの大文字と前後の空白を区別せずに引けること。
// 与える情報: キーが ` Review ` の対応表と、値 `review`。
// 成功条件: 見つかり、行き先が `In Review` である。
func TestLookup_大文字と前後の空白を区別しない(t *testing.T) {
	values := map[string]*string{" Review ": strPtr("In Review")}

	dest, ok := statussignal.Lookup(values, "review")

	if !ok || dest == nil || *dest != "In Review" {
		t.Fatalf("大文字と空白の違うキーを引けていない: ok=%v dest=%v", ok, dest)
	}
	signals := statussignal.Parse([]string{"CONTINUO-STATUS: review"}, prefix, current)
	if got := statussignal.FindInvalid(signals, values); len(got) != 0 {
		t.Errorf("キーの大文字の違いで、正しい値を決まり以外と数えている: %+v", got)
	}
}

// TestBlockReason_値と取り得る値の一覧を名前順で載せる は、差し戻しの本文を確かめる。
//
// 目的: 何が違ったか（書いた値）と、取り得る値の全部と、それぞれの行き先が伝わること。
// 与える情報: 自分の issue の `done`。
// 成功条件: 値が「」付きで載り、一覧が blocked → review → working の順に並び、
// 行き先と「動かしません」が載り、行頭から書き直すよう求めている。
func TestBlockReason_値と取り得る値の一覧を名前順で載せる(t *testing.T) {
	got := statussignal.BlockReason(prefix, current,
		[]statussignal.Invalid{{Target: current, Value: "done"}}, defaultValues())

	for _, want := range []string{
		"この応答の表明の値 「done」 は、決められた値ではありません",
		"この値では Status を動かせません",
		"- `CONTINUO-STATUS: blocked` … Status を Blocked へ動かします",
		"- `CONTINUO-STATUS: review` … Status を In Review へ動かします",
		"- `CONTINUO-STATUS: working` … Status を動かしません",
		"作業は進めず、この中から選んで、応答の最後に、行頭から1行で書き直してください",
		"Status を動かす値から選んでください",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("本文に %q が無い:\n%s", want, got)
		}
	}
	b, r, w := strings.Index(got, "blocked`"), strings.Index(got, "review`"), strings.Index(got, "working`")
	if b >= r || r >= w {
		t.Errorf("一覧が名前順でない（blocked=%d review=%d working=%d）:\n%s", b, r, w, got)
	}
	if strings.Contains(got, current) {
		t.Errorf("自分の issue の表明なのに、対象を載せている:\n%s", got)
	}
}

// TestBlockReason_一覧を書き写しても表明として読まれない は、文面が新しい誤りを作らないことを確かめる。
//
// **一覧の行が印から始まっていると、エージェントが一覧を応答へ書き写しただけで、
// 名前順の最後の値（既定では `working`）が表明として採られる。**
//
// 目的: 本文をそのまま応答に書いても、`Parse` が1件も拾わないこと。
// 与える情報: `BlockReason` が返した本文そのもの。
// 成功条件: 拾った表明が0件。
func TestBlockReason_一覧を書き写しても表明として読まれない(t *testing.T) {
	reason := statussignal.BlockReason(prefix, current,
		[]statussignal.Invalid{
			{Target: current, Value: "done"},
			{Target: "octocat/hello-world#45", Value: "finished"},
		}, defaultValues())

	if got := statussignal.Parse([]string{reason}, prefix, current); len(got) != 0 {
		t.Fatalf("本文を書き写すと表明として読まれる: %+v\n%s", got, reason)
	}
}

// TestBlockReason_別のissueを指す表明は対象を付けて書き直させる は、グループの run での文面を確かめる。
//
// **一覧の行は対象を書かない形である。**そのまま写すと、直したかった issue の代わりに、
// いま作業している issue の Status が動く。
//
// 目的: 別の issue を指す表明が決まり以外だったとき、対象を付けた形で書き直すよう求めること。
// 与える情報: 同じリポジトリの #45 の `done` と、別のリポジトリの issue の `done`。
// 成功条件: 同じリポジトリは `#45 <値>`、別のリポジトリは `<owner>/<repo>#<番号> <値>` の形を示す。
func TestBlockReason_別のissueを指す表明は対象を付けて書き直させる(t *testing.T) {
	got := statussignal.BlockReason(prefix, current, []statussignal.Invalid{
		{Target: "octocat/hello-world#45", Value: "done"},
		{Target: "octocat/spoon-knife#9", Value: "done"},
	}, defaultValues())

	for _, want := range []string{
		"octocat/hello-world#45 の値 「done」",
		"`CONTINUO-STATUS: #45 <値>` の形で書き直してください",
		"`CONTINUO-STATUS: octocat/spoon-knife#9 <値>` の形で書き直してください",
		"対象を付けずに書くと、いま作業している issue の表明になります",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("本文に %q が無い:\n%s", want, got)
		}
	}
}

// TestBlockReason_値の制御文字を落とし長さを切りbacktickは残す は、エージェントが書いた値の載せ方を確かめる。
//
// **値はエージェントの出力で、そのまま pane への入力に入る。**制御文字は落とす。
// **backtick は残す。**落とすと、backtick 付きで書いた `review` が、一覧に在る値と
// 同じに見えて、何が違ったのかが伝わらない。
//
// 目的: 制御文字を落とすこと・40字で切ること・backtick を残すこと・何も残らなければ断ること。
// 与える情報: 制御文字入りの値、長い値、backtick 付きの値、制御文字だけの値。
// 成功条件: それぞれ、制御文字が無い・切った印が付く・backtick が見える・「表示できない値」と出る。
func TestBlockReason_値の制御文字を落とし長さを切りbacktickは残す(t *testing.T) {
	reason := func(value string) string {
		return statussignal.BlockReason(prefix, current,
			[]statussignal.Invalid{{Target: current, Value: value}}, defaultValues())
	}

	if got := reason("do\x1b[31mne"); strings.Contains(got, "\x1b") || !strings.Contains(got, "「do[31mne」") {
		t.Errorf("制御文字を落としていない:\n%q", got)
	}
	long := strings.Repeat("あ", 60)
	if got := reason(long); strings.Contains(got, long) || !strings.Contains(got, strings.Repeat("あ", 40)+"…」") {
		t.Errorf("長い値を40字で切っていない:\n%s", got)
	}
	if got := reason("`review`"); !strings.Contains(got, "「`review`」") {
		t.Errorf("backtick を落としている（何が違ったのかが伝わらない）:\n%s", got)
	}
	if got := reason("\x07\x08"); !strings.Contains(got, "（表示できない値）") {
		t.Errorf("何も残らない値の断りが無い:\n%s", got)
	}
}

// TestBlockReason_Statusを動かす値が無い対応表ではその1文を出さない は、実行できない指示を出さないことを確かめる。
//
// 目的: 行き先が全部 null の対応表では、「Status を動かす値から選んで」を出さないこと。
// 与える情報: `working` だけの対応表。
// 成功条件: その1文が本文に無い。
func TestBlockReason_Statusを動かす値が無い対応表ではその1文を出さない(t *testing.T) {
	got := statussignal.BlockReason(prefix, current,
		[]statussignal.Invalid{{Target: current, Value: "done"}}, map[string]*string{"working": nil})

	if strings.Contains(got, "Status を動かす値から選んでください") {
		t.Errorf("選べる値が無いのに、選ぶよう求めている:\n%s", got)
	}
}

// TestFile_書いて読み戻すと同じ中身になる は、hook へ渡すファイルの形を確かめる。
//
// 目的: 本体が書いた中身を hook が同じ意味で読めること。**null の行き先が null のまま残ること**
// （空文字へ化けると、`working` が「Status を空文字へ動かす値」に見える）。
// 与える情報: 既定の対応表と識別子と印。
// 成功条件: 読み戻した中身が同じで、`Usable` が真。
func TestFile_書いて読み戻すと同じ中身になる(t *testing.T) {
	dir := t.TempDir()
	path := statussignal.PathInIssueDir(dir)
	data, err := statussignal.Encode(statussignal.File{Identifier: current, Prefix: prefix, Values: defaultValues()})
	if err != nil {
		t.Fatalf("Encode に失敗した: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("ファイルを書けない: %v", err)
	}

	got, err := statussignal.Read(path)

	if err != nil {
		t.Fatalf("Read に失敗した: %v", err)
	}
	if !got.Usable() || got.Identifier != current || got.Prefix != prefix {
		t.Fatalf("読み戻した中身が違う: %+v", got)
	}
	if dest, ok := got.Values["working"]; !ok || dest != nil {
		t.Errorf("null の行き先が null のまま残っていない: ok=%v dest=%v", ok, dest)
	}
	if dest := got.Values["review"]; dest == nil || *dest != "In Review" {
		t.Errorf("行き先が違う: %v", dest)
	}
}

// TestFile_材料が欠けていれば使えない は、hook が調べない条件を確かめる。
//
// 目的: 識別子・印・値のどれかが欠けたファイルを、使えないと判定すること。
// 与える情報: 1つずつ欠かした中身。
// 成功条件: どれも `Usable` が偽。
func TestFile_材料が欠けていれば使えない(t *testing.T) {
	for name, f := range map[string]statussignal.File{
		"識別子が無い": {Prefix: prefix, Values: defaultValues()},
		"印が無い":   {Identifier: current, Values: defaultValues()},
		"値が無い":   {Identifier: current, Prefix: prefix},
	} {
		if f.Usable() {
			t.Errorf("%s のに、使えると判定している", name)
		}
	}
}

// TestPathFromPendingDir_逃がし先の親に置く は、hook がファイルを探す場所を確かめる。
//
// **逃がし先の中には置かない。**本体は逃がし先の `.json` を、届かなかった hook として読む。
//
// 目的: `--pending-dir` の親（issue ごとのディレクトリ）の直下を指すこと。
// 与える情報: `<実行時ディレクトリ>/issues/<スラグ>/pending` と、末尾に `/` を付けた同じパス。
// 成功条件: どちらも `<実行時ディレクトリ>/issues/<スラグ>/status-signal.json`。
func TestPathFromPendingDir_逃がし先の親に置く(t *testing.T) {
	issueDir := filepath.Join(string(filepath.Separator), "run", "issues", "octocat-hello-world-274")
	want := filepath.Join(issueDir, "status-signal.json")

	for _, pending := range []string{
		filepath.Join(issueDir, "pending"),
		filepath.Join(issueDir, "pending") + string(filepath.Separator),
	} {
		if got := statussignal.PathFromPendingDir(pending); got != want {
			t.Errorf("置き場所が違う: got %q, want %q", got, want)
		}
	}
	if got := statussignal.PathInIssueDir(issueDir); got != want {
		t.Errorf("本体が書く場所と hook が読む場所が違う: got %q, want %q", got, want)
	}
}
