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
# Before anything stops, the channel configs on this machine are mirrored onto
# the Pi, so editing the YAML here and running this is the whole workflow. The
# local directory is the master copy: a channel file deleted here is deleted
# there, though its downloaded videos stay on the drive. It defaults to
# ~/srv/channel3/channels and CHANNEL3_CHANNELS overrides it.
#
# Ingesting on the Mac into ~/srv/channel3 and rsyncing the library to the Pi is
# the no-downtime route.
#
# Stopping the service is announced and then waited on for a few seconds, so an
# ingest started in the middle of a programme can still be called off. Set
# CHANNEL3_YES=1 to skip the wait.
#
# Starting the service again rescans the library, so the new items are on air
# right away rather than at the next 04:00 rollover.
#
set -euo pipefail

REMOTE_BIN="/usr/local/bin/channel3"
REMOTE_ROOT="/srv/channel3"
LOCAL_CHANNELS="${CHANNEL3_CHANNELS:-$HOME/srv/channel3/channels}"

# How long the warning sits on screen before the broadcast is stopped, so a
# command typed while the kids are watching can still be interrupted. Set
# CHANNEL3_YES=1 to skip the pause, which is what a script wants.
STOP_WARNING_SECONDS=3

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
		if ! ssh -t "$PI_HOST" sudo systemctl start channel3; then
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

# An empty or missing directory would mirror as "delete every channel", which is
# never what a typo in CHANNEL3_CHANNELS means.
if ! compgen -G "$LOCAL_CHANNELS/*.yaml" >/dev/null; then
	printf 'ingest: no channel files in %s, so nothing was synced and nothing was stopped.\n' "$LOCAL_CHANNELS" >&2
	printf 'Set CHANNEL3_CHANNELS to the directory that holds your channel YAML.\n' >&2
	exit 1
fi
printf '==> syncing channel configs from %s\n' "$LOCAL_CHANNELS"
rsync -rt --delete --itemize-changes --include='*.yaml' --exclude='*' \
	--rsync-path="sudo -u channel3 rsync" \
	"$LOCAL_CHANNELS/" "$PI_HOST:$REMOTE_ROOT/channels/"
printf '\n'

if [ "$active_status" -eq 0 ]; then
	cat <<EOF
==> channel3 is broadcasting on $PI_HOST and ingest cannot run alongside it.

    Stopping the service now. The television goes dark until the ingest
    finishes, which for a large channel is minutes, not seconds.

EOF
	if [ "${CHANNEL3_YES:-}" != "1" ]; then
		printf '    Stopping in %ds. Press Ctrl-C now to leave the broadcast alone.\n\n' \
			"$STOP_WARNING_SECONDS"
		sleep "$STOP_WARNING_SECONDS"
	fi
	ssh -t "$PI_HOST" sudo systemctl stop channel3
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
