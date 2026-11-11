#!/bin/bash
# 本地构建 deb 包（CI 上由 .cnb.yml 的 go-build-deb 完成）。
#
# 用法：./build_dpkg.sh [version]
#   version 省略时取 git describe，再省略则为 0.0.0。
# 二进制从 bin/ 拷入暂存目录，该目录不入库。
set -euo pipefail

cd "$(dirname "$0")"

VERSION="${1:-$(git describe --tags --always 2>/dev/null | sed 's/^v//' || echo 0.0.0)}"
if [ -z "$VERSION" ] || [ "$VERSION" = "v" ]; then
  VERSION="0.0.0"
fi
ARCH="amd64"
# Makefile 产物名历史上用过 localsend_go；两种都接受，避免本地按提示构建后仍找不到
SRC="bin/localsend-go-linux-$ARCH"
if [ ! -f "$SRC" ]; then
  SRC="bin/localsend_go-linux-$ARCH"
fi

if [ ! -f "$SRC" ]; then
  echo "缺少构建产物 bin/localsend-go-linux-$ARCH，请先执行 make build" >&2
  exit 1
fi

mkdir -p debian/usr/local/bin
cp "$SRC" debian/usr/local/bin/localsend-cli
chmod 755 debian/usr/local/bin/localsend-cli debian/DEBIAN/postinst
sed -i "s/^Version: .*/Version: $VERSION/" debian/DEBIAN/control
sed -i "s/^Architecture: .*/Architecture: $ARCH/" debian/DEBIAN/control

dpkg-deb --build --root-owner-group debian "localsend-cli_${VERSION}_${ARCH}.deb"
ls -lh ./*.deb
