#!/bin/sh
# Installs the latest Tutortop release on Linux or macOS:
#
#   curl -fsSL https://raw.githubusercontent.com/LukasKH/tutortop/main/install.sh | sh
#
# Everything runs from main at the bottom, so a download cut off halfway runs nothing.
set -eu

repo=LukasKH/tutortop
releases=https://github.com/$repo/releases
latest=$releases/latest/download
raw=https://raw.githubusercontent.com/$repo/main

main() {
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

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

	if ! has_linux_libs; then
		if command -v apt-get >/dev/null 2>&1; then
			$sudo apt-get update
			$sudo apt-get install -y libgtk-4-1 libwebkitgtk-6.0-4
		elif command -v dnf >/dev/null 2>&1; then
			$sudo dnf install -y gtk4 webkitgtk6.0
		elif command -v pacman >/dev/null 2>&1; then
			$sudo pacman -S --needed --noconfirm gtk4 webkitgtk-6.0
		else
			install_appimage
			return
		fi
	fi

	# Install into ~/.local/bin so in-app updates work without sudo.
	download tutortop-linux-amd64.tar.gz
	tar -xzf "$tmp/tutortop-linux-amd64.tar.gz" -C "$tmp"
	mkdir -p "$HOME/.local/bin"
	mv "$tmp/tutortop" "$HOME/.local/bin/tutortop"
	chmod +x "$HOME/.local/bin/tutortop"

	install_linux_desktop
	echo "Tutortop is installed in ~/.local/bin. Open it from your app launcher, or run: tutortop"
	warn_linux_path
}

has_linux_libs() {
	command -v ldconfig >/dev/null 2>&1 &&
		ldconfig -p | grep -q 'libgtk-4\.so\.1' &&
		ldconfig -p | grep -q 'libwebkitgtk-6\.0\.so\.4'
}

install_linux_desktop() {
	data_home=${XDG_DATA_HOME:-$HOME/.local/share}
	icon_dir=$data_home/icons/hicolor/1024x1024/apps
	app_dir=$data_home/applications
	mkdir -p "$icon_dir" "$app_dir"

	# Best-effort icon download so the app launcher entry shows Tutortop's logo.
	curl -fsSL -o "$icon_dir/tutortop.png" "$raw/build/appicon.png" 2>/dev/null || true

	cat >"$app_dir/tutortop.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Tutortop
Comment=Learn anything you can prove with code
Exec=$HOME/.local/bin/tutortop
Icon=tutortop
Terminal=false
Categories=Development;Education;
Keywords=learning;tutor;programming;
StartupWMClass=tutortop
EOF

	command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$app_dir" 2>/dev/null || true
	command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$data_home/icons/hicolor" 2>/dev/null || true
}

warn_linux_path() {
	if [ -x /usr/bin/tutortop ]; then
		echo "Note: /usr/bin/tutortop is also installed from an older system package."
		echo "Remove it (e.g. sudo pacman -R tutortop / sudo apt remove tutortop) so ~/.local/bin/tutortop is used."
	fi
	case ":$PATH:" in
	*":$HOME/.local/bin:"*) ;;
	*) echo "Add $HOME/.local/bin to your PATH, or run $HOME/.local/bin/tutortop." ;;
	esac
}

# No apt, dnf or pacman and GTK4/WebKitGTK aren't installed: fall back to the AppImage.
install_appimage() {
	download tutortop-x86_64.AppImage
	mkdir -p "$HOME/.local/bin"
	mv "$tmp/tutortop-x86_64.AppImage" "$HOME/.local/bin/tutortop"
	chmod +x "$HOME/.local/bin/tutortop"
	install_linux_desktop
	echo "Tutortop is installed in ~/.local/bin. Run: tutortop"
	warn_linux_path
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

	# Install into a user-writable Applications folder so in-app updates work without admin rights.
	if [ -w /Applications ]; then
		apps_dir=/Applications
	else
		apps_dir=$HOME/Applications
		mkdir -p "$apps_dir"
	fi
	target="$apps_dir/$(basename "$app")"
	rm -rf "$target"
	cp -R "$app" "$target"
	echo "Tutortop is installed in $apps_dir."
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
