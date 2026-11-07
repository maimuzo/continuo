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

// 目的: WORKFLOW.local.md が既にあるときは、雛形を置かず、置かなかったことを出すことを確かめる
// （issue #325 の実装レビュー1周目）。
//
// **雛形を置くと、利用者が案内どおり写して、使っている WORKFLOW.local.md を上書きしかねない。**
// **使い始めた人の init が毎回雛形を置き直し、「3枚とも在る」に二度と当たらなくなる。**
//
// 与える情報: WORKFLOW.md・continuo-ci.yaml・WORKFLOW.local.md を置いたディレクトリ。
// 成功条件: 1回目（--force 無し）は 3枚とも在る扱いで 1、2回目（--force）は 0 で、どちらも雛形を置かず、
// WORKFLOW.local.md が1バイトも変わらず、置かなかったことが標準出力に出ること。
func TestInit_WORKFLOWlocalmdが在れば雛形を置かない(t *testing.T) {
	dir := writeWorkflowFor(t)
	if _, err := scaffold.WriteCIWorkflowWithValues(dir, false, scaffold.Values{}); err != nil {
		t.Fatalf("%s を置けません: %v", scaffold.CIFileName(), err)
	}
	local := filepath.Join(dir, "WORKFLOW.local.md")
	const mine = "---\ntracker:\n  provider:\n    handoff:\n      weekly_margin_percent: 30\n---\n"
	if err := os.WriteFile(local, []byte(mine), 0o600); err != nil {
		t.Fatalf("WORKFLOW.local.md を書けません: %v", err)
	}

	code, _, stderr := runInitOffline(dir)
	if code != 1 {
		t.Errorf("終了コードが %d です（3枚とも在る扱いで 1 であるべきです）: %q", code, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("--force の案内がありません: %q", stderr)
	}

	code, stdout, stderr := runInitOffline("--force", dir)
	if code != 0 {
		t.Fatalf("--force の終了コードが %d です（0 であるべきです。stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, local) {
		t.Errorf("雛形を置かなかったことと WORKFLOW.local.md の場所が出ていません: %q", stdout)
	}
	if _, err := os.Lstat(filepath.Join(dir, scaffold.LocalTemplateFileName())); !os.IsNotExist(err) {
		t.Errorf("WORKFLOW.local.md が在るのに雛形を置いています: %v", err)
	}
	if got := readFile(t, local); got != mine {
		t.Errorf("WORKFLOW.local.md が変わっています: %q", got)
	}
}
