-- =====================================================================
-- 智为康乐消费版 · RBAC / 配置中心 / 设备事实表 / 资产登记 / 训练记录按月分区（003）
--
-- 幂等：全部 IF NOT EXISTS / 容错 ALTER / DO 块判定；服务每次启动都会整体执行一遍，
--       重复执行不报错、不重复搬数据。
-- 兼容：仅使用 PostgreSQL 11+ 通用语法，可在 golang:1.23-alpine 部署的 PG 上运行。
-- =====================================================================

-- ---------------------------------------------------------------------
-- 1) RBAC：角色 / 权限点 / 角色-权限 / 用户-角色
--    设计：roles + permissions 为字典表；user_roles 是「用户实际拥有的角色」事实来源；
--          users.role 仅作为兼容旧逻辑的派生标记（拥有 admin 角色 → 'admin'）。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    code        text PRIMARY KEY,
    name        text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    builtin     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS permissions (
    code        text PRIMARY KEY,
    name        text NOT NULL DEFAULT '',
    group_name  text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_code       text NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    permission_code text NOT NULL REFERENCES permissions(code) ON DELETE CASCADE,
    PRIMARY KEY (role_code, permission_code)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_code  text NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    granted_at timestamptz NOT NULL DEFAULT now(),
    granted_by uuid,
    PRIMARY KEY (user_id, role_code)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_role ON user_roles(role_code);

-- 权限点种子（任务约定的 15 个）
INSERT INTO permissions (code, name, group_name, description) VALUES
    ('device:read',     '设备型号查看',   'device',    '查看设备型号目录'),
    ('device:write',    '设备型号维护',   'device',    '新增/修改/删除设备型号与设备映射'),
    ('game:read',       '游戏目录查看',   'game',      '查看游戏目录'),
    ('game:write',      '游戏目录维护',   'game',      '新增/修改/删除游戏与上下架'),
    ('record:read',     '训练记录查看',   'record',    '查看训练记录与仪表盘统计'),
    ('user:read',       '用户查看',       'user',      '查看用户列表'),
    ('user:write',      '用户维护',       'user',      '启用/停用用户、分配用户角色'),
    ('role:read',       '角色权限查看',   'role',      '查看角色与权限点'),
    ('role:write',      '角色权限维护',   'role',      '新增/修改/删除角色与角色权限'),
    ('config:read',     '配置查看',       'config',    '查看配置中心 JSONB 配置项'),
    ('config:write',    '配置维护',       'config',    '修改配置中心配置项'),
    ('asset:read',      '资产查看',       'asset',     '查看 Web 包/游戏资源/固件资产'),
    ('asset:write',     '资产维护',       'asset',     '登记资产、发布/下线、删除资产'),
    ('whitelist:read',  '设备白名单查看', 'whitelist', '查看设备白名单'),
    ('whitelist:write', '设备白名单维护', 'whitelist', '增删设备白名单')
ON CONFLICT (code) DO NOTHING;

-- 角色种子：admin / operator / viewer（均为内置角色，不可删除）
INSERT INTO roles (code, name, description, builtin) VALUES
    ('admin',    '超级管理员', '拥有全部权限（含后续新增权限点），内置角色不可删除', true),
    ('operator', '运营',       '设备/游戏/白名单/资产的日常运营，可读配置与训练记录', true),
    ('viewer',   '只读观察',   '仅查看类权限，适合客服与数据查看',                  true)
ON CONFLICT (code) DO NOTHING;

-- admin：全部权限（用 SELECT 全量授予，后续新增权限点会自动纳入）
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'admin', code FROM permissions
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- operator
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'operator', code FROM permissions WHERE code IN (
    'device:read', 'device:write', 'game:read', 'game:write', 'record:read',
    'user:read', 'asset:read', 'asset:write', 'whitelist:read', 'whitelist:write',
    'config:read'
)
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- viewer
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'viewer', code FROM permissions WHERE code IN (
    'device:read', 'game:read', 'record:read', 'user:read', 'role:read',
    'config:read', 'asset:read', 'whitelist:read'
)
ON CONFLICT (role_code, permission_code) DO NOTHING;

-- 兼容既有数据：历史 users.role='admin' 的用户自动补齐 admin 角色
INSERT INTO user_roles (user_id, role_code)
SELECT id, 'admin' FROM users WHERE role = 'admin'
ON CONFLICT (user_id, role_code) DO NOTHING;

-- ---------------------------------------------------------------------
-- 2) 配置中心（JSONB，value 为任意 JSON）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS app_config (
    key         text PRIMARY KEY,
    value       jsonb NOT NULL DEFAULT '{}'::jsonb,
    description text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid
);

INSERT INTO app_config (key, value, description) VALUES
    ('catalog.version',       '"1.0.0"'::jsonb, '目录配置版本号（客户端缓存失效用）'),
    ('catalog.gray.enabled',  'false'::jsonb,   '目录灰度开关（false 时全量下发）'),
    ('feature.firmware_ota',  'true'::jsonb,    '固件 OTA 开关'),
    ('feature.samples_upload','true'::jsonb,    '训练高频采样文件上传开关')
ON CONFLICT (key) DO NOTHING;

-- ---------------------------------------------------------------------
-- 3) 设备事实表：白名单与「设备标识 → 型号」映射
--    PostgreSQL 为唯一事实来源，cache.Devices 仅做读缓存（写后失效）。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS device_whitelist (
    device_key text PRIMARY KEY,
    model_id   text NOT NULL DEFAULT '',
    note       text NOT NULL DEFAULT '',
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS device_mappings (
    device_key text PRIMARY KEY,
    model_id   text NOT NULL,
    note       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- 4) 资产登记：Web 包 / 游戏资源 / 固件（对象存储中的对象 + 版本元数据）
--    kind  ∈ web | game | firmware
--    status ∈ draft | published | offline
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS assets (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind         text NOT NULL,
    ref_id       text NOT NULL DEFAULT '',
    version      text NOT NULL,
    filename     text NOT NULL,
    object_key   text NOT NULL,
    size         bigint NOT NULL DEFAULT 0,
    sha256       text NOT NULL DEFAULT '',
    content_type text NOT NULL DEFAULT '',
    notes        text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'draft',
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    CONSTRAINT assets_kind_status_check CHECK (kind IN ('web', 'game', 'firmware')),
    CONSTRAINT assets_status_check CHECK (status IN ('draft', 'published', 'offline')),
    CONSTRAINT assets_kind_ref_version_filename_key UNIQUE (kind, ref_id, version, filename)
);

CREATE INDEX IF NOT EXISTS idx_assets_kind_ref ON assets(kind, ref_id, status);

-- ---------------------------------------------------------------------
-- 5) 目录扩展配置（JSONB）：机型/游戏的扩展配置，如
--    {"handleTags":[...],"difficulty":{...}}；原有 text[]/int[] 列保持不动。
-- ---------------------------------------------------------------------
ALTER TABLE device_models ADD COLUMN IF NOT EXISTS config jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE game_catalog  ADD COLUMN IF NOT EXISTS config jsonb NOT NULL DEFAULT '{}'::jsonb;

-- ---------------------------------------------------------------------
-- 6) training_records 按月 RANGE 分区（分区键 completed_at）
--
-- 设计取舍（不停机、可重复执行、不丢数据）：
--   * 分区表的唯一索引必须包含分区键，因此原 `record_id text UNIQUE` 改为复合主键
--     (record_id, completed_at)；写入侧改为
--     `ON CONFLICT (record_id, completed_at) DO NOTHING`，
--     保持「同一次训练重复上报（recordId + completedAt 相同）只落 1 行」的幂等语义。
--   * 旧普通表不 DROP / 不 TRUNCATE：仅改名为 training_records_legacy 后原样留存
--     （数据完整保留，便于对账），再把数据复制进新分区表，原 id 一并保留。
--   * 新表的主键约束与序列显式改名（training_records_part_pkey / training_records_part_id_seq），
--     避免与留存的旧对象（training_records_pkey / training_records_id_seq）重名冲突；
--     业务索引则沿用 idx_training_records_* 原名（先让归档表的索引改名腾出名字，见文末）。
--   * 每个 DO 块都是单个语句，隐式事务保证「改名 + 建表 + 搬数据」要么全成要么全回滚。
--   * 顺序至关重要：先建 DEFAULT 分区 → 再按 legacy 数据跨度预建月分区 → 最后搬数据。
--     PostgreSQL 在「默认分区里已经存在属于该月的行」时拒绝新建该月分区
--     （updated partition constraint for default partition would be violated），
--     所以必须让历史数据直接落进它自己的月分区；ensure_partition 里也带了
--     「先搬走 DEFAULT 里属于该月的行再建分区」的兜底逻辑。
--   * 最终保证：DEFAULT 分区兜底 + 写入前 training_records_ensure_partition() 按需建分区，
--     任何月份的写入都不会因缺分区而失败。
-- ---------------------------------------------------------------------
DO $$
DECLARE
    rel_kind "char";
BEGIN
    SELECT c.relkind INTO rel_kind
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = current_schema() AND c.relname = 'training_records';

    IF rel_kind IS NULL THEN
        -- 全新库：直接建分区父表
        CREATE TABLE training_records (
            id                bigint GENERATED BY DEFAULT AS IDENTITY (SEQUENCE NAME training_records_part_id_seq),
            record_id         text NOT NULL,
            user_id           uuid REFERENCES users(id) ON DELETE SET NULL,
            game_id           text NOT NULL,
            game_name         text NOT NULL DEFAULT '',
            device_model_id   text NOT NULL DEFAULT '',
            device_model_name text NOT NULL DEFAULT '',
            capability_tags   text[] NOT NULL DEFAULT '{}',
            statistics        jsonb NOT NULL DEFAULT '{}'::jsonb,
            client_version    text NOT NULL DEFAULT '',
            completed_at      timestamptz NOT NULL,
            uploaded_at       timestamptz NOT NULL DEFAULT now(),
            created_at        timestamptz NOT NULL DEFAULT now(),
            CONSTRAINT training_records_part_pkey PRIMARY KEY (record_id, completed_at)
        ) PARTITION BY RANGE (completed_at);

        CREATE TABLE training_records_default PARTITION OF training_records DEFAULT;
    ELSIF rel_kind = 'r' THEN
        -- 已有普通表（含数据）：改名留存 → 建分区表（数据在下面的 DO 块里搬）
        ALTER TABLE training_records RENAME TO training_records_legacy;

        CREATE TABLE training_records (
            id                bigint GENERATED BY DEFAULT AS IDENTITY (SEQUENCE NAME training_records_part_id_seq),
            record_id         text NOT NULL,
            user_id           uuid REFERENCES users(id) ON DELETE SET NULL,
            game_id           text NOT NULL,
            game_name         text NOT NULL DEFAULT '',
            device_model_id   text NOT NULL DEFAULT '',
            device_model_name text NOT NULL DEFAULT '',
            capability_tags   text[] NOT NULL DEFAULT '{}',
            statistics        jsonb NOT NULL DEFAULT '{}'::jsonb,
            client_version    text NOT NULL DEFAULT '',
            completed_at      timestamptz NOT NULL,
            uploaded_at       timestamptz NOT NULL DEFAULT now(),
            created_at        timestamptz NOT NULL DEFAULT now(),
            CONSTRAINT training_records_part_pkey PRIMARY KEY (record_id, completed_at)
        ) PARTITION BY RANGE (completed_at);

        CREATE TABLE training_records_default PARTITION OF training_records DEFAULT;

        COMMENT ON TABLE training_records_legacy IS
            '003 迁移前的老表（数据保留的只读归档，业务不再写入；确认无误后可人工 DROP）';
    END IF;
END $$;

-- 按需创建月分区（写入前由后端调用；并发创建同名分区时忽略冲突，不阻断业务写入）。
CREATE OR REPLACE FUNCTION training_records_ensure_partition(target timestamptz)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    part_start timestamptz := date_trunc('month', target);
    part_end   timestamptz := part_start + interval '1 month';
    part_name  text := 'training_records_' || to_char(part_start, 'YYYYMM');
    carried    bigint := 0;
BEGIN
    IF to_regclass(part_name) IS NOT NULL THEN
        RETURN part_name;
    END IF;

    BEGIN
        -- DEFAULT 分区里若已有属于该月的行，必须先搬走，否则建分区会被拒绝
        IF to_regclass('training_records_default') IS NOT NULL THEN
            IF to_regclass('pg_temp.training_records_part_carry') IS NOT NULL THEN
                DROP TABLE training_records_part_carry;
            END IF;
            CREATE TEMP TABLE training_records_part_carry AS
                SELECT id, record_id, user_id, game_id, game_name, device_model_id, device_model_name,
                       capability_tags, statistics, client_version, completed_at, uploaded_at, created_at
                FROM training_records_default
                WHERE completed_at >= part_start AND completed_at < part_end;
            carried := (SELECT count(*) FROM training_records_part_carry);
            IF carried > 0 THEN
                DELETE FROM training_records_default
                WHERE completed_at >= part_start AND completed_at < part_end;
            END IF;
        END IF;

        EXECUTE format('CREATE TABLE %I PARTITION OF training_records FOR VALUES FROM (%L) TO (%L)',
                       part_name, part_start, part_end);

        IF carried > 0 THEN
            INSERT INTO training_records (id, record_id, user_id, game_id, game_name, device_model_id,
                                          device_model_name, capability_tags, statistics, client_version,
                                          completed_at, uploaded_at, created_at)
            SELECT id, record_id, user_id, game_id, game_name, device_model_id,
                   device_model_name, capability_tags, statistics, client_version,
                   completed_at, uploaded_at, created_at
            FROM training_records_part_carry;
        END IF;
        IF to_regclass('pg_temp.training_records_part_carry') IS NOT NULL THEN
            DROP TABLE training_records_part_carry;
        END IF;
    EXCEPTION WHEN duplicate_table OR unique_violation THEN
        NULL; -- 并发下另一个连接刚建好同名分区
    END;

    RETURN part_name;
END $$;

-- 预建月分区：覆盖 legacy 数据最早月份 ~ 未来 5 个月（最多 120 个月，防异常数据拖垮迁移）
DO $$
DECLARE
    first_month timestamptz;
    last_month  timestamptz := date_trunc('month', now()) + make_interval(months => 5);
    cursor_month timestamptz;
    guard integer := 0;
BEGIN
    IF to_regclass('training_records_legacy') IS NOT NULL THEN
        SELECT date_trunc('month', COALESCE(min(completed_at), now())) INTO first_month
        FROM training_records_legacy;
    END IF;
    IF first_month IS NULL THEN
        first_month := date_trunc('month', now()) - make_interval(months => 1);
    END IF;
    IF first_month > last_month THEN
        first_month := date_trunc('month', now()) - make_interval(months => 1);
    END IF;

    cursor_month := first_month;
    WHILE cursor_month <= last_month AND guard < 120 LOOP
        PERFORM training_records_ensure_partition(cursor_month);
        cursor_month := cursor_month + interval '1 month';
        guard := guard + 1;
    END LOOP;
END $$;

-- 搬历史数据（分区已就位；ON CONFLICT 保证重复执行不会重复插入）
DO $$
BEGIN
    IF to_regclass('training_records_legacy') IS NOT NULL THEN
        INSERT INTO training_records (id, record_id, user_id, game_id, game_name, device_model_id,
                                      device_model_name, capability_tags, statistics, client_version,
                                      completed_at, uploaded_at, created_at)
        SELECT id, record_id, user_id, game_id, game_name, device_model_id,
               device_model_name, capability_tags, statistics, client_version,
               completed_at, uploaded_at, created_at
        FROM training_records_legacy
        ON CONFLICT (record_id, completed_at) DO NOTHING;
    END IF;
END $$;

-- ---------------------------------------------------------------------
-- 索引名是 schema 级命名空间，不会跟着表走：旧表 RENAME 成 training_records_legacy 之后，
-- 002 建的 idx_training_records_user / _game / _completed 仍然占用原名。
-- 此时若直接创建同名索引，PostgreSQL 的 IF NOT EXISTS 只按「名字」判断存在性
-- （不校验索引挂在哪张表上），会只发一个 NOTICE 就静默跳过，
-- 结果分区表上一个业务索引都没有、而归档表反倒占着名字。
--
-- 因此必须先让旧表的索引改名腾出名字，再在分区表上按原名创建：
--   1) 改名遍历「归档表实际拥有的索引」（走 pg_index/pg_class 取 OID 归属，
--      不依赖 to_regclass 的名字解析，避免 search_path 干扰）；
--   2) 建索引后做强制自检，任何一个索引没挂在 training_records 上就 RAISE EXCEPTION
--      让迁移整体失败回滚——绝不允许这类问题再以 NOTICE 的形式静默通过。
-- ---------------------------------------------------------------------
DO $$
DECLARE
    legacy_oid oid := to_regclass('training_records_legacy');
    rec        record;
BEGIN
    IF legacy_oid IS NULL THEN
        RETURN;
    END IF;
    FOR rec IN
        SELECT c.relname AS index_name
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        WHERE i.indrelid = legacy_oid
          AND c.relname IN ('idx_training_records_user',
                            'idx_training_records_game',
                            'idx_training_records_completed')
    LOOP
        BEGIN
            EXECUTE format('ALTER INDEX %I.%I RENAME TO %I',
                           current_schema(), rec.index_name,
                           replace(rec.index_name, 'idx_training_records_', 'idx_training_records_legacy_'));
        EXCEPTION WHEN duplicate_table THEN
            NULL; -- 目标名已被占用（重复执行或人工建过同名索引）：跳过
        END;
    END LOOP;
END $$;

-- 过渡版本（早期 003 草稿）曾在分区表上使用 idx_training_records_part_* 名字；
-- 只清理「确实挂在 training_records 上」的同名索引，使任何中间状态都收敛到规范名。
DO $$
DECLARE
    target oid := to_regclass('training_records');
    rec    record;
BEGIN
    IF target IS NULL THEN
        RETURN;
    END IF;
    FOR rec IN
        SELECT c.relname AS index_name
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        WHERE i.indrelid = target
          AND c.relname IN ('idx_training_records_part_user',
                            'idx_training_records_part_game',
                            'idx_training_records_part_completed',
                            'idx_training_records_part_record_id')
    LOOP
        EXECUTE format('DROP INDEX %I.%I', current_schema(), rec.index_name);
    END LOOP;
END $$;

-- 分区索引：非唯一索引不要求包含分区键（新建分区会自动带上同名子索引）。
CREATE INDEX IF NOT EXISTS idx_training_records_user ON training_records(user_id);
CREATE INDEX IF NOT EXISTS idx_training_records_game ON training_records(game_id);
CREATE INDEX IF NOT EXISTS idx_training_records_completed ON training_records(completed_at DESC);
CREATE INDEX IF NOT EXISTS idx_training_records_record_id ON training_records(record_id);

-- 强制自检：4 个业务索引必须实实在在挂在分区表 training_records 上。
-- 只按名字判存在性的 IF NOT EXISTS 会静默跳过，这里把「静默」变成「迁移失败」。
DO $$
DECLARE
    missing text;
BEGIN
    SELECT string_agg(name, ', ') INTO missing
    FROM unnest(ARRAY['idx_training_records_user',
                      'idx_training_records_game',
                      'idx_training_records_completed',
                      'idx_training_records_record_id']) AS name
    WHERE NOT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        WHERE i.indrelid = to_regclass('training_records')
          AND c.relname = name
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION '分区表 training_records 缺少索引: %（CREATE INDEX IF NOT EXISTS 可能被同名 relation 静默跳过）', missing;
    END IF;
END $$;

-- 复制过历史数据后，把序列推进到 max(id)，避免后续插入主键冲突
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM training_records) THEN
        PERFORM setval('training_records_part_id_seq', (SELECT max(id) FROM training_records));
    END IF;
END $$;
