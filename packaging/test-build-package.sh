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
grep -q 'alterego/etc/systemd/system/alterego-web.service$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/etc/alterego/alterego.env.example$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/machines/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/repositories/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/workspaces/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/templates/example.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/configs/templates/example-code-review.yaml$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/docs/workflows/example-feature-dev.md$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/config/docs/workflows/example-code-review.md$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/.agents/skills/ability-thinking-models/SKILL.md$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/.agents/skills/ability-work-methodology/SKILL.md$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/.agents/skills/ability-communication-skills/SKILL.md$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/.agents/ability-rag/rag-db/rag.sqlite$' "${TMP_DIR}/archive.txt"
grep -q 'alterego/opt/alterego/.agents/ability-rag/scripts/search_rag.py$' "${TMP_DIR}/archive.txt"

printf 'package smoke test passed: %s\n' "${ARCHIVE_PATH}"
