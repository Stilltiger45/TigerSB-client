#!/usr/bin/env bash
# Проверяет лицензии Go-зависимостей клиента (GPL-3.0). Падает, если у
# зависимости нет лицензии или она несовместима (AGPL, SSPL, BSL, Commons Clause).
# С флагом --notices печатает таблицу для THIRD_PARTY_NOTICES.md.
# Запускать из корня репозитория.
set -euo pipefail

notices=0
[ "${1:-}" = "--notices" ] && notices=1

kind() {
	local f=$1
	# В тексте MPL-2.0 упоминается AGPL как «вторичная лицензия», поэтому MPL проверяется первым.
	if grep -qiE 'Mozilla Public License' "$f"; then echo MPL-2.0; return; fi
	# Текст GPL-3.0 сам упоминает AGPL (раздел 13), поэтому AGPL узнаётся по заголовку.
	if head -n 5 "$f" | grep -qi 'GNU AFFERO' || grep -qiE 'SERVER SIDE PUBLIC LICENSE|Business Source License|Commons Clause' "$f"; then echo BAD; return; fi
	if grep -qiE 'GNU General Public License' "$f"; then echo GPL-3.0; return; fi
	if grep -qiE 'GNU Lesser General Public' "$f"; then echo LGPL; return; fi
	if grep -qiE 'Apache License' "$f"; then echo Apache-2.0; return; fi
	if grep -qiE 'MIT License|Permission is hereby granted' "$f"; then echo MIT; return; fi
	if grep -qiE 'Redistribution and use' "$f"; then echo BSD; return; fi
	echo UNKNOWN
}

bad=0
[ "$notices" = 1 ] && printf '| Модуль | Версия | Лицензия |\n|---|---|---|\n'
while read -r dir path version; do
	[ -n "$dir" ] || continue
	file=$(find "$dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' \) | sort | head -n 1)
	if [ -z "$file" ]; then
		echo "НЕТ ЛИЦЕНЗИИ: $path" >&2
		bad=1
		continue
	fi
	k=$(kind "$file")
	if [ "$k" = BAD ] || [ "$k" = UNKNOWN ]; then
		echo "НЕСОВМЕСТИМАЯ ИЛИ НЕОПОЗНАННАЯ ЛИЦЕНЗИЯ: $path ($file)" >&2
		bad=1
		continue
	fi
	if [ "$notices" = 1 ]; then printf '| %s | %s | %s |\n' "$path" "$version" "$k"; else printf '%-60s %s\n' "$path" "$k"; fi
done < <(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Dir}} {{.Path}} {{.Version}}{{end}}{{end}}' ./cmd/tigersb-core ./mobile | sort -u -k2)
exit "$bad"
