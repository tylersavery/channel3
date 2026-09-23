#!/usr/bin/env bash
#
# One-time Channel Three setup for a freshly flashed Raspberry Pi OS Lite.
# Run it as root on the Pi. It is idempotent: re-running changes nothing that
# is already in place, and it prints what it changed and what it skipped.
#
#   sudo bash pi-setup.sh /dev/sda1
#   sudo bash pi-setup.sh PARTUUID=1a2b3c4d-01 America/Toronto
#
# It never formats anything. Format the library SSD by hand once, before the
# first run, as deploy/README.md describes:
#
#   mkfs.ext4 -L channel3 /dev/sdX1
#
set -euo pipefail

# yt-dlp is pinned to a known version and installed as the standalone binary,
# because Debian's package is always months stale and ingest breaks the day
# YouTube changes something. Bump this deliberately, then re-run this script.
YTDLP_VERSION="2026.08.19"
YTDLP_ASSET="yt-dlp_linux_aarch64"
YTDLP_RELEASE="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}"
YTDLP_URL="${YTDLP_RELEASE}/${YTDLP_ASSET}"
YTDLP_SUMS_URL="${YTDLP_RELEASE}/SHA2-256SUMS"
YTDLP_PATH="/usr/local/bin/yt-dlp"

APT_PACKAGES=(mpv v4l-utils ffmpeg)
SERVICE_USER="channel3"
SERVICE_GROUPS=(video render input audio)
ROOT_DIR="/srv/channel3"
UNIT_PATH="/etc/systemd/system/channel3.service"
ENV_PATH="/etc/default/channel3"
CMDLINE="/boot/firmware/cmdline.txt"
CMDLINE_PARAMS=(quiet loglevel=0 logo.nologo vt.global_cursor_default=0 consoleblank=0)
DEFAULT_TZ="America/Toronto"
FSTAB_OPTIONS="defaults,noatime,nofail,x-systemd.device-timeout=15"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

changed=()
unchanged=()
warnings=()

note_changed() {
	changed+=("$1")
	printf '  changed  %s\n' "$1"
}

note_ok() {
	unchanged+=("$1")
	printf '  ok       %s\n' "$1"
}

note_warn() {
	warnings+=("$1")
	printf '  warn     %s\n' "$1"
}

print_summary() {
	printf '\nSummary: %d changed, %d already in place, %d warnings\n' \
		"${#changed[@]}" "${#unchanged[@]}" "${#warnings[@]}"
	if [ ${#changed[@]} -gt 0 ]; then
		printf '\nChanged:\n'
		for item in "${changed[@]}"; do printf '  - %s\n' "$item"; done
	fi
	if [ ${#warnings[@]} -gt 0 ]; then
		printf '\nWarnings:\n'
		for item in "${warnings[@]}"; do printf '  - %s\n' "$item"; done
	fi
}

# Anything already done is still reported on the way out, so a re-run knows
# where it stopped.
die() {
	printf 'pi-setup: %s\n' "$1" >&2
	if [ ${#changed[@]} -gt 0 ] || [ ${#warnings[@]} -gt 0 ]; then
		print_summary >&2
	fi
	exit 1
}

usage() {
	cat <<'EOF'
usage: pi-setup.sh <ssd> [timezone]

  ssd       the library SSD partition, as a device path (/dev/sda1) or a
            PARTUUID (PARTUUID=1a2b3c4d-01, or the bare value). It must
            already hold an ext4 filesystem; this script never formats.
  timezone  an IANA zone name. Defaults to America/Toronto.
EOF
}

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	usage >&2
	exit 2
fi

disk_arg="$1"
timezone="${2:-$DEFAULT_TZ}"

[ "$(id -u)" -eq 0 ] || die "run as root: sudo bash pi-setup.sh $*"
[ -f "$SCRIPT_DIR/channel3.service" ] || die "channel3.service is not next to this script"
[ -f "$SCRIPT_DIR/channel3.env.example" ] || die "channel3.env.example is not next to this script"
command -v curl >/dev/null || die "curl is required to fetch yt-dlp"

printf 'Channel Three setup on %s\n\n' "$(hostname)"

# --- library SSD ------------------------------------------------------------
# fstab always names the partition by PARTUUID, so a USB enumeration order
# change never sends the library to the wrong disk.
case "$disk_arg" in
PARTUUID=*)
	partuuid="${disk_arg#PARTUUID=}"
	;;
/dev/*)
	[ -b "$disk_arg" ] || die "$disk_arg is not a block device"
	partuuid="$(blkid -s PARTUUID -o value "$disk_arg" 2>/dev/null || true)"
	[ -n "$partuuid" ] || die "$disk_arg has no PARTUUID; format it first with: mkfs.ext4 -L channel3 $disk_arg"
	;;
*)
	partuuid="$disk_arg"
	;;
esac

part_dev="$(blkid -t "PARTUUID=$partuuid" -o device 2>/dev/null | head -n 1 || true)"
[ -n "$part_dev" ] ||
	die "no partition with PARTUUID=$partuuid; check lsblk -o NAME,SIZE,PARTUUID and format it first with mkfs.ext4 -L channel3"

# The fstab line says ext4, so a partition holding anything else would mount
# nowhere and the library would silently land on the microSD instead.
part_fstype="$(blkid -s TYPE -o value "$part_dev" 2>/dev/null || true)"
[ "$part_fstype" = "ext4" ] ||
	die "$part_dev (PARTUUID=$partuuid) holds ${part_fstype:-no filesystem}, not ext4; format it first with: mkfs.ext4 -L channel3 $part_dev"

# --- packages ---------------------------------------------------------------
export DEBIAN_FRONTEND=noninteractive
apt_missing=()
for pkg in "${APT_PACKAGES[@]}"; do
	if dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q '^install ok installed$'; then
		note_ok "apt package $pkg"
	else
		apt_missing+=("$pkg")
	fi
done
if [ ${#apt_missing[@]} -gt 0 ]; then
	apt-get update
	apt-get install -y "${apt_missing[@]}"
	for pkg in "${apt_missing[@]}"; do
		note_changed "apt package $pkg"
	done
fi

# --- yt-dlp -----------------------------------------------------------------
installed_ytdlp=""
if [ -x "$YTDLP_PATH" ]; then
	installed_ytdlp="$("$YTDLP_PATH" --version 2>/dev/null || true)"
fi
if [ "$installed_ytdlp" = "$YTDLP_VERSION" ]; then
	note_ok "yt-dlp $YTDLP_VERSION"
else
	tmp_ytdlp="$(mktemp)"
	tmp_sums="$(mktemp)"
	trap 'rm -f "$tmp_ytdlp" "$tmp_sums"' EXIT
	curl -fsSL -o "$tmp_ytdlp" "$YTDLP_URL" || die "could not download $YTDLP_URL"

	# This is the one binary this script downloads and it ends up running as
	# root's install target, so the release checksum is checked before it is
	# ever executed.
	curl -fsSL -o "$tmp_sums" "$YTDLP_SUMS_URL" || die "could not download $YTDLP_SUMS_URL"
	expected_sum="$(awk -v asset="$YTDLP_ASSET" '$2 == asset { print $1 }' "$tmp_sums")"
	[ -n "$expected_sum" ] || die "SHA2-256SUMS for $YTDLP_VERSION has no line for $YTDLP_ASSET"
	actual_sum="$(sha256sum "$tmp_ytdlp" | awk '{ print $1 }')"
	[ "$actual_sum" = "$expected_sum" ] ||
		die "checksum mismatch on $YTDLP_ASSET: got $actual_sum, the release says $expected_sum"

	chmod 0755 "$tmp_ytdlp"
	[ -x "$tmp_ytdlp" ] || die "the downloaded yt-dlp is not executable"
	downloaded_version="$("$tmp_ytdlp" --version 2>/dev/null || true)"
	[ "$downloaded_version" = "$YTDLP_VERSION" ] ||
		die "the downloaded yt-dlp reports '${downloaded_version:-nothing}', wanted $YTDLP_VERSION"
	install -m 0755 "$tmp_ytdlp" "$YTDLP_PATH"
	rm -f "$tmp_ytdlp" "$tmp_sums"
	trap - EXIT
	note_changed "yt-dlp $YTDLP_VERSION at $YTDLP_PATH, checksum verified (was ${installed_ytdlp:-absent})"
fi

# --- service user -----------------------------------------------------------
if id -u "$SERVICE_USER" >/dev/null 2>&1; then
	note_ok "user $SERVICE_USER"
else
	useradd --system --no-create-home --home-dir "$ROOT_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
	note_changed "user $SERVICE_USER"
fi

for grp in "${SERVICE_GROUPS[@]}"; do
	if ! getent group "$grp" >/dev/null 2>&1; then
		note_warn "group $grp does not exist on this system, so $SERVICE_USER was not added to it"
		continue
	fi
	if id -nG "$SERVICE_USER" | tr ' ' '\n' | grep -qx "$grp"; then
		note_ok "user $SERVICE_USER in group $grp"
	else
		usermod -aG "$grp" "$SERVICE_USER"
		note_changed "user $SERVICE_USER added to group $grp"
	fi
done

# --- mount ------------------------------------------------------------------
mkdir -p "$ROOT_DIR"
if grep -qE "^[^#]+[[:space:]]+${ROOT_DIR}[[:space:]]+" /etc/fstab; then
	note_ok "/etc/fstab line for $ROOT_DIR"
else
	printf 'PARTUUID=%s  %s  ext4  %s  0  2\n' "$partuuid" "$ROOT_DIR" "$FSTAB_OPTIONS" >>/etc/fstab
	note_changed "/etc/fstab line for $ROOT_DIR (PARTUUID=$partuuid)"
	systemctl daemon-reload
fi

# A failure here is usually an unrelated fstab line, and the summary is worth
# more than an abort.
mount -a || note_warn "mount -a reported an error; check the other lines in /etc/fstab"

# Everything below writes into $ROOT_DIR. If the SSD is not mounted, those
# writes would land on the microSD underneath the mount point and be hidden the
# moment the disk does mount, so stop here instead.
if mountpoint -q "$ROOT_DIR"; then
	note_ok "$ROOT_DIR is mounted"
else
	note_warn "$ROOT_DIR is not mounted; nofail means a missing disk is silent, so check lsblk and dmesg"
	die "refusing to create the library directories on the microSD under an empty mount point"
fi

# --- directories ------------------------------------------------------------
for dir in "$ROOT_DIR" "$ROOT_DIR/channels" "$ROOT_DIR/library" "$ROOT_DIR/local"; do
	if [ -d "$dir" ]; then
		note_ok "$dir"
	else
		mkdir -p "$dir"
		note_changed "$dir"
	fi
	# Not recursive on purpose: the library is up to two terabytes and is
	# written by this same user anyway.
	if [ "$(stat -c '%U' "$dir")" = "$SERVICE_USER" ]; then
		note_ok "$dir owned by $SERVICE_USER"
	else
		chown "$SERVICE_USER:$SERVICE_USER" "$dir"
		note_changed "$dir owned by $SERVICE_USER"
	fi
done

# --- unit and flags ---------------------------------------------------------
unit_changed=0
if [ -f "$UNIT_PATH" ] && cmp -s "$SCRIPT_DIR/channel3.service" "$UNIT_PATH"; then
	note_ok "$UNIT_PATH"
else
	install -m 0644 "$SCRIPT_DIR/channel3.service" "$UNIT_PATH"
	unit_changed=1
	note_changed "$UNIT_PATH"
fi

if [ -f "$ENV_PATH" ]; then
	note_ok "$ENV_PATH (left exactly as it is; this is the file you edit)"
else
	install -m 0644 "$SCRIPT_DIR/channel3.env.example" "$ENV_PATH"
	note_changed "$ENV_PATH from channel3.env.example"
fi

systemctl daemon-reload
if systemctl is-enabled --quiet channel3 2>/dev/null; then
	note_ok "channel3.service enabled at boot"
else
	systemctl enable channel3 >/dev/null
	note_changed "channel3.service enabled at boot"
fi

# try-restart picks up a changed unit on a box that is already broadcasting and
# does nothing at all on one that is not, which is the first-run case.
if [ "$unit_changed" -eq 1 ]; then
	if systemctl try-restart channel3; then
		note_changed "channel3.service restarted to pick up the new unit, if it was running"
	else
		note_warn "the new unit is installed but try-restart failed; run systemctl restart channel3 by hand"
	fi
fi

# --- quiet boot -------------------------------------------------------------
# Nothing here touches /boot/firmware/config.txt. If Phase 4 finds that a
# television power cycle drops the HDMI output, hdmi_force_hotplug=1 (or its
# Pi 5 equivalent, which the KMS driver handles differently) goes in config.txt
# and gets added to this script then, with the hardware in front of you.
if [ ! -f "$CMDLINE" ]; then
	note_warn "$CMDLINE not found, so the quiet boot parameters were not applied"
else
	cmdline_before="$(tr -d '\n' <"$CMDLINE")"
	cmdline_after="$cmdline_before"
	for param in "${CMDLINE_PARAMS[@]}"; do
		key="${param%%=*}"
		if [ "$key" = "$param" ]; then
			case " $cmdline_after " in
			*" $param "*)
				note_ok "cmdline.txt $param"
				continue
				;;
			esac
			cmdline_after="$cmdline_after $param"
			note_changed "cmdline.txt $param"
			continue
		fi
		current="$(printf '%s' "$cmdline_after" | tr ' ' '\n' | grep -E "^$key=" || true)"
		if [ "$current" = "$param" ]; then
			note_ok "cmdline.txt $param"
		elif [ -n "$current" ]; then
			cmdline_after="$(printf '%s' "$cmdline_after" | sed -E "s#(^| )$key=[^ ]*#\1$param#")"
			note_changed "cmdline.txt $param (was $current)"
		else
			cmdline_after="$cmdline_after $param"
			note_changed "cmdline.txt $param"
		fi
	done
	if [ "$cmdline_after" != "$cmdline_before" ]; then
		cp -p "$CMDLINE" "$CMDLINE.channel3.bak"
		# The kernel reads exactly one line, so this stays one line.
		printf '%s\n' "$cmdline_after" >"$CMDLINE"
		note_changed "$CMDLINE rewritten, previous copy at $CMDLINE.channel3.bak"
	fi
fi

if systemctl is-enabled --quiet getty@tty1 2>/dev/null || systemctl is-active --quiet getty@tty1 2>/dev/null; then
	if systemctl disable --now getty@tty1 >/dev/null 2>&1; then
		note_changed "getty@tty1 disabled, so no login prompt draws on the television"
	else
		note_warn "could not disable getty@tty1; a login prompt may appear over mpv"
	fi
else
	note_ok "getty@tty1 already disabled"
fi

# --- time zone --------------------------------------------------------------
current_tz="$(timedatectl show -p Timezone --value 2>/dev/null || true)"
if [ "$current_tz" = "$timezone" ]; then
	note_ok "timezone $timezone"
else
	timedatectl set-timezone "$timezone"
	note_changed "timezone $timezone (was ${current_tz:-unknown})"
fi

# --- summary ----------------------------------------------------------------
print_summary

cat <<EOF

The service is enabled but not started, because the mpv output flags are not
known until the Phase 4 hardware checklist has run on this Pi.

Next:
  1. Put a channel config in $ROOT_DIR/channels/ and ingest something.
  2. Put the mpv flags Phase 4 proved into CHANNEL3_FLAGS in $ENV_PATH.
  3. systemctl start channel3 && journalctl -u channel3 -f
  4. Reboot once to confirm the boot is quiet and video comes up on its own.
EOF
