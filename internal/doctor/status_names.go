package doctor

import (
	"sort"
	"strings"
	"unicode"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// 設定に書いた Status 名の出どころを表す語である。
//
// **どの設定キーに書いた名前が紛らわしいのかを、必ず出す。**
// 「`In Progress` が紛らわしい」とだけ言われても、人間はどの行を直せばよいか分からない。
const (
	// stateOriginActiveStates は `tracker.active_states` に書いた名前である。
	stateOriginActiveStates = "tracker.active_states"
	// stateOriginTerminalStates は `tracker.terminal_states` に書いた名前である。
	stateOriginTerminalStates = "tracker.terminal_states"
	// stateOriginRunningState は `tracker.running_state` に書いた名前である。
	stateOriginRunningState = "tracker.running_state"
	// stateOriginDispatchState は `tracker.dispatch_state` に書いた名前である。
	stateOriginDispatchState = "tracker.dispatch_state"
	// stateOriginFailureState は `tracker.failure_state` に書いた名前である。
	stateOriginFailureState = "tracker.failure_state"
	// stateOriginStatusSignalMap は `tracker.status_signal_map` の遷移先に書いた名前である。
	stateOriginStatusSignalMap = "tracker.status_signal_map"
	// stateOriginAutomatedStateRewrite は `tracker.automated_state_rewrite` の**キー**
	// （ボードの自動化が書く Status 名）に書いた名前である。
	stateOriginAutomatedStateRewrite = "tracker.automated_state_rewrite"
	// stateOriginCleanupOnStates は `cleanup.on_states` に書いた名前である。
	stateOriginCleanupOnStates = "cleanup.on_states"
)

// configuredState は設定に書いた Status 名1つである。
type configuredState struct {
	// Origin は書いてある設定キーである（stateOriginActiveStates などのいずれか）。
	Origin string
	// Name は書いてある名前（綴りそのまま）である。
	Name string
}

// confusionKind は「なぜ紛らわしいのか」の種別である。
type confusionKind int

const (
	// confusionSameIgnoringSeparators は、大文字小文字と区切り（空白・記号）を落とすと
	// 同じ綴りになる組である（`InProgress` / `in-progress` / `In Progress`）。
	confusionSameIgnoringSeparators confusionKind = iota
	// confusionContains は、語の並びとして一方が他方を丸ごと含む組である
	// （`In Progress` ⊂ `AI In Progress`）。
	confusionContains
)

// confusingPair は「設定に書いた名前」と「ボードの別の選択肢」の紛らわしい組である。
type confusingPair struct {
	// Configured は設定に書いた名前とその出どころである。
	Configured configuredState
	// BoardOption はボード側の選択肢名（GitHub の綴りそのまま）である。
	BoardOption string
	// Kind はなぜ紛らわしいのかの種別である。
	Kind confusionKind
}

// checkStatusNames は、設定に書いた Status 名と紛らわしい選択肢がボードに無いかを見る
// （見出し語 `Status の名前`）。
//
// **記号は `✗` ではなく `!` にする。**紛らわしいだけでは continuo は動く。
// 起動を止めるほどではないが、**取り違えたまま無人で回すと、人間が作業中の issue に
// エージェントが着手する**ので、黙って通してもいけない。
//
// **なぜ既にある検査では捕まらないのか。**Bootstrap が照合するのは
// 「設定に書いた名前がボード側に在るか」だけである。ボードに `In Progress` と
// `AI In Progress` が並んでいても、設定に書いたほうが在れば通る。
// **設定どうしの重なり（`terminal_states` や `cleanup.on_states` と同じ名前）は
// config.Validate が起動前に落とす**が、落とせるのは**綴りが同じとき**だけである。
// **紛らわしい組は綴りが違うので、どちらの検査も素通りする。**
//
// cfg: 読めた場合の設定。
// boardOptions: ボード側の Status の選択肢名（tracker.Adapter.StatusOptionNames の戻り値）。
// boardSymbol: 上流（ボード）の記号。
// 戻り値: 検査結果。
func checkStatusNames(cfg loadedConfig, boardOptions []string, boardSymbol Symbol) Result {
	if !cfg.OK {
		return Result{
			Label:  LabelStatusNames,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorStatusNamesConfigUnreadable),
		}
	}
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelStatusNames,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorStatusNamesBoardUnreadable),
		}
	}

	pairs := findConfusingPairs(configuredStates(cfg.Config), boardOptions)
	if len(pairs) == 0 {
		return Result{
			Label:  LabelStatusNames,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorStatusNamesOK, len(boardOptions)),
		}
	}

	notes := make([]string, 0, len(pairs))
	for _, p := range pairs {
		reason := i18n.T(i18n.KeyDoctorStatusNamesReasonSame)
		if p.Kind == confusionContains {
			reason = i18n.T(i18n.KeyDoctorStatusNamesReasonContains)
		}
		notes = append(notes, i18n.T(i18n.KeyDoctorStatusNamesNote,
			p.Configured.Origin, p.Configured.Name, p.BoardOption, reason))
	}
	return Result{
		Label:  LabelStatusNames,
		Symbol: SymbolUnknown,
		Detail: i18n.T(i18n.KeyDoctorStatusNamesConfusing, len(pairs)),
		Notes:  notes,
		Remedies: []string{
			i18n.T(i18n.KeyDoctorStatusNamesRemedyPickOne),
			i18n.T(i18n.KeyDoctorStatusNamesRemedyActiveStates),
			i18n.T(i18n.KeyDoctorStatusNamesRemedyOverlap),
		},
	}
}

// configuredStates は設定に書いた Status 名を、出どころ付きで重複なく集める。
//
// **`cleanup.on_states` も含める。**Bootstrap は照合しないが、
// **人間が「片付ける Status」を取り違えると、走っている worktree を消す。**
//
// **`tracker.automated_state_rewrite` のキーも含める**（設計 3-55）。
// **キーは「continuo が知らない Status」なので、実行時の照合には一度も掛からない。**
// 紛らわしい選択肢がボードにあると（`In Progress` と `AI In Progress` など）、
// **書き戻しが一度も動かないのに、その理由がどこにも出ない。**
//
// 同じ名前が複数のキーに書いてあるときは、**先に見たキーだけを残す。**
// 同じ組を出どころ違いで何度も並べても、直すべき行は結局同じである。
//
// cfg: 読めた設定。
// 戻り値: 設定に書いた Status 名（並びは下の add の呼び順で固定する）。
func configuredStates(cfg config.Config) []configuredState {
	seen := make(map[string]bool)
	var result []configuredState
	add := func(origin, name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		key := foldForConfusion(name)
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, configuredState{Origin: origin, Name: name})
	}
	for _, s := range cfg.Tracker.ActiveStates {
		add(stateOriginActiveStates, s)
	}
	for _, s := range cfg.Tracker.TerminalStates {
		add(stateOriginTerminalStates, s)
	}
	add(stateOriginRunningState, cfg.Tracker.RunningState)
	add(stateOriginDispatchState, cfg.Tracker.DispatchState)
	add(stateOriginFailureState, cfg.Tracker.FailureState)
	// **map の反復順に頼らない。**遷移先を読んだ順で並べると、実行のたびに出力が変わる。
	signals := make([]string, 0, len(cfg.Tracker.StatusSignalMap))
	for signal := range cfg.Tracker.StatusSignalMap {
		signals = append(signals, signal)
	}
	sort.Strings(signals)
	for _, signal := range signals {
		if target := cfg.Tracker.StatusSignalMap[signal]; target != nil {
			add(stateOriginStatusSignalMap, *target)
		}
	}
	// **対応表も map の反復順に頼らない。**キーを名前順に見る。
	// **足すのはキーだけである。**戻す先は `config.Validate` が
	// 「`active_states` に入っていること」を要求しているので、上の active_states で既に足りている。
	rewrites := make([]string, 0, len(cfg.Tracker.AutomatedStateRewrite))
	for from := range cfg.Tracker.AutomatedStateRewrite {
		rewrites = append(rewrites, from)
	}
	sort.Strings(rewrites)
	for _, from := range rewrites {
		add(stateOriginAutomatedStateRewrite, from)
	}
	for _, s := range cfg.Cleanup.OnStates {
		add(stateOriginCleanupOnStates, s)
	}
	return result
}

// findConfusingPairs は、設定に書いた名前と紛らわしいボードの選択肢を全部拾う。
//
// **同じ選択肢を指している組は数えない。**設定の `in progress` とボードの `In Progress` は
// 前後の空白と大文字小文字を無視すれば同じ選択肢であり（tracker がその規則で引き当てる）、
// 取り違えようがない。
//
// states: 設定に書いた Status 名。
// boardOptions: ボード側の選択肢名。
// 戻り値: 紛らわしい組（設定に書いた順・ボードの選択肢名の昇順）。
func findConfusingPairs(states []configuredState, boardOptions []string) []confusingPair {
	sorted := append([]string(nil), boardOptions...)
	sort.Strings(sorted)

	var pairs []confusingPair
	for _, st := range states {
		for _, opt := range sorted {
			// tracker が同じ選択肢として引き当てるものは、取り違えの対象にならない。
			if strings.EqualFold(strings.TrimSpace(st.Name), strings.TrimSpace(opt)) {
				continue
			}
			kind, ok := confusionBetween(st.Name, opt)
			if !ok {
				continue
			}
			pairs = append(pairs, confusingPair{Configured: st, BoardOption: opt, Kind: kind})
		}
	}
	return pairs
}

// confusionBetween は2つの Status 名が紛らわしい組かどうかを判定する。
//
// **紛らわしいとは何かを、ここで2つに限って決める。**
//
//	同じに見える … 大文字小文字と区切り（空白・記号）を落とすと同じ綴りになる
//	              （`InProgress` と `In Progress`、`in-progress` と `In Progress`）
//	含んでいる   … 語の並びとして一方が他方を丸ごと含む
//	              （`In Progress` ⊂ `AI In Progress`、`Ready` ⊂ `Ready for Review`）
//
// **含む・含まれるを、ただの部分文字列で見てはならない。**`Abandoned` は文字の並びとして
// `Done` を含む（a-b-a-n-**d-o-n-e**-d）。ボードに `Done` と `Abandoned` を並べるのは
// ごく普通で、そこを警告すると、警告そのものが読まれなくなる。
// **語の並びとして含むかどうかで見る。**
//
// a: 比べる名前。
// b: 比べる名前。
// 戻り値の1つ目: 紛らわしい種別。
// 戻り値の2つ目: 紛らわしい組なら true。
func confusionBetween(a, b string) (confusionKind, bool) {
	foldedA, foldedB := foldForConfusion(a), foldForConfusion(b)
	if foldedA == "" || foldedB == "" {
		return 0, false
	}
	if foldedA == foldedB {
		return confusionSameIgnoringSeparators, true
	}
	wa, wb := statusWords(a), statusWords(b)
	if containsWordRun(wa, wb) || containsWordRun(wb, wa) {
		return confusionContains, true
	}
	return 0, false
}

// foldForConfusion は「同じに見えるか」を比べるための綴りに直す。
//
// **大文字小文字と、区切りに使う文字（空白・記号）を全部落とす。**
// `In Progress` / `in-progress` / `InProgress` はどれも `inprogress` になる。
//
// s: 直す名前。
// 戻り値: 比較に使う綴り。
func foldForConfusion(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// statusWords は Status 名を語の並びに切る。
//
// **文字と数字以外はすべて区切りとして扱う。**空白・`-`・`_`・`/`・`:` のどれで
// 区切られていても、同じ並びになる。
//
// s: 切る名前。
// 戻り値: 小文字にした語の並び（空の語は含まない）。
func statusWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// containsWordRun は outer が inner を「語の並びとして丸ごと」含むかを返す。
//
// **連続した並びだけを含むとみなす。**`Ready Review` は `Ready for Review` を含まない。
//
// outer: 含むほうの語の並び。
// inner: 含まれるほうの語の並び。
// 戻り値: outer が inner より長く、inner を連続した並びとして含めば true。
func containsWordRun(outer, inner []string) bool {
	if len(inner) == 0 || len(outer) <= len(inner) {
		return false
	}
	for start := 0; start+len(inner) <= len(outer); start++ {
		match := true
		for i, w := range inner {
			if outer[start+i] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
