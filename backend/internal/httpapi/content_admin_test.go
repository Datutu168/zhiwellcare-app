package httpapi

import (
	"net/http"
	"sort"
	"testing"

	"zhiwellcare/backend/internal/model"
)

// contentCourseBody 后台创建课程的请求体（字段与契约一致）。
func contentCourseBody(courseID, title string, sortIndex int, status string) map[string]any {
	return map[string]any{
		"courseId": courseID, "title": title, "summary": "摘要", "coverUrl": "https://cdn/cover.png",
		"videoUrl": "https://cdn/video.mp4", "durationLabel": "5 分钟", "level": "入门",
		"tags": []string{"balance", "wrist"}, "status": status, "sort": sortIndex,
	}
}

// contentGoodsBody 后台创建商品的请求体（字段与契约一致）。
func contentGoodsBody(goodsID, name string, sortIndex int, status string) map[string]any {
	return map[string]any{
		"goodsId": goodsID, "name": name, "summary": "摘要", "priceCents": 19900, "priceLabel": "¥199",
		"coverUrl": "https://cdn/goods.png", "detailUrl": "https://shop/goods", "specs": []string{"S", "M"},
		"status": status, "sort": sortIndex,
	}
}

// jsonKeys 返回 JSON 对象的字段名（升序），用于断言下发的字段集合与契约完全一致。
func jsonKeys(t *testing.T, item map[string]any) []string {
	t.Helper()
	keys := make([]string, 0, len(item))
	for key := range item {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameKeys(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	sort.Strings(want)
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestPublicContentEndpointsReadOnlyOn 公开接口：无需登录即可读，只返回 on，按 sort 升序。
func TestPublicContentEndpointsReadOnlyOn(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800014001", "admin")

	// sort 故意乱序：off 的 sort 最小，仍必须被过滤掉
	for _, body := range []map[string]any{
		contentCourseBody("course-on-b", "平衡进阶", 20, "on"),
		contentCourseBody("course-off", "未上架课程", 5, "off"),
		contentCourseBody("course-on-a", "坐姿基础", 10, "on"),
	} {
		if code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken, body); code != http.StatusOK {
			t.Fatalf("创建课程失败: %d %s", code, res.Message)
		}
	}
	for _, body := range []map[string]any{
		contentGoodsBody("goods-on-b", "握力球", 20, "on"),
		contentGoodsBody("goods-off", "未上架商品", 5, "off"),
		contentGoodsBody("goods-on-a", "训练腕带", 10, "on"),
	} {
		if code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken, body); code != http.StatusOK {
			t.Fatalf("创建商品失败: %d %s", code, res.Message)
		}
	}

	// 无 token 读取公开目录 → 200
	code, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/catalog/courses", "", nil)
	if code != http.StatusOK {
		t.Fatalf("公开课程接口应 200，实际 %d %s", code, res.Message)
	}
	courses := decode[[]map[string]any](t, res.Data)
	if len(courses) != 2 {
		t.Fatalf("公开课程应只含 on 的 2 条，实际 %+v", courses)
	}
	if courses[0]["courseId"] != "course-on-a" || courses[1]["courseId"] != "course-on-b" {
		t.Fatalf("公开课程应按 sort 升序: %+v", courses)
	}
	// 字段集合与契约逐字一致（courseId/title/summary/coverUrl/videoUrl/durationLabel/level/tags/status/sort）
	if keys := jsonKeys(t, courses[0]); !sameKeys(keys,
		"courseId", "title", "summary", "coverUrl", "videoUrl", "durationLabel", "level", "tags", "status", "sort") {
		t.Fatalf("公开课程字段与契约不一致: %v", keys)
	}
	if courses[0]["title"] != "坐姿基础" || courses[0]["durationLabel"] != "5 分钟" || courses[0]["level"] != "入门" {
		t.Fatalf("公开课程字段值异常: %+v", courses[0])
	}
	if tags, ok := courses[0]["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("公开课程 tags 应下发数组: %+v", courses[0]["tags"])
	}

	code, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/catalog/goods", "", nil)
	if code != http.StatusOK {
		t.Fatalf("公开商品接口应 200，实际 %d %s", code, res.Message)
	}
	goods := decode[[]map[string]any](t, res.Data)
	if len(goods) != 2 || goods[0]["goodsId"] != "goods-on-a" || goods[1]["goodsId"] != "goods-on-b" {
		t.Fatalf("公开商品应按 sort 升序且只含 on: %+v", goods)
	}
	if keys := jsonKeys(t, goods[0]); !sameKeys(keys,
		"goodsId", "name", "summary", "priceCents", "priceLabel", "coverUrl", "detailUrl", "specs", "status", "sort") {
		t.Fatalf("公开商品字段与契约不一致: %v", keys)
	}
	if goods[0]["priceCents"] != float64(19900) || goods[0]["priceLabel"] != "¥199" {
		t.Fatalf("商品价格字段未下发: %+v", goods[0])
	}

	// 空目录也返回数组而不是 null
	emptyEnv := newRBACTestEnv(t, "")
	emptyAdminToken, _ := emptyEnv.newUserWithRoles(t, "13800014002", "admin")
	code, res = doJSON(t, emptyEnv.engine, http.MethodGet, "/api/v1/catalog/courses", "", nil)
	if code != http.StatusOK || string(res.Data) != "[]" {
		t.Fatalf("空目录应返回 data:[]，实际 %d %s", code, res.Data)
	}
	if code, res = doJSON(t, emptyEnv.engine, http.MethodGet, "/api/v1/catalog/goods", "", nil); code != http.StatusOK || string(res.Data) != "[]" {
		t.Fatalf("空目录应返回 data:[]，实际 %d %s", code, res.Data)
	}
	if code, res = doJSON(t, emptyEnv.engine, http.MethodGet, "/api/v1/admin/goods", emptyAdminToken, nil); code != http.StatusOK {
		t.Fatalf("后台空列表应 200，实际 %d", code)
	}
	if got := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, res.Data); got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("后台空列表应为 items:[]，实际 %s", res.Data)
	}
}

// TestAdminContentPermissionEnforcement 后台接口鉴权：无权限 403、有权限 200。
func TestAdminContentPermissionEnforcement(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800014101", "admin")
	operatorToken, _ := env.newUserWithRoles(t, "13800014102", "operator")
	viewerToken, _ := env.newUserWithRoles(t, "13800014103", "viewer")
	plainToken := registerAndLogin(t, env.engine, "13800014104", "pass123")

	// 未登录 → 401
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/courses", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("未登录访问后台内容接口应 401，实际 %d", code)
	}

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   any
		want   int
	}{
		{"admin 读课程", http.MethodGet, "/api/v1/admin/courses", adminToken, nil, http.StatusOK},
		{"admin 建课程", http.MethodPost, "/api/v1/admin/courses", adminToken,
			contentCourseBody("perm-course-admin", "管理员课程", 1, "on"), http.StatusOK},
		{"operator 读课程", http.MethodGet, "/api/v1/admin/courses", operatorToken, nil, http.StatusOK},
		{"operator 建课程", http.MethodPost, "/api/v1/admin/courses", operatorToken,
			contentCourseBody("perm-course-operator", "运营课程", 2, "on"), http.StatusOK},
		{"operator 改课程", http.MethodPut, "/api/v1/admin/courses/perm-course-operator", operatorToken,
			contentCourseBody("perm-course-operator", "运营课程（改）", 2, "on"), http.StatusOK},
		{"operator 上下架", http.MethodPatch, "/api/v1/admin/courses/perm-course-operator/status", operatorToken,
			map[string]any{"status": "off"}, http.StatusOK},
		{"operator 建商品", http.MethodPost, "/api/v1/admin/goods", operatorToken,
			contentGoodsBody("perm-goods-operator", "运营商品", 1, "on"), http.StatusOK},
		{"operator 删商品", http.MethodDelete, "/api/v1/admin/goods/perm-goods-operator", operatorToken, nil, http.StatusOK},
		{"viewer 读课程", http.MethodGet, "/api/v1/admin/courses", viewerToken, nil, http.StatusOK},
		{"viewer 读商品", http.MethodGet, "/api/v1/admin/goods", viewerToken, nil, http.StatusOK},
		{"viewer 建课程被拒", http.MethodPost, "/api/v1/admin/courses", viewerToken,
			contentCourseBody("perm-course-viewer", "只读课程", 3, "on"), http.StatusForbidden},
		{"viewer 建商品被拒", http.MethodPost, "/api/v1/admin/goods", viewerToken,
			contentGoodsBody("perm-goods-viewer", "只读商品", 3, "on"), http.StatusForbidden},
		{"viewer 上下架被拒", http.MethodPatch, "/api/v1/admin/courses/perm-course-operator/status", viewerToken,
			map[string]any{"status": "on"}, http.StatusForbidden},
		{"普通用户读课程被拒", http.MethodGet, "/api/v1/admin/courses", plainToken, nil, http.StatusForbidden},
		{"普通用户建课程被拒", http.MethodPost, "/api/v1/admin/courses", plainToken,
			contentCourseBody("perm-course-plain", "越权课程", 4, "on"), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, res := doJSON(t, env.engine, tc.method, tc.path, tc.token, tc.body)
			if code != tc.want {
				t.Fatalf("%s %s 期望 %d 实际 %d（%s）", tc.method, tc.path, tc.want, code, res.Message)
			}
		})
	}

	// operator 建的课程确实落库（后台列表含 off）
	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/courses", operatorToken, nil)
	list := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, res.Data)
	found := false
	for _, item := range list.Items {
		if item["courseId"] == "perm-course-operator" && item["title"] == "运营课程（改）" && item["status"] == "off" {
			found = true
		}
	}
	if !found {
		t.Fatalf("operator 的改动未生效: %+v", list.Items)
	}
}

// TestAdminContentValidationAndContract 参数校验（ID / status / 必填）与响应契约。
func TestAdminContentValidationAndContract(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800014201", "admin")

	// --- 课程：ID 校验 ---
	badIDs := []string{"A", "UPPER-case", "bad_id", "-leading", "有中文"}
	for _, badID := range badIDs {
		if code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken,
			contentCourseBody(badID, "非法 ID", 1, "on")); code != http.StatusBadRequest {
			t.Fatalf("非法课程 ID %q 应 400，实际 %d（%s）", badID, code, res.Message)
		}
	}
	// 标题必填
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken,
		map[string]any{"courseId": "course-no-title"}); code != http.StatusBadRequest {
		t.Fatalf("缺标题应 400，实际 %d", code)
	}
	// status 非法值
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken,
		contentCourseBody("course-bad-status", "状态非法", 1, "published")); code != http.StatusBadRequest {
		t.Fatalf("非法 status 应 400，实际 %d", code)
	}
	// 请求体不是 JSON
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken, "not-json"); code != http.StatusBadRequest {
		t.Fatalf("非法请求体应 400，实际 %d", code)
	}

	// --- 课程：创建契约 data:{item}，未传 status 默认 off ---
	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/courses", adminToken, map[string]any{
		"courseId": "course-contract", "title": "契约课程", "sort": 3,
	})
	if code != http.StatusOK {
		t.Fatalf("创建课程应 200，实际 %d %s", code, res.Message)
	}
	created := decode[struct {
		Item model.Course `json:"item"`
	}](t, res.Data)
	if created.Item.CourseID != "course-contract" || created.Item.Status != "off" || created.Item.Sort != 3 {
		t.Fatalf("创建课程返回异常: %+v", created.Item)
	}
	if created.Item.Tags == nil {
		t.Fatalf("创建课程返回的 tags 应为空数组: %+v", created.Item)
	}
	// 未上架 → 公开目录看不到
	if public := decode[[]map[string]any](t, mustGet(t, env.engine, "/api/v1/catalog/courses")); len(public) != 0 {
		t.Fatalf("off 课程不应出现在公开目录: %+v", public)
	}

	// --- 后台列表契约 data:{items:[...]}（含 off） ---
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/courses", adminToken, nil)
	list := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, res.Data)
	if len(list.Items) != 1 || list.Items[0]["courseId"] != "course-contract" {
		t.Fatalf("后台课程列表契约异常: %+v", list.Items)
	}

	// --- PATCH 上下架：非法 status → 400；非法 ID → 400；正常 → 200 ---
	if code, _ := doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/courses/course-contract/status", adminToken,
		map[string]any{"status": "bogus"}); code != http.StatusBadRequest {
		t.Fatalf("非法 status 应 400，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/courses/Bad_ID/status", adminToken,
		map[string]any{"status": "on"}); code != http.StatusBadRequest {
		t.Fatalf("非法路径 ID 应 400，实际 %d", code)
	}
	code, res = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/courses/course-contract/status", adminToken,
		map[string]any{"status": "on"})
	if code != http.StatusOK {
		t.Fatalf("上架失败: %d %s", code, res.Message)
	}
	if got := decode[struct {
		Status string `json:"status"`
	}](t, res.Data); got.Status != "on" {
		t.Fatalf("PATCH 应返回 status:on，实际 %+v", got)
	}
	if public := decode[[]map[string]any](t, mustGet(t, env.engine, "/api/v1/catalog/courses")); len(public) != 1 {
		t.Fatalf("上架后公开目录应可见: %+v", public)
	}

	// --- PUT：返回更新后的对象 data:{item}（{deleted:true} 只属于 DELETE）；路径参数为准 ---
	code, res = doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/courses/course-contract", adminToken, map[string]any{
		"courseId": "ignored-by-path", "title": "契约课程（改）", "sort": 4, "status": "on",
	})
	if code != http.StatusOK {
		t.Fatalf("更新课程失败: %d %s", code, res.Message)
	}
	putItem := decode[struct {
		Item     model.Course `json:"item"`
		Deleted  *bool        `json:"deleted"`
	}](t, res.Data)
	if putItem.Item.CourseID != "course-contract" || putItem.Item.Title != "契约课程（改）" ||
		putItem.Item.Sort != 4 || putItem.Item.Status != "on" {
		t.Fatalf("PUT 应返回更新后的 item: %+v", putItem.Item)
	}
	if putItem.Deleted != nil {
		t.Fatalf("PUT 不应返回 deleted 字段（那属于 DELETE）: %s", res.Data)
	}
	updated := decode[[]map[string]any](t, mustGet(t, env.engine, "/api/v1/catalog/courses"))
	if len(updated) != 1 || updated[0]["title"] != "契约课程（改）" || updated[0]["sort"] != float64(4) {
		t.Fatalf("PUT 未按路径 ID 更新: %+v", updated)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/courses/Bad_ID", adminToken,
		contentCourseBody("x", "非法", 1, "on")); code != http.StatusBadRequest {
		t.Fatalf("PUT 非法路径 ID 应 400，实际 %d", code)
	}

	// --- DELETE：data:{deleted:true}；重复删除 404；非法 ID 400 ---
	code, res = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/courses/course-contract", adminToken, nil)
	if code != http.StatusOK || !decode[struct {
		Deleted bool `json:"deleted"`
	}](t, res.Data).Deleted {
		t.Fatalf("删除课程失败: %d %s", code, res.Message)
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/courses/course-contract", adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("重复删除应 404，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/courses/Bad_ID", adminToken, nil); code != http.StatusBadRequest {
		t.Fatalf("DELETE 非法 ID 应 400，实际 %d", code)
	}

	// --- 商品：ID / 名称 / 价格 / status 校验 + 契约 ---
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken,
		contentGoodsBody("Bad_ID", "非法商品", 1, "on")); code != http.StatusBadRequest {
		t.Fatalf("非法商品 ID 应 400，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken,
		map[string]any{"goodsId": "goods-no-name"}); code != http.StatusBadRequest {
		t.Fatalf("缺商品名称应 400，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken,
		map[string]any{"goodsId": "goods-neg", "name": "负价商品", "priceCents": -1}); code != http.StatusBadRequest {
		t.Fatalf("负价格应 400，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken,
		contentGoodsBody("goods-bad-status", "状态非法", 1, "online")); code != http.StatusBadRequest {
		t.Fatalf("非法 status 应 400，实际 %d", code)
	}

	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken,
		contentGoodsBody("goods-contract", "契约商品", 1, "on"))
	if code != http.StatusOK {
		t.Fatalf("创建商品应 200，实际 %d %s", code, res.Message)
	}
	goodsItem := decode[struct {
		Item map[string]any `json:"item"`
	}](t, res.Data)
	if goodsItem.Item["goodsId"] != "goods-contract" || goodsItem.Item["priceCents"] != float64(19900) {
		t.Fatalf("创建商品返回异常: %+v", goodsItem.Item)
	}

	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/goods", adminToken, nil)
	goodsList := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, res.Data)
	if len(goodsList.Items) != 1 || goodsList.Items[0]["goodsId"] != "goods-contract" {
		t.Fatalf("后台商品列表契约异常: %+v", goodsList.Items)
	}

	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/goods/goods-contract/status", adminToken,
		map[string]any{"status": "off"}); code != http.StatusOK {
		t.Fatalf("商品下架失败: %d", code)
	}
	if public := decode[[]map[string]any](t, mustGet(t, env.engine, "/api/v1/catalog/goods")); len(public) != 0 {
		t.Fatalf("下架商品不应出现在公开目录: %+v", public)
	}
	if code, res = doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/goods/goods-contract", adminToken,
		contentGoodsBody("goods-contract", "契约商品（改）", 2, "on")); code != http.StatusOK {
		t.Fatalf("更新商品失败: %d %s", code, res.Message)
	}
	putGoods := decode[struct {
		Item    model.MallGoods `json:"item"`
		Deleted *bool           `json:"deleted"`
	}](t, res.Data)
	if putGoods.Item.GoodsID != "goods-contract" || putGoods.Item.Name != "契约商品（改）" ||
		putGoods.Item.Sort != 2 || putGoods.Item.Status != "on" || putGoods.Item.PriceCents != 19900 {
		t.Fatalf("PUT 商品应返回更新后的 item: %+v", putGoods.Item)
	}
	if putGoods.Deleted != nil {
		t.Fatalf("PUT 商品不应返回 deleted 字段: %s", res.Data)
	}
	if code, _ = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/goods/goods-contract", adminToken, nil); code != http.StatusOK {
		t.Fatalf("删除商品失败: %d", code)
	}
	if code, _ = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/goods/goods-contract", adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("重复删除商品应 404，实际 %d", code)
	}
	// 改不存在的课程状态 → 404（内容接口用 404，而不是 401）
	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/courses/course-missing/status", adminToken,
		map[string]any{"status": "on"}); code != http.StatusNotFound {
		t.Fatalf("不存在的课程改状态应 404，实际 %d", code)
	}
}
