// Package secretvalue пишет и читает значения ключей в файлах секретов.
//
// Форматы разные, но операция одна: заменить или удалить ключ, не
// потеряв остальные и не сломав порядок файла.
package secretvalue

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format — формат файла секрета.
type Format string

// Форматы файлов секретов.
const (
	// FormatEnv — формат KEY=VALUE.
	FormatEnv Format = "env"
	// FormatYAML — yaml-документ.
	FormatYAML Format = "yaml"
	// FormatJSON — json-документ.
	FormatJSON Format = "json"
)

// Set заменяет значение ключа или добавляет его.
//
// Значение хранится как есть: кавычки и экранирование — забота
// формата, а не вызывающего.
func Set(format Format, content []byte, key, value string) ([]byte, error) {
	switch format {
	case FormatEnv:
		return setEnv(content, key, value)
	case FormatYAML:
		return setYAML(content, key, value)
	case FormatJSON:
		return setJSON(content, key, value)
	default:
		return nil, fmt.Errorf("формат не поддерживается: %s", format)
	}
}

// Unset удаляет ключ из файла.
//
// Отсутствующий ключ — не ошибка: удалять нечего.
func Unset(format Format, content []byte, key string) ([]byte, error) {
	switch format {
	case FormatEnv:
		return unsetEnv(content, key)
	case FormatYAML:
		return unsetYAML(content, key)
	case FormatJSON:
		return unsetJSON(content, key)
	default:
		return nil, fmt.Errorf("формат не поддерживается: %s", format)
	}
}

// Get возвращает значение ключа.
func Get(format Format, content []byte, key string) (string, bool, error) {
	switch format {
	case FormatEnv:
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			name, value, found := strings.Cut(trimmed, "=")
			if !found || strings.TrimSpace(name) != key {
				continue
			}
			return unquote(strings.TrimSpace(value)), true, nil
		}
		return "", false, nil
	case FormatYAML:
		var document map[string]any
		if err := yaml.Unmarshal(content, &document); err != nil {
			return "", false, fmt.Errorf("не удалось разобрать yaml: %w", err)
		}
		value, found := document[key]
		if !found {
			return "", false, nil
		}
		return scalar(value), true, nil
	case FormatJSON:
		var document map[string]any
		if err := yaml.Unmarshal(content, &document); err != nil {
			return "", false, fmt.Errorf("не удалось разобрать json: %w", err)
		}
		value, found := document[key]
		if !found {
			return "", false, nil
		}
		return scalar(value), true, nil
	default:
		return "", false, fmt.Errorf("формат не поддерживается: %s", format)
	}
}

// Keys возвращает имена ключей файла в алфавитном порядке.
func Keys(format Format, content []byte) ([]string, error) {
	switch format {
	case FormatEnv:
		var names []string
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			name, _, found := strings.Cut(trimmed, "=")
			if !found {
				continue
			}
			names = append(names, strings.TrimSpace(name))
		}
		sort.Strings(names)
		return names, nil
	case FormatYAML, FormatJSON:
		var document map[string]any
		if err := yaml.Unmarshal(content, &document); err != nil {
			return nil, fmt.Errorf("не удалось разобрать документ: %w", err)
		}
		names := make([]string, 0, len(document))
		for name := range document {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	default:
		return nil, fmt.Errorf("формат не поддерживается: %s", format)
	}
}

// setEnv заменяет или добавляет ключ в dotenv-файле.
//
// Строки сохраняются как есть: комментарии и порядок не меняются,
// иначе diff в хранилище превратился бы в шум.
func setEnv(content []byte, key, value string) ([]byte, error) {
	lines := strings.Split(string(content), "\n")
	replaced := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, _, found := strings.Cut(trimmed, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		lines[index] = key + "=" + quote(value)
		replaced = true
		break
	}
	if replaced {
		return []byte(strings.Join(lines, "\n")), nil
	}
	// Новый ключ дописывается в конец, перед завершающим переводом
	// строки: файл обязан заканчиваться переводом строки.
	trailing := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailing {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, key+"="+quote(value))
	if trailing {
		lines = append(lines, "")
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// unsetEnv удаляет ключ из dotenv-файла.
func unsetEnv(content []byte, key string) ([]byte, error) {
	lines := strings.Split(string(content), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			name, _, found := strings.Cut(trimmed, "=")
			if found && strings.TrimSpace(name) == key {
				continue
			}
		}
		kept = append(kept, line)
	}
	return []byte(strings.Join(kept, "\n")), nil
}

// setYAML заменяет или добавляет ключ в yaml-документе.
//
// Комментарии сохраняются: документ правится через yaml.Node, а не
// пересобирается из карты.
func setYAML(content []byte, key, value string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("не удалось разобрать yaml: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode, Tag: "!!map"},
		}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("ожидался yaml-документ с отображением")
	}
	setMapValue(root, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	return marshalYAML(&doc)
}

// unsetYAML удаляет ключ из yaml-документа.
func unsetYAML(content []byte, key string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("не удалось разобрать yaml: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return content, nil
	}
	removeMapValue(doc.Content[0], key)
	return marshalYAML(&doc)
}

// setJSON заменяет или добавляет ключ в json-документе.
//
// Формат json не поддерживает комментарии, поэтому документ
// пересобирается: порядок ключей сохраняется через сортировку.
func setJSON(content []byte, key, value string) ([]byte, error) {
	var document map[string]any
	if len(strings.TrimSpace(string(content))) > 0 {
		if err := yaml.Unmarshal(content, &document); err != nil {
			return nil, fmt.Errorf("не удалось разобрать json: %w", err)
		}
	}
	if document == nil {
		document = map[string]any{}
	}
	document[key] = value
	return marshalJSON(document)
}

// unsetJSON удаляет ключ из json-документа.
func unsetJSON(content []byte, key string) ([]byte, error) {
	var document map[string]any
	if len(strings.TrimSpace(string(content))) == 0 {
		return content, nil
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("не удалось разобрать json: %w", err)
	}
	if _, found := document[key]; !found {
		return content, nil
	}
	delete(document, key)
	return marshalJSON(document)
}

// marshalYAML сериализует yaml-документ.
func marshalYAML(doc *yaml.Node) ([]byte, error) {
	var builder strings.Builder
	encoder := yaml.NewEncoder(&builder)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("не удалось собрать yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("не удалось собрать yaml: %w", err)
	}
	return []byte(builder.String()), nil
}

// marshalJSON сериализует json-документ с отсортированными ключами.
//
// Сортировка нужна для стабильности diff'а: иначе порядок ключей
// менялся бы при каждой записи.
func marshalJSON(document map[string]any) ([]byte, error) {
	names := make([]string, 0, len(document))
	for name := range document {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	builder.WriteString("{\n")
	for index, name := range names {
		builder.WriteString("  " + quoteJSON(name) + ": " + quoteJSON(scalar(document[name])))
		if index < len(names)-1 {
			builder.WriteString(",")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("}\n")
	return []byte(builder.String()), nil
}

// setMapValue задаёт или добавляет значение ключа.
func setMapValue(node *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			node.Content[index+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// removeMapValue убирает ключ из отображения.
func removeMapValue(node *yaml.Node, key string) {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != key {
			continue
		}
		node.Content = append(node.Content[:index], node.Content[index+2:]...)
		return
	}
}

// scalar приводит значение к строке.
func scalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// quote экранирует значение для dotenv-формата.
//
// Значение с пробелами или спецсимволами оборачивается в кавычки:
// иначе dotenv-парсер прочёл бы его неверно.
func quote(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\"'#\n\r") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}

// unquote снимает кавычки вокруг значения.
func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if first == last && (first == '"' || first == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// quoteJSON экранирует значение для json-формата.
func quoteJSON(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(value) + `"`
}
