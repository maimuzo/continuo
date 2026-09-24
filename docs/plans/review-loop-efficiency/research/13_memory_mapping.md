# メモリの移した先の対応表

**言いたいこと。**このリポジトリを直すときのメモリ（`~/.claude/projects/（このリポジトリ）/memory/`）124件のうち、
プラグインと組み込みの指示書へ移した部分だけを消した（2026-09-24）。**移していない部分は、ファイルの中に残してある。**
内訳は、移した部分を消した 45件・触れていない 79件である。丸ごと消したものは無い。

**判定のしかた。**1件ずつ本文の全行を「移った / 移っていない」に振り分け、移った行には移した先の行を添えさせた（読むだけの worker 4人）。
**1度目の判定が甘く、移した先に無い決まりまで消していた。**そこで消した行を全部「同じ決まりが、同じ範囲で書いてあるか」で照らし直し、無いものを戻した。
**組み込みの指示書は、このリポジトリを直す AI に効く 5-6・5-7 だけを、移した先に数えた。**
**移した先と逆のことを言っている行のうち、どちらが新しいかを決められなかったものは「移っていない」として残した。**
**どちらにも振り分けられていない行も、消さずに残した。**

**移した先の名前。**`general-claude-md`・`issue-management`・`docs-standard` は `maimuzo-dev-core` の、`chat-response` は `maimuzo-chat-response` のスキルである。
組み込みの指示書は [internal/prompt/builtin.md](../../../../internal/prompt/builtin.md)。

| メモリ | どうしたか | 移した先 |
| --- | --- | --- |
| feedback_adversarial_review_needs_reasons | 移した部分を消した | 組み込みの指示書（5-6・5-7） |
| feedback_adversarial_reviewer_decides | 触れていない | ― |
| feedback_agent_review_response_format | 移した部分を消した | 組み込みの指示書（5-6・5-7）／CLAUDE.md |
| feedback_always_explain_with_sequence_diagrams | 移した部分を消した | ― |
| feedback_always_record_to_plan_file | 移した部分を消した | CLAUDE.md |
| feedback_always_write_issue_pr_title_verbatim | 移した部分を消した | CLAUDE.md |
| feedback_apologize_when_you_are_wrong | 触れていない | ― |
| feedback_ask_via_issue_comment_not_files | 触れていない | ― |
| feedback_build_after_every_code_change | 触れていない | ― |
| feedback_bulk_replace_protects_six_contexts | 触れていない | ― |
| feedback_caffeinate_while_agents_run | 触れていない | ― |
| feedback_check_overfit_after_many_reviews | 移した部分を消した | 組み込みの指示書（5-6・5-7） |
| feedback_check_plan_and_history_first | 触れていない | ― |
| feedback_check_primary_source_first | 触れていない | ― |
| feedback_check_reply_before_sending | 移した部分を消した | ― |
| feedback_check_sudo_capabilities_first | 触れていない | ― |
| feedback_clean_up_what_you_created | 移した部分を消した | CLAUDE.md |
| feedback_close_issue_with_pr_and_fix | 触れていない | ― |
| feedback_code_review_before_pr_ready | 移した部分を消した | CLAUDE.md |
| feedback_compare_and_recommend_never_decide | 移した部分を消した | ― |
| feedback_complete_means_all_questions_resolved | 触れていない | ― |
| feedback_cowork_term | 触れていない | ― |
| feedback_define_jargon_in_context | 移した部分を消した | ― |
| feedback_explain_as_fact_consequence_decision | 触れていない | ― |
| feedback_explain_detail | 移した部分を消した | ― |
| feedback_explicit_permission_before_implementation | 触れていない | ― |
| feedback_file_path_markdown_link | 移した部分を消した | ― |
| feedback_fix_whats_wrong_dont_ask | 触れていない | ― |
| feedback_full_deployment_chain_before_proposing | 触れていない | ― |
| feedback_glob_yaml_yml | 触れていない | ― |
| feedback_hand_the_human_a_one_liner | 移した部分を消した | ― |
| feedback_human_only_writes_issues_and_reviews | 触れていない | ― |
| feedback_investigate_workarounds_before_declaring_impossible | 触れていない | ― |
| feedback_isolate_conflicting_work_in_worktrees | 移した部分を消した | `general-claude-md` |
| feedback_issue_creation_is_not_permission_to_start | 移した部分を消した | `general-claude-md` |
| feedback_issue_creation_needs_human_approval | 触れていない | ― |
| feedback_issue_titles_must_be_self_explanatory | 触れていない | ― |
| feedback_japanese_background_term | 触れていない | ― |
| feedback_long_waits_must_run_in_background | 触れていない | ― |
| feedback_measure_before_you_write | 移した部分を消した | `chat-response` |
| feedback_merge_permission_is_not_rule_exemption | 移した部分を消した | CLAUDE.md |
| feedback_mermaid_rect_rgba | 移した部分を消した | `general-claude-md` |
| feedback_mermaid_validation_order | 移した部分を消した | `general-claude-md` |
| feedback_minimal_bold_emphasis | 触れていない | ― |
| feedback_never_block_on_autonomous_work | 触れていない | ― |
| feedback_never_cite_rules_by_number_alone | 移した部分を消した | `chat-response` |
| feedback_never_pipe_test_output_to_tail | 触れていない | ― |
| feedback_never_put_maskable_info_in_titles | 移した部分を消した | `general-claude-md` |
| feedback_never_reference_issues_by_number_alone | 移した部分を消した | `chat-response` |
| feedback_never_say_workflow_alone_in_continuo | 触れていない | ― |
| feedback_no_ascii_art_use_mermaid | 移した部分を消した | `general-claude-md` |
| feedback_no_ask_tool_for_design_decisions | 触れていない | ― |
| feedback_no_claude_binary_analysis | 触れていない | ― |
| feedback_no_compat_design_for_nonexistent_artifacts | 触れていない | ― |
| feedback_no_japanese_translation_of_tech_terms | 触れていない | ― |
| feedback_no_scope_free_claims | 触れていない | ― |
| feedback_no_scratchpad_for_research | 触れていない | ― |
| feedback_no_unilateral_design_changes | 触れていない | ― |
| feedback_no_unverified_mechanism_as_truth | 触れていない | ― |
| feedback_parallel_by_default | 移した部分を消した | ― |
| feedback_plan_file_holds_decisions_not_history | 移した部分を消した | ― |
| feedback_pr_draft_only_when_blocked | 触れていない | ― |
| feedback_prove_fixes_landed_in_the_commit | 触れていない | ― |
| feedback_public_repo_only_clean_artifacts | 移した部分を消した | ― |
| feedback_read_the_source_yourself_when_you_design | 触れていない | ― |
| feedback_record_measurements_in_comments | 触れていない | ― |
| feedback_register_promises_immediately | 移した部分を消した | ― |
| feedback_release_notes_are_for_users | 触れていない | ― |
| feedback_remove_worktrees_before_finishing | 移した部分を消した | ― |
| feedback_research_agents_are_read_only | 移した部分を消した | ― |
| feedback_review_failures_mean_bad_design | 移した部分を消した | ― |
| feedback_review_loop_is_a_checkin_not_a_quota | 触れていない | ― |
| feedback_review_loop_max_three | 移した部分を消した | 組み込みの指示書（5-6・5-7） |
| feedback_review_scope_not_only_diff | 移した部分を消した | ― |
| feedback_review_the_design_before_implementing | 移した部分を消した | ― |
| feedback_run_the_binary_yourself | 触れていない | ― |
| feedback_say_ratelimit_and_marker_not_waku | 触れていない | ― |
| feedback_self_contained_command_blocks | 触れていない | ― |
| feedback_self_serve_before_delegating_to_human | 触れていない | ― |
| feedback_separate_current_state_from_proposal | 触れていない | ― |
| feedback_shell_var_before_fullwidth | 触れていない | ― |
| feedback_skills_must_be_generic | 触れていない | ― |
| feedback_spec_first_never_reject_human_request | 触れていない | ― |
| feedback_split_prs_merge_one_first | 触れていない | ― |
| feedback_staged_flow_human_checkpoints | 触れていない | ― |
| feedback_standing_instructions_stay_until_revoked | 触れていない | ― |
| feedback_state_purpose_not_just_request | 触れていない | ― |
| feedback_state_what_youre_answering | 移した部分を消した | ― |
| feedback_supervise_dont_execute | 移した部分を消した | ― |
| feedback_supervise_workers_not_relay | 移した部分を消した | ― |
| feedback_todo_completion_needs_human_ok | 触れていない | ― |
| feedback_track_all_requests_in_todo | 触れていない | ― |
| feedback_translate_english_quotes | 触れていない | ― |
| feedback_use_real_names_not_my_own_words | 触れていない | ― |
| feedback_use_specified_plan_file | 触れていない | ― |
| feedback_verify_in_a_disposable_env_before_writing | 触れていない | ― |
| feedback_verify_oss_status_properly | 触れていない | ― |
| feedback_verify_own_spec_consistency_first | 触れていない | ― |
| feedback_verify_worker_findings_always | 触れていない | ― |
| feedback_when_to_confirm_decision_framework | 触れていない | ― |
| feedback_word_question_means_explanation_failed | 触れていない | ― |
| feedback_worker_context_both_sides | 移した部分を消した | `chat-response` |
| feedback_workers_record_progress_to_plan | 触れていない | ― |
| feedback_workflow_args_stringified | 触れていない | ― |
| feedback_write_decisions_to_plan_immediately | 触れていない | ― |
| feedback_write_so_first_reader_understands | 移した部分を消した | ― |
| feedback_write_the_comment_before_blocked | 触れていない | ― |
| project_all_issues_on_board | 移した部分を消した | ― |
| project_board_scope_frozen | 触れていない | ― |
| project_breaking_changes_are_allowed | 触れていない | ― |
| project_continuo_halted_until_issue60 | 触れていない | ― |
| project_do_not_touch_kanban_until_told | 触れていない | ― |
| project_issue_group_representative | 移した部分を消した | ― |
| project_kanban_is_operated_by_ai | 移した部分を消した | ― |
| project_no_local_workaround_for_bashfirst | 触れていない | ― |
| project_rejected_candidates_are_final | 触れていない | ― |
| reference_agent_isolation_worktree_base_is_default_branch | 触れていない | ― |
| reference_auto_mode_rm_denied | 触れていない | ― |
| reference_herdr_080_status_display | 移した部分を消した | ― |
| reference_herdr_api_schema | 触れていない | ― |
| reference_macos_tcc_desktop_blocked | 触れていない | ― |
| reference_projectv2_select_option_reset | 移した部分を消した | ― |
| reference_python39_long_line_tokenizer | 触れていない | ― |
| reference_what_consumes_quota | 移した部分を消した | ― |
