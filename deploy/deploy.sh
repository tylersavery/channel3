#!/usr/bin/env bash
#
# Build Channel Three for the Pi, copy it over, swap it in and restart.
#
#   PI_HOST=channel3.local deploy/deploy.sh
#   make deploy PI_HOST=channel3.local
#
# A deploy restarts the service, which kills mpv and anything else in flight.
# Never deploy while the kids are watching, and never while an ingest runs.
# The previous binary is kept at /usr/local/bin/channel3.prev for rollback.
#
set -euo pipefail

BINARY="bin/channel3-linux-arm64"
REMOTE_TMP="/tmp/channel3.new"
REMOTE_BIN="/usr/local/bin/channel3"
JOURNAL_SECONDS=10

REPO_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -z "${PI_HOST:-}" ]; then
	cat >&2 <<'EOF'
deploy: PI_HOST is not set, so there is nothing to deploy to.

  make deploy PI_HOST=channel3.local
  PI_HOST=channel3.local deploy/deploy.sh

Nothing was built and nothing was copied.
EOF
	exit 2
fi

# The ingest check comes before the build, so a refused deploy costs a second
# rather than a full arm64 build. The [c] in the pattern keeps pgrep from
# matching the shell that ssh started to run it.
printf '==> checking %s for a running ingest\n' "$PI_HOST"
set +e
ssh "$PI_HOST" "pgrep -f '[c]hannel3 ingest' >/dev/null 2>&1"
ingest_status=$?
set -e
case "$ingest_status" in
0)
	cat >&2 <<EOF
deploy: an ingest is running on $PI_HOST.

Restarting the service now would kill it mid download. Wait for it to finish,
or stop it deliberately, then run this again. Nothing was built or copied.
EOF
	exit 1
	;;
1)
	printf '    no ingest running\n'
	;;
*)
	printf 'deploy: could not reach %s over ssh (status %d)\n' "$PI_HOST" "$ingest_status" >&2
	exit 1
	;;
esac

printf '==> building %s\n' "$BINARY"
make -C "$REPO_DIR" build-arm64

printf '==> copying to %s:%s\n' "$PI_HOST" "$REMOTE_TMP"
scp "$REPO_DIR/$BINARY" "$PI_HOST:$REMOTE_TMP"

# set -e inside the remote shell so a failed install never leaves a half
# swapped binary behind and never restarts the service on top of one.
remote_install="set -e; \
install -m 0755 $REMOTE_TMP $REMOTE_BIN.new; \
if [ -f $REMOTE_BIN ]; then cp -p $REMOTE_BIN $REMOTE_BIN.prev; fi; \
mv $REMOTE_BIN.new $REMOTE_BIN; \
systemctl restart channel3"

printf '==> installing and restarting\n'
ssh -t "$PI_HOST" "sudo sh -c '$remote_install'"
# The paths below are local constants, so expanding them here, on the client,
# is exactly what is wanted.
# shellcheck disable=SC2029
ssh "$PI_HOST" "rm -f $REMOTE_TMP"

printf '==> journal for %d seconds\n' "$JOURNAL_SECONDS"
# timeout kills journalctl --follow and exits 124 every time, which is the
# normal path here, so its status says nothing about the deploy.
# shellcheck disable=SC2029  # local constant, expanded here on purpose
ssh "$PI_HOST" "timeout $JOURNAL_SECONDS journalctl -u channel3 --follow --lines=20" || true

if ! ssh "$PI_HOST" "systemctl is-active --quiet channel3"; then
	printf '\ndeploy: channel3 is not active on %s after the restart\n' "$PI_HOST" >&2
	# systemctl status exits non-zero for a failed unit, which is the case here.
	ssh "$PI_HOST" "systemctl status --no-pager --lines=30 channel3" >&2 || true
	cat >&2 <<EOF

Roll back to the previous binary with:

  ssh $PI_HOST "sudo sh -c 'cp -p $REMOTE_BIN.prev $REMOTE_BIN && systemctl restart channel3'"

The unit has no start rate limit, so it keeps retrying once a second rather
than parking in the failed state. A crash loop shows up as a repeating line in
the journal: ssh $PI_HOST journalctl -u channel3 --since -5min
EOF
	exit 1
fi

# shellcheck disable=SC2029  # local constant, expanded here on purpose
deployed_version="$(ssh "$PI_HOST" "$REMOTE_BIN --version")"
printf '\n==> deployed %s to %s\n' "$deployed_version" "$PI_HOST"
