// Client と Pane の小さな読み取りメソッドの検査である。
//
// **どれも1行だが、値を取り違えると気づきにくい。**待ち時間を取り違えれば
// 「なぜか turn が早く切れる」になり、セッション UUID を取り違えれば
// 再起動時の復元が黙って失敗する。
package herdr_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestClient_待ち時間を種類ごとに返す は、3つの待ち時間が混ざらないことを確かめる。
//
// **read / startup / turn は用途も長さも違う。**取り違えると、
// 待ち受けありの呼び出しに短い上限が掛かって turn が途中で切れる。
//
// 目的: 渡した値がそれぞれ対応するメソッドから返ること。
// 与える情報: 3つとも違う値を入れた Timeouts。
// 成功条件: それぞれが自分の値を返すこと。
func TestClient_待ち時間を種類ごとに返す(t *testing.T) {
	c := herdr.New("/tmp/dummy.sock", herdr.Timeouts{
		Read:    1 * time.Second,
		Startup: 2 * time.Second,
		Turn:    3 * time.Second,
	})
	if got := c.ReadTimeout(); got != 1*time.Second {
		t.Errorf("ReadTimeout が違う: %v", got)
	}
	if got := c.StartupTimeout(); got != 2*time.Second {
		t.Errorf("StartupTimeout が違う: %v", got)
	}
	if got := c.TurnTimeout(); got != 3*time.Second {
		t.Errorf("TurnTimeout が違う: %v", got)
	}
}

// TestClient_待ち時間が0以下なら既定を使う は、未設定のときの振る舞いを確かめる。
//
// **0 のまま使うと、接続した瞬間に期限切れになる。**
//
// 目的: 0 と負の値を既定値に置き換えること。
// 与える情報: すべて 0 の Timeouts と、すべて負の Timeouts。
// 成功条件: どちらも既定値が返ること。
func TestClient_待ち時間が0以下なら既定を使う(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   herdr.Timeouts
	}{
		{"すべて0", herdr.Timeouts{}},
		{"すべて負", herdr.Timeouts{Read: -1, Startup: -1, Turn: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := herdr.New("/tmp/dummy.sock", tc.in)
			if got := c.ReadTimeout(); got != herdr.DefaultReadTimeout {
				t.Errorf("ReadTimeout が既定でない: %v", got)
			}
			if got := c.StartupTimeout(); got != herdr.DefaultStartupTimeout {
				t.Errorf("StartupTimeout が既定でない: %v", got)
			}
			if got := c.TurnTimeout(); got != herdr.DefaultTurnTimeout {
				t.Errorf("TurnTimeout が既定でない: %v", got)
			}
		})
	}
}

// TestPane_SessionUUIDはkindがidのときだけ返す は、復元の鍵の取り出しを確かめる。
//
// **`kind` が `path` のときの値はセッションファイルのパスであって UUID ではない。**
// それを UUID として使うと、`claude --resume` が存在しないセッションを指す。
//
// 目的: `kind: id` のときだけ値を返し、それ以外は取り出せなかったものとして扱うこと。
// 与える情報: AgentSession が無い pane、`kind: path` の pane、値が空の pane、`kind: id` の pane。
// 成功条件: 最後だけが true を返すこと。
func TestPane_SessionUUIDはkindがidのときだけ返す(t *testing.T) {
	const uuid = "11111111-2222-3333-4444-555555555555"
	for _, tc := range []struct {
		name     string
		pane     herdr.Pane
		wantUUID string
		wantOK   bool
	}{
		{"AgentSession が無い", herdr.Pane{}, "", false},
		{
			"kind が path",
			herdr.Pane{AgentSession: &herdr.AgentSession{Kind: herdr.AgentSessionKindPath, Value: "/tmp/s.jsonl"}},
			"", false,
		},
		{
			"kind は id だが値が空",
			herdr.Pane{AgentSession: &herdr.AgentSession{Kind: herdr.AgentSessionKindID, Value: ""}},
			"", false,
		},
		{
			"kind が id",
			herdr.Pane{AgentSession: &herdr.AgentSession{Kind: herdr.AgentSessionKindID, Value: uuid}},
			uuid, true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotUUID, gotOK := tc.pane.SessionUUID()
			if gotUUID != tc.wantUUID || gotOK != tc.wantOK {
				t.Errorf("SessionUUID() = (%q, %v), want (%q, %v)", gotUUID, gotOK, tc.wantUUID, tc.wantOK)
			}
		})
	}
}
