// 確認の画面で止まったときに、閉じる前に画面を1回読み、何で止まったかと直し方を引き渡しに載せることの検査である
// （設計 3-11 の「確認の画面の文を引き渡しに載せる」。issue #347）。
//
// **入力に使う画面は、実機で読んだものである**（testdata/blocked_screen/。Claude Code 2.1.322・herdr 0.9.1。
// 2026-11-12。`herdr agent read <名前> --source detection --lines 40` の出力。パスは伏せ字済み）。
//
//	bash.txt   Bash の権限の確認（`&&` でつないだコマンド。`don’t ask again for:` の行がある）
//	write.txt  Write の権限の確認（`Create file`）
//	trust.txt  フォルダの信頼の確認
//	ask.txt    AskUserQuestion（全幅の罫線が2本ある）
//
// **この package は `lang_test.go` の `TestMain` で日本語に固定されている。**だから日本語の文字列で見る。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// blockedScreenLog は、画面を読んだ結果をログへ書くときの msg に必ず入る文字列である。
// **この WARN はテスト自身が起こしている**（確認の画面で止まる台本を置いているため）。
const blockedScreenLog = "確認の画面で止まったときの画面を"

// blockedScreenFixture は testdata/blocked_screen/ の画面を1つ読む。
//
// t: 呼び出し元のテスト。
// name: ファイル名（bash.txt など）。
// 戻り値: 画面の文。
func blockedScreenFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "blocked_screen", name))
	if err != nil {
		t.Fatalf("実測した画面 %s を読めません: %v", name, err)
	}
	return string(b)
}

// 目的: 実測した4つの画面で、種類・見出し・許可が要る部分・抜粋の範囲を見分けられることを固定する。
//
// **見分けを外すと、直し方が出ない。**画面の文は載るが、人間は何を足せばよいかを自分で読み解くことになる。
//
// 与える情報: 実測した4つの画面。
// 成功条件: 種類・見出し・`don’t ask again for:` の後ろ・抜粋の頭と終わりが期待どおりであること。
func TestBlockedScreen_実測した画面の種類と見出しを見分ける(t *testing.T) {
	cases := []struct {
		file          string
		kind          string
		heading       string
		needsApproval string
		mustHave      []string
		mustNotHave   []string
	}{
		{
			file: "bash.txt", kind: "tool_permission", heading: "Bash command",
			needsApproval: "curl -s https://example.com -o /dev/null",
			mustHave:      []string{"Bash command", "Do you want to proceed?", "don’t ask again for: curl", "Esc to cancel"},
			// **罫線より上（入力欄・作業ディレクトリ）は載せない。**
			mustNotHave: []string{"/tmp/measure", "Run this exact bash command", "────"},
		},
		{
			file: "write.txt", kind: "tool_permission", heading: "Create file",
			mustHave:    []string{"Create file", "Do you want to create hello.txt?", "1. Yes"},
			mustNotHave: []string{"User declined to answer questions", "────"},
		},
		{
			file: "trust.txt", kind: "folder_trust",
			mustHave:    []string{"Accessing workspace:", "Yes, I trust this folder", "Enter to confirm"},
			mustNotHave: []string{"octocat@host", "────"},
		},
		{
			// **罫線が2本ある。最初のものから下を使う**（見出し `☐ Color` から `Esc to cancel` まで）。
			file: "ask.txt", kind: "ask_user_question",
			mustHave:    []string{"☐ Color", "Which color?", "Type something.", "4. Chat about this", "Enter to select"},
			mustNotHave: []string{"Interrupted", "Use the AskUserQuestion tool", "────"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got := orchestrator.ParseBlockedScreenForTest(blockedScreenFixture(t, tc.file))
			if !got.Extracted {
				t.Errorf("確認の画面を切り出せていない:\n%s", got.Excerpt)
			}
			if got.Kind != tc.kind {
				t.Errorf("種類 = %q, want %q", got.Kind, tc.kind)
			}
			if got.Heading != tc.heading {
				t.Errorf("見出し = %q, want %q", got.Heading, tc.heading)
			}
			if got.NeedsApproval != tc.needsApproval {
				t.Errorf("許可が要る部分 = %q, want %q", got.NeedsApproval, tc.needsApproval)
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(got.Excerpt, want) {
					t.Errorf("抜粋に %q が無い:\n%s", want, got.Excerpt)
				}
			}
			for _, ng := range tc.mustNotHave {
				if strings.Contains(got.Excerpt, ng) {
					t.Errorf("抜粋に %q が入っている:\n%s", ng, got.Excerpt)
				}
			}
			// **`&&` でつないだうち、許可の要らない `git status` は許可が要る部分に出ない**（実測）。
			if strings.Contains(got.NeedsApproval, "git status") {
				t.Errorf("許可が要る部分に git status が入っている: %q", got.NeedsApproval)
			}
		})
	}
}

// 目的: 載せる量の上限（20 行・2000 字）と中略の形を固定する。
//
// **上限が外れると、長いコマンドや大きな Edit の画面がコメントを埋める。**
//
// 与える情報: 罫線の下に 30 行ある画面と、罫線の下に 3000 字の1行がある画面。
// 成功条件: 30 行＋カーソルの行の画面は頭の5行・（中略）・終わりの15行になり、3000 字の画面は 2000 字と（以下略）になること。
func TestBlockedScreen_長い画面は中略して文字数でも切る(t *testing.T) {
	rule := strings.Repeat("─", 40)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	// **切り出しの起点はカーソル（`❯`）の行のすぐ上の罫線なので、最後の行にカーソルを置く。**
	lines = append(lines, "❯ 1. Yes")
	got := orchestrator.ParseBlockedScreenForTest("prompt above\n" + rule + "\n" + strings.Join(lines, "\n") + "\n\n")
	want := strings.Join(append(append(append([]string{}, lines[:5]...), "（中略）"), lines[len(lines)-15:]...), "\n")
	if got.Excerpt != want {
		t.Errorf("中略の形が違う:\n got:\n%s\nwant:\n%s", got.Excerpt, want)
	}

	long := strings.Repeat("a", 3000)
	got = orchestrator.ParseBlockedScreenForTest(rule + "\n" + long)
	if want := strings.Repeat("a", 2000) + "\n（以下略）"; got.Excerpt != want {
		t.Errorf("2000 字で切っていない（%d 字）", len([]rune(got.Excerpt)))
	}
}

// 目的: 罫線が無い画面では、下の 20 行を「切り出せなかった」として載せることを固定する。
//
// **表示が版で変わって罫線を見つけられなくても、画面の文は載る。**見分けが外れたことは前置きで分かる。
//
// 与える情報: 罫線の無い 30 行の画面。
// 成功条件: 切り出せなかった扱いになり、抜粋が下の 20 行であること。前置きが切り出せなかったときの文であること。
func TestBlockedScreen_罫線が無ければ下の行を載せる(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "row "+itoa(i))
	}
	text := strings.Join(lines, "\n") + "\n\n\n"
	got := orchestrator.ParseBlockedScreenForTest(text)
	if got.Extracted {
		t.Fatalf("罫線が無いのに切り出せたことになっている")
	}
	if want := strings.Join(lines[10:], "\n"); got.Excerpt != want {
		t.Errorf("下の 20 行ではない:\n%s", got.Excerpt)
	}
	section, _ := orchestrator.BlockedScreenSectionForTest(text, false, false)
	if !strings.Contains(section, "確認の画面を切り出せなかったので、画面の下の行を載せます") {
		t.Errorf("切り出せなかったことを書いていない:\n%s", section)
	}
}

// 目的: 資格情報の形の文字列を伏せ、制御文字と ANSI を落とすことを固定する。
//
// **引き渡しのコメントは、公開かどうかを見ずに投稿される。**画面の文は外から入った文字列として扱う。
// **伏せ字は網羅ではない**（知らない形は残る）。ここで見るのは、決めた形が伏せられることだけである。
//
// 与える情報: 罫線の下に、トークン・Bearer・Authorization・URL の資格情報・`password=` と、
// ANSI のエスケープ列・ベル・CR を混ぜた画面。
// 成功条件: 秘密の値が1つも残らず `***` があり、制御文字と ANSI が1つも残らないこと。
func TestBlockedScreen_資格情報の形を伏せて制御文字を落とす(t *testing.T) {
	secrets := []string{
		"ghp_abcdefABCDEF0123456789",
		"github_pat_11ABCDEFG_xyz",
		"sk-ant-api03-SECRETSECRET",
		"xoxb-1234-5678-abcdef",
		"AKIAABCDEFGHIJKLMNOP",
		"bearersecretvalue",
		"authsecretvalue",
		"hunter2",
		"tok123",
		"s3cr3tpass",
	}
	screen := strings.Repeat("─", 30) + "\n" +
		"\x1b[1mBash command\x1b[0m\r\n" +
		"  curl -H \"Authorization: token authsecretvalue\" https://user:s3cr3tpass@example.com/x\x07\n" +
		"  echo ghp_abcdefABCDEF0123456789 github_pat_11ABCDEFG_xyz sk-ant-api03-SECRETSECRET\n" +
		"  echo xoxb-1234-5678-abcdef AKIAABCDEFGHIJKLMNOP\n" +
		"  curl -H 'X: Bearer bearersecretvalue' 'https://x/?access_token=tok123&password=hunter2'\n" +
		"\x1b]0;title\x07Do you want to proceed?\n" +
		" ❯ 1. Yes\n"
	got := orchestrator.ParseBlockedScreenForTest(screen)
	for _, s := range secrets {
		if strings.Contains(got.Excerpt, s) {
			t.Errorf("秘密 %q が残っている:\n%s", s, got.Excerpt)
		}
	}
	if !strings.Contains(got.Excerpt, "***") {
		t.Errorf("伏せ字が1つも無い:\n%s", got.Excerpt)
	}
	for _, r := range got.Excerpt {
		if r == '\x1b' || r == '\x07' || r == '\r' {
			t.Errorf("制御文字 %q が残っている:\n%q", r, got.Excerpt)
		}
	}
	// **ANSI を落としても、見出しと種類は見分けられる。**
	if got.Kind != "tool_permission" || got.Heading != "Bash command" {
		t.Errorf("ANSI を落としたあとの見分けが外れた: 種類=%q 見出し=%q", got.Kind, got.Heading)
	}
}

// 目的: 中身に ``` があっても囲みが閉じないことを固定する。
//
// **閉じると、そこから下の画面の文が Markdown として表示される**（`@名前` が通知になる）。
//
// 与える情報: 罫線の下に ``` を含む画面。
// 成功条件: 画面の部分の囲みが4つの backtick になること。
func TestBlockedScreen_中身にbacktickがあれば囲みを長くする(t *testing.T) {
	screen := strings.Repeat("─", 30) + "\nCreate file\n```go\nfmt.Println(1)\n```\nDo you want to create x.md?\n ❯ 1. Yes\n"
	section, _ := orchestrator.BlockedScreenSectionForTest(screen, false, false)
	if !strings.Contains(section, "\n````\n") {
		t.Errorf("囲みが4つの backtick になっていない:\n%s", section)
	}
}

// 目的: 種類ごとの【再発するか】【止まった確認への直し方】と【補足】の出し分けを固定する。
//
// 与える情報: 実測した4つの画面と、道ごとの組み立て方（作業の途中・起動直後）。
// 成功条件: ツールの確認には直し方の1文があり、起動直後の道でだけ規則を足す先と再起動の1文が付くこと。
// フォルダの信頼の直し方は起動直後の道で出ないこと。【補足】はツールの確認でだけ出ること。
func TestBlockedScreen_種類ごとに直し方と補足を出し分ける(t *testing.T) {
	bash := blockedScreenFixture(t, "bash.txt")
	mid, note := orchestrator.BlockedScreenSectionForTest(bash, false, false)
	for _, want := range []string{
		"【止まった画面】Claude Code は次の確認で止まっていました",
		"伏せ字は網羅ではありません",
		"【種類】ツールを使ってよいかの確認です（画面の見出し: Bash command）。",
		"【再発するか】設定を変えない限り、同じ操作でまた止まりえます。",
		"【止まった確認への直し方】上の画面の操作が許してよいものかを確かめてください。",
	} {
		if !strings.Contains(mid, want) {
			t.Errorf("作業の途中の道の画面の部分に %q が無い:\n%s", want, mid)
		}
	}
	// **作業の途中の道は `permissionRemedyText` が続くので、規則の例や再起動を重ねない。**
	if strings.Contains(mid, "continuo を再起動してください") || strings.Contains(mid, "claude.permissions.allow") {
		t.Errorf("作業の途中の道に、続く【対処】と同じ案内を重ねている:\n%s", mid)
	}
	if !strings.Contains(note, "【補足】") || !strings.Contains(note, "The user doesn't want to proceed with this tool use") {
		t.Errorf("ツールの確認なのに【補足】が無い: %q", note)
	}

	startup, _ := orchestrator.BlockedScreenSectionForTest(bash, true, true)
	for _, want := range []string{"claude.permissions.allow", "足したら continuo を再起動してください"} {
		if !strings.Contains(startup, want) {
			t.Errorf("起動直後の道のツールの確認に %q が無い:\n%s", want, startup)
		}
	}

	trust := blockedScreenFixture(t, "trust.txt")
	trustMid, trustNote := orchestrator.BlockedScreenSectionForTest(trust, false, false)
	if !strings.Contains(trustMid, "`continuo trust`") || !strings.Contains(trustMid, "起動のたびに止まります") {
		t.Errorf("フォルダの信頼の直し方が無い:\n%s", trustMid)
	}
	if trustNote != "" {
		t.Errorf("フォルダの信頼なのに【補足】を出した: %q", trustNote)
	}
	trustStartup, _ := orchestrator.BlockedScreenSectionForTest(trust, true, true)
	if strings.Contains(trustStartup, "【止まった確認への直し方】") {
		t.Errorf("起動直後の道で、もとの文言の【対処】と同じ直し方を重ねている:\n%s", trustStartup)
	}

	ask := blockedScreenFixture(t, "ask.txt")
	askMid, askNote := orchestrator.BlockedScreenSectionForTest(ask, false, false)
	for _, want := range []string{"AskUserQuestion", "`claude.permissions.deny`"} {
		if !strings.Contains(askMid, want) {
			t.Errorf("AskUserQuestion の画面の部分に %q が無い:\n%s", want, askMid)
		}
	}
	// **AskUserQuestion を esc で閉じると画面に「User declined to answer questions」が残る**（実測）ので、【補足】を出す。
	if !strings.Contains(askNote, "【補足】") || !strings.Contains(askNote, "User declined to answer questions") {
		t.Errorf("AskUserQuestion なのに【補足】が無い: %q", askNote)
	}
}

// 目的: 行の境で割れた資格情報とホームのパスも伏せることを固定する。
//
// **画面は `detection`（画面の格子の平文）なので、pane の幅を超える行は割れて届く。**
// 1行ずつ伏せると、割れたトークンの後半や、途中で割れたホームのパスが公開の issue に残る。
//
// 与える情報: 確認の画面の中で、`ghp_` のトークン・`Bearer` の値・ホームのパスが行の境で割れた画面。
// 成功条件: 割れた後半の文字列がどこにも残らず、ホームのパスが `~` になること。
func TestBlockedScreen_行の境で割れた秘密とホームのパスも伏せる(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || len(home) < 2 {
		t.Skip("ホームディレクトリが分からない")
	}
	rule := strings.Repeat("─", 30)
	text := "prev\n" + rule + "\n Bash command\n" +
		"  curl -H \"Authorization: Bearer abcdefSECRETFIRST\n" +
		"HALFSECRETTAIL\" https://x.invalid -d token=ghp_ABCDEFGH\n" +
		"IJKLMNOPQRSTUV && cat " + home[:len(home)/2] + "\n" +
		home[len(home)/2:] + "/notes.txt\n" +
		" Do you want to proceed?\n ❯ 1. Yes\n   2. No\n"
	got := orchestrator.ParseBlockedScreenForTest(text)
	for _, ng := range []string{"SECRETFIRST", "HALFSECRETTAIL", "ABCDEFGH", "IJKLMNOPQRSTUV", home[len(home)/2:] + "/notes"} {
		if strings.Contains(got.Excerpt, ng) {
			t.Errorf("行の境で割れた %q が残っている:\n%s", ng, got.Excerpt)
		}
	}
	if !strings.Contains(got.Excerpt, "~") {
		t.Errorf("ホームのパスが ~ になっていない:\n%s", got.Excerpt)
	}
	if got.Kind != "tool_permission" {
		t.Errorf("種類: got %q, want tool_permission", got.Kind)
	}
}

// 目的: 会話の表示に罫線があっても、確認の画面はカーソルの行のすぐ上の罫線から切り出すことを固定する。
//
// 与える情報: 会話の表示に全幅の罫線と `Accessing workspace:` を含み、その下に Bash の確認の画面がある画面。
// 成功条件: 抜粋に会話の行が入らず、種類がツールの権限の確認であること。
func TestBlockedScreen_会話の表示の罫線から切り出さない(t *testing.T) {
	rule := strings.Repeat("─", 30)
	text := "⏺ 説明\n" + rule + "\nAccessing workspace: の話をしていた会話\n\n" + rule +
		"\n Bash command\n  ls\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend\n"
	got := orchestrator.ParseBlockedScreenForTest(text)
	if strings.Contains(got.Excerpt, "会話") {
		t.Errorf("会話の行が抜粋に入っている:\n%s", got.Excerpt)
	}
	if got.Kind != "tool_permission" || got.Heading != "Bash command" {
		t.Errorf("種類と見出し: got %q / %q", got.Kind, got.Heading)
	}
}

// 目的: AskUserQuestion の質問が「Do you want to …?」で選択肢が Yes でも、AskUserQuestion と見分けることを固定する。
//
// 与える情報: 質問が「Do you want to merge now?」、選択肢が Yes / No の AskUserQuestion の画面。
// 成功条件: 種類が ask_user_question であること。
func TestBlockedScreen_質問がDoYouWantToでもAskUserQuestionと見分ける(t *testing.T) {
	rule := strings.Repeat("─", 30)
	text := "❯ 質問して\n" + rule + "\n ☐ Merge\n\nDo you want to merge now?\n\n❯ 1. Yes\n  2. No\n  3. Type something.\n" +
		rule + "\n  4. Chat about this\n\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	got := orchestrator.ParseBlockedScreenForTest(text)
	if got.Kind != "ask_user_question" {
		t.Errorf("種類: got %q, want ask_user_question\n%s", got.Kind, got.Excerpt)
	}
}

// 目的: 起動系・コメントの道で、種類が分からないとき・読めないときも直し方を出すことを固定する。
//
// 与える情報: 罫線もカーソルも無い画面を、起動系の組み立て方で組み立てる。
// 成功条件: 汎用の直し方と再起動の1文が入ること。
func TestBlockedScreen_起動系の道は種類が分からなくても直し方を出す(t *testing.T) {
	mid, _ := orchestrator.BlockedScreenSectionForTest("何かの出力\nもう1行\n", true, true)
	for _, want := range []string{"【止まった確認への直し方】", "claude.permissions.allow", "足したら continuo を再起動してください"} {
		if !strings.Contains(mid, want) {
			t.Errorf("%q が無い:\n%s", want, mid)
		}
	}
}

// 目的: コメントを書かせる本文に、esc で閉じた印が立っているときだけ1行足すことを固定する。
//
// **足さないと、エージェントは会話に残った拒否の文を人間の拒否と読み、人間が答えられない質問をする。**
//
// 与える情報: 印が立っているときと立っていないとき。
// 成功条件: 立っているときだけ、人間の拒否ではないという1行が入ること。
func TestBlockedScreen_書かせ直しの本文には印が立っているときだけ1行足す(t *testing.T) {
	const line = "人間に「どの操作を止めたのか」と訊かないでください"
	on := orchestrator.BuildCommentRequestPromptForTest("https://github.com/octocat/hello-world/issues/1", "<!-- m -->", true)
	off := orchestrator.BuildCommentRequestPromptForTest("https://github.com/octocat/hello-world/issues/1", "<!-- m -->", false)
	if !strings.Contains(on, line) {
		t.Errorf("印が立っているのに1行が無い:\n%s", on)
	}
	if strings.Contains(off, line) {
		t.Errorf("印が立っていないのに1行が入っている:\n%s", off)
	}
}

// blockedRunScript は、作業の途中で確認の画面に止まる台本を fixture に入れる。
//
// **1回目の turn が `blocked` で返り、読み直し（esc の前の `agent.get`）でも `blocked` を返す。**
// そのあとの `agent.get`（コメントを書かせるための復元の起動の確認）は `idle` に戻す。
//
// fx: 台本を入れる fixture。
// transcript: hook が渡す会話の記録のパス。
// recheckStatus: 読み直しで返す状態（"blocked" か "idle"）。
// readText: `agent.read` が返す画面の文。空なら `agent.read` の台本を入れない（読めない）。
// 戻り値: `agent.prompt` に送られた本文を集めたものを返す関数。
func blockedRunScript(fx *fixture, transcript, recheckStatus, readText string) func() []string {
	var mu sync.Mutex
	var prompts []string
	var afterBlocked atomic.Bool
	var rechecked atomic.Bool
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		prompts = append(prompts, text)
		n := len(prompts)
		mu.Unlock()
		status := "idle"
		if n == 1 {
			fx.Orc.OnHook(stopEvent("session-1", transcript, "p1"))
			afterBlocked.Store(true)
			status = "blocked"
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": true},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status := "idle"
		if afterBlocked.Load() && !rechecked.Swap(true) {
			status = recheckStatus
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": true},
		}, nil
	})
	if readText != "" {
		fx.Herdr.Handle(herdr.MethodAgentRead, func(params map[string]any) (any, *rpcErr) {
			return map[string]any{
				"type": "pane_read",
				"read": map[string]any{
					"pane_id": "p1", "workspace_id": "w1", "tab_id": "t1",
					"source": params["source"], "format": "text", "text": readText,
					"revision": 1, "truncated": false,
				},
			}, nil
		})
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), prompts...)
	}
}

// 目的: 作業の途中で確認の画面に止まったとき、esc より前に画面を読み、引き渡しに画面の文・種類・直し方・
// 【補足】を載せ、同じ会話へ送る書かせ直しの本文にも1行足すことを固定する。
//
// **issue #347 の報告そのものである。**以前は「何の確認だったかは continuo の側には残りません」とだけ書き、
// 人間は何を許せばよいかを決められず、エージェントは esc を人間の拒否と取り違えた。
//
// 与える情報: 1回目の turn で `blocked` を返し、`agent.read` が実測した Bash の確認の画面を返す台本。
// 会話の記録（書かせ直しの復元が `--resume` できるように置く）。
// 成功条件: `agent.read` が `detection` と 40 行で、`agent.send_keys` より前に呼ばれること。
// 引き渡しに【止まった画面】・見出し・`don’t ask again for:` の行・【補足】・`permissionRemedyText` が入り、
// 「continuo の側には残りません」が入らないこと。書かせ直しの本文に1行が足されること。
func TestBlockedScreen_作業の途中で止まったら画面と直し方と補足を載せる(t *testing.T) {
	transcriptDir := t.TempDir()
	parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
	})
	fx := newFixture(t, fixtureOptions{TranscriptRoot: transcriptDir})
	fx.AllowLog(blockedScreenLog, "コメントを書かせる", "run を終えます", "Status を落とせません")
	fx.Tracker.AddIssue(sampleIssue(347, "Ready"))
	prompts := blockedRunScript(fx, parent, "blocked", blockedScreenFixture(t, "bash.txt"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffBodyOf(fx, "I_node347") != ""
	})
	fx.WaitRunsDrained(t, 30*time.Second)
	body := handoffBodyOf(fx, "I_node347")

	methods := fx.Herdr.Methods()
	readAt := slices.Index(methods, herdr.MethodAgentRead)
	escAt := slices.Index(methods, herdr.MethodAgentSendKeys)
	if readAt < 0 || escAt < 0 || readAt > escAt {
		t.Errorf("esc より前に画面を読んでいない（agent.read=%d, agent.send_keys=%d）: %v", readAt, escAt, methods)
	}
	params := fx.Herdr.ParamsOf(t, herdr.MethodAgentRead)
	if params["source"] != "detection" || params["lines"] != float64(40) {
		t.Errorf("agent.read の source と lines が違う: %v", params)
	}
	for _, want := range []string{
		"【止まった画面】Claude Code は次の確認で止まっていました",
		"don’t ask again for: curl -s https://example.com -o /dev/null",
		"【種類】ツールを使ってよいかの確認です（画面の見出し: Bash command）。",
		"【再発するか】",
		"【止まった確認への直し方】",
		"【補足】",
		orchestrator.PermissionRemedyTextForTest(config.ClaudePermissionModeAuto, true),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しに %q が無い:\n%s", want, body)
		}
	}
	if strings.Contains(body, "continuo の側には残りません") {
		t.Errorf("画面を載せたのに「残りません」と書いている:\n%s", body)
	}
	// **【補足】は最後に置く**（`permissionRemedyText` のあと）。
	if strings.Index(body, "【補足】") < strings.Index(body, "【対処】") {
		t.Errorf("【補足】が【対処】より前にある:\n%s", body)
	}

	got := prompts()
	if len(got) < 2 {
		t.Fatalf("書かせ直しの本文が送られていない（送った本文 %d 件）", len(got))
	}
	if !strings.Contains(got[len(got)-1], "人間に「どの操作を止めたのか」と訊かないでください") {
		t.Errorf("書かせ直しの本文に、esc は人間の拒否ではないという1行が無い:\n%s", got[len(got)-1])
	}
}

// 目的: 画面を読めなかったときと、読み直したら確認の画面でなくなっていたときの文を固定する。
//
// **読めなくても引き渡しは止めない。**読めなかったことを1行書き、汎用の【対処】に任せる。
//
// 与える情報: `agent.read` の台本が無い（読めない）場合と、読み直しの `agent.get` が `idle` を返す場合。
// 成功条件: それぞれの文が【止まった画面】に入り、画面の文（`Bash command`）が入らないこと。
func TestBlockedScreen_読めないときと確認の画面でなくなっていたとき(t *testing.T) {
	cases := []struct {
		name     string
		recheck  string
		readText string
		want     string
		// wantNote は【補足】を出すかである。**読めなかったときは見分けられないので出す（esc は送っている）。**
		// **確認の画面でなくなっていたときは出さない**（そのときの esc は確認の画面を閉じていない）。
		wantNote bool
	}{
		// **読めなければ読み直しの `agent.get` は呼ばれない。**次の `agent.get`（書かせ直しの起動の確認）は idle を返す。
		{name: "読めない", recheck: "idle", readText: "", want: "【止まった画面】画面を読めませんでした（", wantNote: true},
		{name: "確認の画面でなくなっていた", recheck: "idle", readText: "x\n" + strings.Repeat("─", 20) + "\nBash command\n",
			want: "【止まった画面】画面を読んだときには、確認の画面ではなくなっていました。", wantNote: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog(blockedScreenLog, "コメントを書かせる", "run を終えます", "Status を落とせません",
				"セッションを復元できません", "復帰する先のセッションに会話の記録が無い")
			fx.Tracker.AddIssue(sampleIssue(348, "Ready"))
			blockedRunScript(fx, filepath.Join(fx.TranscriptRoot, "none.jsonl"), tc.recheck, tc.readText)

			fx.Orc.Tick(context.Background())
			waitFor(t, 20*time.Second, "引き渡しの通知が投稿される", func() bool {
				return handoffBodyOf(fx, "I_node348") != ""
			})
			fx.WaitRunsDrained(t, 30*time.Second)
			body := handoffBodyOf(fx, "I_node348")
			if !strings.Contains(body, tc.want) {
				t.Errorf("引き渡しに %q が無い:\n%s", tc.want, body)
			}
			if strings.Contains(body, "Bash command") {
				t.Errorf("使わないはずの画面の文が入っている:\n%s", body)
			}
			if got := strings.Contains(body, "【補足】"); got != tc.wantNote {
				t.Errorf("【補足】の有無: got %v, want %v:\n%s", got, tc.wantNote, body)
			}
		})
	}
}

// 目的: 起動直後に確認の画面で止まったとき、esc より前に画面を読み、着手の引き渡しに画面の部分を足すことを固定する。
//
// **【補足】は出さない**（起動直後は保留中のツール操作が無いことが多い）。
// **もとの文言はそのまま残る**（`Error()` は変えない）。
//
// 与える情報: `agent.get` が `blocked` を返し、`agent.read` がフォルダの信頼の画面か Bash の確認の画面を返す台本。
// 成功条件: `agent.read` が `agent.send_keys` より前。引き渡しにもとの文言と【止まった画面】が入り、【補足】が入らないこと。
// Bash の確認なら、規則を足す先と再起動の1文が入ること。フォルダの信頼なら、直し方を重ねないこと。
func TestBlockedScreen_起動直後に止まったら画面を着手の引き渡しに足す(t *testing.T) {
	cases := []struct {
		file string
		want []string
		ng   []string
	}{
		{file: "trust.txt",
			want: []string{"Accessing workspace:", "【種類】Claude Code がこのフォルダを信頼してよいかを訊く確認です。"},
			ng:   []string{"【止まった確認への直し方】"}},
		{file: "bash.txt",
			want: []string{"Bash command", "【止まった確認への直し方】", "claude.permissions.allow", "足したら continuo を再起動してください"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			fx := newFixture(t, fixtureOptions{})
			fx.AllowLog(blockedScreenLog, "着手に失敗しました")
			fx.Tracker.AddIssue(sampleIssue(349, "Ready"))
			fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
				return map[string]any{
					"type":  "agent_info",
					"agent": map[string]any{"name": params["target"], "agent_status": "blocked"},
				}, nil
			})
			text := blockedScreenFixture(t, tc.file)
			fx.Herdr.Handle(herdr.MethodAgentRead, func(params map[string]any) (any, *rpcErr) {
				return map[string]any{"type": "pane_read", "read": map[string]any{
					"pane_id": "p1", "workspace_id": "w1", "tab_id": "t1",
					"source": params["source"], "format": "text", "text": text, "revision": 1, "truncated": false,
				}}, nil
			})

			fx.Orc.Tick(context.Background())
			waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
				return handoffBodyOf(fx, "I_node349") != ""
			})
			fx.WaitRunsDrained(t, 15*time.Second)
			body := handoffBodyOf(fx, "I_node349")

			methods := fx.Herdr.Methods()
			readAt := slices.Index(methods, herdr.MethodAgentRead)
			escAt := slices.Index(methods, herdr.MethodAgentSendKeys)
			if readAt < 0 || escAt < 0 || readAt > escAt {
				t.Errorf("esc より前に画面を読んでいない（agent.read=%d, agent.send_keys=%d）: %v", readAt, escAt, methods)
			}
			for _, want := range append([]string{
				"Claude Code が起動直後に確認の画面で止まりました",
				"【止まった画面】Claude Code は次の確認で止まっていました",
			}, tc.want...) {
				if !strings.Contains(body, want) {
					t.Errorf("引き渡しに %q が無い:\n%s", want, body)
				}
			}
			for _, ng := range append([]string{"【補足】", "continuo の側に残りません", "画面は残っていません"}, tc.ng...) {
				if strings.Contains(body, ng) {
					t.Errorf("引き渡しに %q が入っている:\n%s", ng, body)
				}
			}
		})
	}
}

// 目的: 再起動の引き継ぎで確認の画面に止まった pane を、閉じる前に読み、引き渡しに画面の部分を足すことを固定する。
//
// **この道は esc を送らない**（pane ごと閉じる）ので【補足】を出さない。
//
// 与える情報: `agent_status` が `blocked` の pane と、`agent.read` が Write の確認の画面を返す台本。
// 成功条件: 引き渡しに【止まった画面】と見出しが入り、【補足】と「画面は残っていません」が入らないこと。
func TestBlockedScreen_再起動の引き継ぎで止まっていたら画面を載せる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("権限の確認で止まっているので引き継ぎません", blockedScreenLog)
	issue := sampleIssue(350, "In Progress")
	fx.Tracker.AddIssue(issue)
	wt := prepareWorktree(t, fx, issue, identityOverride{})
	installPanes(fx, livePane{
		PaneID: "p-350", Cwd: wt.Path, AgentName: "continuo-hello-world-350",
		AgentStatus: herdr.AgentStatusBlocked, SessionUUID: "sess-350",
	})
	text := blockedScreenFixture(t, "write.txt")
	fx.Herdr.Handle(herdr.MethodAgentRead, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_read", "read": map[string]any{
			"pane_id": "p-350", "workspace_id": "w1", "tab_id": "t1",
			"source": params["source"], "format": "text", "text": text, "revision": 1, "truncated": false,
		}}, nil
	})

	restore(t, fx)

	waitFor(t, 10*time.Second, "引き渡しの通知が投稿される", func() bool {
		return handoffBodyOf(fx, "I_node350") != ""
	})
	body := handoffBodyOf(fx, "I_node350")
	for _, want := range []string{
		"【確かめ方】【止まった画面】に画面の文があればそれと、worktree の中身（下記）を見て",
		"【止まった画面】Claude Code は次の確認で止まっていました",
		"（画面の見出し: Create file）",
		"【よくある原因】フォルダの信頼が切れた・ツールの権限の確認で止まった。",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("引き渡しに %q が無い:\n%s", want, body)
		}
	}
	for _, ng := range []string{"【補足】", "画面は残っていません", "continuo の側に残りません"} {
		if strings.Contains(body, ng) {
			t.Errorf("引き渡しに %q が入っている:\n%s", ng, body)
		}
	}
}

// commentRecoveryScript は、コメントを書かずに `review` を表明して終わる run の台本を入れる。
//
// **コメントを書かせるための復元**（設計 3-25 の9段）へ進ませる。引き渡しをまだ投稿していないので、
// 段6・段9 の通知がそのまま投稿される。
//
// fx: 台本を入れる fixture。
// transcript: 会話の記録のパス。
// startupBlocked: 真なら、復元の起動の確認（2回目の agent.start のあとの agent.get）で `blocked` を返す。
// promptBlocked: 真なら、書かせ直しの `agent.prompt`（2回目）が `blocked` で返る。
// readText: `agent.read` が返す画面の文。
func commentRecoveryScript(fx *fixture, transcript string, startupBlocked, promptBlocked bool, readText string) {
	var starts atomic.Int32
	var prompts atomic.Int32
	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		starts.Add(1)
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"name": params["name"], "agent_status": "idle",
				"interactive_ready": true, "pane_id": params["pane_id"],
			},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status := "idle"
		if startupBlocked && starts.Load() >= 2 {
			status = "blocked"
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": true},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		n := prompts.Add(1)
		status := "idle"
		if n == 1 {
			// **エージェントはコメントを書かずに `review` を表明する。**
			fx.Orc.OnHook(stopEvent("session-1", transcript, "p1"))
		} else if promptBlocked {
			status = "blocked"
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": true},
		}, nil
	})
	fx.Herdr.Handle(herdr.MethodAgentRead, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_read", "read": map[string]any{
			"pane_id": "p1", "workspace_id": "w1", "tab_id": "t1",
			"source": params["source"], "format": "text", "text": readText, "revision": 1, "truncated": false,
		}}, nil
	})
}

// 目的: コメントを書かせるための復元が確認の画面で止まったとき（起動直後と、書かせる turn の途中）、
// 引き渡しの原因に画面の部分を載せることを固定する。
//
// **この2つの道は、先に引き渡しを投稿していなければ、通知がそのまま人間へ届く。**
// 以前は「入力を受け付けられる状態にならなかった」「コメントの投稿に失敗した」と、事実と違う原因を書いていた。
//
// 与える情報: コメントを書かずに `review` を表明する run。復元の起動の確認で `blocked` を返す台本と、
// 書かせ直しの `agent.prompt` が `blocked` で返る台本。`agent.read` は Bash の確認の画面を返す。
// 成功条件: それぞれの原因の文と【止まった画面】が通知に入り、【補足】が入らないこと。
func TestBlockedScreen_コメントを書かせる途中で止まったら原因に画面を載せる(t *testing.T) {
	cases := []struct {
		name           string
		startupBlocked bool
		promptBlocked  bool
		cause          string
	}{
		{name: "復元の起動直後", startupBlocked: true,
			cause: "コメントを書かせるために復元した Claude Code が、起動直後に確認の画面で止まった。"},
		{name: "書かせる途中", promptBlocked: true,
			cause: "コメントを書かせる途中で、Claude Code が確認の画面で止まった。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transcriptDir := t.TempDir()
			parent := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
				typedUserLine("p1", "実装してください"),
				assistantLine("req1", "終わりました。\nCONTINUO-STATUS: review", false),
			})
			fx := newFixture(t, fixtureOptions{TranscriptRoot: transcriptDir})
			fx.AllowLog(blockedScreenLog, "コメントを書かせる", "復元した agent が落ち着きません",
				"Status を落とせません", "run を終えます")
			fx.Tracker.AddIssue(sampleIssue(351, "Ready"))
			commentRecoveryScript(fx, parent, tc.startupBlocked, tc.promptBlocked, blockedScreenFixture(t, "bash.txt"))

			fx.Orc.Tick(context.Background())
			fx.WaitRunsDrained(t, 40*time.Second)
			handoffs := fx.Tracker.HandoffCommentsOf("I_node351")
			if len(handoffs) != 1 {
				t.Fatalf("引き渡しの通知が1件ではない: %d 件（%+v）", len(handoffs), fx.Tracker.CommentsOf("I_node351"))
			}
			body := handoffs[0].Body
			for _, want := range []string{
				"【よくある原因】" + tc.cause,
				"【止まった画面】Claude Code は次の確認で止まっていました",
				"（画面の見出し: Bash command）",
				"【止まった確認への直し方】",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("通知に %q が無い:\n%s", want, body)
				}
			}
			if strings.Contains(body, "【補足】") {
				t.Errorf("esc を送っていない道（または起動直後の道）なのに【補足】がある:\n%s", body)
			}
		})
	}
}
