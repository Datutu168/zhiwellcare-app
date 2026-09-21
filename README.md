# 智为康乐 ZhiWellCare · 消费级智能训练 APP（zhiwellcare-app）

> 纯消费级居家主动训练产品，对标 Keep 生态：自研智能硬件 IoT 互联、游戏化训练、课程体系、硬件商城 + 居家训练服务、个人训练数据。
> 本产品**无医疗属性、无康复服务、无被动电机驱动、无医学评估**；医疗器械设备独立承载于全新医疗 APP，与本工程完全隔离。

## 一、多端统一架构（一套代码跑通手机/平板/桌面端/网页）

```
┌─────────────────────────── 一套 Web 业务代码库 ───────────────────────────┐
│  TypeScript + Vue3 + Pinia + PixiJS 8 + Vite                              │
│                                                                            │
│  1. Web 业务层 src/views · src/components · src/stores                     │
│     页面 / 游戏 / 课程 / 商城 / 数据上报                                    │
│  2. 抽象适配层 src/core —— 统一系统能力接口（无平台感知）                    │
│     catalog（设备-游戏标签目录）/ sensor / motion / game / training / report│
│  3. 平台适配层 src/platform —— 运行时注入原生能力                            │
│     BLE / 屏幕常亮 / 返回键 / 更新 / 生命周期                                │
│  适配：视口归一化 + 媒体查询（CSS 令牌对齐官网品牌）                          │
└────────────────────────────────────────────────────────────────────────────┘
        │ Android/iOS/平板: Capacitor 8 │ 桌面端(Windows/macOS): Tauri 2 │ 网页: 静态 CDN
```

- 页面/业务/游戏/适配逻辑全部统一，无多端差异；原生差异（BLE、常亮、更新、返回键）只在 `src/platform` 注入。
- 工程由上一代「不倒翁居家功能训练」重构而来，已按消费健身口径替换全部面向用户文案。

## 二、5 大底部导航

| Tab | 路由 | 说明 |
|---|---|---|
| 设备 | `/devices` | IoT 中枢：BLE 扫描连接、多设备绑定/切换/重命名/解绑、固件检测（合规：型号+SN 白名单、消费版固件）、说明书/自检 |
| 训练 | `/games` | 游戏大厅：**设备能力标签 ⊇ 游戏所需标签** 动态匹配，连接设备自动过滤适配游戏 |
| 课程 | `/courses` | 居家上肢健身课程、图文跟练、打卡日历雏形 |
| 商城 | `/mall` | 硬件器材 + 上门健身指导服务（统一交易入口，购物车/支付为演示占位） |
| 我的 | `/mine` | 训练数据总览、订单聚合、设备、设置、免责声明与隐私 |

沉浸页（训练 `/training/:gameId`、结果 `/result`）隐藏底部导航，Android 训练横屏 + 常亮由平台层接管。

## 三、设备 ↔ 游戏 动态标签体系（核心差异化）

摒弃固定设备绑游戏：**新增设备/游戏无需发版**（目录由后端配置下发）。

- 设备能力标签：姿态传感 / 力矩传感（纯主动阻力）/ 六轴体感 / 握力传感（预留）；手柄配件为二级标签（方向盘/球形/T型/钥匙，预留）。
- 匹配公式：`设备能力标签 ⊇ 游戏所需标签`（`src/core/catalog/tagMatching.ts`）。
- 目录源抽象 `ICatalogSource`：本地 Mock（`src/data/catalog.ts`）↔ HTTP（Golang 后端 `/api/v1/device/{model}/games`，含 CDN 资源地址/版本/灰度），由 `VITE_CATALOG_MODE=mock|http` 切换，后端不可用自动回退本地。
- 首批数据：`wobble-wrist-band`（不倒翁手腕训练仪 · BS-BT91 BLE 协议 · 姿态传感）↔ `target-reach`（四方挥腕挑战）、`trajectory-follow`（8字轨迹跟随）；`desk-torque-base`（桌面力矩主动训练底座 · 多手柄）演示"标签不满足 → 引导连接对应设备"。

## 四、训练数据与上报

- 每条训练记录**强制携带**：游戏 ID、设备型号、能力标签快照、训练统计数据（`TrainingRecord.device`，`src/core/training/TrainingRecord.ts`）。
- 本地入库 IndexedDB（`zhiwellcare-training`）；摘要上报走 `src/core/reporting`：`VITE_REPORT_MODE=local`（localStorage 待发队列）或 `http`（POST `/api/v1/training/records`，Golang 后端 PG 训练摘要；高频采样文件后续经 S3）。
- 「我的」页可看到待上报数量与统计。

## 五、合规终身红线（强制）

1. 纯消费健身产品：无医疗器械功能、无康复医疗服务、无诊断/评估/分级/诊疗话术。
2. 全部设备仅支持**用户主动发力**对抗阻尼/阻力；无被动电机驱动控制。
3. 消费版设备固件永久禁止升级为医疗固件，服务端强制拦截（前端不可绕过，演示环境不做真实 OTA）。
4. 上门服务为**非医疗**居家健身指导，禁止与硬件捆绑医疗宣传；服务页/商城均强制免责声明。
5. 面向用户的界面文案已全量中性化（禁止词：康复/ROM/医疗/患者/疗效/评估/损伤等，见代码评审清单）。

## 六、开发与验证

```powershell
# Node.js 22+
npm install
npm run test          # vitest（含目录匹配 / 上报载荷等新用例）
npm run version:check # 版本基线一致性（release-version.json = 0.1.0）
npm run dev           # 浏览器预览（BLE 不可用，界面与游戏可预览）
npm run build         # vue-tsc + vite build（产物 dist/ 供三端壳复用）
```

平台壳（原生构建环境齐备后）：
```powershell
npm run tauri:dev            # 桌面窗口（Windows / macOS，BLE 走 Tauri Rust btleplug）
npm run android:build:debug  # Android APK（BLE 走 Capacitor bluetooth-le）
```
macOS 打包用 `./04-pack-macos.sh`（**必须在 Mac 上构建**，Tauri 无法交叉编译 macOS 目标），
详见 `打包与发布说明.md` 第五节。
环境变量参考 `.env.example`（目录/上报/签名密钥均不落库）。

### 各端实现现状（前端已就绪，原生工程按需生成）

| 端 | 工程 | BLE | 横屏/常亮 | 应用内更新 |
| --- | --- | --- | --- | --- |
| Web | 无需工程 | 不可用（浏览器限制，返回可读提示） | 无 | 无 |
| Windows | `src-tauri/`（已生成） | Tauri + Rust btleplug | 无 | NSIS 自更新 |
| macOS | `src-tauri/`（同一套代码） | Tauri + Rust btleplug | 无 | 未签名时不可用（需 Apple 账号） |
| Android | `android/`（已生成） | Capacitor bluetooth-le | 有 | APK 自更新 |
| iOS | **尚未生成 `ios/`** | 前端已接好 Capacitor bluetooth-le | 已接好 | 无（App Store 不允许自更新） |

iOS 说明：`src/platform` 已按 Capacitor 原生（Android / iOS）统一分支，iOS 上 BLE、横屏常亮、
前后台生命周期都会走到与 Android 相同的实现；返回键仍是 Android 专属（iOS 没有该事件）。
**生成 iOS 工程需要 Mac + Xcode 与 Apple 开发者账号**（未签名包在 iOS 上装不了，这点与 macOS 不同）：

```bash
npm i @capacitor/ios
npx cap add ios
npx cap sync ios
# 然后在 ios/App/App/Info.plist 里加 NSBluetoothAlwaysUsageDescription，
# 否则一用蓝牙就会崩溃（插件官方文档明确写了这一条）。
```

蓝牙在 **iOS 模拟器上不可用**（插件会直接报 "BLE unsupported"），必须真机调试。

## 七、目录速览

```
src/
├─ app/         路由、全局服务装配（AppServices：连接/目录/上报/更新单例）
├─ core/        抽象适配层（目录 catalog / 传感器 / 运动 / 游戏契约 / 训练 / 上报 / 更新）
├─ platform/    Capacitor·Tauri·Web 三端适配注入（BLE/常亮/返回/更新/生命周期）
├─ games/       PixiJS 8 游戏（四方挥腕挑战、8字轨迹跟随）与注册表
├─ views/       页面（device/games/courses/mall/mine + 功能页）
├─ components/  AppTabBar、设备、历史、更新、校准等组件
├─ data/        Mock 目录/课程/商品数据（等价后端配置表快照）
└─ stores/      Pinia（sensor / deviceLibrary 多设备库）
android/  Capacitor Android 壳（包名 com.zhiwellcare.app）
src-tauri/ Tauri 2 桌面壳（Windows / macOS，identifier com.zhiwellcare.app）
```

## 八、后端与基础设施（Golang + Gin）

技术栈：Golang + Gin · PostgreSQL 16 · Redis（映射/白名单/会话缓存 + 分布式限流）· S3 兼容对象存储（Web 包/游戏资源/固件/采样文件）· CDN · Vue3 + Element Plus 管理后台。
目录与上报的 HTTP 契约固化在前端 `src/core/catalog/HttpCatalogSource.ts`、`src/core/reporting/HttpTrainingReportTransport.ts`（已对齐后端实现）。

本节所列能力均已在**真实进程 + 真实 PostgreSQL** 上端到端验证（隔离 schema 跑迁移与接口，跑完即清理）：

| 能力 | 实现位置 | 说明 |
|---|---|---|
| Redis 缓存 | `backend/internal/cache` | 设备映射读缓存、白名单读缓存、访问令牌吊销名单；**未配置 `APP_REDIS_URL` 或连不上时自动回退进程内实现**，单实例部署零额外依赖 |
| 分布式限流 | `httpapi.RateLimit(cache.Store, …)` | Redis 部署下多实例共享额度；缓存故障退化为进程内计数，不会限流失效 |
| 会话吊销 | `auth.Claims.jti` + `cache.Sessions` | 登出后访问令牌立即失效（按剩余有效期自动过期），不必等自然过期 |
| 对象存储 | `backend/internal/objectstore` | S3 兼容（MinIO/OSS/COS/AWS，桶不存在自动创建）预签名直传；**未配置 S3 时回退本地磁盘 + HMAC 签名端点**，客户端流程完全一致 |
| 采样文件直传 | `POST /api/v1/training/records/:recordId/samples/upload-url` | 归属校验（本人或管理员）→ 预签名地址 → 客户端直传对象存储 |
| RBAC | `roles/permissions/role_permissions/user_roles` + `rbac.Resolver` | 15 个权限点、3 个内置角色（admin/operator/viewer）；`RequirePermission` 逐路由鉴权，**解析失败按拒绝处理**，权限变更走写路径失效缓存（改角色后无需重新登录即生效） |
| JSONB 配置中心 | `app_config` + 目录 `config` 列 | 任何 JSON 值可读写（目录版本、灰度开关、功能开关已种子化） |
| 训练摘要按月分区 | `migrations/003_rbac_config.sql` | RANGE(completed_at) 月分区 + DEFAULT 兜底 + `ensure_partition()` 按需建分区；`UNIQUE(record_id)` 改为复合主键 `(record_id, completed_at)` 以兼容分区表，重复上报仍幂等；旧表只改名归档、不删数据 |
| 资产发布与 CDN 下发 | `assets` 表 + `/api/v1/admin/assets*` | Web 包/游戏资源/固件统一登记与上下架；已发布资产经 CDN 前缀（未配则预签名）下发 |
| 固件 / Web 包更新 | `/api/v1/device/:modelId/firmware`、`/api/v1/app/web-bundle` | 公开接口按 `currentVersion` 返回「有更新 / 已是最新」，草稿资产不下发 |
| 前端消费 CDN | `src/core/resources/` | `resourceUrl` 真正生效（清单校验 + 成功 6h/失败 5min 双 TTL 缓存），兼容「目录前缀」与「文件 URL」两种形状；**清单缺失或不可用时静默回退内置资源** |
| 一键编排 | `docker-compose.yml` + `deploy/nginx.conf` | PostgreSQL 16 + Redis 7 + MinIO + 后端 + Nginx 静态下发（`/api` 反代、`/assets` 强缓存、SPA 回退、`client_max_body_size 64m`） |

运维要点：
- **启动日志会打印数据库主机与库名**（不含密码），请务必核对，避免误连（曾发生过 `$env:APP_DB_URL=''` 被 PowerShell 删除变量、`godotenv` 又回填 `.env` 里正式库连接串的事故）。
- **同源部署要显式把 `VITE_API_BASE` 设为空串**：`src/api/http.ts` 用 `import.meta.env.VITE_API_BASE ?? 'http://127.0.0.1:8080'`，未设置时会退到本机 8080；构建时写 `VITE_API_BASE=`（空串，非「不设置」）才会走同源 `/api`，从而复用 `deploy/nginx.conf` 的反代。
- 本地对象存储模式下，采样文件直传会经过 Nginx 反代，已放宽到 64MB；S3 部署则由客户端直连对象存储。
- `docker compose up -d` 全套启动；仅起中间件用 `docker compose up -d postgres redis minio`。`web` 服务需要先在本机 `npm run build` 生成 `dist/`。
- 管理后台自带冒烟回归：`cd admin && npm run smoke`（admin 侧自包含 vitest + jsdom，**不影响根 `npm test`**；根套件只收集 `src/` 与 `tests/` 的用例）。

## 九、注册 / 登录（已落地，见 backend/）

**后端（Golang + Gin，PostgreSQL 真库）**：`backend/cmd/server` 提供服务，接口如下：

| 接口 | 说明 |
|---|---|
| `POST /api/v1/auth/register` | 手机号注册（CN 手机号 + 密码 ≥6 位 + 昵称选填），成功即返回令牌 |
| `POST /api/v1/auth/login` | 登录（账号不存在与密码错误返回同一文案，防账号枚举） |
| `POST /api/v1/auth/refresh` | 刷新令牌轮换（旧令牌立即失效） |
| `POST /api/v1/auth/logout` | 退出（撤销刷新令牌，幂等） |
| `GET/PATCH /api/v1/me` | 当前用户资料 / 修改昵称（需 Bearer 访问令牌） |
| `GET /healthz` | 健康检查（DB 状态） |

- 密码 bcrypt；访问令牌 JWT(HS256, 2h)；刷新令牌 32 字节随机值，库中仅存 SHA-256 哈希。
- 表结构 `backend/migrations/001_init.sql`（幂等，服务启动自动执行）；存储层接口化，`APP_DB_URL` 未配置时自动回退内存演示模式。
- 统一响应 `{code, message, data}`；CORS 白名单支持 Vite 网页 / Capacitor(Android/iOS) / Tauri(桌面端 Windows/macOS) 跨端来源。
- 运行：`cd backend` → 复制 `.env.example` 为 `.env` 填 `APP_DB_URL` → `go run ./cmd/server`。
- 测试：`go test ./...`（含内存全链路用例）；配 `APP_TEST_DB_URL` 可跑 PostgreSQL 真库集成用例。

**前端打通**：`src/api/`（http 客户端 + 鉴权 API + 令牌持久化）、`src/stores/auth.ts`（登录态 store）、`/auth/login` 与 `/auth/register` 沉浸页、「我的」页登录态卡片与退出。游客可继续本地体验全部训练功能；训练记录在未登录时走本地上报队列，登录后可切换为云端直传。

## 十、管理后台与运营目录（backend/admin）

- **后端运营接口**（RBAC：`role=admin`，`backend/internal/httpapi`）：
  设备型号 CRUD/上下架、游戏目录 CRUD/上下架/灰度（`grayRatio`）、用户列表/禁用、训练摘要列表、
  仪表盘统计；面向 APP 的公开目录 `GET /catalog/devices`、`GET /catalog/games`、`GET /device/:modelId/games`（按能力标签过滤，已实机验证：不倒翁→2 款游戏，力矩底座→空）。
- **管理后台前端**：`admin/`（Vue3 + Element Plus + Vite），登录后管理上述目录；本机管理员账号
  `13800008888 / admin123456`（也兼容 `13900002222 / demo123456`；启动时由 `APP_ADMIN_BOOTSTRAP_PHONE` 自动提升角色）。
- 接口契约：`backend/API_CONTRACT.md`。

## 十一、微信登录 / 部署 / 文档

- 微信 UnionID 登录扩展已实现（`oauth_identities` 表 + `/auth/wechat/login` + 手机号补绑）；
  未配置凭据自动走 Mock；上线激活步骤见 `backend/WECHAT_ACTIVATION.md`。
- 生产部署：`backend/DEPLOYMENT.md` + `backend/Dockerfile`（多阶段构建，迁移随镜像）；
  域名策略：正式 `game.zhiwellcare.com`，测试复用 CDN `/game-app`。
- 迁移：`backend/migrations/*.sql`（001 账号、002 目录/训练摘要/OAuth）。


