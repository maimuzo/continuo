#!/bin/sh
# install.sh に、シェルが変数名として読み違える書き方が無いかを検査する。
#
# **日本語の文言に変数を埋めるとき、`$NAME` の直後に全角文字が続くと、
# シェルがそこまでを変数名として読む。**`set -u` の下では `unbound variable` になり、
# **その経路を通ったときだけ落ちる。**構文検査（`sh -n`）では見つからない。
#
# 実測（2026-08-21、macOS の bash 3.2）:
#   $ /bin/sh -c 'set -u; os="Plan9"; echo "OS: $os（macOS）"'
#   /bin/sh: os?: unbound variable
#
# **dash では起きない。**CI で dash だけを回しても見つからないので、この検査が要る。

set -eu

target="${1:-install.sh}"

# コメント行を除いて、$NAME の直後が ASCII 以外の箇所を探す。
bad="$(grep -n '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]' "$target" | grep -v '^[0-9]*:[[:space:]]*#' || true)"

if [ -n "$bad" ]; then
	echo "変数の直後に全角文字が続いています。\${NAME} と波括弧で囲んでください:" >&2
	echo "$bad" >&2
	exit 1
fi

echo "OK: $target に、変数名として読み違える書き方はありません"

# **shellcheck も掛ける。**構文検査（`sh -n`）は通るのに問題があるものを拾う。
# CI では必ず走る。**手元に無いと、CI で初めて落ちる**ので、ここでも掛ける。
if command -v shellcheck > /dev/null 2>&1; then
	shellcheck -s sh "$target"
	echo "OK: shellcheck も通りました"
else
	echo "shellcheck が無いので飛ばします（CI では必ず走ります）"
	echo "  入れ方: brew install shellcheck / apt-get install shellcheck"
fi
