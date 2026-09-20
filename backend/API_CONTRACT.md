# 智为康乐管理后台 API 契约（v1）

- 基址：`http://127.0.0.1:8080/api/v1`（正式：`https://game.zhiwellcare.com/api/v1`）
- 鉴权：除登录与公开目录/下发接口外全部 `Authorization: Bearer <accessToken>`
- 后台接口**不再要求固定 `role=admin`**，改为按权限点鉴权（`RequirePermission`）：每个请求都解析「用户 → 角色 → 权限码」集合（结果按 `APP_DEVICE_CACHE_TTL_SECONDS` 缓存，默认 5 分钟；分配用户角色、改角色权限、删角色会立即失效缓存，进程重启后缓存清空、下一次请求即回源），因此**改权限无需重新登录**，旧 token 在角色变更/服务重启后立刻按新权限判定；解析失败按拒绝处理（403）
- 响应统一：`{"code":0,"message":"ok","data":…}`，`code!=0` 视为失败（与 HTTP 状态一致）
- 登录：`POST /auth/login` body `{"phone","password"}` → `data:{user:{id,phoneMasked,nickname,role}, tokens:{accessToken,refreshToken}}`
- 本机管理账号：`13800008888 / admin123456`（兼容 `13900002222 / demo123456`，角色 admin）

## 后台运营接口（需登录 + 下表权限点；`admin` 角色拥有全部权限，行为与旧版一致）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/stats/dashboard` | 仪表盘计数 `{deviceModels,games,users,trainingRecords,todayRecords}` |
| GET | `/admin/devices` | 设备型号列表（含 off） |
| POST | `/admin/devices` | 新建/更新型号（upsert） |
| PUT | `/admin/devices/:modelId` | 更新型号 |
| DELETE | `/admin/devices/:modelId` | 删除型号 |
| PATCH | `/admin/devices/:modelId/status` | body `{"status":"on"|"off"}` |
| GET | `/admin/games` | 游戏目录列表（含 off） |
| POST | `/admin/games` | 新建/更新游戏（upsert） |
| PUT | `/admin/games/:gameId` | 更新游戏 |
| DELETE | `/admin/games/:gameId` | 删除游戏 |
| PATCH | `/admin/games/:gameId/status` | body `{"status":"on"|"off"}` |
| GET | `/admin/courses` | 课程列表（含 off）`data:{items:[…]}` |
| POST | `/admin/courses` | 新建/更新课程（upsert）→ `data:{item}` |
| PUT | `/admin/courses/:courseId` | 更新课程（以路径 ID 为准）→ `data:{item}` |
| DELETE | `/admin/courses/:courseId` | 删除课程 → `data:{deleted:true}` |
| PATCH | `/admin/courses/:courseId/status` | body `{"status":"on"\|"off"}` → `data:{status}` |
| GET | `/admin/goods` | 商城商品列表（含 off）`data:{items:[…]}` |
| POST | `/admin/goods` | 新建/更新商品（upsert）→ `data:{item}` |
| PUT | `/admin/goods/:goodsId` | 更新商品（以路径 ID 为准）→ `data:{item}` |
| DELETE | `/admin/goods/:goodsId` | 删除商品 → `data:{deleted:true}` |
| PATCH | `/admin/goods/:goodsId/status` | body `{"status":"on"\|"off"}` → `data:{status}` |
| GET | `/admin/users?keyword=&status=&page=&pageSize=` | 用户列表（status: -1 全部/0 禁用/1 正常）`data:{items,total}` |
| PATCH | `/admin/users/:id/status` | body `{"status":0\|1}`（不可停用自己） |
| GET | `/admin/training-records?page=&pageSize=&gameId=&modelId=` | 训练摘要 `data:{items,total}` |

### 设备型号字段（DeviceModel）

```json
{
  "modelId": "wobble-wrist-band",
  "name": "不倒翁手腕训练仪",
  "manufacturer": "智为康乐",
  "category": "wrist",
  "description": "…（消费版主动训练口径，禁止医疗表述）",
  "capabilityTags": ["posture-sensor"],
  "supportedHandleTags": [],
  "namePatterns": ["bt91","wobble"],
  "protocol": "bs-bt91",
  "minAppVersion": "0.1.0",
  "firmwareUpdatable": true,
  "icon": "⌚",
  "status": "on"
}
```

### 游戏字段（GameCatalogItem）

```json
{
  "gameId": "target-reach",
  "name": "四方挥腕挑战",
  "summary": "…",
  "requiredTags": ["posture-sensor"],
  "durationPresetsMin": [1,3,5],
  "resourceVersion": "1.0.0",
  "resourceUrl": "",
  "status": "on",
  "grayBatch": null,
  "grayRatio": 0,
  "categoryLabel": "腕部协调",
  "playMode": "active-force"
}
```

标签常量：设备能力 `posture-sensor / torque-sensor / six-axis-imu / grip-sensor`；手柄二级 `handle-steering-wheel / handle-sphere / handle-t / handle-key`。
匹配公式：**设备能力标签 ⊇ 游戏所需标签**（公开接口 `GET /device/:modelId/games` 已实现过滤）。

## 训练记录（APP 侧）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/training/records` | 鉴权上报 `{recordId,gameId,gameName,completedAt(ms),device:{modelId,modelName,capabilityTags},statistics,clientVersion}`；同一 `recordId`+`completedAt` 重复上报幂等（分区表复合主键 `ON CONFLICT DO NOTHING`） |
| GET | `/me/training-records` | 个人云端记录 |
| POST | `/training/records/:recordId/samples/upload-url` | body `{filename,contentType}` → `data:{key,uploadUrl,method:"PUT",expiresInSeconds,publicUrl?}`（**归属校验**：仅本人或管理员） |
| GET | `/training/records/:recordId/samples/:filename/url` | → `data:{key,downloadUrl,method:"GET",expiresInSeconds}`；对象不存在时 404 |

采样文件直传流程：申请 `uploadUrl` → 客户端 `PUT <uploadUrl>` 传文件（S3 模式直连对象存储；本地磁盘模式指向 `/api/v1/files/*`）→ 需要时可再申请 `downloadUrl`。

## 账号自助（需登录，无额外权限点）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| PUT | `/me/password` | 登录即可 | 修改**本人**密码（任意登录用户，无需权限点）→ `data:{updated:true}` |

请求（两个字段均为 JSON 字符串）：

```json
{ "oldPassword": "old123456", "newPassword": "new123456" }
```

成功响应（HTTP 200，统一信封）：

```json
{ "code": 0, "message": "ok", "data": { "updated": true } }
```

规则与错误码：

- `newPassword` 沿用注册口令规则（**6–64 位**）；`oldPassword` 只用于校验，两个字段都**不会**被日志记录或回显；
- 未登录 / 令牌无效 → **401**（`RequireAuth` 中间件，`登录状态已失效，请重新登录`）；
- body 非合法 JSON，或缺少 `oldPassword`/`newPassword` → **400** `请求参数格式不正确`；
- `newPassword` 长度不合法 → **400** `密码至少 6 位` / **400** `密码最长 64 位`；
- `newPassword` 与原密码相同 → **400** `新密码不能与原密码相同`；
- `oldPassword` 校验失败 → **401** `原密码不正确`；
- 当前用户在库中已不存在：查用户阶段 `ErrUserNotFound` → **401** `手机号或密码不正确`；更新阶段受影响行数为 0（`ErrNotFound`）→ **404** `资源不存在`；其它存储层错误 → **500** `服务开小差了，请稍后重试`（统一走 `mapStoreError`）。

**已知限制**：访问令牌是**无状态 JWT，claims 里没有「密码版本」**，因此改密成功后，此前签发的 accessToken 在其 TTL 到期前**仍然有效**（改密不会立刻把其它设备踢下线）；要让旧令牌立即失效，只能由客户端调用 `POST /auth/logout` 或等待令牌自然过期。

## 面向 APP 的公开目录（已接入 CDN 资源下发）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/catalog/devices` | 上架设备型号 |
| GET | `/catalog/games` | 上架游戏；条目含 `resourceUrl`、`resourceSha256`、`resourceSize` |
| GET | `/catalog/courses` | 上架课程（`status='on'`，按 `sort` 升序）`data:[…]` |
| GET | `/catalog/goods` | 上架商城商品（`status='on'`，按 `sort` 升序）`data:[…]` |
| GET | `/device/:modelId/games` | 该型号可用游戏（标签匹配结果） |
| GET | `/app/web-bundle?platform=android\|windows\|web&currentVersion=0.1.0` | Web 包更新：有更新 `{upToDate:false,version,url,sha256,size,notes,publishedAt}`；无更新 `{upToDate:true,version}` |
| GET | `/device/:modelId/firmware?currentVersion=1.0.0` | 固件更新，结构同上 |

`resourceUrl` 回填规则：目录里 `resource_url` 为空、且存在该 `gameId`+`resourceVersion` 的 **published** 资产时，用 CDN 前缀（未配置 `APP_CDN_BASE` 则预签名地址）填充；**draft 资产不下发**。
前端消费约定：`resourceUrl` 可能是「版本目录前缀」（以 `/` 结尾）或「该版本 `manifest.json` 的文件地址」两种形状，`src/core/resources/GameResourceResolver.ts` 均支持；清单缺失/不可用一律**静默回退内置资源**。

## 内容中心（课程 / 商城商品）

后台维护、APP 只读已上架项；两张表见 `migrations/004_content.sql`（`courses` / `mall_goods`）。
`course_id` / `goods_id` 与设备/游戏同规则：`^[a-z0-9][a-z0-9-]{1,63}$`；`status ∈ on|off`（新建缺省 `off`）。
公开接口与后台列表均按 **`sort` 升序**（同值按 ID 升序），公开接口只返回 `status='on'`；非法 ID / 非法 status / 缺必填字段一律 **400**，改状态或删除不存在的 ID → **404**。

```json
// Course
{
  "courseId": "balance-basic",
  "title": "坐姿平衡基础",
  "summary": "…",
  "coverUrl": "https://cdn.example.com/covers/balance-basic.png",
  "videoUrl": "https://cdn.example.com/videos/balance-basic.mp4",
  "durationLabel": "8 分钟",
  "level": "入门",
  "tags": ["balance","wrist"],
  "status": "on",
  "sort": 10
}
```

```json
// MallGoods（保留价格字段：priceCents 供计算，priceLabel 供展示）
{
  "goodsId": "training-band",
  "name": "训练腕带",
  "summary": "…",
  "priceCents": 19900,
  "priceLabel": "¥199",
  "coverUrl": "https://cdn.example.com/goods/training-band.png",
  "detailUrl": "https://shop.example.com/goods/training-band",
  "specs": ["S","M","L"],
  "status": "on",
  "sort": 20
}
```

## RBAC：权限点与角色

权限点共 17 个（`code / 名称 / 分组`）：

`device:read` `device:write`（device）· `game:read` `game:write`（game）· `record:read`（record）· `user:read` `user:write`（user）· `role:read` `role:write`（role）· `config:read` `config:write`（config）· `asset:read` `asset:write`（asset）· `whitelist:read` `whitelist:write`（whitelist）· `content:read` `content:write`（content，004 迁移新增）

内置角色（`builtin=true`，不可删除）：`admin`（17 项全部）· `operator`（13 项：设备/游戏/白名单/资产/内容读写 + 记录与用户只读 + 配置只读）· `viewer`（9 项：只读，含 `content:read`）。
`admin` 的「全部权限」在 003 里是全量 `INSERT … SELECT`，而 003 先于 004 执行，故 004 里**显式补授**了 `content:read` / `content:write`（已实测：重复执行迁移不会重复插入）。
历史数据兼容：迁移时把 `users.role='admin'` 的用户自动补入 `user_roles`。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/admin/permissions` | `role:read` | 权限点字典 `data:{items:[{code,name,group,description}]}` |
| GET | `/admin/roles` | `role:read` | `data:{items:[{code,name,description,builtin,permissions,userCount}]}` |
| POST | `/admin/roles` | `role:write` | body `{code,name,description,permissions[]}` → `data:{role}`（**返回 200**，同仓库惯例非 201） |
| PUT | `/admin/roles/:code` | `role:write` | body `{name,description,permissions[]}` |
| DELETE | `/admin/roles/:code` | `role:write` | builtin 或仍有用户的角色 → **409** |
| GET | `/admin/users/:id/roles` | `user:read` | `data:{roles,permissions}` |
| PUT | `/admin/users/:id/roles` | `user:write` | body `{roles[]}`；同步维护 `users.role`（含 admin → 'admin'） |
| GET | `/me/permissions` | 登录即可 | `data:{roles,permissions}`（后台前端控制菜单/按钮用）；同时注册 `/admin/me/permissions` 作为别名 |

## 配置中心（JSONB）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/admin/config` | `config:read` | `data:{items:[{key,value,description,updatedAt}]}`（`value` 为任意 JSON） |
| GET | `/admin/config/:key` | `config:read` | 单项 |
| PUT | `/admin/config/:key` | `config:write` | body `{value,description?}` |

已种子化：`catalog.version`、`catalog.gray.enabled`、`feature.firmware_ota`、`feature.samples_upload`。

## 设备白名单与映射（PostgreSQL 为准，Redis 仅做读缓存）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/admin/device-whitelist` | `whitelist:read` | `data:{items:[{deviceKey,modelId,note,enabled}]}`（白名单为空 = 不限制） |
| POST | `/admin/device-whitelist` | `whitelist:write` | body `{deviceKey,modelId,note,enabled?}`；**`enabled` 为可选扩展**（冻结契约只列前三项，缺省视为启用），返回该条目 |
| DELETE | `/admin/device-whitelist/:deviceKey` | `whitelist:write` | `data:{deleted:true}` |
| GET | `/admin/device-mappings` | `device:read` | `data:{items:[{deviceKey,modelId,note}]}` |
| POST | `/admin/device-mappings` | `device:write` | body `{deviceKey,modelId,note}`（upsert），返回该条目 |
| DELETE | `/admin/device-mappings/:deviceKey` | `device:write` |  |

增删后**写路径会立即失效对应读缓存**，因此随后的读取即为最新值（已实测）。`deviceKey` 落库统一小写去空白。

## 资产发布（Web 包 / 游戏资源 / 固件）

`kind ∈ web|game|firmware`；`status ∈ draft|published|offline`。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `/admin/assets/upload-url` | `asset:write` | body `{kind,refId,version,filename,contentType}` → `{key,uploadUrl,method:"PUT",expiresInSeconds,publicUrl?}` |
| POST | `/admin/assets` | `asset:write` | body `{kind,refId,version,filename,objectKey,size,sha256,contentType,notes}` → 资产记录（**初始 status=draft**） |
| GET | `/admin/assets?kind=&refId=&status=` | `asset:read` | `data:{items:[…]}`（**`status` 为可选过滤扩展**；响应只含 `items`，无分页字段） |
| PATCH | `/admin/assets/:id/status` | `asset:write` | body `{status:"draft"\|"published"\|"offline"}`；`published_at` 在首次发布时写入 |
| DELETE | `/admin/assets/:id` | `asset:write` | `data:{deleted:true}` |

发布流程：申请 `uploadUrl` → 客户端 `PUT` 上传对象 → `POST /admin/assets` 登记（draft）→ `PATCH …/status` 置 published → 此后公开目录的 `resourceUrl` 与 `web-bundle`/`firmware` 接口才会下发。

## 本地对象存储模式的签名端点

未配置 S3 时，预签名地址指向本服务；该端点**不依赖登录态，仅校验 HMAC 签名与有效期**：

| 方法 | 路径 | 说明 |
|---|---|---|
| PUT | `/api/v1/files/*key?op=put&exp=&sig=` | 上传（单文件上限 64MB） |
| GET | `/api/v1/files/*key?op=get&exp=&sig=` | 下载 |

签名算法：`HMAC-SHA256(secret, op + "\n" + key + "\n" + exp)`，`secret = APP_UPLOAD_SIGN_SECRET`（留空取 `APP_JWT_SECRET`）。S3 模式下该端点返回 404。

## 健康检查

`GET /healthz`（无需鉴权）→ `{"code":0,"data":{"status":"ok","uptimeSeconds":N,"cache":"redis|memory","storage":"s3|local"}}`。据此确认 Redis/S3 是否真正生效（若为 `memory`/`local` 即表示已回退）。

## 契约偏差说明（实现与最初约定的 4 处不一致，前端按此对接）

1. **`POST /admin/roles` 成功返回 200**（与仓库内其它「新建」接口一致），不是 201；只有 `POST /admin/assets` 按要求返回 **201**。
2. **`GET /me/permissions` 有两个路径**：`/api/v1/me/permissions` 与别名 `/api/v1/admin/me/permissions`（约定未指明前缀，两处行为完全一致，后台可统一走 `/admin` 前缀）。
3. **三个写接口的响应体**（约定未规定，实现为返回资源本身，HTTP 200）：
   - `POST /admin/device-whitelist` → 白名单条目 `{deviceKey,modelId,note,enabled}`；
   - `POST /admin/device-mappings` → 映射条目 `{deviceKey,modelId,note}`；
   - `PATCH /admin/assets/:id/status` → 更新后的资产记录。
4. **`GET /admin/assets` 额外支持可选 `status` 过滤**（`?kind=&refId=&status=`），且响应只含 `items`（无 `total`/分页字段）。
5. **内容中心（课程/商品）响应体，前端按此对接**：
   - `GET /admin/courses`、`GET /admin/goods` 返回 `data:{items:[…]}`（对象包 items，与 `/admin/assets`、`/admin/roles` 一致；**不是** `/admin/games` 那样的裸数组）；
   - `POST`（upsert）与 `PUT`（按路径 ID 更新）都返回 `data:{item}`（写入后的对象，与 `POST /admin/roles` 返回 `{role}` 同风格）；`data:{deleted:true}` **只属于 DELETE**；
   - `PATCH …/status` 返回 `data:{status}`（沿用 `/admin/games/:gameId/status` 的既有形状）；
   - 公开接口 `GET /catalog/courses`、`GET /catalog/goods` 返回裸数组 `data:[…]`（与 `/catalog/games` 一致），空目录为 `[]`（不是 `null`）。
6. **创建时只有 `title`（课程）/ `name`（商品）必填**：`summary/coverUrl/videoUrl/durationLabel/level/tags/sort/status` 均可省略（`status` 省略按 `off`，`sort` 省略按 `0`，数组字段省略下发为 `[]`），不会因为缺字段返回 400。

另：`POST /admin/device-whitelist` 的 body 接受一个**可选扩展字段 `enabled`**（冻结契约只列 `deviceKey/modelId/note`），缺省视为启用。
