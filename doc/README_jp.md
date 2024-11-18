<div align="center">
    <h1>LocalSend Go</h1>
    <h4>✨Goで実装されたLocalSendのCLI✨</h4>
    <img src="https://forthebadge.com/images/badges/built-with-love.svg" />
    <br>
    <img src="https://counter.seku.su/cmoe?name=localsend-go&theme=mb" alt="localsend-go" />
</div>

## ドキュメント

[中文](doc/README_zh.md) | [EN](doc/README_en.md) | [日本語](doc/README_jp.md)

現在、v1.1.0とv1.2.0のバージョンに分かれています。v1.1.0のドキュメントについては [Localsend-Go-Version-1.1.0 ドキュメント](version1.1.0/) を参照してください。

以下はv1.2.0バージョンのドキュメントです。

## インストール

### Arch Linux

```bash
yay -Syy
yay -S localsend-go
```

> 😊 または、リリースセクションから実行可能ファイルをダウンロードしてください。ご使用のプラットフォームに対応したものを選択してください。

### 前提条件

- [Go](https://golang.org/dl/) 1.16以降

### リポジトリのクローン

```sh
git clone https://github.com/meowrain/localsend_cli.git
cd localsend_cli
```

### ビルド

`Makefile`を使用してプログラムをビルドします。

```sh
make build
```

これにより、サポートされているすべてのプラットフォーム用のバイナリが生成され、`bin`ディレクトリに保存されます。

## 使用方法

### プログラムの実行

Windowsでは、実行可能ファイルをダブルクリックするだけで起動できます。

![Windows](images/windows.png)

または以下のコマンドを実行してください：

```sh
.\localsend_cli-windows-amd64.exe
```

![Version 1.2](images/v1.2.png)

キーボードを使用して希望するモードを選択するだけで、対応するモードが自動的に開始されます。

![Windows Run](images/windows_run.png)

> 受信モードでは、ファイル受信後に`Ctrl + C`を使用してプログラムを終了してください。ウィンドウを直接閉じないでください。Windowsでは、ウィンドウを閉じてもプログラムは終了しません。

ご使用のOSとアーキテクチャに対応したバイナリを実行してください。

Linuxでは、以下のコマンドを実行してping機能を有効にします：

```sh
sudo setcap cap_net_raw=+ep localsend_cli
```

## コントリビュート

> 以下の貢献者の皆さんのサポートに感謝します！

> <a href="https://github.com/meowrain/doc-for-sxau/graphs/contributors">
> <img src="https://contrib.rocks/image?repo=meowrain/localsend-go" />
> </a>

このプロジェクトの改善にご協力いただける場合は、ぜひissueやpull requestを提出してください。

## ライセンス

[MIT](LICENSE)

## Todo

- [x] 送信機能の改善：送信したテキストを受信デバイスに直接表示する。
- [ ] TUIの更新問題の修正。
- [ ] 国際化（i18n）の追加。

## スター履歴

[![Star History Chart](https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date)](https://star-history.com/#meowrain/localsend-go&Date)
