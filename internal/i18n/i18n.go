// Package i18n は continuo が人間に見せる文言を、言語ごとの資源から引く仕組みである
// （docs/plans/continuo_design.md 3-35）。
//
// **日本語が正である。**messages/ja.json に全部のキーがあり、ほかの言語はそこからの
// 差分として置く。**訳が無いキーは日本語へ落とす。**生のキーを画面に出さない。
//
// **書式は fmt の verb をそのまま使う。**`%d` に3桁区切りを入れたりしないので、
// `project #1234` が `project #1,234` に化けることがない。Errorf は fmt.Errorf を
// そのまま呼ぶので `%w` の連鎖も切れない。
//
// **いま移してあるのは画面に出す文言だけである**（`continuo doctor` の検査結果・
// CLI の案内・ダッシュボードの HTML）。エラーとログも同じ T / Errorf で移せる形にしてある。
//
// 使い方。
//
//	i18n.Use(i18n.FromEnv(os.Getenv))          // 起動直後に環境変数から決める
//	lang, err := i18n.Resolve(cfg.Language, os.Getenv) // 設定を読めたら設定で上書きする
//	fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitCreated, path))
//	return i18n.Errorf(i18n.KeyXxx, path, err) // %w を含められる
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Lang は画面に出す言語である。値は BCP 47 の言語部分（`ja` / `en`）だけを持つ。
//
// **地域まで持たない。**`ja_JP` と `ja` で別の資源を用意する予定が無いのに分けると、
// 資源が2つの名前で呼ばれることになる。
type Lang string

const (
	// LangJA は日本語である。**資源の正はこの言語である。**
	LangJA Lang = "ja"
	// LangEN は英語である。**いまは資源が空である。**
	//
	// **中途半端に訳さない。**一部だけ英訳すると、1つの画面に英語と日本語が混ざる
	// （実際、13件だけ訳したとき `doctor` の出力が混ざった）。**混ざったものは、
	// 全部日本語であるより読みにくい。**訳すときは全部訳す。
	LangEN Lang = "en"
)

// SourceLang は資源の正である言語である。
//
// **ほかの言語に訳が無いキーは、必ずこの言語へ落とす。**
const SourceLang = LangJA

// DefaultLang は言語を決められなかったときに使う言語である。
const DefaultLang = LangJA

// EnvLangName は言語を決めるときに読む環境変数の名前である。
//
// **`LC_ALL` と `LC_MESSAGES` は読まない。**言語は WORKFLOW.md の `language` で
// 直接指定できるので、環境変数は「設定に何も書かなかったときの当て推量」以上のものに
// しない。読む変数を増やすと、どれが効いたのかを説明できなくなる。
const EnvLangName = "LANG"

// LangConfigAuto は WORKFLOW.md の `language` に書ける「環境変数から決める」の値である。
const LangConfigAuto = "auto"

// Key は文言を引くための識別子である。
//
// **画面に出る文字列そのものを識別子にしない。**文言を直すたびに識別子が変わると、
// 訳文の対応が切れる。宣言は keys.go にまとめてある。
type Key string

//go:embed messages
var messagesDir embed.FS

// messagesDirName は埋め込んだ資源の置き場所である。
const messagesDirName = "messages"

// catalogs は言語ごとの資源である。init で埋め、以後は書き換えない。
var catalogs = map[Lang]*Catalog{}

// current はいま使っている資源である。**Use と T が別の goroutine から呼ばれるので
// atomic で持つ。**常駐プロセスのダッシュボードは要求ごとに goroutine で走る。
var current atomic.Pointer[Catalog]

// missingMu は missing を守る。
var missingMu sync.Mutex

// missing は「正の言語にも無かったキー」である。**これが空でないのは実装の誤りである。**
// テストから Missing() で読める（設計 3-35 の「キーが存在しないことをテストで検出できる」）。
var missing = map[Key]bool{}

// init は埋め込んだ資源を全部読み、既定の言語を環境変数から決める。
//
// **資源が壊れていたら panic する。**文言を1つも引けない状態で走らせても、
// 利用者には意味の分からない画面しか出せない。埋め込んだ資源は go build の時点で
// 中身が確定しているので、ここで落ちるのは開発中だけである。
func init() {
	entries, err := messagesDir.ReadDir(messagesDirName)
	if err != nil {
		panic(fmt.Sprintf("i18n: 埋め込んだ %s を読めません: %v", messagesDirName, err))
	}
	source := map[Key]string{}
	raw := map[Lang]map[Key]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := Lang(strings.TrimSuffix(e.Name(), ".json"))
		messages, err := readMessages(messagesDir, path.Join(messagesDirName, e.Name()))
		if err != nil {
			panic(fmt.Sprintf("i18n: %s を読めません: %v", e.Name(), err))
		}
		raw[lang] = messages
		if lang == SourceLang {
			source = messages
		}
	}
	if len(source) == 0 {
		panic(fmt.Sprintf("i18n: 正の言語 %s の資源が空です（messages/%s.json）", SourceLang, SourceLang))
	}
	for lang, messages := range raw {
		catalogs[lang] = &Catalog{lang: lang, messages: messages, source: source}
	}
	current.Store(catalogs[DefaultLang])
}

// readMessages は資源のファイル1つを読む。
//
// fsys: 読み出し元。
// name: 読むファイルのパス。
// 戻り値: キーと書式文字列の対応と、読めなかった場合のエラー。
func readMessages(fsys fs.FS, name string) (map[Key]string, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	// **平らな1階層だけを受ける。**入れ子の JSON を書くと、値が文字列でないので
	// ここで落ちる。キーの名前空間は "." でつないだ1本の文字列で表す。
	var m map[Key]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[Key]string{}
	}
	return m, nil
}

// Catalog は言語1つぶんの資源である。
//
// **package の既定（Use / T）とは別に、明示的に持ち回ることもできる。**
// 要求ごとに言語を変える必要が出たときに、既定を書き換えずに済ませるためである。
type Catalog struct {
	// lang はこの資源の言語である。
	lang Lang
	// messages はこの言語のキーと書式文字列の対応である。
	messages map[Key]string
	// source は正の言語（日本語）のキーと書式文字列の対応である。訳が無いときの落とし先。
	source map[Key]string
}

// Lang はこの資源の言語を返す。
//
// 戻り値: 言語。
func (c *Catalog) Lang() Lang { return c.lang }

// Lookup はキーに対応する書式文字列を引く。
//
// **訳が無ければ正の言語（日本語）へ落とす。**
//
// key: 引くキー。
// 戻り値の1つ目: 書式文字列。どちらの言語にも無ければ空文字。
// 戻り値の2つ目: どの言語から引けたか。引けなければ空文字。
// 戻り値の3つ目: 引けたかどうか。
func (c *Catalog) Lookup(key Key) (string, Lang, bool) {
	if pattern, ok := c.messages[key]; ok {
		return pattern, c.lang, true
	}
	if pattern, ok := c.source[key]; ok {
		return pattern, SourceLang, true
	}
	return "", "", false
}

// pattern はキーに対応する書式文字列を返す。
//
// **どちらの言語にも無いキーは、実装の誤りである。**そのキーを missing へ記録し、
// 生のキーではなく「文言が無い」と分かる文字列を返す（設計 3-35）。
//
// key: 引くキー。
// 戻り値: 書式文字列。
func (c *Catalog) pattern(key Key) string {
	if p, _, ok := c.Lookup(key); ok {
		return p
	}
	recordMissing(key)
	return fmt.Sprintf("（文言が登録されていません: %s）", string(key))
}

// T はキーに対応する文言を組み立てる。
//
// **引数が無いときは fmt.Sprintf を通さない。**通すと文言の中の `%` が化ける。
//
// key: 引くキー。
// args: 書式に当てる値。
// 戻り値: 組み立てた文言。
func (c *Catalog) T(key Key, args ...any) string {
	pattern := c.pattern(key)
	if len(args) == 0 {
		return pattern
	}
	return fmt.Sprintf(pattern, args...)
}

// Errorf はキーに対応する文言でエラーを作る。
//
// **fmt.Errorf をそのまま呼ぶ。**`%w` を書いた文言はエラーの連鎖を保つ。
//
// key: 引くキー。
// args: 書式に当てる値。`%w` に対応する位置へ包むエラーを渡す。
// 戻り値: 組み立てたエラー。
func (c *Catalog) Errorf(key Key, args ...any) error {
	pattern := c.pattern(key)
	if len(args) == 0 {
		return fmt.Errorf("%s", pattern)
	}
	return fmt.Errorf(pattern, args...)
}

// Keys はこの資源が持つキーを昇順で返す（訳の埋まり具合を調べるために使う）。
//
// 戻り値: この言語の資源に実際に書かれているキー（正の言語への落とし先は含めない）。
func (c *Catalog) Keys() []Key {
	keys := make([]Key, 0, len(c.messages))
	for k := range c.messages {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// CatalogOf は言語1つぶんの資源を返す。
//
// lang: 欲しい言語。
// 戻り値の1つ目: 資源。
// 戻り値の2つ目: その言語の資源があるかどうか。
func CatalogOf(lang Lang) (*Catalog, bool) {
	c, ok := catalogs[lang]
	return c, ok
}

// Available は資源を持っている言語を昇順で返す。
//
// 戻り値: 言語の一覧。
func Available() []Lang {
	langs := make([]Lang, 0, len(catalogs))
	for l := range catalogs {
		langs = append(langs, l)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

// Supported はその言語の資源があるかどうかを返す。
//
// lang: 調べる言語。
// 戻り値: 資源があれば true。
func Supported(lang Lang) bool {
	_, ok := catalogs[lang]
	return ok
}

// Use は以後 T / Errorf が使う言語を決める。
//
// **資源の無い言語を渡したときは既定の言語にする。**画面に何も出せなくなるより、
// 日本語で出るほうがましである（呼ぶ前に Resolve で弾くこと）。
//
// lang: 使う言語。
func Use(lang Lang) {
	if c, ok := catalogs[lang]; ok {
		current.Store(c)
		return
	}
	current.Store(catalogs[DefaultLang])
}

// Current はいま使っている言語を返す。
//
// 戻り値: 言語。
func Current() Lang { return currentCatalog().lang }

// currentCatalog はいま使っている資源を返す。
//
// 戻り値: 資源。
func currentCatalog() *Catalog {
	if c := current.Load(); c != nil {
		return c
	}
	return catalogs[DefaultLang]
}

// T はいま使っている言語でキーに対応する文言を組み立てる。
//
// key: 引くキー。
// args: 書式に当てる値。
// 戻り値: 組み立てた文言。
func T(key Key, args ...any) string { return currentCatalog().T(key, args...) }

// Errorf はいま使っている言語でキーに対応する文言のエラーを作る。
//
// **`%w` を書いた文言はエラーの連鎖を保つ**（fmt.Errorf をそのまま呼ぶため）。
//
// key: 引くキー。
// args: 書式に当てる値。
// 戻り値: 組み立てたエラー。
func Errorf(key Key, args ...any) error { return currentCatalog().Errorf(key, args...) }

// FromEnv は環境変数から言語を決める（設定に何も書かれていないときの当て推量）。
//
// **読むのは LANG だけである**（EnvLangName の説明を参照）。
// `ja_JP.UTF-8` / `ja-JP` / `ja` のいずれも `ja` になる。`C` と `POSIX`、
// 資源の無い言語、空のときは既定の言語（日本語）にする。
//
// getenv: 環境変数を引く関数（os.Getenv を渡す。テストはmockを渡す）。
// 戻り値: 決まった言語。
func FromEnv(getenv func(string) string) Lang {
	if getenv == nil {
		return DefaultLang
	}
	lang := ParseLocale(getenv(EnvLangName))
	if lang == "" || !Supported(lang) {
		return DefaultLang
	}
	return lang
}

// ParseLocale はロケール文字列から言語の部分を取り出す。
//
// `ja_JP.UTF-8` → `ja`、`en-US` → `en`、`C` / `POSIX` / 空 → 空文字。
//
// value: ロケール文字列。
// 戻り値: 言語（取り出せなければ空文字）。
func ParseLocale(value string) Lang {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	// 文字集合（`.UTF-8`）と修飾子（`@euro`）を落とす。
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	v = strings.ReplaceAll(v, "-", "_")
	if i := strings.Index(v, "_"); i >= 0 {
		v = v[:i]
	}
	v = strings.ToLower(v)
	// `C` と `POSIX` は「ロケールを使わない」の意味であって、言語ではない。
	if v == "" || v == "c" || v == "posix" {
		return ""
	}
	return Lang(v)
}

// Resolve は画面に出す言語を決める。**設定を主、環境変数 LANG を従にする。**
//
// configured: WORKFLOW.md の `language` の値。空か `auto` なら環境変数から決める。
// getenv: 環境変数を引く関数（os.Getenv を渡す）。
// 戻り値の1つ目: 決まった言語。
// 戻り値の2つ目: 設定に資源の無い言語が書かれていた場合のエラー
// （**黙って既定へ落とさない。**書いたつもりの設定が効いていないことに、
// 無人運用では誰も気づけない）。
func Resolve(configured string, getenv func(string) string) (Lang, error) {
	v := strings.TrimSpace(configured)
	if v == "" || v == LangConfigAuto {
		return FromEnv(getenv), nil
	}
	lang := Lang(strings.ToLower(v))
	if !Supported(lang) {
		return DefaultLang, fmt.Errorf(
			"language: %q は対応していません（%s か %q のいずれかにすること）",
			configured, joinLangs(Available()), LangConfigAuto)
	}
	return lang, nil
}

// joinLangs は言語の一覧を読める1行にする。
//
// langs: 並べる言語。
// 戻り値: `"en" / "ja"` のような文字列。
func joinLangs(langs []Lang) string {
	parts := make([]string, 0, len(langs))
	for _, l := range langs {
		parts = append(parts, fmt.Sprintf("%q", string(l)))
	}
	return strings.Join(parts, " / ")
}

// recordMissing は「どの言語にも無かったキー」を控える。
//
// key: 引けなかったキー。
func recordMissing(key Key) {
	missingMu.Lock()
	defer missingMu.Unlock()
	missing[key] = true
}

// Missing は、これまでに引けなかったキーを昇順で返す。
//
// **通常の運用では必ず空である。**空でなければ、messages/ja.json に無いキーを
// コードが引いている（設計 3-35）。テストがこれを見て落ちるようにしてある。
//
// 戻り値: 引けなかったキーの一覧。
func Missing() []Key {
	missingMu.Lock()
	defer missingMu.Unlock()
	keys := make([]Key, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// ResetMissing は控えを空にする（テストが1件ずつ確かめるために使う）。
func ResetMissing() {
	missingMu.Lock()
	defer missingMu.Unlock()
	missing = map[Key]bool{}
}
