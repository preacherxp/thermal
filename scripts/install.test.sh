#!/bin/sh
# Offline integration test: real archives/hashes, mocked GitHub downloads and uname.
set -eu
root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
export FIXTURE="$fixture"
mkdir -p "$fixture/bin" "$fixture/payload"
printf '#!/bin/sh\necho fixture executable\n' > "$fixture/payload/thermal"
for platform in linux darwin; do
    for arch in amd64 arm64; do
        tar -czf "$fixture/thermal-$platform-$arch.tar.gz" -C "$fixture/payload" thermal
    done
done
hash_archives() {
    (cd "$fixture"
     if command -v sha256sum >/dev/null 2>&1; then sha256sum ./*.tar.gz
     else shasum -a 256 ./*.tar.gz; fi) | sed 's| [ *]\./|  |' > "$fixture/checksums.txt"
}
hash_archives
cat > "$fixture/bin/uname" <<'EOF'
#!/bin/sh
case "$1" in -s) echo "$TEST_OS" ;; -m) echo "$TEST_ARCH" ;; *) exit 1 ;; esac
EOF
cat > "$fixture/bin/curl" <<'EOF'
#!/bin/sh
set -eu
[ "${FAIL_DOWNLOAD:-0}" = 0 ] || exit 22
url=''
output=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        https://*) url=$1 ;;
        -o) shift; output=$1 ;;
    esac
    shift
done
case "$url" in https://github.com/test-owner/thermal/releases/download/v1.2.3/*) ;; *) echo "Unexpected URL: $url" >&2; exit 1 ;; esac
cp "$FIXTURE/${url##*/}" "$output"
EOF
chmod +x "$fixture/bin/uname" "$fixture/bin/curl"
export PATH="$fixture/bin:$PATH"
export THERMAL_REPOSITORY=test-owner/thermal THERMAL_VERSION=v1.2.3
export THERMAL_INSTALL_DIR="$fixture/install with spaces"
for TEST_OS in Linux Darwin; do
    for TEST_ARCH in x86_64 aarch64 arm64; do
        export TEST_OS TEST_ARCH
        sh "$root/install.sh"
        cmp "$fixture/payload/thermal" "$THERMAL_INSTALL_DIR/thermal"
        [ -x "$THERMAL_INSTALL_DIR/thermal" ]
    done
done
# A packaged installer must work with its pinned defaults and no overrides.
sed 's|@VERSION@|v1.2.3|g; s|preacherxp/thermal|test-owner/thermal|g' "$root/install.sh" > "$fixture/pinned.sh"
unset THERMAL_VERSION THERMAL_REPOSITORY
sh "$fixture/pinned.sh"
export THERMAL_VERSION=v1.2.3 THERMAL_REPOSITORY=test-owner/thermal
printf 'existing installation\n' > "$THERMAL_INSTALL_DIR/thermal"
cp "$THERMAL_INSTALL_DIR/thermal" "$fixture/previous"
expect_failure() {
    if sh "$root/install.sh" > "$fixture/stdout" 2> "$fixture/stderr"; then
        echo 'Expected installer to fail' >&2; exit 1
    fi
    cmp "$fixture/previous" "$THERMAL_INSTALL_DIR/thermal"
}
export TEST_OS=Linux TEST_ARCH=x86_64
printf 'corrupt download\n' >> "$fixture/thermal-linux-amd64.tar.gz"
expect_failure
grep -q 'Checksum mismatch' "$fixture/stderr"
export FAIL_DOWNLOAD=1
expect_failure
unset FAIL_DOWNLOAD
export TEST_ARCH=i686
expect_failure
export TEST_ARCH=x86_64 TEST_OS=FreeBSD
expect_failure
export TEST_OS=Linux THERMAL_VERSION=../../bad
expect_failure
export THERMAL_VERSION=v1.2.3
printf '' > "$fixture/checksums.txt"
expect_failure
grep -q 'Missing or duplicate' "$fixture/stderr"
echo 'Shell installer tests passed.'
