package adytum

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lowern1ght/adytum/internal/envfile"
)

// newEntryTemplate — содержимое нового секрета.
//
// Значение намеренно пустое: пользователь заполняет его через
// `adytum set` или редактор.
const newEntryTemplate = "# example KEY=\n"

// parseEntries разбирает расшифрованный env-файл.
func parseEntries(content []byte) ([]envfile.Entry, error) {
	return envfile.Parse(content)
}

// lookup возвращает значение ключа из списка записей.
func lookup(entries []envfile.Entry, key string) (string, bool) {
	return envfile.Lookup(entries, key)
}

// editorCommand возвращает команду редактора из переменных окружения.
//
// Значение разбивается по пробелам: EDITOR часто содержит аргументы
// ("code --wait", "emacsclient -nw").
func editorCommand() []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return strings.Fields(value)
		}
	}
	if runtime.GOOS == "windows" {
		return []string{"notepad"}
	}
	return []string{"vi"}
}

// editInEditor открывает содержимое в редакторе и возвращает результат.
//
// Расширение файла сохраняется: sops определяет тип данных по нему.
func editInEditor(content []byte, ext string) (string, error) {
	file, err := os.CreateTemp("", "adytum-*"+ext)
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())

	if _, err := file.Write(content); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	command := editorCommand()
	args := append(append([]string{}, command[1:]...), file.Name())
	editor := exec.Command(command[0], args...)
	editor.Stdin = os.Stdin
	editor.Stdout = os.Stdout
	editor.Stderr = os.Stderr
	if err := editor.Run(); err != nil {
		return "", fmt.Errorf("редактор завершился с ошибкой: %w", err)
	}

	edited, err := os.ReadFile(file.Name())
	if err != nil {
		return "", err
	}
	return string(edited), nil
}

// ResolveSecret находит файл секрета по проекту и имени сервиса.
//
// Возвращает абсолютный путь и тип файла ("env", "yaml", "json").
func ResolveSecret(root, project, service string) (string, string, error) {
	if project == "" || service == "" {
		return "", "", fmt.Errorf("%w: укажите проект и сервис", ErrNotFound)
	}
	dir := filepath.Join(root, project)
	if _, err := os.Stat(dir); err != nil {
		return "", "", fmt.Errorf("%w: каталог %s", ErrNotFound, project)
	}
	for _, ext := range []string{".env", ".yaml", ".yml", ".json"} {
		candidate := filepath.Join(dir, service+ext)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, strings.TrimPrefix(ext, "."), nil
		}
	}
	return "", "", fmt.Errorf("%w: %s/%s", ErrNotFound, project, service)
}

// SplitTarget разбирает аргумент вида <project>/<service>.
//
// Имя сервиса может содержать расширение (.env, .yaml, .json).
func SplitTarget(target string) (string, string, error) {
	project, service, found := strings.Cut(strings.TrimSpace(target), "/")
	if !found || project == "" || service == "" {
		return "", "", fmt.Errorf("%w: ожидается <project>/<service>", ErrUsage)
	}
	return project, service, nil
}
