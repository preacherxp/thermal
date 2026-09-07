#!/bin/sh
set -eu

fail() { printf 'thermal installer: %s\n' "$*" >&2; exit 1; }

main() {
    repo=${THERMAL_REPOSITORY:-preacherxp/thermal}
    version=${THERMAL_VERSION:-@VERSION@}
    install_dir=${THERMAL_INSTALL_DIR:-"$HOME/.local/bin"}
    case "$version" in
        v[0-9]*) ;;
        *) fail 'Use a release installer, or set THERMAL_VERSION to a release tag such as v0.7.0.' ;;
    esac
    case "$version" in *[!A-Za-z0-9._-]*) fail 'Invalid release tag.' ;; esac
    case "$repo" in ''|*[!A-Za-z0-9._/-]*) fail 'Invalid repository.' ;; esac
    case "$(uname -s)" in
        Linux) platform=linux ;;
        Darwin) platform=darwin ;;
        *) fail 'Supported systems: Linux and macOS. On Windows use install.ps1 or download the ZIP release.' ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'Supported architectures: x64 and ARM64.' ;;
    esac
    for tool in curl tar mktemp; do command -v "$tool" >/dev/null 2>&1 || fail "Required command missing: $tool"; done
    if command -v sha256sum >/dev/null 2>&1; then hash_tool=sha256sum
    elif command -v shasum >/dev/null 2>&1; then hash_tool=shasum
    else fail 'A SHA-256 tool is required (sha256sum or shasum).'; fi

    archive="thermal-$platform-$arch.tar.gz"
    base="https://github.com/$repo/releases/download/$version"
    temp_dir=$(mktemp -d)
    trap 'rm -rf "$temp_dir"' EXIT
    trap 'exit 1' HUP INT TERM
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/$archive" -o "$temp_dir/$archive"
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$temp_dir/checksums.txt"
    expected=$(awk -v name="$archive" '$2 == name {print $1}' "$temp_dir/checksums.txt")
    [ "${#expected}" -eq 64 ] || fail 'Missing or duplicate archive checksum.'
    case "$expected" in *[!0-9a-fA-F]*) fail 'Invalid archive checksum.' ;; esac
    if [ "$hash_tool" = sha256sum ]; then actual=$(sha256sum "$temp_dir/$archive")
    else actual=$(shasum -a 256 "$temp_dir/$archive"); fi
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || fail 'Checksum mismatch; installation cancelled.'
    # Extract only the executable, never arbitrary paths from the archive.
    tar -xzf "$temp_dir/$archive" -C "$temp_dir" thermal
    [ -f "$temp_dir/thermal" ] && [ ! -L "$temp_dir/thermal" ] || fail 'Release contains no regular executable.'
    mkdir -p "$install_dir"
    # Stage in the destination filesystem and rename only after download and verification.
    staged=$(mktemp "$install_dir/.thermal.XXXXXX")
    trap 'rm -rf "$temp_dir"; [ -z "${staged:-}" ] || rm -f "$staged"' EXIT
    cp "$temp_dir/thermal" "$staged"
    chmod 755 "$staged"
    [ ! -d "$install_dir/thermal" ] || fail 'Install target is a directory.'
    mv -f "$staged" "$install_dir/thermal"
    staged=''
    printf 'Installed Thermal %s to %s/thermal\n' "$version" "$install_dir"
    case ":$PATH:" in
        *":$install_dir:"*) printf 'Run: thermal --help\n' ;;
        *) printf 'Add %s to PATH, then run: thermal --help\n' "$install_dir" ;;
    esac
}

main "$@"
