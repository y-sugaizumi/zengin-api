#!/usr/bin/env bash
# zengin-api をホストにインストール/再デプロイする。
#
# 初回実行時はユーザー作成・ディレクトリ作成・env配置・systemd unit配置と有効化まで行う。
# 2回目以降は既存のenvを上書きせず、バイナリとunitファイルの更新・再起動だけを行う
# (何度実行しても安全 = 冪等)。
#
# 使い方: リポジトリを対象ホストにチェックアウトし、`make build` (または `make build-local`)
# で bin/zengin-api を作ってから、このスクリプトをホスト上で実行する。
#
#   make build
#   sudo ./deploy/install.sh
set -euo pipefail

APP=zengin-api
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
  echo "bin/${APP} がありません。先に 'make build' (または 'make build-local') を実行してください" >&2
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

echo "[install] systemd unitを配置します"
cp "${SCRIPT_DIR}/${APP}.service" "${SCRIPT_DIR}/${APP}-update.service" "${SCRIPT_DIR}/${APP}-update.timer" \
  "${SYSTEMD_DIR}/"
systemctl daemon-reload

echo "[install] サービスを起動/再起動します"
systemctl enable "${APP}.service" "${APP}-update.timer" >/dev/null
systemctl restart "${APP}.service"
systemctl start "${APP}-update.timer"

echo "[install] 完了。状態確認: systemctl status ${APP}.service"
