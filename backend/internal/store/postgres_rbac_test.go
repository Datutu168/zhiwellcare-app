package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"zhiwellcare/backend/internal/model"
)

// newPostgresTestStore 连接 APP_TEST_DB_URL（未配置则 Skip）并执行全部迁移。
//
// 注意：本测试会对该连接指向的 schema 做建表/写数据，请指向独立测试库或独立 schema
// （例如连接串追加 ?search_path=zwk_test_partition），不要直连正式库。
func newPostgresTestStore(t *testing.T) (*Postgres, context.Context) {
	t.Helper()
	pg, ctx := connectPostgresTestStore(t)
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return pg, ctx
}

// connectPostgresTestStore 只建连接，不执行迁移（供需要自己构造迁移前状态的用例使用）。
func connectPostgresTestStore(t *testing.T) (*Postgres, context.Context) {
	t.Helper()
	dbURL := os.Getenv("APP_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("未配置 APP_TEST_DB_URL，跳过 PostgreSQL 集成测试")
	}
	ctx := context.Background()
	pg, err := NewPostgres(ctx, dbURL)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg, ctx
}

// execMigrationFile 单独执行某个迁移文件（按文件名）。
func execMigrationFile(t *testing.T, pg *Postgres, ctx context.Context, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(migrationsDir(t), name))
	if err != nil {
		t.Fatalf("读取迁移 %s 失败: %v", name, err)
	}
	if _, err := pg.pool.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("执行迁移 %s 失败: %v", name, err)
	}
}

// newPostgresSchemaStore 为一个用例准备「独占 schema」的连接：用例自洽、不受执行顺序影响，
// 结束时自动 DROP SCHEMA ... CASCADE（不会污染其它用例或正式库的 public）。
func newPostgresSchemaStore(t *testing.T, schema string) (*Postgres, context.Context) {
	t.Helper()
	dbURL := os.Getenv("APP_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("未配置 APP_TEST_DB_URL，跳过 PostgreSQL 集成测试")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("解析连接串失败: %v", err)
	}
	// 该连接池的所有会话都指向用例专属 schema
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("创建连接池失败: %v", err)
	}
	pg := &Postgres{pool: pool}
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("清理 schema %s 失败: %v", schema, err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		pool.Close()
		t.Fatalf("创建 schema %s 失败: %v", schema, err)
	}
	t.Cleanup(func() {
		defer pool.Close()
		if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
			t.Logf("清理 schema %s 失败: %v", schema, err)
		}
	})
	return pg, ctx
}

// TestPostgresLegacyRepartitionKeepsIndexes 复刻真实升级路径（普通表 + 跨月历史数据 → 分区表）：
//
//	只应用 001+002（普通表 training_records 与 002 建的 idx_training_records_* 索引）
//	→ 灌入跨 3 个月的历史数据 → 再执行完整迁移（003，等价服务启动时的单事务执行）
//	→ 断言 4 个业务索引确实挂在分区表 training_records 上（而不是归档表），
//	  历史数据无损、落到月分区而不是 DEFAULT，且重复迁移幂等。
//
// 该用例正是「索引名被归档表占住导致 CREATE INDEX IF NOT EXISTS 静默跳过」的回归防线，
// 用独立 schema 保证每次都会真实执行（而不是因为库已迁移而跳过）。
func TestPostgresLegacyRepartitionKeepsIndexes(t *testing.T) {
	pg, ctx := newPostgresSchemaStore(t, "zwk_legacy_case")

	// 1) 只应用 001 + 002：得到普通表 + 002 建的三个索引（占用 idx_training_records_* 原名）
	execMigrationFile(t, pg, ctx, "001_init.sql")
	execMigrationFile(t, pg, ctx, "002_catalog_admin.sql")

	var relKind string
	if err := pg.pool.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'training_records'`).Scan(&relKind); err != nil {
		t.Fatalf("查询迁移前表类型失败: %v", err)
	}
	if relKind != "r" {
		t.Fatalf("001+002 后 training_records 应为普通表，实际 %q", relKind)
	}
	// 迁移前：索引确实挂在普通表上（这就是会让后面 CREATE INDEX IF NOT EXISTS 静默跳过的名字冲突源）
	assertIndexOwner(t, pg, "idx_training_records_user", "training_records")
	assertIndexOwner(t, pg, "idx_training_records_game", "training_records")
	assertIndexOwner(t, pg, "idx_training_records_completed", "training_records")

	// 2) 灌入跨 3 个月的历史数据（含 user 外键，验证搬迁不破坏外键）
	var userID string
	if err := pg.pool.QueryRow(ctx, `
		INSERT INTO users (phone, password_hash, nickname, role)
		VALUES ('13600000001', 'hash', 'legacy', 'admin') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("创建历史用户失败: %v", err)
	}
	suffix := uniqueSuffix()
	months := []string{"-3 months", "-2 months", "-1 month"}
	wantIDs := make([]string, 0, len(months))
	for index, offset := range months {
		recordID := fmt.Sprintf("legacy-%s-%d", suffix, index)
		wantIDs = append(wantIDs, recordID)
		if _, err := pg.pool.Exec(ctx, `
			INSERT INTO training_records (record_id, user_id, game_id, game_name, capability_tags,
				statistics, client_version, completed_at)
			VALUES ($1, $2, 'target-reach', '四方挥腕挑战', '{posture-sensor}', '{"s":1}'::jsonb, '0.1.0',
			        date_trunc('month', now()) + $3::interval + interval '3 days')`,
			recordID, userID, offset); err != nil {
			t.Fatalf("写入历史训练记录失败: %v", err)
		}
	}

	// 3) 执行完整迁移（等价 RunMigrations：整份 003 在单事务里跑）
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 4) 分区化 + 数据无损
	if err := pg.pool.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'training_records'`).Scan(&relKind); err != nil {
		t.Fatalf("查询迁移后表类型失败: %v", err)
	}
	if relKind != "p" {
		t.Fatalf("迁移后 training_records 应为分区表，实际 %q", relKind)
	}
	var legacyRows, parentRows, copiedRows, defaultRows int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM training_records_legacy`).Scan(&legacyRows); err != nil {
		t.Fatalf("统计归档表失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM training_records`).Scan(&parentRows); err != nil {
		t.Fatalf("统计分区表失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM training_records WHERE record_id LIKE 'legacy-' || $1 || '%'`,
		suffix).Scan(&copiedRows); err != nil {
		t.Fatalf("统计搬运行数失败: %v", err)
	}
	// 历史数据落在各自月份的月分区（不是 DEFAULT）
	if err := pg.pool.QueryRow(ctx, `
		SELECT count(*) FROM training_records
		WHERE tableoid = to_regclass('training_records_default') AND record_id LIKE 'legacy-' || $1 || '%'`,
		suffix).Scan(&defaultRows); err != nil {
		t.Fatalf("统计 DEFAULT 分区失败: %v", err)
	}
	if legacyRows != 3 || parentRows < 3 || copiedRows != 3 {
		t.Fatalf("历史数据搬迁异常: legacy=%d parent=%d copied=%d", legacyRows, parentRows, copiedRows)
	}
	if defaultRows != 0 {
		t.Fatalf("历史数据不应落在 DEFAULT 分区，实际 %d 行", defaultRows)
	}

	// 5) 关键断言：4 个功能索引必须真正建在分区表上（不绑定具体索引名，
	//    规范名 idx_training_records_* 与过渡名 idx_training_records_part_* 都算通过）
	assertFunctionalIndex(t, pg, "training_records", "user_id")
	assertFunctionalIndex(t, pg, "training_records", "game_id")
	assertFunctionalIndex(t, pg, "training_records", "completed_at")
	assertFunctionalIndex(t, pg, "training_records", "record_id")

	// 6) 重复迁移幂等，且索引归属不变
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	var parentRowsAfter int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM training_records`).Scan(&parentRowsAfter); err != nil {
		t.Fatalf("统计分区表失败: %v", err)
	}
	if parentRowsAfter != parentRows {
		t.Fatalf("重复迁移不应搬动数据: %d -> %d", parentRows, parentRowsAfter)
	}
	assertFunctionalIndex(t, pg, "training_records", "user_id")

	// 收尾：清掉本用例产生的历史数据，避免影响其它用例的统计断言
	if _, err := pg.pool.Exec(ctx, `DELETE FROM training_records WHERE record_id LIKE 'legacy-' || $1 || '%'`, suffix); err != nil {
		t.Fatalf("清理历史数据失败: %v", err)
	}
}

// TestPostgresMigrationIdempotent 迁移可重复执行：表结构判定 + 数据不重复搬运。
func TestPostgresMigrationIdempotent(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)

	suffix := uniqueSuffix()
	completed := time.Now().Truncate(time.Millisecond)
	record := model.TrainingSummary{RecordID: "mig-" + suffix, GameID: "target-reach", CompletedAt: completed}
	if err := pg.SaveTrainingRecord(ctx, record); err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	if count := countRecords(t, pg, record.RecordID); count != 1 {
		t.Fatalf("上报后应有 1 行，实际 %d", count)
	}

	// 再跑一次全部迁移（模拟服务重启）
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	if count := countRecords(t, pg, record.RecordID); count != 1 {
		t.Fatalf("重复迁移不应搬动/复制数据，实际 %d 行", count)
	}
}

// TestPostgresPartitionedTrainingRecords 分区表结构 + 复合主键幂等 + 按需建分区。
func TestPostgresPartitionedTrainingRecords(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)

	// 1) 父表必须是分区表（relkind = 'p'）
	var relKind string
	if err := pg.pool.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'training_records'`).Scan(&relKind); err != nil {
		t.Fatalf("查询表类型失败: %v", err)
	}
	if relKind != "p" {
		t.Fatalf("training_records 应为分区表（relkind=p），实际 %q", relKind)
	}

	// 2) 复合主键必须包含分区键
	var primaryKey string
	if err := pg.pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'training_records'::regclass AND contype = 'p'`).Scan(&primaryKey); err != nil {
		t.Fatalf("查询主键失败: %v", err)
	}
	if !strings.Contains(primaryKey, "record_id") || !strings.Contains(primaryKey, "completed_at") {
		t.Fatalf("主键必须为 (record_id, completed_at)，实际 %q", primaryKey)
	}

	// 3) DEFAULT 分区兜底存在
	var hasDefault bool
	if err := pg.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = 'training_records_default')`).Scan(&hasDefault); err != nil {
		t.Fatalf("查询 DEFAULT 分区失败: %v", err)
	}
	if !hasDefault {
		t.Fatal("缺少 DEFAULT 分区兜底")
	}

	// 3b) 业务索引必须真正建在分区表上。
	// PostgreSQL 的 CREATE INDEX IF NOT EXISTS 只按名字判断存在性（不校验索引挂在哪张表），
	// 若归档表仍占用 idx_training_records_* 就会静默跳过，这里按「列 + 归属表」逐个断言。
	assertFunctionalIndex(t, pg, "training_records", "user_id")
	assertFunctionalIndex(t, pg, "training_records", "game_id")
	assertFunctionalIndex(t, pg, "training_records", "completed_at")
	assertFunctionalIndex(t, pg, "training_records", "record_id")
	// 归档表存在时，它的同名索引要么已让位改名、要么至少不能顶替分区表的功能索引
	if tableExists(t, pg, "training_records_legacy") {
		var shadowing int
		if err := pg.pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_index i
			JOIN pg_class ic ON ic.oid = i.indexrelid
			JOIN pg_class tc ON tc.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = tc.relnamespace
			WHERE n.nspname = current_schema() AND tc.relname = 'training_records_legacy'
			  AND ic.relname IN ('idx_training_records_user', 'idx_training_records_game',
			                     'idx_training_records_completed')`).Scan(&shadowing); err != nil {
			t.Fatalf("查询归档表索引失败: %v", err)
		}
		if shadowing > 0 {
			// 只要归档表还占着这些名字，就必须确认分区表的功能索引没被跳过（上面的断言已保证）
			t.Logf("归档表仍占用 %d 个历史索引名（分区表功能索引已由 assertFunctionalIndex 验证存在）", shadowing)
		}
	}

	// 4) 重复上报（同 recordId + completedAt）只落 1 行
	suffix := uniqueSuffix()
	completed := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	record := model.TrainingSummary{
		RecordID: "dup-" + suffix, GameID: "target-reach", GameName: "四方挥腕挑战",
		DeviceModelID: "wobble-wrist-band", CapabilityTags: []string{"posture-sensor"},
		Statistics: json.RawMessage(`{"score":1}`), ClientVersion: "0.1.0", CompletedAt: completed,
	}
	for i := 0; i < 3; i++ {
		if err := pg.SaveTrainingRecord(ctx, record); err != nil {
			t.Fatalf("第 %d 次上报失败: %v", i+1, err)
		}
	}
	if count := countRecords(t, pg, record.RecordID); count != 1 {
		t.Fatalf("重复上报应只落 1 行，实际 %d", count)
	}
	// 落到当月分区（不是 DEFAULT）
	var partition string
	if err := pg.pool.QueryRow(ctx, `SELECT tableoid::regclass::text FROM training_records WHERE record_id=$1`,
		record.RecordID).Scan(&partition); err != nil {
		t.Fatalf("查询所属分区失败: %v", err)
	}
	if strings.HasSuffix(partition, "_default") || !strings.HasPrefix(partition, "training_records_") {
		t.Fatalf("记录应落在月分区，实际 %q", partition)
	}

	// 5) 同 recordId 不同 completedAt 属于两次训练 → 2 行，读取取最新
	second := record
	second.CompletedAt = completed.Add(time.Hour)
	if err := pg.SaveTrainingRecord(ctx, second); err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	if count := countRecords(t, pg, record.RecordID); count != 2 {
		t.Fatalf("不同完成时间应落 2 行，实际 %d", count)
	}
	got, err := pg.GetTrainingRecord(ctx, record.RecordID)
	if err != nil || !got.CompletedAt.Equal(second.CompletedAt) {
		t.Fatalf("GetTrainingRecord 应返回最新一条: %+v %v", got, err)
	}

	// 6) 未来月份：写入前自动建分区（ensure_partition），不落 DEFAULT
	future := time.Now().AddDate(0, 8, 3).Truncate(time.Millisecond)
	futureRecord := model.TrainingSummary{RecordID: "future-" + suffix, GameID: "target-reach", CompletedAt: future}
	if err := pg.SaveTrainingRecord(ctx, futureRecord); err != nil {
		t.Fatalf("未来月份上报失败: %v", err)
	}
	var futurePartition string
	if err := pg.pool.QueryRow(ctx, `SELECT tableoid::regclass::text FROM training_records WHERE record_id=$1`,
		futureRecord.RecordID).Scan(&futurePartition); err != nil {
		t.Fatalf("查询未来分区失败: %v", err)
	}
	wantPartition := "training_records_" + future.Format("200601")
	if futurePartition != wantPartition {
		t.Fatalf("未来月份应自动建分区 %s，实际 %q", wantPartition, futurePartition)
	}

	// 7) 分页与统计可用
	if _, total, err := pg.ListTrainingRecords(ctx, TrainingRecordFilter{GameID: "target-reach", Page: 1, PageSize: 5}); err != nil || total < 2 {
		t.Fatalf("训练记录查询异常: total=%d err=%v", total, err)
	}
	if stats, err := pg.DashboardStats(ctx); err != nil || stats.TrainingRecs < 2 {
		t.Fatalf("仪表盘统计异常: %+v %v", stats, err)
	}
}

// TestPostgresJSONBConfigColumns 机型/游戏 JSONB 扩展配置读写（不丢原有 text[] 列）。
func TestPostgresJSONBConfigColumns(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)
	suffix := uniqueSuffix()

	device := model.DeviceModel{
		ModelID: "cfg-band-" + suffix, Name: "配置机型", Manufacturer: "智为康乐", Category: "wrist",
		CapabilityTags: []string{"posture-sensor"}, SupportedHandleTags: []string{"grip"},
		NamePatterns: []string{"bt91"}, Protocol: "bs-bt91", MinAppVersion: "0.1.0",
		Icon: "⌚", Status: "on",
		Config: json.RawMessage(`{"handleTags":["grip","press"],"difficulty":{"level":3}}`),
	}
	if err := pg.UpsertDeviceModel(ctx, device); err != nil {
		t.Fatalf("写入机型失败: %v", err)
	}
	got, err := pg.GetDeviceModel(ctx, device.ModelID)
	if err != nil {
		t.Fatalf("读取机型失败: %v", err)
	}
	// 原有 text[] 列未丢失
	if len(got.CapabilityTags) != 1 || got.CapabilityTags[0] != "posture-sensor" ||
		len(got.SupportedHandleTags) != 1 || got.SupportedHandleTags[0] != "grip" {
		t.Fatalf("原有标签列异常: %+v", got)
	}
	var deviceConfig struct {
		HandleTags []string `json:"handleTags"`
		Difficulty struct {
			Level int `json:"level"`
		} `json:"difficulty"`
	}
	if err := json.Unmarshal(got.Config, &deviceConfig); err != nil {
		t.Fatalf("解析机型配置失败: %v (%s)", err, got.Config)
	}
	if len(deviceConfig.HandleTags) != 2 || deviceConfig.Difficulty.Level != 3 {
		t.Fatalf("机型 JSONB 配置往返异常: %+v", deviceConfig)
	}

	game := model.GameCatalogItem{
		GameID: "cfg-game-" + suffix, Name: "配置游戏", RequiredTags: []string{"posture-sensor"},
		DurationPresetsMin: []int{1, 3, 5}, ResourceVersion: "1.0.0", Status: "on",
		Config: json.RawMessage(`{"difficulty":{"level":2},"handleTags":["press"]}`),
	}
	if err := pg.UpsertGame(ctx, game); err != nil {
		t.Fatalf("写入游戏失败: %v", err)
	}
	gotGame, err := pg.GetGame(ctx, game.GameID)
	if err != nil {
		t.Fatalf("读取游戏失败: %v", err)
	}
	if len(gotGame.DurationPresetsMin) != 3 || len(gotGame.RequiredTags) != 1 {
		t.Fatalf("原有数组列异常: %+v", gotGame)
	}
	if !strings.Contains(string(gotGame.Config), `"level": 2`) && !strings.Contains(string(gotGame.Config), `"level":2`) {
		t.Fatalf("游戏 JSONB 配置未往返: %s", gotGame.Config)
	}

	// 未提供 config 时落 {}（NOT NULL 列）
	plainGame := model.GameCatalogItem{GameID: "plain-game-" + suffix, Name: "无配置", Status: "on"}
	if err := pg.UpsertGame(ctx, plainGame); err != nil {
		t.Fatalf("写入游戏失败: %v", err)
	}
	gotPlain, _ := pg.GetGame(ctx, plainGame.GameID)
	if strings.TrimSpace(string(gotPlain.Config)) != "{}" {
		t.Fatalf("空配置应落 {}，实际 %s", gotPlain.Config)
	}
}

// TestPostgresRBACAndConfig RBAC / 配置中心 / 设备事实表 / 资产在真库上的读写。
func TestPostgresRBACAndConfig(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)
	suffix := uniqueSuffix()

	permissions, err := pg.ListPermissions(ctx)
	if err != nil || len(permissions) != 15 {
		t.Fatalf("权限点应为 15 个: %d %v", len(permissions), err)
	}
	roles, err := pg.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles 失败: %v", err)
	}
	admin := findRoleByCode(roles, model.RoleAdmin)
	if admin == nil || !admin.Builtin || len(admin.Permissions) != 15 {
		t.Fatalf("admin 角色异常: %+v", admin)
	}
	if operator := findRoleByCode(roles, model.RoleOperator); operator == nil || len(operator.Permissions) != 11 {
		t.Fatalf("operator 角色异常: %+v", operator)
	}
	if viewer := findRoleByCode(roles, model.RoleViewer); viewer == nil || len(viewer.Permissions) != 8 {
		t.Fatalf("viewer 角色异常: %+v", viewer)
	}

	// 角色 CRUD
	code := "care-" + suffix
	created, err := pg.CreateRole(ctx, model.Role{Code: code, Name: "客服", Description: "d",
		Permissions: []string{model.PermUserRead, model.PermRecordRead}})
	if err != nil || len(created.Permissions) != 2 || created.Builtin {
		t.Fatalf("CreateRole 失败: %+v %v", created, err)
	}
	if _, err := pg.CreateRole(ctx, model.Role{Code: code, Name: "重复"}); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("重复编码应返回 ErrRoleExists，实际 %v", err)
	}
	if _, err := pg.CreateRole(ctx, model.Role{Code: code + "-x", Permissions: []string{"nope:read"}}); !errors.Is(err, ErrPermissionUnknown) {
		t.Fatalf("未知权限点应返回 ErrPermissionUnknown，实际 %v", err)
	}
	updated, err := pg.UpdateRole(ctx, code, "客服组", "改后", []string{model.PermAssetRead})
	if err != nil || updated.Name != "客服组" || len(updated.Permissions) != 1 {
		t.Fatalf("UpdateRole 异常: %+v %v", updated, err)
	}
	if _, err := pg.UpdateRole(ctx, "missing-"+suffix, "x", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("更新不存在的角色应返回 ErrNotFound，实际 %v", err)
	}
	if err := pg.DeleteRole(ctx, model.RoleAdmin); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("删除内置角色应返回 ErrRoleBuiltin，实际 %v", err)
	}

	// 用户角色 + users.role 同步
	user := createTestUser(t, pg, ctx, suffix)
	if _, err := pg.SetUserRoles(ctx, user.ID, []string{code}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	if perms, err := pg.ListUserPermissions(ctx, user.ID); err != nil || len(perms) != 1 || perms[0] != model.PermAssetRead {
		t.Fatalf("用户权限异常: %v %v", perms, err)
	}
	after, _ := pg.GetUserByID(ctx, user.ID)
	if after.Role != "user" {
		t.Fatalf("非 admin 角色时 users.role 应为 user，实际 %q", after.Role)
	}
	if err := pg.DeleteRole(ctx, code); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("删除仍有用户的角色应返回 ErrRoleInUse，实际 %v", err)
	}
	if _, err := pg.SetUserRoles(ctx, user.ID, []string{model.RoleAdmin}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	after, _ = pg.GetUserByID(ctx, user.ID)
	if after.Role != model.RoleAdmin {
		t.Fatalf("拥有 admin 角色后 users.role 应为 admin，实际 %q", after.Role)
	}
	roleUserIDs, err := pg.ListRoleUserIDs(ctx, model.RoleAdmin)
	if err != nil || !containsString(roleUserIDs, user.ID) {
		t.Fatalf("ListRoleUserIDs 异常: %v %v", roleUserIDs, err)
	}
	// SetUserRole 兼容路径同步 user_roles
	if err := pg.SetUserRole(ctx, user.ID, "user"); err != nil {
		t.Fatalf("SetUserRole 失败: %v", err)
	}
	if roles, _ := pg.ListUserRoles(ctx, user.ID); len(roles) != 0 {
		t.Fatalf("回收 admin 后角色应为空: %v", roles)
	}

	// 配置中心
	item, err := pg.GetAppConfig(ctx, "catalog.version")
	if err != nil || string(item.Value) != `"1.0.0"` {
		t.Fatalf("配置种子异常: %+v %v", item, err)
	}
	description := "集成测试写入"
	written, err := pg.SetAppConfig(ctx, "catalog.test."+suffix, []byte(`{"score":88}`), &description, user.ID)
	if err != nil {
		t.Fatalf("SetAppConfig 失败: %v", err)
	}
	if written.Description != description || !strings.Contains(string(written.Value), "88") {
		t.Fatalf("写配置返回异常: %+v", written)
	}
	readBack, err := pg.GetAppConfig(ctx, "catalog.test."+suffix)
	if err != nil || !strings.Contains(string(readBack.Value), "88") {
		t.Fatalf("配置回读异常: %+v %v", readBack, err)
	}
	if _, err := pg.GetAppConfig(ctx, "not.exists."+suffix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的配置应返回 ErrNotFound，实际 %v", err)
	}
	if items, err := pg.ListAppConfig(ctx); err != nil || len(items) < 4 {
		t.Fatalf("配置列表异常: %d %v", len(items), err)
	}

	// 设备白名单 / 映射
	whitelistKey := "bt91-" + suffix
	if _, err := pg.UpsertDeviceWhitelist(ctx, model.DeviceWhitelistEntry{
		DeviceKey: " " + strings.ToUpper(whitelistKey) + " ", ModelID: "wobble-wrist-band", Note: "样机", Enabled: true,
	}); err != nil {
		t.Fatalf("白名单写入失败: %v", err)
	}
	if _, err := pg.UpsertDeviceWhitelist(ctx, model.DeviceWhitelistEntry{DeviceKey: "off-" + suffix, Enabled: false}); err != nil {
		t.Fatalf("白名单写入失败: %v", err)
	}
	keys, err := pg.ListWhitelistKeys(ctx)
	if err != nil || !containsString(keys, whitelistKey) || containsString(keys, "off-"+suffix) {
		t.Fatalf("生效白名单异常: %v %v", keys, err)
	}
	if err := pg.DeleteDeviceWhitelist(ctx, whitelistKey); err != nil {
		t.Fatalf("删除白名单失败: %v", err)
	}
	if err := pg.DeleteDeviceWhitelist(ctx, whitelistKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := pg.UpsertDeviceMapping(ctx, model.DeviceMappingEntry{
		DeviceKey: "BT91-" + suffix, ModelID: "wobble-wrist-band", Note: "映射",
	}); err != nil {
		t.Fatalf("映射写入失败: %v", err)
	}
	if mapped, err := pg.GetDeviceMapping(ctx, whitelistKey); err != nil || mapped != "wobble-wrist-band" {
		t.Fatalf("映射读取异常: %q %v", mapped, err)
	}
	if _, err := pg.GetDeviceMapping(ctx, "unknown-"+suffix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的映射应返回 ErrNotFound，实际 %v", err)
	}
	mappings, err := pg.ListDeviceMappings(ctx)
	if err != nil || len(mappings) == 0 {
		t.Fatalf("映射列表异常: %v %v", mappings, err)
	}
	if err := pg.DeleteDeviceMapping(ctx, whitelistKey); err != nil {
		t.Fatalf("删除映射失败: %v", err)
	}

	// 释放自定义角色
	if _, err := pg.SetUserRoles(ctx, user.ID, nil, ""); err != nil {
		t.Fatalf("清空角色失败: %v", err)
	}
	if err := pg.DeleteRole(ctx, code); err != nil {
		t.Fatalf("删除角色失败: %v", err)
	}
}

// TestPostgresIndexNameCollisionConvergence 回归用例：
// 模拟「分区表已建好、但归档表仍占用 idx_training_records_* 原名」的状态
// （历史迁移 / 早期 003 草稿留下的中间状态），重跑迁移后必须收敛为：
// 规范索引名挂在分区表 training_records 上，归档表的索引已改名为 *_legacy_*。
func TestPostgresIndexNameCollisionConvergence(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)

	// 制造冲突状态：分区表上先不建业务索引，改由归档表占住原名
	for _, name := range []string{
		"idx_training_records_user", "idx_training_records_game",
		"idx_training_records_completed", "idx_training_records_record_id",
	} {
		if _, err := pg.pool.Exec(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			t.Fatalf("清理索引 %s 失败: %v", name, err)
		}
	}
	if _, err := pg.pool.Exec(ctx, `DROP TABLE IF EXISTS training_records_legacy`); err != nil {
		t.Fatalf("清理归档表失败: %v", err)
	}
	if _, err := pg.pool.Exec(ctx, `
		CREATE TABLE training_records_legacy AS
		SELECT id, record_id, user_id, game_id, game_name, device_model_id, device_model_name,
		       capability_tags, statistics, client_version, completed_at, uploaded_at, created_at
		FROM training_records WHERE false`); err != nil {
		t.Fatalf("构造归档表失败: %v", err)
	}
	for _, ddl := range []string{
		`CREATE INDEX idx_training_records_user ON training_records_legacy(user_id)`,
		`CREATE INDEX idx_training_records_game ON training_records_legacy(game_id)`,
		`CREATE INDEX idx_training_records_completed ON training_records_legacy(completed_at DESC)`,
	} {
		if _, err := pg.pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("构造归档表索引失败: %v", err)
		}
	}
	// 冲突状态下分区表确实没有业务索引
	assertIndexOwner(t, pg, "idx_training_records_user", "training_records_legacy")

	// 重跑迁移 → 必须自动收敛
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("重跑迁移失败: %v", err)
	}
	assertFunctionalIndex(t, pg, "training_records", "user_id")
	assertFunctionalIndex(t, pg, "training_records", "game_id")
	assertFunctionalIndex(t, pg, "training_records", "completed_at")
	assertFunctionalIndex(t, pg, "training_records", "record_id")

	// 收敛后再跑一次仍然幂等
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("再次重跑迁移失败: %v", err)
	}
	assertFunctionalIndex(t, pg, "training_records", "user_id")

	// 收尾：清掉本用例构造的归档表，避免影响其它用例
	if _, err := pg.pool.Exec(ctx, `DROP TABLE IF EXISTS training_records_legacy`); err != nil {
		t.Fatalf("清理归档表失败: %v", err)
	}
}

// TestPostgresEnsurePartitionBeforeInsert 跨月/未来月份写入必须自动建分区，
// 不落 DEFAULT 分区（SaveTrainingRecord 在 INSERT 前调用 ensure_partition）。
func TestPostgresEnsurePartitionBeforeInsert(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)
	suffix := uniqueSuffix()

	for _, months := range []int{1, 3, 9} {
		at := time.Now().AddDate(0, months, 2).Truncate(time.Millisecond)
		record := model.TrainingSummary{
			RecordID: "ensure-" + suffix + "-" + fmt.Sprint(months), GameID: "target-reach", CompletedAt: at,
		}
		if err := pg.SaveTrainingRecord(ctx, record); err != nil {
			t.Fatalf("第 %d 个月后写入失败: %v", months, err)
		}
		var partition string
		if err := pg.pool.QueryRow(ctx, `SELECT tableoid::regclass::text FROM training_records WHERE record_id=$1`,
			record.RecordID).Scan(&partition); err != nil {
			t.Fatalf("查询所属分区失败: %v", err)
		}
		want := "training_records_" + at.Format("200601")
		if partition != want {
			t.Fatalf("%d 个月后写入应落 %s，实际 %q（可能没在插入前建分区）", months, want, partition)
		}
	}
}

// TestPostgresAssetStore 资产登记 / published 过滤 / 唯一约束。
func TestPostgresAssetStore(t *testing.T) {
	pg, ctx := newPostgresTestStore(t)
	suffix := uniqueSuffix()

	draft, err := pg.CreateAsset(ctx, model.Asset{
		Kind: model.AssetKindWeb, RefID: "android-" + suffix, Version: "0.2.0", Filename: "app.zip",
		ObjectKey: "web/android-" + suffix + "/0.2.0/app.zip", Size: 1024, SHA256: "aabb",
		ContentType: "application/zip", Notes: "修复闪退",
	})
	if err != nil || draft.Status != model.AssetStatusDraft || draft.PublishedAt != nil {
		t.Fatalf("资产登记异常: %+v %v", draft, err)
	}
	if _, err := pg.CreateAsset(ctx, model.Asset{
		Kind: model.AssetKindWeb, RefID: "android-" + suffix, Version: "0.2.0", Filename: "app.zip", ObjectKey: "x",
	}); !errors.Is(err, ErrAssetExists) {
		t.Fatalf("重复登记应返回 ErrAssetExists，实际 %v", err)
	}
	if _, err := pg.LatestPublishedAsset(ctx, model.AssetKindWeb, "android-"+suffix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft 不应命中 published 查询，实际 %v", err)
	}

	published, err := pg.SetAssetStatus(ctx, draft.ID, model.AssetStatusPublished)
	if err != nil || published.PublishedAt == nil {
		t.Fatalf("发布失败: %+v %v", published, err)
	}
	if found, err := pg.FindPublishedAsset(ctx, model.AssetKindWeb, "android-"+suffix, "0.2.0"); err != nil || found.ID != draft.ID {
		t.Fatalf("published 精确查询失败: %+v %v", found, err)
	}
	if latest, err := pg.LatestPublishedAsset(ctx, model.AssetKindWeb, "android-"+suffix); err != nil || latest.ID != draft.ID {
		t.Fatalf("最新 published 查询失败: %+v %v", latest, err)
	}
	if items, err := pg.ListAssets(ctx, model.AssetKindWeb, "android-"+suffix, model.AssetStatusPublished); err != nil || len(items) != 1 {
		t.Fatalf("published 过滤异常: %v %v", items, err)
	}
	if items, err := pg.ListAssets(ctx, model.AssetKindWeb, "android-"+suffix, model.AssetStatusDraft); err != nil || len(items) != 0 {
		t.Fatalf("draft 过滤异常: %v %v", items, err)
	}
	if _, err := pg.SetAssetStatus(ctx, 99999999, model.AssetStatusDraft); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的资产应返回 ErrNotFound，实际 %v", err)
	}
	if err := pg.DeleteAsset(ctx, draft.ID); err != nil {
		t.Fatalf("删除资产失败: %v", err)
	}
	if err := pg.DeleteAsset(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// ---------- 辅助 ----------

func countRecords(t *testing.T, pg *Postgres, recordID string) int {
	t.Helper()
	var count int
	if err := pg.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM training_records WHERE record_id = $1`, recordID).Scan(&count); err != nil {
		t.Fatalf("统计训练记录失败: %v", err)
	}
	return count
}

// assertIndexOwner 断言索引存在且挂在期望的表上（防止 CREATE INDEX IF NOT EXISTS
// 因同名 relation 被静默跳过——索引名是 schema 级命名空间，不跟表走）。
func assertIndexOwner(t *testing.T, pg *Postgres, indexName, wantTable string) {
	t.Helper()
	var owner string
	if err := pg.pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT tablename FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = $1), '')`, indexName).Scan(&owner); err != nil {
		t.Fatalf("查询索引 %s 归属失败: %v", indexName, err)
	}
	if owner == "" {
		t.Fatalf("索引 %s 不存在（CREATE INDEX IF NOT EXISTS 可能被同名索引静默跳过）", indexName)
	}
	if owner != wantTable {
		t.Fatalf("索引 %s 挂在 %s 上，期望 %s", indexName, owner, wantTable)
	}
}

// assertFunctionalIndex 断言某张表上确实存在「覆盖指定列」的功能索引。
//
// 这是索引修复的最终判据，且**不绑定具体索引名**：扫 pg_index + pg_class 拿到该表上
// 所有非主键索引名与 pg_get_indexdef 定义，要求存在一条定义覆盖 column、且名字属于
// 允许的前缀（规范名 idx_training_records_* 或过渡名 idx_training_records_part_*）。
// 只按名字判断存在性的 CREATE INDEX IF NOT EXISTS 一旦被同名 relation 跳过，
// 这里就会失败并打印实际找到的索引清单。
func assertFunctionalIndex(t *testing.T, pg *Postgres, table, column string) {
	t.Helper()
	rows, err := pg.pool.Query(context.Background(), `
		SELECT ic.relname, pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE n.nspname = current_schema() AND tc.relname = $1 AND i.indisprimary = false
		ORDER BY ic.relname`, table)
	if err != nil {
		t.Fatalf("查询表 %s 索引失败: %v", table, err)
	}
	defer rows.Close()

	found := []string{}
	matched := ""
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatalf("扫描索引失败: %v", err)
		}
		found = append(found, name+" => "+definition)
		// 定义必须覆盖目标列（如 (user_id) / (completed_at DESC)）
		if !strings.Contains(definition, "("+column) {
			continue
		}
		if strings.HasPrefix(name, "idx_training_records_part_") || strings.HasPrefix(name, "idx_training_records_") {
			matched = name
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历索引失败: %v", err)
	}
	if matched == "" {
		t.Fatalf("表 %s 上缺少覆盖 %s 的索引（允许 idx_training_records_* 或 idx_training_records_part_*）；实际索引: %v",
			table, column, found)
	}
}

// tableExists 判断当前 schema 下是否存在该表。
func tableExists(t *testing.T, pg *Postgres, table string) bool {
	t.Helper()
	var exists bool
	if err := pg.pool.QueryRow(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatalf("判断表 %s 是否存在失败: %v", table, err)
	}
	return exists
}

func createTestUser(t *testing.T, pg *Postgres, ctx context.Context, suffix string) *model.User {
	t.Helper()
	user, err := pg.CreateUser(ctx, CreateUserParams{
		Phone: fmt.Sprintf("137%08d", time.Now().UnixNano()%100000000), PasswordHash: "hash", Nickname: "集成" + suffix,
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return user
}

func findRoleByCode(roles []model.Role, code string) *model.Role {
	for index := range roles {
		if roles[index].Code == code {
			return &roles[index]
		}
	}
	return nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1000000000)
}
