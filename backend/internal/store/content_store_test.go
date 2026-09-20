package store

import (
	"context"
	"errors"
	"testing"

	"zhiwellcare/backend/internal/model"
)

// TestMemoryContentStoreCRUDAndOrdering 课程 CRUD / 上下架 / 公开过滤 / sort 排序。
func TestMemoryContentStoreCRUDAndOrdering(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	// 空目录：公开接口返回空切片（不是 null）
	items, err := data.ListCourses(ctx, false)
	if err != nil || len(items) != 0 {
		t.Fatalf("空目录异常: %v %v", items, err)
	}

	// sort 故意乱序写入：2 / 1 / 5（最后一门 off）
	seeds := []model.Course{
		{CourseID: "course-b", Title: "平衡训练进阶", Sort: 2, Status: "on", Tags: []string{"balance"}},
		{CourseID: "course-a", Title: "坐姿基础", Sort: 1, Status: "on"},
		{CourseID: "course-off", Title: "未上架课程", Sort: 5, Status: "off"},
	}
	for _, seed := range seeds {
		if err := data.UpsertCourse(ctx, seed); err != nil {
			t.Fatalf("写入课程失败: %v", err)
		}
	}

	// 公开列表（includeOff=false，对应 GET /api/v1/catalog/courses）：只含 on，按 sort 升序
	public, err := data.ListCourses(ctx, false)
	if err != nil {
		t.Fatalf("ListCourses 失败: %v", err)
	}
	if len(public) != 2 || public[0].CourseID != "course-a" || public[1].CourseID != "course-b" {
		t.Fatalf("公开课程应按 sort 升序且只含 on: %+v", public)
	}
	for _, item := range public {
		if item.Status != "on" || item.CourseID == "course-off" {
			t.Fatalf("公开列表不应包含下架课程: %+v", public)
		}
	}
	// tags 未传时归一为空切片（与 PG 的 text[] NOT NULL DEFAULT '{}' 一致）
	if public[0].Tags == nil || len(public[0].Tags) != 0 {
		t.Fatalf("tags 应归一为空切片: %+v", public[0].Tags)
	}

	// 后台列表（includeOff=true，对应 GET /api/v1/admin/courses）：含 off，仍按 sort 升序
	all, err := data.ListCourses(ctx, true)
	if err != nil || len(all) != 3 {
		t.Fatalf("后台课程列表应含 off 共 3 条: %d %v", len(all), err)
	}
	if all[0].CourseID != "course-a" || all[1].CourseID != "course-b" || all[2].CourseID != "course-off" {
		t.Fatalf("后台列表顺序异常: %+v", all)
	}
	if all[2].Status != "off" {
		t.Fatalf("后台列表必须含下架项: %+v", all[2])
	}

	// upsert：同 ID 覆盖字段且不新增记录，createdAt 保持
	created := all[0].CreatedAt
	updated := model.Course{CourseID: "course-a", Title: "坐姿基础（改）", Sort: 1, Status: "on", Summary: "改后摘要"}
	if err := data.UpsertCourse(ctx, updated); err != nil {
		t.Fatalf("更新课程失败: %v", err)
	}
	after, _ := data.ListCourses(ctx, true)
	if len(after) != 3 {
		t.Fatalf("upsert 不应新增记录: %d", len(after))
	}
	if after[0].Title != "坐姿基础（改）" || after[0].Summary != "改后摘要" {
		t.Fatalf("upsert 未覆盖字段: %+v", after[0])
	}
	if !after[0].CreatedAt.Equal(created) {
		t.Fatalf("upsert 不应改变创建时间: %v -> %v", created, after[0].CreatedAt)
	}

	// 上下架：off → 公开列表消失；缺失 ID → ErrNotFound
	if err := data.SetCourseStatus(ctx, "course-b", "off"); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if public, _ = data.ListCourses(ctx, false); len(public) != 1 || public[0].CourseID != "course-a" {
		t.Fatalf("下架后公开列表异常: %+v", public)
	}
	if err := data.SetCourseStatus(ctx, "course-b", "on"); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	if public, _ = data.ListCourses(ctx, false); len(public) != 2 {
		t.Fatalf("上架后公开列表异常: %+v", public)
	}
	if err := data.SetCourseStatus(ctx, "no-such-course", "on"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的课程改状态应返回 ErrNotFound，实际 %v", err)
	}

	// 删除：成功 → 再删 → ErrNotFound
	if err := data.DeleteCourse(ctx, "course-off"); err != nil {
		t.Fatalf("删除课程失败: %v", err)
	}
	if all, _ = data.ListCourses(ctx, true); len(all) != 2 {
		t.Fatalf("删除后列表异常: %+v", all)
	}
	if err := data.DeleteCourse(ctx, "course-off"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// TestMemoryContentStoreGoods 商城商品：价格字段保留 + 上下架 + 排序 + 删除。
func TestMemoryContentStoreGoods(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	seeds := []model.MallGoods{
		{GoodsID: "goods-b", Name: "握力球", PriceCents: 5900, PriceLabel: "¥59", Sort: 3, Status: "on",
			Specs: []string{"软", "硬"}},
		{GoodsID: "goods-a", Name: "训练腕带", PriceCents: 19900, PriceLabel: "¥199", Sort: 1, Status: "on"},
		{GoodsID: "goods-off", Name: "未上架商品", PriceCents: 0, Sort: 2, Status: "off"},
	}
	for _, seed := range seeds {
		if err := data.UpsertGoods(ctx, seed); err != nil {
			t.Fatalf("写入商品失败: %v", err)
		}
	}

	// 公开列表（includeOff=false，对应 GET /api/v1/catalog/goods）：只返回 on，按 sort 升序
	public, err := data.ListGoods(ctx, false)
	if err != nil {
		t.Fatalf("ListGoods 失败: %v", err)
	}
	if len(public) != 2 || public[0].GoodsID != "goods-a" || public[1].GoodsID != "goods-b" {
		t.Fatalf("公开商品应按 sort 升序且只含 on: %+v", public)
	}
	for _, item := range public {
		if item.Status != "on" || item.GoodsID == "goods-off" {
			t.Fatalf("公开列表不应包含下架商品: %+v", public)
		}
	}
	// 价格字段原样保留
	if public[0].PriceCents != 19900 || public[0].PriceLabel != "¥199" {
		t.Fatalf("价格字段异常: %+v", public[0])
	}
	if len(public[1].Specs) != 2 {
		t.Fatalf("规格字段异常: %+v", public[1].Specs)
	}
	if public[0].Specs == nil {
		t.Fatalf("specs 应归一为空切片: %+v", public[0].Specs)
	}

	// 后台列表（includeOff=true，对应 GET /api/v1/admin/goods）：含下架项，同样按 sort 升序 1/2/3
	all, err := data.ListGoods(ctx, true)
	if err != nil || len(all) != 3 {
		t.Fatalf("后台商品列表应含 off 共 3 条: %+v %v", all, err)
	}
	if all[0].GoodsID != "goods-a" || all[1].GoodsID != "goods-off" || all[2].GoodsID != "goods-b" {
		t.Fatalf("后台商品列表应按 sort 升序: %+v", all)
	}
	if all[1].Status != "off" {
		t.Fatalf("后台列表必须含下架项: %+v", all[1])
	}

	// 空状态默认 off
	if err := data.UpsertGoods(ctx, model.MallGoods{GoodsID: "goods-default", Name: "默认状态"}); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	all, _ = data.ListGoods(ctx, true)
	for _, item := range all {
		if item.GoodsID == "goods-default" && item.Status != "off" {
			t.Fatalf("空状态应默认 off，实际 %q", item.Status)
		}
	}

	if err := data.SetGoodsStatus(ctx, "goods-off", "on"); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	if public, _ = data.ListGoods(ctx, false); len(public) != 3 {
		t.Fatalf("上架后公开列表异常: %+v", public)
	}
	if err := data.SetGoodsStatus(ctx, "no-such-goods", "on"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的商品改状态应返回 ErrNotFound，实际 %v", err)
	}
	if err := data.DeleteGoods(ctx, "goods-b"); err != nil {
		t.Fatalf("删除商品失败: %v", err)
	}
	if err := data.DeleteGoods(ctx, "goods-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}
