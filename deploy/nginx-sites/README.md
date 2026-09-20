# 生产 nginx vhost（单机 / 腾讯云 Lighthouse 布局）

三个站点的完整 vhost，作为 `49.233.153.19` 上 `/etc/nginx/sites-available/*`
的唯一事实来源。改配置请改这里再执行安装脚本，不要直接手改服务器。

| 文件 | server_name | root | 反代 `/api/` |
| --- | --- | --- | --- |
| `zwkl-web.conf` | `zhiwellcare.cn` / `www.*` / `zhiwellcare.com` / `www.*` / `_`（default_server） | `/srv/sites/web/current` | 否（纯静态官网） |
| `zwkl-app.conf` | `game.zhiwellcare.cn` / `game.zhiwellcare.com` | `/srv/sites/app/current` | 是 |
| `zwkl-admin.conf` | `admin.zhiwellcare.cn` / `admin.zhiwellcare.com` | `/srv/sites/admin/current` | 是 |

安装/更新：

```bash
sudo bash deploy/nginx-sites/install.sh              # 全部
sudo bash deploy/nginx-sites/install.sh zwkl-admin   # 仅后台
```

脚本会先备份原文件到 `/root/<name>.conf.bak-<时间戳>`，`nginx -t` 通过后才
reload，并顺手删掉与 `zwkl-web` 抢默认站的 `/etc/nginx/sites-enabled/default`。

## 为什么后台 vhost 必须带 `/api/` 反代（事故记录）

`admin/` 前端用的是**相对路径**请求（打包产物里就是 `fetch(`/api/v1${path}`)`）。
若后台 vhost 只做静态下发：

- `POST /api/v1/auth/login` → **405 Not Allowed**（nginx 静态文件不允许 POST）；
- `GET /api/v1/admin/courses` → **200 但响应体是 `index.html`**（SPA 回退），前端 `res.json()` 解析失败。

两种表现都不像"配置漏了"，很容易被误判成"前端 bug / 后端 403"，实际是后台登录都进不去。
`zwkl-admin.conf` 因此带上与 App 站一致的 `location /api/` 反代：后台与 API 同域，
顺带不再依赖 CORS 白名单。

## 注意

- 官网（`zwkl-web.conf`）**故意不反代** `/api/`，所以以 `zhiwellcare.com` 为 Host
  访问 `/api/v1/...` 返回 404 属预期，不是故障；探测 API 请用 `game.*` / `admin.*`。
- `reload` 是平滑重启，旧 worker 会继续服务存量连接：改完配置立刻发请求可能仍打到旧配置。
  自检稍等 1–2 秒，或直接看脚本末尾的自检输出。
- 当前仅监听 80；HTTPS 证书因域名备案拦截暂未签发（详见 `backend/DEPLOYMENT.md`）。
