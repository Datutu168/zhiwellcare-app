package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequiredMigrationsPresent 仓库真实的 migrations 目录必须覆盖 RequiredMigrations 全部文件。
//
// 这条用例是「改了文件名却忘了同步清单」的兜底：一旦重命名（如 004_content.sql → 004_contents.sql），
// 启动守卫会拒绝启动，而这里能在 CI 阶段就指出到底是清单旧了还是文件真的丢了。
// 不连数据库。
func TestRequiredMigrationsPresent(t *testing.T) {
	dir := migrationsDir(t)
	for _, name := range RequiredMigrations {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("必需迁移 %s 未出现在 %s: %v", name, dir, err)
		}
	}
}

// TestRunMigrationsFailsFastOnMissingFiles 迁移文件缺失时必须立即报错，且错误里点名缺了哪些文件。
//
// 用零值 &Postgres{}：守卫发生在任何 p.pool 使用之前，所以无需数据库即可覆盖。
func TestRunMigrationsFailsFastOnMissingFiles(t *testing.T) {
	dir := t.TempDir()
	// 只放部分必需文件（模拟「部署时漏传 002 / 004」）。
	present := []string{"001_init.sql", "003_rbac_config.sql"}
	for _, name := range present {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- dummy\n"), 0o600); err != nil {
			t.Fatalf("写入临时迁移文件失败: %v", err)
		}
	}
	// 目录里还要有一个非必需文件：它既不算缺失，也不影响守卫。
	if err := os.WriteFile(filepath.Join(dir, "999_extra.sql"), []byte("-- dummy\n"), 0o600); err != nil {
		t.Fatalf("写入临时迁移文件失败: %v", err)
	}

	err := (&Postgres{}).RunMigrations(context.Background(), dir)
	if err == nil {
		t.Fatal("缺少必需迁移文件时 RunMigrations 应返回错误，实际为 nil")
	}
	want := []string{"002_catalog_admin.sql", "004_content.sql"}
	for _, name := range want {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误信息应点名缺失文件 %s，实际: %v", name, err)
		}
	}
	for _, name := range present {
		if strings.Contains(err.Error(), name) {
			t.Errorf("错误信息不应把已存在的 %s 报成缺失: %v", name, err)
		}
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("错误信息应带上迁移目录 %s，便于定位部署包: %v", dir, err)
	}
}

// TestRunMigrationsEmptyDirReportsAllRequired 空目录（例如部署包里根本没有 migrations 目录内容）
// 应把 RequiredMigrations 全部报为缺失。
func TestRunMigrationsEmptyDirReportsAllRequired(t *testing.T) {
	dir := t.TempDir()
	err := (&Postgres{}).RunMigrations(context.Background(), dir)
	if err == nil {
		t.Fatal("空迁移目录应返回错误，实际为 nil")
	}
	for _, name := range RequiredMigrations {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误信息应包含 %s，实际: %v", name, err)
		}
	}
}

// TestRunMigrationsMissingDir 迁移目录本身不存在时仍沿用原有报错（不是缺文件守卫）。
func TestRunMigrationsMissingDir(t *testing.T) {
	err := (&Postgres{}).RunMigrations(context.Background(), filepath.Join(t.TempDir(), "not-exists"))
	if err == nil {
		t.Fatal("迁移目录不存在时应返回错误，实际为 nil")
	}
	if !strings.Contains(err.Error(), "读取迁移目录") {
		t.Errorf("应返回读取目录失败的错误，实际: %v", err)
	}
}
