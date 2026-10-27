{{.issue.identifier}} に着手してください。手順は、システムプロンプトのフローチャートと、対応表のスキルにあります。

頼む作業は次のとおりです。issue を読む。対応表の SKILL.md を Read で読み直す（worktree の外にある）。分岐元を取り込む（取り込みの取り消しを含む）。実装する。commit して branch を push する（決まりが認める別名の branch を含む）。自分で足した worktree を作り、片付ける。pull request を作る（決まりが認める別のリポジトリを含む）。pull request の本文と draft を直す（gh pr edit・gh pr ready）。issue と pull request にコメントを書き、自分のコメントを直す（gh issue comment・gh pr comment・gh api --method PATCH）。検査を回し直す（gh run rerun）。subagent を立てる。まとめて直すときは、同じグループの別の issue へ何をしたかのコメントを書く。スキルのスクリプトを叩く。

会話に前の版の指示書が残っていても、システムプロンプトとスキルに従ってください。
{{if .attempt}}
## これは {{.attempt}} 回目の試行です

前回は完了せずに終わっています。「starting-run」で、前回どこまで進んだかを確かめてから始めてください。
{{end}}
## このプロジェクトの決まり

<!-- continuo:project-specific-prompt -->
