-- =====================================================================
-- 智为康乐消费版 · 内容中心（课程 / 商城商品）+ content:* 权限点（004）
--
-- 幂等：全部 IF NOT EXISTS / ON CONFLICT DO NOTHING；服务每次启动都会整体执行一遍，
--       重复执行不报错、不重复写数据。
-- 兼容：仅使用 PostgreSQL 11+ 通用语法，可在 golang:1.23-alpine 部署的 PG 上运行。
-- =====================================================================

-- ---------------------------------------------------------------------
-- 1) 课程目录
--    后台运营维护，APP 经公开接口 GET /api/v1/catalog/courses 读取已上架项（按 sort 升序）。
--    status ∈ on | off（'off' 为默认值：新建内容默认不上架）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS courses (
    course_id      text PRIMARY KEY,
    title          text NOT NULL,
    summary        text NOT NULL DEFAULT '',
    cover_url      text NOT NULL DEFAULT '',
    video_url      text NOT NULL DEFAULT '',
    duration_label text NOT NULL DEFAULT '',
    level          text NOT NULL DEFAULT '',
    tags           text[] NOT NULL DEFAULT '{}',
    status         text NOT NULL DEFAULT 'off',       -- on | off
    sort           int NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- 2) 商城商品（保留价格字段：price_cents 供计算/排序，price_label 供展示）
--    公开接口 GET /api/v1/catalog/goods，过滤与排序规则同课程。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS mall_goods (
    goods_id     text PRIMARY KEY,
    name         text NOT NULL,
    summary      text NOT NULL DEFAULT '',
    price_cents  int NOT NULL DEFAULT 0,
    price_label  text NOT NULL DEFAULT '',
    cover_url    text NOT NULL DEFAULT '',
    detail_url   text NOT NULL DEFAULT '',
    specs        text[] NOT NULL DEFAULT '{}',
    status       text NOT NULL DEFAULT 'off',        -- on | off
    sort         int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- 公开列表固定按「已上架 + sort 升序」扫描，索引与之对齐。
CREATE INDEX IF NOT EXISTS idx_courses_status_sort ON courses(status, sort);
CREATE INDEX IF NOT EXISTS idx_mall_goods_status_sort ON mall_goods(status, sort);

-- ---------------------------------------------------------------------
-- 3) 权限点种子：content:read / content:write
-- ---------------------------------------------------------------------
INSERT INTO permissions (code, name, group_name, description) VALUES
    ('content:read',  '内容查看', 'content', '查看课程与商城商品'),
    ('content:write', '内容维护', 'content', '新增/修改/删除课程与商城商品及上下架')
ON CONFLICT (code) DO NOTHING;

-- admin：与 003 完全同构的全量授予（SELECT … FROM permissions，不带 code 过滤），
-- 因此本迁移新增的 content:read / content:write 会被「自动纳入」admin，不需要按编码逐个补。
-- 之所以必须在 004 里再写一次：003 的全量授权发生在 004 建权限点之前（迁移按文件名顺序执行），
-- 且 003 每次启动只处理「当时已存在」的权限点，不会回头覆盖后来新增的。
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'admin', code FROM permissions
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- operator：课程/商品日常运营（读写）
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'operator', code FROM permissions WHERE code IN ('content:read', 'content:write')
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- viewer：只读
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'viewer', code FROM permissions WHERE code IN ('content:read')
ON CONFLICT (role_code, permission_code) DO NOTHING;
