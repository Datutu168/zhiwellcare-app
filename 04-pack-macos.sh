#!/usr/bin/env bash
# ============================================================
#  ZhiWellCare — macOS 桌面端打包（Tauri 2 / .app + .dmg）
#
#  用法（必须在 macOS 上执行）：
#    ./04-pack-macos.sh                  # 当前机器架构，出 .app + .dmg
#    ./04-pack-macos.sh --universal      # 通用包（Intel + Apple Silicon 合一）
#    ./04-pack-macos.sh --app-only       # 只出 .app（调试用，不生成 dmg）
#    ./04-pack-macos.sh --manifest       # 顺带生成/更新 release-output/latest.json
#
#  为什么必须在 Mac 上跑：Tauri 的 macOS 包依赖 Xcode 命令行工具的
#  codesign / hdiutil / xcrun，Windows 与 Linux 都无法交叉编译 macOS 目标。
#  没有 Mac 也可以推代码后用 GitHub Actions 的 macos-latest 跑同一条命令。
#
#  未签名说明：没有 Apple 开发者账号时只能出未签名包，用户首次打开需要
#  右键 →「打开」，或执行： xattr -dr com.apple.quarantine /Applications/ZhiWellCare.app
# ============================================================
set -euo pipefail
cd "$(dirname "$0")"

UNIVERSAL=no
MANIFEST=no
BUNDLES=app,dmg

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --universal) UNIVERSAL=yes ;;
    --manifest)  MANIFEST=yes ;;
    --app-only)  BUNDLES=app ;;
    -h|--help)   usage; exit 0 ;;
    *) echo "未知参数：$1"; echo; usage; exit 2 ;;
  esac
  shift
done

echo "== 环境检查 =="
if [ "$(uname -s)" != "Darwin" ]; then
  echo "[ERROR] 当前系统是 $(uname -s)，macOS 包只能在 Mac 上构建。"
  echo "        可在 Mac 本机执行，或推到 GitHub 用 macos-latest runner 构建。"
  exit 1
fi
for cmd in node npm cargo rustc; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "[ERROR] 找不到 $cmd，请先安装（Rust 见 https://rustup.rs）。"; exit 1; }
done
if ! xcode-select -p >/dev/null 2>&1; then
  echo "[ERROR] 未安装 Xcode 命令行工具，请先执行： xcode-select --install"
  exit 1
fi
echo "  macOS $(sw_vers -productVersion) / $(uname -m) / node $(node -v)"

VER=$(node -e "console.log(require('./release-version.json').productVersion)")
OUT=release-output
mkdir -p "$OUT"
echo "  版本：$VER"

echo
echo "== 1/3 同步版本号基线（package.json / tauri.conf / android） =="
npm run version:sync

echo
echo "== 2/3 构建（前端 vue-tsc + vite，然后 Rust；首次编译依赖通常 10 分钟以上） =="
if [ "$UNIVERSAL" = yes ]; then
  echo "  通用包需要两个 Rust 目标，缺失时自动安装："
  rustup target add aarch64-apple-darwin x86_64-apple-darwin
  npm run tauri:build:macos:universal
  BUNDLE_DIR="src-tauri/target/universal-apple-darwin/release/bundle"
  MANIFEST_PLATFORM=darwin-aarch64,darwin-x86_64
else
  if [ "$BUNDLES" = "app" ]; then
    npm run tauri:build -- --bundles app
  else
    npm run tauri:build:macos
  fi
  BUNDLE_DIR="src-tauri/target/release/bundle"
  case "$(uname -m)" in
    arm64)  MANIFEST_PLATFORM=darwin-aarch64 ;;
    x86_64) MANIFEST_PLATFORM=darwin-x86_64 ;;
    *)      MANIFEST_PLATFORM="" ;;
  esac
fi

echo
echo "== 3/3 收集产物到 $OUT/ =="
copy_first() {
  # $1=glob 目录，$2=说明
  local found=0
  shopt -s nullglob
  for f in $1; do
    cp -Rf "$f" "$OUT/"
    echo "  copied  $OUT/$(basename "$f")"
    found=1
  done
  shopt -u nullglob
  [ "$found" = 1 ] || echo "  [WARN] 没找到 $2（$1）"
}

copy_first "$BUNDLE_DIR/dmg/*.dmg"         "dmg 安装包"
copy_first "$BUNDLE_DIR/macos/*.app.tar.gz" "Tauri 更新包（.app.tar.gz）"
copy_first "$BUNDLE_DIR/macos/*.app.tar.gz.sig" "更新包签名（.sig）"
copy_first "$BUNDLE_DIR/macos/*.app"       ".app 应用包"

if [ "$MANIFEST" = yes ]; then
  echo
  echo "== 生成 Tauri 更新清单（latest.json） =="
  APP_TGZ=$(ls -1 "$BUNDLE_DIR"/macos/*.app.tar.gz 2>/dev/null | head -1 || true)
  if [ -z "$APP_TGZ" ]; then
    echo "  [WARN] 没有 .app.tar.gz，跳过清单生成"
  elif [ ! -f "$APP_TGZ.sig" ]; then
    echo "  [WARN] 缺少 $APP_TGZ.sig —— 需要设置 TAURI_SIGNING_PRIVATE_KEY 后重新构建"
  else
    node scripts/generate-tauri-update-manifest.mjs \
      --artifact "$APP_TGZ" --platform "$MANIFEST_PLATFORM"
  fi
fi

echo
echo "== 完成 =="
echo "  产物目录：$OUT/"
ls -1 "$OUT" | sed 's/^/    /'
echo
echo "  安装：双击 .dmg 把 ZhiWellCare 拖进「应用程序」。"
echo "  【重要】未签名版本的首次打开："
echo "    方式一：在「应用程序」里右键 ZhiWellCare →「打开」→ 再点「打开」"
echo "    方式二：终端执行  xattr -dr com.apple.quarantine /Applications/ZhiWellCare.app"
echo "  蓝牙：首次连接训练设备时系统会弹权限申请（用途说明来自 src-tauri/Info.plist），必须点允许。"
echo "  【限制】自动更新在 macOS 上要求应用已签名，未签名版本请用上面方式手动安装新包。"
