# 実機で確かめるための環境

**言いたいこと。**実機の確認は**専用のボードとリポジトリ**で行う。**本番のボードは使わない。**
**この環境は消さない。**セッションをまたいで何度も使う。

---

## 何があるか

| 何 | 実体 |
| --- | --- |
| **ボード** | `<ACCOUNT>` の **project #10**（`https://github.com/users/<ACCOUNT>/projects/10`） |
| **リポジトリ** | `<ACCOUNT>/continuo-e2e`（private） |
| **issue** | `#1 README に1行足す` |
| **ラベル** | `continuo-test`（実機テスト用）／`continuo-test/keep`（土台。消さない） |

**`<ACCOUNT>` はこのリポジトリの owner と同じである。**`gh repo view --json owner --jq .owner.login` で引ける。

**本番のボード（project #3）と混同しないこと。**あちらには実データが入っており、検証で書き込んではならない。

## ボードの識別子

**API で Status を動かすときに要る。**

| 何 | 値 |
| --- | --- |
| project_id | `PVT_kwHNNEjOAYV2fA` |
| Status の field_id | `PVTSSF_lAHNNEjOAYV2fM4YE2aC` |
| issue #1 の item_id | `PVTI_lAHNNEjOAYV2fM4N9wYE` |

**Status の option_id。**

| 名前 | option_id |
| --- | --- |
| Ready | `e26993e4` |
| In Progress | `c520e6b8` |
| In Review | `5980043f` |
| Blocked | `2d4f2a34` |
| Done | `cdd586c4` |

**値が変わったら、次で引き直す。**

```bash
gh project view 10 --owner "$(gh repo view --json owner --jq .owner.login)" --format json --jq '.id'
gh project field-list 10 --owner "$(gh repo view --json owner --jq .owner.login)" --format json \
  --jq '.fields[] | select(.name=="Status") | "field=\(.id)", (.options[] | "  \(.name)=\(.id)")'
gh project item-list 10 --owner "$(gh repo view --json owner --jq .owner.login)" --format json \
  --jq '.items[] | "item=\(.id) \(.title) → \(.status)"'
```

## 使い方

**設定は毎回作り直してよい。**`continuo init` がボードから値を引く。

```bash
OWNER="$(gh repo view --json owner --jq .owner.login)"
WORK=~/continuo-e2e-work        # 置き場所は好きにしてよい
mkdir -p "$WORK" && cd "$WORK"

continuo init --project 10 --owner "$OWNER" --force .
```

**worktree の置き場所を本番と分ける。**`workspace.root` を書き換える。

```bash
# WORKFLOW.md の workspace.root を、本番（~/worktrees）以外へ向ける
#   root: ~/continuo-e2e-worktrees
mkdir -p ~/continuo-e2e-worktrees
```

**Status の割り当ては既定のままで合う。**ボードの選択肢を `Ready` / `In Progress` / `In Review` /
`Blocked` / `Done` の5つにしてあるので、`continuo setup` を回さなくてよい。

**前提を確かめてから起動する。**

```bash
continuo trust --dry-run .    # 何を許すかを見る
continuo doctor .             # ✗ が0件になること
continuo                      # 起動（別の端末か背後で）
```

**issue を着手待ちへ動かす。**画面を触らずに API でできる。

```bash
gh project item-edit \
  --id PVTI_lAHNNEjOAYV2fM4N9wYE \
  --project-id PVT_kwHNNEjOAYV2fA \
  --field-id PVTSSF_lAHNNEjOAYV2fM4YE2aC \
  --single-select-option-id e26993e4      # Ready
```

**進み方を見る。**

```bash
gh project item-list 10 --owner "$OWNER" --format json --jq '.items[0] | "\(.title) → \(.status)"'
```

**`In Review` になれば成功である。**手元の実測では、`Ready` から `In Review` まで**2分8秒**だった
（2026-08-25。途中で `agent_prompt_stalled` により1回リトライし、2回目で通った）。

## 終わったあとの片付け

**ボードもリポジトリもラベルも消さない。**次のセッションで再利用する。

**片付けるのは、その回の実行が作ったものだけである。**

```bash
# continuo を止める（pane は閉じないので、必要なら自分で閉じる）
kill -INT "$(pgrep -f 'continuo$' | head -1)"

# worktree と branch と herdr の workspace をまとめて消す
continuo abandon https://github.com/<ACCOUNT>/continuo-e2e/issues/1 . --dry-run   # 先に見る
continuo abandon https://github.com/<ACCOUNT>/continuo-e2e/issues/1 .

# ボードの Status を戻す（Ready へ戻すと、次に起動したとき拾われる）
gh project item-edit --id PVTI_lAHNNEjOAYV2fM4N9wYE --project-id PVT_kwHNNEjOAYV2fA \
  --field-id PVTSSF_lAHNNEjOAYV2fM4YE2aC --single-select-option-id 2d4f2a34   # Blocked（拾わせない）
```

**origin に push された branch は残る。**消すなら次を叩く（**取り消せない**）。

```bash
git -C "$(ghq root)/github.com/<ACCOUNT>/continuo-e2e" push origin --delete continuo/<ACCOUNT>/continuo-e2e/1
```

**issue に付いたコメントも残る。**continuo は消さないので、溜まったら手で消す。

## この環境で確かめられること・確かめられないこと

| 何 | できるか |
| --- | --- |
| issue を着手から `In Review` まで通す | **できる** |
| `continuo abandon` の全経路（`--dry-run` / `--force` / `--park` / `--to`） | **できる**（このボードなら書き込んでよい） |
| リトライとバックオフ | **できる**（`agent_prompt_stalled` は実際に起きる） |
| 再起動をまたぐ引き継ぎ | **できる** |
| `EIO` / `EROFS` の経路 | **できない。**root なしに read-only 再マウントや I/O エラーを作れない |

## 注意

- **Claude Code が実際に動く。**定額プランの枠を消費する。**続けて何度も回さない**
- **`updateProjectV2Field` は、このボードに対してだけ許される。**本番のボードで呼ぶと**設定済みの Status の値が全部消える**
- **`workspace.root` を本番と同じにしない。**同じにすると、本番の worktree と混ざる
- **ボードの題名が「continuo 動作確認（使い捨て）」のままである。**作ったときの名前で、実際は恒久である。
  変えるときは `gh project edit 10 --owner "$OWNER" --title "..."`
