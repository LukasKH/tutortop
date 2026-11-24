#!/bin/sh
# Installs the latest Tutortop release on Linux or macOS:
#
#   curl -fsSL https://raw.githubusercontent.com/LukasKH/tutortop/main/install.sh | sh
#
# Everything runs from main at the bottom, so a download cut off halfway runs nothing.
set -eu

releases=https://github.com/LukasKH/tutortop/releases
latest=$releases/latest/download

main() {
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	# apt reads the .deb as its own unprivileged user.
	chmod 755 "$tmp"

	sudo=sudo
	if [ "$(id -u)" -eq 0 ]; then
		sudo=
	fi

	case "$(uname -s)" in
	Linux) install_linux ;;
	Darwin) install_macos ;;
	*) fail "There's no Tutortop for $(uname -s). Windows has install.ps1; see $releases" ;;
	esac
}

install_linux() {
	if [ "$(uname -m)" != x86_64 ]; then
		fail "Tutortop for Linux is built for x86_64 only, not $(uname -m)."
	fi

	if command -v apt-get >/dev/null 2>&1; then
		download tutortop_amd64.deb
		$sudo apt-get install -y "$tmp/tutortop_amd64.deb"
	elif command -v dnf >/dev/null 2>&1; then
		download tutortop-x86_64.rpm
		$sudo dnf install -y "$tmp/tutortop-x86_64.rpm"
	elif command -v pacman >/dev/null 2>&1; then
		download tutortop-x86_64.pkg.tar.zst
		$sudo pacman -U --noconfirm "$tmp/tutortop-x86_64.pkg.tar.zst"
	else
		install_appimage
		return
	fi
	echo "Tutortop is installed. Open it from your app launcher, or run: tutortop"
}

# No apt, dnf or pacman: the AppImage, which needs no root.
install_appimage() {
	download tutortop-x86_64.AppImage
	mkdir -p "$HOME/.local/bin"
	mv "$tmp/tutortop-x86_64.AppImage" "$HOME/.local/bin/tutortop"
	chmod +x "$HOME/.local/bin/tutortop"
	echo "Tutortop is installed in ~/.local/bin. Run: tutortop"
	case ":$PATH:" in
	*":$HOME/.local/bin:"*) ;;
	*) echo "Add $HOME/.local/bin to your PATH, or run $HOME/.local/bin/tutortop." ;;
	esac
}

# The app isn't signed by Apple. macOS checks unsigned apps only when they're
# marked as downloaded by a browser, and curl doesn't mark them, so it opens
# without the Gatekeeper prompt.
install_macos() {
	download tutortop-macos-universal.dmg
	mnt=$tmp/mnt
	trap 'hdiutil detach -quiet "$mnt" 2>/dev/null; rm -rf "$tmp"' EXIT
	mkdir "$mnt"
	hdiutil attach -nobrowse -readonly -quiet -mountpoint "$mnt" "$tmp/tutortop-macos-universal.dmg"

	app=$(find "$mnt" -maxdepth 1 -name '*.app' | head -n 1)
	if [ -z "$app" ]; then
		fail "The .dmg has no app in it."
	fi
	if [ -w /Applications ]; then
		sudo=
	fi
	target="/Applications/$(basename "$app")"
	$sudo rm -rf "$target"
	$sudo cp -R "$app" "$target"
	echo "Tutortop is installed in /Applications."
}

download() {
	echo "Downloading $1"
	curl -fL --progress-bar -o "$tmp/$1" "$latest/$1" ||
		fail "Couldn't download $latest/$1"
}

fail() {
	echo "$1" >&2
	exit 1
}

main
