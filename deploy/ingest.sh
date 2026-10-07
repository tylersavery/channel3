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
# ~/srv/channel3/channels and CHANNEL3_CHANNELS overrides it. settings.yaml
# beside that directory is mirrored the same way: copied when it exists here,
# removed there when it does not, which puts the Pi back on the defaults. The
# icons directory beside it, which holds the channels' bumper icons, is mirrored
# like the channel files, .svg files only.
#
# Music is different, because the Mac is short of space: the radio directory
# beside the channel configs, one folder per station, is a drop zone. Each song
# in it is moved to /srv/channel3/local/radio on the Pi, and only removed from
# the Mac once rsync has transferred and checked it, so the Pi holds the master
# copy of the music and the Mac's folders empty themselves.
#
# Movies work the same way: the movies directory beside the channel configs is
# a drop zone for DVD and Blu-ray rips, with any .srt subtitles and a poster
# picture of the same name beside them. Each file is moved to
# /srv/channel3/movies-inbox and removed from the Mac once it has arrived
# whole. When the service starts again it prepares them for Movie Mode in the
# background and deletes each rip once its movie is ready.
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

local_icons="$(dirname "$LOCAL_CHANNELS")/icons"
if [ -d "$local_icons" ]; then
	printf '==> syncing icons from %s\n' "$local_icons"
	rsync -rt --delete --itemize-changes --include='*.svg' --exclude='*' \
		--rsync-path="sudo -u channel3 rsync" \
		"$local_icons/" "$PI_HOST:$REMOTE_ROOT/icons/"
fi

local_radio="$(dirname "$LOCAL_CHANNELS")/radio"
if [ -d "$local_radio" ]; then
	printf '==> moving music from %s\n' "$local_radio"
	# shellcheck disable=SC2029  # local constant, expanded here on purpose
	ssh "$PI_HOST" "sudo -u channel3 mkdir -p $REMOTE_ROOT/local/radio"
	rsync -rt --remove-source-files --itemize-changes \
		--include='*/' \
		--include='*.[mM][pP]3' --include='*.[mM]4[aA]' --include='*.[aA][aA][cC]' \
		--include='*.[fF][lL][aA][cC]' --include='*.[oO][gG][gG]' --include='*.[oO][pP][uU][sS]' \
		--include='*.[wW][aA][vV]' \
		--exclude='*' \
		--rsync-path="sudo -u channel3 rsync" \
		"$local_radio/" "$PI_HOST:$REMOTE_ROOT/local/radio/"
fi

local_movies="$(dirname "$LOCAL_CHANNELS")/movies"
if [ -d "$local_movies" ]; then
	printf '==> moving movies from %s\n' "$local_movies"
	# A Blu-ray rip is tens of gigabytes, so an interrupted transfer keeps
	# what it has in a hidden folder and picks up from there next time. serve
	# skips hidden files, so it never prepares half a movie.
	# shellcheck disable=SC2029  # local constant, expanded here on purpose
	ssh "$PI_HOST" "sudo -u channel3 mkdir -p $REMOTE_ROOT/movies-inbox"
	rsync -rt --remove-source-files --partial-dir=.rsync-partial --progress \
		--include='*/' \
		--include='*.[mM][kK][vV]' --include='*.[mM][pP]4' --include='*.[mM]4[vV]' \
		--include='*.[sS][rR][tT]' \
		--include='*.[jJ][pP][gG]' --include='*.[jJ][pP][eE][gG]' --include='*.[pP][nN][gG]' \
		--exclude='*' \
		--rsync-path="sudo -u channel3 rsync" \
		"$local_movies/" "$PI_HOST:$REMOTE_ROOT/movies-inbox/"
fi

local_settings="$(dirname "$LOCAL_CHANNELS")/settings.yaml"
if [ -f "$local_settings" ]; then
	printf '==> syncing %s\n' "$local_settings"
	rsync -t --itemize-changes --rsync-path="sudo -u channel3 rsync" \
		"$local_settings" "$PI_HOST:$REMOTE_ROOT/settings.yaml"
else
	# shellcheck disable=SC2029  # local constant, expanded here on purpose
	ssh "$PI_HOST" "sudo -u channel3 rm -f $REMOTE_ROOT/settings.yaml"
fi
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
