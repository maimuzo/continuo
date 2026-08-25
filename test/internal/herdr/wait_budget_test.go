package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// slowTimeoutServer は、herdr が待ち受けの上限に達して timeout を返す様子を再現する
// 偽サーバを立てる。
//
// **応答は「herdr へ渡した待ち受けの上限」より遅れて返る。**herdr の待ち受けは
// リクエストが届いてから数え始めるので、実機でも応答は必ずその上限より後に来る。
// continuo 側の socket の期限が herdr の待ち受けと同じ長さしかなければ、この応答は
// 受け取れない。
//
// t: 呼び出し元のテスト。
// delay: 応答を返すまでの待ち。
// 戻り値: 起動した偽サーバ。
func slowTimeoutServer(t *testing.T, delay time.Duration) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(t *testing.T, _ int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		time.Sleep(delay)
		writeErrorResponse(t, conn, req.ID, herdr.ErrCodeTimeout,
			"agent did not settle before the wait timeout")
	})
}

// TestAgentPrompt_待機ありのsocketの期限はherdrの待ち受けより長い は、
// 待ち受けつきの agent.prompt が herdr の timeout を受け取れることを固定する。
//
// 目的: continuo 側の socket の期限と herdr へ渡す待ち受けの上限が同じ値だと、
// **herdr が timeout を返すより必ず先に continuo 側が切れる。**そうなると呼び出し側は
// herdr の timeout（ErrCodeTimeout）ではなく continuo 側の読み取り期限
// （ErrCodeReadTimeout）を受け取り、**枠待ちの判定にも「打ち切らずに待ち直す」経路にも
// 入れないまま run を諦める**（設計 3-2 / 3-27）。
//
// 与える情報: Turn を 200 ミリ秒、Read を 2 秒にした Client と、herdr へ渡す待ち受けを
// 200 ミリ秒にした agent.prompt。偽サーバは 700 ミリ秒待ってから herdr の timeout を返す。
//
// 成功条件: 返るエラーが herdr の timeout（ErrCodeTimeout）であること。
// **continuo 側の読み取り期限（ErrCodeReadTimeout）であってはならない。**
func TestAgentPrompt_待機ありのsocketの期限はherdrの待ち受けより長い(t *testing.T) {
	fs := slowTimeoutServer(t, 700*time.Millisecond)

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{
		Read:    2 * time.Second,
		Startup: 5 * time.Second,
		Turn:    200 * time.Millisecond,
	})

	_, err := client.AgentPrompt(context.Background(), herdr.AgentPromptParams{
		Target: normalize.SafeName("continuo-test"),
		Text:   "続きをどうぞ",
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: 200,
			Until:     []herdr.AgentStatus{herdr.AgentStatusIdle},
		},
	})
	if err == nil {
		t.Fatal("herdr が timeout を返したのにエラーにならなかった")
	}
	if herdr.IsCode(err, herdr.ErrCodeReadTimeout) {
		t.Fatalf("herdr の応答より先に continuo 側の socket が切れた"+
			"（枠待ちの判定にも待ち直しにも入れない）: %v", err)
	}
	if !herdr.IsCode(err, herdr.ErrCodeTimeout) {
		t.Fatalf("herdr の timeout として受け取れていない: %v", err)
	}
}

// TestAgentWait_socketの期限はherdrの待ち受けより長い は、
// agent.wait についても同じ境界を固定する。
//
// 目的: 枠待ちの待ち直しは agent.wait で行う（設計 3-2 の段3）。ここで continuo 側が
// 先に切れると、**枠が明けるのを待っている run が「待ち直しに失敗した」として
// 諦められる。**`claude.poll_wait_ms` に `claude.turn_timeout_ms` と同じ値を書く設定を
// config の検証は許しているので、同じ衝突が起きうる。
//
// 与える情報: Turn を 200 ミリ秒、Read を 2 秒にした Client と、herdr へ渡す待ち受けを
// 200 ミリ秒にした agent.wait。偽サーバは 700 ミリ秒待ってから herdr の timeout を返す。
//
// 成功条件: 返るエラーが herdr の timeout（ErrCodeTimeout）であること。
func TestAgentWait_socketの期限はherdrの待ち受けより長い(t *testing.T) {
	fs := slowTimeoutServer(t, 700*time.Millisecond)

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{
		Read:    2 * time.Second,
		Startup: 5 * time.Second,
		Turn:    200 * time.Millisecond,
	})

	_, err := client.AgentWait(context.Background(), herdr.AgentWaitParams{
		Target:    normalize.SafeName("continuo-test"),
		TimeoutMs: 200,
		Until:     []herdr.AgentStatus{herdr.AgentStatusIdle},
	})
	if err == nil {
		t.Fatal("herdr が timeout を返したのにエラーにならなかった")
	}
	if herdr.IsCode(err, herdr.ErrCodeReadTimeout) {
		t.Fatalf("herdr の応答より先に continuo 側の socket が切れた"+
			"（枠待ちの待ち直しが失敗として扱われる）: %v", err)
	}
	if !herdr.IsCode(err, herdr.ErrCodeTimeout) {
		t.Fatalf("herdr の timeout として受け取れていない: %v", err)
	}
}

// TestAgentPrompt_herdrへ渡した待ち受けがTurnより長くてもsocketは先に切れない は、
// 設定で待ち受けを伸ばした場合も socket が先に切れないことを固定する。
//
// 目的: 上限を決めるのは Turn だけではない。**herdr へ渡した待ち受けのほうが長ければ、
// socket の期限もそれより長くなければならない。**
//
// 与える情報: Turn を 100 ミリ秒、Read を 300 ミリ秒にした Client と、herdr へ渡す
// 待ち受けを 600 ミリ秒にした agent.prompt。偽サーバは 700 ミリ秒待ってから
// herdr の timeout を返す（600 + 300 = 900 ミリ秒の期限に収まる）。
//
// 成功条件: 返るエラーが herdr の timeout（ErrCodeTimeout）であること。
func TestAgentPrompt_herdrへ渡した待ち受けがTurnより長くてもsocketは先に切れない(t *testing.T) {
	fs := slowTimeoutServer(t, 700*time.Millisecond)

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{
		Read:    300 * time.Millisecond,
		Startup: 5 * time.Second,
		Turn:    100 * time.Millisecond,
	})

	_, err := client.AgentPrompt(context.Background(), herdr.AgentPromptParams{
		Target: normalize.SafeName("continuo-test"),
		Text:   "続きをどうぞ",
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: 600,
			Until:     []herdr.AgentStatus{herdr.AgentStatusIdle},
		},
	})
	if err == nil {
		t.Fatal("herdr が timeout を返したのにエラーにならなかった")
	}
	if herdr.IsCode(err, herdr.ErrCodeReadTimeout) {
		t.Fatalf("herdr へ渡した待ち受けより先に continuo 側の socket が切れた: %v", err)
	}
	if !herdr.IsCode(err, herdr.ErrCodeTimeout) {
		t.Fatalf("herdr の timeout として受け取れていない: %v", err)
	}
}
