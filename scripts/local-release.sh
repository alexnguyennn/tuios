#!/usr/bin/env bash
# Local-only darwin release builder. --no-install never touches ~/.local/bin
# or any daemon. Install mode ends programs in the live daemon's panes.
set -euo pipefail
export CI=true
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
install=true
case "${1:-}" in
    --no-install) install=false; shift ;;
    --help|-h) echo 'Usage: just build-local | just install-local [--no-install]'; exit 0 ;;
esac
[ "$#" -eq 0 ] || { echo "Unknown argument: $1" >&2; exit 2; }
[ "$(uname -s)" = Darwin ] || { echo 'local release requires darwin' >&2; exit 1; }
[ "$(git branch --show-current)" = local/release ] || { echo 'Check out local/release first' >&2; exit 1; }
# Untracked source and embedded assets can change the compiled binary too.
[ -z "$(git status --porcelain)" ] || { echo 'Commit or move nonignored changes before building local/release' >&2; exit 1; }
export ZIG=${ZIG:-$HOME/.local/share/mise/installs/zig/0.16.0/zig}
nice -n 10 scripts/ghostty-lib.sh native
cache=${GHOSTTY_VT_CACHE:-$root/.ghostty-vt}
export PKG_CONFIG_PATH="$cache/native/pkgconfig" CGO_ENABLED=1
rev=$(git rev-parse HEAD)
ld="-s -w -X main.builtBy=local-release -X main.commit=$rev -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.version=local+${rev:0:12}"
mkdir -p .dev/bin
binary=$root/.dev/bin/tuios-local
nice -n 10 go build -tags ghostty -trimpath -ldflags "$ld" -o "$binary" ./cmd/tuios
version=$("$binary" --version)
printf '%s\n' "$version"
[[ "$version" == *'[ghostty backend]'* ]] || { echo 'Not a ghostty binary' >&2; exit 1; }
if ! "$install"; then
    echo "Built $binary; no backup, install or restart performed."
    exit 0
fi
dest=$HOME/.local/bin/tuios
backup=$HOME/.local/bin/tuios-0.12.1.bak
mkdir -p "$(dirname "$dest")"
# -n refuses to clobber even if another invocation wins the existence race.
if [ -e "$dest" ] && [ ! -e "$backup" ] && [ ! -L "$backup" ]; then
    cp -p -n "$dest" "$backup"
fi
tmp=$(mktemp "$HOME/.local/bin/.tuios-local.XXXXXX")
trap 'rm -f "$tmp"' EXIT
cp "$binary" "$tmp"
chmod 755 "$tmp"
mv -f "$tmp" "$dest"
trap - EXIT
# Deliberately target the user's local daemon, not an inherited pane/socket.
unset TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_WINDOW_ID TUIOS_SOCKET TUIOS_HOST TUIOS_SSH TUIOS_SERVER
"$dest" restart-server
