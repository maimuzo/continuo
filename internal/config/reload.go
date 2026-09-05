package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Reloadable は、走っている最中に差し替えてよい設定である（設計 3-24）。
//
// **ここに無いキーは、走っている continuo では絶対に変わらない。**
// **足すときは1キーずつ、別の issue で確かめてから足すこと。**判定は2つある。
//
//  1. **新しい Status 名を持ち込まないこと。**カンバンのアダプタは Status の選択肢 ID を
//     起動時に焼き込む。新しい名前を入れると Status の書き込みが必ず失敗し、
//     `tracker.verify_states_every` の周期（既定20巡回＝約10分）まで直らない。
//     **0 なら永久に直らない。**
//  2. **走っている run が、その値を前提に外部へ既に何かを書いていないこと。**
//     例: `tracker.comments.marker` はエージェントへ渡すプロンプトに埋まっているので、
//     変えると成功した run が failure_state へ落ちる。
//
// **hook の経路のキーをここへ入れてはならない。**`claude.hook_bridge.listen` は
// 起動時に socket のパスへ解決され、issue ごとの設定ファイルへ書き込み済みである。
// **入れると、動いている Claude Code の hook の宛先と本体が食い違う。**
// この禁止はテストで機械的に止めている。
type Reloadable struct {
	// OnAssigneeGate は tracker.provider.handoff.on_assignee_gate である。
	// 担当者が付いていて着手できないときに、issue へ案内を書くかどうかを決める。
	OnAssigneeGate string
	// AutomatedStateRewrite は tracker.automated_state_rewrite である。
	// カンバンの自動化が動かした Status を、どの Status へ戻すかの対応表である。
	AutomatedStateRewrite map[string]string
	// MaxConcurrentAgents は agent.max_concurrent_agents である。
	MaxConcurrentAgents int
	// MaxConcurrentAgentsByState は agent.max_concurrent_agents_by_state である。
	//
	// **MaxConcurrentAgents と1組で持つ。**空なら全体の上限へ落ちる決まりなので
	// （types.go の MaxConcurrentAgentsByState）、**片方だけを読み直せるようにすると、
	// 同じ判定の中で出どころが2つになる。**
	MaxConcurrentAgentsByState map[string]int
}

// ExtractReloadable は Config から、差し替えてよい値だけを取り出す。
//
// cfg: 取り出し元の設定。
// 戻り値: 差し替えてよい値だけを持つ Reloadable。
func ExtractReloadable(cfg Config) Reloadable {
	return Reloadable{
		OnAssigneeGate:             cfg.Tracker.Provider.Handoff.OnAssigneeGate,
		AutomatedStateRewrite:      cfg.Tracker.AutomatedStateRewrite,
		MaxConcurrentAgents:        cfg.Agent.MaxConcurrentAgents,
		MaxConcurrentAgentsByState: cfg.Agent.MaxConcurrentAgentsByState,
	}
}

// MergeReloadable は、いま効いている設定に、差し替えてよいキーだけを新しい設定から上書きする。
//
// **向きが逆だと危ない。**「凍結するキーを古い設定から書き戻す」形にすると、
// **書き漏らしたキーが「効く」側へ落ちる。**Config にキーを足した人がここの更新を忘れると、
// 走っている run の前提を壊す側で黙って動く。
// **この向きなら、書き漏らしは「変わらない」側へ倒れる。**
//
// **混ぜた結果へ検査を掛け直す。**混ぜた結果は、古いファイルとしても新しいファイルとしても
// 存在しない第3の組み合わせであり、**どちらの検査も通っていない。**
// 例: `tracker.automated_state_rewrite` のキーは、設定のどこにも名前が出てこない Status で
// なければならないが（validateAutomatedStateRewrite）、**新しい対応表と、いま効いている
// `tracker.status_signal_map` の組み合わせは誰も検査していない。**
//
// old: いま効いている設定。
// next: 読み直した設定（Load を通っているので、それ単体では正しい）。
// 戻り値: 混ぜた結果と、混ぜた結果が検査に落ちた場合のエラー。
// **エラーは「利用者のファイルが不正」ではなく「この組み合わせは読み直しでは作れない」を意味する。**
func MergeReloadable(old, next Config) (Config, error) {
	merged := old
	merged.Tracker.Provider.Handoff.OnAssigneeGate = next.Tracker.Provider.Handoff.OnAssigneeGate
	merged.Tracker.AutomatedStateRewrite = next.Tracker.AutomatedStateRewrite
	merged.Agent.MaxConcurrentAgents = next.Agent.MaxConcurrentAgents
	merged.Agent.MaxConcurrentAgentsByState = next.Agent.MaxConcurrentAgentsByState

	// **展開のあとの値を混ぜているので、展開の検査も掛け直す。**
	if err := validate(&merged); err != nil {
		return Config{}, err
	}
	if err := validateExpanded(&merged); err != nil {
		return Config{}, err
	}
	return merged, nil
}

// FrozenChange は、読み直しても効かなかった項目1件である。
type FrozenChange struct {
	// Key はドット区切りのキーである（例 `polling.interval_ms`）。
	//
	// **葉の名前だけでは足りない。**`max` / `order` / `timeout_ms` は設定の中に2箇所以上あり、
	// 葉だけを出すとどれのことか決められない。
	Key string
	// From はいま効いている値である。
	From string
	// To は読み直したファイルに書かれていた値である。
	To string
}

// String は「キー: 前 → 後」の1行にする。
func (c FrozenChange) String() string { return fmt.Sprintf("%s: %s → %s", c.Key, c.From, c.To) }

// PromptBodyKey は、front matter ではなく WORKFLOW.md の本文が変わったことを表すキーである。
//
// **本文は Config の中に無い**（config.Loaded の PromptTemplate は Config の兄弟である）。
// **だから設定の差分だけを見ると、本文だけを直した人には1行も出ない。**
// 読み直しは走るので、利用者は「本文も効いた」と読んでしまう。
const PromptBodyKey = "WORKFLOW.md の本文（エージェントへ送る指示書）"

// FrozenChanges は、読み直しても効かなかった項目を並べる。
//
// **merged が next と違うのは、凍結したキーが変わったときだけである**（MergeReloadable が
// 差し替えてよいキーを next から写しているため）。だから2つを比べれば、効かなかった項目が出る。
//
// **キーごとのコードを書かない。**設定にキーを足しても、この関数は直さなくてよい。
// **`reflect` を使わない。**YAML へ落として map に戻し、キーをたどって比べる。
// **map の並び順に依存しない。**キーで引き当てて比べるためである。
//
// merged: 実際に効かせる設定。
// next: 読み直したファイルの設定。
// 戻り値: 効かなかった項目（キーの順）と、YAML へ落とせなかった場合のエラー。
func FrozenChanges(merged, next Config) ([]FrozenChange, error) {
	a, err := toTree(merged)
	if err != nil {
		return nil, err
	}
	b, err := toTree(next)
	if err != nil {
		return nil, err
	}
	var out []FrozenChange
	diffTree("", a, b, &out)
	for i := range out {
		if maskedKeys[out[i].Key] {
			// **値を伏せる**（下の maskedKeys）。
			out[i].From, out[i].To = maskedValue, maskedValue
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// maskedValue は、値を出さずに「変わった」ことだけを伝える文字列である。
const maskedValue = "（値は伏せます）"

// maskedKeys は、変わったことは伝えるが値をログへ出さないキーである。
//
// **`claude.env` は Claude Code へ渡す環境変数である。**利用者がそこへ鍵を置くことがあり、
// **前後の値をログへ出すと、ログを貼り付けただけで外へ出る。**
// **キーの名前は出す。**何を直したのかが分からないと、再起動が要ることに気づけない。
var maskedKeys = map[string]bool{"claude.env": true}

// toTree は Config を YAML 経由で入れ子の map へ直す。
//
// **YAML を通すのは、キーの名前を front matter と同じにするためである。**
// Go のフィールド名（`IntervalMs`）ではなく、利用者が書いた名前（`interval_ms`）で報告する。
func toTree(cfg Config) (map[string]any, error) {
	// **文言を足さない。**ここへ来るのは「素の構造体を YAML へ直せなかった」という
	// 起こりえない失敗であり、呼び出し側が文脈つきのログを出す。
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// diffTree は2つの入れ子の map を突き合わせ、値の違うところをドット区切りのキーで並べる。
//
// prefix: ここまでのキーの道筋（先頭では空文字）。
// a: 実際に効かせる側。
// b: 読み直したファイルの側。
// out: 見つけた違いの積み先。
func diffTree(prefix string, a, b map[string]any, out *[]FrozenChange) {
	for _, key := range unionKeys(a, b) {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		av, aok := a[key]
		bv, bok := b[key]
		am, aIsMap := av.(map[string]any)
		bm, bIsMap := bv.(map[string]any)
		// **両方が入れ子なら、そこへ降りる。**片方だけが入れ子のときは値として比べる
		// （型が変わることは front matter の検査が通っている以上まず無いが、落とさない）。
		if aok && bok && aIsMap && bIsMap {
			diffTree(path, am, bm, out)
			continue
		}
		if formatValue(av, aok) == formatValue(bv, bok) {
			continue
		}
		*out = append(*out, FrozenChange{
			Key:  path,
			From: formatValue(av, aok),
			To:   formatValue(bv, bok),
		})
	}
}

// unionKeys は2つの map のキーを重複なく名前順で返す。
func unionKeys(a, b map[string]any) []string {
	seen := make(map[string]bool, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// formatValue は値を報告に載せる文字列へ直す。
//
// **map と配列は中身まで出す。**`automated_state_rewrite` のように、
// 何を何へ変えたのかが分からないと直しようがないためである。
//
// v: 値。
// ok: その map にキーがあったか。**無いことと、値が nil であることを区別する。**
func formatValue(v any, ok bool) string {
	if !ok {
		return "（設定なし）"
	}
	switch t := v.(type) {
	case nil:
		return "null"
	case map[string]any:
		parts := make([]string, 0, len(t))
		for _, k := range sortedKeys(t) {
			parts = append(parts, fmt.Sprintf("%s: %v", k, t[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, fmt.Sprintf("%v", e))
		}
		return "[" + strings.Join(parts, " ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// sortedKeys は map のキーを名前順で返す（報告の順序を実行のたびに変えないため）。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FileStamp は WORKFLOW.md が変わったかを見るための印である（設計 3-24）。
//
// **fsnotify は使わない。**`stat` と中身のハッシュで足りる（設計の測定で 4KB のファイル1回 18.5µs）。
// **`SPEC.md` 6.2 が「監視が取りこぼした場合に備えて防御的に再検査せよ」と求めているので、
// どのみち巡回のたびに見る処理は要る。**
type FileStamp struct {
	// ModTime は最後に書き換えられた時刻である。
	ModTime time.Time
	// Size はファイルの大きさである。
	Size int64
	// Sum は中身の SHA-256 である。
	//
	// **更新時刻と大きさだけでは足りない。**エディタは中身を変えずに保存し直すことがあり、
	// そのたびに読み直すと、変えていない設定の読み直しが走る。
	Sum [sha256.Size]byte
}

// Same は2つの印が同じ中身を指しているかを返す。
//
// **中身のハッシュだけで決める。**更新時刻が動いても中身が同じなら、読み直す必要は無い。
func (s FileStamp) Same(other FileStamp) bool { return s.Sum == other.Sum }

// StampOf は path のファイルの印を取る。
//
// path: WORKFLOW.md の絶対パス。
// 戻り値: 印と、読めなかった場合のエラー。
// **呼び出し側は、エラーを致命として扱ってはならない。**エディタが書き換えている最中は
// 一時的に読めないことがある（rename の途中など）。次の巡回で取り直せばよい。
func StampOf(path string) (FileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileStamp{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileStamp{}, err
	}
	return FileStamp{ModTime: fi.ModTime(), Size: fi.Size(), Sum: sha256.Sum256(raw)}, nil
}
