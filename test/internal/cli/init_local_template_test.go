package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 版を上げた利用者がいちばん通る組み合わせ（WORKFLOW.md と continuo-ci.yaml が在り、雛形が無い）で、
// `continuo init` が雛形だけを置いて 0 で終えることを確かめる（issue #325 の実装レビュー1周目）。
//
// **これまでは2枚とも在ると `--force` を勧めて 1 で終えた。**いまは足りない雛形を置いて 0 で終える。
// **`--force` を要求してはならない**（手で書いた WORKFLOW.md の本文を潰す。設計 5-3g）。
//
// 与える情報: WORKFLOW.md（本文に人間が足した行を入れる）と continuo-ci.yaml を置いたディレクトリ。
// 成功条件: 終了コードが 0 で、雛形が増え、WORKFLOW.md に足した行が消えていないこと。
func TestInit_2枚が在れば雛形だけを置いて0で終える(t *testing.T) {
	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "\n# 人間が手で足した行\n"
	if err := os.WriteFile(path, []byte(readFile(t, path)+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("%s を置けません: %v", scaffold.CIFileName(), err)
	}

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（0 であるべきです。stderr: %s）", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.LocalTemplateFileName())); err != nil {
		t.Errorf("%s が置かれていません: %v", scaffold.LocalTemplateFileName(), err)
	}
	if !strings.Contains(stdout, scaffold.LocalTemplateFileName()) {
		t.Errorf("標準出力に %s の行がありません: %q", scaffold.LocalTemplateFileName(), stdout)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, mark) {
		t.Error("人間が足した行が消えています")
	}
}

// 目的: WORKFLOW.local.md が既にあっても雛形を置いて 0 で終え、WORKFLOW.local.md には触らないことを確かめる
// （人間の依頼「continuo init する時に WORKFLOW.md と WORKFLOW.local.md.template を出力し」。issue #325 の実装レビュー2周目）。
//
// **既にある WORKFLOW.local.md を守るのは、写し方の案内（cp -n）である。**案内が `mv` だと、写した瞬間に消える。
//
// 与える情報: WORKFLOW.md・continuo-ci.yaml・WORKFLOW.local.md を置き、雛形だけが無いディレクトリ。
// 成功条件: 終了コードが 0 で、雛形が置かれ、案内が cp -n を勧め、WORKFLOW.local.md が1バイトも変わらないこと。
func TestInit_WORKFLOWlocalmdが在っても雛形を置き触らない(t *testing.T) {
	dir := writeWorkflowFor(t)
	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("%s を置けません: %v", scaffold.CIFileName(), err)
	}
	local := filepath.Join(dir, "WORKFLOW.local.md")
	const mine = "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 30\n---\n"
	if err := os.WriteFile(local, []byte(mine), 0o600); err != nil {
		t.Fatalf("WORKFLOW.local.md を書けません: %v", err)
	}

	code, stdout, stderr := runInitOffline(dir)
	if code != 0 {
		t.Fatalf("終了コードが %d です（0 であるべきです。stderr: %s）", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, scaffold.LocalTemplateFileName())); err != nil {
		t.Errorf("%s が置かれていません: %v", scaffold.LocalTemplateFileName(), err)
	}
	if !strings.Contains(stdout, "cp -n WORKFLOW.local.md.template WORKFLOW.local.md") {
		t.Errorf("写し方の案内が cp -n を勧めていません: %q", stdout)
	}
	if got := readFile(t, local); got != mine {
		t.Errorf("WORKFLOW.local.md が変わっています: %q", got)
	}
}
