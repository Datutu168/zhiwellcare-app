# 生产部署与运维（后端）

## 一、镜像构建

```bash
cd backend
docker build -t zhiwellcare-backend:0.1.0 .
```

## 二、环境变量注入（生产）

| 变量 | 说明 |
|---|---|
| `APP_DB_URL` | PostgreSQL 连接串（**必填**；建议独立用户最小权限 + sslmode=require） |
| `APP_JWT_SECRET` | 32+ 字节随机串，启动前用 `openssl rand -hex 32` 生成并密钥管理 |
| `APP_CORS_ORIGINS` | 正式域名 `https://game.zhiwellcare.com` 及内测来源 |
| `APP_ADMIN_BOOTSTRAP_PHONE` | 首次部署前先用手机号注册，再注入该值重启完成管理员引导后移除 |
| `APP_WECHAT_APPID/SECRET` | 见 `WECHAT_ACTIVATION.md` |

## 三、域名与反向代理（复用产品域名策略）

- 正式：`game.zhiwellcare.com`（子域名，静态 Web 产物与 `/api` 同域，天然规避跨域）；
- 测试：现有 CDN 域名 `/game-app` 路径部署，后端 CORS 需放行该来源；
- Nginx 片段：`location /api/ { proxy_pass http://127.0.0.1:8080; }`，Web 产物由静态/CDN 层承载；
- 对外必须 HTTPS（TLS 证书 + HSTS）；`SecurityHeaders` 中间件已输出基础安全响应头。

## 四、数据与备份

- 迁移：`backend/migrations/*.sql` 启动时幂等执行（按文件名排序）；生产库变更走同样追加迁移。
- 备份：PG `pg_dump` 定时 + PITR。
- `training_records` **已按月 RANGE 分区**（`003_rbac_config.sql`）：DEFAULT 分区兜底 + `training_records_ensure_partition()` 在写入前按需建分区；迁移会预建「历史最早月份 ~ 未来 5 个月」的分区。
  - 分区表的唯一索引必须包含分区键，因此原 `UNIQUE(record_id)` 已改为**复合主键 `(record_id, completed_at)`**，写入用 `ON CONFLICT (record_id, completed_at) DO NOTHING` 保持「重复上报只落一行」的幂等语义。
  - 迁移**不删数据**：旧普通表仅改名为 `training_records_legacy` 留存（确认无误后可人工 DROP）。
  - 迁移内置**索引归属自检**：分区表上四个索引（user/game/completed/record_id）缺任一个即 `RAISE EXCEPTION` 并整体回滚 —— 避免 `CREATE INDEX IF NOT EXISTS` 因同名 relation 被静默跳过。

## 五、Redis（映射 / 白名单 / 会话缓存 / 分布式限流）

已实现，`APP_REDIS_URL` 未配置或连不上时**自动回退进程内实现**（单实例功能一致，多实例必须接 Redis）：

| 用途 | 键前缀 | 说明 |
|---|---|---|
| 设备映射读缓存 | `zwkl:device:mapping:*` | 蓝牙标识 → 型号，TTL 由 `APP_DEVICE_CACHE_TTL_SECONDS` 控制 |
| 设备白名单读缓存 | `zwkl:device:whitelist` | 白名单为空表示不限制；运营增删后写路径主动失效 |
| 用户权限集合缓存 | `zwkl:rbac:user:*` | 「用户 → 角色 → 权限码」解析结果，TTL 同 `APP_DEVICE_CACHE_TTL_SECONDS`（默认 5 分钟）；分配用户角色、改角色权限、删角色都会立即失效（改权限无需重新登录） |
| 访问令牌吊销名单 | `zwkl:session:revoked:*` | 登出后访问令牌立即失效（按剩余有效期自动过期），只存令牌 SHA-256 指纹 |
| 限流计数 | `zwkl:ratelimit:*` | 固定窗口计数（Lua 保证原子），多实例共享额度；缓存故障退化为进程内计数 |

建议开启 AOF 持久化；`maxmemory-policy` 用 `allkeys-lru` 即可（缓存丢只是回源）。

## 六、对象存储（S3 兼容）与 CDN

- 配置 `APP_S3_ENDPOINT/REGION/ACCESS_KEY/SECRET_KEY/BUCKET`（MinIO / 阿里云 OSS / 腾讯云 COS / AWS S3 均可）；**桶不存在时后端自动创建**。未配置 S3 时回退本地磁盘（`APP_S3_LOCAL_DIR`）。
- 上传下载一律走**预签名 URL**，客户端直连对象存储，不经过业务服务器：
  - S3 模式：地址形如 `https://<endpoint>/<bucket>/<key>?X-Amz-Signature=...`
  - 本地磁盘模式：地址指向本服务 `PUT/GET /api/v1/files/{key}?op=&exp=&sig=`（HMAC-SHA256，密钥取 `APP_UPLOAD_SIGN_SECRET`，留空则取 `APP_JWT_SECRET`），因此该模式必须配置 `APP_PUBLIC_BASE_URL`。
- 对象键分区：`web/`（Web 包）、`games/{gameId}/{version}/`、`firmware/{modelId}/{version}/`、`samples/{userId}/{recordId}/`。
- `APP_CDN_BASE` 配置后，已发布资产下发 CDN 地址；未配置则回退预签名地址。**前端同样支持 CDN 前缀兜底**（`VITE_GAME_RESOURCE_CDN`，需包含 `games` 段）。
- 反向代理注意：本地磁盘模式下采样直传会经过 Nginx，`deploy/nginx.conf` 已放宽到 `client_max_body_size 64m`。

## 七、一键编排（本地/单机）

```bash
docker compose up -d --build            # PG16 + Redis7 + MinIO + 后端 + Nginx 静态下发
docker compose up -d postgres redis minio   # 只起中间件，本机 go run 调后端
```

- `web` 服务挂载 `./dist`，**需先在本机 `npm run build`**；`deploy/nginx.conf` 已含 `/api` 反代、`/assets` 强缓存、SPA 回退与 64MB 直传上限。
- 前端同源部署时构建要显式设置 `VITE_API_BASE=`（空串，而非「不设置」），否则会退到本机 `http://127.0.0.1:8080`。

### 7.1 单机（Lighthouse）多域名 vhost

单机生产用三个独立 vhost，完整配置与安装脚本见 `deploy/nginx-sites/`（含 README）：

```bash
sudo bash deploy/nginx-sites/install.sh          # 幂等：备份 → nginx -t → reload → 自检
```

| 站点 | server_name | 静态目录 | `/api/` 反代 |
| --- | --- | --- | --- |
| 官网 | `zhiwellcare.com` / `www` / `.cn` | `/srv/sites/web/current` | 否（纯静态） |
| App 网页版 | `game.zhiwellcare.com` | `/srv/sites/app/current` | 是 |
| 运营后台 | `admin.zhiwellcare.com` | `/srv/sites/admin/current` | **必须有** |

**后台 vhost 缺 `/api/` 反代会表现为"登录进不去"而不是"配置错误"**：`admin/` 前端用相对路径
`fetch(`/api/v1${path}`)`，没有反代时 `POST` 被 nginx 静态层拒成 **405**，`GET` 命中 SPA 回退返回
**200 + index.html**（前端解析 JSON 才失败）。排查时先确认 `admin.zhiwellcare.com` 的 `/api/v1/...`
响应的 `Content-Type` 是 `application/json` 而不是 `text/html`。

官网不反代 `/api/`，以官网域名为 Host 请求 `/api/v1/...` 返回 404 属预期。

## 八、健康与监控

- `GET /healthz` 探活，并回报中间件后端类型：`{"status":"ok","cache":"redis|memory","storage":"s3|local"}`，可据此确认 Redis/S3 是否真正生效（未生效即为已回退）。
- 访问日志为 slog JSON（stdout），可接入云日志服务。
- **启动日志打印数据库主机与库名（不含密码）**，非本机库会额外打 WARN —— 部署时请核对，避免误连。
  （注意：PowerShell 中 `$env:APP_DB_URL=''` 会**删除**该变量，`godotenv` 随后可能回填 `.env` 里的连接串。）
- 限流：`/auth/*` 已接 Redis 计数；生产仍建议前置网关级限流（按账号+IP）。

### 8.1 一次后端发布的标准动作

```bash
# 开发机：交叉编译
cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' -o zhiwellcare-server ./cmd/server

# 上传二进制与迁移目录，然后在服务器执行（迁移目录可省略，但强烈建议一起传）
scp zhiwellcare-server ubuntu@<host>:/tmp/
scp -r migrations ubuntu@<host>:/tmp/migrations
ssh ubuntu@<host> 'sudo bash /srv/app/backend-release.sh /tmp/zhiwellcare-server /tmp/migrations'
```

`deploy/backend-release.sh` 的每一步都必须过，任一失败会**连迁移一起回滚**：

1. 二进制必须是 ELF；迁移文件不得为空、不得带 UTF-8 BOM；
2. 同步 `/srv/app/migrations`（旧目录备份为 `migrations.prev`）；
3. 重启 → `systemctl is-active` → `GET /healthz`；
4. **探测依赖 schema 的公开接口** `/api/v1/catalog/courses`、`/api/v1/catalog/goods` 必须 200 —— 这一步专门抓「新二进制上了、迁移没传」这类"服务是活的、接口 500"的事故；
5. 二进制自身在启动时校验 `store.RequiredMigrations`（见 `internal/store/postgres.go`），迁移文件缺失直接拒绝启动，不依赖人的自觉。

发布后跑一遍上线检查（见 §8.2）。

### 8.2 上线安全检查

```bash
sudo bash /srv/app/security-checklist.sh              # 只检查
sudo bash /srv/app/security-checklist.sh --prune-secret   # 确认初始密码已失效时删掉明文文件
```

检查项：明文初始密码文件是否仍可登录、`.env` 与密钥备份权限是否 600、JWT 密钥是否为默认值/过短、
数据库是否指向本机、`healthz` 的 `cache`/`storage` 是否真生效、启动日志是否清理过权限缓存、
依赖 schema 的公开接口是否 200、备份定时任务与最近备份时效。存在 FAIL 时退出码为 1。

**初始密码处置流程**：首次部署会生成 `/srv/app/admin-initial-password.txt`（600）。登录后台后用
`PUT /api/v1/me/password`（界面入口：右上角用户菜单 → 修改密码）改掉初始密码，再用
`security-checklist.sh --prune-secret` 删除该文件 —— 脚本用"文件里的密码还能不能登录"判定是否已改密，
能登录就 FAIL，不会让明文凭据长期留在服务器上。

## 九、部署后自检清单（按此逐条确认，不要只看服务「起来了」）

1. `GET /healthz` 的 `cache` 必须是 `redis`、`storage` 必须是 `s3`；显示 `memory`/`local` 说明配置没生效、已静默回退。
2. 启动日志核对 `数据库已连接 host=… database=…` 与预期一致（非本机库会同时有 WARN）。
3. **S3 真实链路**：`POST /api/v1/admin/assets/upload-url` 拿到的 `uploadUrl` 必须落在对象存储域名上并带 `X-Amz-Signature`（而不是指向本服务的 `/api/v1/files/*`）；用它 `PUT` 直传后再 `GET` 回来，内容应完全一致。
4. **公开下发**：把固件/Web 包资产 `PATCH …/status` 置 `published` 后，`GET /api/v1/device/:modelId/firmware?currentVersion=<旧版本>` 必须返回 `{upToDate:false,url,…}`，且 `url` 为 S3 预签名地址（配置了 `APP_CDN_BASE` 时则为 CDN 地址）；`currentVersion` 等于已发布版本时返回 `{upToDate:true,version}`。
5. **权限即时生效**：给用户 `PUT /admin/users/:id/roles` 后用**同一个 token** 重试原接口，应立即放行/拒绝（无需重新登录）；服务重启后同样立即按最新角色判定。
6. **迁移自检**：迁移在分区表索引缺失时会 `RAISE EXCEPTION` 并整体回滚——若启动日志出现该错误，说明存在占名的同名 relation，先处理再重启（不要靠删索引绕过）。
7. `training_records` 写入不需要手工建分区：`training_records_ensure_partition()` 在插入前按需创建，DEFAULT 分区兜底；可用 `SELECT tableoid::regclass FROM training_records WHERE record_id='…'` 确认数据落在当月分区。
8. **后台同源可用性**：以 `admin.zhiwellcare.com` 为 Host 请求 `POST /api/v1/auth/login` 必须返回 200 且 `Content-Type: application/json`（返回 405 或 HTML 说明该 vhost 漏了 `/api/` 反代，见 §7.1）。
9. **权限缓存清理**：改动角色/权限的迁移或 `APP_ADMIN_BOOTSTRAP_PHONE` 触发管理员提升时，启动日志应出现 `权限缓存已清理 prefix=zwkl:rbac`，且随后用同一账号访问 `/api/v1/admin/*` 应为 200（不是 403）。
10. **迁移齐全性**：新二进制启动时会校验 `migrations/` 是否含全部必需文件，缺文件直接退出（日志：`migrations 目录缺少必需文件`），此时是**漏传 SQL**，把迁移一起补上再发布，不要靠反复重启绕过。
11. **改密接口可用**：`PUT /api/v1/me/password`（自带 token，无需 RBAC 权限点）用旧密码换新密码后，旧密码登录必须 401、新密码登录必须 200；随后按 §8.2 删掉明文初始密码文件。
12. **上线安全检查无 FAIL**：`sudo bash /srv/app/security-checklist.sh` 退出码为 0（WARN 可接受，例如对象存储仍是 local）。
