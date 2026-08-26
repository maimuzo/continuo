// {"RUCM-CFG-SHA256": "62473a258ede20aa1bab988973d0ce2dece3fdccc05441eb7e28601b480d8958", "SOURCE": "docs/spec/usecases/particular_case/前提が揃っているかを検査する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// **RUCM は見出し語の並びを `DO`〜`UNTIL` の1周として書いてある**ので、
// パスは見出し語の数ではなく分岐の数に比例する。
// **1周の中では「どの見出し語か」を区別しない**ので、
// 「その見出し語の前提が揃っていない」という同じ形に複数のテストが対応づく。
// 対応づけるのは、分岐の形が違うものと、結末が違うものだけである。
package doctor_test

import (
	"context"
	"errors"
	"github.com/maimuzo/continuo/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/i18n"
)

// wantLabels は出力に出る見出し語のキーである（設計 3-32。**この語と順序で固定する**）。
//
// **画面に出る語そのものではなくキーで比べる**（設計 3-35）。語は言語で変わる。
var wantLabels = []i18n.Key{
	doctor.LabelConfig,
	doctor.LabelClaude,
	doctor.LabelRuntimeDir,
	doctor.LabelClaudeHome,
	doctor.LabelWorkspaceRoot,
	doctor.LabelHerdr,
	doctor.LabelGHAuth,
	doctor.LabelBoard,
	doctor.LabelStatusNames,
	doctor.LabelClone,
	doctor.LabelTrust,
	doctor.LabelCredentials,
}

// {"RUCM-PATH": "P001"}
//
// TestDoctor_前提が揃っていれば全項目すべて通る は、揃っている状態の基準線を作る。
//
// 目的: 全項目を固定した見出し語で出し、すべて `✓` になり、終了コードが 0 になること。
// 与える情報: テスト用herdr mock（protocol 19）・偽ボード（Ready の issue が1件）・
// テスト用gh mock（project の scope あり）・信頼登録済みの `~/.claude.json`・`rate_limit.source: none`。
// 成功条件: 見出し語が設計どおりの順序で並び、全部 `✓` で、終了コードが 0 であること。
func TestDoctor_前提が揃っていれば全項目すべて通る(t *testing.T) {
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
	// **ボードは1回だけ読む**（設計 3-32）。Bootstrap と候補の取得で1リクエストずつである。
	if got := fx.GitHub.Queries(); !equalStrings(got, []string{"bootstrap", "items"}) {
		t.Fatalf("ボードへ送ったクエリが想定と違う: %v", got)
	}
}

// {"RUCM-PATH": "P032"}
//
// TestDoctor_設定ファイルを読めなければ設定に依存する検査は確かめられなかったになる は、
// 設定が壊れていても打ち切らないことを確かめる。
//
// 目的: 設定ファイルが `✗` のとき、**設定を読まないと決まらない検査がすべて `!` になる**こと
// （設計 3-32 の依存の図。`gh の認証` も設定ファイルの下流である）。
// 与える情報: WORKFLOW.md を消した状態。ほかは揃っている。
// 成功条件: 全項目が結果を持ち、記号が上のとおりで、終了コードが 1 であること。
func TestDoctor_設定ファイルを読めなければ設定に依存する検査は確かめられなかったになる(t *testing.T) {
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
// TestDoctor_ghが未ログインなら足りないと出しログインの手順を出す は、
// 未ログインの検出と直し方の提示を確かめる。
//
// 目的: `Active account: true` のブロックが1つも無ければ `✗` にし、
// 「`gh auth login -s project` を実行してください」と出すこと。
// 与える情報: `gh auth status` が未ログインの出力を返し、終了コード 1 で終わる。
// 成功条件: `gh の認証` が `✗`、直し方に `gh auth login -s project` が入り、
// 下流（ボード・clone・信頼登録）が `!` になること。
func TestDoctor_ghが未ログインなら足りないと出しログインの手順を出す(t *testing.T) {
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
		t.Fatalf("gh の認証が落ちたのにボードを読んでいる: %v", fx.GitHub.Queries())
	}
}

// TestDoctor_read_projectだけでは合格しない は、scope の読み方を確かめる。
//
// 目的: `project` が単独の scope として並んでいることを合格の条件にすること。
// 与える情報: `Token scopes:` に `read:project` はあるが `project` が無い出力。
// 成功条件: `gh の認証` が `✗` になること。
func TestDoctor_read_projectだけでは合格しない(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'read:project', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
}

// TestDoctor_有効なアカウントのブロックだけを読む は、複数アカウントの取り違えを防ぐ。
//
// 目的: `Active account: true` の行を持つブロックの `Token scopes:` だけを読むこと。
// 与える情報: 2つのアカウントがあり、**有効でないほうにだけ** `project` がある出力。
// 成功条件: `gh の認証` が `✗` になること（有効なほうの scope を読んでいる）。
func TestDoctor_有効なアカウントのブロックだけを読む(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account other (keyring)
  - Active account: false
  - Token scopes: 'gist', 'project', 'repo'

  ✓ Logged in to github.com account tester (keyring)
  - Active account: true
  - Token scopes: 'gist', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
}

// TestDoctor_ghを起動できなければ足りない は、gh そのものが動かない場合を確かめる。
//
// 目的: `gh auth status` を実行できないときに `✗` にすること。
// 与える情報: `gh auth status` を実行する関数が必ずエラーを返す差し替え。
// 成功条件: `gh の認証` が `✗` になり、説明にその理由が入ること。
func TestDoctor_ghを起動できなければ足りない(t *testing.T) {
	fx := newFixture(t)
	opts := fx.Options()
	opts.GHAuthStatus = func(context.Context) (string, error) {
		return "", errors.New("gh を起動できませんでした（テストの差し替え）")
	}

	report := doctor.Run(context.Background(), opts)

	gh := assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
	if !strings.Contains(gh.Detail, "テストの差し替え") {
		t.Fatalf("説明に gh の失敗の理由が入っていない: %q", gh.Detail)
	}
}

// TestDoctor_herdrのprotocolが設定と一致しなければ足りない は、herdr の検査を確かめる。
//
// 目的: socket の ping の応答の protocol が `herdr.protocol` と一致しなければ `✗` にすること。
// 与える情報: テスト用herdr mock が、設定より1つ古い protocol を返す。
// 成功条件: `herdr` が `✗` になり、説明に両方の版が入り、終了コードが 1 になること。
func TestDoctor_herdrのprotocolが設定と一致しなければ足りない(t *testing.T) {
	// **期待値を直書きしない。**既定値から引く（監査の指摘。5箇所に散っていた）。
	want := config.DefaultConfig().Herdr.Protocol
	older := want - 1

	fx := newFixture(t)
	fx.Herdr.SetProtocol(older)

	report := fx.Run(t)

	herdr := assertSymbol(t, report, doctor.LabelHerdr, doctor.SymbolMissing)
	if !strings.Contains(herdr.Detail, strconv.Itoa(older)) ||
		!strings.Contains(herdr.Detail, strconv.Itoa(want)) {
		t.Fatalf("説明に herdr 側の版と設定の期待値の両方が入っていない: %q", herdr.Detail)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
	// **1つ失敗しても残りを全部検査する。**
	assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolOK)
}

// {"RUCM-PATH": "P009"}
//
// TestDoctor_herdrへ繋がらなければ足りない は、socket が無い場合を確かめる。
//
// 目的: socket へ到達できないときに `✗` にし、herdr の起動を促すこと。
// 与える情報: 存在しない socket のパスを設定に書いた WORKFLOW.md。
// 成功条件: `herdr` が `✗` になり、直し方に socket のパスが入ること。
func TestDoctor_herdrへ繋がらなければ足りない(t *testing.T) {
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

// TestDoctor_Statusの選択肢名が設定と一致しなければ足りない は、無言の0件を防ぐ検査である。
//
// 目的: `active_states` の選択肢名がボードに無ければ `✗` にすること
// （巡回が無言で0件を返す原因になる）。
// 与える情報: ボード側の Status の選択肢から `In Progress` を落とした偽ボード。
// 成功条件: `ボード` が `✗` になり、説明に落とした名前が入り、clone と信頼登録が `!` になること。
func TestDoctor_Statusの選択肢名が設定と一致しなければ足りない(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetStatusOptions("Ice Box", "Ready", "Blocked", "In Review", "Done")

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if !strings.Contains(board.Detail, "In Progress") {
		t.Fatalf("説明に一致しなかった Status 名が入っていない: %q", board.Detail)
	}
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// TestDoctor_projectが見つからなければ足りない は、設定の誤りを検出する。
//
// 目的: project が見つからないときに `✗` にすること（設計 3-32 の「落ち方で分ける」）。
// 与える情報: `repositoryOwner` に null を返す偽ボード。
// 成功条件: `ボード` が `✗` になり、終了コードが 1 になること。
func TestDoctor_projectが見つからなければ足りない(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetFailure(failureNoProject)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if !strings.Contains(board.Detail, "project が見つかりません") {
		t.Fatalf("説明が project の不在を指していない: %q", board.Detail)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P008"}
//
// TestDoctor_トークンを取り出せなければ足りない は、認証の取り出しの失敗を検出する。
//
// 目的: ボードを読むトークンを取り出せないときに `✗` にすること（設計 3-32）。
// 与える情報: `tracker.provider.token_source` が指す環境変数を空にした状態。
// 成功条件: `ボード` が `✗` になり、ボードへ1リクエストも送らないこと。
func TestDoctor_トークンを取り出せなければ足りない(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("CONTINUO_TEST_TOKEN", "")

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	if !strings.Contains(board.Detail, "トークンを取り出せません") {
		t.Fatalf("説明がトークンの取り出しの失敗を指していない: %q", board.Detail)
	}
	if len(fx.GitHub.Queries()) != 0 {
		t.Fatalf("トークンが無いのにボードを読んでいる: %v", fx.GitHub.Queries())
	}
}

// TestDoctor_レートリミットは確かめられなかったにする は、一時的な失敗を区別する。
//
// 目的: レートリミットに当たったときだけ `!` にし、終了コードを 0 のままにすること。
// 与える情報: 429 と `Retry-After` を返す偽ボード。
// 成功条件: `ボード` が `!`、clone と信頼登録が `!`、終了コードが 0 であること。
func TestDoctor_レートリミットは確かめられなかったにする(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetFailure(failureRateLimit)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolUnknown)
	if !strings.Contains(board.Detail, "レートリミット") {
		t.Fatalf("説明がレートリミットを指していない: %q", board.Detail)
	}
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if report.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// {"RUCM-PATH": "P016"}
//
// TestDoctor_対象リポジトリが0件ならcloneと信頼登録は確かめられなかったになる は、
// ボードが空の場合の扱いを確かめる。
//
// 目的: 対象が0件のとき、clone と信頼登録を `!` にして**終了コードに影響させない**こと
// （ボードが空なのは設定の誤りではない。設計 3-32）。
// 与える情報: item が1件も無い偽ボード。
// 成功条件: clone と信頼登録が `!` で理由が「対象がありません」であり、終了コードが 0 であること。
func TestDoctor_対象リポジトリが0件ならcloneと信頼登録は確かめられなかったになる(t *testing.T) {
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
		t.Fatalf("ボードが空なだけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// TestDoctor_draft_issueは対象から外す は、リポジトリを持たない item の扱いを確かめる。
//
// 目的: draft issue（`Owner` / `Repo` が空）を対象リポジトリに含めないこと。
// 与える情報: draft issue だけが載っている偽ボード。
// 成功条件: 対象0件として clone と信頼登録が `!` になり、終了コードが 0 であること。
func TestDoctor_draft_issueは対象から外す(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetItems(boardItem{ItemID: "PVTI_draft", State: "Ready"})

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	if !strings.Contains(board.Detail, "対象リポジトリ 0件") {
		t.Fatalf("draft issue が対象リポジトリに数えられている: %q", board.Detail)
	}
	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolUnknown)
	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if report.ExitCode() != 0 {
		t.Fatalf("終了コードが %d だった", report.ExitCode())
	}
}

// TestDoctor_ボードに載る全リポジトリを重複なく検査する は、対象の集め方を確かめる。
//
// 目的: 返ってきた issue の `nameWithOwner` を重複なく集め、**集まった全件**を検査すること。
// 与える情報: 2つのリポジトリの issue が3件（うち2件は同じリポジトリ）載った偽ボード。
// 成功条件: clone の内訳が2件で、両方のリポジトリ名が出ること。
func TestDoctor_ボードに載る全リポジトリを重複なく検査する(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetItems(
		boardItem{ItemID: "PVTI_1", NameWithOwner: "octocat/hello-world", Number: 188, State: "Ready"},
		boardItem{ItemID: "PVTI_2", NameWithOwner: "octocat/hello-world", Number: 189, State: "Ready"},
		boardItem{ItemID: "PVTI_3", NameWithOwner: "maimuzo/continuo", Number: 3, State: "Ready"},
	)
	// **もう1つのリポジトリには clone を用意しない**（検査の対象になったことを記号で示す）。
	fx.GhqPaths = map[string]string{"octocat/hello-world": fx.RepoDir}

	report := fx.Run(t)

	clone := assertSymbol(t, report, doctor.LabelClone, doctor.SymbolMissing)
	if len(clone.Notes) != 2 {
		t.Fatalf("対象リポジトリの内訳が2件ではなく %d件だった: %v", len(clone.Notes), clone.Notes)
	}
	joined := strings.Join(clone.Notes, "\n")
	if !strings.Contains(joined, "octocat/hello-world") || !strings.Contains(joined, "maimuzo/continuo") {
		t.Fatalf("内訳に両方のリポジトリが出ていない: %v", clone.Notes)
	}
}

// {"RUCM-PATH": "P017"}
//
// TestDoctor_cloneが無ければ足りないと直し方を出す は、clone の検査を確かめる。
//
// 目的: `ghq list -p -e` の出力が空なら `✗` にし、`ghq get <owner>/<repo>` を案内すること。
// **その場合、信頼登録は `!`**（鍵にする clone のパスが無い）。
// 与える情報: `ghq list` が空文字を返す状態。
// 成功条件: clone が `✗` で直し方に `ghq get octocat/hello-world` が入り、信頼登録が `!` であること。
func TestDoctor_cloneが無ければ足りないと直し方を出す(t *testing.T) {
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

// TestDoctor_cloneの検査はghq_list_p_eで行う は、呼び方を固定する。
//
// 目的: **`ghq list -p -e <owner>/<repo>` を使う**こと（設計 3-6 の3段と同じ呼び方）。
// **exit code は存在の有無にかかわらず 0 を返す**ので、出力の有無で判定する。
// 与える情報: 受け取った引数を記録する偽の `ghq` を PATH の先頭へ置いた状態
// （`ghq list` の差し替えを注入せず、本物の呼び出し経路を通す）。
// 成功条件: 記録された引数が `list -p -e octocat/hello-world` であること。
func TestDoctor_cloneの検査はghq_list_p_eで行う(t *testing.T) {
	fx := newFixture(t)
	// **注入をやめて、PATH のテスト用ghq mock を実際に起動させる。**
	fx.GhqPaths = nil

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelClone, doctor.SymbolOK)
	recorded, err := os.ReadFile(fx.GhqArgsFile)
	if err != nil {
		t.Fatalf("テスト用ghq mock が受け取った引数を読めません: %v", err)
	}
	if got := strings.TrimSpace(string(recorded)); got != "list -p -e octocat/hello-world" {
		t.Fatalf("ghq の呼び方が違う: %q", got)
	}
}

// TestDoctor_信頼登録されていなければ足りない は、信頼の検査を確かめる。
//
// 目的: clone のパスの `hasTrustDialogAccepted` が false なら `✗` にすること。
// 与える情報: `~/.claude.json` に承認していない登録を書いた状態。
// 成功条件: 信頼登録が `✗` になり、内訳に clone のパスが出て、直し方が出ること。
func TestDoctor_信頼登録されていなければ足りない(t *testing.T) {
	fx := newFixture(t)
	writeTrustFile(t, fx.Home, fx.RepoDir, false)

	report := fx.Run(t)

	trust := assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(trust.Notes, "\n"), fx.RepoDir) {
		t.Fatalf("内訳に clone のパスが出ていない: %v", trust.Notes)
	}
	if len(trust.Remedies) == 0 {
		t.Fatalf("直し方が出ていない")
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// TestDoctor_信頼の鍵はcloneのパスでありworktreeのパスではない は、鍵の取り違えを防ぐ。
//
// 目的: **信頼を引く鍵は `ghq list -p -e` が返した clone の絶対パスである**こと（設計 3-32）。
// 与える情報: `~/.claude.json` には worktree のパスだけが承認済みとして登録されている状態。
// 成功条件: 信頼登録が `✗` になること（worktree のパスでは信頼済みにならない）。
func TestDoctor_信頼の鍵はcloneのパスでありworktreeのパスではない(t *testing.T) {
	fx := newFixture(t)
	worktreePath := filepath.Join(fx.Root, "wt", "hello-world-188")
	if err := os.MkdirAll(worktreePath, 0o700); err != nil {
		t.Fatalf("worktree の置き場所を作れません: %v", err)
	}
	doc := `{"projects":{"` + worktreePath + `":{"hasTrustDialogAccepted":true}}}`
	if err := os.WriteFile(filepath.Join(fx.Home, ".claude.json"), []byte(doc), 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けません: %v", err)
	}

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolMissing)
}

// TestDoctor_資格情報_sourceがnoneならtoken_sourceを見ない は、枠の判定を使わない設定を確かめる。
//
// 目的: `rate_limit.source` が `none` なら、`token_source` を見ずに `✓` にすること。
// 与える情報: `source: none` かつ `token_source: env` で、その環境変数は未設定。
// 成功条件: 資格情報が `✓` になり、説明が「枠の判定を行わない設定」であること。
func TestDoctor_資格情報_sourceがnoneならtoken_sourceを見ない(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: none\n  token_source: env\n  token_env: CONTINUO_NO_SUCH_TOKEN\n")

	report := fx.Run(t)

	credentials := assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolOK)
	if !strings.Contains(credentials.Detail, "枠の判定を行わない設定") {
		t.Fatalf("説明が想定と違う: %q", credentials.Detail)
	}
}

// TestDoctor_資格情報_envは環境変数の有無で分ける は、`token_source: env` の扱いを確かめる。
//
// 目的: `token_source` が `env` のとき、環境変数があれば `✓`、無ければ `✗` にすること。
// 与える情報: `source: oauth_usage_api` / `token_source: env` / `token_env: CONTINUO_DOCTOR_TOKEN`。
// 成功条件: 環境変数が無いと `✗`（説明に環境変数名が入る）、あると `✓` になること。
func TestDoctor_資格情報_envは環境変数の有無で分ける(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: oauth_usage_api\n  token_source: env\n  token_env: CONTINUO_DOCTOR_TOKEN\n")

	missing := fx.Run(t)
	res := assertSymbol(t, missing, doctor.LabelCredentials, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "CONTINUO_DOCTOR_TOKEN") {
		t.Fatalf("説明に環境変数名が入っていない: %q", res.Detail)
	}
	if missing.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", missing.ExitCode())
	}

	fx.Env["CONTINUO_DOCTOR_TOKEN"] = "dummy"
	present := fx.Run(t)
	assertSymbol(t, present, doctor.LabelCredentials, doctor.SymbolOK)
	if present.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった\n%s", present.ExitCode(), renderReport(t, present))
	}
}

// {"RUCM-PATH": "P074"}
//
// TestDoctor_token_envの書き漏らしは設定ファイルの検査で足りないと出る は、
// 設定の書き漏らしがどこで捕まるかを固定する。
//
// 目的: `rate_limit.token_source` が `env` なのに `token_env` が空なら、continuo は
// 起動できない。**その書き漏らしを doctor が見逃さないこと**を確かめる
// （判定は `config.Load` が持ち、doctor はその結果を記号にする。設計 3-32）。
// 与える情報: `token_env: ""` を明示した設定。
// 成功条件: 設定ファイルが `✗` になり、説明が `rate_limit.token_env` を指すこと。
// 下流の資格情報は `!`（設定を読めていないので確かめられない）で、終了コードは 1 であること。
func TestDoctor_token_envの書き漏らしは設定ファイルの検査で足りないと出る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: oauth_usage_api\n  token_source: env\n  token_env: \"\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "token_env") {
		t.Fatalf("説明が token_env の未設定を指していない: %q", res.Detail)
	}
	assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolUnknown)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// TestDoctor_資格情報_claude_credentialsはファイルの有無で分ける は、Keychain を触らないことを含めて確かめる。
//
// 目的: `token_source` が `claude_credentials` のとき、`~/.claude/.credentials.json` が
// あれば `✓`、無ければ `!` にすること（**Keychain は読まない。**読むと確認の画面が出て固まる）。
// 与える情報: `source: oauth_usage_api` / `token_source: claude_credentials` と、
// 一時ディレクトリのホーム（最初はファイルなし、次にファイルあり）。
// 成功条件: ファイルが無いと `!` で終了コードが 0、直し方に「起動には影響しません」が入り、
// ファイルがあると `✓` になること。
func TestDoctor_資格情報_claude_credentialsはファイルの有無で分ける(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: oauth_usage_api\n  token_source: claude_credentials\n")

	absent := fx.Run(t)
	res := assertSymbol(t, absent, doctor.LabelCredentials, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "Keychain") {
		t.Fatalf("説明に Keychain の案内が入っていない: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "起動には影響しません") {
		t.Fatalf("直し方に「起動には影響しません」が入っていない: %v", res.Remedies)
	}
	if absent.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった\n%s", absent.ExitCode(), renderReport(t, absent))
	}

	claudeDir := filepath.Join(fx.Home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("資格情報の置き場所を作れません: %v", err)
	}
	credentials := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(credentials, []byte(`{"claudeAiOauth":{"accessToken":"dummy"}}`), 0o600); err != nil {
		t.Fatalf("資格情報のファイルを書けません: %v", err)
	}

	present := fx.Run(t)
	got := assertSymbol(t, present, doctor.LabelCredentials, doctor.SymbolOK)
	if !strings.Contains(got.Detail, credentials) {
		t.Fatalf("説明にファイルのパスが入っていない: %q", got.Detail)
	}
}

// {"RUCM-PATH": "P009"}
//
// TestDoctor_1つ失敗しても残りを全部検査する は、打ち切らないことを確かめる。
//
// 目的: 複数の前提が同時に欠けても、全項目を検査して結果を並べること。
// 与える情報: herdr の protocol が食い違い、clone が無く、gh の scope も足りない状態。
// 成功条件: 全項目に結果があり、`herdr` / `gh の認証` が `✗`、
// ボードと clone と信頼登録が `!`、終了コードが 1 であること。
func TestDoctor_1つ失敗しても残りを全部検査する(t *testing.T) {
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

// TestDoctor_有効なアカウントが1つも無ければブロックが1つでも足りない は、
// 単独のブロックに `Active account: false` と書かれている場合を確かめる。
//
// 目的: **読むのは `Active account: true` の行を持つブロックだけ**であり、
// **該当ブロックが1つも無ければ `✗`（未ログイン）**にすること（設計 3-32）。
// gh の有効なアカウントが別のホストにあると、`gh auth status --hostname github.com` は
// `Active account: false` のブロックを1つだけ出しうる。
// 与える情報: `Active account: false` のブロックが1つだけの出力（scope に `project` はある）。
// 成功条件: `gh の認証` が `✗` になり、直し方にログインの案内が出ること。
func TestDoctor_有効なアカウントが1つも無ければブロックが1つでも足りない(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  X Logged in to github.com account tester (keyring)
  - Active account: false
  - Token scopes: 'gist', 'project', 'repo'
`, 0)

	report := fx.Run(t)

	gh := assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(gh.Remedies, "\n"), "gh auth login -s project") {
		t.Fatalf("直し方に `gh auth login -s project` が無い: %v", gh.Remedies)
	}
}

// TestDoctor_Active_accountの行が無い版のghではブロックが1つなら読む は、
// 上の判定が古い版の gh を巻き込まないことを確かめる。
//
// 目的: **`Active account:` の行を1つも出さない版の gh** では、ブロックが1つだけなら
// その `Token scopes:` を読むこと（「行が無い」と「false と書いてある」を区別する）。
// 与える情報: `Active account:` の行が無く、`project` の scope を持つブロックが1つだけの出力。
// 成功条件: `gh の認証` が `✓` になること。
func TestDoctor_Active_accountの行が無い版のghではブロックが1つなら読む(t *testing.T) {
	fx := newFixture(t)
	writeFakeGH(t, fx.BinDir, `github.com
  ✓ Logged in to github.com account tester (keyring)
  - Token scopes: 'gist', 'project', 'repo'
`, 0)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelGHAuth, doctor.SymbolOK)
}

// TestDoctor_確かめられなかった検査の説明に件数の見出しを出さない は、
// 記号と説明の食い違いを防ぐ。
//
// 目的: 記号が `!` のときに「0件が未承認です」のような件数の見出しを出さないこと。
// 見出しの行だけを読むと「未承認は0件＝問題なし」に見えてしまう。
// 与える情報: clone が1件も見つからない状態（信頼登録は鍵が無いので `!` になる）。
// 成功条件: 信頼登録の説明が「確かめられませんでした」であり、「0件が未承認」と出ないこと。
func TestDoctor_確かめられなかった検査の説明に件数の見出しを出さない(t *testing.T) {
	fx := newFixture(t)
	fx.GhqPaths = map[string]string{}

	report := fx.Run(t)

	trust := assertSymbol(t, report, doctor.LabelTrust, doctor.SymbolUnknown)
	if !strings.Contains(trust.Detail, "確かめられませんでした") {
		t.Fatalf("説明が「確かめられなかった」になっていない: %q", trust.Detail)
	}
	if strings.Contains(trust.Detail, "0件が未承認") {
		t.Fatalf("記号が ! なのに「0件が未承認」と出ている: %q", trust.Detail)
	}
}

// TestDoctor_対象が0件のときの集計は問題ありと読ませない は、集計の行の文言を確かめる。
//
// 目的: **ボードが空なのは設定の誤りではない**（設計 3-32）ので、`!` だけのときの集計を
// 「問題があります」と書かないこと。
// 与える情報: item が1件も無い偽ボード（clone と信頼登録が `!` になる）。
// 成功条件: 集計の行が「足りないものはありません」を含み、「件に問題があります」を含まないこと。
func TestDoctor_対象が0件のときの集計は問題ありと読ませない(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetItems()

	report := fx.Run(t)

	out := renderReport(t, report)
	if !strings.Contains(out, "足りないものはありません") {
		t.Fatalf("集計の行が「足りないものはありません」になっていない:\n%s", out)
	}
	if strings.Contains(out, "件に問題があります") {
		t.Fatalf("✗ が0件なのに「問題があります」と出ている:\n%s", out)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった:\n%s", report.ExitCode(), out)
	}
}

// equalKeys は見出し語のキーの並びが等しいかを返す。
//
// a: 比べる並び。
// b: 比べる並び。
// 戻り値: 長さも中身も等しければ true。
func equalKeys(a, b []i18n.Key) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalStrings は文字列の並びが等しいかを返す。
//
// a: 比べる並び。
// b: 比べる並び。
// 戻り値: 長さも中身も等しければ true。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDoctor_トークンが失効していれば認証の直し方を出す は、直す先の取り違えを防ぐ。
//
// 目的: 401（トークンが無効・失効）なのに「owner / project_number / status_field を
// 確認してください」と案内すると、人間が直す先を取り違える（設計 3-32 は落ち方で
// 分けることを求めている）。
// 与える情報: 401 を返す偽ボード。ほかの前提は揃っている。
// 成功条件: ボードが `✗` になり、直し方が `gh auth refresh` か token_env を指すこと。
// **owner / project_number を確認せよという案内を出さないこと。**
func TestDoctor_トークンが失効していれば認証の直し方を出す(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetFailure(failureBadCredentials)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolMissing)
	joined := strings.Join(board.Remedies, " / ")
	if !strings.Contains(joined, "gh auth refresh") || !strings.Contains(joined, "token_env") {
		t.Fatalf("直し方が認証の入れ直しを指していない: %q", joined)
	}
	if strings.Contains(joined, "project_number") {
		t.Fatalf("認証の失効なのに tracker.provider を直せと案内している: %q", joined)
	}
}

// {"RUCM-PATH": "P010"}
//
// TestDoctor_ボードが時間内に応答しなければ確かめられなかったとして残りを続ける は、
// 検査に期限があることを確かめる。
//
// 目的: doctor は「使い始める前に前提を機械的に検査する」道具である。**1項目が返らない
// だけで道具そのものが固まると、人間の手が止まる。**
// 与える情報: 期限より長く待ってから応答する偽ボードと、1項目 200ms の期限。
// 成功条件: ボードが `!`（確かめられなかった）になり、説明が「時間内に応答がありません
// でした」であること。**全項目が結果を持ち、終了コードが 1 にならないこと。**
func TestDoctor_ボードが時間内に応答しなければ確かめられなかったとして残りを続ける(t *testing.T) {
	fx := newFixture(t)
	fx.GitHub.SetDelay(30 * time.Second)

	opts := fx.Options()
	// **固定 200ms にしてはならない。**`go test -coverpkg=./...` は全パッケージを
	// instrument するので実行が遅くなり、**期限切れにしたい「ボード」より先に
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

// TestDoctor_接続先を差し替えているとボードの説明に出す は、繋ぎ先の秘匿を防ぐ。
//
// 目的: 接続先は環境変数1つで差し替わり、そこへ GitHub のトークンが送られる。
// **本物の GitHub でない宛先に繋いだことが出力に出ないと、人間が気づけない。**
// 与える情報: 偽ボード（httptest の 127.0.0.1）を接続先にした doctor。
// 成功条件: ボードの説明に「接続先を差し替えています」と、その URL が出ること。
func TestDoctor_接続先を差し替えているとボードの説明に出す(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	board := assertSymbol(t, report, doctor.LabelBoard, doctor.SymbolOK)
	if !strings.Contains(board.Detail, "接続先を差し替えています") {
		t.Fatalf("接続先を差し替えたことが説明に出ていない: %q", board.Detail)
	}
	if !strings.Contains(board.Detail, fx.GitHub.URL) {
		t.Fatalf("差し替えた接続先の URL が説明に出ていない: %q", board.Detail)
	}
}

// TestDoctor_claude が PATH に無ければ落とす は、着手する前に気づけることを確かめる。
//
// **この検査が無かったために、実運用で issue が1件無駄に止まった**（2026-08-21、設計 6-2）。
// claude が無くても herdr は pane を作れるので、着手は段9 まで進み、段10 の
// `agent_status: unknown` で初めて分かる。**そこまで行くと worktree も pane も作ったあとである。**
//
// 目的: `claude.kind` の実行ファイルが PATH に無いとき、`claude` の行が `✗` になり、
// 直し方が出て、終了コードが 1 になること。
// 与える情報: claude 以外の前提はすべて揃った状態。`LookPath` が必ず失敗する。
// 成功条件: `claude` が `✗`、直し方が1件以上、**残りの検査も全部走っていること**。
func TestDoctor_claudeがPATHに無ければ落とす(t *testing.T) {
	fx := newFixture(t)
	fx.ClaudePath = "" // PATH に無い状態にする

	report := doctor.Run(context.Background(), fx.Options())

	res := assertSymbol(t, report, doctor.LabelClaude, doctor.SymbolMissing)
	if len(res.Remedies) == 0 {
		t.Errorf("claude が無いのに直し方が出ていない: %+v", res)
	}
	// **1つ落ちても残りを全部検査する**（設計 3-32）。
	if got := labelsOf(report); len(got) != len(wantLabels) {
		t.Errorf("claude が落ちたら残りを検査していない: %v", got)
	}
	if report.ExitCode() != 1 {
		t.Errorf("終了コードが 1 でない: %d", report.ExitCode())
	}
}

// {"RUCM-PATH": "P020"}
//
// TestDoctor_設定ファイルが無ければ雛形の作成を勧める は、直し方を理由で分けたことを確かめる。
//
// 目的: 設定ファイルが**無い**（ENOENT）ときだけ `continuo init` を勧めること。
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: 直し方に `continuo init` が入り、ファイルシステムの案内が混ざらないこと。
func TestDoctor_設定ファイルが無ければ雛形の作成を勧める(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "continuo init") {
		t.Fatalf("設定ファイルが無いのに雛形の作成を勧めていない: %v", res.Remedies)
	}
	if strings.Contains(remedies, "wsl --shutdown") {
		t.Fatalf("ただ無いだけなのにファイルシステムの案内を出している: %v", res.Remedies)
	}
}

// {"RUCM-PATH": "P038"}
//
// TestDoctor_設定ファイルを読めないだけなら雛形の作成を勧めない は、
// **`continuo init` の案内で本物の設定を潰させないこと**を確かめる（issue #11）。
//
// 目的: ファイルは在るが読めない（EACCES）とき、`continuo init` を勧めないこと。
// その案内に従うと `continuo init` は「既にあります」で止まり、
// **`--force` を足すと本物の設定を雛形で潰す。**
// 与える情報: WORKFLOW.md の権限を 0 にした状態。
// 成功条件: 直し方が所有者と権限の確認になり、`continuo init` を含まないこと。
func TestDoctor_設定ファイルを読めないだけなら雛形の作成を勧めない(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく読めるので、この検査は成立しない")
	}
	fx := newFixture(t)
	if err := os.Chmod(fx.WorkflowPath, 0o000); err != nil {
		t.Fatalf("WORKFLOW.md の権限を変えられません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(fx.WorkflowPath, 0o600) })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if strings.Contains(remedies, "continuo init") {
		t.Fatalf("読めないだけなのに雛形の作成を勧めている: %v", res.Remedies)
	}
	if !strings.Contains(remedies, "所有者と権限") {
		t.Fatalf("所有者と権限の確認を勧めていない: %v", res.Remedies)
	}
}

// {"RUCM-PATH": "P008"}
//
// TestDoctor_ClaudeCodeの設定ディレクトリに書けなければ落とす は、
// **今回の `EROFS` を先に捕まえる検査**があることを確かめる（issue #11）。
//
// Claude Code は SessionStart hook を走らせる前に `~/.claude/session-env/<session_id>/` を作る。
// continuo はその hook を必ず張るので、**ここが書けないと issue は1件も始まらない。**
//
// 目的: `~/.claude` に書けないとき、`Claude の設定` が `✗` になり、
// なぜ困るのかと直し方が出ること。**設定が読めていても読めていなくても走ること。**
// 与える情報: ホームディレクトリの権限を読み取りと実行だけにした状態。
// 成功条件: `Claude の設定` が `✗`、内訳に SessionStart hook の説明が入り、終了コードが 1 であること。
func TestDoctor_ClaudeCodeの設定ディレクトリに書けなければ落とす(t *testing.T) {
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

// TestDoctor_ClaudeCodeの設定ディレクトリは検査のあとに残らない は、
// 検査が使い捨てのディレクトリを片付けることを確かめる。
//
// 目的: `~/.claude/session-env` の下に、検査に使ったディレクトリを残さないこと。
// 与える情報: 何も壊していない状態。
// 成功条件: `Claude の設定` が `✓` で、`session-env` の中身が0件であること。
func TestDoctor_ClaudeCodeの設定ディレクトリは検査のあとに残らない(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	assertSymbol(t, report, doctor.LabelClaudeHome, doctor.SymbolOK)
	entries, err := os.ReadDir(filepath.Join(fx.Home, ".claude", "session-env"))
	if err != nil {
		t.Fatalf("session-env を読めません: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("検査に使ったディレクトリが残っている: %v", entries)
	}
}

// TestDoctor_worktreeの置き場所に書けなければ落とす は、
// 着手してから初めて落ちる経路を、doctor が先に指摘することを確かめる（issue #11）。
//
// 目的: `workspace.root` に書けないとき `worktree の場所` が `✗` になること。
// 与える情報: `workspace.root` の親を書けない権限にした状態。
// 成功条件: `worktree の場所` が `✗`、直し方が1件以上、終了コードが 1 であること。
func TestDoctor_worktreeの置き場所に書けなければ落とす(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく書けるので、この検査は成立しない")
	}
	fx := newFixture(t)
	// `workspace.root` は `<root>/wt` である（fixture の WriteWorkflow）。
	// **その親を書けなくする。**root そのものを 0500 にすると他の検査まで巻き添えになるので、
	// 先に `wt` を作ってから、その中を書けなくする。
	wt := filepath.Join(fx.Root, "wt")
	if err := os.MkdirAll(wt, 0o500); err != nil {
		t.Fatalf("worktree の置き場所を作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(wt, 0o700) })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelWorkspaceRoot, doctor.SymbolMissing)
	if len(res.Remedies) == 0 {
		t.Fatalf("書けないのに直し方が出ていない: %+v", res)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}
