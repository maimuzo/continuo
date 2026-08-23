package herdr

import (
	"context"
	"encoding/json"

	"github.com/maimuzo/continuo/internal/i18n"
)

// MethodPing は herdr の socket API の版と機能を取得するメソッド名である。
//
// 2026-08-18 に `herdr api schema --json` と実機への接続で確認済みである（2-1）。
// 引数は無い。**`protocol.version` というメソッドは存在しない。**
const MethodPing = "ping"

// PingResult は ping の応答である。
//
// 実測値（原文）は次のとおりである（2-1）。
//
//	{"id": "probe", "result": {"type": "pong", "version": "0.8.0", "protocol": 19,
//	 "capabilities": {"live_handoff": true, "detached_server_daemon": false}}}
type PingResult struct {
	// Type は応答の種別である。実測では "pong" が返る。
	Type string `json:"type"`
	// Version は herdr 本体のバージョンである（実測: "0.8.0"）。
	// **protocol の照合には使わない。**人間へのログに出す用途で持つ
	// （「herdr のどの版に繋がっているのか」が運用時にすぐ分かるようにするため）。
	Version string `json:"version"`
	// Protocol は socket API の protocol 版である（実測: 19）。
	// 設定ファイルの herdr.protocol と照合する（5-2）。
	Protocol int `json:"protocol"`
	// Capabilities は herdr が持つ機能の一覧である
	// （実測: live_handoff / detached_server_daemon の2つ）。
	//
	// 実スキーマの `schemas.success_response.$defs.ServerCapabilities` も同じ2つを
	// 定義している（live_handoff が必須、detached_server_daemon は既定 false）。
	//
	// 【それでも map[string]any で受ける】
	// キーの顔ぶれと値の型は herdr の版によって増減しうる。将来これ以外の型の値が
	// 増えても解析が失敗しないようにするためである。continuo はいまのところ内容で
	// 分岐せず、人間へのログに出すだけである。
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

// Ping は ping を呼び、herdr の版・protocol 版・機能一覧を取得する。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: herdr の応答。herdr のエラー応答は *Error として返る（IsCode で判定できる）。
func (c *Client) Ping(ctx context.Context) (*PingResult, error) {
	raw, err := c.call(ctx, MethodPing, nil, c.timeouts.Read)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrPingCallFailed, MethodPing, err)
	}

	var result PingResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrPingUnmarshalFailed, MethodPing, err)
	}
	return &result, nil
}

// CheckProtocol は ping で取得した protocol 版が expected と一致するかを検査する
// （設計 5-2 の herdr.protocol。起動時に照合して合わなければ起動を止める）。
//
// ctx: 呼び出しに適用するコンテキスト。
// expected: 設定ファイルの herdr.protocol の値。
// 戻り値: 一致すれば ping の応答と nil を返す。**版が一致しない場合は、取得できた
// 応答とエラーの両方を返す**（呼び出し側が herdr 本体のバージョンを人間へのログに
// 出せるようにするため）。ping そのものに失敗した場合は nil とエラーを返す。
// 一致しない場合のエラーメッセージには herdr 側の実際の版と設定側の期待値の両方を含める
// （運用者が設定ファイルを直すべきか herdr を更新すべきかを判断できるようにするため）。
func (c *Client) CheckProtocol(ctx context.Context, expected int) (*PingResult, error) {
	result, err := c.Ping(ctx)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCheckProtocolPingFailed, err)
	}

	if result.Protocol != expected {
		return result, i18n.Errorf(
			i18n.KeyHerdrCheckProtocolVersionMismatch,
			result.Protocol, expected, result.Version,
		)
	}
	return result, nil
}
