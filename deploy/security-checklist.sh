#!/usr/bin/env bash
# ============================================================
# 上线安全检查（在服务器上执行）：把"部署完还有哪些坑没堵"变成可执行的一条命令
#
# 用法：
#   security-checklist.sh                 # 只检查，不改动任何文件
#   security-checklist.sh --prune-secret  # 确认初始密码已失效时，顺手删掉明文密码文件
#   security-checklist.sh --phone 13900000000
#
# 退出码：0 = 全部通过（允许 WARN）；1 = 存在 FAIL，需处理后再上线。
#
# 其中「初始管理员密码文件」这一项是重点：早期部署把初始密码明文写在
# /srv/app/admin-initial-password.txt，而当时后台没有改密接口，
# 于是这个文件只能一直留着。现在后台支持 PUT /api/v1/me/password，
# 本脚本用「文件里的密码还能不能登录」来判断是否已改密：
#   能登录  → FAIL（明文仍在生效，必须先去后台改密）
#   不能登录 → PASS（已改密，文件可以删；带 --prune-secret 时自动删除）
# ============================================================
set -uo pipefail

ENV_FILE=/srv/app/.env
SECRET_FILE=/srv/app/admin-initial-password.txt
HEALTH=http://127.0.0.1:8080/healthz
API=http://127.0.0.1:8080/api/v1
PRUNE=no
PHONE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --prune-secret) PRUNE=yes ;;
    --phone) shift; PHONE="${1:-}" ;;
    *) echo "未知参数: $1"; exit 2 ;;
  esac
  shift
done

PASS=0; WARN=0; FAIL=0
pass() { echo "  PASS  $1"; PASS=$((PASS + 1)); }
warn() { echo "  WARN  $1"; WARN=$((WARN + 1)); }
fail() { echo "  FAIL  $1"; FAIL=$((FAIL + 1)); }

echo "=== 1. 初始管理员密码文件 ==="
if [ -f "$SECRET_FILE" ]; then
  PERM=$(stat -c '%a' "$SECRET_FILE")
  [ "$PERM" = "600" ] && pass "密码文件权限 600" || fail "密码文件权限 $PERM（应为 600）: chmod 600 $SECRET_FILE"
  [ -z "$PHONE" ] && PHONE=$(grep -oE '1[3-9][0-9]{9}' "$SECRET_FILE" | head -1)
  SECRET=$(grep -v '^[[:space:]]*$' "$SECRET_FILE" | tail -1 | awk '{print $NF}')
  if [ -z "$PHONE" ] || [ -z "$SECRET" ]; then
    warn "无法从文件解析手机号/密码，跳过「已改密」判定（可用 --phone 指定手机号）"
  else
    CODE=$(curl -s -o /dev/null -w '%{http_code}' -m 8 -X POST "$API/auth/login" \
      -H 'Content-Type: application/json' \
      --data-binary "$(printf '{"phone":"%s","password":"%s"}' "$PHONE" "$SECRET")")
    if [ "$CODE" = "200" ]; then
      fail "初始密码（$PHONE）仍然可以登录 —— 明文密码等于有效凭据，必须先去后台改成新密码"
      echo "        登录后台: http://admin.zhiwellcare.com/  →  右上角用户菜单 → 修改密码"
      echo "        改完再执行: $0 --prune-secret"
    elif [ "$CODE" = "401" ]; then
      pass "初始密码已失效（已改密），不再构成凭据泄漏风险"
      if [ "$PRUNE" = "yes" ]; then
        rm -f "$SECRET_FILE" && echo "        已删除 $SECRET_FILE"
      else
        echo "        建议删除: rm -f $SECRET_FILE（或加 --prune-secret 自动删）"
      fi
    else
      warn "登录探测返回 $CODE（无法判定初始密码是否已失效），请人工确认"
    fi
  fi
else
  pass "不存在明文初始密码文件"
fi

echo "=== 2. 配置文件权限与密钥 ==="
if [ -f "$ENV_FILE" ]; then
  PERM=$(stat -c '%a' "$ENV_FILE")
  [ "$PERM" = "600" ] && pass "$ENV_FILE 权限 600" || fail "$ENV_FILE 权限 $PERM（应为 600，内含数据库口令与 JWT 密钥）"
  SECRET_VAL=$(grep '^APP_JWT_SECRET=' "$ENV_FILE" | cut -d= -f2- || true)
  case "$SECRET_VAL" in
    "" ) fail "APP_JWT_SECRET 为空" ;;
    zhiwellcare-dev-secret-change-me) fail "APP_JWT_SECRET 还是内置默认值" ;;
    *) [ "${#SECRET_VAL}" -ge 32 ] && pass "APP_JWT_SECRET 长度 ${#SECRET_VAL}（≥32）" || warn "APP_JWT_SECRET 仅 ${#SECRET_VAL} 字符，建议 ≥32" ;;
  esac
  DBURL=$(grep '^APP_DB_URL=' "$ENV_FILE" | cut -d= -f2- || true)
  case "$DBURL" in
    *@127.0.0.1*|*@localhost*) pass "数据库指向本机" ;;
    "") warn "未配置 APP_DB_URL（内存演示模式）" ;;
    *) fail "数据库不是本机（$(echo "$DBURL" | sed -E 's#.*@([^/?]+).*#\1#')）—— 确认这不是误连其它环境的正式库" ;;
  esac
  # 备份文件里同样有口令，不能比正式文件更松
  while IFS= read -r bak; do
    [ -f "$bak" ] || continue
    PERM=$(stat -c '%a' "$bak")
    [ "$PERM" = "600" ] && pass "备份 $(basename "$bak") 权限 600" || fail "备份 $(basename "$bak") 权限 $PERM（应为 600）"
  done < <(find /srv/app -maxdepth 1 -name '.env.bak-*' | sort -u)
else
  fail "找不到 $ENV_FILE"
fi

echo "=== 3. 运行时后端是否真的生效（不许静默回退） ==="
H=$(curl -fsS -m 8 "$HEALTH" 2>/dev/null || true)
if [ -z "$H" ]; then
  fail "健康检查不可达: $HEALTH"
else
  pass "健康检查可达: $H"
  echo "$H" | grep -q '"cache":"redis"' && pass "缓存后端 = redis" || fail "缓存后端不是 redis（已静默回退内存）: $H"
  echo "$H" | grep -q '"storage":"s3"' && pass "对象存储后端 = s3" || warn "对象存储后端 = local（未配置 APP_S3_* 时属预期，但多实例/大文件场景应上 S3）"
fi

echo "=== 4. 权限缓存在启动时被清理过（避免升级后管理员 403） ==="
if [ -r /srv/logs/app.log ]; then
  N=$(grep -c '权限缓存已清理' /srv/logs/app.log || true)
  [ "${N:-0}" -ge 1 ] && pass "启动日志含「权限缓存已清理」（$N 次）" || warn "启动日志未见「权限缓存已清理」，升级迁移后请确认管理员接口不是 403"
else
  warn "读不到 /srv/logs/app.log，跳过"
fi

echo "=== 5. 依赖 schema 的公开接口 ==="
for path in /catalog/courses /catalog/goods; do
  CODE=$(curl -s -o /dev/null -w '%{http_code}' -m 8 "$API$path")
  [ "$CODE" = "200" ] && pass "GET $path -> 200" || fail "GET $path -> $CODE（多半是迁移没生效）"
done

echo "=== 6. 备份任务 ==="
# 定时任务可能挂在服务账号（ubuntu）名下，而本脚本常以 sudo 运行（此时 `crontab -l` 看到的是 root 的），
# 所以三种位置都要找：当前用户 crontab、其它用户 crontab（需要 root）、/etc/cron.d 与 /etc/crontab。
CRON_HIT=""
crontab -l 2>/dev/null | grep -q 'backup\.sh' && CRON_HIT="当前用户"
if [ -z "$CRON_HIT" ]; then
  for d in /etc/cron.d/* /etc/crontab; do
    [ -f "$d" ] || continue
    grep -q 'backup\.sh' "$d" 2>/dev/null && { CRON_HIT="$d"; break; }
  done
fi
if [ -z "$CRON_HIT" ] && [ "$(id -u)" -eq 0 ]; then
  for u in $(cut -d: -f1 /etc/passwd); do
    crontab -l -u "$u" 2>/dev/null | grep -q 'backup\.sh' && { CRON_HIT="用户 $u"; break; }
  done
fi
if [ -n "$CRON_HIT" ]; then
  pass "存在 backup.sh 定时任务（$CRON_HIT）"
  LATEST=$(find /srv/backup -type f -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -1 | cut -d' ' -f2-)
  if [ -n "$LATEST" ]; then
    AGE_H=$(( ( $(date +%s) - $(stat -c %Y "$LATEST") ) / 3600 ))
    [ "$AGE_H" -le 26 ] && pass "最近备份 ${AGE_H}h 前: $(basename "$LATEST")" || warn "最近备份是 ${AGE_H}h 前（>26h），检查定时任务: $LATEST"
  else
    warn "/srv/backup 下没有备份文件"
  fi
else
  fail "找不到 backup.sh 定时任务（当前用户 crontab / /etc/cron.d / 其它用户 crontab 都没有；非 root 运行无法检查其它用户）"
fi

echo
echo "=== 汇总: PASS=$PASS WARN=$WARN FAIL=$FAIL ==="
[ "$FAIL" -eq 0 ] || { echo "存在 FAIL 项，处理完再上线。"; exit 1; }
echo "未发现阻断性问题。"
