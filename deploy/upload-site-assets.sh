#!/usr/bin/env bash
# 上传官网静态资源（图片 / 说明书）到 COS 的 site/ 前缀
set -eu
export PATH="$HOME/.local/bin:$PATH"

rm -rf /tmp/sa
mkdir -p /tmp/sa
tar -xzf /tmp/img.tgz -C /tmp/sa
tar -xzf /tmp/dl.tgz -C /tmp/sa

echo "== 本地待上传清单 =="
find /tmp/sa -type f | sed 's|/tmp/sa/|  |'

echo "== 上传 img =="
coscmd upload -r -f /tmp/sa/img/ site/img/ 2>&1 | tail -3

echo "== 上传 downloads =="
coscmd upload -r -f /tmp/sa/downloads/ site/downloads/ 2>&1 | tail -3

echo "== 远端 site/ 对象 =="
coscmd list -r site/ 2>&1
