
<div align="center">
<h1>LocalSend CLI</h1>
  <img src="../doc/images/logo.png" alt="LocalSend CLI logo" width="150" height="150">
  <p>✨LocalSend CLI✨</p>
</div>

## ドキュメント | Document

[中文](doc/README_zh.md) | [EN](doc/README_en.md) | [日本語](doc/README_ja.md)

## インストール

> 😊Releaseページから実行ファイルをダウンロードし、ご利用のプラットフォームに対応するものを選択してください。

### 必須条件

- [Go](https://golang.org/dl/) 1.16 以上

### リポジトリのクローン

```sh
git clone --branch v1.1.0 https://github.com/meowrain/localsend-go.git
cd localsend_cli
```

### ビルド

`Makefile` を使用してプログラムをビルドします。

```sh
make build
```

これにより、サポートされているすべてのプラットフォーム向けにバイナリファイルが生成され、`bin` ディレクトリに保存されます。

## 使用方法

### ヘルプ

![使用ヘルプ](../doc/images/image-1.png)

### プログラムの実行

#### 受信モード

```sh
.\localsend_cli-windows-amd64.exe -mode receive
```

![alt text](../doc/images/image-2.png)

ご使用のOSとアーキテクチャに対応するバイナリファイルを選択して実行してください。
Linuxの場合、ping機能を有効にするために次のコマンドを実行する必要があります：
`sudo setcap cap_net_raw=+ep localsend_cli`

#### 送信モード

```
.\localsend_cli-windows-amd64.exe -mode send -file ./xxxx.xx
```

例：

```
.\localsend_cli-windows-amd64.exe -mode send -file ./hello.tar.gz
```

![alt text](../doc/images/image-3.png)
![alt text](../doc/images/image-4.png)

> `j`, `k` または矢印キーで上下に移動、`q`で終了、`enter`で決定できます。

## コントリビューション

このプロジェクトの改善のために、issueやpull requestの提出を歓迎します。

## ライセンス

[MIT](LICENSE)

# Todo

- [x] 送信機能の改善：送信したテキストがデバイス上に直接表示されます
- [ ] TUIの更新問題
- [ ] i18n（多言語対応）
