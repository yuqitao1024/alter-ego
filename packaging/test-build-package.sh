#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C
export LANG=C

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/alterego-package-test.XXXXXX")"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

ARCHIVE_PATH="$(
	cd "${ROOT_DIR}"
	VERSION=test OUTPUT_DIR="${TMP_DIR}/dist" ./packaging/build-package.sh
)"

test -f "${ARCHIVE_PATH}"

tar -tzf "${ARCHIVE_PATH}" > "${TMP_DIR}/archive.txt"

grep -q 'alterego/opt/alterego/bin/alterego$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/etc/systemd/system/alteregod.service$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/etc/alterego/alterego.env.example$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/machines/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/repositories/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/templates/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/docs/workflows/example-feature-dev.md$' "${TMP_DIR}/archive.txt"

printf 'package smoke test passed: %s\n' "${ARCHIVE_PATH}"
