#!/usr/bin/env bash
#
# Run channel3 ingest on the Pi, as the service user, against the real root.
#
#   PI_HOST=channel3.local deploy/ingest.sh --channel saturday-morning
#   make pi-ingest PI_HOST=channel3.local ARGS="--dry-run"
#
# Ingest refuses to run while serve is broadcasting on the same machine, and on
# the Pi serve is always up. So this script does the whole operation: it stops
# the service, ingests, and starts it again even if the ingest failed or you
# interrupted it. The television is dark for the duration.
#
# Ingesting on the Mac into ~/srv/channel3 and rsyncing the library to the Pi is
# the no-downtime route.
#
# Starting the service again rescans the library, so the new items are on air
# right away rather than at the next 04:00 rollover.
#
set -euo pipefail

REMOTE_BIN="/usr/local/bin/channel3"
REMOTE_ROOT="/srv/channel3"

if [ -z "${PI_HOST:-}" ]; then
	cat >&2 <<'EOF'
ingest: PI_HOST is not set, so there is nothing to ingest on.

  make pi-ingest PI_HOST=channel3.local ARGS="--channel bluey"
  PI_HOST=channel3.local deploy/ingest.sh --channel bluey
EOF
	exit 2
fi

# ssh hands the whole command line to a remote shell, which splits it again, so
# every argument is single quoted here to survive that second parse.
quote_for_remote() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

remote_args=""
for arg in "$@"; do
	remote_args="$remote_args $(quote_for_remote "$arg")"
done

service_stopped=0

# Runs on every exit, including a Ctrl-C, so the broadcast never stays down
# because an ingest died halfway.
start_service() {
	if [ "$service_stopped" -eq 1 ]; then
		service_stopped=0
		printf '\n==> starting channel3 again\n'
		if ! ssh "$PI_HOST" sudo systemctl start channel3; then
			printf 'ingest: could not start channel3 again. Start it by hand with:\n' >&2
			printf '  ssh %s sudo systemctl start channel3\n' "$PI_HOST" >&2
		fi
	fi
}
trap start_service EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

set +e
ssh "$PI_HOST" systemctl is-active --quiet channel3
active_status=$?
set -e
if [ "$active_status" -eq 255 ]; then
	printf 'ingest: could not reach %s over ssh\n' "$PI_HOST" >&2
	exit 1
fi

if [ "$active_status" -eq 0 ]; then
	cat <<EOF
==> channel3 is broadcasting on $PI_HOST and ingest cannot run alongside it.

    Stopping the service now. The television goes dark until the ingest
    finishes, which for a large channel is minutes, not seconds.

EOF
	ssh "$PI_HOST" sudo systemctl stop channel3
	service_stopped=1
else
	printf '==> channel3 is not running on %s, ingesting directly\n\n' "$PI_HOST"
fi

# -t gives yt-dlp a terminal, so its progress streams instead of arriving in
# one block at the end.
set +e
ssh -t "$PI_HOST" "sudo -u channel3 $REMOTE_BIN ingest --root $REMOTE_ROOT$remote_args"
status=$?
set -e

start_service

case "$status" in
0)
	printf '\ningest finished, nothing failed.\n'
	;;
2)
	printf '\ningest finished, but some items failed. The failed sidecars under %s/library say why, and the next run retries them.\n' \
		"$REMOTE_ROOT" >&2
	;;
*)
	printf '\ningest failed with exit %d. Nothing about the schedule changed.\n' "$status" >&2
	;;
esac

exit "$status"
