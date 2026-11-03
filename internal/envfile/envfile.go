// Package envfile разбирает файлы формата KEY=VALUE (dotenv).
//
// Формат намеренно минимальный: одна запись на строку, "#" начинает
// комментарий, значение может быть в кавычках. Этого достаточно для
// секретов, которые adytum хранит в репозитории.
package envfile

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Entry — одна пара KEY=VALUE.
type Entry struct {
	Key   string
	Value string
}

// Template — содержимое нового файла-заготовки.
const Template = "# example KEY=\n"

// Parse разбирает содержимое dotenv-файла.
//
// Пустые строки и комментарии пропускаются. Префикс "export " допускается.
// Значение в кавычках снимается, внутри двойных кавычек раскрываются \n и \".
func Parse(content []byte) ([]Entry, error) {
	var entries []Entry
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		text = strings.TrimSpace(text)
		key, value, found := strings.Cut(text, "=")
		if !found {
			return nil, fmt.Errorf("строка %d: ожидалась пара KEY=VALUE", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("строка %d: пустой ключ", line)
		}
		entries = append(entries, Entry{Key: key, Value: unquote(strings.TrimSpace(value))})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// ParseFile читает и разбирает файл.
func ParseFile(path string) ([]Entry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(content)
}

// Lookup возвращает значение по ключу. Дубликаты: побеждает последний.
func Lookup(entries []Entry, key string) (string, bool) {
	found := false
	value := ""
	for _, entry := range entries {
		if entry.Key == key {
			value = entry.Value
			found = true
		}
	}
	return value, found
}

// unquote снимает обрамляющие кавычки и раскрывает базовые escape-последовательности.
func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if first == last && (first == '"' || first == '\'') {
			inner := value[1 : len(value)-1]
			if first == '\'' {
				return inner
			}
			replacer := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`)
			return replacer.Replace(inner)
		}
	}
	return value
}
