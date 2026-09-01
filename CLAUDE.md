# CLAUDE.md

## 絶対条件：発言前の確認（最優先ルール）

**時間がかかってもいいから、すべての発言前に自分の発言に批判的にレビューし、不明瞭な点があれば追加調査して、全て確認できてから答えること。絶対条件。**

- ファイルの存在・内容・パスについて発言する前に、必ず Read / Glob / Grep / Bash で実態を確認する
- 「〜が未作成」「〜が壊れている」「〜が必要」などの問題指摘は、実際にファイルを確認してから行う
- 確認せずに推測で発言することは禁止
- **「〜してよいですか？」と確認した場合は、必ず返答を待ってから実行すること。返答なしに勝手に進めることは禁止**
- **指示の意図が不明瞭な場合は、「〇〇という理解でよいですか？」と自分の解釈を先に述べてから作業すること。**解釈が正しいか確認せずに作業を始めることは禁止

---

## このプロジェクトは何か

**`continuo` は、GitHub Projects v2 のボード1枚を見張り、issue ごとに git worktree を用意して、[herdr](https://github.com/herdrdev/herdr) の pane で Claude Code を対話モードで起動し、完了までを面倒見る常駐プロセスである。**Go で書く。

**名前は通奏低音（basso continuo）に由来する。**バロック音楽で、曲の最初から最後まで途切れず鳴り続け、全体の和声を支える低音パート。

**準拠する仕様は [openai/symphony](https://github.com/openai/symphony) の [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md)**（Apache-2.0）。

> **仕様はこのリポジトリに同梱しない。**再配布になり、ライセンスの扱いが増えるためである。
> **作業に使うときは各自が手元に置く**（`docs/spec/symphony/` は `.gitignore` 済み）。
>
> ```bash
> mkdir -p docs/spec/symphony
> curl -sL https://raw.githubusercontent.com/openai/symphony/main/SPEC.md -o docs/spec/symphony/SPEC.md
> ```
>
> **文書から参照するときは節番号を使う**（例: `SPEC.md` 6.2）。**行番号は使わない**（upstream で動く）。

**設計は [docs/plans/continuo_design.md](docs/plans/continuo_design.md) が正である。**設計の判断はすべてここに記録する。指示を待たない。

---

## 絶対に守る制約

### 1. `claude -p` は使用禁止

**従量課金になるため禁止。提案もしないこと。**Claude Agent SDK と Anthropic API の直叩きも同じ理由で対象外である。

別のコンテキストで作業させたい場合は **herdr の別 pane を開く**（このマシンでは herdr が常駐。`HERDR_ENV=1`）。

```bash
herdr pane split --current --direction down --cwd <ディレクトリ> --no-focus
# 返る JSON の result.pane.pane_id を使う
herdr agent start <名前> --kind claude --pane <pane_id>           # 検知されるまで待つ（既定30秒）
herdr agent prompt <名前> "<プロンプト>" --wait --timeout 120000  # idle/done/blocked まで待つ
herdr agent read <名前> --source recent-unwrapped --lines 50
```

**`herdr wait agent-status …` は存在しない**（herdr 0.8.0 で確認）。待機は `herdr agent wait <名前> --until <status>`。
**`pane run "claude"` で起動する経路も避ける。**`agent start` と違って起動完了を待たないため、直後に `agent wait` を呼ぶと `agent_not_found` で失敗する。

### 2. GitHub Projects v2 の project #3 は本番のボードである

104件の実データが入っている。**検証で書き込まない。**

**実機で確かめるための専用の環境がある。**ボードもリポジトリも issue もラベルも用意済みで、
**Status は API で動かせる。**在りかと使い方は [docs/test_environment.md](docs/test_environment.md) にある。
**この環境は消さない。**セッションをまたいで再利用する。

**とくに `updateProjectV2Field` を本番のボードで呼んではならない。**選択肢の指定は全件置き換えとして扱われ、**設定済みの Status の値が全部消える。**

**テスト用のボード（project #10）に対してだけは呼んでよい。**そこは実データを持たないので、選択肢を作り直しても失うものが無い。
**それ以外のボードでは、選択肢の追加は人間が GitHub の画面から行う。**

### 3. `~/.claude/projects/` 配下を消さない

調査を subagent に依頼するときは「調査結果の書き込み以外、変更・削除を一切禁止」と明示すること。パスの許可リスト／禁止リストは必ず穴が開く。

### 4. ファイルの書き換えは「一時ファイルへ書いてから差し替える」

**書き込む先をその場で開いて中身を空にしてから書いてはならない。**
**途中で落ちると、元の内容が失われる。**

```go
// してはいけない（O_TRUNC で中身を消してから書く）
os.WriteFile(path, data, perm)
os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)

// こうする（同じディレクトリの一時ファイルへ書き切ってから差し替える）
tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
// … 書く / Sync / Close / 権限を元に戻す …
os.Rename(tmp.Name(), path)
```

**一時ファイルは必ず書き込む先と同じディレクトリに作る。**
`os.Rename` が不可分なのは同じファイルシステムの中だけである。

**揃えられない箇所があるなら、その理由をコードのコメントに書く。**黙って例外にしない。
例: ロックファイル（差し替えるとロックが切れる）、追記だけのファイル。

### 5. 公開してよい情報かを常に判断する

**このリポジトリは PUBLIC であり、OSS として公開する予定である。**
**何かを書く前に「これは公開してよいか」を判断すること。**判断せずに書かない。

**絶対にコミットしないもの。**

| 何 | 例 |
| --- | --- |
| API キー・トークン | `ghp_…` / `sk-ant-…` / `github_pat_…` |
| tailnet のホスト名 | `*.ts.net` |
| 個人の絶対パス | `/Users/<名前>/…`（`~/` に直すこと） |
| 個人の環境に依存する設定 | プラグインの有効化・マーケットプレイスの登録（`.claude/settings.local.json` へ。`.gitignore` 済み） |

**例を書くときは架空の名前を使う。**

- **リポジトリ名**: `<owner>/<repo>` か `octocat/hello-world`。**自分の実在のリポジトリ名を書かない**
- **アカウント名**: `<ACCOUNT>` か `octocat`
- **そもそも特定の名前を書かずに済む書き方を先に探す**

> **実在のリポジトリ名やプラグイン名は、漏れても実害は無い**（公開情報である）。
> **だが読む人には「なぜこの人の名前が?」と映る。**架空の名前にしておけば、その疑問が生じない。
>
> **既に履歴へ入ってしまったものは、そのままでよい。**書き換えのために履歴を作り直さない。

### 6. 不特定多数の環境と、maimuzo の環境を混同しない

このcontinuoプロジェクトはOSSである。不特定多数の開発環境でコードが利用され、continuoもまた不特定多数に利用される前提。
一方、開発にはmaimuzoのオリジナルプラグインが使用され、テスト環境もプライベート環境として用意してある。
この不特定多数の環境と、maimuzoの環境を混同してはならない。要件がある時、それは不特定多数向けなのか、maimuzoの環境向けなのかを常に意識すること。
何度もAIは混同し、判断を間違っているので、これは重要な前提であることを忘れないで。

---

## 共通ガイドライン

**コーディングとテストの共通ルールは、このリポジトリの外で管理している。**
**どこから読むかは [.claude/local-guidelines.md](.claude/local-guidelines.md) に書いてある**
（`.gitignore` 済み。環境ごとに違うため共有しない）。

**そのファイルが無い環境では、この節は読み飛ばしてよい。**
このリポジトリの規則は、この CLAUDE.md と [.claude/rules/](.claude/rules/) だけで完結している。

---

## 作業の進め方

**次の5本に従うこと。**`.claude/rules/` の下のファイルは自動では読まれない。**ここから辿る。**

### 設計のレビュー

[.claude/rules/design-review.md](.claude/rules/design-review.md) に従うこと。とくに次の3点。

- **設計が固まったら、実装の前にレビューを通す。**段を飛ばさない
- **レビューは最大3回。**3回で収まらなければ、捨てられる部分を検討して人間に報告する
- **設計を書く前に、そもそも対応するかを疑う**

### 並列で進める

[.claude/rules/parallel-work.md](.claude/rules/parallel-work.md) に従うこと。とくに次の3点。

- **指示が無いときも、既定で Workflow と git worktree の並列にする**
- **直列にしてよいのは、依存があるときだけ**
- **レビューを並列にして、修正を直列にしない**

### worker への指示の書き方

[.claude/rules/worker-instructions.md](.claude/rules/worker-instructions.md) に従うこと。とくに次の3点。

- **worker にも、判断の土台になる文書を読ませる。**指示だけを渡さない
- **検査するコマンドを渡すときは、自分で試してから渡す**
- **不可逆な操作**（PR のマージ・削除・本番への書き込み）**は worker に渡さない**

### worktree の片付け

[.claude/rules/worktree.md](.claude/rules/worktree.md) に従うこと。とくに次の3点。

- **使い終わった worktree は、その作業を終える前に消す**
- **`git worktree prune` は片付けの手段ではない。**`git worktree remove` が要る
- **消す前に、未コミットの変更・未マージの commit・開いている PR・走っている作業の4つを確かめる**

### プラグイン

[.claude/rules/plugins.md](.claude/rules/plugins.md) に従うこと。

**これは開発者の環境向けである**（絶対に守る制約6）。**このリポジトリを clone した人には当てはまらない。**

---

## プランファイルの書き方

[.claude/rules/plan-file.md](.claude/rules/plan-file.md) に従うこと。とくに次の3点。

- **節の冒頭に「言いたいこと」を3行以内で置く。**そのあとに結論、最後に詳細
- **修正の履歴を書かない。**置くのは最新の仕様・選定根拠・比較した案の否定根拠だけ
- **1つの節は50行以内。**長くなったら要約版を別ファイルに作る（元は残す）

## リリースの手順

[.claude/rules/release.md](.claude/rules/release.md) に従うこと。とくに次の3点。

- **実機で issue を1件通してから出す。**テストが全部通っていても、実機で初めて出る欠陥がある
- **文書を直してから出す。**[docs/FAQ.md](docs/FAQ.md) と [docs/upgrading.md](docs/upgrading.md) の両方に入れる。ノートは1回きりで、あとから困った人が引けない
- **`--generate-notes` のまま放置しない。**commit の一覧は利用者に読めない

## 報告のルール

[.claude/rules/reporting.md](.claude/rules/reporting.md) に従うこと。とくに次の3点。

- **返答の冒頭に「何が言いたいのか」を置く**
- **番号だけのラベルを人間に見せない**
- **英語の技術用語（worktree / pane / hook / branch / commit）を日本語に直訳しない**

---

## PR を出すときの絶対条件

**PR を出すときは、必ず `/code-review` でレビューする。**
**レビューを通していないものを、マージ可能な状態にしてはならない。**

| 状態 | レビュー |
| --- | --- |
| **draft の PR** | **通していなくてよい** |
| **draft を外した PR**（マージ可能な状態） | **必ず通してあること** |

**手順。**

1. **まず draft で作る**（`gh pr create --draft`）
2. **`/code-review` を通す**
3. **レビュー結果を、その PR のコメントに貼る。**
   **コメントの先頭に `<!-- code-review-result -->` を置く**（リリース前の検査がこの目印を数える）
4. **指摘に対応する**（下の「コードレビュー記録フロー」に従う）
5. **`gh pr ready` で draft を外す**

**3 を飛ばしたものは、レビューを実施していないものとして扱う。**
**貼ってあることが、実施したことの唯一の証拠である。**

**既に draft を外してしまったものは、`gh pr ready --undo` で戻してからレビューする。**

**この規則は機械で止める。**[.claude/hooks/block-merge-without-review.py](.claude/hooks/block-merge-without-review.py) が
`gh pr merge <番号>` と `gh pr ready <番号>` を実行の前に見て、**目印が無ければ拒否する。**

**規則に書くだけでは守られなかった**（2026-08-29。12本をレビューせずにマージし、あとから回し直すことになった）。
**人間が明示的に許すときだけ、環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` を置いて通す。**
**AI が自分でその環境変数を置いてはならない。**

**エージェントが作る PR にも同じ規則を当てる。**continuo が作った PR も、
レビューを通すまで draft のままにする。

### 絶対条件：PR のマージは、メインエージェントが自分で行う

**worker（subagent / Workflow の agent）に `gh pr merge` を実行させてはならない。**
**マージできる状態かどうかの確認も、メインエージェントが自分で行う。**

**なぜか。**2026-09-01、worker に6本のマージを任せ、**2本をレビュー未実施のままマージした。**
原因はメインエージェントが渡した確認コマンドで、`contains` を使っていた。

```
# 誤り。本文のどこかに含まれていれば1と数える
gh pr view <番号> --json comments --jq '[.comments[] | select(.body | contains("code-review-result"))] | length'

# 正しい。本文の先頭にあるかを見る
gh api "repos/<owner>/<repo>/issues/<番号>/comments" \
  --jq '[.[] | select(.body | startswith("<!-- code-review-result -->"))] | length'
```

**進捗のコメントの本文中に、手順の説明として同じ文字列が入っていた。**
**それを1件と数えて通した。**

### マージの条件は、なるべく機械で判定する

**AI の判断に頼る部分を減らす。**

| 何を確かめるか | どう確かめるか |
| --- | --- |
| **レビュー結果が貼ってあるか** | **GitHub Actions の `review-result`**（`main` の必須の検査。2026-09-01 に追加） |
| ビルドとテスト | `build` 6本と `test` 2本（必須の検査） |
| 衝突が無いか | `gh pr view <番号> --json mergeable,mergeStateStatus` |

**必須の検査は `gh api repos/<owner>/<repo>/branches/main/protection/required_status_checks` で見られる。**

**機械で判定できないものだけを AI が見る。**
**判定できるようにできるなら、issue を立てて機械へ移す。**

## コードレビュー記録フロー

`code-reviewer` / `security-reviewer` エージェントでレビューを受けた後:

1. レビュー結果はそのままの形でプランファイルに記録する
2. まずは AI の判断で修正範囲を決め、修正してよい
3. レビュー指摘内容と、どれを修正したかを一覧で人間に報告する。プランファイルにも記録する
4. 報告の後、人間が追加の修正指示を出す場合があるので、確認を待つ
5. 人間の指示で追加修正した場合は、その内容もプランファイルに記録する

---

## コミットメッセージ形式

`"{何を実装したか} {作業内容を簡潔に表現}"` とする。
