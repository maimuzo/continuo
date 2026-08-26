package config

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
