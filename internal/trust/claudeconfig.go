package trust

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// indentUnit は `~/.claude.json` を書き戻すときの字下げ1段分である。
//
// **Claude Code が書いている形にそろえる。**実測（2026-08-20）で、このファイルは
// 空白2つの字下げで書かれていた。ここを変えると、人間が差分を見たときに
// 全行が変わったように見える。
const indentUnit = "  "

// trustedKey は `~/.claude.json` の projects の要素が持つ、信頼の承認を表すキーである。
//
// **非公開の内部ファイルのキー名である**（3-6）。将来変わりうる前提で扱う。
const trustedKey = "hasTrustDialogAccepted"

// projectsKey は `~/.claude.json` のうち、フォルダごとの記録が入っているキーである。
const projectsKey = "projects"

// orderedObject は JSON のオブジェクトを、キーの並び順を保ったまま持つ。
//
// **値を json.RawMessage のまま持つのが要点である。**`~/.claude.json` には認証情報を含む
// 全設定が同居しており（実測で83のトップレベルキー、129KB）、Go の構造体へ写して
// 書き戻すと、写していないキーが全部消える。**触るのは対象の1キーだけにする**（3-33）。
//
// **並び順も保つ。**map をそのまま json.Marshal するとキーが辞書順に並び替わり、
// 中身は同じでも差分が全行になる。
type orderedObject struct {
	// keys は現れた順のキーである。
	keys []string
	// vals はキーから値の生のバイト列への対応である。
	vals map[string]json.RawMessage
}

// newOrderedObject は空のオブジェクトを作る。
//
// 戻り値: 空の orderedObject。
func newOrderedObject() *orderedObject {
	return &orderedObject{vals: map[string]json.RawMessage{}}
}

// parseOrderedObject は JSON のオブジェクトを、キーの並び順ごと読み取る。
//
// **値は json.RawMessage で受ける。**Go の型へ写さないので、数値の表記
// （大きな整数や末尾の 0）も文字列のエスケープの書き方も、元のバイト列のまま残る。
//
// raw: JSON のバイト列。
// 戻り値の1つ目: 読み取ったオブジェクト。
// 戻り値の2つ目: オブジェクトでない場合・JSON として読めない場合のエラー。
func parseOrderedObject(raw []byte) (*orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))

	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("JSON として読めません: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("JSON のオブジェクト（`{` で始まる形）ではありません")
	}

	o := newOrderedObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("JSON のキーを読めません: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("JSON のキーが文字列ではありません: %v", keyTok)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("キー %q の値を読めません: %w", key, err)
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("JSON のオブジェクトが閉じていません: %w", err)
	}
	return o, nil
}

// get はキーに対応する値の生のバイト列を返す。
//
// key: 引くキー。
// 戻り値の1つ目: 値の生のバイト列。
// 戻り値の2つ目: そのキーがあれば true。
func (o *orderedObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// set はキーに値を入れる。**既にあるキーは並び順を保ったまま値だけを差し替える。**
//
// key: 入れるキー。
// value: 入れる値の生のバイト列。
func (o *orderedObject) set(key string, value json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

// marshalIndent はオブジェクトを、字下げつきの JSON のバイト列に直す。
//
// **値は json.Indent で並べ直すだけで、中身は読み替えない。**文字列の
// エスケープの書き方も、数値の表記も、元のバイト列のまま残る。
//
// depth: このオブジェクト自身が置かれる深さ（トップレベルは 0）。
// 戻り値の1つ目: JSON のバイト列（末尾に改行は付けない）。
// 戻り値の2つ目: 値を並べ直せない場合のエラー。
func (o *orderedObject) marshalIndent(depth int) ([]byte, error) {
	if len(o.keys) == 0 {
		return []byte("{}"), nil
	}
	inner := strings.Repeat(indentUnit, depth+1)
	closing := strings.Repeat(indentUnit, depth)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteString(",\n")
		}
		buf.WriteString(inner)
		encoded, err := encodeJSONString(k)
		if err != nil {
			return nil, fmt.Errorf("キー %q を JSON の文字列にできません: %w", k, err)
		}
		buf.Write(encoded)
		buf.WriteString(": ")
		var indented bytes.Buffer
		if err := json.Indent(&indented, o.vals[k], inner, indentUnit); err != nil {
			return nil, fmt.Errorf("キー %q の値を並べ直せません: %w", k, err)
		}
		buf.Write(indented.Bytes())
	}
	buf.WriteString("\n")
	buf.WriteString(closing)
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// encodeJSONString は文字列を JSON の文字列リテラルに直す。
//
// **`<` `>` `&` を \u00XX に置き換えない**（json.Marshal の既定はそうする）。
// キーはファイルの絶対パスなので、置き換わると同じパスが2通りの書き方で書かれることになり、
// Claude Code が書いた行と continuo が書いた行が見た目で食い違う。
//
// s: 直す文字列。
// 戻り値の1つ目: JSON の文字列リテラル（前後の `"` を含む）。
// 戻り値の2つ目: 変換に失敗した場合のエラー。
func encodeJSONString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	// Encode は末尾に改行を付ける。
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
