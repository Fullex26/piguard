#!/bin/sh
set -eu

ENV_FILE=${PIGUARD_ENV_FILE:-/etc/piguard/env}

if [ "$(id -u)" -ne 0 ]; then
	echo "Run as root: sudo piguard-rotate-telegram-token" >&2
	exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
	echo "PiGuard environment file not found: $ENV_FILE" >&2
	exit 1
fi

printf "New Telegram bot token: "
stty -echo
IFS= read -r token
stty echo
printf "\n"

case "$token" in
	*:* ) ;;
	* )
		echo "Token format is invalid." >&2
		exit 1
		;;
esac

umask 077
tmp=$(mktemp)
trap 'rm -f "$tmp"; stty echo 2>/dev/null || true' EXIT HUP INT TERM
found=false

while IFS= read -r line || [ -n "$line" ]; do
	case "$line" in
		PIGUARD_TELEGRAM_TOKEN=*)
			printf 'PIGUARD_TELEGRAM_TOKEN=%s\n' "$token" >>"$tmp"
			found=true
			;;
		*) printf '%s\n' "$line" >>"$tmp" ;;
	esac
done <"$ENV_FILE"

if [ "$found" = false ]; then
	printf 'PIGUARD_TELEGRAM_TOKEN=%s\n' "$token" >>"$tmp"
fi

install -o root -g root -m 0600 "$tmp" "$ENV_FILE"
token=
systemctl restart piguard

if ! systemctl is-active --quiet piguard; then
	echo "PiGuard did not restart successfully." >&2
	exit 1
fi

echo "Telegram token updated and PiGuard restarted."
