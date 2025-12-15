# ElasticRelay - Elasticsearch へのマルチソース CDC ゲートウェイ

![ElasticRelay Screenshot](/releases/download/asset/screenshot_02.png)

<p align="center">
  <a href="https://github.com/yogoosoft/ElasticRelay/releases"><img src="https://img.shields.io/badge/version-v1.3.1-blue.svg" alt="バージョン"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.25.2+-00ADD8.svg" alt="Go バージョン"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-green.svg" alt="ライセンス"></a>
</p>
<p align="center">
  <a href="/README.md">English</a> |
  <a href="README.de.md">Deutsch</a> |
  <a href="README.fr.md">Français</a> |
  <a href="README.ja.md">日本語</a> |
  <a href="README.ru.md">Русский</a> |
  <a href="README.zh-CN.md">中文</a>
</p>

## ビジョン

ElasticRelay は、主要な OLTP データベース（MySQL、PostgreSQL、MongoDB）から Elasticsearch へのリアルタイム Change Data Capture (CDC) を提供するために設計された、シームレスで異種データ同期ツールです。Logstash や Flink などの既存のソリューションよりも使いやすく、信頼性が高いことを目指しています。

## 🎉 v1.3.1 ハイライト - マルチソース CDC プラットフォーム

**3つの主要なデータベースソースを完全サポート:**

| ソース | ステータス | 機能 |
|--------|--------|----------|
| **MySQL** | ✅ 完了 | Binlog CDC + 初期同期 + 並列スナップショット |
| **PostgreSQL** | ✅ 完了 | 論理レプリケーション + WAL パース + LSN 管理 |
| **MongoDB** | ✅ 完了 | Change Streams + シャードクラスター + Resume Tokens |

## 主な機能

- **マルチソース CDC**: MySQL、PostgreSQL、MongoDB のリアルタイム変更キャプチャを完全サポート
- **ゼロコード設定**: ウィザードスタイルの GUI を備えた JSON ベースの設定（開発中）
- **マルチテーブル動的インデックス作成**: 設定可能な命名パターンで、各ソーステーブルに個別の Elasticsearch インデックスを自動作成（例: `elasticrelay-users`、`elasticrelay-orders`）
- **組み込みガバナンス**: データ構造化、匿名化、型変換、正規化、エンリッチメントを処理
- **デフォルトで信頼性**: トランザクションログレベルの CDC、再開のための正確なチェックポイント、データ整合性を確保するための冪等書き込みを活用
- **Dead Letter Queue (DLQ)**: 指数バックオフリトライと永続ストレージによる包括的な障害処理
- **並列処理**: 大規模テーブル向けのチャンキング戦略を備えた高度な並列スナップショット処理

## 技術スタック

- **データプレーン (Go)**: コアのデータ同期ロジックは Go (1.25.2+) で構築されており、高い並行性、低いメモリフットプリント、シンプルなデプロイメントを実現。
- **コントロールプレーン & GUI (TypeScript/Next.js)**: 設定とモニタリングのためのリッチでインタラクティブな UI（開発中）。
- **API (gRPC)**: 完全なサービス実装による高性能な gRPC を介したコンポーネント間の内部通信。
- **データベースサポート**: 
  - **MySQL CDC**: リアルタイム同期による高度な binlog パース（go-mysql ライブラリ）
  - **PostgreSQL CDC**: WAL パース、レプリケーションスロット、パブリケーションによる論理レプリケーション
  - **MongoDB CDC**: レプリカセットとシャードクラスターサポートによる Change Streams（mongo-driver）
- **Elasticsearch 統合**: バルクインデックスサポート付きの公式 Elasticsearch Go クライアント (v8)
- **設定**: 自動フォーマット検出と移行を備えた JSON ベースの設定
- **信頼性**: 包括的なエラー処理、DLQ システム、チェックポイント管理

## アーキテクチャ

システムはいくつかの主要コンポーネントで構成されています:

- **ソースコネクター**: ソースデータベースから変更をキャプチャ。
- **永続バッファー**: ソースとシンクを分離し、リプレイを可能にする永続バッファー。
- **変換 & ガバナンスエンジン**: データ変換ルールを実行。
- **ES シンクライター**: Elasticsearch に効率的なバッチでデータを書き込み。
- **オーケストレーター**: 同期タスクのライフサイクルを管理。
- **コントロールプレーン**: UI と設定管理バックエンド。

## クイックスタート

ElasticRelay を迅速に起動するには、以下の3つの簡単なステップに従ってください:

### ステップ 1: ビルド
```sh
./scripts/build.sh
```

### ステップ 2: 設定

#### MongoDB セットアップ（MongoDB CDC に必須）
MongoDB は Change Streams のためにレプリカセットモードが必要です。セットアップスクリプトを実行してください:
```sh
./scripts/reset-mongodb.sh
```

または手動で:
```sh
docker-compose down
rm -rf ./data/mongodb/*
docker-compose up -d mongodb
docker-compose up mongodb-init
```

MongoDB の準備ができているか確認:
```sh
./scripts/verify-mongodb.sh
```

📚 **参照**: MongoDB セットアップの詳細な手順については `QUICKSTART.md` をご覧ください。

#### PostgreSQL セットアップ
PostgreSQL では、論理レプリケーションが有効になっていることを確認してください:
```sql
-- postgresql.conf で論理レプリケーションを有効化
wal_level = logical
max_replication_slots = 10
max_wal_senders = 10

-- レプリケーション権限を持つユーザーを作成
CREATE USER elasticrelay_user WITH LOGIN PASSWORD 'password' REPLICATION;
GRANT CONNECT ON DATABASE your_database TO elasticrelay_user;
GRANT USAGE ON SCHEMA public TO elasticrelay_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO elasticrelay_user;
```

#### 設定ファイル
設定ファイル `./config/parallel_config.json` を編集し、データベースと Elasticsearch の接続情報が正しいことを確認してください。

### ステップ 3: 実行
```sh
./start.sh
```

これらのステップを完了すると、ElasticRelay はデータベースの変更を監視し、Elasticsearch に同期を開始します。

---

## 実行方法

### 前提条件

- Go (1.25.2+)
- Protobuf コンパイラー (`protoc`)
- Elasticsearch (7.x または 8.x)
- **MySQL** (5.7+ または 8.x) binlog 有効
- **PostgreSQL** (10+ 推奨、9.4+ 最小) 論理レプリケーション有効
- **MongoDB** (4.0+) レプリカセットまたはシャードクラスター設定

### インストール

1.  **Go 依存関係とツールをインストール**:
    ```sh
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
    ```

2.  **`protoc` をインストール**:
    macOS で Homebrew を使用:
    ```sh
    brew install protobuf
    ```

3.  **依存関係を整理**:
    ```sh
    go mod tidy
    ```

### サーバーのビルドと実行

#### クイックビルド（開発用）
```sh
# バージョン情報なしの簡易ビルド
go build -o elasticrelay ./cmd/elasticrelay

# サーバーを実行
./elasticrelay -config multi_config.json
```

#### 本番ビルド（推奨）
```sh
# Makefile でバージョン情報付きビルド
make build

# バージョン付きバイナリを実行
./bin/elasticrelay -config multi_config.json
```

#### バージョン管理
ElasticRelay はビルド時インジェクションによる包括的なバージョン管理を備えています:

```sh
# 詳細なビルド情報付きの現在のバージョン情報を表示
./bin/elasticrelay -version

# Makefile からバージョン情報を確認
make version

# 開発ビルド（高速、バージョンインジェクションなし）
make dev

# 本番ビルド（バージョン情報付き最適化）
make release

# 複数アーキテクチャ向けクロスプラットフォームビルド
make build-all

# カスタムバージョンでビルド
VERSION="v1.3.0" make build

# マイグレーションユーティリティを含むすべてのツールをビルド
make build-tools
```

バージョンシステムには以下が含まれます:
- **Git 統合**: git タグからの自動バージョン検出
- **ビルドメタデータ**: コミットハッシュ、ビルド時間、Go バージョン、プラットフォーム情報
- **カラー出力**: バージョン詳細と ASCII アートロゴ付きのリッチなコンソール出力
- **クロスプラットフォーム**: Linux、macOS (Intel/ARM)、Windows のサポート

サーバーはデフォルトでポート `50051` で起動してリッスンします。

**代替方法**: ビルドせずに直接実行することもできます:
```sh
go run ./cmd/elasticrelay -config multi_config.json
```

### マルチテーブル設定

ElasticRelay は、レガシーの単一設定と自動検出・移行付きのモダンなマルチ設定フォーマットの両方をサポートしています。

#### モダンなマルチ設定フォーマット (`multi_config.json`):

```json
{
  "version": "3.0",
  "data_sources": [
    {
      "id": "mysql-main",
      "type": "mysql",
      "host": "localhost",
      "port": 3306,
      "user": "elastic_user",
      "password": "password",
      "database": "elasticrelay",
      "server_id": 100,
      "table_filters": ["users", "orders", "products"]
    },
    {
      "id": "postgresql-main",
      "type": "postgresql",
      "host": "localhost",
      "port": 5432,
      "user": "elastic_user",
      "password": "password",
      "database": "elasticrelay",
      "table_filters": ["users", "orders", "products"],
      "options": {
        "ssl_mode": "disable",
        "slot_name": "elasticrelay_slot",
        "publication_name": "elasticrelay_publication",
        "batch_size": 1000,
        "max_connections": 10,
        "parallel_snapshots": true
      }
    },
    {
      "id": "mongodb-main",
      "type": "mongodb",
      "host": "localhost",
      "port": 27017,
      "user": "elasticrelay_user",
      "password": "password",
      "database": "elasticrelay",
      "table_filters": ["users", "orders", "products"],
      "options": {
        "auth_source": "admin",
        "replica_set": "rs0"
      }
    }
  ],
  "sinks": [
    {
      "id": "es-main",
      "type": "elasticsearch",
      "addresses": ["http://localhost:9200"],
      "options": {
        "index_prefix": "elasticrelay"
      }
    }
  ],
  "jobs": [],
  "global": {
    "log_level": "info",
    "grpc_port": 50051,
    "dlq_config": {
      "enabled": true,
      "storage_path": "dlq",
      "max_retries": 3,
      "retry_delay": "30s"
    }
  }
}
```

#### レガシー設定フォーマット (`config.json`):

```json
{
  "db_host": "localhost",
  "db_port": 3306,
  "db_user": "elastic_user",
  "db_password": "password",
  "db_name": "elasticrelay",
  "server_id": 100,
  "table_filters": ["users", "orders", "products"],
  "es_addresses": ["http://localhost:9200"]
}
```

システムは設定フォーマットを自動的に検出し、フォーマット間の移行をサポートします。これにより個別のインデックスが作成されます:
- `users` テーブル用の `elasticrelay-users`
- `orders` テーブル用の `elasticrelay-orders`  
- `products` テーブル用の `elasticrelay-products`

### Dead Letter Queue (DLQ) サポート

ElasticRelay には、失敗したイベントを処理するための包括的な DLQ システムが含まれています:

- **自動リトライ**: 失敗したイベントは指数バックオフで自動的にリトライ
- **永続ストレージ**: DLQ アイテムは完全な状態管理でディスクに永続化
- **重複排除**: 重複イベントがキューに追加されるのを防止
- **ステータス追跡**: 完全なライフサイクル追跡（保留、リトライ中、使い果たし、解決済み、破棄済み）
- **手動管理**: 手動でのアイテム検査と管理をサポート
- **自動クリーンアップ**: 解決済みアイテムは設定可能な期間後に自動的にクリーンアップ

### PostgreSQL サポート

ElasticRelay は高度な機能を備えた包括的な PostgreSQL CDC 機能を提供します:

#### コア PostgreSQL 機能
- **論理レプリケーション**: `pgoutput` プラグインによる PostgreSQL のネイティブ論理レプリケーションを使用
- **WAL パース**: リアルタイム変更キャプチャのための高度な Write-Ahead Log パース
- **レプリケーションスロット**: 論理レプリケーションスロットの自動作成と管理
- **パブリケーション**: テーブルフィルタリングのための動的パブリケーション管理
- **LSN 管理**: チェックポイント/再開機能のための正確な Log Sequence Number 追跡

#### 高度な PostgreSQL 機能
- **コネクションプーリング**: 設定可能な制限を持つインテリジェントなコネクションプール管理
- **並列スナップショット**: チャンキング戦略によるマルチスレッド初期データ同期
- **型マッピング**: 以下を含む包括的な PostgreSQL から Elasticsearch への型変換:
  - すべての数値型（bigint、integer、real、double、numeric）
  - テキストと文字型（text、varchar、char）
  - タイムゾーンサポート付きの日付/時刻型（timestamp、timestamptz、date、time）
  - ネイティブオブジェクトマッピングによる JSON/JSONB
  - 配列型（integer 配列、text 配列）
  - 高度な型（UUID、bytea、inet、幾何型）
- **パフォーマンス最適化**: 
  - 大規模テーブル向けの適応型スケジューリング
  - メモリ効率のためのストリーミングモード
  - 設定可能なバッチサイズとワーカープール
  - コネクションライフサイクル管理

#### PostgreSQL 設定オプション
```json
{
  "type": "postgresql",
  "options": {
    "ssl_mode": "disable|require|verify-ca|verify-full",
    "slot_name": "custom_replication_slot_name",
    "publication_name": "custom_publication_name",
    "batch_size": 1000,
    "max_connections": 10,
    "min_connections": 2,
    "parallel_snapshots": true,
    "enable_performance_monitoring": true
  }
}
```

### MongoDB サポート

ElasticRelay は Change Streams を使用した完全な MongoDB CDC 機能を提供します:

#### コア MongoDB 機能
- **Change Streams**: MongoDB のネイティブ Change Streams API を使用したリアルタイム CDC
- **クラスターサポート**: レプリカセットとシャードクラスターの自動検出とサポート
- **Resume Tokens**: チェックポイント/再開機能のための永続的な resume token 管理
- **操作マッピング**: INSERT、UPDATE、REPLACE、DELETE 操作の完全サポート

#### 高度な MongoDB 機能
- **シャードクラスターサポート**: 
  - mongos 経由のマルチシャード監視
  - チャンク移行中の一貫性のためのマイグレーション認識
  - チャンク分散監視
- **型変換**: 完全な BSON から JSON 対応型への変換:
  - ObjectID → 文字列（16進形式）
  - DateTime → RFC3339 タイムスタンプ
  - Decimal128 → 文字列（精度保持）
  - Binary → base64 エンコード
  - 設定可能なフラット化深度を持つネストされたドキュメント
- **並列スナップショット**: 
  - 標準コレクション用の ObjectID ベースチャンキング
  - 整数主キー用の数値 ID ベースチャンキング
  - 複雑な ID タイプ用の Skip/Limit フォールバック

#### MongoDB 設定オプション
```json
{
  "type": "mongodb",
  "host": "localhost",
  "port": 27017,
  "user": "elasticrelay_user",
  "password": "password",
  "database": "your_database",
  "options": {
    "auth_source": "admin",
    "replica_set": "rs0",
    "read_preference": "primaryPreferred",
    "batch_size": 1000,
    "flatten_depth": 3
  }
}
```

#### MongoDB セットアップ要件
```sh
# MongoDB は Change Streams のためにレプリカセットモードで実行する必要があります
# 提供されているセットアップスクリプトを使用:
./scripts/reset-mongodb.sh

# または Docker Compose で:
docker-compose up -d mongodb
docker-compose up mongodb-init

# レプリカセットが設定されていることを確認:
./scripts/verify-mongodb.sh
```

### 並列処理

高度な並列スナップショット処理機能:

- **チャンキング戦略**: ID ベース、時間ベース、ハッシュベースのチャンキングをサポート
- **ワーカープール**: 適応型スケジューリング付きの設定可能なワーカープールサイズ
- **進捗追跡**: リアルタイム進捗監視と統計
- **大規模テーブルサポート**: インテリジェントなチャンキングによる大規模テーブルの最適化された処理
- **ストリーミングモード**: 大規模データセット向けのメモリ効率的なストリーミング処理

## 現在のステータス

**現在のバージョン**: v1.3.1 | **フェーズ**: フェーズ 2 完了 ✅、フェーズ 3 に移行中

このプロジェクトはコアとなるマルチソース CDC プラットフォーム（フェーズ 2）を完了し、エンタープライズグレードの強化に向けて準備中です。

### ✅ 完了した機能（フェーズ 2 - v1.3.1）
- **マルチソース CDC パイプライン**: 
  - **MySQL CDC**: binlog ベースのリアルタイム同期による完全な実装
  - **PostgreSQL CDC**: WAL パース、レプリケーションスロット、パブリケーションによる完全な論理レプリケーション
  - **MongoDB CDC**: レプリカセットとシャードクラスターサポートによる完全な Change Streams 実装
- **マルチテーブル動的インデックス作成**: 設定可能な命名によるテーブルごとの自動 Elasticsearch インデックス作成と管理
- **gRPC アーキテクチャ**: 完全なサービス定義と実装（Connector、Orchestrator、Sink、Transform、Health）
- **高度な設定管理**: 
  - レガシー移行サポート付きのマルチソース設定システム
  - 設定同期とホットリロード機能
  - 自動フォーマット検出と移行ツール
- **Elasticsearch 統合**: 自動インデックス管理とデータクリーニング付きの高性能バルク書き込み
- **チェックポイント/再開**: 自動復旧によるフォールトトレランスのための永続的位置追跡（binlog、LSN、resume tokens）
- **データ変換**: データ処理とガバナンスのための完全なパイプライン（パススルー、完全なエンジンはフェーズ 3 で）
- **Dead Letter Queue (DLQ)**: 
  - 指数バックオフリトライ付きの包括的な DLQ システム（設定可能な最大リトライ回数）
  - 重複排除とステータス追跡付きの永続ストレージ
  - 解決済みアイテムの自動クリーンアップ
  - 手動アイテム管理と検査のサポート
- **並列処理**: 
  - チャンキング戦略による高度な並列スナップショット処理
  - 設定可能なワーカープールと適応型スケジューリング
  - 進捗追跡と統計収集
  - 大規模テーブル最適化のサポート（MySQL、PostgreSQL、MongoDB）
- **バージョン管理**: ビルド時メタデータ付きの完全なバージョンインジェクションシステム
- **堅牢なエラー処理**: フォールバックメカニズム付きの包括的なエラー処理
- **ログレベル制御**: 集中管理によるランタイム設定可能なログ

### 🚧 進行中（フェーズ 3 - v1.0-beta）
- **変換エンジン**: 完全なデータ変換実装（フィールドマッピング、型変換、式、マスキング）
- **Prometheus メトリクス**: メトリクスエクスポート付きの完全なオブザーバビリティ
- **HTTP REST API**: OpenAPI ドキュメント付きの grpc-gateway 統合
- **ヘルスチェック強化**: Kubernetes 対応の readiness/liveness プローブ

### 📋 今後予定（フェーズ 4+）
- **フロントエンド開発**: コントロールプレーン GUI（TypeScript/Next.js）
- **高可用性**: 自動フェイルオーバー付きのマルチレプリカデプロイメント
- **セキュリティ強化**: mTLS、RBAC、監査ログ
- **高度なガバナンス**: リッチなデータ変換ルールとフィールドレベルのガバナンス

---

## 📄 ライセンス

ElasticRelay は [Apache License 2.0](LICENSE) の下でライセンスされています。

```
Copyright 2024 上海悦高软件股份有限公司 (Yogoo Software Co., Ltd.)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## 🤝 コントリビューション

コントリビューションを歓迎します！詳細については [コントリビューションガイドライン](CONTRIBUTING.md) をご覧ください。

## 📞 サポート

- 🐦 X (Twitter): [@ElasticRelay](https://x.com/ElasticRelay)
- 🌐 公式ウェブサイト: [www.elasticrelay.com](http://www.elasticrelay.com)
- 📧 メール: support@yogoo.net
- 💬 コミュニティ: [GitHub Discussions](https://github.com/yogoosoft/ElasticRelay/discussions)
- 🐛 バグレポート: [GitHub Issues](https://github.com/yogoosoft/ElasticRelay/issues)
- 📖 ドキュメント: [docs.elasticrelay.com](https://docs.elasticrelay.com)
