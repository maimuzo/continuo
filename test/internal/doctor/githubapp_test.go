// Package doctor_test のうち、このファイルは見出し語 `GitHub App` を確かめる
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82c「`continuo doctor` が検査すること」・3-82f）。
//
// **本物の GitHub は1回も叩かない。**この検査はトークンを取らない（更新用のトークンを回さない）ので、
// 偽サーバも要らない。資格情報は fixture の一時ディレクトリの `.continuo/` に置く。
// **本物の `gh api user` も呼ばない。**fixture の GHLogin が代わりに答える。
package doctor_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/githubapp"
)

// attributionKey は `tracker.comments.github_app_attribution` のパスである。
var attributionKey = []string{"tracker", "comments", "github_app_attribution"}

// serverPortKey は `server.port` のパスである。
var serverPortKey = []string{"server", "port"}

// validCredentials は、全部揃った資格情報を返す（認可した人は fixture の既定と同じ `octocat`）。
//
// expiresIn: 更新用のトークンの残り。
// 戻り値: 書き込める資格情報。
func validCredentials(expiresIn time.Duration) githubapp.Credentials {
	return githubapp.Credentials{
		ClientID:              "Iv23liTESTCLIENTID",
		ClientSecret:          "test-client-secret",
		RefreshToken:          "ghr_test_refresh_token",
		RefreshTokenExpiresAt: time.Now().Add(expiresIn).UTC(),
		AuthorizedLogin:       "octocat",
		Slug:                  "continuo-octocat",
	}
}

// writeCredentials は fixture のホームの `.continuo/github-app-credentials.json` に資格情報を書く（0600）。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
// creds: 書く資格情報。
// 戻り値: 書いたファイルの絶対パス。
func writeCredentials(t *testing.T, fx *fixture, creds githubapp.Credentials) string {
	t.Helper()
	store := githubapp.NewStore(fx.Home)
	if err := store.Write(creds); err != nil {
		t.Fatalf("資格情報を書けません: %v", err)
	}
	return store.Path()
}

// enableAttribution は WORKFLOW.md の `github_app_attribution` を true にする。
//
// t: 呼び出し元のテスト。
// fx: 使っている fixture。
func enableAttribution(t *testing.T, fx *fixture) {
	t.Helper()
	fx.SetFrontMatter(t, attributionKey, "true")
}

// 目的: `github_app_attribution` が false（雛形の既定）なら、資格情報が無くても `✓` にすることを確認する。
//
// **書かない利用者の continuo はいままでどおり動く**（3-82c）。資格情報を要求してはならない。
//
// 与える情報: 雛形どおりの WORKFLOW.md（`github_app_attribution: false`）と、資格情報の無いホーム。
// 成功条件: `GitHub App` が `✓` で、説明に「false なので」が入り、終了コードが 0 であること。
func TestDoctor_GitHubApp_falseなら資格情報が無くても通る(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "false なので") {
		t.Fatalf("説明が「false なので使わない」になっていない: %q", res.Detail)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: 設定ファイルが読めなければ `!` にし、直し方に WORKFLOW.md を直すことを出すことを確認する。
//
// **`github_app_attribution` が true かどうかが決まらないので、資格情報を見に行かない。**
//
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: `GitHub App` が `!` で、説明が設定ファイルを読めなかったことを指すこと。
func TestDoctor_GitHubApp_設定が読めなければ確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "設定ファイルを読めなかったため") {
		t.Fatalf("説明が上流の失敗を指していない: %q", res.Detail)
	}
}

// 目的: true なのに資格情報が無いとき `✗` にし、起動を止める文面と同じ4段の手順を直し方に出すことを確認する。
//
// **起動しない状態では doctor しか叩けない**（3-82c）。手順が doctor に無いと、その人は
// 「チームの設定を勝手に変えてよいのか」を判断できずに止まる。
// **`server.port` を書いていない人は画面を開けない**ので、「server.port を書いて起動する」も足す（3-82g）。
//
// 与える情報: `github_app_attribution: true`・資格情報の無いホーム・`server.port: null`（雛形の既定）。
// 成功条件: `✗` で、説明に資格情報のパスが入り、直し方に4段（手元だけ false / 起動 / `/github-app` を開く /
// true に戻す）と `server.port` の案内が入り、終了コードが 1 であること。
func TestDoctor_GitHubApp_trueなのに資格情報が無ければ4段の手順を出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "github-app-credentials.json") {
		t.Fatalf("説明に資格情報のパスが入っていない: %q", res.Detail)
	}
	remedies := strings.Join(res.Remedies, "\n")
	for _, want := range []string{
		"1. WORKFLOW.md の tracker.comments.github_app_attribution を、手元だけ false にする",
		"commit しないでください",
		"2. continuo を起動する",
		"3. http://127.0.0.1:<port>/github-app を開き、ボタンを3回押す",
		"4. github_app_attribution を true に戻して、continuo を再起動する",
		"server.port を書いて起動し、/github-app を開いてください",
	} {
		if !strings.Contains(remedies, want) {
			t.Errorf("直し方に %q が入っていない:\n%s", want, remedies)
		}
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: `server.port` が具体的な番号なら、直し方の URL にその番号を埋め、`server.port` の案内を出さないことを確認する。
//
// 与える情報: `github_app_attribution: true`・資格情報の無いホーム・`server.port: 8080`。
// 成功条件: 直し方に `http://127.0.0.1:8080/github-app` が入り、「server.port を書いて」が入らないこと。
func TestDoctor_GitHubApp_server_portが決まっていればURLに番号を埋める(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	fx.SetFrontMatter(t, serverPortKey, "8080")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "http://127.0.0.1:8080/github-app") {
		t.Fatalf("直し方の URL に server.port の番号が埋まっていない:\n%s", remedies)
	}
	if strings.Contains(remedies, "server.port を書いて") {
		t.Fatalf("server.port を書いてあるのに、書けという案内が出ている:\n%s", remedies)
	}
}

// 目的: 資格情報の権限が 0600 でなければ `✗` にし、`chmod 600 <パス>` を直し方に出すことを確認する。
//
// **本体は WARN を出して読むが、doctor は `✗` を出す**（3-82b の表）。直す案内が出る場所はここだけである。
//
// 与える情報: `github_app_attribution: true` と、0644 の資格情報。
// 成功条件: `✗` で、説明に権限が入り、直し方に `chmod 600` とパスが入ること。
func TestDoctor_GitHubApp_権限が0600でなければchmodを出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	path := writeCredentials(t, fx, validCredentials(180*24*time.Hour))
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("権限を変えられません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "-rw-r--r--") {
		t.Fatalf("説明にいまの権限が入っていない: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "chmod 600 "+path) {
		t.Fatalf("直し方に chmod 600 とパスが入っていない: %v", res.Remedies)
	}
}

// 目的: `authorized_login` が欠けていれば `✗` にし、次の検査が比べる相手を失うことを説明することを確認する。
//
// **`authorized_login` が無いと、doctor は認可した人を知るためにトークンを取ることになる**（3-82b）。
// doctor は1度も回さない約束なので、欠けていること自体を `✗` にする。
//
// 与える情報: `github_app_attribution: true` と、`authorized_login` だけが空の資格情報。
// 成功条件: `✗` で、説明に `authorized_login` が入り、内訳に「比べる相手」の説明が入り、
// 直し方に `/github-app/authorize` が入ること。
func TestDoctor_GitHubApp_authorized_loginが欠けていれば足りないと出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	creds := validCredentials(180 * 24 * time.Hour)
	creds.AuthorizedLogin = ""
	writeCredentials(t, fx, creds)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "authorized_login") {
		t.Fatalf("説明に欠けている欄の名前が入っていない: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "比べる相手") {
		t.Fatalf("次の検査が比べる相手を失うことを説明していない: %v", res.Notes)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "/github-app/authorize") {
		t.Fatalf("直し方に認可のやり直しが入っていない: %v", res.Remedies)
	}
}

// 目的: 更新用のトークンの期限が切れていれば `✗` にし、認可のやり直しを直し方に出すことを確認する。
//
// 与える情報: `github_app_attribution: true` と、期限が1日前の資格情報。
// 成功条件: `✗` で、説明に「切れています」が入り、直し方に `/github-app/authorize` が入ること。
func TestDoctor_GitHubApp_期限が切れていれば足りないと出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	writeCredentials(t, fx, validCredentials(-24*time.Hour))

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "切れています") {
		t.Fatalf("説明が期限切れを指していない: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "/github-app/authorize") {
		t.Fatalf("直し方に認可のやり直しが入っていない: %v", res.Remedies)
	}
}

// 目的: 残りが30日を切っていれば `!` にし、期限と残り日数を出し、認可をやり直せば延びることを出すことを確認する。
//
// **`✗` にしない。**動くが、181日目に突然止まる前に知らせる（3-82c）。
//
// 与える情報: `github_app_attribution: true` と、期限が10日と12時間後の資格情報
// （12時間の余裕は、テストの実行中に日付の境を跨いでも「残り 10 日」のままにするため）。
// 成功条件: `!` で、説明に「残り 10 日」が入り、直し方に `/github-app/authorize` が入り、終了コードが 0 であること。
func TestDoctor_GitHubApp_残りが30日を切っていれば警告する(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	writeCredentials(t, fx, validCredentials(10*24*time.Hour+12*time.Hour))

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolUnknown)
	if !strings.Contains(res.Detail, "残り 10 日") {
		t.Fatalf("説明に残り日数が入っていない: %q", res.Detail)
	}
	if !strings.Contains(strings.Join(res.Remedies, "\n"), "/github-app/authorize") {
		t.Fatalf("直し方に認可のやり直しが入っていない: %v", res.Remedies)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("! だけなのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: 認可した人と `gh` の持ち主が違えば `✗` にし、両方の名前と2つの直し方を出すことを確認する。
//
// **違ったまま起動すると、その機械の run が全部、黙って人間へ渡る**（3-82f）。
//
// 与える情報: `github_app_attribution: true`・認可した人が `octocat` の資格情報・`gh api user` が `hubot`。
// 成功条件: `✗` で、説明に「gh の持ち主は hubot、GitHub App を認可したのは octocat」が入り、
// 直し方に `gh auth switch` と `/github-app/authorize` の両方が入ること。
func TestDoctor_GitHubApp_認可した人がghの持ち主と違えば足りないと出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	writeCredentials(t, fx, validCredentials(180*24*time.Hour))
	fx.GHLogin = "hubot"

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "gh の持ち主は hubot、GitHub App を認可したのは octocat") {
		t.Fatalf("説明に両方の名前が入っていない: %q", res.Detail)
	}
	// **認可し直すのは gh の持ち主（hubot）としてである**（3-82f の「B ではなく A として」）。
	remedies := strings.Join(res.Remedies, "\n")
	for _, want := range []string{"gh auth switch で gh の持ち主を hubot から octocat に", "/github-app/authorize で hubot として認可し直して"} {
		if !strings.Contains(remedies, want) {
			t.Errorf("直し方に %q が入っていない:\n%s", want, remedies)
		}
	}
}

// 目的: `gh api user` が取れなければ `!` にし、突き合わせられなかった理由を出すことを確認する。
//
// **`✗` にしない。**`gh api` に一時的に届かないだけで足りないと言うと、直すものが無い人を止める（3-82f）。
//
// 与える情報: `github_app_attribution: true`・揃った資格情報・落ちる `gh api user`。
// 成功条件: `!` で、説明に認可した人と `gh api user` の失敗の文言が入ること。
func TestDoctor_GitHubApp_gh_api_userが取れなければ確かめられなかったになる(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	writeCredentials(t, fx, validCredentials(180*24*time.Hour))
	fx.GHLoginErr = errors.New("gh: not logged in")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolUnknown)
	for _, want := range []string{"octocat", "突き合わせられませんでした", "not logged in"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("説明に %q が入っていない: %q", want, res.Detail)
		}
	}
}

// 目的: 全部揃っていれば `✓` にし、認可した人と更新用のトークンの期限を出すことを確認する。
//
// **トークンは1度も取らない。**この test は GitHub の偽サーバを立てていないので、
// 取りに行けば失敗する。`✓` になること自体が、取りに行っていない証拠である。
//
// 与える情報: `github_app_attribution: true`・揃った資格情報（残り180日）・`gh api user` が `octocat`。
// 成功条件: `✓` で、説明に `octocat` と期限が入り、資格情報のファイルの更新用のトークンが変わっていないこと。
func TestDoctor_GitHubApp_全部揃っていれば通りトークンを回さない(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	creds := validCredentials(180 * 24 * time.Hour)
	writeCredentials(t, fx, creds)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolOK)
	if !strings.Contains(res.Detail, "認可した人 octocat") {
		t.Fatalf("説明に認可した人が入っていない: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, creds.RefreshTokenExpiresAt.Format(time.RFC3339)) {
		t.Fatalf("説明に更新用のトークンの期限が入っていない: %q", res.Detail)
	}
	after, _, err := githubapp.NewStore(fx.Home).Read()
	if err != nil {
		t.Fatalf("資格情報を読み直せません: %v", err)
	}
	if after.RefreshToken != creds.RefreshToken {
		t.Fatalf("doctor が更新用のトークンを回している: %q → %q", creds.RefreshToken, after.RefreshToken)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("すべて通ったのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}

// 目的: 資格情報のファイルは在るのに JSON として壊れていれば `✗` にし、消して作り直す直し方を出すことを確認する。
//
// **認可のやり直しでは直らない。**`client_id` と `client_secret` も読めないので、段1（作る）からやり直す。
//
// 与える情報: `github_app_attribution: true` と、中身が JSON でない資格情報のファイル。
// 成功条件: `✗` で、説明に「読めません」が入り、直し方に「消してから」と `/github-app` が入ること。
func TestDoctor_GitHubApp_壊れていれば消して作り直す案内を出す(t *testing.T) {
	fx := newFixture(t)
	enableAttribution(t, fx)
	store := githubapp.NewStore(fx.Home)
	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		t.Fatalf("資格情報の置き場所を作れません: %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte("これは JSON ではない"), 0o600); err != nil {
		t.Fatalf("壊れた資格情報を書けません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelGitHubApp, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "読めません") {
		t.Fatalf("説明が読めないことを指していない: %q", res.Detail)
	}
	remedies := strings.Join(res.Remedies, "\n")
	for _, want := range []string{store.Path() + " を消してから", "/github-app"} {
		if !strings.Contains(remedies, want) {
			t.Errorf("直し方に %q が入っていない:\n%s", want, remedies)
		}
	}
}
