<!-- 目的: レビューループの効率化の材料として、リポジトリの外（maimuzo の個人環境）でレビューループを定義している箇所を一覧にする -->

# レビューループの定義の棚卸し（リポジトリの外）

**この文書は maimuzo の個人環境の棚卸しである。**このリポジトリを clone した人の手元には、ここに挙げるメモリも個人のプラグインも無い
（[CLAUDE.md](../../../../CLAUDE.md) の「不特定多数の環境と、maimuzo の環境を混同しない」）。

**調べた時点。**2026-09-15。worktree の HEAD は df36f9d7。プラグインは cache に展開されている版を読んだ。

**表の中のパスの書き方。**長いので、次の前置きで書く。

| 表での書き方 | 実際のパス |
| --- | --- |
| メモリ/ | ~/.claude/projects/（continuo のプロジェクトのディレクトリ）/memory/ 。ディレクトリ名に利用者名が入るので伏せた |
| codex/ | ~/.claude/plugins/cache/openai-codex/codex/1.0.6/ |
| maimuzo/ | ~/.claude/plugins/cache/maimuzo-marketplace/ |
| ecc/ | ~/.claude/plugins/cache/everything-claude-code/everything-claude-code/1.9.0/ |
| mattpocock/ | ~/.claude/plugins/cache/mattpocock/mattpocock-skills/1.2.3/ |
| continuo/ | ~/Sources/github/continuo/ |

---

## 0. worker-briefing の問いへの答え

**1. この作業に当てはまる規則。**

- [worker-briefing 2-4](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L161)（指示に名前が出ていないものも探して読む）。
  呼ぶ側が挙げた範囲に加え、~/.claude/skills・~/.claude/hooks・~/.claude/settings.local.json・~/.claude.json・continuo/.claude/local-guidelines.md・リポジトリ側の Stop hook の止め方まで読んだ
- [worker-briefing 2-5](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L183)（同じものが他に無いかを数える）。食い違いの文言ごとの件数と検索パターンを「8」に書いた
- [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) の「worker への指示に必ず入れること（根拠の付け方）」。主張ごとに場所・原文・コマンドの出力を付けた
- [CLAUDE.md](../../../../CLAUDE.md) の「不特定多数の環境と、maimuzo の環境を混同しない」「公開してよい情報かを常に判断する」「`~/.claude/projects/` 配下を消さない」（読むだけにした）

**2. 飛ばしてよい段。**

- [.claude/rules/design-review.md:9-17](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L9-L17) の9段は issue の作業の手順である。この作業は設計も実装もしないので通らない
- commit と PR の段は、呼ぶ側の指示が禁じている
- [worker-briefing 2-6](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L256-L258) は「この節は、コードレビューや設計レビューを頼まれた worker に効く。」とある。この作業は棚卸しなので直接は当たらない。ただし「4」の食い違いの表には、2-7 に倣って「誰が何を失うか」を書いた

**3. 公開してよくない情報を書きうる場面。**この文書は公開リポジトリの worktree に置かれる。

- メモリのディレクトリ名に利用者名が入る。上の表のとおり伏せた
- ~/.claude.json には内部の設定値が多数ある。レビューに関係する利用回数だけを書き、それ以外は書いていない
- ~/.claude/settings.json の hook に個人のツールの名前がある。レビューに関係しないので名前を書いていない
- トークン・認証情報は1つも書いていない

**4. どう探し、何を読んだか。**

- **メモリ。**`ls | wc -l` で118件を出した。`grep -c -i -E 'レビュー|review|code-review|指摘|周目|収ま|critical|high'` で件数順に並べ、上位（37〜5件）の13ファイルは全文を読み、残りは該当行だけを読んだ。索引に無いファイルが5件あった（`feedback_review_loop_is_a_checkin_not_a_quota.md` を含む）
- **settings。**python で ~/.claude/settings.json・~/.claude/settings.local.json・continuo/.claude/settings.json・continuo/.claude/settings.local.json・worktree の .claude/settings.json から、hooks と enabledPlugins を出した
- **プラグイン。**~/.claude/plugins/installed_plugins.json から continuo に入っている版と導入先を出した。cache の中を `grep -rl -i -E 'review|レビュー|反復|iteration|収束|converge'` で列挙し、該当するファイルを読んだ
- **codex の gate。**hooks.json → stop-review-gate-hook.mjs → lib/state.mjs → 状態ファイルの実物、の順に辿った
- **同梱の `/code-review`。**公式文書の https://code.claude.com/docs/en/code-review と https://code.claude.com/docs/en/commands を読んだ
- **読めなかったもの。**Claude Code 本体は解析が禁止なので読んでいない。そのため `/code-review` の内部の依頼文は確かめていない。
  everything-claude-code がこのセッションに読み込まれていない理由は、~/.claude/debug の直近のログを grep したが手がかりが無かった。
  `/code-review` が再利用する「前回打った effort level」の保存先は、~/.claude.json をキー名 `review|effort` で探したが見つからなかった

---

## 1. 言いたいこと

**言いたいこと。**リポジトリの外にも、レビューを起こす定義・止める条件・受けた側への指示が散らばっている。
メモリの6ファイルと索引1行はリポジトリの現行の規則と違うことを言っており、読んだ AI が古い止め方・直し方で動く余地がある。
プラグインには、このリポジトリの規則に0件の「確信度で絞る」「検証してから出す」「2回目以降は重い指摘だけ出す」がある。

**決めてほしいことは無い。**この文書は材料である。

| いちばん重い発見 | どこに書いたか |
| --- | --- |
| **codex の Stop 時のレビュー（stop-time review gate）は無効である。**continuo の状態ファイルで `stopReviewGate` が false、ジョブは0件。今回の周回の原因ではない | 3-3 |
| **リポジトリの手順 `/code-review <PR 番号>` は effort level を指定していない。**公式文書によれば、指定しないと前回打った level、打ったことが無ければセッションの effort で走る。この環境の settings の effortLevel は xhigh で、公式文書は high〜max を「確信の低い指摘も含みうる」と書く。**実際にどの level で回ったかは測っていない** | 3-5 と 7 |
| **1回のコード変更に対し、PR 時の `/code-review` とは別に、general-claude-md が「変更のたびに code-reviewer と security-reviewer」を求めている** | 6 |
| **メモリの6ファイルと索引1行が、リポジトリの規則と食い違う**（上限3回の説明文・5列の対応表・人間の返事を待つ・消さず人間に確認・議論して決める・目印の無い設計レビューの記録） | 4 |
| **確信度の閾値・検証の段・再レビューの収束・3ストライク・Spec 軸は、リポジトリの規則に0件**（git grep の結果） | 5 |

---

## 2. 範囲の確認

### 2-1. 有効なプラグイン

- continuo/.claude/settings.local.json の enabledPlugins に17件あり、呼ぶ側の一覧と一致した
- **ユーザー単位で別に有効化されているものは無い。**~/.claude/settings.json の enabledPlugins は `{}`。~/.claude/settings.local.json のキーは `permissions` だけで、enabledPlugins も hooks も無い。continuo/.claude/settings.json に enabledPlugins は無い
- installed_plugins.json にはユーザー単位の導入が1件（`tq@tq-marketplace` 0.21.36）ある。ただし読んだどの settings でも有効化されておらず、このセッションの skill 一覧にも出ていない
- **このセッションの skill 一覧と agent 一覧に、everything-claude-code の項目は0件である。**enabledPlugins では true になっている。maimuzo-*・codex・mattpocock-skills の項目は出ている。理由は特定できていない
- worktree の .claude/ に settings.local.json は無い（`ls -la` の結果は hooks・rules・settings.json・skills だけ）。それでも maimuzo のプラグインはこのセッションに出ている。どの設定が効いているのかは調べていない

### 2-2. 版

- **cache と marketplace の写しの差。**レビュー系（co-review・cosper-team・auto-debug・from-ecc・chat-response）は、`diff -rq` で `.in_use` と plugin.json の version 行を除いて同じだった
- **maimuzo-from-ecc・maimuzo-go・maimuzo-ts は、cache に `0.1.0` と `f8b3c5c70f6b` の2つがある。**差は plugin.json の version 行の位置と `.in_use` だけで、agents と skills は同じ。installed_plugins.json は continuo に `0.1.0` を記録している。表では `0.1.0` のパスで書いた
- **marketplace の写しのほうが、編集用 clone より新しい。**~/.claude/plugins/marketplaces/maimuzo-marketplace の HEAD は 540a152（2026-08-31）。~/Sources/github/maimuzo-claude-plugins の main は 01f58ad（2026-08-26）で、`git merge-base --is-ancestor 01f58ad 540a152` が真を返した。
  レビュー系プラグインは両者で `diff -rq` の行数が0。差があるのは dev-core（3行。detect-usage 系の2スキルと plugin.json）と issue-manage（1行。bug-tickets-priority-analysis）だけである
- codex は cache が 1.0.6、marketplace の写しの plugin.json も 1.0.6

---

## 3. 一覧

**観点の略さない名前。**「起動条件」はいつ・何を・誰がレビューするか。「レビュワーへの指示」は徹底度・重大度・根拠・確信度。「受けた側」は直す範囲・同様の箇所・直したあとの確認。「収束」は閾値・回数・止まったら何をするか。

### 3-1. 個人指示・個人の skills と hooks・settings

| 場所 | 何を定義しているか | 原文（短く） | 区分 |
| --- | --- | --- | --- |
| ~/.claude/CLAUDE.md:1-12 | **レビューループの定義は無い。**Codex で作業するときの読み込み手順だけ（全12行を読んだ） | 「この節はCodexにのみ適用する。」 | 個人指示 |
| ~/.claude/rules/ | 存在しない | `ls: … No such file or directory` | 個人指示 |
| ~/.claude/skills/ と ~/.claude/hooks/ | レビューに触れるファイルは0件（`grep -rl -i -E 'review\|レビュー'`） | herdr への symlink・空の learned・herdr-agent-state.sh | 個人指示 |
| ~/.claude/settings.json の hooks | Stop hook は無い。SessionStart など8種はどれもレビューを起こさない（herdr の状態通知と、個人の記録用ツール） | — | settings |
| ~/.claude/settings.json の effortLevel | セッションの既定の effort。`/code-review` に level を打ったことが無いときに使われる（3-5） | `effortLevel= xhigh` | settings |
| continuo/.claude/settings.json の hooks.Stop | 返答を差し戻す Stop hook が2本（中身はリポジトリの担当なので、止め方だけ見た）。どちらも `stop_hook_active` が真なら何もしない | check-verified-commands.py:302 `if payload.get("stop_hook_active"):`、check-reply-clarity.py:984 | settings（リポジトリ） |
| continuo/.claude/settings.local.json | enabledPlugins 17件。hooks は無い | — | settings |
| continuo/.claude/local-guidelines.md:5-7（.gitignore 済み） | general-claude-md を毎セッション必ず読む対象にしている。これで 3-4 の general-claude-md の手順5が continuo でも効く | 「セッション開始時・Context compaction 後に必ず以下のスキルを読むこと。」「`maimuzo-dev-core:general-claude-md` — 言語非依存の共通ルール全文」 | 個人指示 |

### 3-2. メモリ

| 場所 | 何を定義しているか | 原文（短く） | 区分 |
| --- | --- | --- | --- |
| メモリ/MEMORY.md:76 | 索引。回数の数え方（更新済み） | 「レビューの回数は数える。3・6・9回目で突き合わせ直し、10回で止まる」 | メモリ |
| メモリ/feedback_review_loop_max_three.md:3 | **説明文（frontmatter）が古い上限のまま** | 「レビュー→修正のループは回数を数える。上限は3回。3回で通らなかったら目的の確認役を立て」 | メモリ |
| 同:11-13 | 収束しないときの回数（本文は更新済み） | 「3・6・9回目で収まらなかったら…連続10回で完全に止まり、人間に方針を確認する。」 | メモリ |
| 同:40-44 | 回ごとの対応。**3回目までしか無い** | 「3回目のあと｜収まっていれば進む。収まっていなければ4段へ入り」 | メモリ |
| 同:46-49 | 周回が収まらない原因の見立て。**`/code-review` の依頼文を引いているが、確かめる手段が無い**（7） | "You are reviewing for recall … Err on the side of surfacing."（取りこぼさないことを狙う。見つけたら出す側に倒せ）「取りこぼさないことを狙う道具に上限15件を指定している」 | メモリ |
| 同:90-94 | 受けた側への指示 | 「0件になるまで回さない」「重い指摘（Critical / High）だけ直す。中くらい以下は follow-up の issue へ切り出す」 | メモリ |
| メモリ/feedback_review_loop_is_a_checkin_not_a_quota.md:11-13 | 統合済みの抜け殻。索引に無い | 「この覚え書きは統合された。索引（MEMORY.md）からも外してある。」 | メモリ |
| メモリ/feedback_review_failures_mean_bad_design.md:25-30 | 3回目を「作り方を問う回」にする | 「レビューが2回目で同じ規模の指摘を出したら、3回目は指摘潰しの回にしない。」 | メモリ |
| メモリ/feedback_check_overfit_after_many_reviews.md:8-10, 31-35 | 掛けすぎたときに、元の依頼との乖離を別にレビューする。見つけたら人間に確認 | 「見つけても自分で消さない。表にして人間に確認する」 | メモリ |
| メモリ/feedback_adversarial_review_needs_reasons.md:29-37, 42-44 | レビュワーへの指示（合理的理由）と、依頼側の反論 | 「説明できないものは出さない」「議論して決める」「「取りこぼさないことを狙う」だけの依頼文は、理由の薄い指摘を量産する」 | メモリ |
| メモリ/feedback_adversarial_reviewer_decides.md:24-29 | 迷ったら、敵対的レビュワーを説得できるかで決める | 「否定されたら捨てる。合意できたら、盛り込むための issue を作る」 | メモリ |
| メモリ/feedback_review_the_design_before_implementing.md:13-23, 39-48 | 設計レビューの9段と、レビュワーへ渡す4観点 | 「4｜指摘を直す。直したことも issue に書く」 | メモリ |
| メモリ/feedback_code_review_before_pr_ready.md:24-31 | PR の起動条件と対応表 | 「レビューの結果は対応表で報告する（識別子・レベル・内容・修正可否・理由）。」 | メモリ |
| メモリ/feedback_agent_review_response_format.md:14-22, 44-50 | 受けた側の対応表（5列）と手順 | 「列は5つ。」「同じ表をそのまま人間へ報告し、追加の指示を待つ」 | メモリ |
| メモリ/feedback_merge_permission_is_not_rule_exemption.md:21-23 | `/code-review` を AI から起動する | 「`Skill` ツールで `code-review` を呼ぶ（引数に PR 番号）。」 | メモリ |
| メモリ/feedback_issue_creation_needs_human_approval.md:33-44, 56 | 起票の前の敵対的レビュー。**回数の上限はこのファイルに書かれていない**（全文を読んだ） | 「指摘に対応し、問題が無くなるまで直す」「起票の前に必ず1回通す」 | メモリ |
| メモリ/feedback_verify_own_spec_consistency_first.md:22 | 設計文書を直したら同じ種類の矛盾を全文で洗う。検証役への指示 | 「同じ種類の矛盾が他に無いかを全文で洗う。」「「根拠のない指摘は返すな」「原文の引用と行番号を必ず添えろ」」 | メモリ |
| メモリ/feedback_parallel_by_default.md:12-15 | レビューは並列、修正は直列にしていた反省 | 「レビューは並列に回し、その結果を1つずつ順番に直していた。」 | メモリ |
| メモリ/feedback_check_reply_before_sending.md:13-30 | 返答の書き直しの反復。レビューではないが、Stop hook の差し戻しで回る | 「実測（2026-08-30、24時間）。やり直しは60回。」 | メモリ |

### 3-3. codex（openai-codex）

| 場所 | 何を定義しているか | 原文（短く） | 区分 |
| --- | --- | --- | --- |
| codex/hooks/hooks.json:26-35 | 起動条件: 毎 turn の Stop で gate のスクリプトを走らせる（最大900秒） | `"Stop": … "stop-review-gate-hook.mjs\"", "timeout": 900` | codex |
| codex/scripts/stop-review-gate-hook.mjs:154-157 | gate が無効なら、何もせずに終わる | `if (!config.stopReviewGate) { logNote(runningTaskNote); return; }` | codex |
| 同:166-172 | 有効なら Codex にレビューさせ、失敗や BLOCK なら turn を終えさせない | `emitDecision({ decision: "block", …` | codex |
| codex/scripts/lib/state.mjs:23, 29-43 | 既定は無効。状態は作業場所（git の top）ごとに別のディレクトリへ置く | `stopReviewGate: false`、`` path.join(stateRoot, `${slug}-${hash}`) `` | codex |
| ~/.claude/plugins/data/codex-openai-codex/state/continuo-6b2284a55cb82f98/state.json | **いまの値は無効。ジョブは0件。**名前は continuo の本体のパスから計算した値と一致した。worktree の名前（review-loop-efficiency-33f809cd2eaaba30）のディレクトリは無いので、既定の無効になる | `state.config= {'stopReviewGate': False} jobs in state= 0` | codex（実測） |
| codex/prompts/stop-review-gate.md:3-8, 23-25, 29-31 | レビュワーへの指示: 直前の turn の編集だけを見る | "Only review the work from the previous Claude turn."（直前の Claude の turn の作業だけをレビューせよ）"Do not block based on older edits from earlier turns when the immediately previous turn did not itself make direct edits."（直前の turn が編集していないなら、それより前の turn の編集を理由に止めるな） | codex |
| codex/commands/review.md:4 と codex/commands/adversarial-review.md:4 | モデルからは起動できない。人間が打つ | `disable-model-invocation: true` | codex |
| codex/prompts/adversarial-review.md:38-46, 53-57, 65, 68-71 | 重大なものだけ出す・指摘ごとに確信度を付ける・弱い指摘を並べない | "Report only material findings."（重大な指摘だけを出せ）"Prefer one strong finding over several weak ones."（弱い指摘を複数並べるより、強い指摘を1つ出せ）"keep the confidence honest"（確信度を正直に付けよ） | codex |
| codex/schemas/review-output.schema.json:28-37, 68-72 | 指摘ごとに confidence（0〜1）が必須 | `"confidence": { "type": "number", "minimum": 0, "maximum": 1 }` | codex |

### 3-4. maimuzo のプラグイン

| 場所 | 何を定義しているか | 原文（短く） | 区分 |
| --- | --- | --- | --- |
| maimuzo/maimuzo-co-review/0.1.0/skills/use-co-reviewer/SKILL.md:16-17, 35-37 | 起動条件: プランモード中と明示指示のとき、人間へ送る応答をレビューに通す | 「プランモード中: ハーネスが ExitPlanMode を呼ぶまで自動有効化、抜けで自動無効化」 | co-review |
| 同:39-50 | 書く側への指示: 提出前に7項目を自分で確かめる | 「7. 過去の co-reviewer 指摘の横断反映: 直近 3 回の指摘を読み返し、同種問題が他に残っていないか全文走査」 | co-review |
| 同:52-58 | 収束しないときの止め方（3ストライク） | 「1 回目: 指摘箇所と類似パターンの欠落を全文走査してから再提出」「2 回目: …応答案を最初から full rewrite」「3 回目: …(1) レビューループ中断 … (5) 不能なら co-reviewer 通過を諦め人間に現状報告」 | co-review |
| 同:97-100 | 収束の定義: critical と high が0件なら通過 | 「exit 3（NEEDS-FIX）: stdout に critical/high 指摘が出る。3 ストライク則に従い修正 → 手順 2〜4 を再実行。」 | co-review |
| maimuzo/maimuzo-co-review/0.1.0/skills/co-reviewer/SKILL.md:25-30, 34-37 | レビュワーへの指示: 全事実を一次資料で確かめる。未確認は high。medium と low は差し戻さない | 「未確認 = high で差し戻し」「medium / low は差し戻ししない（`issues` に記録のみ。メインが修正要否を判断）。」 | co-review |
| ~/.claude.json の skillUsage | 最終利用は 2026-07-02（co-reviewer 8回、use-co-reviewer 2回） | `maimuzo-co-review:co-reviewer = {"usageCount": 8, …}` | co-review（実測） |
| maimuzo/maimuzo-cosper-team/0.1.0/skills/cosper-team/SKILL.md:109-127 | 実装しない別コンテキストの検証者が合否を構造化して返し、不合格のときだけ修正する。2回連続で収束しなければ上へ上げる | 「修正はせず指摘のみ返せ」「不合格のときだけ opus が修正する(2回連続で収束しなければオーケストレーターにエスカレート)」 | cosper-team |
| 同:131-134 | 受け入れ検査で code-reviewer と security-reviewer を併用 | 「指摘は識別子/レベル/内容/修正可否/理由の対応表にしてプランファイルへ記録し、人間へ報告する。」 | cosper-team |
| maimuzo/maimuzo-auto-debug/0.1.1/skills/debug-loop/SKILL.md:134-143 | 収束: 2反復連続で新規 issue が0件 | 「`consecutive_zero_iterations >= 2` なら 収束達成」 | auto-debug |
| 同:179-190 | 反復ごとの監査。同じ致命の違反が2反復続いたら全体を止める | 「連続 2 反復で同じ「致命」違反が解消されない場合、run 全体を `failed` 終了 (暴走防止)」 | auto-debug |
| maimuzo/maimuzo-auto-debug/0.1.1/skills/auto-fix/SKILL.md:78-93 | 修正計画を architect 役がレビューする | 「`subagent_type: general-purpose` (project 専用 architect エージェント未定義のため汎用 agent で代替)」 | auto-debug |
| 同:170-177 | 1 issue につき2回まで再試行し、3反復連続で人間へ | 「同一 issue が 3 反復連続で `architect 再検討必要` 付与 されたら `人間判断必要` に昇格」 | auto-debug |
| maimuzo/maimuzo-from-ecc/0.1.0/agents/code-reviewer.md:27-31, 88-92 | 重大度と承認基準。確信度での絞り込みは書かれていない（全104行を読んだ） | 「FAIL: ブロック: CRITICALまたはHIGH問題が見つかった」 | from-ecc |
| maimuzo/maimuzo-from-ecc/0.1.0/agents/security-reviewer.md:532-541 | 成功の指標 | 「PASS: すべての高い問題が対処されている」 | from-ecc |
| maimuzo/maimuzo-from-ecc/0.1.0/agents/architect.md:19-22 | アーキテクチャのレビュー。リポジトリの設計レビューが指名する役 | 「## アーキテクチャレビュープロセス」 | from-ecc |
| maimuzo/maimuzo-from-ecc/0.1.0/agents/gan-generator.md:3, 28 | 書く側: 閾値まで反復し、指摘は全部直す | 「エバリュエーターのフィードバック項目は提案ではない。すべて修正する。」 | from-ecc |
| maimuzo/maimuzo-from-ecc/0.1.0/agents/gan-evaluator.md:24-28, 82-97, 117, 128-134 | 評価側: 厳格に採点し、閾値は7.0。前回からの改善と退行を毎回書く | 「あなたの自然な傾向は甘くなることです。」「判定: PASS / FAIL (閾値: 7.0)」「前回のイテレーションから退行した点」 | from-ecc |
| maimuzo/maimuzo-chat-response/0.2.0/hooks/hooks.json:4-14 | 起動条件: 毎 turn の Stop で返答の5段構成を検査する | 「返答の5段構成を turn の終了直前に検査する Stop hook。」 | chat-response |
| maimuzo/maimuzo-chat-response/0.2.0/hooks/check-reply-structure.py:290-291, 320 | 1回差し戻したら、次は通す | `# 既に block した結果の再実行なら、そのまま通す（無限ループを避ける）。` | chat-response |
| maimuzo/maimuzo-chat-response/0.2.0/README.md:84-95 | 上の挙動の実測 | 「1回目は偽で block、2回目は真で、書き直された返答（5段そろい）はそのまま通っている。」 | chat-response |
| maimuzo/maimuzo-dev-core/0.4.0/skills/general-claude-md/SKILL.md:60, 106-108 | 起動条件: コードを変更するたびに code-reviewer と security-reviewer。受けた側: CRITICAL と HIGH は直し、スコープ外で放置しない | 「5. code-reviewerおよびsecurity-reviewerエージェントでコードレビューを受ける」「「スコープ外」として指摘を放置しないこと」 | dev-core |
| maimuzo/maimuzo-issue-manage/0.2.4/skills/bug-ticket-resolve/SKILL.md:102-105 | バグを直したあとに code-reviewer と security-reviewer | 「CRITICAL, HIGHの指摘は修正する。」 | issue-manage |
| maimuzo/maimuzo-issue-manage/0.2.4/skills/bug-ticket-report/SKILL.md:63-71 と bug-tickets-root-cause-analysis/SKILL.md:44-46 | 起票や調査結果を architect がレビューする。反復の定義は無い | 「architectエージェントを使って調査結果をレビューする」 | issue-manage |
| maimuzo/maimuzo-go/0.1.0/skills/ecc-agents/SKILL.md:21-22, 33, 52-59（maimuzo-ts も同じ行） | 起動条件: コードを書いた直後に code-reviewer。役割を分けた多視点の分析 | 「コード記述/変更直後 - code-reviewer エージェントを使用」「事実レビューア」「一貫性レビューア」 | go / ts |
| maimuzo/maimuzo-go/0.1.0/skills/ecc-security/SKILL.md:38-44（maimuzo-ts は 44-50） | 受けた側: 同様の問題をコードベース全体で探す | 「5. 同様の問題がないかコードベース全体をレビュー」 | go / ts |
| maimuzo/maimuzo-pencil/0.5.0/skills/screen-design/SKILL.md:334, 370 と maimuzo/maimuzo-rucm/0.6.2/skills/rucm-checker/SKILL.md:84-88 | 機械の検査を0件まで繰り返す。レビューではなく検査器。読んだ範囲（screen-design:325-372、rucm-checker:80-92）に回数の上限は無い | 「修正後は手順1を再実行し、差分0件になるまで繰り返す。」「エラー 0 件になるまで 1〜2 を繰り返す」 | pencil / rucm |
| maimuzo/maimuzo-pencil/0.5.0/skills/screen-design-check/SKILL.md:3 | 実装者とは別のエージェントが検証する | 「実装者自身ではなく別エージェントが検証することで、手抜きチェックを防止する。」 | pencil |
| maimuzo-ai-ownwork・maimuzo-naming・ecc-completion | レビューに触れるファイルは0件（上の `grep -rl` の結果に出ない）。ecc-completion の hooks/*.js に `review\|block\|decision` は0件 | — | その他 |

### 3-5. mattpocock・everything-claude-code・同梱の `/code-review`

| 場所 | 何を定義しているか | 原文（短く） | 区分 |
| --- | --- | --- | --- |
| mattpocock/skills/engineering/code-review/SKILL.md:6-11 | Standards（規約）と Spec（仕様）の2軸を、並列の subagent で見る | "Both axes run as parallel sub-agents so they don't pollute each other's context"（2軸を並列の subagent で走らせ、互いの文脈を汚さない） | mattpocock |
| 同:64, 70 | レビュワーへの指示: 400語以内。頼まれていない振る舞いを挙げる | "(b) behaviour in the diff that wasn't asked for (scope creep)"（diff の中の、頼まれていない振る舞い）"Under 400 words."（400語以内） | mattpocock |
| 同:76-78 | 2軸を混ぜて並べ替えない | "Do not merge or rerank findings"（指摘を統合したり、順位を付け直したりするな） | mattpocock |
| ecc/agents/code-reviewer.md:18-27 | 確信度80%超だけを出す。似た指摘はまとめる。変更していないコードは原則出さない | "Report if you are >80% confident it is a real issue"（本物の問題だと80%を超えて確信できるときだけ報告せよ）"Consolidate similar issues"（似た指摘は1つにまとめよ）"Skip issues in unchanged code unless they are CRITICAL security issues"（変更していないコードの問題は、重大なセキュリティ問題でない限り出すな） | ECC（このセッションには出ていない） |
| ecc/commands/code-review.md:71, 177-182 | 判定の表 | "Only MEDIUM/LOW issues, validation passes → APPROVE with comments"（MEDIUM と LOW だけで検証が通るなら、コメント付きで承認） | ECC |
| ecc/rules/common/code-review.md:9-15, 51-58, 111-115 | 起動条件と重大度。plugin.json に rules のキーは無い（キーは agents・skills・commands） | "After writing or modifying code"（コードを書いた・変えたあと） | ECC |
| ecc/hooks/hooks.json の Stop（6本） | レビューではない（整形と型検査・console.log の検査・セッション状態の保存・パターン抽出・コスト記録・通知） | "Batch format (Biome/Prettier) and typecheck (tsc) all JS/TS files edited this response"（この応答で編集した JS/TS をまとめて整形し、型を検査する） | ECC |
| このセッションの skill 一覧の説明文 | 同梱の `/code-review` の effort level | "low/medium: fewer, high-confidence findings; high→max: broader coverage, may include uncertain findings"（low と medium は確信の高い少数の指摘、high〜max は範囲が広く不確かな指摘も含みうる） | 同梱 |
| https://code.claude.com/docs/en/code-review の「Tune effort and arguments」 | level を指定しないときの決まり方 | "When you don't type a level, the review reuses the last level from low through max you typed, even in an earlier session"（level を打たないと、前のセッションも含め、最後に打った low〜max の level を再利用する）"If you've never typed a level, the review uses the session's current effort."（一度も打ったことが無ければ、セッションのいまの effort を使う） | 同梱（公式文書） |
| 同「What the review reads and edits」 | ローカルの `/code-review` は CLAUDE.md に従い、REVIEW.md は読まない | "The review follows your CLAUDE.md like any Claude Code session, but it doesn't read REVIEW.md."（ほかのセッションと同じく CLAUDE.md には従うが、REVIEW.md は読まない） | 同梱（公式文書） |
| 同「Review a diff locally」 | バックグラウンドの subagent として走る | "The review runs as a background subagent with its own context window"（自分のコンテキストを持つ、バックグラウンドの subagent として走る） | 同梱（公式文書） |
| ~/.claude.json の skillUsage.code-review | 利用回数と最終利用。全プロジェクトの合計かどうかは確かめていない | `usageCount: 246`、最終利用 2026-09-15 21:36 | 同梱（実測） |

---

## 4. メモリとリポジトリの規則の食い違い

**言いたいこと。**メモリの6ファイルと索引1行が、リポジトリの現行の規則と逆のことを言っている。
メモリは会話ごとに注入されるので、読んだ AI がリポジトリの規則より先にこちらで動く余地がある。
いちばん効くのは「人間の返事を待つ」と「消さず人間に確認」で、どちらも周回を止めずに人間待ちを増やす。

| メモリの場所と原文 | リポジトリの規則の場所と原文 | 食い違いで誰が何を失うか |
| --- | --- | --- |
| feedback_review_loop_max_three.md:3「上限は3回。3回で通らなかったら目的の確認役を立て」 | [CLAUDE.md:668](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L668)「どちらかが連続10回で収まらなかったら、そこで完全に止めて人間の返事を待つ。」 | 説明文だけで当たりを付けた AI が、3回で止まるか、4回目以降の手順を知らないまま回す。本文 11-13 と索引 MEMORY.md:76 は更新済みで、説明文だけが残っている |
| 同:40-44 の表が3回目で終わる。「収まったあと最大1周」は、このファイルに無い（全文を読んだ） | [CLAUDE.md:566](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L566)「「収まっている」とは何か」、[CLAUDE.md:573](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L573)「収まったあと、何周回すか」 | メモリだけを読んだ AI は、Critical と High が0件になったあとも Medium と Low のために回す。2026-09-05 に人間が止めさせた周回が戻る |
| 同:92「重い指摘（Critical / High）だけ直す。中くらい以下は follow-up の issue へ切り出す」 | [CLAUDE.md:545](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L545)「Critical と High は直す。それ以下は、簡単に直るなら直し、設計に触るなら follow-up の issue へ切り出す。」 | どちらを読むかで、簡単な Medium と Low を直すかが変わる。直すと「最後に1回」が発火するので、最後の1周の有無まで変わる |
| feedback_agent_review_response_format.md:14「列は5つ。」、feedback_code_review_before_pr_ready.md:30「（識別子・レベル・内容・修正可否・理由）」 | [CLAUDE.md:543](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L543) の「分類」列（前の周に既に在ったか、前の周の直しが持ち込んだか） | 分類の列が落ちる。周が増えた原因が「前の周のレビューの見落とし」か「直しが持ち込んだ誤り」かを数えられず、効率化の打ち手を選べない |
| feedback_agent_review_response_format.md:50「同じ表をそのまま人間へ報告し、追加の指示を待つ」 | [CLAUDE.md:527](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L527)「同じ表をそのまま人間へ報告する。返事は待たずに次を回す」 | メモリに従うと、毎周で人間待ちになる。人間の時間が周の数だけ取られる |
| feedback_check_overfit_after_many_reviews.md:35「見つけても自分で消さない。表にして人間に確認する」、MEMORY.md:77「見つけても消さず人間に確認」 | [CLAUDE.md:647](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L647)「説得できなかった内容は削除する。削除する内容を、対になる issue のコメントへまとめてから消す。」 | 3・6・9回目の6段で、削除せずに人間待ちになる。issue に無い仕様が残ったまま、次の周が回る |
| feedback_review_the_design_before_implementing.md:18「指摘を直す。直したことも issue に書く」 | [.claude/rules/design-review.md:12](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L12)「指摘ごとの判断票を issue のコメントに貼る（1行目を `<!-- design-review-result -->` にする）。そのとおりに直す」 | 目印の無いコメントになり、[.claude/rules/design-review.md:29](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L29) の CI の検査が赤になる。直さないと決めた指摘の理由も残らない |
| feedback_adversarial_review_needs_reasons.md:34-35「納得できない指摘には、理由を付けて反論する。」「議論して決める。」 | [.claude/rules/design-review.md:124](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L124)「絶対条件：合理的根拠を否定できるなら、直さない」、[.claude/rules/design-review.md:146](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L146)「`/code-review` では渡せない。」 | `/code-review` には反論を返す経路が無い。メモリの「議論」をやろうとすると、手段が無いまま周が増えるか、反論を人間へ回すことになる |

**食い違いではないが、上限が書かれていないループ。**
feedback_issue_creation_needs_human_approval.md:39「指摘に対応し、問題が無くなるまで直す」は、起票の前の敵対的レビューである。
このファイルに回数の上限は無い（全文を読んだ）。リポジトリの規則が数えるのは設計レビューと実装レビューの2つで、起票の前のレビューをどう数えるかは、この棚卸しでは調べていない。

---

## 5. プラグインの中の考え方で、このリポジトリの規則に無いもの

**数えた範囲。**worktree の df36f9d7 で `git grep -n -E -- <パターン> -- CLAUDE.md .claude/rules .claude/skills` を叩いた。件数は「8」にもある。

| 考え方 | プラグイン側の原文と場所 | リポジトリの規則で探した結果 |
| --- | --- | --- |
| **確信度の閾値で、出す指摘を絞る** | ecc/agents/code-reviewer.md:18-27 の ">80% confident"（80%を超える確信）。codex/schemas/review-output.schema.json:68-72 の confidence。同梱の `/code-review` の low と medium（3-5） | `confiden\|確信度\|80%` で0件 |
| **指摘を出す前に、実際の挙動と突き合わせる検証の段** | 公式の Code Review（GitHub の有料機能）"a verification step checks candidates against actual code behavior to filter out false positives"（候補を実際のコードの挙動と突き合わせ、偽陽性を落とす検証の段がある）。同 "Verification bar: … behavior claims need a file:line citation in the source, not an inference from naming"（振る舞いの主張には、名前からの推測ではなく file:line の引用を求める）。co-reviewer/SKILL.md:37「未確認 = high で差し戻し」 | 根拠を添えさせる規則はある（[worker-briefing 2-7](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L277)）。**レビュワーとは別の役が、指摘を再現してから採る段**は `fresh-context\|別コンテキスト` で0件。「検証」の語そのものでは数えていない |
| **2回目以降のレビューでは、重い指摘だけを出させる** | 公式の Code Review "Re-review convergence: … "after the first review, suppress new nits and post Important findings only" stops a one-line fix from reaching round seven on style alone."（初回のあとは新しい nit を抑えて Important だけ出す、という規則で、1行の直しがスタイルだけで7周目に達するのを防ぐ） | 受けた側の「収まったら最大1周」は在る（[CLAUDE.md:573](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L573)）。**レビュワー側に出す指摘を絞らせる規則**は、nit と件数の上限のパターンで0件。`/code-review` にはプロンプトを渡せない（[.claude/rules/design-review.md:146](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L146)） |
| **PR が持ち込んでいない既存のバグを、別の重大度に分ける** | 公式の Code Review "Pre-existing: A bug that exists in the codebase but was not introduced by this PR"（この PR が持ち込んだのではない、コードベースに既に在るバグ） | `Pre-existing\|pre-existing\|既存のバグ\|前から在ったバグ` で0件。近いのは分類の列（[CLAUDE.md:543](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L543)）だが、分けているのは「前の周に在ったか」で、「PR の前から在ったか」ではない |
| **1回に出す軽い指摘の件数に上限を置き、残りは件数だけ書く** | 公式の Code Review "Nit volume: cap how many 🟡 Nit comments a single review posts."（1回のレビューで出す Nit の数に上限を置く） | nit と件数の上限のパターンで0件 |
| **差し戻しの回ごとに直し方を変える**（1回目は類似パターンの全文走査、2回目は全面の書き直し、3回目は中断） | use-co-reviewer/SKILL.md:52-58 | `3 ストライク\|ストライク` で0件 |
| **実装しない別コンテキストの検証者が、合否と指摘だけを構造化して返す** | cosper-team/SKILL.md:109-127「修正はせず指摘のみ返せ」、schema `{ pass, issues }` | `fresh-context\|別コンテキスト` で0件 |
| **前回からの退行を毎回書かせる** | gan-evaluator.md:131「前回のイテレーションから退行した点」 | `退行` で0件 |
| **仕様の軸で「頼まれていない振る舞い」を、毎回のレビューで挙げさせる** | mattpocock/skills/engineering/code-review/SKILL.md:70 "(b) behaviour in the diff that wasn't asked for (scope creep)" | `Standards\|Spec 軸\|scope creep\|頼まれていない` で0件。リポジトリでは3・6・9回目の6段で目的の確認役が行う（[CLAUDE.md](../../../../CLAUDE.md) の節）ので、毎回ではない |
| **再レビューの対象を、直前の変更に絞る** | codex/prompts/stop-review-gate.md:29-31 "Do not block based on older edits from earlier turns"（それより前の turn の編集を理由に止めるな） | **数えていない** |
| **`/code-review` の effort level を決める** | このセッションの skill 一覧の説明文と、公式文書の「Tune effort and arguments」（3-5） | `effort\|/code-review (low\|…\|ultra)\|code-review ultra` で2件。[.claude/skills/pr-review-and-merge/SKILL.md:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99) は受け取る引数の説明、[同:130-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L130-L131) は ultra を人間の指示のときだけ使うという規則。**段2のコマンド（[同:95-96](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L95-L96)）は level を指定していない** |

**材料にならないもの。**REVIEW.md は、公式文書が「ローカルの `/code-review` は読まない」と書いている（3-5）。リポジトリの規則にも0件（`REVIEW\.md`）。

---

## 6. 重複して走りうるレビュー

**言いたいこと。**1回のコード変更に対して、PR 時の `/code-review` と、変更のたびの code-reviewer と security-reviewer が別々に起きうる。
bug-ticket-resolve と cosper-team を使えば、さらに重なる。codex の gate は無効で、everything-claude-code は読み込まれていない。
返答を差し戻す Stop hook は3本あるが、コードから読むと1回の差し戻しで3本とも黙る。

| 仕組み | いつ起きるか（原文と場所） | いま有効か（根拠） |
| --- | --- | --- |
| **同梱の `/code-review`** | PR を出すとき（[CLAUDE.md](../../../../CLAUDE.md) の「PR を出すときの絶対条件」）。コマンドは [.claude/skills/pr-review-and-merge/SKILL.md:95-96](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L95-L96) の `/code-review <PR 番号>` | 有効。~/.claude.json の skillUsage で246回、最終 2026-09-15 |
| **maimuzo-from-ecc の code-reviewer と security-reviewer**（general-claude-md の手順5） | general-claude-md/SKILL.md:106-108「コードを変更するたびに以下を実行: … 5. code-reviewerおよびsecurity-reviewerエージェントでコードレビューを受ける」 | general-claude-md は continuo/.claude/local-guidelines.md:5-7 で毎セッション必読。**実際に何回呼ばれたかは測っていない** |
| **bug-ticket-resolve の Step 5** | bug-ticket-resolve/SKILL.md:102-105 | そのスキルを使ったときだけ。使われた回数は測っていない |
| **cosper-team の Verify と受け入れ検査** | cosper-team/SKILL.md:109-134 | 同上 |
| **codex の stop-time review gate** | codex/hooks/hooks.json:26-35。毎 turn の Stop | **無効。**state.json の `stopReviewGate` が False、ジョブ0件（3-3） |
| **co-review** | use-co-reviewer/SKILL.md:35-37。プランモード中と明示指示のとき | 最終利用は 2026-07-02。maimuzo の cache のうち hooks/hooks.json を持つのは chat-response だけ（`find … -name '*.json'` の結果）なので、スキルを呼ばない限り走らない |
| **everything-claude-code の code-reviewer とコマンド** | ecc/rules/common/code-review.md:9-15 | このセッションの一覧に0件。rules は plugin.json に無い |
| **返答を差し戻す Stop hook**（chat-response 1本、リポジトリ 2本） | 毎 turn の Stop | 有効。3本とも `stop_hook_active` が真なら何もしない（check-reply-structure.py:290-291、check-verified-commands.py:302、check-reply-clarity.py:984）。**コードと chat-response の README の実測から読むと、1回差し戻された直後の Stop では3本とも検査しない。**3本を同時に動かして測ってはいない。コードレビューの周は増やさないが、返答の書き直しを1回起こしうる |
| **設計や起票の前の敵対的レビュー**（メモリ） | feedback_adversarial_reviewer_decides.md:24-29、feedback_issue_creation_needs_human_approval.md:33-44 | 判断に迷ったとき・起票するとき。メモリに回数の上限は無い |

---

## 7. 測っていないこと・確かめられなかったこと

| 何を | なぜ確かめられなかったか |
| --- | --- |
| **`/code-review` が実際にどの effort level で回ったか** | 前回打った level の保存先を、~/.claude.json のキー名 `review\|effort` で探したが見つからなかった。確かめたのは ~/.claude/settings.json の effortLevel が xhigh であることだけで、セッション中に effort を変えたかは分からない |
| **`/code-review` の内部の依頼文**（feedback_review_loop_max_three.md:48-49 の "You are reviewing for recall …" と「上限15件」） | Claude Code 本体の解析が禁止されている。公式文書は effort level ごとの違い（low と medium は確信の高いものだけ、high〜max は不確かなものも含みうる）しか書いていない |
| everything-claude-code がこのセッションの一覧に出ない理由 | ~/.claude/debug の直近のログに手がかりが無かった |
| worktree に settings.local.json が無いのに、プラグインが出ている理由 | 調べていない |
| general-claude-md の手順5が、continuo の PR の周回で実際に何回呼ばれたか | 会話の履歴を数えていない |
| 人間の観測「20回ほど」の実数 | 呼ぶ側の指示どおり、別の worker の担当 |

---

## 8. 数えた記録（worker-briefing 2-5）

### メモリ（範囲: メモリ/ の全118件。`grep -rn -E`）

| 検索パターン | 件数 | どこか |
| --- | --- | --- |
| `上限は3回\|最大3回\|3回まで` | 3件 | feedback_review_loop_max_three.md:3, 51, 56。51と56は人間の発言の引用なので、食い違いは3の説明文だけ |
| `修正可否` | 1件 | feedback_code_review_before_pr_ready.md:30。feedback_agent_review_response_format.md:14 の「列は5つ」は別の書き方なので、この文字列では出ない（全文を読んで見つけた） |
| `見つけても自分で消さない\|消さず人間に確認` | 2件 | feedback_check_overfit_after_many_reviews.md:35、MEMORY.md:77 |
| `直したことも issue に書く` | 1件 | feedback_review_the_design_before_implementing.md:18 |
| `議論して決める` | 1件 | feedback_adversarial_review_needs_reasons.md:35 |
| `問題が無くなるまで\|0件になるまで\|0 件になるまで` | 2件 | feedback_review_loop_max_three.md:91（「0件になるまで回さない」で逆の意味）、feedback_issue_creation_needs_human_approval.md:39 |
| `スコープ外\|範囲外` | 2件 | feedback_when_to_confirm_decision_framework.md:65、feedback_issue_creation_needs_human_approval.md:55。どちらもレビュー指摘の扱いではない |
| 「追加の指示を待つ」 | **数えていない** | feedback_agent_review_response_format.md:50 を読んで見つけた |

**~/.claude/CLAUDE.md。**`上限は3回|最大3回|3回まで|修正可否|0件になるまで|スコープ外` で0件。

### プラグイン（範囲: 有効な17件のうち everything-claude-code を除く16件の cache の版。`*.md` と `*.json`）

**最初の数え方は誤っていた。**zsh が引用符の無い変数を空白で分けず、全パターンが0件と出た。bash の配列でパスを渡して数え直した値を書く。

| 検索パターン | 件数 | どこか |
| --- | --- | --- |
| `上限は3回\|最大3回\|3回まで\|3 ストライク` | 3件 | use-co-reviewer/SKILL.md:52, 58, 99 |
| `修正可否` | 1件 | cosper-team/SKILL.md:134 |
| `問題が無くなるまで\|0件になるまで\|0 件になるまで\|すべて修正する` | 5件 | gan-generator.md:28、screen-design/SKILL.md:334, 370、rucm/SKILL.md:200、rucm-checker/SKILL.md:88 |
| `スコープ外\|範囲外\|out of scope` | 17件 | レビュー指摘の扱いは general-claude-md/SKILL.md:60, 108 の2件。残り15件は機能の範囲の話 |
| `confident\|確信\|confidence` | 16件 | 指摘ごとの確信度は codex の4行（review-output.schema.json:35, 68、adversarial-review.md:56, 65）。rucm-judge-review の4行は判断ログの自信度。残りは無関係 |
| `同様の問題\|類似パターン\|同種問題` | 4件 | use-co-reviewer/SKILL.md:50, 54、ecc-security/SKILL.md（maimuzo-go:44、maimuzo-ts:50） |

### リポジトリの規則（範囲: worktree の df36f9d7。`git grep -n -E -- <パターン> -- CLAUDE.md .claude/rules .claude/skills`）

**最初のパターンのうち2つは誤っていた。**`effort|low|medium` は「workflow」の中の「low」に当たって38件、nit は「init」に当たって1件と出た。数え直した値を書く。

| 検索パターン | 件数 |
| --- | --- |
| `confiden\|確信度\|80%` | 0件 |
| `Pre-existing\|pre-existing\|既存のバグ\|前から在ったバグ` | 0件 |
| `REVIEW\.md` | 0件 |
| `effort\|/code-review (low\|medium\|high\|xhigh\|max\|ultra)\|code-review ultra` | 2件（pr-review-and-merge/SKILL.md:99, 130） |
| `fresh-context\|別コンテキスト` | 0件 |
| `full rewrite\|最初から書き直` | 0件 |
| `退行` | 0件 |
| `(^\|[^a-zA-Z])[Nn]its?([^a-zA-Z]\|$)\|件数の上限\|上限.件\|最大.件` | 0件 |
| `3 ストライク\|ストライク` | 0件 |
| `Standards\|Spec 軸\|scope creep\|頼まれていない` | 0件 |
