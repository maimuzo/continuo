package config

import (
	"sort"
	"strings"
)

// KnownStates は continuo が意味を知っている Status 名をすべて返す（設計 3-50 / 3-55）。
//
// **`active_states` / `terminal_states` / `running_state` / `dispatch_state` /
// `failure_state` / `status_signal_map` の遷移先**を、書かれた順に集める。
//
// **`automated_state_rewrite` は、キーも値もここへ入れない**（設計 3-54 / 3-55）。
//
//	キー … 「ボードの自動化が書く、continuo が知らない Status」である。
//	       **ここへ入れると知っている Status になり、書き戻しの分岐が二度と通らなくなる**
//	値   … `Validate` が「`active_states` に入っていること」を起動前に要求しているので
//	       （`validateAutomatedStateRewrite`）、**足しても1件も増えない**
//
// **起動時に「ボードに実在しなければ起動を止める」一覧も、これである**
// （`tracker` の `requiredStatesForBootstrap`。設計 3-57）。
// **キーも含む一覧が要るのは、ボード側の選択肢が設定に出てくるかを見るときだけである**
// （`BootstrapStates`）。
//
// **集めるのはこの1箇所だけである。**同じ処理を tracker と orchestrator の両方に書くと、
// 片方だけ直したときに「起動時に照合する Status」と「実行時に知っている Status」が
// 食い違い、**照合を通った設定が実行時に知らない Status として扱われる。**
//
// **重複の判定は大文字小文字と前後の空白を無視する。**トラッカーの Status の照合が
// そうしているので（SPEC.md 11.3）、ここだけ完全一致で数えると、綴りだけが違う同じ Status を
// 2件として扱ってしまう。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: Status 名の並び（重複と空文字は落とす。書かれた順）。
func KnownStates(cfg TrackerConfig) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(cfg.ActiveStates)+len(cfg.TerminalStates)+3)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range cfg.ActiveStates {
		add(s)
	}
	for _, s := range cfg.TerminalStates {
		add(s)
	}
	add(cfg.RunningState)
	add(cfg.DispatchState)
	add(cfg.FailureState)
	// **map の反復順に頼らない。**遷移先を読んだ順で並べると、実行のたびに出力が変わる。
	// この一覧は起動時の照合のメッセージと issue のコメントにそのまま載る。
	for _, target := range sortedSignalTargets(cfg.StatusSignalMap) {
		add(target)
	}
	return out
}

// BootstrapStates は、設定のどこかに名前が出てくる Status をすべて返す（設計 3-57）。
//
// **`KnownStates` に `automated_state_rewrite` のキーを足したものである。**
//
// **使うのは「ボードの選択肢が設定に出てくるか」を見る向きだけである**
// （`tracker` の `unknownStatusOptions`）。キーに書いてある Status へ動かされた issue は
// 書き戻されるので、**「continuo が知らない Status」として名前を挙げてはならない。**
//
// **逆向き（設定の名前がボードに実在するか）には使わない。**そちらは `KnownStates` である。
// **キーはボードに実在しなくてよい**（消した人が抜け出せなくなる。設計 3-57）。
//
// **キーは名前順で末尾に足す。**map の反復順は決まらないので、そのまま回すと
// 出てくる並びが実行のたびに変わる。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: Status 名の並び（重複と空文字は落とす。順序は `KnownStates` の並び、
// 対応表のキーだけは名前順で末尾に付く）。
func BootstrapStates(cfg TrackerConfig) []string {
	known := KnownStates(cfg)
	seen := make(map[string]bool, len(known))
	for _, s := range known {
		seen[strings.ToLower(strings.TrimSpace(s))] = true
	}
	keys := make([]string, 0, len(cfg.AutomatedStateRewrite))
	for from := range cfg.AutomatedStateRewrite {
		keys = append(keys, from)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(known)+len(keys))
	out = append(out, known...)
	for _, from := range keys {
		from = strings.TrimSpace(from)
		if from == "" {
			continue
		}
		key := strings.ToLower(from)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, from)
	}
	return out
}

// sortedSignalTargets は `tracker.status_signal_map` の遷移先を、表明の値の名前順に返す。
//
// **map をそのまま回してはならない。**反復順が決まらないので、同じ設定から実行のたびに
// 違う並びが出る。
//
// m: `tracker.status_signal_map`。
// 戻り値: 遷移先の Status 名（null の項目は落とす。キーの名前順）。
func sortedSignalTargets(m map[string]*string) []string {
	signals := make([]string, 0, len(m))
	for signal := range m {
		signals = append(signals, signal)
	}
	sort.Strings(signals)
	out := make([]string, 0, len(signals))
	for _, signal := range signals {
		if target := m[signal]; target != nil {
			out = append(out, *target)
		}
	}
	return out
}

// CleanupStatesOutsideTerminal は `cleanup.on_states` のうち、`tracker.terminal_states` に
// 無いものを書いてある綴りのまま返す（設計 3-9e）。
//
// **「終わったとみなさない Status で worktree を片付ける」設定を見つけるためにある。**
// この2つのキーは別の問いに答える。
//
//	tracker.terminal_states … その issue は終わったか（終わっていなければ worker を止める）
//	cleanup.on_states       … その issue の worktree を片付けてよいか
//
// **片付ける値が「終わった」に入っていないと、continuo は同じ issue を
// 「終わっていない」と判定した直後に worktree を消す。**実際に、ボードの自動化が
// PR のマージで `Done` を書く運用で起きた（issue #35）。
//
// **起動は止めない。**止めると、いま動いている人の continuo が版を上げた瞬間に
// 起動しなくなる。**壊れるものは無く、片付けの筋が通らないだけである**ので、
// 起動時の警告（internal/daemon）と `continuo doctor` の見出し語 `片付けの状態` で知らせる。
// **`cleanup.on_states` と `tracker.active_states` の重なりとは扱いが違う。**
// あちらは走っている worktree を消すので、`config.Validate` が起動前に止める。
//
// **比べ方は `containsStateFold` に合わせる。**大文字小文字と前後の空白だけが違う値は
// 同じ Status として扱う（トラッカーの照合がそうしているので、ここだけ完全一致で
// 比べると、検査を通った設定が実行時には別の意味になる）。
//
// cfg: 検証を通った設定。
// 戻り値: 食い違っている値（`cleanup.on_states` に書いてある綴り・書いてある順）。
// **`cleanup.enabled` が偽なら常に nil を返す**（片付けそのものが走らないので、
// 食い違っていても何も起きない）。
func CleanupStatesOutsideTerminal(cfg Config) []string {
	if !cfg.Cleanup.Enabled {
		return nil
	}
	var out []string
	for _, state := range cfg.Cleanup.OnStates {
		if containsStateFold(cfg.Tracker.TerminalStates, state) {
			continue
		}
		out = append(out, state)
	}
	return out
}
