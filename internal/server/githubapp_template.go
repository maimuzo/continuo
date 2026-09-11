package server

import (
	"html/template"

	"github.com/maimuzo/continuo/internal/i18n"
)

// githubAppTemplateText は GitHub App を作る導線の4枚の画面（と、続けられなかったときの1枚）の
// HTML である（docs/plans/impl/issue245_github_app_attribution.md の 3-82g）。
//
// **押す前に、そのボタンが何をするかを画面へ出す。**人間の決定（2026-09-08）
// 「ボタンを押すと何が起こるのか、どういう仕組なのかを人間に提示しておかないと、怖がって人間が
// ボタンを押せない。画面上で説明するようにして。installするときも同様」に依る。
//
// **`html/template` を使う。**GitHub App の名前・slug・ログイン名は continuo の外から来る文字列で
// あり、テンプレートの外で文字列を組み立てない。manifest の JSON は hidden の入力の `value` に
// 入れ、html/template が属性の文脈でエスケープする（ブラウザが復号して GitHub へ送る）。
//
// **inline の HTML と CSS だけである。script は1行も書かない。**CSP は `default-src 'none'` のまま、
// `form-action` だけを `'self' https://github.com` に緩める（`githubAppCSP`）。
//
// **form は2つだけである。**manifest を GitHub へ POST する form（段1）と、名前を入れ直す
// GET の form（段1。送り先は `/github-app?name=…`）。認可と install はクエリを持つ GET のリンクである。
//
// **画面に出る文言はこのファイルに書かない**（設計 3-35）。`t` に渡したキーで internal/i18n の
// 資源から引く。**`TemplateSource` がこの原文も返す**ので、test/internal/i18n が
// `t "…"` のキーを拾って messages/ja.json との対応を確かめる。
const githubAppTemplateText = `<!doctype html>
<html lang="{{ lang }}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ t "dashboard.github_app.title" }}</title>
<style>
:root { color-scheme: light dark; }
body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 1.5rem; line-height: 1.6; max-width: 48rem; }
h1 { font-size: 1.25rem; margin: 0 0 .25rem; }
h2 { font-size: 1.1rem; margin: 1.5rem 0 .5rem; }
h3 { font-size: 1rem; margin: 1.25rem 0 .25rem; }
.meta { color: #666; font-size: .85rem; margin-bottom: 1.25rem; }
ul { padding-left: 1.25rem; }
li { margin: .2rem 0; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .9em; }
.notes { border-left: 3px solid #8886; padding-left: 1rem; list-style: none; }
.action { margin: 1.25rem 0; }
button, a.button { font: inherit; padding: .5rem 1rem; border: 1px solid #8888; border-radius: .4rem; background: #8881; color: inherit; text-decoration: none; display: inline-block; cursor: pointer; }
input[type=text] { font: inherit; padding: .3rem .5rem; border: 1px solid #8888; border-radius: .3rem; min-width: 18rem; }
.warn { border: 1px solid #c60; border-radius: .4rem; padding: .75rem 1rem; }
.error { border: 1px solid #c33; border-radius: .4rem; padding: .75rem 1rem; }
</style>
</head>
<body>
<h1>{{ t "dashboard.github_app.title" }}</h1>
<p class="meta"><a href="/">{{ t "dashboard.github_app.back" }}</a></p>

{{- if eq .Stage "create" }}
<section>
<h2>{{ t "dashboard.github_app.create.heading" }}</h2>
<p>{{ t "dashboard.github_app.create.intro" }}</p>

<form method="get" action="{{ .SelfPath }}">
<label>{{ t "dashboard.github_app.create.name_label" }} <input type="text" name="name" value="{{ .Name }}"></label>
<button type="submit">{{ t "dashboard.github_app.create.name_apply" }}</button>
</form>
<p class="meta">{{ t "dashboard.github_app.create.name_note" }}</p>

<h3>{{ t "dashboard.github_app.create.what_happens" }}</h3>
<ol>
<li>{{ t "dashboard.github_app.create.step_manifest" }}</li>
<li>{{ t "dashboard.github_app.create.step_confirm" }}</li>
<li>{{ t "dashboard.github_app.create.step_return" .RedirectURL }}</li>
<li>{{ t "dashboard.github_app.create.step_store" .CredentialsPath }}</li>
</ol>

<h3>{{ t "dashboard.github_app.create.permissions_heading" }}</h3>
<ul>
<li>{{ t "dashboard.github_app.create.permission_issues" }}</li>
<li>{{ t "dashboard.github_app.create.permission_metadata" }}</li>
</ul>
<p>{{ t "dashboard.github_app.create.permission_note" }}</p>

<h3>{{ t "dashboard.github_app.create.redirects_heading" }}</h3>
<ul>
<li>{{ t "dashboard.github_app.create.redirect_created" }} <code>{{ .RedirectURL }}</code></li>
<li>{{ t "dashboard.github_app.create.redirect_installed" }} <code>{{ .SetupURL }}</code></li>
<li>{{ t "dashboard.github_app.create.redirect_authorized" }} <code>{{ .CallbackURL }}</code></li>
</ul>

<ul class="notes">
<li>{{ t "dashboard.github_app.create.note_nothing_written" }}</li>
<li>{{ t "dashboard.github_app.create.note_personal" }}</li>
<li>{{ t "dashboard.github_app.create.note_sudo" }}</li>
<li>{{ t "dashboard.github_app.create.note_interrupted" }}</li>
</ul>

<form method="post" action="{{ .ManifestAction }}" class="action">
<input type="hidden" name="manifest" value="{{ .ManifestJSON }}">
<button type="submit">{{ t "dashboard.github_app.create.button" .Name }}</button>
</form>
</section>

{{- else if eq .Stage "install" }}
<section>
<h2>{{ t "dashboard.github_app.install.heading" }}</h2>
{{- if .JustCreated }}
<p>{{ t "dashboard.github_app.install.created" .AppName .CredentialsPath }}</p>
{{- else }}
<p>{{ t "dashboard.github_app.install.resume" .AppName }}</p>
{{- end }}
{{- if .AppHTMLURL }}
<p class="meta"><a href="{{ .AppHTMLURL }}" rel="noreferrer noopener">{{ t "dashboard.github_app.install.app_settings" }}</a></p>
{{- end }}
<p>{{ t "dashboard.github_app.install.intro" }}</p>
<ul class="notes">
<li>{{ t "dashboard.github_app.install.scope" }}</li>
<li>{{ t "dashboard.github_app.install.removable" }}</li>
<li>{{ t "dashboard.github_app.install.note_nothing_written" }}</li>
</ul>
<p class="action"><a class="button" href="{{ .InstallURL }}">{{ t "dashboard.github_app.install.button" }}</a></p>
<p><a href="{{ .AuthorizePath }}">{{ t "dashboard.github_app.install.already" }}</a></p>
</section>

{{- else if eq .Stage "authorize" }}
<section>
<h2>{{ t "dashboard.github_app.authorize.heading" }}</h2>
{{- if .ExpiredAt }}
<p class="warn">{{ t "dashboard.github_app.authorize.expired" .ExpiredAt }}</p>
{{- end }}
<p>{{ t "dashboard.github_app.authorize.intro" .Slug }}</p>
<ul class="notes">
<li>{{ t "dashboard.github_app.authorize.attribution" .Slug }}</li>
<li>{{ t "dashboard.github_app.authorize.expiry" }}</li>
<li>{{ t "dashboard.github_app.authorize.note_stored" .CredentialsPath }}</li>
<li>{{ t "dashboard.github_app.authorize.note_interrupted" }}</li>
</ul>
<p class="action"><a class="button" href="{{ .AuthorizeURL }}">{{ t "dashboard.github_app.authorize.button" }}</a></p>
</section>

{{- else if eq .Stage "done" }}
<section>
<h2>{{ t "dashboard.github_app.done.heading" }}</h2>
<p>{{ t "dashboard.github_app.done.authorized_as" .AuthorizedLogin }}</p>
<p>{{ t "dashboard.github_app.done.stored" .ExpiresAt .CredentialsPath }}</p>
{{- if .Mismatch }}
<p class="warn">{{ t "dashboard.github_app.done.mismatch" .GHLogin .AuthorizedLogin }}</p>
<p><a href="{{ .AuthorizePath }}">{{ t "dashboard.github_app.done.reauthorize" }}</a></p>
{{- else if .GHLoginUnknown }}
<p class="meta">{{ t "dashboard.github_app.done.gh_unknown" }}</p>
<p>{{ t "dashboard.github_app.done.next" }}</p>
{{- else }}
<p>{{ t "dashboard.github_app.done.next" }}</p>
{{- end }}
</section>

{{- else if eq .Stage "configured" }}
<section>
<h2>{{ t "dashboard.github_app.configured.heading" }}</h2>
<p>{{ t "dashboard.github_app.configured.detail" .Slug .CredentialsPath .AuthorizedLogin .ExpiresAt }}</p>
<p class="meta">{{ t "dashboard.github_app.configured.reauthorize_note" }}</p>
<p><a href="{{ .AuthorizePath }}">{{ t "dashboard.github_app.configured.reauthorize" }}</a></p>
</section>

{{- else }}
<section>
<h2>{{ t "dashboard.github_app.error.heading" }}</h2>
<p class="error">{{ .ErrorText }}</p>
<p><a href="{{ .SelfPath }}">{{ t "dashboard.github_app.error.restart" }}</a></p>
</section>
{{- end }}

</body>
</html>
`

// githubAppTemplate は githubAppTemplateText を解釈したものである。
//
// **`t` と `lang` は要求のたびに呼ばれる**（indexTemplate と同じ理由。文言を解釈の時点で焼き付けない）。
var githubAppTemplate = template.Must(template.New("github-app").Funcs(template.FuncMap{
	"t":    translate,
	"lang": func() string { return string(i18n.Current()) },
}).Parse(githubAppTemplateText))
