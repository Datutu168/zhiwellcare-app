package model

import "time"

// Course 课程目录（后台运营维护；APP 经 /api/v1/catalog/courses 读取已上架项）。
//
// 字段与 migrations/004_content.sql 的 courses 表、以及「内容中心」接口契约一一对应。
// CreatedAt / UpdatedAt 仅内部使用：契约未列出这两个字段，故不下发（json:"-"）。
type Course struct {
	CourseID      string   `json:"courseId"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	CoverURL      string   `json:"coverUrl"`
	VideoURL      string   `json:"videoUrl"`
	DurationLabel string   `json:"durationLabel"`
	Level         string   `json:"level"`
	Tags          []string `json:"tags"`
	Status        string   `json:"status"`
	Sort          int      `json:"sort"`

	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// MallGoods 商城商品（后台运营维护；APP 经 /api/v1/catalog/goods 读取已上架项）。
//
// 保留价格字段：PriceCents 供计算/排序，PriceLabel 供展示（"¥199" 这类运营文案）。
type MallGoods struct {
	GoodsID    string   `json:"goodsId"`
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	PriceCents int      `json:"priceCents"`
	PriceLabel string   `json:"priceLabel"`
	CoverURL   string   `json:"coverUrl"`
	DetailURL  string   `json:"detailUrl"`
	Specs      []string `json:"specs"`
	Status     string   `json:"status"`
	Sort       int      `json:"sort"`

	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}
