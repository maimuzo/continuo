package doctor_test

// continuo doctor が WORKFLOW.local.md（設計 5-1a / issue #325）を名指すことを確かめる。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// 目的: WORKFLOW.local.md を権限で読めないとき、直し方の `ls -l` が WORKFLOW.local.md の1つのパスだけを名指すことを確かめる。
// 2つのファイルを名指す句を入れると、貼っても叩けないコマンドになる（実装レビュー1周目の指摘）。
// 与える情報: 権限を 0 にした WORKFLOW.local.md。
// 成功条件: 設定ファイルが ✗ で、直し方に「`ls -l <WORKFLOW.local.md のパス>`」がそのまま入り、WORKFLOW.md のパスは入らないこと。
func TestDoctorLocal_読めないWORKFLOW_local_mdは直し方でそのパスだけを名指す(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく読めるので、この検査は成立しない")
	}
	fx := newFixture(t)
	localPath := filepath.Join(filepath.Dir(fx.WorkflowPath), "WORKFLOW.local.md")
	if err := os.WriteFile(localPath, []byte("---\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(localPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(localPath, 0o600) })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "`ls -l "+localPath+"`") {
		t.Fatalf("直し方が WORKFLOW.local.md の1つのパスで ls -l を出していない: %v", res.Remedies)
	}
	if strings.Contains(remedies, fx.WorkflowPath+"`") || strings.Contains(remedies, fx.WorkflowPath+" ") {
		t.Fatalf("直し方が WORKFLOW.md を名指している: %v", res.Remedies)
	}
}

// 目的: WORKFLOW.local.md を重ねたとき、設定ファイルの行が両方のファイルを名指すことを確かめる。
// 与える情報: 何も上書きしない WORKFLOW.local.md。
// 成功条件: 設定ファイルが ✓ で、説明に WORKFLOW.md と WORKFLOW.local.md の両方のパスが入ること。
func TestDoctorLocal_重ねたら設定ファイルの行が両方を名指す(t *testing.T) {
	fx := newFixture(t)
	localPath := filepath.Join(filepath.Dir(fx.WorkflowPath), "WORKFLOW.local.md")
	if err := os.WriteFile(localPath, []byte("---\n# 個人の上書きはまだ無い\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolOK)
	if !strings.Contains(res.Detail, fx.WorkflowPath) || !strings.Contains(res.Detail, localPath) {
		t.Fatalf("設定ファイルの行が両方を名指していない: %q", res.Detail)
	}
}
