# zengin-api

全銀フォーマット（金融機関コード・支店コード）を JSON API として提供する Go 製サービスです。
標準ライブラリのみで書かれた単一バイナリで、`serve`（APIサーバー）と `update`（データ更新）の
2つのサブコマンドを持ちます。

## 構成

```
zengin-api/
├── cmd/zengin-api/       # エントリポイント（serve / update / healthcheck サブコマンド）
├── internal/
│   ├── zengindata/        # データ取得(source.go)・保持(store.go)・定期再読込(watch.go)
│   ├── httpapi/            # HTTPハンドラ・ルーティング
│   └── logging/            # log/slog によるJSON構造化ログ設定
├── docker/
│   ├── app.Dockerfile      # マルチステージビルド
│   ├── compose.build.yaml  # イメージビルド専用
│   ├── compose.yaml        # 本番起動用（UNIXソケット経由でCaddyにリバースプロキシ）
│   └── entrypoint.sh
├── docker-compose.yml      # ローカル動作確認用（TCPポート直接公開）
├── systemd/                 # 外側から update をキックするsystemd timerの例
└── sample/                  # 移行前のPython/Datasette版の試作（参考・未使用）
```

## データソースの取得方針

データは [zengin-code/source-data](https://github.com/zengin-code/source-data) から取得します。
このリポジトリは GitHub Releases を作っておらず、GitHub Actions が毎日 `master` ブランチ上の
`data/*.json` を直接更新する運用（README のバッジ参照）です。そのため:

- **git clone/pull で追随せず、tarball アーカイブ (`codeload.github.com/.../tar.gz/refs/heads/master`)
  を毎回まるごと取得する方式にしました。** git バイナリへの依存や `.git` ディレクトリの永続化が不要になり、
  「取得して展開し、成功したらアトミックに書き換える」だけの冪等な操作にできます。
- 展開したデータは `DATA_DIR/banks.json` と `DATA_DIR/branches/{bank_code}.json` にJSON配列として
  書き出します（元データの `{code: {...}}` 形式を配列に正規化）。取得元の更新日時は
  `DATA_DIR/updated_at` に保存し、`/healthz` で確認できます。

## 定期実行について

アプリ自身はcronを内蔵しません（SPEC参照：定期実行は外側からキックする前提）。
`zengin-api update` を実行すると1回だけデータ取得・書き出しを行って終了するので、
systemd timer やジョブスケジューラから都度呼び出してください。`systemd/` に日次02:00実行の例を
置いています。

`serve` プロセスは別プロセス（`update`）が書き換えたファイルを直接は検知できないため、
`RELOAD_INTERVAL`（デフォルト5分）ごとにディスクを読み直してインメモリのデータを差し替えます。
zengin-code のデータ更新頻度（日次）に対して十分な追随速度です。

初回起動時にデータが存在しない場合は、`docker/entrypoint.sh` が `serve` の前に自動で
`update` を1回実行します（Dockerでの利用時のみ。ローカルで直接バイナリを叩く場合は
先に `zengin-api update` を実行してください）。

## API エンドポイント

| エンドポイント | 内容 |
|---|---|
| `GET /healthz` | ヘルスチェック（データ取得元の更新日時を含む） |
| `GET /v1/banks` | 金融機関一覧（`?q=` でcode/name/kana/hira/romaの部分一致検索） |
| `GET /v1/banks/{code}` | 金融機関コードで1件取得（未存在は404） |
| `GET /v1/banks/{code}/branches` | 指定金融機関の支店一覧（`?q=` で部分一致検索） |
| `GET /v1/banks/{code}/branches/{branchCode}` | 支店コードで1件取得（未存在は404） |
| `GET /v1/branches` | 全支店（`?bank_code=` で絞り込み、`?q=` で部分一致検索） |

レスポンスは常にJSON。金融機関は `{code, name, kana, hira, roma}`、支店は
`{bank_code, code, name, kana, hira, roma}` の形式です。読み取り専用の公開マスタデータAPIとして
すべてのOriginからのGETを許可しています（CORS）。

### 既存Laravelアプリとの対応

`iw-michinoku` の `BankController` が内部DB（`m_banks` / `m_bank_brunches`）に対して行っている
以下の操作は、このAPIで代替できます。

- `BankController@list`（全銀行取得）→ `GET /v1/banks`
- `BankController@brunches`（`parent_id` 指定で支店取得）→ `GET /v1/banks/{code}/branches`

`BankController@update`（YAMLからDBへの取り込み）は、Laravel側でこのAPIを定期的に叩いて
`m_banks` / `m_bank_brunches` を更新するように置き換えられます（未実装・今回のスコープ外）。

## ローカルでの動作確認

```bash
go run ./cmd/zengin-api update   # DATA_DIR (デフォルト ./data) にデータ取得
go run ./cmd/zengin-api serve    # ADDR (デフォルト :8080) でAPIサーバー起動
```

```bash
curl http://localhost:8080/v1/banks/0001
curl http://localhost:8080/v1/banks/0001/branches
```

Dockerで試す場合:

```bash
# コンテナ内は uid 1000 (appuser) で動くため、bind mount先を先に作って権限を合わせておく
# (rootが自動作成すると書き込み権限エラーになる)
mkdir -p data && sudo chown -R 1000:1000 data

docker compose up -d --build
curl http://localhost:8080/v1/banks?q=みずほ
```

## 本番デプロイ

Growstar標準のDocker構成（ホスト側Caddyへのリバースプロキシ、bind mount永続化）に従います。
GoアプリのためTCPポートではなくUNIXドメインソケットでCaddyと通信し、`caddy_net` への参加は
不要です。

```bash
# ビルド
docker compose -f docker/compose.build.yaml build

# ホスト側の事前準備
mkdir -p /srv/sockets/zengin-api
mkdir -p /srv/containers/docker/zengin-api/app-data
chown -R 1000:1000 /srv/containers/docker/zengin-api/app-data

# 起動
docker compose -f docker/compose.yaml up -d

# 初回データ取得（entrypointが自動実行しない場合や手動再取得したい場合）
docker compose -f docker/compose.yaml run --rm --no-deps app update
```

Caddyfile側では以下のように設定してもらいます。

```
example.com {
    reverse_proxy unix//srv/sockets/zengin-api/app.sock
}
```

定期更新は `systemd/zengin-api-update.{service,timer}` をホストに配置し、
`systemctl enable --now zengin-api-update.timer` で有効化してください。

## 環境変数

| 変数 | デフォルト | 内容 |
|---|---|---|
| `DATA_DIR` | `./data`（Docker内は`/app/data`） | データ格納ディレクトリ |
| `ADDR` | `:8080` | TCPリッスンアドレス（`SOCKET_PATH`未設定時のみ使用） |
| `SOCKET_PATH` | 未設定 | UNIXドメインソケットのパス（設定時はこちらを優先） |
| `RELOAD_INTERVAL` | `5m` | `serve`がディスクを再読込する間隔 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
