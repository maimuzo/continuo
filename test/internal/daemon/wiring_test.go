package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
)

// writeWiringWorkflow は、結線だけを確かめるための最小の WORKFLOW.md を書く。
//
// **バイナリを起動しない。**このファイルのテストは daemon.Run をそのまま呼び、
// 起動の段（設定 → 接続先の検査 → flock → 組み立て）で止まるところまでを見る。
//
// t: 呼び出し元のテスト。
// root: WORKFLOW.md を置くディレクトリ。
// providerExtra: `tracker.provider` の中へ足す行（4桁の字下げ。空でよい）。
// extra: front matter へ足す節（末尾に改行を含めること。空でよい）。
// 戻り値: 書いた WORKFLOW.md の絶対パス。
func writeWiringWorkflow(t *testing.T, root, providerExtra, extra string) string {
	t.Helper()

	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
%sworkspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
rate_limit:
  source: none
%s---

{{.issue.identifier}} を実装してください。
`, providerExtra, filepath.Join(root, "wt"), filepath.Join(root, "h.sock"), extra)

	path := filepath.Join(root, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return path
}

// wiringRoot は短い一時ディレクトリを1つ作る（socket のパスを103バイト以内に保つため）。
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func wiringRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "cw")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}
	return root
}

// TestResolveLockFilePath_lock_fileの指定があればそれを使い無ければsocketと同じ場所に置く は、
// 設定の解釈を固定する。
//
// 目的: 設計 3-17 / 3-23。`runtime.lock_file` を書いたときと書かないときで、
// ロックファイルの場所が変わることを確かめる。**この関数は公開されているのに
// 一度も実行されていなかった。**
// 与える情報: `runtime.lock_file` が null・空文字・絶対パスの3通りと、hook の socket のパス。
// 成功条件: 絶対パスを書いたときだけその値になり、ほかは socket と同じディレクトリになること。
func TestResolveLockFilePath_lock_fileの指定があればそれを使い無ければsocketと同じ場所に置く(t *testing.T) {
	sockPath := "/tmp/continuo-rt/hooks.sock"
	empty := ""
	explicit := "/var/tmp/continuo-elsewhere/continuo.lock"

	cases := []struct {
		name     string
		lockFile *string
		want     string
	}{
		{
			name:     "lock_file が null なら socket と同じディレクトリに置く",
			lockFile: nil,
			want:     "/tmp/continuo-rt/continuo.lock",
		},
		{
			name:     "lock_file が空文字でも socket と同じディレクトリに置く",
			lockFile: &empty,
			want:     "/tmp/continuo-rt/continuo.lock",
		},
		{
			name:     "lock_file に絶対パスを書いたらそこに置く",
			lockFile: &explicit,
			want:     explicit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.Config
			cfg.Runtime.LockFile = tc.lockFile

			if got := daemon.ResolveLockFilePath(cfg, sockPath); got != tc.want {
				t.Fatalf("ロックファイルの場所が違う: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestValidateGraphQLEndpoint_httpsとループバック宛のhttpだけを受け付ける は、
// トークンの送り先の検査を固定する。
//
// 目的: 環境変数に書いた URL へ `gh auth token` のトークンが `Authorization: Bearer` で
// 送られる。**scheme もホストも見ずに採用すると、1行書き足すだけで送り先を変えられる。**
// 与える情報: 空文字・https・ループバック宛の http・外部宛の http・scheme 違い・壊れた URL。
// 成功条件: 空文字と https とループバック宛の http だけが通り、ほかはエラーになること。
func TestValidateGraphQLEndpoint_httpsとループバック宛のhttpだけを受け付ける(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "空なら本番の GitHub を使うので検査しない", raw: "", wantErr: false},
		{name: "本番の GitHub は通る", raw: "https://api.github.com/graphql", wantErr: false},
		{name: "GitHub Enterprise Server も https なら通る", raw: "https://ghe.example.com/api/graphql", wantErr: false},
		{name: "テストの偽サーバ（127.0.0.1 の http）は通る", raw: "http://127.0.0.1:18999/graphql", wantErr: false},
		{name: "localhost の http も通る", raw: "http://localhost:18999/graphql", wantErr: false},
		{name: "ループバック以外の http は断る", raw: "http://example.com/graphql", wantErr: true},
		{name: "https でも http でもない scheme は断る", raw: "ftp://example.com/graphql", wantErr: true},
		{name: "ホスト名が無ければ断る", raw: "https:///graphql", wantErr: true},
		{name: "URL として解釈できなければ断る", raw: "https://%zz/graphql", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := daemon.ValidateGraphQLEndpoint(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("%q を受け付けてしまった（トークンの送り先になる）", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%q を断ってしまった: %v", tc.raw, err)
			}
		})
	}
}

// TestWaitWithTimeout_期限内に終わらなければ待つのをやめる は、終了処理の期限を固定する。
//
// 目的: 終了時の turn ループの待ちが無期限だと、turn ループが1本でも返らなくなった時点で
// `SIGKILL` でしか止められなくなる（設計 3-4 の終了の作法）。
// 与える情報: すぐ返る処理と、テストが終わるまで返らない処理。
// 成功条件: すぐ返る処理では true、返らない処理では期限のあとに false が返ること。
func TestWaitWithTimeout_期限内に終わらなければ待つのをやめる(t *testing.T) {
	if !daemon.WaitWithTimeout(func() {}, 5*time.Second) {
		t.Fatal("すぐ返る処理を待ち切れなかった")
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	start := time.Now()
	if daemon.WaitWithTimeout(func() { <-release }, 50*time.Millisecond) {
		t.Fatal("返らない処理を待ち切ったことになっている（無期限に待つと終了できない）")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("期限を過ぎても待ち続けた: %v", elapsed)
	}
}

// TestRun_ロックファイルを開けないときは二重起動と報告しない は、
// 起動を止めた理由の言い分けを固定する。
//
// 目的: `runtime.lock_file` のパスを打ち間違えた運用者に「二重起動」と報告すると、
// 動いてもいない2つ目の continuo を探しに行かせることになる。
// 与える情報: 親ディレクトリが存在しない `runtime.lock_file`。
// 成功条件: 起動の段のエラー（daemon.ErrStartup）で、文言が「ロックファイルを用意できません」
// であり、**「二重起動」を含まないこと。**
func TestRun_ロックファイルを開けないときは二重起動と報告しない(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	lockFile := filepath.Join(root, "この階層は存在しない", "continuo.lock")
	path := writeWiringWorkflow(t, root, "", fmt.Sprintf("runtime:\n  lock_file: %s\n", lockFile))

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("ロックファイルを開けないのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "ロックファイルを用意できません") {
		t.Fatalf("ロックファイルを用意できないことが文言に出ていない: %v", err)
	}
	if strings.Contains(err.Error(), "二重起動") {
		t.Fatalf("設定の誤りを二重起動として報告している: %v", err)
	}
}

// TestRun_接続先の差し替えがループバック以外のhttpなら起動を止める は、
// トークンの送り先の検査が結線に入っていることを確かめる。
//
// 目的: 環境変数の URL へ GitHub のトークンが送られる。**平文で外部へ流さない。**
// 与える情報: `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` に `http://example.com/graphql`。
// 成功条件: 起動の段のエラーで止まり、文言が https を求めていること。
// **ロックファイルを作る前に止まること**（実行時ディレクトリに何も残らない）。
func TestRun_接続先の差し替えがループバック以外のhttpなら起動を止める(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "http://example.com/graphql")

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("ループバック以外の http へ繋ぐ設定なのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatalf("https を求める文言になっていない: %v", err)
	}
	if entries, readErr := os.ReadDir(runtimeDir); readErr == nil && len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("接続先を確かめる前に実行時ディレクトリへ書き込んでいる: %v", names)
	}
}

// TestRun_ghがPATHに無ければ起動時検査の文言で落ちる は、gh を起動する順序を固定する。
//
// 目的: `token_source` の既定は `gh_auth` なので、依存の組み立ての中で `gh auth token` が
// 走る。**先に `gh` の有無を見ないと、直し方の書いてある起動時検査（設計 3-6）の文言に
// 辿り着かず、「トークンを取得できません」で落ちる。**
// 与える情報: `gh` が1つも無い PATH。
// 成功条件: 起動の段のエラーで、文言が「PATH にありません」であること。
func TestRun_ghがPATHに無ければ起動時検査の文言で落ちる(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	emptyBin := filepath.Join(root, "emptybin")
	for _, dir := range []string{runtimeDir, emptyBin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("ディレクトリを作れません（%s）: %v", dir, err)
		}
	}
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")
	// **`gh` を1つも見つけられない PATH にする。**本物の gh を起動しないためでもある。
	t.Setenv("PATH", emptyBin)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("gh が無いのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "PATH にありません") {
		t.Fatalf("gh が PATH に無いことを指す文言になっていない: %v", err)
	}
}

// TestRestoreDefaultSignalsOnShutdown_1回目を受けたら登録を外す は、
// 2回目の signal が効くことを支える結線を確かめる。
//
// 目的: `signal.NotifyContext` は1回目の signal のあとも登録を残すため、**外さないと
// 2回目以降の `SIGTERM` / `SIGINT` が吸われて効かない。**終了処理が長引いたときに
// `SIGKILL` しか手が無くなる（設計 3-4 の終了の作法）。
// 与える情報: 途中で終わらせるコンテキストと、呼ばれたことを記録する解除の関数。
// 成功条件: コンテキストが生きている間は解除が呼ばれず、終わったあとに1回呼ばれること。
func TestRestoreDefaultSignalsOnShutdown_1回目を受けたら登録を外す(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})

	daemon.RestoreDefaultSignalsOnShutdown(ctx, func() { close(stopped) })

	select {
	case <-stopped:
		t.Fatal("コンテキストが生きているうちに signal の登録を外した")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("コンテキストが終わっても signal の登録を外さなかった（2回目の signal が効かない）")
	}
}

// TestRun_起動時検査が期限内に終わらなければ起動を止める は、外向きの呼び出しの期限を確かめる。
//
// 目的: 起動時検査（設計 3-6）は `gh` を起動し、herdr の socket を叩き、GitHub の GraphQL を
// 読む。**期限が1つも無いと、応答を返さない相手に当たった時点で起動が無言で止まる**
// （復元にも巡回にも進まない）。
//
// 与える情報: 応答を返さないテスト用GraphQL mock（127.0.0.1）と、200ms の起動時検査の期限。
// gh と herdr は答える（止まるのはボードの読み取りだけ）。
//
// 成功条件: 30 秒以内に起動の段のエラーで返り、文言が起動時検査の失敗を指すこと。
func TestRun_起動時検査が期限内に終わらなければ起動を止める(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	// **テスト用gh / ghq mock を PATH の先頭に置く。**本物の認証情報を読ませない。
	writeFakeGH(t, binDir, root)
	// **テスト用herdr mock は ping に答える。**止まるのはボードの読み取りだけにする。
	newFakeHerdr(t, root, &timeline{})

	// **応答を1バイトも返さないサーバ。**
	blocked := make(chan struct{})
	github := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
	}))
	// **後始末は「待たせるのをやめる → サーバを閉じる」の順に走らせる**（t.Cleanup は
	// 後に登録したものから走る）。逆順だと、応答を待たせたまま Close が処理中の
	// リクエストの終わりを待ち、テストが終われなくなる。
	t.Cleanup(github.Close)
	t.Cleanup(func() { close(blocked) })

	path := writeWiringWorkflow(t, root,
		"    token_source: env\n    token_env: CONTINUO_TEST_TOKEN\n", "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, github.URL)
	t.Setenv("CONTINUO_TEST_TOKEN", "dummy-token-for-the-fake-server")

	start := time.Now()
	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath:          path,
		Logger:              slog.New(slog.DiscardHandler),
		StartupCheckTimeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ボードが応答しないのに起動できてしまった")
	}
	if elapsed > 30*time.Second {
		t.Fatalf("期限を過ぎても待ち続けた: %v", elapsed)
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "起動時の検査に落ちました") {
		t.Fatalf("起動時の検査で止まったことが文言に出ていない: %v", err)
	}
}

// TestHookCLI_socketとpending_dirが相対パスなら受け付けない は、
// hook の逃がし先が worktree の中に掘られるのを防ぐ。
//
// 目的: hook の cwd は worktree である（設計 1-5）。相対パスを受けると、逃がし先が
// worktree の中に作られ、continuo は実行時ディレクトリの下しか走査しないので
// **その hook は永久に読まれない**（worktree も汚れる）。受け口の側
// （`hookserver.New`）は同じ理由で既に絶対パスを要求している。
//
// 与える情報: `--socket` か `--pending-dir` の片方だけを相対パスにした `continuo hook`。
//
// 成功条件: 終了コードが 1 で、文言が絶対パスを求めていること。**2 を返さないこと**
// （Claude Code は hook の 2 を「その操作を止めろ」と解釈するため）。
// 相対パスのディレクトリを1つも作らないこと。
func TestHookCLI_socketとpending_dirが相対パスなら受け付けない(t *testing.T) {
	root := wiringRoot(t)
	bin := buildBinary(t, root)

	cases := []struct {
		name       string
		socket     string
		pendingDir string
	}{
		{
			name:       "socket が相対パス",
			socket:     "hooks.sock",
			pendingDir: filepath.Join(root, "pending"),
		},
		{
			name:       "pending_dir が相対パス",
			socket:     filepath.Join(root, "hooks.sock"),
			pendingDir: "pending",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "hook", "--socket="+tc.socket, "--pending-dir="+tc.pendingDir)
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(`{"hook_event_name":"Stop","session_id":"s","cwd":"` + root + `"}` + "\n")
			out, err := cmd.CombinedOutput()

			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("continuo hook を実行できません: %v\n%s", err, out)
				}
				code = exitErr.ExitCode()
			}
			if code != 1 {
				t.Fatalf("終了コードが 1 ではない: got %d\n%s", code, out)
			}
			if !strings.Contains(string(out), "絶対パス") {
				t.Fatalf("絶対パスを求める文言が出ていない:\n%s", out)
			}
			if _, statErr := os.Stat(filepath.Join(root, "pending")); statErr == nil && !filepath.IsAbs(tc.pendingDir) {
				t.Fatal("相対パスの逃がし先を掘ってしまった（worktree の中に作られる）")
			}
		})
	}
}
