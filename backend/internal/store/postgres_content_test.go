package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"zhiwellcare/backend/internal/model"
)

// TestPostgresContentStore 内容中心（courses / mall_goods）在真库上的建表、CRUD、
// 上下架过滤与 sort 排序。用「独占 schema」连接（newPostgresSchemaStore），
// 用例结束自动 DROP SCHEMA，不会污染 public 或其它用例。
func TestPostgresContentStore(t *testing.T) {
	pg, ctx := newPostgresSchemaStore(t, "zwk_content_case")
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 1) 004 建表：两张表都存在
	for _, table := range []string{"courses", "mall_goods"} {
		if !tableExists(t, pg, table) {
			t.Fatalf("004 迁移未建表: %s", table)
		}
	}
	// 索引也在
	var indexCount int
	if err := pg.pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname IN ('idx_courses_status_sort', 'idx_mall_goods_status_sort')`).Scan(&indexCount); err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if indexCount != 2 {
		t.Fatalf("(status, sort) 索引应存在 2 个，实际 %d", indexCount)
	}

	// 2) 列默认值与 NOT NULL：直接插入最小行，status/sort/tags 走默认
	if _, err := pg.pool.Exec(ctx, `INSERT INTO courses (course_id, title) VALUES ('pg-default', '默认值课程')`); err != nil {
		t.Fatalf("插入最小课程失败: %v", err)
	}
	var status, summary, durationLabel string
	var sort int
	var tags []string
	if err := pg.pool.QueryRow(ctx, `SELECT status, summary, duration_label, sort, tags FROM courses WHERE course_id='pg-default'`).
		Scan(&status, &summary, &durationLabel, &sort, &tags); err != nil {
		t.Fatalf("读取默认值失败: %v", err)
	}
	if status != "off" || sort != 0 || summary != "" || durationLabel != "" || len(tags) != 0 {
		t.Fatalf("courses 列默认值异常: status=%q sort=%d summary=%q duration=%q tags=%v",
			status, sort, summary, durationLabel, tags)
	}
	// title 为 NOT NULL
	if _, err := pg.pool.Exec(ctx, `INSERT INTO courses (course_id) VALUES ('pg-no-title')`); err == nil {
		t.Fatal("title 为 NOT NULL，缺 title 的插入应失败")
	}
	if _, err := pg.pool.Exec(ctx, `DELETE FROM courses WHERE course_id='pg-default'`); err != nil {
		t.Fatalf("清理课程失败: %v", err)
	}

	// 3) 课程 CRUD：乱序写入 → 列表按 sort 升序；公开过滤 off
	if err := pg.UpsertCourse(ctx, model.Course{
		CourseID: "pg-course-b", Title: "平衡训练进阶", Summary: "s", CoverURL: "https://cdn/b.png",
		VideoURL: "https://cdn/b.mp4", DurationLabel: "8 分钟", Level: "进阶",
		Tags: []string{"balance", "wrist"}, Status: "on", Sort: 2,
	}); err != nil {
		t.Fatalf("写入课程失败: %v", err)
	}
	if err := pg.UpsertCourse(ctx, model.Course{
		CourseID: "pg-course-a", Title: "坐姿基础", Status: "on", Sort: 1,
	}); err != nil {
		t.Fatalf("写入课程失败: %v", err)
	}
	if err := pg.UpsertCourse(ctx, model.Course{
		CourseID: "pg-course-off", Title: "未上架课程", Status: "off", Sort: 9,
	}); err != nil {
		t.Fatalf("写入课程失败: %v", err)
	}

	public, err := pg.ListCourses(ctx, false)
	if err != nil {
		t.Fatalf("ListCourses 失败: %v", err)
	}
	if len(public) != 2 || public[0].CourseID != "pg-course-a" || public[1].CourseID != "pg-course-b" {
		t.Fatalf("公开课程应按 sort 升序且只含 on: %+v", public)
	}
	if len(public[1].Tags) != 2 || public[1].DurationLabel != "8 分钟" || public[1].CoverURL != "https://cdn/b.png" {
		t.Fatalf("课程字段往返异常: %+v", public[1])
	}
	if public[0].Tags == nil {
		t.Fatalf("空 tags 应为空切片而不是 nil: %+v", public[0])
	}

	all, err := pg.ListCourses(ctx, true)
	if err != nil || len(all) != 3 {
		t.Fatalf("后台课程列表异常: %d %v", len(all), err)
	}
	if all[0].CourseID != "pg-course-a" || all[2].CourseID != "pg-course-off" {
		t.Fatalf("后台列表顺序异常: %+v", all)
	}

	// upsert：不新增记录、created_at 不变、updated_at 前移
	createdAt := all[0].CreatedAt
	time.Sleep(2 * time.Millisecond)
	if err := pg.UpsertCourse(ctx, model.Course{
		CourseID: "pg-course-a", Title: "坐姿基础（改）", Status: "on", Sort: 1,
	}); err != nil {
		t.Fatalf("更新课程失败: %v", err)
	}
	again, _ := pg.ListCourses(ctx, true)
	if len(again) != 3 || again[0].Title != "坐姿基础（改）" {
		t.Fatalf("upsert 未生效: %+v", again)
	}
	if !again[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("upsert 不应改变 created_at: %v -> %v", createdAt, again[0].CreatedAt)
	}
	if !again[0].UpdatedAt.After(createdAt) {
		t.Fatalf("upsert 应刷新 updated_at: created=%v updated=%v", createdAt, again[0].UpdatedAt)
	}

	// 上下架 + 不存在 → ErrNotFound
	if err := pg.SetCourseStatus(ctx, "pg-course-off", "on"); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	if public, _ = pg.ListCourses(ctx, false); len(public) != 3 {
		t.Fatalf("上架后公开列表异常: %+v", public)
	}
	if err := pg.SetCourseStatus(ctx, "pg-course-off", "off"); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if public, _ = pg.ListCourses(ctx, false); len(public) != 2 {
		t.Fatalf("下架后公开列表异常: %+v", public)
	}
	if err := pg.SetCourseStatus(ctx, "pg-no-such-course", "on"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的课程改状态应返回 ErrNotFound，实际 %v", err)
	}
	if err := pg.DeleteCourse(ctx, "pg-course-off"); err != nil {
		t.Fatalf("删除课程失败: %v", err)
	}
	if err := pg.DeleteCourse(ctx, "pg-course-off"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// 4) 商品 CRUD：价格字段保留
	if err := pg.UpsertGoods(ctx, model.MallGoods{
		GoodsID: "pg-goods-a", Name: "训练腕带", Summary: "腕部训练配件", PriceCents: 19900,
		PriceLabel: "¥199", CoverURL: "https://cdn/goods-a.png", DetailURL: "https://shop/goods-a",
		Specs: []string{"S", "M", "L"}, Status: "on", Sort: 1,
	}); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	if err := pg.UpsertGoods(ctx, model.MallGoods{
		GoodsID: "pg-goods-off", Name: "未上架商品", PriceCents: 100, Status: "off", Sort: 0,
	}); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	goodsPublic, err := pg.ListGoods(ctx, false)
	if err != nil || len(goodsPublic) != 1 {
		t.Fatalf("公开商品列表异常: %+v %v", goodsPublic, err)
	}
	first := goodsPublic[0]
	if first.GoodsID != "pg-goods-a" || first.PriceCents != 19900 || first.PriceLabel != "¥199" ||
		len(first.Specs) != 3 || first.DetailURL != "https://shop/goods-a" {
		t.Fatalf("商品字段往返异常: %+v", first)
	}
	if goodsAll, err := pg.ListGoods(ctx, true); err != nil || len(goodsAll) != 2 {
		t.Fatalf("后台商品列表异常: %+v %v", goodsAll, err)
	}
	if err := pg.SetGoodsStatus(ctx, "pg-goods-off", "on"); err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}
	if goodsPublic, _ = pg.ListGoods(ctx, false); len(goodsPublic) != 2 {
		t.Fatalf("上架后公开商品列表异常: %+v", goodsPublic)
	}
	if err := pg.SetGoodsStatus(ctx, "pg-no-such-goods", "on"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的商品改状态应返回 ErrNotFound，实际 %v", err)
	}
	if err := pg.DeleteGoods(ctx, "pg-goods-off"); err != nil {
		t.Fatalf("删除商品失败: %v", err)
	}
	if err := pg.DeleteGoods(ctx, "pg-goods-off"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// TestPostgresContentMigrationGrantsAndIdempotent 004：权限点种子 + 种子角色归属 + 重复执行幂等。
func TestPostgresContentMigrationGrantsAndIdempotent(t *testing.T) {
	pg, ctx := newPostgresSchemaStore(t, "zwk_content_grants_case")
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 1) 权限点：content:read / content:write（名称与分组）
	rows, err := pg.pool.Query(ctx, `SELECT code, name, group_name FROM permissions WHERE code LIKE 'content:%' ORDER BY code`)
	if err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	seeded := map[string]model.Permission{}
	for rows.Next() {
		var item model.Permission
		if err := rows.Scan(&item.Code, &item.Name, &item.Group); err != nil {
			t.Fatalf("扫描权限点失败: %v", err)
		}
		seeded[item.Code] = item
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历权限点失败: %v", err)
	}
	if len(seeded) != 2 {
		t.Fatalf("content:* 权限点应为 2 个，实际 %d（%+v）", len(seeded), seeded)
	}
	if seeded[model.PermContentRead].Name != "内容查看" || seeded[model.PermContentRead].Group != "content" {
		t.Fatalf("content:read 种子异常: %+v", seeded[model.PermContentRead])
	}
	if seeded[model.PermContentWrite].Name != "内容维护" || seeded[model.PermContentWrite].Group != "content" {
		t.Fatalf("content:write 种子异常: %+v", seeded[model.PermContentWrite])
	}

	// 2) 种子角色归属：admin 全部、operator 读写、viewer 只读
	grants := func(role string) []string {
		t.Helper()
		roleRows, err := pg.pool.Query(ctx,
			`SELECT permission_code FROM role_permissions WHERE role_code=$1 AND permission_code LIKE 'content:%' ORDER BY permission_code`, role)
		if err != nil {
			t.Fatalf("查询角色 %s 授权失败: %v", role, err)
		}
		defer roleRows.Close()
		var codes []string
		for roleRows.Next() {
			var code string
			if err := roleRows.Scan(&code); err != nil {
				t.Fatalf("扫描授权失败: %v", err)
			}
			codes = append(codes, code)
		}
		return codes
	}
	admin := grants(model.RoleAdmin)
	if len(admin) != 2 || !containsString(admin, model.PermContentRead) || !containsString(admin, model.PermContentWrite) {
		t.Fatalf("admin 应拥有全部 content:* 权限，实际 %v", admin)
	}
	operator := grants(model.RoleOperator)
	if len(operator) != 2 || !containsString(operator, model.PermContentWrite) {
		t.Fatalf("operator 应拥有 content:read + content:write，实际 %v", operator)
	}
	viewer := grants(model.RoleViewer)
	if len(viewer) != 1 || viewer[0] != model.PermContentRead {
		t.Fatalf("viewer 应只拥有 content:read，实际 %v", viewer)
	}

	// 3) 写入一条课程后重复执行迁移（服务重启语义）+ 单独重跑 004 → 数据与授权都不重复
	if err := pg.UpsertCourse(ctx, model.Course{CourseID: "pg-idem-course", Title: "幂等课程", Status: "on"}); err != nil {
		t.Fatalf("写入课程失败: %v", err)
	}
	for _, step := range []string{"RunMigrations", "004 again", "RunMigrations again"} {
		if step == "RunMigrations" || step == "RunMigrations again" {
			if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
				t.Fatalf("%s 失败: %v", step, err)
			}
		} else {
			execMigrationFile(t, pg, ctx, "004_content.sql")
		}
	}
	courses, err := pg.ListCourses(ctx, true)
	if err != nil || len(courses) != 1 || courses[0].CourseID != "pg-idem-course" {
		t.Fatalf("重复迁移不应改变数据: %+v %v", courses, err)
	}
	var permissionCount, grantCount int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE code LIKE 'content:%'`).Scan(&permissionCount); err != nil {
		t.Fatalf("统计权限点失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE permission_code LIKE 'content:%'`).Scan(&grantCount); err != nil {
		t.Fatalf("统计授权失败: %v", err)
	}
	if permissionCount != 2 {
		t.Fatalf("重复迁移不应重复插入权限点，实际 %d", permissionCount)
	}
	// admin 2 + operator 2 + viewer 1 = 5
	if grantCount != 5 {
		t.Fatalf("重复迁移不应重复授权，实际 %d 行（期望 5）", grantCount)
	}

	// 4) 每张表只有一条 (status, sort) 索引（004 未重复建索引）
	var duplicates int
	if err := pg.pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT indexname FROM pg_indexes
			WHERE schemaname = current_schema() AND tablename IN ('courses', 'mall_goods')
			GROUP BY indexname HAVING count(*) > 1
		) AS dup`).Scan(&duplicates); err != nil {
		t.Fatalf("统计重复索引失败: %v", err)
	}
	if duplicates != 0 {
		t.Fatalf("索引名不应重复: %d", duplicates)
	}
}

// TestPostgresContentMigrationGrantsAdminUpgradePath 钉死「004 新增的权限点自动纳入 admin」：
//
//	复刻真实升级路径：先只应用 001+002+003（此时库里到处是 15 个权限点，admin 拥有这 15 个，
//	而 content:* 还不存在）→ 再执行 004 → 断言 content:* 被建出来并「自动」进了 admin，
//	且 admin 的授权集合精确等于权限点全集（不多不少）。
//
// 这条用例正是「003 的全量授权按文件名顺序先于 004 执行、不会回头覆盖新权限点」的回归防线：
// 一旦 004 里 admin 的授予写成按编码硬编码（或漏写），admin 就会缺 content:*，这里立刻失败。
func TestPostgresContentMigrationGrantsAdminUpgradePath(t *testing.T) {
	pg, ctx := newPostgresSchemaStore(t, "zwk_content_upgrade_case")

	// 1) 升级前：只有 001+002+003 —— 15 个权限点，admin 全量持有，且压根没有 content:*
	execMigrationFile(t, pg, ctx, "001_init.sql")
	execMigrationFile(t, pg, ctx, "002_catalog_admin.sql")
	execMigrationFile(t, pg, ctx, "003_rbac_config.sql")

	var beforePermissions, beforeAdmin int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM permissions`).Scan(&beforePermissions); err != nil {
		t.Fatalf("统计权限点失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_code='admin'`).Scan(&beforeAdmin); err != nil {
		t.Fatalf("统计 admin 授权失败: %v", err)
	}
	if beforePermissions != 15 || beforeAdmin != 15 {
		t.Fatalf("003 之后应为 15 个权限点且 admin 全量持有，实际 permissions=%d admin=%d", beforePermissions, beforeAdmin)
	}
	var contentBefore int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE code LIKE 'content:%'`).Scan(&contentBefore); err != nil {
		t.Fatalf("统计 content:* 失败: %v", err)
	}
	if contentBefore != 0 {
		t.Fatalf("004 之前不应存在 content:* 权限点，实际 %d", contentBefore)
	}

	// 2) 执行 004（真实升级只跑这一个文件）
	execMigrationFile(t, pg, ctx, "004_content.sql")

	// 3) admin 精确拥有全部权限点（含 content:read / content:write）
	var permissions, adminGrants int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM permissions`).Scan(&permissions); err != nil {
		t.Fatalf("统计权限点失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_code='admin'`).Scan(&adminGrants); err != nil {
		t.Fatalf("统计 admin 授权失败: %v", err)
	}
	if permissions != 17 {
		t.Fatalf("004 之后权限点应为 17 个，实际 %d", permissions)
	}
	if adminGrants != permissions {
		t.Fatalf("admin 必须全量持有 %d 个权限点，实际 %d（004 的 content:* 未自动纳入 admin）", permissions, adminGrants)
	}
	var missing string
	if err := pg.pool.QueryRow(ctx, `
		SELECT COALESCE(string_agg(p.code, ', ' ORDER BY p.code), '') FROM permissions p
		WHERE NOT EXISTS (SELECT 1 FROM role_permissions rp
			WHERE rp.role_code='admin' AND rp.permission_code = p.code)`).Scan(&missing); err != nil {
		t.Fatalf("查询 admin 缺失权限点失败: %v", err)
	}
	if missing != "" {
		t.Fatalf("admin 缺少权限点: %s", missing)
	}
	for _, code := range []string{model.PermContentRead, model.PermContentWrite} {
		var exists bool
		if err := pg.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM role_permissions WHERE role_code='admin' AND permission_code=$1)`, code).Scan(&exists); err != nil {
			t.Fatalf("查询 admin 的 %s 授权失败: %v", code, err)
		}
		if !exists {
			t.Fatalf("admin 应自动持有 %s（004 的全量授予未生效）", code)
		}
	}

	// 4) operator / viewer 的归属不变，且都在 004 里被正确授予
	var operatorGrants, viewerGrants int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_code='operator' AND permission_code LIKE 'content:%'`).Scan(&operatorGrants); err != nil {
		t.Fatalf("统计 operator 授权失败: %v", err)
	}
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_code='viewer' AND permission_code LIKE 'content:%'`).Scan(&viewerGrants); err != nil {
		t.Fatalf("统计 viewer 授权失败: %v", err)
	}
	if operatorGrants != 2 || viewerGrants != 1 {
		t.Fatalf("operator 应 2 项、viewer 应 1 项 content 权限，实际 %d / %d", operatorGrants, viewerGrants)
	}

	// 5) 再跑一次全量迁移（服务重启语义）：admin 授权不重复、仍等于权限点全集
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	var adminGrantsAfter int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE role_code='admin'`).Scan(&adminGrantsAfter); err != nil {
		t.Fatalf("统计 admin 授权失败: %v", err)
	}
	if adminGrantsAfter != permissions {
		t.Fatalf("重复迁移后 admin 授权应仍为 %d，实际 %d", permissions, adminGrantsAfter)
	}
}

// TestPostgresContentStoreIsolatedSchema 自检：本文件的 PG 用例必须跑在「非 public」的独占 schema 上，
// 防止误配连接串时把测试 DDL 打到正式库。
func TestPostgresContentStoreIsolatedSchema(t *testing.T) {
	pg, ctx := newPostgresSchemaStore(t, "zwk_content_scope_case")
	var schema string
	if err := pg.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("查询 current_schema 失败: %v", err)
	}
	if schema != "zwk_content_scope_case" {
		t.Fatalf("PG 用例必须跑在独占 schema 上，实际 %q", schema)
	}
	// 该 schema 内的表必须建在这里，而不是 public
	if err := pg.RunMigrations(ctx, migrationsDir(t)); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	var inPublic bool
	if err := pg.pool.QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'courses')`).Scan(&inPublic); err != nil {
		t.Fatalf("查询 public.courses 失败: %v", err)
	}
	if inPublic {
		t.Log("注意：public 里已存在 courses 表（可能来自正式库历史迁移），本用例只在独占 schema 内断言自己的建表结果")
	}
	var localCount int
	if err := pg.pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename IN ('courses', 'mall_goods')`).Scan(&localCount); err != nil {
		t.Fatalf("统计本 schema 表失败: %v", err)
	}
	if localCount != 2 {
		t.Fatalf("独占 schema 内应建好 2 张内容表，实际 %d", localCount)
	}
}
