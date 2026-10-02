// {"RUCM-CFG-SHA256": "028f9d2cde6778e9342fe29589f048aa2bdfa20039790816882f3c3f6d10748c", "SOURCE": "docs/spec/usecases/particular_case/見出し語を1つずつ検査する.cfg.json"}
//
// **ユースケース記述「見出し語を1つずつ検査する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package doctor_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/doctor"
)

// {"RUCM-PATH": "P001"}
//
// Test_見出し語を1つずつ検査する_P001_前提が揃っていれば全項目すべて通る は、揃っている状態の基準線を作る。
//
// 目的: 全項目を固定した見出し語で出し、すべて `✓` になり、終了コードが 0 になること。
// 与える情報: テスト用herdr mock（protocol は設定の既定値と同じ値を返す）・偽カンバン（Ready の issue が1件）・
// テスト用gh mock（project の scope あり）・信頼登録済みの `~/.claude.json`・`rate_limit.source: none`。
// 成功条件: 見出し語が設計どおりの順序で並び、全部 `✓` で、終了コードが 0 であること。
func Test_見出し語を1つずつ検査する_P001_前提が揃っていれば全項目すべて通る(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	if got := labelsOf(report); !equalKeys(got, wantLabels) {
		t.Fatalf("見出し語が設計 3-32 と違う: %v（期待: %v）", got, wantLabels)
	}
	for _, label := range wantLabels {
		assertSymbol(t, report, label, doctor.SymbolOK)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
	if fx.Herdr.Pings() != 1 {
		t.Fatalf("herdr の ping を呼んだ回数が 1 ではなく %d だった", fx.Herdr.Pings())
	}
	// **カンバンへ送るのは3本である**（設計 3-32）。Bootstrap・候補の取得・自動化で
	// 1リクエストずつ。**自動化を Bootstrap のクエリへ混ぜてはならない。**あちらは
	// GraphQL が `errors` を1件でも返した時点で落ちるので、`workflows` を読めない環境では
	// **常駐プロセスが起動しなくなる**（issue #209）。
	// **自動化はいちばん最後である。**要る2本より前に置くと、止まった自動化の1本が
	// 候補の取得の残り時間を食い、**見出し語 `カンバン` が巻き添えで `!` になる。**
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items", "workflows"}) {
		t.Fatalf("カンバンへ送ったクエリが想定と違う: %v", got)
	}
}

// {"RUCM-PATH": "P013"}
//
// Test_見出し語を1つずつ検査する_P013_設定ファイルを読めなければ設定に依存する検査は確かめられなかったになる は、
// 設定が壊れていても打ち切らないことを確かめる。
//
// 目的: 設定ファイルが `✗` のとき、**設定を読まないと決まらない検査がすべて `!` になる**こと
// （設計 3-32 の依存の図。`gh の認証` も設定ファイルの下流である）。
// 与える情報: WORKFLOW.md を消した状態。ほかは揃っている。
// 成功条件: 全項目が結果を持ち、記号が上のとおりで、終了コードが 1 であること。
func Test_見出し語を1つずつ検査する_P013_設定ファイルを読めなければ設定に依存する検査は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	// **既定値だけで成立する検査は、設定が読めなくても走る**（issue #11）。
	// **設定が読めないという理由で全部を `!` にすると、本当の原因を1つも指摘できない。**
	claude := assertSymbol(t, report, doctor.LabelClaude, doctor.SymbolOK)
	if !strings.Contains(strings.Join(claude.Notes, "\n"), "既定値で確かめました") {
		t.Fatalf("既定値で確かめたことが出ていない: %+v", claude)
	}
	runtimeDir := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	if !strings.Contains(strings.Join(runtimeDir.Notes, "\n"), "既定値で確かめました") {
		t.Fatalf("既定値で確かめたことが出ていない: %+v", runtimeDir)
	}
	assertSymbol(t, report, doctor.LabelClaudeHome, doctor.SymbolOK)
	// **worktree の置き場所は `workspace.root` にしか書いていない。**設定が読めなければ決まらない。
	assertSymbol(t, report, doctor.LabelWorkspaceRoot, doctor.SymbolUnknown)
	// **片付けの状態も、突き合わせる2つのキーが両方とも設定にしか無い。**
	assertSymbol(t, report, doctor.LabelCleanupStates, doctor.SymbolUnknown)
	// **未記入の項目も、突き合わせる相手（WORKFLOW.md の原文）が無い。**
	assertSymbol(t, report, doctor.LabelMissingKeys, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelHerdr, doctor.SymbolUnknown)
	// **gh の認証は設定ファイルの下流である**（設計 3-32 の依存の図）。
	gh := assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolUnknown)
	if !strings.Contains(gh.Detail, "設定ファイルを読めなかったため") {
		t.Fatalf("gh の認証の理由が上流の失敗を指していない: %q", gh.Detail)
	}
	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	credentials := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolUnknown)
	if !strings.Contains(credentials.Detail, "何を見るべきか決まりません") {
		t.Fatalf("資格情報の理由が「何を見るべきか決まらない」になっていない: %q", credentials.Detail)
	}
	if len(report.Results) != len(wantLabels) {
		t.Fatalf("1つ落ちたのに残りを検査していない（結果が %d件）", len(report.Results))
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_ghが未ログインなら足りないと出しログインの手順を出す は、
// 未ログインの検出と直し方の提示を確かめる。
//
// 目的: `Active account: true` のブロックが1つも無ければ `✗` にし、
// 「`gh auth login -s project` を実行してください」と出すこと。
// 与える情報: `gh auth status` が未ログインの出力を返し、終了コード 1 で終わる。
// 成功条件: `gh の認証` が `✗`、直し方に `gh auth login -s project` が入り、
// 下流（カンバン・clone・信頼登録）が `!` になり、終了コードが 1 になること。
func Test_見出し語を1つずつ検査する_P007_ghが未ログインなら足りないと出しログインの手順を出す(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, "You are not logged into any GitHub hosts. To log in, run: gh auth login", 1)

	report := fx.Run(t)

	gh := assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(gh.Remedies, "\n"), "gh auth login -s project") {
		t.Fatalf("直し方に `gh auth login -s project` が無い: %v", gh.Remedies)
	}
	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if len(fx.GitHub.Queries()) != 0 {
		t.Fatalf("gh の認証が落ちたのにカンバンを読んでいる: %v", fx.GitHub.Queries())
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_herdrへ繋がらなければ足りない は、socket が無い場合を確かめる。
//
// 目的: socket へ到達できないときに `✗` にし、herdr の起動を促すこと。
// 与える情報: 存在しない socket のパスを設定に書いた WORKFLOW.md。
// 成功条件: `herdr` が `✗` になり、直し方に socket のパスが入ること。
func Test_見出し語を1つずつ検査する_P007_herdrへ繋がらなければ足りない(t *testing.T) {
	fx := newFixture(t)
	missing := filepath.Join(fx.Root, "no.sock")
	fx.Herdr.SocketPath = missing
	fx.WriteWorkflow(t, "")

	report := fx.Run(t)

	herdr := assertSymbol(t, report, doctor.LabelHerdr, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(herdr.Remedies, "\n"), missing) {
		t.Fatalf("直し方に socket のパスが入っていない: %v", herdr.Remedies)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_トークンを取り出せなければ足りない は、認証の取り出しの失敗を検出する。
//
// 目的: カンバンを読むトークンを取り出せないときに `✗` にすること（設計 3-32）。
// 与える情報: `tracker.provider.token_source` が指す環境変数を空にした状態。
// 成功条件: `カンバン` が `✗` になり、カンバンへ1リクエストも送らず、終了コードが 1 になること。
func Test_見出し語を1つずつ検査する_P007_トークンを取り出せなければ足りない(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("CONTINUO_TEST_TOKEN", "")

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if !strings.Contains(board.Detail, "トークンを取り出せません") {
		t.Fatalf("説明がトークンの取り出しの失敗を指していない: %q", board.Detail)
	}
	if len(fx.GitHub.Queries()) != 0 {
		t.Fatalf("トークンが無いのにカンバンを読んでいる: %v", fx.GitHub.Queries())
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_ClaudeCodeの設定ディレクトリに書けなければ落とす は、
// **今回の `EROFS` を先に捕まえる検査**があることを確かめる（issue #11）。
//
// Claude Code は SessionStart hook を走らせる前に `~/.claude/session-env/<session_id>/` を作る。
// continuo はその hook を必ず張るので、**ここが書けないと issue は1件も始まらない。**
//
// 目的: `~/.claude` に書けないとき、`Claude の設定` が `✗` になり、
// なぜ困るのかと直し方が出ること。**設定が読めていても読めていなくても走ること。**
// 与える情報: ホームディレクトリの権限を読み取りと実行だけにした状態。
// 成功条件: `Claude の設定` が `✗`、内訳に SessionStart hook の説明が入り、終了コードが 1 であること。
func Test_見出し語を1つずつ検査する_P007_ClaudeCodeの設定ディレクトリに書けなければ落とす(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく書けるので、この検査は成立しない")
	}
	fx := newFixture(t)
	if err := os.Chmod(fx.Home, 0o500); err != nil {
		t.Fatalf("ホームディレクトリの権限を変えられません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(fx.Home, 0o700) })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelClaudeHome, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(res.Notes, "\n"), "SessionStart hook") {
		t.Fatalf("なぜここが書けないと困るのかが出ていない: %+v", res)
	}
	if len(res.Remedies) == 0 {
		t.Fatalf("書けないのに直し方が出ていない: %+v", res)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_cloneが無ければ足りないと直し方を出す は、clone の検査を確かめる。
//
// 目的: `ghq list -p -e` の出力が空なら `✗` にし、`ghq get <owner>/<repo>` を案内すること。
// **その場合、信頼登録は `!`**（鍵にする clone のパスが無い）。
// 与える情報: `ghq list` が空文字を返す状態。
// 成功条件: clone が `✗` で直し方に `ghq get octocat/hello-world` が入り、信頼登録が `!` であること。
func Test_見出し語を1つずつ検査する_P007_cloneが無ければ足りないと直し方を出す(t *testing.T) {
	fx := newFixture(t)
	fx.GhqPaths = map[string]string{}

	report := fx.Run(t)

	clone := assertSymbol(t, report, doctor.LabelClone, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(clone.Remedies, "\n"), "ghq get octocat/hello-world") {
		t.Fatalf("直し方に `ghq get` が無い: %v", clone.Remedies)
	}
	trust := assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if !strings.Contains(strings.Join(trust.Notes, "\n"), "clone が無いので") {
		t.Fatalf("信頼登録の理由が「clone が無い」になっていない: %v", trust.Notes)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P008"}
//
// Test_見出し語を1つずつ検査する_P008_1つ失敗しても残りを全部検査する は、打ち切らないことを確かめる。
//
// 目的: 複数の前提が同時に欠けても、全項目を検査して結果を並べること。
// 与える情報: herdr の protocol が食い違い、clone が無く、gh の scope も足りない状態。
// 成功条件: 全項目に結果があり、`herdr` / `gh の認証` が `✗`、
// カンバンと clone と信頼登録が `!`、終了コードが 1 であること。
func Test_見出し語を1つずつ検査する_P008_1つ失敗しても残りを全部検査する(t *testing.T) {
	fx := newFixture(t)
	fx.Herdr.SetProtocol(18)
	fx.GhqPaths = map[string]string{}
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'repo'
`, 0)

	report := fx.Run(t)

	if got := labelsOf(report); !equalKeys(got, wantLabels) {
		t.Fatalf("全項目を検査していない: %v", got)
	}
	assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolOK)
	assertSymbol(t, report, doctor.LabelHerdr, doctor.SymbolMissing)
	assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolOK)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_見出し語を1つずつ検査する_P003_対象リポジトリが0件ならcloneと信頼登録は確かめられなかったになる は、
// カンバンが空の場合の扱いを確かめる。
//
// 目的: 対象が0件のとき、clone と信頼登録を `!` にして**終了コードに影響させない**こと
// （カンバンが空なのは設定の誤りではない。設計 3-32）。
// 与える情報: item が1件も無い偽カンバン。
// 成功条件: clone と信頼登録が `!` で理由が「対象がありません」であり、終了コードが 0 であること。
func Test_見出し語を1つずつ検査する_P003_対象リポジトリが0件ならcloneと信頼登録は確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetItems()

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	clone := assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	trust := assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	for _, res := range []doctor.Result{clone, trust} {
		if !strings.Contains(res.Detail, "検査する対象がありません") {
			t.Fatalf("%s の理由が「対象が0件」になっていない: %q", res.Label, res.Detail)
		}
	}
	if report.ExitCode() != 0 {
		t.Fatalf("カンバンが空なだけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_見出し語を1つずつ検査する_P009_カンバンが時間内に応答しなければ確かめられなかったとして残りを続ける は、
// 検査に期限があることを確かめる。
//
// 目的: doctor は「使い始める前に前提を機械的に検査する」道具である。**1項目が返らない
// だけで道具そのものが固まると、人間の手が止まる。**
// 与える情報: 期限より長く待ってから応答する偽カンバンと、1項目 200ms の期限。
// 成功条件: カンバンが `!`（確かめられなかった）になり、説明が「時間内に応答がありません
// でした」であること。**全項目が結果を持ち、終了コードが 1 にならないこと。**
func Test_見出し語を1つずつ検査する_P009_カンバンが時間内に応答しなければ確かめられなかったとして残りを続ける(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetDelay(30 * time.Second)

	opts := fx.Options()
	// **固定 200ms にしてはならない。**`go test -coverpkg=./...` は全パッケージを
	// instrument するので実行が遅くなり、**期限切れにしたい「カンバン」より先に
	// 「gh の認証」が期限切れになる**（2026-08-21 に実際に起きた）。
	// ここで見たいのは「1項目が固まっても残りを続けること」であって、期限の短さではない。
	opts.CheckTimeout = 2 * time.Second

	start := time.Now()
	report := doctor.Run(context.Background(), opts)
	elapsed := time.Since(start)

	if elapsed > 20*time.Second {
		t.Fatalf("期限を過ぎても待ち続けた: %v", elapsed)
	}
	if got := labelsOf(report); len(got) != len(wantLabels) {
		t.Fatalf("1項目が固まっただけで残りの検査が落ちた: %v", got)
	}
	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	if !strings.Contains(board.Detail, "時間内に応答がありませんでした") {
		t.Fatalf("説明が期限切れを指していない: %q", board.Detail)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("確かめられなかっただけなのに終了コードが %d になった\n%s",
			report.ExitCode(), renderReport(t, report))
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_見出し語を1つずつ検査する_P001_何も無ければ作って消す は、検査の本体が実際に走ることを確かめる。
//
// 目的: 置き場所に何も無いとき、socket を作れることを確かめ、**作った socket を残さない**こと。
// 与える情報: 一時ディレクトリへ閉じた置き場所（CONTINUO_RUNTIME_DIR）。
// 成功条件: `✓` で、説明が一時ディレクトリの下の socket を指し、
// 検査のあとにその socket が残っておらず、終了コードが 0 になること。
func Test_見出し語を1つずつ検査する_P001_何も無ければ作って消す(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	assertSocketUnderRoot(t, fx, res.Detail)
	if !strings.Contains(res.Detail, "作れます") {
		t.Fatalf("socket を作れたことが出ていない: %q", res.Detail)
	}
	if _, err := os.Lstat(fx.SocketPath()); !os.IsNotExist(err) {
		t.Fatalf("検査が作った socket が残っている: %s（%v）", fx.SocketPath(), err)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_見出し語を1つずつ検査する_P001_既にcontinuoが待ち受けていれば通る は、動いている continuo を
// 「用意できない」と言わないことを確かめる。
//
// 目的: 置き場所で誰かが待ち受けているとき、`✓` にし、**「作れます」とは言わない**こと。
// 与える情報: 一時ディレクトリの置き場所に、テストが自分で listen した socket。
// 成功条件: `✓` で、説明が「既に continuo が待ち受けています」で、終了コードが 0 になること。
func Test_見出し語を1つずつ検査する_P001_既にcontinuoが待ち受けていれば通る(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.RunDir, 0o700); err != nil {
		t.Fatalf("置き場所を作れません: %v", err)
	}
	ln, err := net.Listen("unix", fx.SocketPath())
	if err != nil {
		t.Fatalf("テストが socket を listen できません: %v", err)
	}
	defer ln.Close()

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	assertSocketUnderRoot(t, fx, res.Detail)
	if !strings.Contains(res.Detail, "待ち受けています") {
		t.Fatalf("既に使われていることが出ていない: %q", res.Detail)
	}
	if strings.Contains(res.Detail, "作れます") {
		t.Fatalf("使われているのに「作れます」と出ている: %q", res.Detail)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_残骸があれば足りないと出す は、**「作れます」の嘘**を落とす。
//
// **置き場所に何かが在れば、AF_UNIX の bind は必ず EADDRINUSE を返す**
// （通常ファイル・ディレクトリ・listen していない socket のすべてで、darwin で実測した）。
// それを「既に continuo が動いている」と読むと、continuo が起動できない状態を ✓ と報告する。
//
// 目的: 待ち受けていない残骸があるとき、`✗` にし、確かめて消す手順を出すこと。
// 与える情報: 置き場所に置いた、listen していない通常ファイル。
// 成功条件: `✗` で、直し方に `ls -l` と `rm` が出ること。
func Test_見出し語を1つずつ検査する_P007_残骸があれば足りないと出す(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.RunDir, 0o700); err != nil {
		t.Fatalf("置き場所を作れません: %v", err)
	}
	if err := os.WriteFile(fx.SocketPath(), []byte("これは socket ではない\n"), 0o600); err != nil {
		t.Fatalf("残骸を置けません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	assertSocketUnderRoot(t, fx, res.Detail)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "ls -l") || !strings.Contains(remedies, "rm ") {
		t.Fatalf("残骸を確かめて消す手順が出ていない: %v", res.Remedies)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_置き場所がディレクトリなら足りないと出す は、
// 残骸が socket とは限らないことを確かめる。
//
// 目的: socket のパスがディレクトリのとき、`✗` にすること。
// 与える情報: 置き場所に作った、`hooks.sock` という名前のディレクトリ。
// 成功条件: `✗` になること。
func Test_見出し語を1つずつ検査する_P007_置き場所がディレクトリなら足りないと出す(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.SocketPath(), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	assertSocketUnderRoot(t, fx, res.Detail)
}

// {"RUCM-PATH": "P007"}
//
// Test_見出し語を1つずつ検査する_P007_置き場所を決められなければ足りないと出す は、
// 置き場所そのものを用意できない場合を確かめる。
//
// 目的: `CONTINUO_RUNTIME_DIR` が絶対パスでないとき、`✗` にして直し方を出すこと。
// 与える情報: 相対パスを入れた `CONTINUO_RUNTIME_DIR`。
// 成功条件: `✗` で、直し方に `CONTINUO_RUNTIME_DIR` が出ること。
func Test_見出し語を1つずつ検査する_P007_置き場所を決められなければ足りないと出す(t *testing.T) {
	fx := newFixture(t)
	t.Setenv(envRuntimeDir, filepath.Join("relative", "run"))

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(res.Remedies, "\n"), envRuntimeDir) {
		t.Fatalf("置き場所の直し方が出ていない: %v", res.Remedies)
	}
}
