package envfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document разбирает секрет в плоские пары ключ-значение.
//
// Формат выбирается по расширению файла:
//
//   - .env — формат KEY=VALUE;
//   - .yaml, .yml — плоские ключи верхнего уровня;
//   - .json — плоские ключи верхнего уровня.
//
// Вложенные структуры пропускаются: переменная окружения хранит одно
// значение, а не дерево.
func Document(path string, content []byte) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".env", ".txt":
		entries, err := Parse(content)
		if err != nil {
			return nil, err
		}
		values := make(map[string]string, len(entries))
		for _, entry := range entries {
			values[entry.Key] = entry.Value
		}
		return values, nil
	case ".yaml", ".yml":
		return yamlDocument(content)
	case ".json":
		return jsonDocument(content)
	default:
		return nil, fmt.Errorf("формат секрета не поддерживается: %s", path)
	}
}

// yamlDocument разбирает yaml с плоскими ключами.
func yamlDocument(content []byte) (map[string]string, error) {
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("не удалось разобрать yaml: %w", err)
	}
	return flatten(document), nil
}

// jsonDocument разбирает json с плоскими ключами.
func jsonDocument(content []byte) (map[string]string, error) {
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("не удалось разобрать json: %w", err)
	}
	return flatten(document), nil
}

// flatten оставляет только скалярные значения верхнего уровня.
//
// Вложенные объекты и списки пропускаются: их нельзя положить в
// переменную окружения без потери структуры.
func flatten(document map[string]any) map[string]string {
	values := make(map[string]string, len(document))
	for key, value := range document {
		switch typed := value.(type) {
		case string:
			values[key] = typed
		case bool:
			values[key] = fmt.Sprintf("%t", typed)
		case int:
			values[key] = fmt.Sprintf("%d", typed)
		case int64:
			values[key] = fmt.Sprintf("%d", typed)
		case float64:
			values[key] = fmt.Sprintf("%v", typed)
		case nil:
			values[key] = ""
		}
	}
	return values
}

// SetEnv заменяет или добавляет переменную окружения.
//
// Значение существующей переменной перезаписывается: повторный
// --env с одним и тем же ключом должен давать последнее значение.
func SetEnv(environment []string, key, value string) []string {
	prefix := key + "="
	for index, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

// EnvFile читает env-файл и возвращает его содержимое.
//
// Файл читается как есть: значения секретов не должны попадать в
// логи, поэтому ошибки чтения не включают содержимое.
func EnvFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	return content, nil
}
