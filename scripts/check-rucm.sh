#!/bin/sh
# RUCM → CFG → テストコードの連鎖が揃っているかを検査する。
#
# **手元で回すためのものである。**検査のスクリプトは maimuzo-rucm プラグインに
# 同梱されており、**このリポジトリは公開なので、外部の貢献者はそれを持っていない。**
# CI でも呼ぶが、見つからなければ飛ばす（落とさない）。
#
# 何を見るか:
#   check_update_cfg.py     … CFG が、いまの RUCM から再生成した内容と一致するか
#   check_update_tests.py   … テストのマーカーが、いまの CFG のハッシュと一致するか
#   check_reference_format.py … rucm.md と judge_log.md のコード参照に行番号が無いか
#
# 使い方:
#   sh scripts/check-rucm.sh          … 見つからなければ飛ばす（CI 向け）
#   sh scripts/check-rucm.sh --strict … 見つからなければ落とす（手元向け）

set -eu

strict=0
if [ "${1:-}" = "--strict" ]; then
	strict=1
fi

# プラグインのキャッシュから、いちばん新しい版のスクリプトを探す。
scripts_dir=""
for cand in $(ls -d "${HOME}"/.claude/plugins/cache/*/maimuzo-rucm/*/scripts 2> /dev/null | sort -Vr); do
	if [ -f "$cand/check_update_cfg.py" ]; then
		scripts_dir="$cand"
		break
	fi
done

if [ -z "$scripts_dir" ]; then
	if [ "$strict" = "1" ]; then
		echo "maimuzo-rucm のスクリプトが見つかりません。" >&2
		echo "プラグインを入れるか、--strict を外して飛ばしてください。" >&2
		exit 1
	fi
	echo "maimuzo-rucm のスクリプトが無いので、RUCM の検査は飛ばします"
	exit 0
fi

echo "RUCM の検査に使うスクリプト: $scripts_dir"

# CFG が RUCM から再生成した内容と一致するか。
python3 "$scripts_dir/check_update_cfg.py"

# テストのマーカーが CFG のハッシュと一致するか。
# **[W1]（テスト未生成パス）は警告であって、落とす理由にはしない。**
python3 "$scripts_dir/check_update_tests.py" --scan-dir test

# rucm.md と judge_log.md のコード参照に行番号が混ざっていないか。
python3 "$scripts_dir/check_reference_format.py"

echo "OK: RUCM → CFG → テストの連鎖は揃っています"
