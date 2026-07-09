#!/bin/sh
# Installs the lytecache CLI: detects OS/arch, downloads the matching
# release asset, verifies its SHA-256 checksum, and installs the binary --
# no sudo, no package manager. Usage:
#
#   curl -fsSL https://raw.githubusercontent.com/lytecache/lytecache-cli/main/install.sh | sh
#
# Set VERSION to install a specific tag (e.g. VERSION=v0.2.0 sh install.sh)
# instead of the latest release, and INSTALL_DIR to override the target
# directory.
set -eu

REPO="lytecache/lytecache-cli"
VERSION="${VERSION:-latest}"

os="$(uname -s)"
case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*)
		echo "error: unsupported OS: $os (Windows users: see scoop/winget in the README instead)" >&2
		exit 1
		;;
esac

arch="$(uname -m)"
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*)
		echo "error: unsupported architecture: $arch" >&2
		exit 1
		;;
esac

# The archive filename embeds the version, so a concrete tag is needed even
# for the "latest" case -- ask GitHub which tag "latest" actually resolves to.
if [ "$VERSION" = "latest" ]; then
	tag="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
else
	tag="$VERSION"
fi
base_url="https://github.com/$REPO/releases/download/$tag"
asset="lytecache_${tag#v}_${os}_${arch}.tar.gz"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "Downloading $asset..." >&2
curl -fsSL -o "$workdir/$asset" "$base_url/$asset"
curl -fsSL -o "$workdir/checksums.txt" "$base_url/checksums.txt"

echo "Verifying checksum..." >&2
(
	cd "$workdir"
	if command -v sha256sum >/dev/null 2>&1; then
		grep " $asset\$" checksums.txt | sha256sum -c -
	else
		grep " $asset\$" checksums.txt | shasum -a 256 -c -
	fi
)

tar -xzf "$workdir/$asset" -C "$workdir" lytecache

install_dir="${INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
	if [ -w /usr/local/bin ]; then
		install_dir=/usr/local/bin
	else
		install_dir="$HOME/.local/bin"
		mkdir -p "$install_dir"
	fi
fi

install -m 755 "$workdir/lytecache" "$install_dir/lytecache"
echo "Installed lytecache to $install_dir/lytecache" >&2

case ":$PATH:" in
	*":$install_dir:"*) ;;
	*) echo "note: $install_dir is not on your PATH -- add it to your shell profile" >&2 ;;
esac

"$install_dir/lytecache" --version
