# zengin-api

全銀フォーマット（金融機関コード・支店コード）を JSON API として提供する Go 製サービスです。
標準ライブラリのみで書かれた単一バイナリで、`serve`（APIサーバー）・`update`（データ更新）・
`healthcheck`（外形監視用）の3つのサブコマンドを持ちます。

**デプロイはコンテナ化せず、ビルド済みバイナリをそのままホストに配置してsystemdで動かします。**
Dockerはビルド時（[`golang-build`](#ビルド) コンテナでのクロスコンパイル）にのみ使い、
実行環境には登場しません。

## 構成

```
zengin-api/
├── cmd/zengin-api/       # エントリポイント（serve / update / healthcheck サブコマンド）
├── internal/
│   ├── zengindata/        # データ取得(source.go)・保持(store.go)・定期再読込(watch.go)
│   ├── httpapi/            # HTTPハンドラ・ルーティング
│   └── logging/            # log/slog によるJSON構造化ログ設定
├── Makefile                # golang-buildコンテナ経由のビルド / ローカルビルド / 開発用タスク
├── systemd/
│   ├── zengin-api.service         # serve を常駐させるunit
│   ├── zengin-api-update.service  # update を1回実行するunit（timerから起動）
│   ├── zengin-api-update.timer    # 毎日02:00に update.service をキック
│   └── zengin-api.env.example     # /etc/zengin-api/env に配置する環境変数の例
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
systemd timer やジョブスケジューラから都度呼び出してください。`systemd/zengin-api-update.timer`
が毎日02:00実行の例です。

`serve` プロセスは別プロセス（`update`）が書き換えたファイルを直接は検知できないため、
`RELOAD_INTERVAL`（デフォルト5分）ごとにディスクを読み直してインメモリのデータを差し替えます。
zengin-code のデータ更新頻度（日次）に対して十分な追随速度です。

初回起動時にデータが存在しない場合、`serve` は起動時に自動で `update` を1回実行してから
サーバーを立ち上げます（`cmd/zengin-api/main.go` の `runServe` 参照）。

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
make update   # DATA_DIR (デフォルト ./data) にデータ取得 (go run update相当)
make run      # ADDR (デフォルト :8080) でAPIサーバー起動 (go run serve相当)
```

```bash
curl http://localhost:8080/v1/banks/0001
curl http://localhost:8080/v1/banks/0001/branches
```

## ビルド

Goツールチェインをホストに入れなくても、[`growstar-jp/docker-compose-collection`](https://github.com/growstar-jp/docker-compose-collection)
の `tooling/golang-build` コンテナで静的バイナリをビルドできます。既定では
`$HOME/repos/internal/docker-compose-collection` にそのリポジトリがある前提です
（`GOLANG_BUILD_DIR` で変更可）。

```bash
make build
# => bin/zengin-api （linux/amd64, CGO_ENABLED=0 の静的バイナリ）
```

ホストにGoが入っていれば `make build-local` でも同じものがビルドできます。

## 本番デプロイ

コンテナは使わず、`make build` で作った `bin/zengin-api` をそのままホストに配置し、systemdで
動かします。Caddyへの接続は他のGrowstarアプリと同じくUNIXドメインソケット経由です。

```bash
# ビルド（開発機やビルドサーバーで実行）
make build

# --- 対象ホストでの事前準備 ---
sudo useradd --system --home /var/lib/zengin-api --shell /usr/sbin/nologin zengin-api
sudo mkdir -p /opt/zengin-api/bin /var/lib/zengin-api /srv/sockets/zengin-api /etc/zengin-api
sudo chown -R zengin-api:zengin-api /var/lib/zengin-api /srv/sockets/zengin-api
sudo cp systemd/zengin-api.env.example /etc/zengin-api/env   # 内容を必要に応じて編集

# --- バイナリ配置 ---
sudo cp bin/zengin-api /opt/zengin-api/bin/zengin-api

# --- systemd unit配置 ---
sudo cp systemd/zengin-api.service systemd/zengin-api-update.service systemd/zengin-api-update.timer \
  /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now zengin-api.service
sudo systemctl enable --now zengin-api-update.timer
```

再デプロイ（バイナリ更新）は `cp` してから `systemctl restart zengin-api.service` するだけです。

Caddyfile側では以下のように設定してもらいます。

```
example.com {
    reverse_proxy unix//srv/sockets/zengin-api/app.sock
}
```

## 環境変数

| 変数 | デフォルト | 内容 |
|---|---|---|
| `DATA_DIR` | `./data` | データ格納ディレクトリ（本番は`/var/lib/zengin-api`） |
| `ADDR` | `:8080` | TCPリッスンアドレス（`SOCKET_PATH`未設定時のみ使用、主にローカル用） |
| `SOCKET_PATH` | 未設定 | UNIXドメインソケットのパス（設定時はこちらを優先、本番用） |
| `RELOAD_INTERVAL` | `5m` | `serve`がディスクを再読込する間隔 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
