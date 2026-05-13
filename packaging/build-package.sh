#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C
export LANG=C

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_NAME="${PACKAGE_NAME:-alterego}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
VERSION="${VERSION:-$(git -C "${ROOT_DIR}" rev-parse --short HEAD)}"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/dist}"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/alterego-package.XXXXXX")"
STAGE_DIR="${TMP_DIR}/${PACKAGE_NAME}"
ARCHIVE_NAME="${PACKAGE_NAME}-${GOOS}-${GOARCH}-${VERSION}.tar.gz"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_NAME}"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p \
	"${STAGE_DIR}/opt/alterego/bin" \
	"${STAGE_DIR}/opt/alterego/config/configs/machines" \
	"${STAGE_DIR}/opt/alterego/config/configs/repositories" \
	"${STAGE_DIR}/opt/alterego/config/configs/templates" \
	"${STAGE_DIR}/opt/alterego/config/docs/workflows" \
	"${STAGE_DIR}/etc/alterego" \
	"${STAGE_DIR}/etc/systemd/system" \
	"${STAGE_DIR}/var/lib/alterego" \
	"${OUTPUT_DIR}"

(
	cd "${ROOT_DIR}"
	CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build -o "${STAGE_DIR}/opt/alterego/bin/alterego" ./cmd/alterego
)

chmod +x "${STAGE_DIR}/opt/alterego/bin/alterego"

cp "${ROOT_DIR}/packaging/templates/alteregod.service" "${STAGE_DIR}/etc/systemd/system/alteregod.service"
cp "${ROOT_DIR}/packaging/templates/alterego.env.example" "${STAGE_DIR}/etc/alterego/alterego.env.example"

cp "${ROOT_DIR}/configs/machines/example.yaml" "${STAGE_DIR}/opt/alterego/config/configs/machines/example.yaml"
cp "${ROOT_DIR}/configs/repositories/example.yaml" "${STAGE_DIR}/opt/alterego/config/configs/repositories/example.yaml"
cp "${ROOT_DIR}/configs/templates/example.yaml" "${STAGE_DIR}/opt/alterego/config/configs/templates/example.yaml"
cp "${ROOT_DIR}/docs/workflows/example-feature-dev.md" "${STAGE_DIR}/opt/alterego/config/docs/workflows/example-feature-dev.md"

cp "${ROOT_DIR}/README.md" "${STAGE_DIR}/opt/alterego/README.md"
cp "${ROOT_DIR}/LICENSE" "${STAGE_DIR}/opt/alterego/LICENSE"

tar -C "${TMP_DIR}" -czf "${ARCHIVE_PATH}" "${PACKAGE_NAME}"

printf '%s\n' "${ARCHIVE_PATH}"
