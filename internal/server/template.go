package server

import (
	"html/template"

	"github.com/maimuzo/continuo/internal/i18n"
)

// templateText は実行中の run の一覧を出す HTML である。
//
// **`html/template` を使う。**issue のタイトルも Status も、continuo の外から来る文字列で
// あり、そのまま HTML に混ぜると壊れる（あるいは仕込まれる）。**テンプレートの外で
// 文字列を組み立てない。**`href` に入る URL も、この経路を通せば
// `javascript:` などの危ない scheme が落ちる。
//
// **JavaScript は1行も使わない。**更新は `<meta http-equiv="refresh">` で行う。
//
// **画面に出る文言はこのファイルに書かない**（設計 3-35）。`t` に渡したキーで
// internal/i18n の資源から引く。`lang` は `<html lang="...">` に入れる言語の名前である。
// **キーの文字列はここにしか現れないので、test/internal/i18n がこのファイルを読んで
// messages/ja.json との対応を確かめている。**
const templateText = `<!doctype html>
<html lang="{{ lang }}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="{{ refreshSeconds }}">
<title>continuo</title>
<style>
:root { color-scheme: light dark; }
body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 1.5rem; line-height: 1.6; }
h1 { font-size: 1.25rem; margin: 0 0 .25rem; }
.meta { color: #666; font-size: .85rem; margin-bottom: 1.25rem; }
table { border-collapse: collapse; width: 100%; font-size: .9rem; }
caption { text-align: left; font-weight: 600; margin-bottom: .4rem; }
th, td { border-bottom: 1px solid #8884; padding: .4rem .6rem; text-align: left; vertical-align: top; }
th { font-weight: 600; white-space: nowrap; }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
.title { color: #666; display: block; font-size: .85rem; }
.badge { border: 1px solid #8886; border-radius: 999px; font-size: .75rem; padding: 0 .5rem; white-space: nowrap; }
.empty { color: #666; }
section { margin-bottom: 2rem; }
</style>
</head>
<body>
<h1>continuo</h1>
<p class="meta">{{ t "dashboard.meta" (.GeneratedAt.Format "2006-01-02 15:04:05 MST") .Counts.Running .Counts.Retrying refreshSeconds }}</p>

<section>
<table>
<caption>{{ t "dashboard.caption_runs" }}</caption>
<thead>
<tr>
<th>{{ t "dashboard.col_issue" }}</th>
<th>{{ t "dashboard.col_status" }}</th>
<th class="num">{{ t "dashboard.col_turn" }}</th>
<th>{{ t "dashboard.col_last_hook" }}</th>
<th class="num">{{ t "dashboard.col_tokens_total" }}</th>
</tr>
</thead>
<tbody>
{{- if .Runs }}
{{- range .Runs }}
<tr>
<td>
{{- if .URL }}<a href="{{ .URL }}" rel="noreferrer noopener">{{ .Identifier }}</a>
{{- else }}{{ .Identifier }}{{ end }}
<span class="title">{{ .Title }}</span>
</td>
<td>
{{ .State }}
{{- if .WaitingQuota }} <span class="badge">{{ t "dashboard.badge_waiting_quota" }}</span>{{ end }}
{{- if .RetryCount }} <span class="badge">{{ t "dashboard.badge_retry" .RetryCount }}</span>{{ end }}
{{- if .BackoffUntil }} <span class="badge">{{ t "dashboard.badge_resume" (formatTime .BackoffUntil) }}</span>{{ end }}
</td>
<td class="num">{{ .TurnCount }}</td>
<td>{{ if .LastHookAt }}{{ formatTime .LastHookAt }}<span class="title">{{ .LastHookAgo }}</span>{{ else }}{{ t "dashboard.none" }}<span class="title">{{ t "dashboard.no_hook_yet" }}</span>{{ end }}</td>
<td class="num">{{ formatInt .Tokens.Total }}<span class="title">{{ if .TokensAt }}{{ t "dashboard.tokens_at" (formatTime .TokensAt) }}{{ else }}{{ t "dashboard.tokens_not_counted" }}{{ end }}</span></td>
</tr>
{{- end }}
{{- else }}
<tr><td colspan="5" class="empty">{{ t "dashboard.no_runs" }}</td></tr>
{{- end }}
</tbody>
</table>
<p class="meta">{{ t "dashboard.note_last_hook" }}</p>
</section>

<section>
<table>
<caption>{{ t "dashboard.caption_tokens" }}</caption>
<thead>
<tr>
<th>{{ t "dashboard.col_issue" }}</th>
<th class="num">{{ t "dashboard.col_api_calls" }}</th>
<th class="num">{{ t "dashboard.col_input" }}</th>
<th class="num">{{ t "dashboard.col_cache_creation" }}</th>
<th class="num">{{ t "dashboard.col_cache_read" }}</th>
<th class="num">{{ t "dashboard.col_output" }}</th>
</tr>
</thead>
<tbody>
{{- range .Runs }}
<tr>
<td>{{ .Identifier }}</td>
<td class="num">{{ formatInt .Tokens.APICalls }}</td>
<td class="num">{{ formatInt .Tokens.Input }}</td>
<td class="num">{{ formatInt .Tokens.CacheCreation }}</td>
<td class="num">{{ formatInt .Tokens.CacheRead }}</td>
<td class="num">{{ formatInt .Tokens.Output }}</td>
</tr>
{{- end }}
<tr>
<td><strong>{{ t "dashboard.total" }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.APICalls }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.Input }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.CacheCreation }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.CacheRead }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.Output }}</strong></td>
</tr>
</tbody>
</table>
<p class="meta">{{ t "dashboard.note_tokens" }}</p>
</section>

</body>
</html>
`

// indexTemplate は templateText を解釈したものである。
//
// **`t` と `lang` は要求のたびに呼ばれる。**言語を決めるのは起動時だが、
// テンプレートを解釈するのは init の1回きりなので、文言を解釈の時点で焼き付けない。
var indexTemplate = template.Must(template.New("index").Funcs(template.FuncMap{
	"formatTime":     formatTime,
	"formatInt":      formatInt,
	"refreshSeconds": func() int { return refreshSeconds },
	"t":              translate,
	"lang":           func() string { return string(i18n.Current()) },
}).Parse(templateText))

// translate はテンプレートから文言を引く。
//
// **テンプレートに書けるのはキーだけである**（設計 3-35）。文言そのものを
// テンプレートに書くと、訳文を差し替える口が無くなる。
//
// key: 引くキー（internal/i18n の keys.go に宣言があるもの）。
// args: 書式に当てる値。
// 戻り値: 組み立てた文言。**html/template が出力時にエスケープする。**
func translate(key string, args ...any) string {
	return i18n.T(i18n.Key(key), args...)
}

// TemplateSource は解釈する前のテンプレートの文字列を返す。
//
// **テストが `t "..."` に書かれたキーを拾って、資源との対応を確かめるために使う**
// （設計 3-35）。テンプレートに書いたキーは Go の定数ではないので、
// このファイルを読まないと打ち間違いを見つけられない。
//
// 戻り値: テンプレートの原文。
func TemplateSource() string { return templateText }
