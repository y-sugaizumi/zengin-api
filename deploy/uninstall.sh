#!/usr/bin/env bash
# install.sh でホストに入れたものを取り除く。
#
# 既定では、データ(/var/lib/zengin-api)とシステムユーザー zengin-api は残す
# (入れ直したときにそのまま使えるように)。--purge を付けるとそれらも消す。
# 何度実行しても安全(無いものは飛ばす)。Caddyのサイト定義は Taskfile の uninstall が先に外す。
#
#   task uninstall         # = caddy-site.sh uninstall + sudo ./deploy/uninstall.sh
#   task uninstall:purge   # = 上記 + --purge
set -euo pipefail

APP="zengin-api"
BIN_DIR=/opt/${APP}
DATA_DIR=/var/lib/${APP}
SOCKET_DIR=/srv/sockets/${APP}
CONFIG_DIR=/etc/${APP}
SYSTEMD_DIR=/etc/systemd/system
UNITS=("${APP}.service" "${APP}-update.service" "${APP}-update.timer")

purge=0
case "${1:-}" in
  "") ;;
  --purge) purge=1 ;;
  *) echo "使い方: $0 [--purge]" >&2; exit 1 ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  echo "root権限で実行してください(sudo ./deploy/uninstall.sh)" >&2
  exit 1
fi

# env で既定外の場所を指していたら、消す前に控えておく(消さずに最後に知らせる)
leftovers=()
if [ -f "${CONFIG_DIR}/env" ]; then
  while IFS='=' read -r key value; do
    value=${value%$'\r'}; value=${value#\"}; value=${value%\"}
    case "${key}" in
      DATA_DIR)
        [[ -n ${value} && ${value} != "${DATA_DIR}" && -e ${value} ]] && leftovers+=("${value}(DATA_DIR)") ;;
      SOCKET_PATH)
        [[ -n ${value} && ${value} != "${SOCKET_DIR}/"* && -e ${value} ]] && leftovers+=("${value}(SOCKET_PATH)") ;;
    esac
  done < <(grep -E '^[A-Z_]+=' "${CONFIG_DIR}/env")
fi

echo "[uninstall] サービスを止めて無効にします"
for unit in "${UNITS[@]}"; do
  if [ -f "${SYSTEMD_DIR}/${unit}" ]; then
    systemctl disable --now "${unit}" >/dev/null 2>&1 || true
    rm -f "${SYSTEMD_DIR}/${unit}"
  fi
done
systemctl daemon-reload
systemctl reset-failed "${UNITS[@]}" >/dev/null 2>&1 || true

echo "[uninstall] バイナリ・ソケット・設定を削除します"
rm -rf "${BIN_DIR}" "/usr/local/bin/${APP}" "${SOCKET_DIR}" "${CONFIG_DIR}"

if [ "${purge}" -eq 1 ]; then
  echo "[uninstall] データ(${DATA_DIR})とシステムユーザー ${APP} を削除します"
  rm -rf "${DATA_DIR}"
  if id "${APP}" >/dev/null 2>&1; then
    userdel "${APP}"
  fi
else
  [ -e "${DATA_DIR}" ] && leftovers+=("${DATA_DIR}(データ。消すには task uninstall:purge)")
  id "${APP}" >/dev/null 2>&1 && leftovers+=("システムユーザー ${APP}(データの所有者として残す)")
fi

echo "[uninstall] 完了"
if [ ${#leftovers[@]} -gt 0 ]; then
  echo "[uninstall] 次のものは残っています:"
  printf '  - %s\n' "${leftovers[@]}"
fi
