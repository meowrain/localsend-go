# Maintainer: meowrain <meowrain@126.com>
# Contributor: ilius <saeedgnu@riseup.net>

pkgname=localsend-go
pkgver=1.2.2
pkgrel=1
pkgdesc="CLI implementation of LocalSend protocol in Go"
arch=('x86_64' 'aarch64' 'armv7h' 'riscv64')
url="https://github.com/meowrain/localsend-go"
license=('MIT')
depends=('glibc')
makedepends=('go')

source=("$pkgname-$pkgver.tar.gz::$url/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=('07f121b98ab3d5e6b48213b6637dfcf530182ca31e0817e7ba04ab805912a275')

build() {
  cd "$pkgname-$pkgver"
  go build -o "$pkgname" "cmd/main.go"
}

package() {
  cd "$pkgname-$pkgver"
  install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
