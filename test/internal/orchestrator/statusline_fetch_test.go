package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// statusline取得（issue #284。internal/orchestrator/statuslinefetch.go）を、テスト用herdr mock
// （socket）と本物の git で確かめる。
//
// **statusline取得**は、使用率を受け取るために、利用者が既に信頼している clone の中で短い haiku の
// Claude Code を起動し、`hello` を1回送り、ステータスラインが運ぶ使用率を受け取って閉じることである。
// 長く待つ経路（3分の値待ち・全体の上限）は statusline_fetch_sync_test.go が偽の時計で確かめる。
//
// **Claude Code は起動しない。**テスト用herdr mock の `sl-` で始まる agent の台本が、
// hello を受けたときにステータスラインの行を OnStatusline で直に入れる。

// slFetchLabel は statusline取得の workspace に貼る label である（herdr.StatuslineFetchLabel と同じ値）。
const slFetchLabel = herdr.StatuslineFetchLabel

// newFetchFixture は、statusline取得を走らせられる fixture を作る。
//
// **`trust.repositories` に fixture のリポジトリ（octocat/hello-world）を1つ書く。**
// fixture の `~/.claude.json` はそのリポジトリを信頼済みと記録している。
//
// t: 呼び出し元のテスト。
// mutate: 設定を書き換える関数。nil なら既定のまま。
// extra: `ghq list -p -e` の偽物の差し替え（fixtureOptions.GhqExtra）。
// 戻り値: 組み立てた fixture。
func newFetchFixture(t *testing.T, mutate func(cfg *config.Config), extra map[string]string) *fixture {
	t.Helper()
	return newFixture(t, fixtureOptions{
		GhqExtra: extra,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			cfg.Trust.Repositories = []string{"octocat/hello-world"}
			if mutate != nil {
				mutate(cfg)
			}
		},
	})
}

// deliverValueOnPrompt は、statusline取得の hello を受けたら、そのセッションから
// rate_limits を持つ新しい応答の行を送る台本を入れる。
func deliverValueOnPrompt(fx *fixture) {
	fx.Herdr.HandleSL(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		ids := fx.Herdr.SLSessionIDs()
		if len(ids) > 0 {
			now := time.Now()
			fx.Orc.OnStatusline(slLine(ids[len(ids)-1], 10728, slWin(3, now.Add(2*time.Hour)), slWin(34, now.Add(72*time.Hour))))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
}

// runFetchOnce は巡回を1回回して statusline取得を起こし、終わるまで待つ。
func runFetchOnce(t *testing.T, fx *fixture) {
	t.Helper()
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "statusline取得が終わる", func() bool {
		return !fx.Orc.StatuslineFetchRunningForTest()
	})
}

// leftoverList は閉じ残しの一覧を読む。無ければ nil。
func leftoverList(t *testing.T, fx *fixture) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fx.RuntimeDir, "statusline-fetch", "workspaces.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("閉じ残しの一覧を読めません: %v", err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatalf("閉じ残しの一覧を読めません: %v\n%s", err, raw)
	}
	return ids
}

// writeLeftoverList は閉じ残しの一覧を書く。
func writeLeftoverList(t *testing.T, fx *fixture, ids []string) {
	t.Helper()
	dir := filepath.Join(fx.RuntimeDir, "statusline-fetch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("statusline取得の作業ディレクトリを作れません: %v", err)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		t.Fatalf("閉じ残しの一覧を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces.json"), raw, 0o600); err != nil {
		t.Fatalf("閉じ残しの一覧を書けません: %v", err)
	}
}

// createdSLWorkspace は、テスト用herdr mock が statusline取得のために作った workspace の ID を返す。
func createdSLWorkspace(fx *fixture) string {
	for id, ws := range fx.Herdr.OpenWorkspaces() {
		if ws.Label == slFetchLabel {
			return id
		}
	}
	return ""
}

// logCount は fixture のログのうち substr を含む行の数を返す。
func logCount(fx *fixture, substr string) int {
	n := 0
	for _, line := range strings.Split(fx.Logs.String(), "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// TestStatuslineFetch_信頼済みのcloneでhaikuを起動しhelloを送って値を受け閉じる は、statusline取得の
// 成功の経路を確かめる。
//
// 目的: 計画の「statusline取得」の表のとおりに herdr を呼ぶこと。
//   - workspace.create は cwd が選んだ clone、label が `continuo statusline fetch`、focus が偽
//   - agent の名前は `sl-` と UUID の先頭12桁の16進
//   - 起動の引数は `--settings`・`--model haiku`・`--permission-mode dontAsk`・`--restricted`・
//     `--strict-mcp-config`・`--system-prompt Reply with one word.`・`--tools`（空）・
//     `--disable-slash-commands`・`--session-id`
//   - 送る文は `hello`
//   - statusline取得用の設定ファイルは `statusLine` と `env` だけを持ち、`env` は `claude.env` から
//     `CLAUDE_CODE_RETRY_WATCHDOG` を除いて `CLAUDE_CODE_SKIP_PROMPT_HISTORY=1` を足したもの。0600
//
// 与える情報: `claude.env` に架空のプロキシと `CLAUDE_CODE_RETRY_WATCHDOG` を書いた設定。
// hello を受けたら値を送る Claude Code。
// 成功条件: 上の全部が成り立ち、workspace を閉じて閉じ残しの一覧が空になり、入札が読めるように
// なり、巡回のループへの知らせが1つ置かれること。WARN が出ないこと（fixture が検査する）。
func TestStatuslineFetch_信頼済みのcloneでhaikuを起動しhelloを送って値を受け閉じる(t *testing.T) {
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.Claude.Env = map[string]string{
			"HTTPS_PROXY":                "http://proxy.example.com:8080",
			"CLAUDE_CODE_RETRY_WATCHDOG": "1",
		}
	}, nil)
	deliverValueOnPrompt(fx)

	runFetchOnce(t, fx)

	create := fx.Herdr.ParamsOf(t, herdr.MethodWorkspaceCreate)
	if create["cwd"] != fx.Repo.Dir || create["label"] != slFetchLabel || create["focus"] != false {
		t.Errorf("workspace.create の params が違う: %v", create)
	}

	var start map[string]any
	for _, r := range fx.Herdr.SLRequests() {
		if r.Method == herdr.MethodAgentStart {
			start = r.Params
		}
	}
	if start == nil {
		t.Fatalf("statusline取得の agent.start を受けていない: %v", fx.Herdr.SLRequests())
	}
	name, _ := start["name"].(string)
	if !regexp.MustCompile(`^sl-[0-9a-f]{12}$`).MatchString(name) {
		t.Errorf("agent の名前が sl- と12桁の16進ではない: %q", name)
	}
	uuid := argAfter(start, "--session-id")
	if hex := strings.ReplaceAll(uuid, "-", ""); len(hex) < 12 || name != "sl-"+hex[:12] {
		t.Errorf("agent の名前が UUID %q の先頭12桁ではない: %q", uuid, name)
	}
	settingsPath := filepath.Join(fx.RuntimeDir, "statusline-fetch", "settings.json")
	wantArgs := []any{
		"--settings", settingsPath,
		"--model", "haiku",
		"--permission-mode", "dontAsk",
		"--restricted",
		"--strict-mcp-config",
		"--system-prompt", "Reply with one word.",
		"--tools", "",
		"--disable-slash-commands",
		"--session-id", uuid,
	}
	if got, _ := start["args"].([]any); !slices.Equal(got, wantArgs) {
		t.Errorf("起動の引数が違う:\n got  %q\n want %q", got, wantArgs)
	}
	if start["kind"] != fx.Config.Claude.Kind {
		t.Errorf("agent の kind が %v（want %s）", start["kind"], fx.Config.Claude.Kind)
	}

	var prompts []string
	for _, r := range fx.Herdr.SLRequests() {
		if r.Method == herdr.MethodAgentPrompt {
			prompts = append(prompts, r.Params["text"].(string))
		}
	}
	if !slices.Equal(prompts, []string{"hello"}) {
		t.Errorf("送った文が hello の1回ではない: %q", prompts)
	}

	info, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("statusline取得用の設定ファイルが無い: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("statusline取得用の設定ファイルの権限が %o（want 600）", perm)
	}
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("statusline取得用の設定ファイルを読めません: %v\n%s", err, raw)
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"env", "statusLine"}) {
		t.Errorf("statusline取得用の設定ファイルが statusLine と env 以外を持つ: %v", keys)
	}
	var env map[string]string
	_ = json.Unmarshal(settings["env"], &env)
	wantEnv := map[string]string{
		"HTTPS_PROXY":                     "http://proxy.example.com:8080",
		"CLAUDE_CODE_SKIP_PROMPT_HISTORY": "1",
	}
	if len(env) != len(wantEnv) || env["HTTPS_PROXY"] != wantEnv["HTTPS_PROXY"] ||
		env["CLAUDE_CODE_SKIP_PROMPT_HISTORY"] != "1" {
		t.Errorf("env が違う: %v（want %v）", env, wantEnv)
	}
	var sl struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	_ = json.Unmarshal(settings["statusLine"], &sl)
	wantCmd := `'/opt/continuo/bin/continuo' statusline --socket '` + fx.StatuslineSocketPath + `'`
	if sl.Type != "command" || sl.Command != wantCmd {
		t.Errorf("statusLine が違う: %+v（want command %q）", sl, wantCmd)
	}

	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceClose); got != 1 {
		t.Errorf("workspace.close が %d 回（want 1）", got)
	}
	if id := createdSLWorkspace(fx); id != "" {
		t.Errorf("statusline取得の workspace %s が開いたまま", id)
	}
	if got := leftoverList(t, fx); len(got) != 0 {
		t.Errorf("閉じたのに閉じ残しの一覧に残っている: %v", got)
	}
	if fx.Orc.QuotaForBidForTest() == nil {
		t.Error("値が届いたのに入札が読めない")
	}
	if got := fx.Orc.PendingStatuslineNotifyForTest(); got != 1 {
		t.Errorf("巡回のループへの知らせが %d（want 1）", got)
	}
}

// TestStatuslineFetch_確認の画面で止まったらhelloを送らずWARNを出して閉じる は、blocked の経路を確かめる。
//
// 目的: 確認の画面（信頼の確認など）で止まった Claude Code に hello を送らない。選んだ clone を
// Claude Code が信頼済みと見なしていないことを案内する。
// 与える情報: agent.get が blocked を返す Claude Code。
// 成功条件: hello を送らず、「確認の画面で止まった」の WARN が1行出て、workspace を閉じること。
func TestStatuslineFetch_確認の画面で止まったらhelloを送らずWARNを出して閉じる(t *testing.T) {
	fx := newFetchFixture(t, nil, nil)
	fx.AllowLog("確認の画面で止まった")
	fx.Herdr.HandleSL(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "blocked", "interactive_ready": false},
		}, nil
	})

	runFetchOnce(t, fx)

	if got := fx.Herdr.CountSL(herdr.MethodAgentPrompt); got != 0 {
		t.Errorf("確認の画面で止まっているのに hello を %d 回送った", got)
	}
	if got := logCount(fx, "確認の画面で止まった"); got != 1 {
		t.Errorf("「確認の画面で止まった」の WARN が %d 行（want 1）", got)
	}
	if id := createdSLWorkspace(fx); id != "" {
		t.Errorf("statusline取得の workspace %s が開いたまま", id)
	}
}

// TestStatuslineFetch_idleでもdoneでも入力を受け付けるならhelloを送る は、送ってよい状態を確かめる。
//
// 目的: `idle` か `done`（internal/orchestrator/dispatch.go と同じ判定）で、かつ
// `interactive_ready` なら hello を送る。
// 与える情報: agent.get が idle / done で interactive_ready を返す Claude Code。
// 成功条件: どちらでも hello を1回送り、値を受けること。
func TestStatuslineFetch_idleでもdoneでも入力を受け付けるならhelloを送る(t *testing.T) {
	for _, status := range []string{"idle", "done"} {
		t.Run(status, func(t *testing.T) {
			fx := newFetchFixture(t, nil, nil)
			deliverValueOnPrompt(fx)
			fx.Herdr.HandleSL(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
				return map[string]any{
					"type":  "agent_info",
					"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": true},
				}, nil
			})

			runFetchOnce(t, fx)

			if got := fx.Herdr.CountSL(herdr.MethodAgentPrompt); got != 1 {
				t.Errorf("%s で入力を受け付けるのに hello を %d 回送った（want 1）", status, got)
			}
			if fx.Orc.QuotaForBidForTest() == nil {
				t.Error("値を受けていない")
			}
		})
	}
}

// TestStatuslineFetch_trust_repositoriesを上から見てcloneが無いものと信頼されていないものを飛ばす は、
// clone の選び方を確かめる。
//
// 目的: 起動時に読んだ `trust.repositories` を上から見て、手元に clone があり、Claude Code に
// 信頼されている最初の1つを選ぶ（issue の run と同じ判定）。
// 与える情報: `trust.repositories` の1つ目は clone が無い、2つ目は clone があるが信頼されていない、
// 3つ目は信頼済み。
// 成功条件: workspace.create の cwd が3つ目の clone であること。
func TestStatuslineFetch_trust_repositoriesを上から見てcloneが無いものと信頼されていないものを飛ばす(t *testing.T) {
	other := newTestRepo(t)
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.Trust.Repositories = []string{"octocat/missing", "octocat/untrusted", "octocat/hello-world"}
	}, map[string]string{"octocat/missing": "", "octocat/untrusted": other.Dir})
	deliverValueOnPrompt(fx)

	runFetchOnce(t, fx)

	if got := fx.Herdr.ParamsOf(t, herdr.MethodWorkspaceCreate)["cwd"]; got != fx.Repo.Dir {
		t.Errorf("statusline取得の cwd が %v（want 信頼済みの %s）", got, fx.Repo.Dir)
	}
}

// TestStatuslineFetch_使えるcloneが無ければworkspaceを作らずWARNを出す は、「使える clone が無い」を確かめる。
//
// 目的: 信頼済みの clone が1つも無ければ、workspace を作らずに、`trust.repositories` に書いて
// `continuo trust` を叩き立て直すよう案内する。
// 与える情報: clone の無いリポジトリだけを書いた `trust.repositories`。
// 成功条件: workspace.create を呼ばず、「使える clone が無い」の WARN が1行出ること。
func TestStatuslineFetch_使えるcloneが無ければworkspaceを作らずWARNを出す(t *testing.T) {
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.Trust.Repositories = []string{"octocat/missing"}
	}, map[string]string{"octocat/missing": ""})
	fx.AllowLog("使える clone が無い")

	runFetchOnce(t, fx)

	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 0 {
		t.Errorf("使える clone が無いのに workspace.create を %d 回呼んだ", got)
	}
	if got := logCount(fx, "使える clone が無い"); got != 1 {
		t.Errorf("「使える clone が無い」の WARN が %d 行（want 1）", got)
	}
}

// TestStatuslineFetch_workspaceを作れなければ途中の誤りとしてWARNを出す は、herdr を呼べなかった経路を確かめる。
//
// 目的: workspace.create の誤りは「途中の誤り」として文面を添えて WARN を出す。
// 作れなかった workspace を閉じ残しの一覧に足さない。
// 与える情報: workspace.create に誤りを返す herdr。
// 成功条件: 「途中の誤り」の WARN が1行出て、Claude Code を起動せず、閉じ残しの一覧が空であること。
func TestStatuslineFetch_workspaceを作れなければ途中の誤りとしてWARNを出す(t *testing.T) {
	fx := newFetchFixture(t, nil, nil)
	fx.AllowLog("途中の誤り")
	fx.Herdr.Handle(herdr.MethodWorkspaceCreate, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "作れません（テスト用）"}
	})

	runFetchOnce(t, fx)

	if got := logCount(fx, "途中の誤り"); got != 1 {
		t.Errorf("「途中の誤り」の WARN が %d 行（want 1）", got)
	}
	if got := fx.Herdr.CountSL(herdr.MethodAgentStart); got != 0 {
		t.Errorf("workspace を作れなかったのに Claude Code を %d 回起動した", got)
	}
	if got := leftoverList(t, fx); len(got) != 0 {
		t.Errorf("作れなかった workspace を閉じ残しの一覧に足した: %v", got)
	}
}

// TestStatuslineFetch_作る応答が届かなくてもherdrが作ったworkspaceを閉じ残しに足す は、
// 作る呼び出しが誤りで返っても herdr が作っていた workspace を、閉じ残しの一覧へ足すことを確かめる。
//
// 目的: 作る呼び出しの応答だけが期限を過ぎたときなど、herdr には statusline取得の workspace が
// できているのに ID が届かないことがある。**一覧へ足さないと ID を知る者が居なくなり、誰も閉じない。**
// 与える情報: workspace.create を受けたら、statusline取得の label の workspace（w88）を herdr の一覧へ
// 足したうえで誤りを返す herdr。
// 成功条件: 「途中の誤り」の WARN が1行出て、Claude Code を起動せず、閉じ残しの一覧が [w88] であること。
func TestStatuslineFetch_作る応答が届かなくてもherdrが作ったworkspaceを閉じ残しに足す(t *testing.T) {
	fx := newFetchFixture(t, nil, nil)
	fx.AllowLog("途中の誤り")
	fx.Herdr.Handle(herdr.MethodWorkspaceCreate, func(params map[string]any) (any, *rpcErr) {
		cwd, _ := params["cwd"].(string)
		fx.Herdr.AddWorkspace("w88", fakeWorkspace{
			Checkout: cwd, RepoRoot: cwd, Label: herdr.StatuslineFetchLabel, Created: true,
		})
		return nil, &rpcErr{Code: "internal", Message: "応答が届かなかった（テスト用）"}
	})

	runFetchOnce(t, fx)

	if got := logCount(fx, "途中の誤り"); got != 1 {
		t.Errorf("「途中の誤り」の WARN が %d 行（want 1）", got)
	}
	if got := fx.Herdr.CountSL(herdr.MethodAgentStart); got != 0 {
		t.Errorf("workspace を作れなかったのに Claude Code を %d 回起動した", got)
	}
	if got := leftoverList(t, fx); !slices.Equal(got, []string{"w88"}) {
		t.Errorf("閉じ残しの一覧 = %v, want [w88]", got)
	}
}

// TestStatuslineFetch_設定ファイルを書けなければworkspaceを作らずWARNを出す は、ファイルを書けなかった経路を確かめる。
//
// 目的: statusline取得用の設定ファイルを書けなければ、その試行をやめる（途中の誤り）。
// 与える情報: 実行時ディレクトリの statusline-fetch の場所に置いた通常のファイル（ディレクトリを作れない）。
// 成功条件: workspace.create を呼ばず、「途中の誤り」の WARN が1行出ること。
func TestStatuslineFetch_設定ファイルを書けなければworkspaceを作らずWARNを出す(t *testing.T) {
	fx := newFetchFixture(t, nil, nil)
	// **閉じ残しの一覧も同じディレクトリに置くので、先に「一覧を読めない」の WARN も出る。**
	// どちらもこのテストが置いた邪魔なファイルが起こしている。
	fx.AllowLog("途中の誤り", "閉じ残しの statusline取得の一覧を読めません")
	if err := os.WriteFile(filepath.Join(fx.RuntimeDir, "statusline-fetch"), []byte("x"), 0o600); err != nil {
		t.Fatalf("邪魔なファイルを置けません: %v", err)
	}

	runFetchOnce(t, fx)

	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 0 {
		t.Errorf("設定ファイルを書けないのに workspace.create を %d 回呼んだ", got)
	}
	if got := logCount(fx, "途中の誤り"); got != 1 {
		t.Errorf("「途中の誤り」の WARN が %d 行（want 1）", got)
	}
}

// TestStatuslineFetch_走っている間は次を開かず終わってから開く は、「走っている」の範囲を確かめる。
//
// 目的: 試行は、閉じる仕事の Do が返り goroutine が終わるまで「走っている」。その間に次の試行を
// 開くと、前の試行の閉じる仕事が次の試行の押さえを放す順が作れる。
// 与える情報: `refresh_interval_ms` を1ミリ秒にした設定。agent.get が working を返し続ける
// Claude Code（起動の期限 `herdr.startup_timeout_ms` = 2秒で「起動しなかった」になる）。
// 成功条件: 走っている間の巡回では workspace.create が増えず、終わったあとの巡回で2回目を開くこと。
func TestStatuslineFetch_走っている間は次を開かず終わってから開く(t *testing.T) {
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.RateLimit.RefreshIntervalMs = 1
	}, nil)
	fx.AllowLog("起動しなかった")
	fx.Herdr.HandleSL(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "working", "interactive_ready": false},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "statusline取得の workspace を作る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate) == 1
	})
	time.Sleep(100 * time.Millisecond)
	fx.Orc.Tick(context.Background())
	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 1 {
		t.Fatalf("走っている間に次の statusline取得を開いた: workspace.create %d 回", got)
	}
	waitFor(t, 20*time.Second, "1回目が終わる", func() bool {
		return !fx.Orc.StatuslineFetchRunningForTest()
	})
	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 1 {
		t.Fatalf("走っている間の巡回が次を開いていた: workspace.create %d 回", got)
	}

	runFetchOnce(t, fx)
	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 2 {
		t.Errorf("終わったあとの巡回で次を開いていない: workspace.create %d 回（want 2）", got)
	}
}

// TestStatuslineFetch_閉じ残しの一覧はlabelの合うものだけ閉じる は、閉じ残しの片付けを確かめる。
//
// 目的: 閉じ残しの一覧の ID は、herdr の一覧に在って label が `continuo statusline fetch` のとき
// だけ閉じる（ID が別の workspace に使い回されていたら閉じない）。親にされて子が居るものは
// 閉じずに一覧に残す。herdr の一覧に無いものと label の合わないものは一覧から外す。
// 起動時検査のあと・復元の前に呼ぶ PrepareStatusline で片付ける。
// 与える情報: 閉じ残しの一覧に4つの ID。label の合う作ったままの workspace（wa）、label の違う
// workspace（wb）、herdr に無い ID（wc）、label が合い親にされて子の居る workspace（wd）。
// 成功条件: wa だけを閉じ、一覧が wd だけになること。親にされた旨の WARN が出ること。
func TestStatuslineFetch_閉じ残しの一覧はlabelの合うものだけ閉じる(t *testing.T) {
	fx := newFetchFixture(t, nil, nil)
	fx.AllowLog("親にされたので")
	repo := fx.Repo.Dir
	fx.Herdr.AddWorkspace("wa", fakeWorkspace{Checkout: repo, RepoRoot: repo, Label: slFetchLabel, Created: true})
	fx.Herdr.AddWorkspace("wb", fakeWorkspace{Checkout: "/tmp/elsewhere", RepoRoot: "/tmp/elsewhere", Label: "octocat/hello-world/issues/1"})
	fx.Herdr.AddWorkspace("wd", fakeWorkspace{Checkout: repo, RepoRoot: repo, Label: slFetchLabel})
	fx.Herdr.AddWorkspace("we", fakeWorkspace{Checkout: repo + "-issue-7", RepoRoot: repo, Label: "octocat/hello-world/issues/7"})
	writeLeftoverList(t, fx, []string{"wa", "wb", "wc", "wd"})

	fx.Orc.PrepareStatusline(context.Background())

	var closed []string
	for _, r := range fx.Herdr.Requests() {
		if r.Method == herdr.MethodWorkspaceClose {
			closed = append(closed, r.Params["workspace_id"].(string))
		}
	}
	if !slices.Equal(closed, []string{"wa"}) {
		t.Errorf("閉じた workspace が %v（want [wa]）", closed)
	}
	if got := leftoverList(t, fx); !slices.Equal(got, []string{"wd"}) {
		t.Errorf("閉じ残しの一覧が %v（want [wd]）", got)
	}
	if logCount(fx, "親にされたので") == 0 {
		t.Error("親にされて閉じなかったことを WARN で知らせていない")
	}
}

// TestStatuslineFetch_閉じ残しの一覧を引けなければ一覧を残し一覧が無ければ何もしない は、
// 閉じ残しの片付けの失敗と空の場合を確かめる。
//
// 目的: herdr の一覧を引けないときは、閉じてよいか分からないので一覧を残して次に回す。
// 一覧が無ければ herdr を呼ばない。
// 与える情報: サブテスト1は workspace.list に誤りを返す herdr と、ID を1つ持つ一覧。
// サブテスト2は一覧の無い実行時ディレクトリ。
// 成功条件: 1は一覧がそのまま残ること。2は workspace.list を1回も呼ばないこと。
func TestStatuslineFetch_閉じ残しの一覧を引けなければ一覧を残し一覧が無ければ何もしない(t *testing.T) {
	t.Run("一覧を引けない", func(t *testing.T) {
		fx := newFetchFixture(t, nil, nil)
		fx.AllowLog("一覧を引けない")
		fx.Herdr.Handle(herdr.MethodWorkspaceList, func(map[string]any) (any, *rpcErr) {
			return nil, &rpcErr{Code: "internal", Message: "引けません（テスト用）"}
		})
		writeLeftoverList(t, fx, []string{"wa"})

		fx.Orc.PrepareStatusline(context.Background())

		if got := leftoverList(t, fx); !slices.Equal(got, []string{"wa"}) {
			t.Errorf("一覧を引けないのに閉じ残しの一覧を変えた: %v", got)
		}
	})
	t.Run("一覧が無い", func(t *testing.T) {
		fx := newFetchFixture(t, nil, nil)

		fx.Orc.PrepareStatusline(context.Background())

		if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceList); got != 0 {
			t.Errorf("閉じ残しの一覧が無いのに workspace.list を %d 回呼んだ", got)
		}
	})
}

// TestStatuslineFetch_source_noneでも閉じ残しは片付けるがstatusline取得は開かずquota_jsonも読まない は、
// `rate_limit.source: none` の起動時の準備と巡回を確かめる。
//
// 目的: source を statusline から none へ替えて立て直した機械にも、前回の閉じ残しが残りうる。
// **片付けは source によらず行う。**それ以外（quota.json・statusline取得）は行わない。
// 与える情報: `rate_limit.source: none`・閉じ残しの一覧に label の合う workspace が1つ・
// 100% を書いた quota.json・信頼済みの clone を書いた `trust.repositories`。
// 成功条件: 閉じ残しを閉じ、保管値が空で、巡回のあとも workspace.create を呼ばないこと。
func TestStatuslineFetch_source_noneでも閉じ残しは片付けるがstatusline取得は開かずquota_jsonも読まない(t *testing.T) {
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.RateLimit.Source = ratelimit.SourceNone
	}, nil)
	repo := fx.Repo.Dir
	fx.Herdr.AddWorkspace("wa", fakeWorkspace{Checkout: repo, RepoRoot: repo, Label: slFetchLabel, Created: true})
	writeLeftoverList(t, fx, []string{"wa"})
	quota := `{"session":{"percent":100,"resets_at":"` + time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339) + `"}}`
	if err := os.WriteFile(filepath.Join(fx.RuntimeDir, "quota.json"), []byte(quota), 0o600); err != nil {
		t.Fatalf("quota.json を書けません: %v", err)
	}

	fx.Orc.PrepareStatusline(context.Background())
	fx.Orc.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceClose); got != 1 {
		t.Errorf("source が none でも閉じ残しを片付けるはずが、workspace.close が %d 回", got)
	}
	if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
		t.Errorf("source が none なのに quota.json を読んだ: %+v", snap)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate); got != 0 {
		t.Errorf("source が none なのに statusline取得を開いた: workspace.create %d 回", got)
	}
}

// TestStatuslineFetch_親にされて子が居たら閉じずに残し子が居なくなれば次の片付けで閉じる は、
// statusline取得の workspace が issue の親にされたときを確かめる。
//
// 目的: statusline取得の workspace が開いている clone で、別のプロセス（`--id` を分けた
// 2つ目の continuo・`continuo abandon`）が `worktree.open` すると、herdr はそれを issue の親にする。
// **親を閉じると herdr 0.8.x では下の issue の pane まで消え、0.9.x では断られる。**
// そこで子が居る間は閉じず、WARN を出して閉じ残しの一覧に残す。子が居なくなれば閉じる。
// `worktree` 欄を持っていても子が居なければ閉じる。
// 与える情報: hello を受けたときに、作った workspace を親にし（サブテスト1は子の workspace も足す）、
// そのあと値を送る Claude Code。
// 成功条件: 子が居るときは閉じず、WARN を出し、一覧に残す。子を消して PrepareStatusline を呼ぶと
// 閉じて一覧が空になる。子が居ないときは最初から閉じる。
func TestStatuslineFetch_親にされて子が居たら閉じずに残し子が居なくなれば次の片付けで閉じる(t *testing.T) {
	adopt := func(fx *fixture, withChild bool) {
		fx.Herdr.HandleSL(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
			id := createdSLWorkspace(fx)
			repo := fx.Repo.Dir
			// **親にされる。**本物の herdr は `worktree` 欄を付け直す（Created を外す）。
			fx.Herdr.AddWorkspace(id, fakeWorkspace{Checkout: repo, RepoRoot: repo, Label: slFetchLabel})
			if withChild {
				fx.Herdr.AddWorkspace("child", fakeWorkspace{Checkout: repo + "-issue-7", RepoRoot: repo, Label: "octocat/hello-world/issues/7"})
			}
			ids := fx.Herdr.SLSessionIDs()
			fx.Orc.OnStatusline(slLine(ids[len(ids)-1], 500, slWin(3, time.Now().Add(2*time.Hour)), nil))
			return map[string]any{"type": "agent_prompted", "agent": map[string]any{"name": params["target"]}}, nil
		})
	}

	t.Run("子が居る", func(t *testing.T) {
		fx := newFetchFixture(t, nil, nil)
		fx.AllowLog("親にされたので")
		adopt(fx, true)

		runFetchOnce(t, fx)

		id := createdSLWorkspace(fx)
		if id == "" {
			t.Fatal("子が居るのに親を閉じた（issue の pane まで消える）")
		}
		if logCount(fx, "親にされたので") != 1 {
			t.Errorf("親にされた旨の WARN が1行ではない:\n%s", fx.Logs.String())
		}
		if got := leftoverList(t, fx); !slices.Equal(got, []string{id}) {
			t.Errorf("閉じ残しの一覧が %v（want [%s]）", got, id)
		}

		// **子の issue の workspace が閉じた。**次の片付けで閉じる。
		fx.Herdr.forgetWorkspace("child")
		fx.Orc.PrepareStatusline(context.Background())
		if got := createdSLWorkspace(fx); got != "" {
			t.Errorf("子が居なくなったのに閉じていない: %s", got)
		}
		if got := leftoverList(t, fx); len(got) != 0 {
			t.Errorf("閉じたのに閉じ残しの一覧に残っている: %v", got)
		}
	})
	t.Run("子が居ない", func(t *testing.T) {
		fx := newFetchFixture(t, nil, nil)
		adopt(fx, false)

		runFetchOnce(t, fx)

		if got := createdSLWorkspace(fx); got != "" {
			t.Errorf("worktree 欄を持つが子の居ない workspace を閉じていない: %s", got)
		}
		if got := leftoverList(t, fx); len(got) != 0 {
			t.Errorf("閉じたのに閉じ残しの一覧に残っている: %v", got)
		}
	})
}

// TestStatuslineFetch_開いている間は同じcloneの着手がworktree_openを待ち取得はスロットを数えない は、
// herdr の workspace の開け閉めの順と、statusline取得がスロットを使わないことを確かめる。
//
// 目的: statusline取得の workspace が開いている clone で issue の worktree を開くと、herdr はその
// workspace を issue の親にしてしまう。**着手の段7 は、閉じるまで `worktree.open` を呼ばない。**
// また、statusline取得は `agent.max_concurrent_agents` に数えない。
// 与える情報: `agent.max_concurrent_agents` が1。statusline取得の Claude Code は、関門が開くまで
// working を返し、開いたら入力を受け付けて hello に値を返す。statusline取得を始めたあとで、
// 同じ clone の `Ready` の issue を1件足して巡回する（値を新しくしてから。入札させるため）。
// 成功条件: statusline取得の間に issue の run が1件走り出す（スロットを使われていない）。
// 関門を開くまで `worktree.open` が呼ばれず、statusline取得の workspace.close のあとに呼ばれること。
func TestStatuslineFetch_開いている間は同じcloneの着手がworktree_openを待ち取得はスロットを数えない(t *testing.T) {
	fx := newFetchFixture(t, func(cfg *config.Config) {
		cfg.Agent.MaxConcurrentAgents = 1
	}, nil)
	holdPrompt(fx)
	gate := make(chan struct{})
	fx.Herdr.HandleSL(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		status, ready := "working", false
		select {
		case <-gate:
			status, ready = "idle", true
		default:
		}
		return map[string]any{
			"type":  "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": status, "interactive_ready": ready},
		}, nil
	})
	deliverValueOnPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "statusline取得の workspace を作る", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodWorkspaceCreate) == 1
	})

	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 10, 20)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "issue の run が走り出す（statusline取得はスロットを数えない）", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})
	time.Sleep(500 * time.Millisecond)
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Fatalf("statusline取得の workspace が開いている clone で worktree.open を %d 回呼んだ（親にされる）", got)
	}

	close(gate)
	waitFor(t, 20*time.Second, "閉じたあとに worktree.open を呼ぶ", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodWorktreeOpen) > 0
	})
	closeIdx := fx.Timeline.IndexOf("herdr." + herdr.MethodWorkspaceClose)
	openIdx := fx.Timeline.IndexOf("herdr." + herdr.MethodWorktreeOpen)
	if closeIdx < 0 || openIdx < closeIdx {
		t.Errorf("statusline取得の workspace.close（%d）より先に worktree.open（%d）を呼んだ: %v",
			closeIdx, openIdx, fx.Timeline.Entries())
	}
}

// TestSettings_statusLineはsource_statuslineのときだけ入りhookの部分は変わらない は、issue ごとの
// 設定ファイルを確かめる（hook の規則。CLAUDE.md の「hook の挙動が変化する変更」に当たらないこと）。
//
// 目的: `source` が `none` でなく statusline を使えるときだけ `statusLine` として
// `continuo statusline --socket <sl.sock>` を書き、`source: none` と、`oauth_usage_api` で
// statusline を使えない（DisableStatusline）ときは書かない（利用者のステータスラインがそのまま出る）。
// **hook の部分（コマンド行と張る hook の種類）は source によらず同じである。**
// 与える情報: source が statusline・oauth_usage_api・oauth_usage_api で DisableStatusline・none の
// 4つの fixture で、同じ issue を着手させる。
// 成功条件: statusline と oauth_usage_api の設定ファイルにだけ statusLine があり、コマンド行が
// `'<continuo>' statusline --socket '<sl.sock>'` であること。hooks が、実行時ディレクトリの
// パスを伏せると一致すること。
func TestSettings_statusLineはsource_statuslineのときだけ入りhookの部分は変わらない(t *testing.T) {
	read := func(t *testing.T, source string, disable bool) (*fixture, map[string]json.RawMessage) {
		fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = source
			if source == ratelimit.SourceOAuthUsageAPI {
				// **起動時の WARN を出さない**（refresh_interval_ms を polling.interval_ms より長くする）。
				cfg.Polling.IntervalMs = 30000
			}
		}})
		if disable {
			fx.Orc.DisableStatusline()
		}
		holdPrompt(fx)
		if source != ratelimit.SourceNone {
			// **値を新しくしてから着手させる**（入札させ、statusline取得を開かせない）。
			feedFreshQuota(fx.Orc, "pane-a", time.Now(), 10, 20)
		}
		fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
		settingsPath := filepath.Join(fx.RuntimeDir, "issues", "octocat-hello-world-188", "settings.json")
		fx.Orc.Tick(context.Background())
		waitFor(t, 20*time.Second, "issue ごとの設定ファイルが書かれる", func() bool {
			_, err := os.Stat(settingsPath)
			return err == nil
		})
		raw, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("設定ファイルを読めません: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("設定ファイルを読めません: %v\n%s", err, raw)
		}
		return fx, m
	}
	withSL, slSettings := read(t, ratelimit.SourceStatusline, false)
	withNone, noneSettings := read(t, ratelimit.SourceNone, false)
	withAPI, apiSettings := read(t, ratelimit.SourceOAuthUsageAPI, false)
	withDisabled, disabledSettings := read(t, ratelimit.SourceOAuthUsageAPI, true)

	var sl struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(slSettings["statusLine"], &sl); err != nil {
		t.Fatalf("source が statusline なのに statusLine が無い: %v", err)
	}
	wantCmd := `'/opt/continuo/bin/continuo' statusline --socket '` + withSL.StatuslineSocketPath + `'`
	if sl.Type != "command" || sl.Command != wantCmd {
		t.Errorf("statusLine が %+v（want command %q）", sl, wantCmd)
	}
	if _, ok := noneSettings["statusLine"]; ok {
		t.Errorf("source が none なのに statusLine を書いた: %s", noneSettings["statusLine"])
	}
	var api struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(apiSettings["statusLine"], &api); err != nil {
		t.Fatalf("source が oauth_usage_api なのに statusLine が無い: %v", err)
	}
	if wantAPI := `'/opt/continuo/bin/continuo' statusline --socket '` + withAPI.StatuslineSocketPath + `'`; api.Command != wantAPI {
		t.Errorf("oauth_usage_api の statusLine が %+v（want command %q）", api, wantAPI)
	}
	if _, ok := disabledSettings["statusLine"]; ok {
		t.Errorf("statusline を使えないのに statusLine を書いた: %s", disabledSettings["statusLine"])
	}
	for name, other := range map[string]struct {
		fx *fixture
		m  map[string]json.RawMessage
	}{"oauth_usage_api": {withAPI, apiSettings}, "DisableStatusline": {withDisabled, disabledSettings}} {
		hooks := strings.ReplaceAll(string(other.m["hooks"]), other.fx.RuntimeDir, "<RT>")
		want := strings.ReplaceAll(string(slSettings["hooks"]), withSL.RuntimeDir, "<RT>")
		if hooks != want {
			t.Errorf("%s で hook の部分が変わった:\n %s\n %s", name, hooks, want)
		}
	}

	hooksSL := strings.ReplaceAll(string(slSettings["hooks"]), withSL.RuntimeDir, "<RT>")
	hooksNone := strings.ReplaceAll(string(noneSettings["hooks"]), withNone.RuntimeDir, "<RT>")
	if hooksSL == "" || hooksSL != hooksNone {
		t.Errorf("source によって hook の部分が変わった:\n statusline %s\n none       %s", hooksSL, hooksNone)
	}
}
