package orchestrator

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// agentNameMaxLen は herdr が受け付ける agent 名の長さの上限である
// （`^[a-z][a-z0-9_-]{0,31}$`。設計 3-3）。
const agentNameMaxLen = 32

// agentNamePrefix は agent 名の接頭辞である（設計 3-3 の `continuo-<repo>-<番号>`）。
const agentNamePrefix = "continuo-"

// agentNameSuffixAttempts は名前が重複したときに末尾の連番を試す回数の上限である（設計 3-3）。
const agentNameSuffixAttempts = 10

// BuildAgentName は設計 3-3 の4段のうち、1〜3段（組み立て・正規化・32文字へ収める）を行う。
//
// **agent 名は「人間が端末で見分けるためのもの」に役割を限定する。**
// 名前から元の issue を復元しない（復元の主キーは worktree の身元ファイルである。設計 3-18）。
//
//  1. `--id`・repo・番号から continuo-<名前>-<repo>-<番号> を組み立てる
//     （**`--id` を付けていなければ continuo-<repo>-<番号> である**）
//  2. 小文字にし、英数字とハイフン以外をハイフンに置き換え、連続するハイフンを1つにまとめる
//  3. 32文字を超えていたら、repo の部分を後ろから1文字ずつ削って収める
//     （**番号は削らない。**番号が消えると別の issue と同じ名前になりうる）
//
// **`--id` を混ぜるのは、別のボードを見ている continuo と見分けるためである**（設計 3-17b）。
// 混ぜないと、別のボードの同じ番号の issue が同じ名前になる。
//
// **repo を削り切ってもなお収まらないときは、`--id` の名前も後ろから削る。**
// **一意性はここでは保証しない。**重複は段4（resolveAgentName）が `agent.list` を見て
// `-2`, `-3` を足して解消する。**この関数が守るのは、herdr の 32 文字の上限だけである。**
//
// instanceID: `--id` に渡された名前。**既定なら空文字。**
// repo: リポジトリ名。
// number: GitHub issue の番号。
// 戻り値: 32文字以内に収めた候補の名前。
func BuildAgentName(instanceID, repo string, number int) string {
	suffix := fmt.Sprintf("-%d", number)
	body := foldToAgentNameChars(repo)
	head := foldToAgentNameChars(instanceID)

	// 段3: 番号は削らず、repo の部分を後ろから削って収める。
	budget := agentNameMaxLen - len(agentNamePrefix) - len(suffix)
	if head != "" {
		budget -= len(head) + 1
	}
	if budget < 0 {
		// **repo を全部削ってもなお収まらない。**`--id` の名前を削って上限に合わせる。
		// **番号だけは残す**（消えると別の issue と同じ名前になりうる）。
		head = trimToFit(head, len(head)+budget)
		budget = 0
	}
	if len(body) > budget {
		body = body[:budget]
	}
	body = strings.Trim(body, "-")

	name := agentNamePrefix
	if head != "" {
		name += head + "-"
	}
	name += body + suffix
	// repo が空になった場合に "continuo--188" のような連続ハイフンが残らないようにする。
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

// trimToFit は名前を後ろから削って n 文字に収める。
//
// s: 削る文字列。
// n: 収める長さ。**0 以下なら空文字を返す。**
// 戻り値: n 文字以内に収め、前後のハイフンを落とした文字列。
func trimToFit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return strings.Trim(s, "-")
	}
	return strings.Trim(s[:n], "-")
}

// foldToAgentNameChars は文字列を小文字にし、英数字とハイフン以外をハイフンに置き換え、
// 連続するハイフンを1つにまとめる（設計 3-3 の段2）。
//
// raw: 元の文字列。
// 戻り値: 変換後の文字列。前後のハイフンは落とす。
func foldToAgentNameChars(raw string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(raw) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if prevHyphen {
			continue
		}
		b.WriteByte('-')
		prevHyphen = true
	}
	return strings.Trim(b.String(), "-")
}

// resolveAgentName は設計 3-3 の4段すべてを通して、実際に使える agent 名を決める。
//
// 段4 は「herdr が名前の重複を拒否したら、末尾に -2、-3 と付けて空くまで試す（上限10回）」
// である。**herdr へ実際に起動を試みる前に `agent.list` で使用中の名前を調べる。**
// 起動してから拒否されると pane に半端な状態が残りうるためである。
//
// ctx: 呼び出しに適用するコンテキスト。
// repo: リポジトリ名。
// number: GitHub issue の番号。
// 戻り値の1つ目: 使える agent 名（herdr のパターンを満たすことを検査済み）。
// 戻り値の2つ目: `agent.list` に失敗した場合、または10回試しても空きが無い場合のエラー。
func (o *Orchestrator) resolveAgentName(ctx context.Context, repo string, number int) (normalize.SafeName, error) {
	base := BuildAgentName(o.instanceID, repo, number)

	used := map[string]bool{}
	list, err := o.herdr.AgentList(ctx)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyOrchestratorResolveAgentNameAgentListFailed, err)
	}
	for _, a := range list.Agents {
		if a.Name != "" {
			used[a.Name] = true
		}
	}

	for attempt := 1; attempt <= agentNameSuffixAttempts; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = withNumericSuffix(base, attempt)
		}
		if used[candidate] {
			continue
		}
		name, warnings := normalize.Normalize(candidate)
		for _, w := range warnings {
			o.logger.Warn("agent 名の正規化で情報が落ちました", "message", w.Message)
		}
		if err := herdr.ValidateAgentName(name); err != nil {
			return "", i18n.Errorf(i18n.KeyOrchestratorResolveAgentNameInvalidName, candidate, err)
		}
		return name, nil
	}
	return "", i18n.Errorf(i18n.KeyOrchestratorResolveAgentNameNoFreeName, base, agentNameSuffixAttempts)
}

// withNumericSuffix は名前の末尾に `-<n>` を付ける。32文字を超える場合は
// 接尾辞のぶんだけ前を削る（**接尾辞は必ず残す。**残さないと重複が解消しない）。
//
// base: 元の名前。
// n: 付ける連番。
// 戻り値: 32文字以内に収めた名前。
func withNumericSuffix(base string, n int) string {
	suffix := fmt.Sprintf("-%d", n)
	if len(base)+len(suffix) <= agentNameMaxLen {
		return base + suffix
	}
	cut := agentNameMaxLen - len(suffix)
	if cut < 1 {
		cut = 1
	}
	return strings.TrimRight(base[:cut], "-") + suffix
}

// NewSessionUUID は Claude Code のセッション UUID（version 4）を1つ作る。
//
// **起動のたびに新しく作る。使い回してはならない**（設計 3-3）。
// 一度使った UUID をもう一度渡すと Claude Code が
// `Error: Session ID ... is already in use.` を出して起動に失敗する。
//
// 戻り値の1つ目: `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx` の形の UUID。
// 戻り値の2つ目: 乱数を取得できなかった場合のエラー。
func NewSessionUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", i18n.Errorf(i18n.KeyOrchestratorNewSessionUUIDRandFailed, err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
