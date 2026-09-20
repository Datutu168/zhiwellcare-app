#!/usr/bin/env bash
# ============================================================
# 发布到腾讯云 COS（在开发机/CI 上执行，需要本机已配 coscmd）
#
# 用法：
#   deploy/publish-cos.sh android <APK 路径> [--notes 文件]
#   deploy/publish-cos.sh cos-manifest-only      # 只同步 Android 清单（不上传 APK）
#
# 环境变量：
#   COS_BASE  默认 https://zhiwellcare-release-1471190121.cos.ap-beijing.myqcloud.com/releases
#             备案通过并绑定自定义域名后，改成 https://dl.zhiwellcare.com/releases 即可
#   UPDATE_ASSET_BASE 传给清单生成脚本（与 COS_BASE 保持一致）
#
# ⚠️ 已知限制：腾讯云禁止用 COS 默认域名公开分发 APK/IPA
#    （DownloadForbidden: please use custom domain instead），
#    因此本脚本对 APK 会先尝试上传，失败时提示改用自定义域名 —— 清单/固件/Web 包不受限。
# ============================================================
set -euo pipefail

COS_BASE="${COS_BASE:-https://zhiwellcare-release-1471190121.cos.ap-beijing.myqcloud.com/releases}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODE="${1:-android}"

dev_ok() { command -v coscmd >/dev/null 2>&1; }
remote() { coscmd "$@"; }

echo "[publish] COS_BASE = $COS_BASE"

case "$MODE" in
  android)
    APK="${2:?用法: publish-cos.sh android <APK 路径> [--notes 文件]}"
    NOTES_ARG=""
    [ "${3:-}" = "--notes" ] && NOTES_ARG="--notes ${4:?缺少 notes 文件}"
    [ -f "$APK" ] || { echo "找不到 APK: $APK"; exit 1; }

    VERSION=$(node -e "console.log(require('$ROOT/release-version.json').productVersion)")
    OUT="$ROOT/release-output/android-latest.json"

    echo "[publish] 生成清单（URL 指向 $COS_BASE）"
    UPDATE_ASSET_BASE="$COS_BASE" node "$ROOT/scripts/generate-android-update-manifest.mjs" \
      --apk "$APK" --output "$OUT" $NOTES_ARG

    echo "[publish] 上传 APK: releases/v${VERSION}/$(basename "$APK")"
    if dev_ok; then
      if ! remote upload -f "$APK" "releases/v${VERSION}/$(basename "$APK")" 2>&1 | tail -2; then
        echo "[publish] ⚠️ APK 上传或公网分发被拒 —— 腾讯云要求用自定义域名分发 APK。"
        echo "           清单仍会上传；APK 请改用已备案的自定义域名（绑定到 COS/CDN）后重试。"
      fi
      echo "[publish] 上传清单: releases/android-latest.json"
      remote upload -f "$OUT" "releases/android-latest.json" 2>&1 | tail -2
      echo "[publish] 远端对象:"; remote list -r releases/ 2>&1 | sed 's/^/  /'
    else
      echo "[publish] 本机没有 coscmd，跳过上传。安装：pip3 install --user coscmd 并配置 ~/.cos.conf"
      echo "           清单已生成在: $OUT"
    fi

    cat <<EOF

[publish] 验证（公网）：
  curl -sI "$COS_BASE/android-latest.json" | head -3
  curl -s  "$COS_BASE/android-latest.json"
  # APK 需自定义域名后：curl -sI "$COS_BASE/v${VERSION}/$(basename "$APK")" | head -3
EOF
    ;;

  cos-manifest-only)
    echo "[publish] 仅同步 Android 清单（APK 走 GitHub 或其他源时使用）"
    OUT="$ROOT/release-output/android-latest.json"
    [ -f "$OUT" ] || { echo "先运行 publish-cos.sh android <APK> 生成清单"; exit 1; }
    remote upload -f "$OUT" "releases/android-latest.json"
    ;;

  *)
    echo "未知模式: $MODE（可用：android | cos-manifest-only）"; exit 1;;
esac
