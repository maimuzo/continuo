package server

import (
	"html/template"
)

// indexTemplate は実行中の run の一覧を出す HTML である。
//
// **`html/template` を使う。**issue のタイトルも Status も、continuo の外から来る文字列で
// あり、そのまま HTML に混ぜると壊れる（あるいは仕込まれる）。**テンプレートの外で
// 文字列を組み立てない。**`href` に入る URL も、この経路を通せば
// `javascript:` などの危ない scheme が落ちる。
//
// **JavaScript は1行も使わない。**更新は `<meta http-equiv="refresh">` で行う。
var indexTemplate = template.Must(template.New("index").Funcs(template.FuncMap{
	"formatTime":     formatTime,
	"formatInt":      formatInt,
	"refreshSeconds": func() int { return refreshSeconds },
}).Parse(`<!doctype html>
<html lang="ja">
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
<p class="meta">取得時刻 {{ .GeneratedAt.Format "2006-01-02 15:04:05 MST" }} ／ 実行中 {{ .Counts.Running }} 件 ／ バックオフ中 {{ .Counts.Retrying }} 件 ／ {{ refreshSeconds }} 秒ごとに再読み込み</p>

<section>
<table>
<caption>実行中の run</caption>
<thead>
<tr>
<th>issue</th>
<th>Status</th>
<th class="num">turn</th>
<th>最後に hook を受けた時刻</th>
<th class="num">トークン合計</th>
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
{{- if .WaitingQuota }} <span class="badge">枠待ち</span>{{ end }}
{{- if .RetryCount }} <span class="badge">リトライ {{ .RetryCount }}</span>{{ end }}
{{- if .BackoffUntil }} <span class="badge">再開 {{ formatTime .BackoffUntil }}</span>{{ end }}
</td>
<td class="num">{{ .TurnCount }}</td>
<td>{{ if .LastHookAt }}{{ formatTime .LastHookAt }}<span class="title">{{ .LastHookAgo }}</span>{{ else }}—<span class="title">まだ1件も受けていない</span>{{ end }}</td>
<td class="num">{{ formatInt .Tokens.Total }}<span class="title">{{ if .TokensAt }}{{ formatTime .TokensAt }} 時点{{ else }}未集計{{ end }}</span></td>
</tr>
{{- end }}
{{- else }}
<tr><td colspan="5" class="empty">実行中の run はありません。</td></tr>
{{- end }}
</tbody>
</table>
<p class="meta">「最後に hook を受けた時刻」は、その run から実際に hook が届いた時刻である。届いていなければ「—」になる。stall の判定に使う時計はこれとは別に進む（JSON の stall_clock_at）。</p>
</section>

<section>
<table>
<caption>トークンの集計（requestId で重複排除済み。再 dispatch より前のセッションの分も含む）</caption>
<thead>
<tr>
<th>issue</th>
<th class="num">API 応答</th>
<th class="num">input</th>
<th class="num">cache_creation</th>
<th class="num">cache_read</th>
<th class="num">output</th>
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
<td><strong>合計</strong></td>
<td class="num"><strong>{{ formatInt .Totals.APICalls }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.Input }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.CacheCreation }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.CacheRead }}</strong></td>
<td class="num"><strong>{{ formatInt .Totals.Output }}</strong></td>
</tr>
</tbody>
</table>
<p class="meta">トークンは turn の終わりに transcript を読んで集計する（設計 3-15）。走行中の turn の分はまだ入っていない。</p>
</section>

</body>
</html>
`))
