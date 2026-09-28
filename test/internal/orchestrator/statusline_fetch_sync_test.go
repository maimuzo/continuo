package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// statusline取得のうち、**長く待つ経路**を testing/synctest の偽の時計で確かめる（issue #284）。
//
// **なぜ偽の herdr（socket）を使わないか。**値を待つ上限は3分、1回の全体の上限は
// `herdr.startup_timeout_ms` の2倍と3分の和（既定5分）で、実時間で待つとテストが数分かかる。
// synctest の中では network I/O の期限が偽の時計で決まるので、socket の偽物は使えない。
// そこで、orchestrator の herdr は stub（stub_test.go の stubHerdr）、workspace の Manager の
// herdr はこのファイルの memWorkspaceHerdr にし、loop は本物を渡す。
//
// **clone の選び方は本物の git を起こす**（internal/workspace/trust.go の TrustedClonePath）。
// bubble の中で git を起こしても、その間は偽の時計が進まないだけで、結果は返る
// （2026-09-28 に go 1.26.7 / macOS で、bubble の中の `git rev-parse` が返ることを確かめた）。
// リポジトリと `~/.claude.json` は bubble の外で用意する。

// memWorkspaceHerdr は、workspace の Manager に渡す herdr の偽物である（通信しない）。
//
// statusline取得の workspace の開け閉め（workspace.create / workspace.list / workspace.close）
// だけを持つ。issue の worktree は開かない（このファイルのテストは着手しない）。
type memWorkspaceHerdr struct {
	mu sync.Mutex
	// next は次に払い出す workspace の通し番号である。
	next int
	// workspaces は開いている workspace である。
	workspaces map[string]herdr.Workspace
	// creates は workspace.create に渡した params である。
	creates []herdr.WorkspaceCreateParams
	// createHadDeadline は workspace.create に渡された ctx が期限を持っていたかである。
	createHadDeadline []bool
	// closes は workspace.close に渡した ID である（断ったものも含む）。
	closes []string
	// closeErr は workspace.close が返す誤りである（nil なら閉じる）。
	closeErr error
}

// newMemWorkspaceHerdr は空の偽物を作る。
func newMemWorkspaceHerdr() *memWorkspaceHerdr {
	return &memWorkspaceHerdr{workspaces: map[string]herdr.Workspace{}}
}

// WorktreeOpen は使わない（このファイルのテストは着手しない）。
func (m *memWorkspaceHerdr) WorktreeOpen(context.Context, herdr.WorktreeOpenParams) (*herdr.WorktreeOpenResult, error) {
	return nil, errors.New("memWorkspaceHerdr: worktree.open は使わない")
}

// WorktreeRemove は使わない。
func (m *memWorkspaceHerdr) WorktreeRemove(context.Context, herdr.WorktreeRemoveParams) (*herdr.WorktreeRemoveResult, error) {
	return nil, errors.New("memWorkspaceHerdr: worktree.remove は使わない")
}

// WorkspaceRename は使わない。
func (m *memWorkspaceHerdr) WorkspaceRename(context.Context, herdr.WorkspaceRenameParams) (*herdr.WorkspaceRenameResult, error) {
	return nil, errors.New("memWorkspaceHerdr: workspace.rename は使わない")
}

// WorkspaceList は開いている workspace を返す。
func (m *memWorkspaceHerdr) WorkspaceList(context.Context) (*herdr.WorkspaceListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := &herdr.WorkspaceListResult{Type: "workspace_list"}
	for _, ws := range m.workspaces {
		out.Workspaces = append(out.Workspaces, ws)
	}
	return out, nil
}

// WorkspaceClose は workspace を閉じる（closeErr があれば断る）。
func (m *memWorkspaceHerdr) WorkspaceClose(_ context.Context, params herdr.WorkspaceCloseParams) (*herdr.WorkspaceCloseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, params.WorkspaceID)
	if m.closeErr != nil {
		return nil, m.closeErr
	}
	delete(m.workspaces, params.WorkspaceID)
	return &herdr.WorkspaceCloseResult{Type: "ok"}, nil
}

// WorkspaceCreate は `worktree` 欄の無い workspace を作る（本物と同じ）。
func (m *memWorkspaceHerdr) WorkspaceCreate(ctx context.Context, params herdr.WorkspaceCreateParams) (*herdr.WorkspaceCreateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, has := ctx.Deadline()
	m.creates = append(m.creates, params)
	m.createHadDeadline = append(m.createHadDeadline, has)
	m.next++
	id := fmt.Sprintf("m%d", m.next)
	ws := herdr.Workspace{WorkspaceID: id, Label: params.Label}
	m.workspaces[id] = ws
	return &herdr.WorkspaceCreateResult{
		Type:      "workspace_created",
		Workspace: ws,
		RootPane:  herdr.Pane{PaneID: id + ":p1", WorkspaceID: id},
	}, nil
}

// counts は、作った数・閉じようとした数・開いたままの数を返す。
func (m *memWorkspaceHerdr) counts() (created, closed, open int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.creates), len(m.closes), len(m.workspaces)
}

// setCloseErr は workspace.close が返す誤りを入れる。
func (m *memWorkspaceHerdr) setCloseErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

// trustedClone は、statusline取得に使える clone（本物の git のリポジトリ）と、それを信頼済みと
// 記録した `~/.claude.json` を置いたホームディレクトリである。
type trustedClone struct {
	// Dir は clone のパスである。
	Dir string
	// Home は `~/.claude.json` を置いたホームディレクトリである。
	Home string
}

// newTrustedClone は信頼済みの clone を用意する。**bubble の外で呼ぶこと**（git を何度も起こす）。
func newTrustedClone(t *testing.T) trustedClone {
	t.Helper()
	repo := newTestRepo(t)
	home := t.TempDir()
	toplevel := runGit(t, repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	doc, err := json.Marshal(map[string]any{
		"projects": map[string]any{toplevel: map[string]any{"hasTrustDialogAccepted": true}},
	})
	if err != nil {
		t.Fatalf("~/.claude.json を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), doc, 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けません: %v", err)
	}
	return trustedClone{Dir: repo.Dir, Home: home}
}

// newFetchStubFixture は、statusline取得を走らせられる stub の fixture を作る。
//
// **bubble の中で呼ぶこと。終わる前に Close を呼ぶこと。**
//
// t: 呼び出し元のテスト。
// clone: newTrustedClone が用意した clone。
// ws: workspace の Manager に渡す herdr の偽物。
// 戻り値: 組み立てた fixture。
func newFetchStubFixture(t *testing.T, clone trustedClone, ws *memWorkspaceHerdr) *stubFixture {
	t.Helper()
	return newStubFixture(t, stubFixtureOptions{
		Logs:           true,
		WorkspaceHerdr: ws,
		HomeDir:        clone.Home,
		GhqList: func(_ context.Context, owner, repo string) (string, error) {
			if owner+"/"+repo == "octocat/hello-world" {
				return clone.Dir, nil
			}
			return "", nil
		},
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = "statusline"
			cfg.Trust.Repositories = []string{"octocat/hello-world"}
		},
	})
}

// slSessionOf は、stub が受けた n 番目（0 始まり）の statusline取得の agent.start の
// `--session-id` を返す。無ければ空文字。
func slSessionOf(fx *stubFixture, n int) string {
	starts := fx.Herdr.SLStarts()
	if n >= len(starts) {
		return ""
	}
	args := starts[n].Args
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// fetchWarns は、statusline取得の失敗の WARN の行を返す。
func fetchWarns(fx *stubFixture) []string {
	var out []string
	for _, line := range strings.Split(fx.Logs.String(), "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, "statusline取得ができません") {
			out = append(out, line)
		}
	}
	return out
}

// startFetch は巡回を1回回して statusline取得を起こし、goroutine が止まるまで待つ。
func startFetch(fx *stubFixture) {
	fx.Orc.Tick(context.Background())
	synctest.Wait()
}

// TestStatuslineFetchSync_値が1行も届かなければ3分でWARNを出して閉じる は、「値が1行も届かなかった」を確かめる。
//
// 目的: 送ってから3分の間に api_ms が1度も増えなければ、失敗として WARN を出し、workspace を閉じる。
// 上限・組織の managed settings の statusLine・古い herdr を疑う案内を出す。
// 与える情報: 入力を受け付けるが、行を1行も送らない Claude Code。
// 成功条件: 3分の直前までは走っていて、3分を過ぎると「値が1行も届かなかった」の WARN が
// 1行だけ出て、workspace を閉じ、走っていない状態に戻ること。
func TestStatuslineFetchSync_値が1行も届かなければ3分でWARNを出して閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()

		startFetch(fx)
		if got := fx.Herdr.SLPrompts(); len(got) != 1 {
			t.Fatalf("前提: hello を送っていない: %v", got)
		}
		time.Sleep(3*time.Minute - time.Second)
		synctest.Wait()
		if !fx.Orc.StatuslineFetchRunningForTest() {
			t.Fatal("3分を待たずに終えた")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()

		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "値が1行も届かなかった") {
			t.Errorf("WARN が「値が1行も届かなかった」の1行ではない: %q", warns)
		}
		if created, closed, open := ws.counts(); created != 1 || closed != 1 || open != 0 {
			t.Errorf("workspace を閉じていない: 作った %d・閉じた %d・開いたまま %d", created, closed, open)
		}
		if fx.Orc.StatuslineFetchRunningForTest() {
			t.Error("終えたのに走っている状態のまま（次の試行を開けない）")
		}
	})
}

// TestStatuslineFetchSync_応答はあったが値が無ければ3分でWARNを出して閉じる は、「応答はあったが値が無い」を確かめる。
//
// 目的: api_ms が増えたのに rate_limits を持つ行が来なければ、Pro / Max 以外の契約か
// API キーを疑う案内を出す（値が1行も届かないときと理由を分ける）。
// 与える情報: hello に応答し、`rate_limits` が null で api_ms の増えた行を1行送る Claude Code。
// 成功条件: 3分後に「応答はあったが値が無い」の WARN が1行だけ出て、workspace を閉じること。
// 入札は読めないままであること。
func TestStatuslineFetchSync_応答はあったが値が無ければ3分でWARNを出して閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{Prompt: func(_ context.Context, p herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
			fx.Orc.OnStatusline(slLine(slSessionOf(fx, 0), 900, nil, nil))
			return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
		}})

		startFetch(fx)
		time.Sleep(3*time.Minute + time.Second)
		synctest.Wait()

		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "応答はあったが値が無い") {
			t.Errorf("WARN が「応答はあったが値が無い」の1行ではない: %q", warns)
		}
		if _, closed, open := ws.counts(); closed != 1 || open != 0 {
			t.Errorf("workspace を閉じていない: 閉じた %d・開いたまま %d", closed, open)
		}
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Errorf("値の無い行で入札が読めるようになった: %+v", snap)
		}
	})
}

// TestStatuslineFetchSync_起動の段で全体の上限に当たったら起動しなかったとして閉じる は、
// 全体の上限（起動の段）を確かめる。
//
// 目的: 1回の statusline取得には `herdr.startup_timeout_ms` の2倍と3分の和の上限を掛ける。
// 起動の段で上限に当たったら「起動しなかった」とし、止めるときも取り消さない ctx で閉じる。
// 与える情報: agent.start が ctx の終わりまで返らない herdr。
// 成功条件: 全体の上限を過ぎると「起動しなかった」の WARN が1行だけ出て、workspace を閉じること。
func TestStatuslineFetchSync_起動の段で全体の上限に当たったら起動しなかったとして閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{Start: func(ctx context.Context, _ herdr.AgentStartParams) (*herdr.AgentStartResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}})
		overall := 2*time.Duration(fx.Config.Herdr.StartupTimeoutMs)*time.Millisecond + 3*time.Minute

		startFetch(fx)
		time.Sleep(overall + time.Second)
		synctest.Wait()

		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "起動しなかった") {
			t.Errorf("WARN が「起動しなかった」の1行ではない: %q", warns)
		}
		if _, closed, open := ws.counts(); closed != 1 || open != 0 {
			t.Errorf("全体の上限で抜けた経路で workspace を閉じていない: 閉じた %d・開いたまま %d", closed, open)
		}
	})
}

// TestStatuslineFetchSync_workingのまま起動の期限を過ぎたら起動しなかったとして閉じる は、起動の期限を確かめる。
//
// 目的: `herdr.startup_timeout_ms` までに idle か done で入力を受け付ける状態にならなければ、
// hello を送らずに閉じる。
// 与える情報: agent.get が working を返し続ける herdr。
// 成功条件: 起動の期限の直前までは走っていて、期限を過ぎると「起動しなかった」の WARN が
// 1行だけ出て、hello を送らずに workspace を閉じること。
func TestStatuslineFetchSync_workingのまま起動の期限を過ぎたら起動しなかったとして閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{Get: func(_ context.Context, p herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
			return &herdr.AgentGetResult{Agent: herdr.Agent{Name: p.Target.String(), AgentStatus: herdr.AgentStatusWorking}}, nil
		}})
		startup := time.Duration(fx.Config.Herdr.StartupTimeoutMs) * time.Millisecond

		startFetch(fx)
		time.Sleep(startup - 2*time.Second)
		synctest.Wait()
		if !fx.Orc.StatuslineFetchRunningForTest() {
			t.Fatal("起動の期限を待たずに終えた")
		}
		time.Sleep(4 * time.Second)
		synctest.Wait()

		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "起動しなかった") {
			t.Errorf("WARN が「起動しなかった」の1行ではない: %q", warns)
		}
		if got := fx.Herdr.SLPrompts(); len(got) != 0 {
			t.Errorf("入力を受け付けないのに hello を送った: %v", got)
		}
		if _, closed, open := ws.counts(); closed != 1 || open != 0 {
			t.Errorf("workspace を閉じていない: 閉じた %d・開いたまま %d", closed, open)
		}
	})
}

// TestStatuslineFetchSync_送ったあとに全体の上限に当たったら値が1行も届かなかったとして閉じる は、
// 全体の上限（送ったあと）を確かめる。
//
// 目的: 送るのに時間が掛かり、値を待つ3分より先に全体の上限に当たっても、その時点で終えて閉じる。
// 与える情報: agent.prompt が `herdr.startup_timeout_ms` の2倍と1秒掛かって返り、行を送らない herdr。
// 成功条件: 全体の上限を過ぎた時点で「値が1行も届かなかった」の WARN が1行だけ出て、
// workspace を閉じること。
func TestStatuslineFetchSync_送ったあとに全体の上限に当たったら値が1行も届かなかったとして閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		startup := time.Duration(fx.Config.Herdr.StartupTimeoutMs) * time.Millisecond
		fx.Herdr.SetSL(stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
			time.Sleep(2*startup + time.Second)
			return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
		}})
		overall := 2*startup + 3*time.Minute

		startFetch(fx)
		time.Sleep(overall + time.Millisecond)
		synctest.Wait()

		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "値が1行も届かなかった") {
			t.Errorf("WARN が「値が1行も届かなかった」の1行ではない: %q", warns)
		}
		if _, closed, open := ws.counts(); closed != 1 || open != 0 {
			t.Errorf("workspace を閉じていない: 閉じた %d・開いたまま %d", closed, open)
		}
		if fx.Orc.StatuslineFetchRunningForTest() {
			t.Error("全体の上限を過ぎても走っている")
		}
	})
}

// TestStatuslineFetchSync_agent_not_foundが続けば新しいworkspaceとUUIDで1回だけやり直す は、作り直しを確かめる。
//
// 目的: herdr が Claude Code を見つけられない状態が `herdr.startup_timeout_ms` の半分続いたら、
// その workspace を閉じ、新しい workspace と新しい UUID で1回だけやり直す。
// 与える情報: agent.get が agent_not_found を返し続ける herdr。
// 成功条件: workspace を2回作って2回閉じ、2回の起動の `--session-id` が違い、最後に
// 「起動しなかった」の WARN が1行だけ出ること。
func TestStatuslineFetchSync_agent_not_foundが続けば新しいworkspaceとUUIDで1回だけやり直す(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{Get: func(context.Context, herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
			return nil, &herdr.Error{Code: herdr.ErrCodeAgentNotFound, Message: "agent not found"}
		}})

		startFetch(fx)
		time.Sleep(time.Duration(fx.Config.Herdr.StartupTimeoutMs)*time.Millisecond + 10*time.Second)
		synctest.Wait()

		if created, closed, open := ws.counts(); created != 2 || closed != 2 || open != 0 {
			t.Errorf("作り直しの回数が違う: 作った %d・閉じた %d・開いたまま %d（want 2・2・0）", created, closed, open)
		}
		if a, b := slSessionOf(fx, 0), slSessionOf(fx, 1); a == "" || a == b {
			t.Errorf("やり直しで新しい UUID を使っていない: %q と %q", a, b)
		}
		if got := len(fx.Herdr.SLStarts()); got != 2 {
			t.Errorf("起動が %d 回（want 2。やり直しは1回だけ）", got)
		}
		warns := fetchWarns(fx)
		if len(warns) != 1 || !strings.Contains(warns[0], "起動しなかった") {
			t.Errorf("WARN が「起動しなかった」の1行ではない: %q", warns)
		}
	})
}

// TestStatuslineFetchSync_やり直したstatusline取得で値が届けば成功とする は、作り直しのあとの判定を確かめる。
//
// 目的: 作り直したら、見張るセッションの ID と登録を新しい UUID へ入れ替える。入れ替えないと、
// やり直した statusline取得の行を「知らないセッション」として受け、成功と判定できない。
// 与える情報: 1回目の起動だけ agent_not_found を返し、2回目は入力を受け付ける herdr。
// hello を受けたら、2回目の UUID で rate_limits を持つ行を送る。
// 成功条件: WARN が出ず、入札が読めるようになり、巡回のループへの知らせが1つ置かれること。
func TestStatuslineFetchSync_やり直したstatusline取得で値が届けば成功とする(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{
			Get: func(_ context.Context, p herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
				if len(fx.Herdr.SLStarts()) == 1 {
					return nil, &herdr.Error{Code: herdr.ErrCodeAgentNotFound, Message: "agent not found"}
				}
				return &herdr.AgentGetResult{Agent: herdr.Agent{
					Name: p.Target.String(), AgentStatus: herdr.AgentStatusIdle, InteractiveReady: true,
				}}, nil
			},
			Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
				now := time.Now()
				fx.Orc.OnStatusline(slLine(slSessionOf(fx, 1), 800, slWin(3, now.Add(2*time.Hour)), slWin(34, now.Add(72*time.Hour))))
				return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
			},
		})

		startFetch(fx)
		time.Sleep(time.Minute)
		synctest.Wait()

		if warns := fetchWarns(fx); len(warns) != 0 {
			t.Errorf("やり直して値が届いたのに WARN を出した: %q", warns)
		}
		if fx.Orc.QuotaForBidForTest() == nil {
			t.Error("やり直した statusline取得の値で入札が読めない")
		}
		if got := fx.Orc.PendingStatuslineNotifyForTest(); got != 1 {
			t.Errorf("巡回のループへの知らせが %d（want 1）", got)
		}
		if created, closed, open := ws.counts(); created != 2 || closed != 2 || open != 0 {
			t.Errorf("作った %d・閉じた %d・開いたまま %d（want 2・2・0）", created, closed, open)
		}
	})
}

// TestStatuslineFetchSync_同じセッションから値の行が2本続けて届いても落ちず知らせは1つ は、
// 見張りの done を1度だけ閉じることを確かめる。
//
// 目的: ステータスラインは描き直すたびに行を送るので、値の行は2本以上届く。
// **2本目で done を閉じ直すと panic して本体が落ちる。**また、作る仕事の中の herdr の呼び出しに、
// 全体の上限の期限を流し込まない（loop を最長約5分止めないため）。
// 与える情報: hello を受けたら、同じセッションから api_ms を増やした rate_limits を持つ行を2本送る herdr。
// 成功条件: 落ちず、WARN が出ず、知らせが1つだけ置かれ、workspace.create に渡った ctx が期限を持たないこと。
func TestStatuslineFetchSync_同じセッションから値の行が2本続けて届いても落ちず知らせは1つ(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		defer fx.Close()
		fx.Herdr.SetSL(stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
			now := time.Now()
			id := slSessionOf(fx, 0)
			fx.Orc.OnStatusline(slLine(id, 800, slWin(3, now.Add(2*time.Hour)), nil))
			fx.Orc.OnStatusline(slLine(id, 900, slWin(4, now.Add(2*time.Hour)), nil))
			return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
		}})

		startFetch(fx)
		time.Sleep(time.Minute)
		synctest.Wait()

		if warns := fetchWarns(fx); len(warns) != 0 {
			t.Errorf("値が届いたのに WARN を出した: %q", warns)
		}
		if got := fx.Orc.PendingStatuslineNotifyForTest(); got != 1 {
			t.Errorf("巡回のループへの知らせが %d（want 1）", got)
		}
		ws.mu.Lock()
		hadDeadline := append([]bool(nil), ws.createHadDeadline...)
		ws.mu.Unlock()
		if len(hadDeadline) != 1 || hadDeadline[0] {
			t.Errorf("workspace.create に全体の上限の期限を流し込んだ: %v", hadDeadline)
		}
	})
}

// TestStatuslineFetchSync_止めるときの取り消しではWARNを出さずに閉じる は、止めるときの経路を確かめる。
//
// 目的: continuo を止めるときの取り消しは失敗ではないので WARN を出さない。
// **閉じる仕事は止めるときも取り消さない ctx で積む**（押さえを放すのはこの仕事だけ）。
// 与える情報: agent.get が working を返し続ける herdr。起動を待っている間に orchestrator を閉じる。
// 成功条件: WARN が出ず、workspace を閉じていること。
func TestStatuslineFetchSync_止めるときの取り消しではWARNを出さずに閉じる(t *testing.T) {
	clone := newTrustedClone(t)
	synctest.Test(t, func(t *testing.T) {
		ws := newMemWorkspaceHerdr()
		fx := newFetchStubFixture(t, clone, ws)
		closed := false
		defer func() {
			if !closed {
				fx.Close()
			}
		}()
		fx.Herdr.SetSL(stubSLScript{Get: func(_ context.Context, p herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
			return &herdr.AgentGetResult{Agent: herdr.Agent{Name: p.Target.String(), AgentStatus: herdr.AgentStatusWorking}}, nil
		}})

		startFetch(fx)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		fx.Close()
		closed = true

		if warns := fetchWarns(fx); len(warns) != 0 {
			t.Errorf("止めるときの取り消しで WARN を出した: %q", warns)
		}
		if _, n, open := ws.counts(); n != 1 || open != 0 {
			t.Errorf("止めるときに workspace を閉じていない: 閉じた %d・開いたまま %d", n, open)
		}
	})
}

// TestStatuslineFetchSync_閉じられなかったときのWARNは値が届いたら1行ほかの失敗なら2行 は、
// 閉じられなかったときの WARN を確かめる。
//
// 目的: 閉じられなかった workspace は herdr の画面に残るので、人間に知らせる。値が届いた
// あとならこの1行だけ、ほかの理由で失敗したあとなら、その理由の行とあわせて2行にする。
// 閉じられなかった ID は閉じ残しの一覧に残し、次の試行か起動時に閉じ直す。
// 与える情報: workspace.close を断る herdr。サブテストの1つは値を送り、もう1つは確認の画面で止まる。
// 成功条件: WARN の行数が1行と2行で、閉じ残しの一覧に作った ID が残っていること。
func TestStatuslineFetchSync_閉じられなかったときのWARNは値が届いたら1行ほかの失敗なら2行(t *testing.T) {
	cases := []struct {
		name   string
		script func(fx *stubFixture) stubSLScript
		want   []string
	}{
		{
			name: "値が届いた",
			script: func(fx *stubFixture) stubSLScript {
				return stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
					fx.Orc.OnStatusline(slLine(slSessionOf(fx, 0), 800, slWin(3, time.Now().Add(2*time.Hour)), nil))
					return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
				}}
			},
			want: []string{"閉じられなかった"},
		},
		{
			name: "確認の画面で止まった",
			script: func(*stubFixture) stubSLScript {
				return stubSLScript{Get: func(_ context.Context, p herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
					return &herdr.AgentGetResult{Agent: herdr.Agent{Name: p.Target.String(), AgentStatus: herdr.AgentStatusBlocked}}, nil
				}}
			},
			want: []string{"閉じられなかった", "確認の画面で止まった"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone := newTrustedClone(t)
			synctest.Test(t, func(t *testing.T) {
				ws := newMemWorkspaceHerdr()
				ws.setCloseErr(errors.New("閉じられません（テスト用）"))
				fx := newFetchStubFixture(t, clone, ws)
				defer fx.Close()
				fx.Herdr.SetSL(tc.script(fx))

				startFetch(fx)
				time.Sleep(time.Minute)
				synctest.Wait()

				warns := fetchWarns(fx)
				if len(warns) != len(tc.want) {
					t.Fatalf("WARN が %d 行（want %d）: %q", len(warns), len(tc.want), warns)
				}
				for i, w := range tc.want {
					if !strings.Contains(warns[i], w) {
						t.Errorf("%d 行目の WARN に %q が無い: %q", i+1, w, warns[i])
					}
				}
				raw, err := os.ReadFile(filepath.Join(fx.Root, "statusline-fetch", "workspaces.json"))
				if err != nil || !strings.Contains(string(raw), `"m1"`) {
					t.Errorf("閉じられなかった ID が閉じ残しの一覧に無い: %s（%v）", raw, err)
				}
			})
		})
	}
}
