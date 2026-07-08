#!/bin/sh
set -eu

mkdir -p /var/log/clamav
touch /var/log/clamav/clamav.log /var/log/rkhunter.log
chown clamav:adm /var/log/clamav/clamav.log 2>/dev/null || true
chmod 0640 /var/log/clamav/clamav.log /var/log/rkhunter.log

freshclam --quiet || true

SCAN_PATHS="/home /etc /usr/local /opt"
for path in /srv /var/www; do
	if [ -e "$path" ]; then
		SCAN_PATHS="$SCAN_PATHS $path"
	fi
done

clamscan --recursive --infected --log=/var/log/clamav/clamav.log $SCAN_PATHS || true
rkhunter --update --nocolors >/dev/null 2>&1 || true
rkhunter --check --skip-keypress --report-warnings-only --nocolors --appendlog || true
