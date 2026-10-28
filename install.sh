#!/bin/sh
set -eu

repo="stevetosak/git-idm"
install_dir="${GIT_IDM_INSTALL_DIR:-$HOME/.local/bin}"
version="${GIT_IDM_VERSION:-latest}"
releases_url="${GIT_IDM_RELEASES_URL:-https://github.com/$repo/releases}"

say() { printf '%s\n' "$*"; }
fail() { printf 'git-idm install: %s\n' "$*" >&2; exit 1; }

detect_os() {
	case "$(uname -s)" in
		Darwin) echo darwin ;;
		Linux) echo linux ;;
		*) fail "unsupported OS $(uname -s), git-idm runs on macOS and Linux" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo amd64 ;;
		arm64 | aarch64) echo arm64 ;;
		*) fail "unsupported CPU $(uname -m), git-idm runs on amd64 and arm64" ;;
	esac
}

check_inputs() {
	case "$version" in
		latest) ;;
		*)
			printf '%s' "$version" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' ||
				fail "GIT_IDM_VERSION must look like v1.2.3, got: $version"
			case "$version" in v*) ;; *) version="v$version" ;; esac
			;;
	esac
	case "$releases_url" in
		https://* | file://*) ;;
		*) fail "GIT_IDM_RELEASES_URL must start with https://" ;;
	esac
	case "$install_dir" in
		/*) ;;
		*) fail "GIT_IDM_INSTALL_DIR must be an absolute path, got: $install_dir" ;;
	esac
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https,file' -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is needed"
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		fail "sha256sum or shasum is needed to check the download"
	fi
}

main() {
	check_inputs
	os="$(detect_os)"
	arch="$(detect_arch)"
	asset="git-idm_${os}_${arch}.tar.gz"
	if [ "$version" = latest ]; then
		base="$releases_url/latest/download"
	else
		base="$releases_url/download/$version"
	fi

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Downloading $asset ($version)"
	download "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"
	download "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

	want="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
	[ -n "$want" ] || fail "checksums.txt has no entry for $asset"
	got="$(sha256_of "$tmp/$asset")"
	[ "$got" = "$want" ] || fail "checksum mismatch for $asset, the download is not safe to use"

	tar -xzf "$tmp/$asset" -C "$tmp" git-idm || fail "the archive has no git-idm binary"
	mkdir -p "$install_dir"
	chmod 755 "$tmp/git-idm"
	mv -f "$tmp/git-idm" "$install_dir/git-idm.new"
	mv -f "$install_dir/git-idm.new" "$install_dir/git-idm"

	say "✓ installed $("$install_dir/git-idm" --version) to $install_dir/git-idm"
	case ":$PATH:" in
		*":$install_dir:"*) ;;
		*)
			say ""
			say "$install_dir is not on your PATH. Add this line to ~/.zshrc or ~/.bashrc:"
			say "  export PATH=\"$install_dir:\$PATH\""
			;;
	esac
	say ""
	say "Next: git-idm init    (imports your current git setup)"
	say "  or: git-idm add     (creates a new identity)"
}

main "$@"
