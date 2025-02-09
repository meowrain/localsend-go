<div align="center">
    <h1>LocalSend Go</h1>
    <h4>✨Go言語で実装されたLocalSendのCLIツール✨</h4>
    <img src="https://forthebadge.com/images/badges/built-with-love.svg" />
    <br>
    <img src="https://counter.seku.su/cmoe?name=localsend-go&theme=mb" alt="localsend-go" />
</div>

## はじめに

LocalSend Goは、Go言語で実装されたLocalSendプロトコルのコマンドラインツールで、クロスプラットフォームのファイル転送をサポートしています。このプロジェクトは、シンプルなコマンドラインインターフェースとTUI（ターミナルユーザーインターフェース）を提供し、デバイス間の迅速なファイル転送を実現します。

## 特徴

- ファイルの送受信
- クロスプラットフォーム対応（Windows、Linux、macOS）
- シンプルなTUIインターフェース
- テキストとファイルの転送サポート
- 自動デバイス検出
- 多言語対応

## ドキュメント

[中文](doc/README_zh.md) | [EN](doc/README_en.md) | [日本語](doc/README_jp.md)

現在、v1.1.0とv1.2.0のバージョンに分かれています。v1.1.0のドキュメントについては [Localsend-Go-Version-1.1.0 ドキュメント](version1.1.0/) を参照してください。

以下はv1.2.0バージョンのドキュメントです。

## インストール

### パッケージマネージャーから

#### Arch Linux
> ⚠️ 注意：archパッケージはまだバージョン1.1.0です

```bash
yay -Syy
yay -S localsend-go
```

### ソースからビルド

1. Go 1.22以上がインストールされていることを確認
2. リポジトリをクローン
   ```bash
   git clone https://github.com/meowrain/localsend_cli.git
   cd localsend_cli
   ```

3. ビルド
   ```bash
   make build
   ```

コンパイルされたバイナリは `bin` ディレクトリに保存されます。

## 使用方法

### 基本的な使い方

<div align="center">
    <p><b>メイン画面</b></p>
    <img src="https://blog.meowrain.cn/api/i/2025/02/09/eHAgcd1739113761477122645.avif" width="80%" />
</div>

1. プログラムの起動
   - Windows: 実行ファイルをダブルクリックまたはコマンドラインから実行
   - Linux/macOS: ターミナルで実行ファイルを実行

2. モードの選択
   - 矢印キーで操作モード（送信/受信）を選択
   - Enterで確定

3. 送信モード
   - 送信するファイルを選択
   - 受信側の接続を待機
   - 転送を確認

   <div align="center">
       <p><b>送信画面</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/xPUd841739113859215495111.avif" width="80%" />
       <p><b>クライアント確認</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/mS1J3k1739113875412020167.avif" width="80%" />
   </div>

4. 受信モード
   - 送信側の接続を待機
   - 自動的にファイルを受信
   - `Ctrl + C` でプログラムを終了

   <div align="center">
       <p><b>受信画面</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/OZuXZu1739113816793484432.avif" width="80%" />
       <p><b>転送完了</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/YjbG9f1739113834583691367.avif" width="80%" />
   </div>

### 特記事項

Linuxシステムでは、追加のping権限設定が必要です：
```bash
sudo setcap cap_net_raw=+ep localsend_cli
```

## プロジェクト構造

```
.
├── cmd/          # メインプログラムエントリー
├── internal/     # 内部パッケージ
│   ├── discovery/   # デバイス検出
│   ├── handlers/    # リクエストハンドラー
│   ├── models/      # データモデル
│   └── utils/       # ユーティリティ関数
├── static/       # 静的リソース
└── templates/    # テンプレートファイル
```

## 開発計画

- [x] テキスト表示対応の送信機能強化
- [x] TUIリフレッシュの最適化
- [ ] 完全な国際化対応
- [x] 転送進捗表示の改善
- [ ] ファイル転送の再開機能

## 貢献

IssueやPull Requestを歓迎します。貢献する際は以下の点に注意してください：

1. Goのコード規約に従う
2. 必要なテストを追加
3. 関連ドキュメントを更新
4. コードをクリーンで明確に保つ

## ライセンス

このプロジェクトは[MIT](../LICENSE)ライセンスの下で公開されています。

## Star History

<div align="center">
    <a href="https://star-history.com/#meowrain/localsend-go&Date">
        <img src="https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date" width="80%" />
    </a>
</div>
