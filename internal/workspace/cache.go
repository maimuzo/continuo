package workspace

import (
	"sync"
	"time"
)

// ttlEntry は ttlCache が持つ値1件である。
type ttlEntry[V any] struct {
	// value は覚えている値である。
	value V
	// expiresAt はこの値を捨てる時刻である。
	expiresAt time.Time
}

// ttlCache は文字列を鍵にした、期限つきの小さなキャッシュである。
//
// **外部プロセスの起動回数を減らすためだけに使う。**判定の正しさをここに依存させない
// （期限が切れれば必ず引き直すので、覚えた値が古くても最大で ttl のあいだだけである）。
//
// **複数の goroutine から同時に呼んでよい。**
type ttlCache[V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]ttlEntry[V]
}

// newTTLCache は期限つきのキャッシュを作る。
//
// ttl: 覚えておく時間。**0 以下なら何も覚えない**（毎回引き直す）。
// now: 現在時刻を返す関数（テストが差し替えられるように受け取る）。
// 戻り値: 組み立てたキャッシュ。
func newTTLCache[V any](ttl time.Duration, now func() time.Time) *ttlCache[V] {
	return &ttlCache[V]{ttl: ttl, now: now, entries: map[string]ttlEntry[V]{}}
}

// get は鍵に対応する値を返す。
//
// key: 引く鍵。
// 戻り値の1つ目: 覚えていた値。
// 戻り値の2つ目: 期限内の値があれば true。
func (c *ttlCache[V]) get(key string) (V, bool) {
	var zero V
	if c == nil || c.ttl <= 0 {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expiresAt) {
		return zero, false
	}
	return entry.value, true
}

// put は鍵に値を覚えさせる。
//
// key: 覚える鍵。
// value: 覚える値。
func (c *ttlCache[V]) put(key string, value V) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = ttlEntry[V]{value: value, expiresAt: c.now().Add(c.ttl)}
}

// keyedMutex はパスなどの文字列ごとに1本ずつ排他を持つ集合である。
//
// **同じ worktree の身元ファイルを読んで書き戻す処理を直列化するために使う**
// （read-modify-write を並行で走らせると、あとの書き込みが前の書き込みを消す）。
// 鍵が違えば並行に走る。
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// newKeyedMutex は鍵ごとの排他の集合を作る。
//
// 戻り値: 組み立てた keyedMutex。
func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: map[string]*sync.Mutex{}}
}

// lock は鍵に対応する排他を取り、それを解く関数を返す。
//
// **鍵を2つ以上取るときは、必ず同じ順序で取ること**（違う順序で取ると相互に待つ）。
//
// key: 排他の鍵。
// 戻り値: 排他を解く関数（defer で呼ぶ）。
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	entry, ok := k.locks[key]
	if !ok {
		entry = &sync.Mutex{}
		k.locks[key] = entry
	}
	k.mu.Unlock()

	entry.Lock()
	return entry.Unlock
}
