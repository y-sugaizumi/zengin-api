#!/bin/sh
# /usr/local/bin/zengin-api として配置する管理コマンドの入口(install.sh が配置する)。
# サービスと同じ zengin-api ユーザー・/etc/zengin-api/env の設定で /opt/zengin-api/bin/zengin-api を実行する。
#
#   zengin-api update        # データを今すぐ取得し直す
#   zengin-api healthcheck   # ソケット越しに /healthz を確認する
set -eu

case "${1:-}" in
  serve)
    # 動いているサービスのソケットを消して奪ってしまうので、ここからは起動させない
    echo "serve は systemd で起動します(sudo systemctl restart zengin-api.service)" >&2
    exit 2
    ;;
esac

if [ "$(id -un)" != zengin-api ]; then
  exec sudo -u zengin-api "$0" "$@"
fi

set -a
. /etc/zengin-api/env
set +a
exec /opt/zengin-api/bin/zengin-api "$@"
