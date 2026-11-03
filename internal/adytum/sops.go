package adytum

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sops — обёртка над бинарём sops.
//
// Все вызовы идут с --filename-override: sops определяет тип файла по
// расширению и ищет .sops.yaml по тому же пути. Без этого sops не смог бы
// выбрать creation_rules для файлов в подкаталогах.
type Sops struct {
	// Runner выполняет внешние команды.
	Runner Runner
	// Root — корень репозитория (там лежит .sops.yaml).
	Root string
	// KeyFile — файл приватной идентичности age (SOPS_AGE_KEY_FILE).
	KeyFile string
}

// inputTypeFor возвращает тип входа sops для расширения файла.
func inputTypeFor(ext string) string {
	switch strings.ToLower(ext) {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return "dotenv"
	}
}

// Encrypt шифрует содержимое файла и возвращает шифротекст.
//
// path — путь относительно корня репозитория (например personal/git.env).
func (s Sops) Encrypt(path string, content []byte) ([]byte, error) {
	out, err := s.Runner.Run(s.command(path), "sops", "encrypt", "--filename-override", filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("не удалось зашифровать %s: %w", path, err)
	}
	return out, nil
}

// Decrypt расшифровывает содержимое файла.
//
// path — путь относительно корня репозитория.
func (s Sops) Decrypt(path string, content []byte) ([]byte, error) {
	out, err := s.Runner.Run(s.command(path), "sops", "decrypt", "--filename-override", filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать %s: %w", path, err)
	}
	return out, nil
}

// UpdateKeys перешифровывает файл по текущим creation_rules.
//
// Используется после изменения .sops.yaml: старые файлы должны получить
// новый набор получателей.
func (s Sops) UpdateKeys(path string) error {
	command := s.command(path)
	command.Stdin = nil
	command.Env = append(command.Env, "SOPS_AGE_KEY_FILE="+s.KeyFile)
	if _, err := s.Runner.Run(command, "sops", "updatekeys", "--yes", filepath.ToSlash(path)); err != nil {
		return fmt.Errorf("не удалось перешифровать %s: %w", path, err)
	}
	return nil
}

// command собирает Command для вызова sops.
func (s Sops) command(path string) Command {
	command := Command{
		Dir: s.Root,
		Env: []string{"SOPS_AGE_KEY_FILE=" + s.KeyFile},
	}
	return command
}

// DecryptFile читает файл с диска и расшифровывает его содержимое.
func (s Sops) DecryptFile(path string) ([]byte, error) {
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(s.Root, path)
	if err != nil {
		relative = path
	}
	return s.Decrypt(relative, encrypted)
}

// EncryptFile шифрует содержимое файла на диске и записывает шифротекст.
//
// Возвращает зашифрованные байты; вызывающий решает, что с ними делать.
func (s Sops) EncryptFile(path string) ([]byte, error) {
	plain, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(s.Root, path)
	if err != nil {
		relative = path
	}
	return s.Encrypt(relative, plain)
}

// DetectType определяет тип файла по расширению.
func DetectType(path string) string {
	return inputTypeFor(filepath.Ext(path))
}

// StripSopsHeader убирает служебные строки sops из расшифрованного вывода.
//
// Нужна для файлов, где sops возвращает метаданные в том же формате.
func StripSopsHeader(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))
	var out [][]byte
	for _, line := range lines {
		text := string(line)
		if strings.HasPrefix(text, "sops_version=") ||
			strings.HasPrefix(text, "sops_lastmodified=") ||
			strings.HasPrefix(text, "sops_mac=") ||
			strings.HasPrefix(text, "sops_unencrypted_suffix=") ||
			strings.HasPrefix(text, "sops_age__list_") {
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte("\n"))
}
