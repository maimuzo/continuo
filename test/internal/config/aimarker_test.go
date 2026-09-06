// Package config_test のうち、このファイルは
// 「人間ではなく機械が書いた」の印（`config.AIMarker`）と、
// その綴りが他の印とぶつかっていないことを固定する（設計 3-82。issue #245）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する。
// **投稿者でも `author_association` でも見分けられない。**
// **本文へ印を差し込む処理は、この package には無い。**
// あれはコメントの本文を編む処理なので、`internal/tracker` にある
// （検査は [test/internal/tracker/ai_marker_test.go](../tracker/ai_marker_test.go)）。
// **ここが持つのは、印の綴りだけである。**
package config_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 目的: 印の綴りを固定する（設計 3-82）。
//
// **組み込みのプロンプトがエージェントへ書かせる文字列と、1文字も違ってはならない。**
// 違うと、エージェントが書いた印を、あとから読む人も機械も同じものとして数えられない。
//
// 与える情報: config.AIMarker。
// 成功条件: `<!-- continuo:ai -->` であること。既存の印のどれとも一致しないこと。
func TestAIMarker_綴りが固定されている(t *testing.T) {
	if config.AIMarker != "<!-- continuo:ai -->" {
		t.Fatalf("印の綴りが変わっています: got %q, want %q",
			config.AIMarker, "<!-- continuo:ai -->")
	}
	// **既存の印と食い違っていること。**同じ綴りにすると、continuo の判定が変わる
	// （issue #245 の本文が「既にある2つを流用してはいけません」と決めている）。
	// **issue #245 が名指ししたのは、この2つである。**
	// `<!-- continuo:agent -->` を流用すると `FetchComments` が `IsAgent` を立て、
	// `<!-- continuo:self -->` を流用すると、そのコメントが次の turn の入力から丸ごと外れる。
	defaults := config.DefaultConfig().Tracker.Comments
	for _, other := range []string{
		defaults.Marker, defaults.SelfMarker,
		config.ProgressMarker, config.PlanMarker,
		config.HandoffBidMarker, config.HandoffHoldMarker, config.HandoffReleasedMarker,
	} {
		if config.AIMarker == other {
			t.Errorf("印が既存の印 %q と同じです。流用すると continuo の判定が変わります", other)
		}
	}
}
