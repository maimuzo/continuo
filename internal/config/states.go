package config

import (
	"sort"
	"strings"
)

// AutomatedRewriteTargets は `tracker.automated_state_rewrite` の**戻す先**（値）だけを、
// 名前順に並べて返す（設計 3-54）。
//
// **キーは入れない。**キーは「ボードの自動化が書く、continuo が知らない Status」であり、
// 知っている Status の一覧へ入れると、書き戻しの分岐が二度と通らなくなる。
//
// **並べ替えるのは、map の反復順が決まらないからである。**この一覧は起動時の照合の
// メッセージと issue のコメントにそのまま載るので、実行のたびに順序が変わってはならない。
//
// **集める処理をここ1箇所に置く。**同じ処理を tracker と orchestrator の両方に書くと、
// 片方だけ直したときに「起動時に照合する Status」と「実行時に知っている Status」が
// 食い違い、**照合を通った設定が実行時に知らない Status として扱われる。**
//
// table: `tracker.automated_state_rewrite`。
// 戻り値: 戻す先の Status 名（設定に書かれた綴りのまま。名前順）。空の表なら空のスライス。
func AutomatedRewriteTargets(table map[string]string) []string {
	out := make([]string, 0, len(table))
	for _, target := range table {
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

// KnownStates は continuo が意味を知っている Status 名をすべて返す（設計 3-50 / 3-55）。
//
// **`StatesNamedInConfig` に `AutomatedRewriteTargets` を足したものである。**
// **足す場所が2つあってはならない。**起動時にボードと照合する一覧
// （`tracker` の `requiredStatesForBootstrap`）と、実行時に「知っている Status か」を
// 判定する一覧（`orchestrator` の `knownStates`）は、**同じ集合でなければならない。**
// 片方だけに足すと、**起動時の照合を通った設定が、実行時には知らない Status として扱われる。**
//
// **`automated_state_rewrite` のキーは入れない**（設計 3-54）。キーは
// 「ボードの自動化が書く、continuo が知らない Status」であり、ここへ入れると
// 知っている Status になって、書き戻しの分岐が二度と通らなくなる。
//
// **重複の判定は大文字小文字と前後の空白を無視する**（`StatesNamedInConfig` と同じ）。
// トラッカーの Status の照合がそうしているので（SPEC.md 11.3）、ここだけ完全一致で
// 数えると、綴りだけが違う同じ Status を2件として扱ってしまう。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: Status 名の並び（重複と空文字は落とす。順序は設定に書かれた順、
// 戻す先だけは名前順で末尾に付く）。
func KnownStates(cfg TrackerConfig) []string {
	named := StatesNamedInConfig(cfg)
	out := make([]string, 0, len(named)+len(cfg.AutomatedStateRewrite))
	seen := map[string]bool{}
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
	for _, s := range named {
		add(s)
	}
	// **`AutomatedRewriteTargets` が名前順に並べて返す。**
	// この一覧は issue のコメントとログにそのまま載るので、実行のたびに順序が変わってはならない。
	for _, target := range AutomatedRewriteTargets(cfg.AutomatedStateRewrite) {
		add(target)
	}
	return out
}

// StatesNamedInConfig は、`tracker.automated_state_rewrite` のキー以外で設定に名前が
// 出てくる Status をすべて返す（設計 3-50 / 3-54）。
//
// **`active_states` / `terminal_states` / `running_state` / `dispatch_state` /
// `failure_state` / `status_signal_map` の遷移先**を、書かれた順に集める。
//
// **`automated_state_rewrite` は、キーも値もここへ入れない。**戻す先（値）を足すのは
// 呼び出し側の仕事である（`AutomatedRewriteTargets`）。**設定の検査は、キーが
// 「既に名前の出てくる Status」かどうかをここで判定する**ので、混ぜてはならない。
//
// cfg: WORKFLOW.md の front matter の tracker セクション。
// 戻り値: Status 名の並び（重複と空文字は落とす。書かれた順）。
func StatesNamedInConfig(cfg TrackerConfig) []string {
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
	for _, target := range cfg.StatusSignalMap {
		if target != nil {
			add(*target)
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
