#!/usr/bin/env bash
# ============================================================
# 静态站点发布（在服务器上执行）：解包 → 切软链 → 清理旧版本
#
# 用法：release.sh <web|app|admin> <tarball>
#   release.sh app /tmp/zwkl-app.tar.gz
#
# 设计：产物解到 releases/<时间戳>/，用 current 软链原子切换，
#       出错不会影响正在服务的版本；保留最近 KEEP 个版本便于回滚。
# 回滚：把 current 指回上一个 release 即可（脚本末尾有提示）。
# ============================================================
set -eu

SITE="${1:?用法: release.sh <web|app|admin> <tarball>}"
TARBALL="${2:?缺少 tarball 路径}"
KEEP="${KEEP:-5}"
ROOT="/srv/sites/$SITE"

[ -f "$TARBALL" ] || { echo "找不到产物: $TARBALL"; exit 1; }
case "$SITE" in web|app|admin) ;; *) echo "非法站点名: $SITE（只能是 web/app/admin）"; exit 1;; esac

TS=$(date +%Y%m%d-%H%M%S)
PREV=$(readlink -f "$ROOT/current" 2>/dev/null || true)

mkdir -p "$ROOT/releases/$TS"
tar -xzf "$TARBALL" -C "$ROOT/releases/$TS"
[ -f "$ROOT/releases/$TS/index.html" ] || { echo "产物里没有 index.html，拒绝发布"; rm -rf "$ROOT/releases/$TS"; exit 1; }

ln -sfn "$ROOT/releases/$TS" "$ROOT/current"
echo "[release] $SITE -> $ROOT/releases/$TS（文件 $(find -L "$ROOT/current" -type f | wc -l) 个）"

# 清理旧版本（保留 current 指向的那个）
cd "$ROOT/releases"
ls -1dt */ 2>/dev/null | tail -n +$((KEEP + 1)) | while read -r old; do
  [ "$ROOT/releases/${old%/}" = "$ROOT/releases/$TS" ] && continue
  echo "[release] 清理旧版本 $old"
  rm -rf "$old"
done

echo "[release] 完成。上一个版本: ${PREV:-无}"
echo "[release] 回滚命令: ln -sfn '${PREV}' '$ROOT/current'"
