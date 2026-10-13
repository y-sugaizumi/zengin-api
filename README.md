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
│   │   └── assets/          # /openapi.json /docs /ai/* で配信する静的ドキュメント(go:embed)
│   └── logging/            # log/slog によるJSON構造化ログ設定
├── Taskfile.yml            # ビルド・デプロイ・開発用タスク（`task --list` で一覧）
├── infra/caddy/site.caddy  # Caddyのサイト定義のテンプレート（`task caddy:install` で配置）
├── docs/
│   └── upgrade-2026-10-taskfile.md  # Makefile運用からの移行手順（運用中ホスト向け）
├── deploy/
│   ├── install.sh                 # ホストへのインストール/再デプロイスクリプト（冪等）
│   ├── uninstall.sh               # install.sh で入れたものを取り除く（`--purge` でデータも）
│   ├── zengin-api-cli.sh          # /usr/local/bin/zengin-api に置く管理コマンドの入口
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
systemd timer やジョブスケジューラから都度呼び出してください。`deploy/zengin-api-update.timer`
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

### ドキュメント/ディスカバリエンドポイント

このAPIと連携するアプリを実装する際、別ワークスペースのこのREADMEを読みに行かなくても、
API自身が実装に必要な情報を返せるようにしている。

| エンドポイント | 内容 |
|---|---|
| `GET /` | 全ルートへのリンクを一覧表示する概要ページ（ルートパス完全一致のみ。未定義パスは通常通り404） |
| `GET /openapi.json` | OpenAPI 3.0仕様 |
| `GET /docs` | Swagger UI（`/openapi.json`を描画する人間向けページ） |
| `GET /ai` | 概要・エンドポイント一覧・他の`/ai/*`へのリンクを含むインデックス |
| `GET /ai/concepts` | ドメイン概念（bank/branchの定義、code/name/kana/hira/romaの意味、データ鮮度、エラー形式、検索仕様等） |
| `GET /ai/guides` | ユースケース別の実装手順（コード→名称解決、オートコンプリート、支店選択、更新検知等） |
| `GET /ai/examples` | 各エンドポイントのcurlコマンドとレスポンス例 |

`/ai/*`はコード生成・実装補助を行うAIエージェント（Claude Code等）が、このAPIと連携する
アプリを別ワークスペースで実装する際に、READMEを横断参照する代わりに読む想定のエンドポイント。
内容は `internal/httpapi/assets/` にJSON/HTMLとして置き、`go:embed` でバイナリに埋め込んでいる。
APIの仕様を変えたときはこれらのファイルも合わせて更新すること。

## ローカルでの動作確認

よく使う操作は `Taskfile.yml`（[go-task](https://taskfile.dev/)）にまとめてあります。一覧は `task --list`。
`dev` / `update` / `test` / `lint` / `fmt` は `growstar/golang-builder` イメージのコンテナで `go` を動かすので、
開発機にGoは要りません（モジュール・ビルドのキャッシュはリポジトリ内の `.cache/` に置きます）。

```bash
task update   # ./data にデータ取得 (go run update相当)
task dev      # 127.0.0.1:8080 でAPIサーバー起動 (go run serve相当。データが無ければ起動時に取得)
task test     # go test（task test -- -run TestXxx で引数を渡す）
task lint     # go vet と gofmt の差分確認
```

```bash
curl http://localhost:8080/v1/banks/0001
curl http://localhost:8080/v1/banks/0001/branches
```

## ビルド

Goツールチェインをホストに入れなくても、[`growstar-jp/docker-compose-collection`](https://github.com/growstar-jp/docker-compose-collection)
の `tooling/golang-build` コンテナで静的バイナリをビルドできます。既定では
`$HOME/repos/internal/docker-compose-collection` にそのリポジトリがある前提です
（`task build GOLANG_BUILD_DIR=...` で変更可）。

```bash
task build
# => bin/zengin-api （linux/amd64, CGO_ENABLED=0 の静的バイナリ。所有者は実行したユーザー）
```

ホストにGoが入っていれば `task build:local` でも同じものがビルドできます。

## 本番デプロイ

コンテナは使わず、`task build` で作った `bin/zengin-api` をそのままホストに配置し、systemdで
動かします。Caddyへの接続は他のGrowstarアプリと同じくUNIXドメインソケット経由です。

> Makefile時代（2026-09）からの移行は [`docs/upgrade-2026-10-taskfile.md`](docs/upgrade-2026-10-taskfile.md) を参照。

前提（対象ホスト）:

- go-task（`task`）が入っていること
- [`growstar-jp/docker-compose-collection`](https://github.com/growstar-jp/docker-compose-collection) が
  `$HOME/repos/internal/docker-compose-collection` にあること（ビルド用の `tooling/golang-build` と、
  Caddyのサイト定義を配置する `scripts/caddy-site.sh` を使う）
- Caddyのコンテナに `/srv/sockets/zengin-api:/srv/sockets/zengin-api:ro` がマウントされ、
  `Caddyfile` が `import apps/*.caddy` していること

```bash
task build           # bin/zengin-api をビルド
task deploy          # = sudo ./deploy/install.sh（確認プロンプトあり）
task caddy:install   # infra/caddy/site.caddy を Caddy の apps/zengin-api.caddy に配置して reload（初回とサイト定義の変更時）
```

`deploy/install.sh` がやること（`deploy/*.service` `deploy/*.timer` `deploy/*.env.example` を参照）。
初回と再デプロイのどちらにも同じスクリプトを使えます（冪等。既存の `/etc/zengin-api/env` は上書きしません）:

1. システムユーザー `zengin-api` を作成（未作成の場合のみ）
2. `/opt/zengin-api/bin` `/var/lib/zengin-api` `/srv/sockets/zengin-api` `/etc/zengin-api` を用意
3. `/etc/zengin-api/env` が無ければ `deploy/zengin-api.env.example` から作成（既存なら上書きしない）
4. `bin/zengin-api` を `/opt/zengin-api/bin/zengin-api` に、`deploy/zengin-api-cli.sh` を
   `/usr/local/bin/zengin-api` に配置
5. `deploy/*.service` `deploy/*.timer` を `/etc/systemd/system/` に配置して `daemon-reload`
6. `zengin-api.service` を再起動、`zengin-api-update.timer` を有効化
7. `/healthz` が返るまで待つ（返らなければ直近のログを出して終了コード1。初回はデータ取得のため数十秒かかる）

### 配置先

| パス | 内容 |
|---|---|
| `/opt/zengin-api/bin/zengin-api` | バイナリ（PATHには入れない） |
| `/usr/local/bin/zengin-api` | 管理コマンドの入口（下記） |
| `/etc/zengin-api/env` | 環境変数（`EnvironmentFile=`） |
| `/var/lib/zengin-api/` | データ（`banks.json` `branches/` `updated_at`） |
| `/srv/sockets/zengin-api/app.sock` | Caddyとの通信用UNIXソケット |
| `/etc/systemd/system/zengin-api*.{service,timer}` | systemd unit |
| Caddyの `apps/zengin-api.caddy`（`/srv/containers/docker/caddy/caddy_files/apps/`） | `task caddy:install` が配置するサイト定義。直接編集しない |

### 管理コマンド

`/usr/local/bin/zengin-api` は、サービスと同じ `zengin-api` ユーザー・`/etc/zengin-api/env` の設定で
バイナリを実行するだけのスクリプトです（`serve` は systemd の管轄なので拒否します）。

```bash
zengin-api update        # データを今すぐ取得し直す（serveはRELOAD_INTERVAL以内に取り込む）
zengin-api healthcheck   # ソケット越しに /healthz を確認する
```

### Caddyのサイト定義

正本は `infra/caddy/site.caddy` で、ドメインの既定値は `zengin.growstar-jp.com`（`import internal_only`
で接続元を制限）。別のドメインで動かすときだけ、リポジトリ直下の `.env` に `CADDY_DOMAIN=...` を書きます。
配置内容の確認は `task caddy:show`。

### 削除

```bash
task uninstall         # Caddyのサイト定義・unit・バイナリ・/etc/zengin-api を削除（データとユーザーは残す）
task uninstall:purge   # 上記に加えてデータ /var/lib/zengin-api とユーザーも削除
```

## ログ

`log/slog` でJSONをstdoutに出力するだけで、ファイル出力やアプリ側でのログローテーションは
行わない。systemd管理下ではstdoutは自動でjournalに入るため、`task logs`
（= `journalctl -u zengin-api.service -u zengin-api-update.service -f`）で確認できる。`task logs` は
`sudo` を付けないので、実行するユーザーを `systemd-journal` グループに入れておく
（`sudo usermod -aG systemd-journal <ユーザー>` して再ログイン）。journalの保持・ローテーションはホスト共通のjournald設定に従う（このリポジトリでは
何も設定しない）。

> **TODO**: 2026-09時点でホストのログ収集基盤(`docker-compose-collection/stacks/observability`)は
> PromtailがDockerコンテナのログしかスクレイプしておらず、journald→Lokiの経路が無い。整備するまでは
> このアプリのログはLokiで検索できず、`journalctl`でのローカル確認のみになる
> (詳細は `growstar-docker-deploy` skillの `references/go-binary-deploy.md` 参照)。

## 環境変数

| 変数 | デフォルト | 内容 |
|---|---|---|
| `DATA_DIR` | `./data` | データ格納ディレクトリ（本番は`/var/lib/zengin-api`） |
| `ADDR` | `:8080` | TCPリッスンアドレス（`SOCKET_PATH`未設定時のみ使用、主にローカル用） |
| `SOCKET_PATH` | 未設定 | UNIXドメインソケットのパス（設定時はこちらを優先、本番用） |
| `RELOAD_INTERVAL` | `5m` | `serve`がディスクを再読込する間隔 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
