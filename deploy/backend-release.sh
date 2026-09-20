#!/usr/bin/env bash
# ============================================================
# 后端发布（在服务器上执行）：安装二进制 →（可选）同步迁移 → 重启 → 健康检查 + 依赖 schema 的接口探测，失败自动回滚
#
# 用法：
#   backend-release.sh <新二进制路径> [迁移目录]
#   backend-release.sh /tmp/zhiwellcare-server                 # 只换二进制
#   backend-release.sh /tmp/zhiwellcare-server /tmp/migrations # 二进制 + 迁移一起上（推荐）
#
# 前置：本机（开发机）已交叉编译好 linux/amd64 静态二进制：
#   cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
#     go build -trimpath -ldflags='-s -w' -o zhiwellcare-server ./cmd/server
# 注意：scp 上传的二进制默认没有可执行位，本脚本会 chmod +x。
#
# 为什么要探测「依赖 schema 的接口」：
#   曾经发生过「新二进制上了、migrations/004 忘了传」——服务健康检查照过，
#   但 /catalog/courses 因为表不存在而 500。现在每步都必须过，否则连迁移一起回滚。
#   二进制自身也会在启动时校验所需迁移文件是否齐全（store.RequiredMigrations），失败即退出，
#   所以漏传迁移会直接启动失败 → 下面的健康检查失败 → 回滚。
# ============================================================
set -eu

NEW="${1:?用法: backend-release.sh <新二进制路径> [迁移目录]}"
MIG_SRC="${2:-}"
BIN_DIR=/srv/app/bin
BIN="$BIN_DIR/zhiwellcare-server"
BACKUP="$BIN_DIR/zhiwellcare-server.prev"
MIG_DIR=/srv/app/migrations
MIG_BACKUP="${MIG_DIR}.prev"
STAMP=$(date +%Y%m%d-%H%M%S)
HEALTH=http://127.0.0.1:8080/healthz

[ -f "$NEW" ] || { echo "找不到二进制: $NEW"; exit 1; }
head -c 4 "$NEW" | grep -q $'\x7fELF' || { echo "不是 ELF 可执行文件，拒绝安装"; exit 1; }
"$NEW" --help >/dev/null 2>&1 || true   # 仅确认可执行（无 --help 也不报错）

# ---------- 迁移文件同步（可选但推荐） ----------
MIG_CHANGED=no
if [ -n "$MIG_SRC" ]; then
  [ -d "$MIG_SRC" ] || { echo "找不到迁移目录: $MIG_SRC"; exit 1; }
  COUNT=$(find "$MIG_SRC" -maxdepth 1 -name '*.sql' | wc -l)
  [ "$COUNT" -gt 0 ] || { echo "迁移目录里没有 .sql 文件: $MIG_SRC"; exit 1; }
  echo "[backend] 迁移文件检查: $COUNT 个 .sql"
  for f in "$MIG_SRC"/*.sql; do
    head -c 3 "$f" | grep -q $'\xef\xbb\xbf' && { echo "迁移文件带 UTF-8 BOM，拒绝安装: $f"; exit 1; }
    [ -s "$f" ] || { echo "迁移文件为空，拒绝安装: $f"; exit 1; }
  done
  rm -rf "$MIG_BACKUP"
  [ -d "$MIG_DIR" ] && cp -a "$MIG_DIR" "$MIG_BACKUP"
  mkdir -p "$MIG_DIR"
  find "$MIG_DIR" -maxdepth 1 -name '*.sql' -delete
  cp -f "$MIG_SRC"/*.sql "$MIG_DIR/"
  chmod 644 "$MIG_DIR"/*.sql
  MIG_CHANGED=yes
  echo "[backend] 已同步迁移: $(ls -1 "$MIG_DIR"/*.sql | xargs -n1 basename | tr '\n' ' ')"
fi

# ---------- 安装二进制 ----------
mkdir -p "$BIN_DIR"
[ -f "$BIN" ] && cp -f "$BIN" "$BACKUP"

install -m 755 "$NEW" "$BIN.new"
mv -f "$BIN.new" "$BIN"
echo "[backend] 已安装 $BIN（$(stat -c '%A %s bytes' "$BIN")，备份 ${STAMP}）"

rollback() {
  echo "[backend] 回滚中…"
  sudo journalctl -u zhiwellcare-app -n 20 --no-pager | tail -20
  if [ -f "$BACKUP" ]; then install -m 755 "$BACKUP" "$BIN"; fi
  if [ "$MIG_CHANGED" = yes ] && [ -d "$MIG_BACKUP" ]; then
    find "$MIG_DIR" -maxdepth 1 -name '*.sql' -delete
    cp -a "$MIG_BACKUP"/. "$MIG_DIR"/
  fi
  sudo systemctl restart zhiwellcare-app
  sleep 5
  echo "[backend] 回滚后状态: $(systemctl is-active zhiwellcare-app)"
  exit 1
}

sudo systemctl restart zhiwellcare-app
sleep 6

systemctl is-active --quiet zhiwellcare-app || rollback
curl -fsS -m 8 "$HEALTH" >/tmp/_health || rollback
echo "[backend] 健康检查通过: $(cat /tmp/_health)"

# 依赖 schema 的接口探测：迁移漏传/失败会在这里暴露（而不是等用户点开页面才发现 500）
for path in /api/v1/catalog/courses /api/v1/catalog/goods; do
  CODE=$(curl -s -o /dev/null -w '%{http_code}' -m 8 "http://127.0.0.1:8080$path")
  if [ "$CODE" != "200" ]; then
    echo "[backend] 探测 $path 返回 $CODE（期望 200）—— 多半是迁移没生效"
    rollback
  fi
  echo "[backend] 探测 $path -> 200"
done

echo "[backend] 发布完成，上一版本二进制备份: $BACKUP"
[ -d "$MIG_BACKUP" ] && echo "[backend] 上一版本迁移备份: $MIG_BACKUP"
rm -f /tmp/_health
