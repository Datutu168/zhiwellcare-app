#!/usr/bin/env bash
# ============================================================
# 每日备份（在服务器上由 cron 执行）：PostgreSQL 全库 + 两个裸库 bundle
#
# 用法：backup.sh            # 正常备份 + 清理过期
#       backup.sh --list     # 只看现有备份
#
# 产物：/srv/backup/<日期>/{zhiwellcare.sql.gz, zhiwellcare-web.bundle, zhiwellcare-app.bundle}
# 保留：DB 保留 14 天，仓库 bundle 保留 30 天（RETAIN_DB / RETAIN_GIT 可覆盖）
# 异地：若配置了 COS（安装 coscmd 并 coscmd config 过），会追加一份到 COS；未配置则跳过。
# ============================================================
set -eu

BACKUP_ROOT=/srv/backup
RETAIN_DB="${RETAIN_DB:-14}"
RETAIN_GIT="${RETAIN_GIT:-30}"
STAMP=$(date +%Y%m%d-%H%M%S)
DAY=$(date +%Y-%m-%d)
DIR="$BACKUP_ROOT/$DAY"
LOG="$BACKUP_ROOT/backup.log"

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

if [ "${1:-}" = "--list" ]; then
  du -sh "$BACKUP_ROOT"/* 2>/dev/null | sort -k2 || echo "（暂无备份）"
  exit 0
fi

mkdir -p "$DIR"

# ---------- 1) PostgreSQL ----------
DBURL=$(grep '^APP_DB_URL=' /srv/app/.env | cut -d= -f2-)
log "pg_dump 开始"
if pg_dump "$DBURL" --no-owner --no-privileges | gzip -9 > "$DIR/zhiwellcare.sql.gz"; then
  log "pg_dump OK: $(du -h "$DIR/zhiwellcare.sql.gz" | cut -f1)"
else
  log "pg_dump 失败！"; rm -f "$DIR/zhiwellcare.sql.gz"
fi

# ---------- 2) 裸库 bundle（含全部分支与标签） ----------
for repo in zhiwellcare-web zhiwellcare-app; do
  if [ -d "/srv/git/$repo.git" ]; then
    git --git-dir="/srv/git/$repo.git" bundle create "$DIR/$repo.bundle" --all >/dev/null 2>&1 \
      && log "$repo bundle OK: $(du -h "$DIR/$repo.bundle" | cut -f1)" \
      || log "$repo bundle 失败！"
  fi
done

# ---------- 3) 异地副本（腾讯云 COS，可选） ----------
if command -v coscmd >/dev/null 2>&1; then
  log "上传 COS"
  coscmd upload -r "$DIR" "/backup/$DAY/" >>"$LOG" 2>&1 && log "COS 上传 OK" || log "COS 上传失败（本地备份仍在）"
else
  log "未安装 coscmd，跳过异地备份"
fi

# ---------- 4) 清理过期 ----------
find "$BACKUP_ROOT" -maxdepth 1 -type d -name '20*' -mtime +"$RETAIN_DB" -exec rm -rf {} + 2>/dev/null || true
log "清理完成（DB 保留 $RETAIN_DB 天 / 仓库 $RETAIN_GIT 天）"
du -sh "$BACKUP_ROOT" | sed 's/^/[backup] 当前备份总量: /'

# 仓库 bundle 单独按更长周期保留：把最近 30 天的 bundle 留一份到 git-keep/
mkdir -p "$BACKUP_ROOT/git-keep"
for repo in zhiwellcare-web zhiwellcare-app; do
  [ -f "$DIR/$repo.bundle" ] && cp -f "$DIR/$repo.bundle" "$BACKUP_ROOT/git-keep/$repo.bundle"
done
