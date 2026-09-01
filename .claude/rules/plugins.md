# プラグイン

## 絶対条件：`maimuzo-marketplace` のプラグインは、全プロジェクトで有効化する

**規則やスキルは、プラグインがあることを前提に書いてよい。**
**「入っていない環境では」という但し書きを付けない。**

---

## 有効化されていないものを見つけたら、有効化する

**確かめ方。**

```bash
python3 -c "
import json, os
d = json.load(open('.claude/settings.local.json'))
enabled = {k.split('@')[0] for k, v in (d.get('enabledPlugins') or {}).items() if v}
avail = set(os.listdir(os.path.expanduser('~/.claude/plugins/marketplaces/maimuzo-marketplace/plugins')))
missing = sorted(avail - enabled)
print('\n'.join(missing) if missing else '（全部有効）')
"
```

**出たものは有効化する。**`/plugin` で入れるか、`.claude/settings.local.json` の
`enabledPlugins` に `"<名前>@maimuzo-marketplace": true` を足す。

**`.claude/settings.local.json` は `.gitignore` 済みである。**個人の環境の設定なので、commit しない。

---

## 気づいたときに直す

**プラグインが要る作業に入る前に、上の確認を1回だけ叩く。**
**毎回は要らない。**

**有効化していないものが見つかったら、その場で有効化する。**
**人間に訊かない。**
