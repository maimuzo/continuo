// Package daemon_test のうち、このファイルは GitHub App の起動時の検査と、走行中の更新用のトークンの見張りを確かめる
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82c「取れないときに止める」・3-82f）。
//
// **本物の GitHub は1回も叩かない。**トークンの回転は httptest.Server の偽の OAuth に向け、
// カンバンはテスト用GitHub mock、herdr はテスト用herdr mock に向ける。
// **本物のホームも見ない。**`daemon.Options.HomeDir` に一時ディレクトリを渡す（渡さないと、
// 起動時の検査が本物の `~/.continuo/github-app-credentials.json` を読み、本物の更新用のトークンを回す）。
package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/githubapp"
)

// fakeOAuth は `POST /login/oauth/access_token` に答える偽の GitHub である。
type fakeOAuth struct {
	// URL は偽サーバのエンドポイントである。
	URL string
	// Deny は空でなければ、GitHub が断った形（`{"error": Deny}`）で答える。
	Deny string
	// RefreshExpiresIn は回転に成功したときに返す、新しい更新用のトークンの残り（秒）である。
	// **0 なら本物の GitHub の実測値（15638399秒。約181日）を返す**（3-82b）。
	RefreshExpiresIn int

	mu    sync.Mutex
	calls int
}

// newFakeOAuth は偽の OAuth のサーバを1本立てる。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// deny: 空なら回転に成功する応答、空でなければその値を `error` に入れて断る。
// 戻り値: 起動した偽サーバ。
func newFakeOAuth(t *testing.T, deny string) *fakeOAuth {
	t.Helper()
	fo := &fakeOAuth{Deny: deny}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fo.mu.Lock()
		fo.calls++
		fo.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/login/oauth/access_token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if fo.Deny != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": fo.Deny, "error_description": "テストの理由",
			})
			return
		}
		fo.mu.Lock()
		expiresIn := fo.RefreshExpiresIn
		fo.mu.Unlock()
		if expiresIn <= 0 {
			expiresIn = 15638399
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ghu_fake_access_token", "expires_in": 28800,
			"refresh_token": "ghr_fake_rotated", "refresh_token_expires_in": expiresIn,
			"token_type": "bearer",
		})
	}))
	t.Cleanup(srv.Close)
	fo.URL = srv.URL
	return fo
}

// Calls は回転を求められた回数を返す。
func (fo *fakeOAuth) Calls() int {
	fo.mu.Lock()
	defer fo.mu.Unlock()
	return fo.calls
}

// githubAppEnv は、起動時の検査の GitHub App の段まで進むための一式である。
type githubAppEnv struct {
	// Root は一時ディレクトリの根である。
	Root string
	// Home は `HOME` と `daemon.Options.HomeDir` に渡すホームである。
	Home string
	// OAuth は偽の OAuth である。
	OAuth *fakeOAuth
	// WorkflowPath は WORKFLOW.md の絶対パスである。
	WorkflowPath string
}

// newGitHubAppEnv は、書ける場所・偽 gh・テスト用herdr mock・テスト用GitHub mock（カンバン）・偽の OAuth を用意し、
// **GitHub App の検査より前の起動時の検査が全部通る**状態を作る。
//
// t: 呼び出し元のテスト。
// deny: 偽の OAuth が断るときの `error` の値（空なら成功）。
// attribution: WORKFLOW.md に書く `github_app_attribution` の値。
// serverPort: WORKFLOW.md に書く `server.port` の節（空なら書かない）。
// 戻り値: 起動に必要な一式。
func newGitHubAppEnv(t *testing.T, deny string, attribution bool, serverPort string) *githubAppEnv {
	t.Helper()
	root := wiringRoot(t)
	home := wiringHome(t)
	runtimeDir := filepath.Join(root, "rt")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	// **テスト用gh / ghq mock を PATH の先頭に置く。**本物の認証情報を読ませない。
	writeFakeGH(t, binDir, root)
	tl := &timeline{}
	herdr := newFakeHerdr(t, root, tl)
	github := newFakeGitHub(t, "octocat", tl)

	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
    token_source: env
    token_env: CONTINUO_TEST_TOKEN
  comments:
    github_app_attribution: %t
workspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
rate_limit:
  source: none
%s---

{{.issue.identifier}} を実装してください。
`, attribution, filepath.Join(root, "wt"), herdr.SocketPath, serverPort)
	path := filepath.Join(root, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, github.URL)
	t.Setenv("CONTINUO_TEST_TOKEN", "dummy-token-for-the-fake-server")

	return &githubAppEnv{Root: root, Home: home, OAuth: newFakeOAuth(t, deny), WorkflowPath: path}
}

// options は daemon.Run へ渡す入力を組み立てる。
//
// ghLogin: `gh api user` の代わりに使う関数。
// logger: ログの出力先。
// 戻り値: 偽のサーバと一時ディレクトリだけを見る入力。
func (e *githubAppEnv) options(ghLogin func(context.Context) (string, error), logger *slog.Logger) daemon.Options {
	return daemon.Options{
		ConfigPath:          e.WorkflowPath,
		Logger:              logger,
		StartupCheckTimeout: 10 * time.Second,
		HomeDir:             e.Home,
		GHLogin:             ghLogin,
		GitHubAppEndpoints:  githubapp.Endpoints{Web: e.OAuth.URL, API: e.OAuth.URL},
		GitHubAppHTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// writeCredentials は一時ディレクトリのホームに、全部揃った資格情報を書く（認可した人は `octocat`）。
//
// t: 呼び出し元のテスト。
// home: ホームディレクトリ。
// expiresIn: 更新用のトークンの残り。
// 戻り値: 書いた資格情報。
func writeCredentials(t *testing.T, home string, expiresIn time.Duration) githubapp.Credentials {
	t.Helper()
	creds := githubapp.Credentials{
		ClientID:              "Iv23liTESTCLIENTID",
		ClientSecret:          "test-client-secret",
		RefreshToken:          "ghr_test_refresh_token",
		RefreshTokenExpiresAt: time.Now().Add(expiresIn).UTC(),
		AuthorizedLogin:       "octocat",
		Slug:                  "continuo-octocat",
	}
	if err := githubapp.NewStore(home).Write(creds); err != nil {
		t.Fatalf("資格情報を書けません: %v", err)
	}
	return creds
}

// octocatLogin は `gh api user` が `octocat` を返す偽の関数である。
func octocatLogin(context.Context) (string, error) { return "octocat", nil }

// runUntilLog は daemon.Run を別の goroutine で呼び、ログに want が出るか Run が返るまで待ってから止める。
//
// **起動時の検査を通ったあとの経路（復元・巡回）は、この検査の相手ではない。**通ったことは
// ログの1行で見分け、見えたら ctx を切って戻す。
//
// t: 呼び出し元のテスト。
// opts: daemon.Run へ渡す入力（Logger は logged へ向けたものにすること）。
// logged: ログの溜まる先。
// want: 待つ文字列。
// 戻り値: daemon.Run の戻り値。
func runUntilLog(t *testing.T, opts daemon.Options, logged *syncBuffer, want string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, opts) }()

	deadline := time.After(30 * time.Second)
	for {
		select {
		case err := <-done:
			return err
		case <-deadline:
			cancel()
			t.Fatalf("30秒待っても %q がログに出ず、Run も返らない\n%s", want, logged.String())
		case <-time.After(20 * time.Millisecond):
		}
		if strings.Contains(logged.String(), want) {
			cancel()
			select {
			case err := <-done:
				return err
			case <-time.After(daemon.ShutdownBudget() + 5*time.Second):
				t.Fatalf("ctx を切っても Run が返らない\n%s", logged.String())
			}
		}
	}
}

// assertNotGitHubAppFailure は、起動を止めた理由が GitHub App の検査でないことを見る。
//
// t: 呼び出し元のテスト。
// err: daemon.Run の戻り値（nil でもよい）。
func assertNotGitHubAppFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, bad := range []string{"github_app_attribution が true ですが", "gh の持ち主は", "authorized_login がありません"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("GitHub App の検査で止まっている: %v", err)
		}
	}
}

// 目的: `github_app_attribution: true` で資格情報が無いとき、起動しないことと、1通り目の文面が出ることを確認する。
//
// **人間の決定「アクセストークンが取得できなかったならエラーで停止して良い」**（3-82c）。
// **止めたままにしない。**何をすればよいか（手元だけ false にして画面を通す4段）をその場で出す。
// **`server.port` を書いていない人には、書くように末尾の1行で言う。**書いてあれば URL に番号を埋める。
//
// 与える情報: `github_app_attribution: true`・資格情報の無いホーム・`server.port` が無い / 8080 / 0 の3通り。
// 成功条件: `ErrStartup` を包み、文面に「github_app_attribution が true ですが、GitHub App の資格情報がありません」と
// `/github-app` が入り、`server.port` の3通りで末尾の1行と URL の番号が変わること。偽の OAuth は1回も叩かれないこと。
func TestRun_GitHubApp_trueで資格情報が無ければ起動しない(t *testing.T) {
	cases := []struct {
		name       string
		serverPort string
		wantURL    string
		wantHint   string
		noHint     string
	}{
		{"server.port が無い", "", "http://127.0.0.1:<port>/github-app", "server.port を書いていないときは、先に書いてください", "0 にしているときは"},
		{"server.port が 8080", "server:\n  port: 8080\n", "http://127.0.0.1:8080/github-app", "", "server.port を書いていないときは"},
		{"server.port が 0", "server:\n  port: 0\n", "http://127.0.0.1:<port>/github-app", "0 にしているときは、具体的な番号にしてください", "server.port を書いていないときは"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newGitHubAppEnv(t, "", true, tc.serverPort)

			err := daemon.Run(context.Background(), env.options(octocatLogin, slog.New(slog.DiscardHandler)))
			if err == nil {
				t.Fatal("資格情報が無いのに起動できてしまった")
			}
			if !errors.Is(err, daemon.ErrStartup) {
				t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
			}
			msg := err.Error()
			for _, want := range []string{
				"github_app_attribution が true ですが、GitHub App の資格情報がありません",
				"手元だけ false にする",
				tc.wantURL,
				"true に戻して、continuo を再起動する",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("文面に %q が入っていない:\n%s", want, msg)
				}
			}
			if tc.wantHint != "" && !strings.Contains(msg, tc.wantHint) {
				t.Errorf("末尾の1行 %q が入っていない:\n%s", tc.wantHint, msg)
			}
			if strings.Contains(msg, tc.noHint) {
				t.Errorf("この場合には出ないはずの %q が入っている:\n%s", tc.noHint, msg)
			}
			if env.OAuth.Calls() != 0 {
				t.Errorf("資格情報が無いのに GitHub へ回転を求めた（%d 回）", env.OAuth.Calls())
			}
		})
	}
}

// 目的: 資格情報は在るのに GitHub が回転を断ったとき、起動しないことと、2通り目の文面が出ることを確認する。
//
// **「資格情報がありません」は嘘になる**（3-82c）。認可のやり直しで直る道と、作り直す道の両方を出す。
// **`（<GitHub が返した error の値>）` には `error` の値そのもの**（`bad_refresh_token`）**を入れる。**
//
// 与える情報: `github_app_attribution: true`・揃った資格情報・`{"error":"bad_refresh_token"}` を返す偽の OAuth。
// 成功条件: `ErrStartup` を包み、文面に「更新用のトークンを回せませんでした（bad_refresh_token）」と
// `/github-app/authorize` が入ること。偽の OAuth が1回だけ叩かれること。
func TestRun_GitHubApp_回転を断られたら認可をやり直す文面で起動しない(t *testing.T) {
	env := newGitHubAppEnv(t, "bad_refresh_token", true, "")
	writeCredentials(t, env.Home, 180*24*time.Hour)

	err := daemon.Run(context.Background(), env.options(octocatLogin, slog.New(slog.DiscardHandler)))
	if err == nil {
		t.Fatal("回転を断られたのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"GitHub App の更新用のトークンを回せませんでした（bad_refresh_token）",
		"/github-app/authorize を開き、認可をやり直す",
		"client secret を作り直しています",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("文面に %q が入っていない:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "資格情報がありません") {
		t.Errorf("資格情報は在るのに「ありません」と言っている:\n%s", msg)
	}
	if env.OAuth.Calls() != 1 {
		t.Errorf("回転を %d 回求めた（起動1回につき1回のはず）", env.OAuth.Calls())
	}
}

// 目的: 認可した人と `gh` の持ち主が違えば、起動しないことと、両方の名前を並べた文面が出ることを確認する。
//
// **違ったまま起動させない**（3-82f）。あとで気づくと、その間の run が全部、黙って人間へ渡る。
// **突き合わせのためにトークンをもう1回取らない**（回転は起動1回につき1回）。
//
// 与える情報: 認可した人が `octocat` の資格情報と、`hubot` を返す `gh api user`。
// 成功条件: `ErrStartup` を包み、文面に「gh の持ち主は hubot、GitHub App を認可したのは octocat」と
// `gh auth switch` と `/github-app/authorize` が入り、偽の OAuth が1回だけ叩かれること。
func TestRun_GitHubApp_認可した人がghの持ち主と違えば起動しない(t *testing.T) {
	env := newGitHubAppEnv(t, "", true, "")
	writeCredentials(t, env.Home, 180*24*time.Hour)

	hubot := func(context.Context) (string, error) { return "hubot", nil }
	err := daemon.Run(context.Background(), env.options(hubot, slog.New(slog.DiscardHandler)))
	if err == nil {
		t.Fatal("認可した人が違うのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"gh の持ち主は hubot、GitHub App を認可したのは octocat です",
		"gh auth switch で hubot を octocat に替える",
		"/github-app/authorize で octocat ではなく hubot として認可し直してください",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("文面に %q が入っていない:\n%s", want, msg)
		}
	}
	if env.OAuth.Calls() != 1 {
		t.Errorf("回転を %d 回求めた（突き合わせのために2回転させてはならない）", env.OAuth.Calls())
	}
}

// 目的: `gh api user` が取れなくても、その理由では起動を止めず、WARN を1行出して検査を通すことを確認する。
//
// **このコードベースは「取れなくても止めない」を2箇所で決めている**（設計 3-65。3-82f）。
// 恒久的なずれは、次に `gh api` が届いた起動で必ず捕まる。
//
// 与える情報: 揃った資格情報と、落ちる `gh api user`。
// 成功条件: ログに「突き合わせずに起動します」の WARN と「GitHub App のトークンが取れることを確かめました」の
// Info が出て、Run の戻り値が GitHub App の検査の失敗でないこと。
func TestRun_GitHubApp_gh_api_userが取れなくてもその理由では止まらない(t *testing.T) {
	env := newGitHubAppEnv(t, "", true, "")
	writeCredentials(t, env.Home, 180*24*time.Hour)

	logged := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	failing := func(context.Context) (string, error) { return "", errors.New("gh: not logged in") }

	err := runUntilLog(t, env.options(failing, logger), logged, "GitHub App のトークンが取れることを確かめました")
	assertNotGitHubAppFailure(t, err)
	if !strings.Contains(logged.String(), "突き合わせずに起動します") {
		t.Fatalf("gh api user が取れないことを WARN で出していない\n%s", logged.String())
	}
	if !strings.Contains(logged.String(), "GitHub App のトークンが取れることを確かめました") {
		t.Fatalf("検査を通ったことを Info で出していない\n%s", logged.String())
	}
}

// 目的: 全部揃っていれば検査を通り、回転を1回だけ求め、新しい更新用のトークンと期限を書き戻すことを確認する。
//
// **残りが30日を切っていれば WARN を1行出す**（3-82c）。**見るのは書き戻したあとの期限である。**
// 回転が通れば GitHub は新しい期限を返すので（実測は約181日）、起動時に残りが短いのは
// GitHub が短い期限を返したときだけである。偽の OAuth にその形で答えさせる。
//
// 与える情報: 揃った資格情報と、残り10日と12時間の期限を返す偽の OAuth。
// 成功条件: Info の1行が出て、資格情報の更新用のトークンが偽の OAuth の返した値に書き戻され、
// 期限が近いことの WARN が出ること。
func TestRun_GitHubApp_揃っていれば検査を通り書き戻す(t *testing.T) {
	env := newGitHubAppEnv(t, "", true, "")
	env.OAuth.RefreshExpiresIn = int((10*24*time.Hour + 12*time.Hour) / time.Second)
	writeCredentials(t, env.Home, 180*24*time.Hour)

	logged := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	err := runUntilLog(t, env.options(octocatLogin, logger), logged, "GitHub App のトークンが取れることを確かめました")
	assertNotGitHubAppFailure(t, err)
	if env.OAuth.Calls() != 1 {
		t.Errorf("回転を %d 回求めた（起動1回につき1回のはず）", env.OAuth.Calls())
	}
	after, _, rerr := githubapp.NewStore(env.Home).Read()
	if rerr != nil {
		t.Fatalf("資格情報を読み直せません: %v", rerr)
	}
	if after.RefreshToken != "ghr_fake_rotated" {
		t.Errorf("新しい更新用のトークンが書き戻されていない: %q", after.RefreshToken)
	}
	if !strings.Contains(logged.String(), "更新用のトークンの期限が近づいています") {
		t.Errorf("残りが30日を切っているのに WARN が出ていない\n%s", logged.String())
	}
}

// 目的: `github_app_attribution: false` なら、資格情報が無くても起動時の検査を通ることを確認する。
//
// **書かない利用者の continuo はいままでどおり動く**（3-82c）。
//
// 与える情報: `github_app_attribution: false` と、資格情報の無いホーム。
// 成功条件: Run の戻り値が GitHub App の検査の失敗でなく、偽の OAuth が1回も叩かれず、
// 「GitHub App のトークンが取れることを確かめました」の Info が出ないこと。
func TestRun_GitHubApp_falseなら資格情報が無くても検査を通る(t *testing.T) {
	env := newGitHubAppEnv(t, "", false, "")

	logged := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	err := runUntilLog(t, env.options(octocatLogin, logger), logged, "巡回を始めます")
	assertNotGitHubAppFailure(t, err)
	if env.OAuth.Calls() != 0 {
		t.Errorf("false なのに GitHub へ回転を求めた（%d 回）", env.OAuth.Calls())
	}
	if strings.Contains(logged.String(), "GitHub App のトークンが取れることを確かめました") {
		t.Errorf("false なのに GitHub App の検査を走らせている\n%s", logged.String())
	}
}

// 目的: 走行中の見張りが、間隔ごとに資格情報を読み、残りが30日を切っていれば WARN を出し、回転はしないことを確認する。
//
// **読むだけで回さない**（3-82c）。回すと、それまでに配ったアクセストークンが死ぬ。
//
// 与える情報: 残り10日の資格情報と、短い間隔。
// 成功条件: WARN が出て、資格情報の更新用のトークンが変わらず、ctx を切ると戻ること。
func TestWatchRefreshTokenExpiry_残りが30日を切っていればWARNを出し回さない(t *testing.T) {
	home := wiringHome(t)
	creds := writeCredentials(t, home, 10*24*time.Hour+12*time.Hour)
	store := githubapp.NewStore(home)

	logged := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.WatchRefreshTokenExpiry(ctx, store, 20*time.Millisecond, nil, logger)
	}()

	waitFor(t, 5*time.Second, "期限が近いことの WARN", func() bool {
		return strings.Contains(logged.String(), "更新用のトークンの期限が近づいています")
	})
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ctx を切っても見張りが戻らない")
	}

	after, _, err := store.Read()
	if err != nil {
		t.Fatalf("資格情報を読み直せません: %v", err)
	}
	if after.RefreshToken != creds.RefreshToken {
		t.Fatalf("見張りが更新用のトークンを回している: %q → %q", creds.RefreshToken, after.RefreshToken)
	}
}

// 目的: 走行中の見張りが、残りが30日より長ければ何も出さないことを確認する。
//
// 与える情報: 残り180日の資格情報と、短い間隔。
// 成功条件: 何回か読んだあとも WARN が1行も出ないこと。
func TestWatchRefreshTokenExpiry_余裕があれば何も出さない(t *testing.T) {
	home := wiringHome(t)
	writeCredentials(t, home, 180*24*time.Hour)

	logged := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	daemon.WatchRefreshTokenExpiry(ctx, githubapp.NewStore(home), 20*time.Millisecond, nil, logger)

	if strings.Contains(logged.String(), "期限が近づいています") {
		t.Fatalf("余裕があるのに WARN を出している\n%s", logged.String())
	}
}
