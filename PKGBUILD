# Maintainer: meowrain <meowrain@126.com>
# Contributor: ilius <saeedgnu@riseup.net>

pkgname=localsend-go
pkgver=1.3.0
pkgrel=1
pkgdesc="CLI implementation of LocalSend protocol in Go"
arch=('x86_64' 'aarch64' 'armv7h' 'riscv64')
url="https://github.com/meowrain/localsend-go"
license=('MIT')
depends=('glibc')
makedepends=('go')

source=("$pkgname-$pkgver.tar.gz::$url/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=('03ad0ae0fec719ec6425023338c8f4017f78237940c868435f8601addb9e99cd')

build() {
  cd "$pkgname-$pkgver"
  # 入口在仓库根目录（package main），不是 cmd/main.go。
  # -X 指向 internal/version：旧 Tag 里还没有这个包时 -X 会被静默忽略，不会导致构建失败。
  go build -trimpath \
    -ldflags "-s -w -X github.com/meowrain/localsend-go/internal/version.Version=$pkgver" \
    -o "$pkgname" .
}

package() {
  cd "$pkgname-$pkgver"
  install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
