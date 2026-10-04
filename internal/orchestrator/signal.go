package orchestrator

import (
	"github.com/maimuzo/continuo/internal/statussignal"
)

// maxSignalsPerTurn は1つの turn で受け付ける表明の件数の上限である。
//
// **実体は `statussignal.MaxSignalsPerTurn` にある**（理由もそこに書いてある）。
// 呼び出し側が警告を出すときに、この名前で引く。
const maxSignalsPerTurn = statussignal.MaxSignalsPerTurn

// ParseSignals は集めた text から表明を拾う（設計 3-25 の段6・段7、および 3-26）。
//
// **実体は `statussignal.Parse` にある**（issue #274）。`continuo hook` が `Stop` の入力から
// 表明を拾うときも同じ関数を呼ぶ。**読み方を2か所に持つと、本体が正しいと読む表明を
// hook が差し戻す。**行頭の印だけを拾う理由・対象の解決・件数と長さの上限は、そちらに書いてある。
//
// texts: assistant の text ブロックの本文の並び（現れた順）。
// prefix: 表明の印（`tracker.status_signal_prefix`。例 `CONTINUO-STATUS:`）。
// currentIdentifier: いま作業している issue の識別子。**対象を書かない行はこれを指す。**
// 戻り値: 対象の識別子から表明の値への対応。
func ParseSignals(texts []string, prefix, currentIdentifier string) map[string]string {
	return statussignal.Parse(texts, prefix, currentIdentifier)
}
