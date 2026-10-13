# Makefile運用(2026-09)から Taskfile運用への移行

2026-09時点の構成(`make build` + `sudo ./deploy/install.sh`、Caddyのサイト定義は `caddy-files` の
`sites/zengin.caddy`)で運用中のホストを、今の構成に移すための手順と注意点。

**アプリのコード・API・systemd unit・envの中身は変わっていない。** 変わるのは「ビルド・デプロイの入口」と
「Caddyのサイト定義の置き場所」と「管理コマンドの追加」だけで、連携しているアプリ(iw-michinoku等)への影響は無い。

## 何が変わるか

| 項目 | 旧 | 新 |
|---|---|---|
| タスクランナー | `Makefile`(`make build` 等) | `Taskfile.yml`(`task build` 等)。Makefileは削除 |
| デプロイ | `sudo ./deploy/install.sh` | `task deploy`(中身は同じ `sudo ./deploy/install.sh`、確認プロンプトあり) |
| Caddyのサイト定義 | `caddy-files` の `sites/zengin.caddy`(手書き) | このリポジトリの `infra/caddy/site.caddy` を `task caddy:install` で Caddyの `apps/zengin-api.caddy` に配置 |
| 管理コマンド | 無し(`sudo -u zengin-api ... /opt/zengin-api/bin/zengin-api update` を手打ち) | `/usr/local/bin/zengin-api`(`deploy/zengin-api-cli.sh`)を新たに配置 |
| install.sh の終わり方 | 再起動したら即終了 | `/healthz` が返るまで待ち、返らなければログを出して終了コード1 |
| 削除 | 手作業 | `task uninstall` / `task uninstall:purge`(`deploy/uninstall.sh`) |
| ログ | `journalctl -u zengin-api.service` | `task logs`(sudoなし。`systemd-journal` グループが必要) |
| make のタスク名 | `build-image` / `build-local` / `run` / `vet` | `build:image` / `build:local` / `dev` / `lint` |

**変わらないもの**: `/opt/zengin-api/bin/zengin-api`、`/var/lib/zengin-api/`(データ)、`/etc/zengin-api/env`、
`/srv/sockets/zengin-api/app.sock`、systemd unit 3つ、システムユーザー `zengin-api`、ドメイン
`zengin.growstar-jp.com` と `import internal_only` による接続元制限。

## 事前確認(対象ホストで)

```bash
task --version                                   # go-task が入っているか(無ければ入れる)
ls ~/repos/internal/docker-compose-collection/scripts/caddy-site.sh   # caddy:install が使う。無ければ clone/pull
id -nG                                           # docker(caddy:install が docker exec する)と systemd-journal に入っているか
ls -la /usr/local/bin/zengin-api                 # 何も無いこと(あれば中身を確認してから進める)
ls -la bin/                                      # 旧Makefileのビルド成果物。root所有なら下の手順1で直す
grep -n 'import apps/\*.caddy' /srv/containers/docker/caddy/caddy_files/Caddyfile   # Caddyが apps/ を読むか
```

- `caddy-site.sh` は既定で `/srv/containers/docker/caddy/caddy_files/apps/` に置き、`caddy` コンテナで
  validate / reload する。Caddyがこれと違う場所・名前のホストでは、このリポジトリ直下の `.env` に
  `CADDY_APPS_DIR=...` / `CADDY_CONTAINER=...` を書く。
- 本番のドメインが `zengin.growstar-jp.com` 以外なら `.env` に `CADDY_DOMAIN=...` を書く。
  `task caddy:show` で、今の `sites/zengin.caddy` と同じ内容(ドメイン・`import access_log`・
  `import internal_only`・ソケットのパス)になっていることを確かめる。

## 手順

### 1. リポジトリを更新してビルド

```bash
cd <zengin-api のチェックアウト>
git pull
sudo chown -R "$(id -u):$(id -g)" bin   # 旧 make build はバイナリを root 所有で作っていたため(bin/ が無ければ不要)
task build
```

### 2. デプロイ

```bash
task deploy
```

- `/opt/zengin-api/bin/zengin-api` が置き換わり、`/usr/local/bin/zengin-api` が新たに置かれ、サービスが再起動する。
  データと `/etc/zengin-api/env` はそのまま使われる(初回データ取得は走らない)。
- 終わりに `/healthz` を待つので、`[install] 完了。zengin-api.service は起動しています` が出れば成功。
- 念のため: `zengin-api healthcheck && echo ok`(新しい管理コマンドの動作確認を兼ねる)。

### 3. Caddyのサイト定義を移す

**同じドメインのサイト定義が `sites/zengin.caddy` と `apps/zengin-api.caddy` の両方にあると、Caddyの
validate が失敗する。** `task caddy:install` は validate に通らなければ配置を元に戻して reload しないので
壊れはしないが、移行は完了しない。旧ファイルを先に外してから配置する。

1. `caddy-files` リポジトリで `sites/zengin.caddy` を削除してコミット・push する
   (`git rm sites/zengin.caddy`。コミットメッセージ例: `refactor: zenginのサイト定義をzengin-apiリポジトリのinfra/caddy/site.caddyに移す`)。
2. 対象ホストのCaddy設定ディレクトリで `git pull` して `sites/zengin.caddy` を消す。**ここでは reload しない**
   (reload すると、次の手順までの間 zengin.growstar-jp.com が応答しなくなる)。
3. すぐに zengin-api のチェックアウトで:
   ```bash
   task caddy:install   # apps/zengin-api.caddy を置く → validate → reload
   ```
   reload は1回だけで、旧定義から新定義へ切り替わるので停止時間は無い。
4. 社内ネットワークから確認する(`internal_only` で外部からは弾かれる):
   ```bash
   curl -s https://zengin.growstar-jp.com/healthz
   curl -s https://zengin.growstar-jp.com/v1/banks/0001
   ```

手順2と3の間にCaddyが再起動されると、その間だけ zengin.growstar-jp.com のサイト定義が無い状態になる
(他のサイトには影響しない)。手順3を実行すれば戻る。

### 4. ログを見られるようにする(任意)

```bash
sudo usermod -aG systemd-journal "$(id -un)"   # 入っていなければ。再ログイン後に task logs が使える
```

## 戻し方

- **Caddy**: `caddy-files` の `sites/zengin.caddy` を revert してホストで `git pull` し、続けて
  `task caddy:uninstall`(apps/zengin-api.caddy を外して reload)。順番を逆にすると一時的にサイトが無くなる。
- **アプリ**: コードは変わっていないので、バイナリを戻す必要は無い。`/usr/local/bin/zengin-api` が不要なら
  `sudo rm /usr/local/bin/zengin-api`。

## 注意点のまとめ

- `make` は使えなくなる(Makefileは削除した)。手順書・cron・CI等で `make build` を呼んでいる箇所があれば
  `task build` に直す。`task deploy` は確認プロンプトが出るので、非対話で呼ぶ場合は `task --yes deploy`。
- `sites/zengin.caddy` を残したまま `task caddy:install` しない(validate で弾かれて何も変わらない)。
  逆に、`sites/zengin.caddy` を消して reload だけして `task caddy:install` を忘れると、サイトが落ちる。
- `apps/zengin-api.caddy` は生成物。直接編集せず、`infra/caddy/site.caddy`(または `.env` の `CADDY_*`)を
  直して `task caddy:install` し直す。
- `task deploy` が `/healthz` を待つようになったので、起動に失敗すると終了コード1で止まる(以前は成功に見えた)。
- `/usr/local/bin/zengin-api` は PATH 上の名前なので、同名のものを別用途で置いていないか事前に確認する。
- `task uninstall` は `/etc/zengin-api` も消す(`env` を手で変えていたら控えておく)。データは残る。
