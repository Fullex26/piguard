#!/bin/sh
set -eu

LOG_DIR=/var/log/piguard
CLAM_LOG="$LOG_DIR/clamav-scan.log"

mkdir -p "$LOG_DIR"
touch "$CLAM_LOG" /var/log/rkhunter.log
chown root:adm "$CLAM_LOG" /var/log/rkhunter.log
chmod 0640 "$CLAM_LOG" /var/log/rkhunter.log

SCAN_PATHS="/home /etc /usr/local /opt"
for path in /srv /var/www; do
	if [ -e "$path" ]; then
		SCAN_PATHS="$SCAN_PATHS $path"
	fi
done

set +e
clamscan --recursive --infected --log="$CLAM_LOG" $SCAN_PATHS
clam_status=$?
set -e

case "$clam_status" in
	0|1) ;;
	*)
		echo "PIGUARD_SCAN_ERROR: ClamAV scan failed with exit status $clam_status" >>"$CLAM_LOG"
		exit "$clam_status"
		;;
esac

rkhunter --update --nocolors >/dev/null 2>&1 || true
set +e
rkhunter --check --skip-keypress --report-warnings-only --nocolors --appendlog
rkhunter_status=$?
set -e

# rkhunter uses status 1 for findings. Status 2 or above is an execution error.
if [ "$rkhunter_status" -gt 1 ]; then
	echo "PIGUARD_SCAN_ERROR: rkhunter scan failed with exit status $rkhunter_status" >>/var/log/rkhunter.log
	exit "$rkhunter_status"
fi
