#!/usr/bin/env bash
# Shared helpers for install-system.sh and update.sh: locating/installing the
# binary, either by downloading a prebuilt release asset (no Go required) or,
# failing that, building from source. Sourced — not run directly.

REPO="mikegio27/nereus"
BIN="${BIN:-/usr/local/bin/nereus}"

# Bounded curl: fail on HTTP errors, follow redirects, and never hang forever on a
# slow/blocked host (the GitHub API and codeload live on different hosts than the
# raw script, so they can stall independently). Used for every network call here.
gir_curl() {
	curl -fsSL --connect-timeout 20 --max-time 300 "$@"
}

# detect_arch maps uname -m to the Go arch used in release asset names. Prints an
# empty string for unsupported machines (callers fall back to a source build).
detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) echo "" ;;
	esac
}

# latest_release_tag prints the newest published release tag, or empty if there
# are no releases yet / the API is unreachable / rate-limited. It is best-effort
# and ALWAYS exits 0: a caller doing `tag="$(latest_release_tag)"` under
# `set -e` must not be aborted on failure.
#
# Capture the response into a variable before parsing it. Piping curl straight
# into `grep -m1` makes grep close the pipe after the first match, and curl —
# still writing the (large) release JSON — then dies with a write error (exit 23),
# which `set -e`+`pipefail` would turn into a fatal install abort.
latest_release_tag() {
	local json
	json="$(gir_curl "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null)" || return 0
	printf '%s\n' "$json" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || return 0
}

# source_version derives a version string for a source build: the git description
# if this is a checkout, otherwise "dev".
source_version() {
	git -C "${REPO_ROOT:-.}" describe --tags --always 2>/dev/null || echo dev
}

# download_release_binary <tag> <arch> downloads, checksum-verifies, and installs
# the prebuilt binary for the given release. Returns non-zero on any failure so
# the caller can fall back to a source build.
download_release_binary() {
	local tag="$1" arch="$2" tmp asset url
	asset="nereus_linux_${arch}.tar.gz" # must match .goreleaser.yaml name_template
	url="https://github.com/$REPO/releases/download/$tag/$asset"
	tmp="$(mktemp -d)"

	echo "==> downloading prebuilt binary $asset ($tag)"
	if ! gir_curl "$url" -o "$tmp/$asset"; then
		rm -rf "$tmp"
		return 1
	fi

	# Verify against the release checksums when available.
	if gir_curl "https://github.com/$REPO/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt"; then
		if ! (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c -); then
			echo "checksum verification failed for $asset" >&2
			rm -rf "$tmp"
			return 1
		fi
	else
		echo "warning: no checksums.txt for $tag; skipping verification" >&2
	fi

	tar xzf "$tmp/$asset" -C "$tmp"
	install -m 0755 "$tmp/nereus" "$BIN"
	rm -rf "$tmp"
	echo "==> installed $BIN ($tag)"
}

# build_from_source compiles the binary from REPO_ROOT, stamping the version.
# Errors out with guidance if Go is not installed.
build_from_source() {
	if ! command -v go >/dev/null 2>&1; then
		echo "no prebuilt binary is available for this platform and Go is not installed." >&2
		echo "install Go (https://go.dev/dl) and re-run, or download a release binary" >&2
		echo "manually from https://github.com/$REPO/releases" >&2
		exit 1
	fi
	echo "==> building from source ($(source_version))"
	(cd "$REPO_ROOT" && go build \
		-ldflags "-X github.com/mikegio27/nereus/cmd.version=$(source_version)" \
		-o "$BIN" .)
	echo "==> installed $BIN (source build)"
}

# install_binary installs the binary, preferring a prebuilt release asset and
# falling back to a source build. Honors:
#   GIR_BUILD_FROM_SOURCE=1  force a source build (skip the download)
#   GIR_VERSION=vX.Y.Z       install a specific release instead of the latest
install_binary() {
	local arch tag
	arch="$(detect_arch)"

	if [[ "${GIR_BUILD_FROM_SOURCE:-0}" != "1" && -n "$arch" ]]; then
		if [[ -n "${GIR_VERSION:-}" ]]; then
			tag="$GIR_VERSION"
		else
			echo "==> checking for the latest release (api.github.com)…"
			tag="$(latest_release_tag || true)" # never let a query failure abort under set -e
		fi
		if [[ -n "$tag" ]]; then
			if download_release_binary "$tag" "$arch"; then
				return 0
			fi
			echo "==> release download failed; falling back to building from source" >&2
		else
			echo "==> could not determine the latest release (api.github.com unreachable or rate-limited); building from source" >&2
		fi
	fi
	build_from_source
}
