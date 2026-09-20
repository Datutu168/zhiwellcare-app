#!/usr/bin/env bash
# ============================================================
# 后端发布（在服务器上执行）：安装二进制 → 重启 → 健康检查，失败自动回滚
#
# 用法：backend-release.sh <新二进制路径>
#   backend-release.sh /tmp/zhiwellcare-server
#
# 前置：本机（开发机）已交叉编译好 linux/amd64 静态二进制：
#   cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
#     go build -trimpath -ldflags='-s -w' -o zhiwellcare-server ./cmd/server
# 注意：scp 上传的二进制默认没有可执行位，本脚本会 chmod +x。
# ============================================================
set -eu

NEW="${1:?用法: backend-release.sh <新二进制路径>}"
BIN_DIR=/srv/app/bin
BIN="$BIN_DIR/zhiwellcare-server"
BACKUP="$BIN_DIR/zhiwellcare-server.prev"
STAMP=$(date +%Y%m%d-%H%M%S)

[ -f "$NEW" ] || { echo "找不到二进制: $NEW"; exit 1; }
head -c 4 "$NEW" | grep -q $'\x7fELF' || { echo "不是 ELF 可执行文件，拒绝安装"; exit 1; }
"$NEW" --help >/dev/null 2>&1 || true   # 仅确认可执行（无 --help 也不报错）

mkdir -p "$BIN_DIR"
[ -f "$BIN" ] && cp -f "$BIN" "$BACKUP"

install -m 755 "$NEW" "$BIN.new"
mv -f "$BIN.new" "$BIN"
echo "[backend] 已安装 $BIN（$(stat -c '%A %s bytes' "$BIN")，备份 ${STAMP}）"

sudo systemctl restart zhiwellcare-app
sleep 6

if systemctl is-active --quiet zhiwellcare-app && curl -fsS -m 8 http://127.0.0.1:8080/healthz >/tmp/_health; then
  echo "[backend] 健康检查通过: $(cat /tmp/_health)"
else
  echo "[backend] 启动失败，回滚到上一个版本"
  sudo journalctl -u zhiwellcare-app -n 15 --no-pager | tail -15
  [ -f "$BACKUP" ] && { install -m 755 "$BACKUP" "$BIN"; sudo systemctl restart zhiwellcare-app; sleep 5; }
  echo "[backend] 回滚后状态: $(systemctl is-active zhiwellcare-app)"
  exit 1
fi
