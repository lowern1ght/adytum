package filters

import (
	"fmt"
	"strings"
)

// MaterializeScript — обёртка smudge-фильтра с учётом machine_scope.
//
// Фильтр расшифровывает только разделы из списка. Файл вне списка
// остаётся шифротекстом: материализация лишних данных на машине —
// это ровно то, чего machine_scope и не даёт сделать.
//
// Ключ этой машины подставляется при установке, список разделов —
// тоже: он задаётся владельцем в ~/.adytum/config и меняется только
// через `adytum init`.
func MaterializeScript(keyFile string, sections []string) string {
	return fmt.Sprintf(`#!/bin/sh
# adytum smudge-фильтр: расшифровывает файл при выгрузке из git.
#
# Сгенерировано командой 'adytum init'. Правьте осторожно: ключ сразу
# после # меняет поведение фильтра.
#
# Git передаёт содержимое файла через stdin, а путь — аргументом.
set -e

file="$1"
key_file="%s"
sections="%s"

tmp_in=$(mktemp)
tmp_out=$(mktemp)
trap 'rm -f "$tmp_in" "$tmp_out"' EXIT INT TERM

cat > "$tmp_in"

# Пустой файл расшифровывать нечего.
if [ ! -s "$tmp_in" ]; then
	cat "$tmp_in"
	exit 0
fi

# Раздел файла — первый компонент пути. Файл вне machine_scope не
# расшифровывается: в рабочем дереве остаётся шифротекст.
section=$(printf '%%s' "$file" | sed 's|^\./||' | cut -d/ -f1)

allowed=false
for wanted in $sections; do
	if [ "$wanted" = "$section" ]; then
		allowed=true
		break
	fi
done

if [ "$allowed" != "true" ]; then
	cat "$tmp_in"
	exit 0
fi

# sops съедает stdin и падает, если расшифровка невозможна (машина не
# получатель). Поэтому при неудаче отдаём исходный шифротекст, а не
# пустой файл: наивный '|| cat' здесь даёт именно пустой файл.
if SOPS_AGE_KEY_FILE="$key_file" sops decrypt --filename-override "$file" < "$tmp_in" > "$tmp_out" 2>/dev/null; then
	cat "$tmp_out"
else
	cat "$tmp_in"
fi
`, shellPath(keyFile), shellList(sections))
}

// shellList готовит список разделов для shell.
//
// Значения разделяются пробелом: список короткий, а кавычки в
// переменной усложнили бы разбор.
func shellList(sections []string) string {
	cleaned := make([]string, 0, len(sections))
	for _, section := range sections {
		trimmed := strings.TrimSpace(section)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	return strings.Join(cleaned, " ")
}
