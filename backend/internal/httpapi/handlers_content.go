package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/store"
)

// contentHandler 承载内容中心（课程 / 商城商品）的公开读取与后台 CRUD。
// 风格与 catalogHandler 一致：校验放 handler，存储错误统一由 mapStoreError 映射。
type contentHandler struct {
	store store.Combined
}

// ---------- 公开目录（APP 读取，无需登录） ----------

func (h *contentHandler) publicCourses(c *gin.Context) {
	items, err := h.store.ListCourses(c, false)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if items == nil {
		items = []model.Course{}
	}
	ok(c, items)
}

func (h *contentHandler) publicGoods(c *gin.Context) {
	items, err := h.store.ListGoods(c, false)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if items == nil {
		items = []model.MallGoods{}
	}
	ok(c, items)
}

// ---------- 课程管理（admin） ----------

func validateCourse(req *model.Course) string {
	req.CourseID = strings.TrimSpace(req.CourseID)
	if !modelIDPattern.MatchString(req.CourseID) {
		return "课程 ID 需为 2-64 位小写字母/数字/中划线"
	}
	if strings.TrimSpace(req.Title) == "" {
		return "课程标题不能为空"
	}
	if req.Status == "" {
		req.Status = "off"
	}
	if !validStatus(req.Status) {
		return "状态只能为 on/off"
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	return ""
}

func (h *contentHandler) adminListCourses(c *gin.Context) {
	items, err := h.store.ListCourses(c, true)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if items == nil {
		items = []model.Course{}
	}
	ok(c, gin.H{"items": items})
}

func (h *contentHandler) adminCreateCourse(c *gin.Context) {
	var req model.Course
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	if message := validateCourse(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertCourse(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"item": req})
}

func (h *contentHandler) adminUpdateCourse(c *gin.Context) {
	courseID := c.Param("courseId")
	if !modelIDPattern.MatchString(courseID) {
		failBadRequest(c, "课程 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	var req model.Course
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	// 路径参数为准（与游戏目录 PUT 的实现一致）
	req.CourseID = courseID
	if message := validateCourse(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertCourse(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	// PUT 返回更新后的对象（data:{item}）；{deleted:true} 只属于 DELETE。
	ok(c, gin.H{"item": req})
}

func (h *contentHandler) adminDeleteCourse(c *gin.Context) {
	courseID := c.Param("courseId")
	if !modelIDPattern.MatchString(courseID) {
		failBadRequest(c, "课程 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	if err := h.store.DeleteCourse(c, courseID); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (h *contentHandler) adminSetCourseStatus(c *gin.Context) {
	courseID := c.Param("courseId")
	if !modelIDPattern.MatchString(courseID) {
		failBadRequest(c, "课程 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.SetCourseStatus(c, courseID, req.Status); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"status": req.Status})
}

// ---------- 商城商品管理（admin） ----------

func validateGoods(req *model.MallGoods) string {
	req.GoodsID = strings.TrimSpace(req.GoodsID)
	if !modelIDPattern.MatchString(req.GoodsID) {
		return "商品 ID 需为 2-64 位小写字母/数字/中划线"
	}
	if strings.TrimSpace(req.Name) == "" {
		return "商品名称不能为空"
	}
	if req.PriceCents < 0 {
		return "商品价格不能为负数"
	}
	if req.Status == "" {
		req.Status = "off"
	}
	if !validStatus(req.Status) {
		return "状态只能为 on/off"
	}
	if req.Specs == nil {
		req.Specs = []string{}
	}
	return ""
}

func (h *contentHandler) adminListGoods(c *gin.Context) {
	items, err := h.store.ListGoods(c, true)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if items == nil {
		items = []model.MallGoods{}
	}
	ok(c, gin.H{"items": items})
}

func (h *contentHandler) adminCreateGoods(c *gin.Context) {
	var req model.MallGoods
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	if message := validateGoods(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertGoods(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"item": req})
}

func (h *contentHandler) adminUpdateGoods(c *gin.Context) {
	goodsID := c.Param("goodsId")
	if !modelIDPattern.MatchString(goodsID) {
		failBadRequest(c, "商品 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	var req model.MallGoods
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.GoodsID = goodsID
	if message := validateGoods(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertGoods(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	// PUT 返回更新后的对象（data:{item}）；{deleted:true} 只属于 DELETE。
	ok(c, gin.H{"item": req})
}

func (h *contentHandler) adminDeleteGoods(c *gin.Context) {
	goodsID := c.Param("goodsId")
	if !modelIDPattern.MatchString(goodsID) {
		failBadRequest(c, "商品 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	if err := h.store.DeleteGoods(c, goodsID); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (h *contentHandler) adminSetGoodsStatus(c *gin.Context) {
	goodsID := c.Param("goodsId")
	if !modelIDPattern.MatchString(goodsID) {
		failBadRequest(c, "商品 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.SetGoodsStatus(c, goodsID, req.Status); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"status": req.Status})
}
