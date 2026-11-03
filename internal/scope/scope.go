// Package scope хранит настройки доступа машины к разделам хранилища.
//
// Настройка называется machine_scope: это список разделов, которые
// машина расшифровывает и материализует в рабочем дереве. Разделы
// вне списка остаются шифротекстом.
//
// Настройка декларативная и правится владельцем вручную: из меток
// машин её не вывести — метка говорит, кто машина, а не какие данные
// ей нужны.
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName — имя файла настройки в каталоге состояния adytum.
const FileName = "config"

// Config — настройки доступа машины.
type Config struct {
	// MachineScope — разделы, которые машина расшифровывает.
	MachineScope []string `yaml:"machine_scope"`
}

// Home возвращает каталог состояния adytum.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить домашний каталог: %w", err)
	}
	return filepath.Join(home, ".adytum"), nil
}

// Path возвращает путь к файлу настройки.
func Path() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, FileName), nil
}

// Load читает настройку доступа.
//
// Отсутствующий файл — не ошибка: машина без настройки просто не
// имеет доступа ни к одному разделу, и это видно в doctor.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("не удалось разобрать %s: %w", path, err)
	}
	config.MachineScope = normalize(config.MachineScope)
	return config, nil
}

// Save записывает настройку доступа.
func Save(config Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("не удалось создать каталог %s: %w", filepath.Dir(path), err)
	}
	config.MachineScope = normalize(config.MachineScope)
	content, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("не удалось собрать %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("не удалось записать %s: %w", path, err)
	}
	return nil
}

// Allows сообщает, входит ли раздел в scope машины.
//
// Раздел определяется первым компонентом пути: personal/github — это
// раздел personal.
func (c Config) Allows(section string) bool {
	if section == "" {
		return false
	}
	for _, allowed := range c.MachineScope {
		if allowed == section {
			return true
		}
	}
	return false
}

// Section возвращает раздел файла по пути внутри хранилища.
//
// Путь без разделителя относится к корню: его раздел — пустая строка,
// и такой файл ни в один scope не попадает.
func Section(path string) string {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	parts := strings.SplitN(normalized, "/", 2)
	if len(parts) < 2 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

// normalize убирает пустые значения и дубликаты, сортирует список.
func normalize(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
