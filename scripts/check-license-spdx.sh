#!/bin/sh
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Fails when a covered source file names another license than this repository's own LICENSE.
# "addlicense -check" only proves a header exists, so it passes a file whose SPDX line says
# another license than LICENSE does. No existing tool compares the two: the mise registry has
# no license text detector, and conftest parses no source comments.
#
# Usage: check-license-spdx.sh [repository root, default .]
# The covered files are the ones "task lint:license" and the CI license job check.
set -eu

cd "${1:-.}"

text=$(head -n 5 LICENSE)
case "${text}" in
*"Apache License"*"Version 2.0"*) want="Apache-2.0" ;;
*"Elastic License 2.0"*) want="Elastic-2.0" ;;
*"MIT License"*) want="MIT" ;;
*)
	echo "LICENSE is not one of Apache-2.0, Elastic-2.0 or MIT; teach this script the new one" >&2
	exit 2
	;;
esac

report=$(find . \( -name "*.go" -o -name "*.sh" \) \
	-not -path "./.git/*" -not -path "./.claude/*" -not -path "./bin/*" -not -path "./dist/*" |
	while IFS= read -r file; do
		got=$(sed -n '1,10s/^.*SPDX-License-Identifier:[[:space:]]*//p' "${file}" | head -n 1)
		if [ "${got}" != "${want}" ]; then
			echo "${file}: SPDX identifier is '${got:-missing}', LICENSE is ${want}"
		fi
	done)

if [ -n "${report}" ]; then
	echo "${report}" >&2
	exit 1
fi
echo "every covered file names ${want}, the license of LICENSE"
