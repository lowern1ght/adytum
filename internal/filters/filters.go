// Package filters генерирует скрипты-обёртки для git-фильтров adytum.
//
// Фильтры запускаются git'ом через shell, поэтому обёртки обязаны быть
// отдельными файлами, а не inline-строками в git config. Причины:
//
//   - clean на пустом файле: sops падает с «must contain at least one
//     document», и git-операция ломается. Обёртка пропускает пустой
//     файл без изменений.
//   - smudge на машине, которая не в списке получателей: sops съедает
//     stdin и падает, в рабочее дерево попадает пустой файл. Обёртка
//     при неудаче возвращает исходный шифротекст.
//
// Git передаёт фильтру содержимое файла через stdin, а путь к файлу —
// аргументом (%f в git config). Отсюда две особенности обёрток:
//
//   - читать нужно stdin, а не файл по пути;
//   - путь из %f нужен sops через --filename-override: по нему sops
//     определяет тип файла и выбирает правило creation_rules.
//
// Скрипты одинаковы для Linux и Windows: git под Git-for-Windows умеет
// исполнять sh-скрипты, а $HOME в них указывает на домашний каталог
// пользователя.
package filters

import (
	"fmt"
	"os"
	"path/filepath"
)

// Имена файлов скриптов-фильтров.
const (
	// CleanName — имя файла clean-фильтра.
	CleanName = "adytum-clean.sh"
	// SmudgeName — имя файла smudge-фильтра.
	SmudgeName = "adytum-smudge.sh"
)

// Scripts — пути к установленным скриптам фильтров.
type Scripts struct {
	// Clean — путь к clean-фильтру.
	Clean string
	// Smudge — путь к smudge-фильтру.
	Smudge string
}

// CleanScript — обёртка clean-фильтра.
//
// Шифрует содержимое файла через sops. Пустые файлы (в которых нет ни
// одного символа, кроме пробелов) передаются без изменений: sops не
// умеет шифровать пустой документ.
//
// Ключ машины шифрованию не нужен: sops берёт получателей из
// .sops.yaml, а приватный ключ требуется только для расшифровки.
func CleanScript() string {
	return `#!/bin/sh
# adytum clean-фильтр: шифрует файл при записи в индекс git.
#
# Сгенерировано командой 'adytum init'. Правьте осторожно: ключ сразу
# после # меняет поведение фильтра.
#
# Git передаёт содержимое файла через stdin, а путь — аргументом.
set -e

file="$1"

tmp_in=$(mktemp)
trap 'rm -f "$tmp_in"' EXIT INT TERM

cat > "$tmp_in"

# Пустой документ sops зашифровать не может: отдаём файл как есть.
if [ -z "$(tr -d '[:space:]' < "$tmp_in")" ]; then
	cat "$tmp_in"
	exit 0
fi

# Ошибка шифрования не должна приводить к plaintext в индексе: при
# неудаче фильтр падает, и операция git откатывается.
sops encrypt --filename-override "$file" < "$tmp_in"
`
}

// SmudgeScript — обёртка smudge-фильтра.
//
// Расшифровывает содержимое файла, полученного из индекса git. Если
// расшифровка не удалась (машина не получатель, нет ключа, файл не
// зашифрован) — возвращает исходные байты: операции git не должны
// падать из-за частичного доступа.
//
// Ключ этой машины подставляется при установке.
func SmudgeScript(keyFile string) string {
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

tmp_in=$(mktemp)
tmp_out=$(mktemp)
trap 'rm -f "$tmp_in" "$tmp_out"' EXIT INT TERM

cat > "$tmp_in"

# Пустой файл расшифровывать нечего.
if [ ! -s "$tmp_in" ]; then
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
`, shellPath(keyFile))
}

// Install записывает скрипты фильтров в каталог и возвращает их пути.
//
// Каталог создаётся, если его ещё нет. Скрипты получают права 0700:
// ими пользуется только владелец машины.
func Install(dir, keyFile string) (Scripts, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Scripts{}, fmt.Errorf("не удалось создать каталог фильтров %s: %w", dir, err)
	}
	scripts := Scripts{
		Clean:  filepath.Join(dir, CleanName),
		Smudge: filepath.Join(dir, SmudgeName),
	}
	if err := os.WriteFile(scripts.Clean, []byte(CleanScript()), scriptMode); err != nil {
		return Scripts{}, fmt.Errorf("не удалось записать clean-фильтр %s: %w", scripts.Clean, err)
	}
	if err := os.WriteFile(scripts.Smudge, []byte(SmudgeScript(keyFile)), scriptMode); err != nil {
		return Scripts{}, fmt.Errorf("не удалось записать smudge-фильтр %s: %w", scripts.Smudge, err)
	}
	return scripts, nil
}

// scriptMode — права скриптов-фильтров: только владелец.
const scriptMode = 0o700

// ConfigValues — значения git config для фильтров adytum.
//
// Путь к скрипту приводится к виду, понятному shell: обратные слэши
// в Windows-путях интерпретируются как экранирование.
func ConfigValues(scripts Scripts) map[string]string {
	return map[string]string{
		"filter.adytum.clean":    fmt.Sprintf("%q %%f", shellPath(scripts.Clean)),
		"filter.adytum.smudge":   fmt.Sprintf("%q %%f", shellPath(scripts.Smudge)),
		"filter.adytum.required": "true",
	}
}

// shellPath приводит путь к виду, пригодному для shell-команды.
//
// Обратные слэши заменяются на прямые: иначе shell трактует их как
// экранирование и путь превращается в мусор.
func shellPath(path string) string {
	return filepath.ToSlash(path)
}
