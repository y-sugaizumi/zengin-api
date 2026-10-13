#!/usr/bin/env bash
# zengin-api をホストにインストール/再デプロイする。
#
# 初回実行時はユーザー作成・ディレクトリ作成・env配置・systemd unit配置と有効化まで行う。
# 2回目以降は既存のenvを上書きせず、バイナリとunitファイルの更新・再起動だけを行う
# (何度実行しても安全 = 冪等)。
#
# 使い方: リポジトリを対象ホストにチェックアウトし、`task build` (または `task build:local`)
# で bin/zengin-api を作ってから、このスクリプトをホスト上で実行する。
#
#   task build
#   task deploy        # = sudo ./deploy/install.sh(確認プロンプトあり)
#
# Caddyのサイト定義はこのスクリプトでは扱わない(`task caddy:install` で別に配置する)。
set -euo pipefail

APP="zengin-api"
BIN_DIR=/opt/${APP}/bin
DATA_DIR=/var/lib/${APP}
SOCKET_DIR=/srv/sockets/${APP}
CONFIG_DIR=/etc/${APP}
SYSTEMD_DIR=/etc/systemd/system

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

if [ "$(id -u)" -ne 0 ]; then
  echo "root権限で実行してください(sudo ./deploy/install.sh)" >&2
  exit 1
fi

if [ ! -f "${REPO_ROOT}/bin/${APP}" ]; then
  echo "bin/${APP} がありません。先に 'task build' (または 'task build:local') を実行してください" >&2
  exit 1
fi

if ! id "${APP}" >/dev/null 2>&1; then
  echo "[install] システムユーザー ${APP} を作成します"
  useradd --system --home "${DATA_DIR}" --shell /usr/sbin/nologin "${APP}"
fi

echo "[install] ディレクトリを準備します"
mkdir -p "${BIN_DIR}" "${DATA_DIR}" "${SOCKET_DIR}" "${CONFIG_DIR}"
chown -R "${APP}:${APP}" "${DATA_DIR}" "${SOCKET_DIR}"

if [ ! -f "${CONFIG_DIR}/env" ]; then
  echo "[install] ${CONFIG_DIR}/env が無いので既定値で作成します(既存の場合は上書きしません)"
  cp "${SCRIPT_DIR}/${APP}.env.example" "${CONFIG_DIR}/env"
fi

echo "[install] バイナリを配置します"
install -m 0755 "${REPO_ROOT}/bin/${APP}" "${BIN_DIR}/${APP}"
# 管理コマンドの入口。zengin-api ユーザー・/etc/zengin-api/env の設定で実行する(PATH の通った場所に置く)
install -m 0755 "${SCRIPT_DIR}/${APP}-cli.sh" "/usr/local/bin/${APP}"

echo "[install] systemd unitを配置します"
cp "${SCRIPT_DIR}/${APP}.service" "${SCRIPT_DIR}/${APP}-update.service" "${SCRIPT_DIR}/${APP}-update.timer" \
  "${SYSTEMD_DIR}/"
systemctl daemon-reload

echo "[install] サービスを起動/再起動します"
systemctl enable "${APP}.service" "${APP}-update.timer" >/dev/null
systemctl restart "${APP}.service"
systemctl start "${APP}-update.timer"

# serve はデータが無ければ初回取得(最大3分)を済ませてからソケットを作る。
# 落ちると Restart=on-failure で再起動を繰り返すだけなので、ここで /healthz が返るまで確かめる。
echo "[install] 起動を確認します(/healthz が返るのを待つ。初回はデータ取得で数十秒かかる)"
for _ in $(seq 1 200); do
  if systemctl is-active --quiet "${APP}.service" && "/usr/local/bin/${APP}" healthcheck >/dev/null 2>&1; then
    echo "[install] 完了。${APP}.service は起動しています"
    exit 0
  fi
  sleep 1
done

echo "[install] ${APP}.service が起動していません。直近のログ:" >&2
journalctl -u "${APP}.service" -n 20 --no-pager >&2 || true
echo "[install] ${CONFIG_DIR}/env 等を直してから、もう一度 task deploy を実行してください" >&2
exit 1
