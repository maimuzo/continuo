// **continuo が issue へ実際に投稿する本文で、home が `~` に縮んでいることを確かめる**
// （issue #75 / 設計 3-63）。
//
// **単体の検査だけでは足りない。**縮める関数が正しくても、投稿する経路がそれを通らなければ
// 公開の issue に利用者名が出る。**ここでは Tick を1周させて、投稿された本文を読む。**
package orchestrator_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestHandoff_調べるところのパスはhomeを縮めて出す は、
// 引き渡しの通知に手元の絶対パスがそのまま出ないことを確かめる。
//
// 目的: issue #75 の「公開の issue に利用者名を含むパスが出る」を塞いだことを示す。
// **issue のコメントは編集履歴が残るので、書いてしまうと取り消せない。**
// 与える情報: worktree の置き場所を home の下に持つ run（`HOME` を fixture の一時
// ディレクトリへ差し替える）。max_dispatch_turns を1にして1回目の turn で引き渡させる。
// 成功条件: 【調べるところ】の worktree の行が `~/` で始まり、home の綴りが本文に
// 1文字も残らないこと。
func TestHandoff_調べるところのパスはhomeを縮めて出す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Mutate: func(cfg *config.Config) {
		cfg.Agent.MaxDispatchTurns = 1
	}})
	// **worktree の置き場所の親を home に見立てる。**fixture は `<一時>/wt` を
	// `workspace.root` にしているので、その親を `HOME` にすると worktree が home の下に入る。
	// **newFixture のあとに差し替える。**先に差し替えると、テスト用のリポジトリを作る
	// git が利用者の設定を読めなくなる。
	home := filepath.Dir(fx.WorktreeRoot)
	t.Setenv("HOME", home)

	fx.Tracker.AddIssue(sampleIssue(194, "Ready"))

	transcriptDir := t.TempDir()
	path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "まだ途中です", false),
	})

	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "引き渡しの通知が投稿される", func() bool {
		for _, c := range fx.Tracker.CommentsOf("I_node194") {
			if strings.Contains(c.Body, "人間へ引き渡しました") {
				return true
			}
		}
		return false
	})

	var body string
	for _, c := range fx.Tracker.CommentsOf("I_node194") {
		if strings.Contains(c.Body, "人間へ引き渡しました") {
			body = c.Body
		}
	}
	if !strings.Contains(body, "作業していた場所: `~/") {
		t.Fatalf("worktree のパスが `~/` で始まっていない:\n%s", body)
	}
	if strings.Contains(body, home) {
		t.Errorf("home の綴りが本文に残っている（home=%s）:\n%s", home, body)
	}
}
