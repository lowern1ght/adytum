// Package adytum реализует команды CLI: тонкая обёртка над sops, git и age.
//
// CLI ничего не реализует криптографией самостоятельно — вся работа
// делегируется внешним бинарям (sops, git, age-keygen), чтобы поведение
// совпадало с ручным использованием этих инструментов.
package adytum

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotFound — целевой файл или запись не найдены.
var ErrNotFound = errors.New("не найдено")

// ErrKeyNotFound — ключ не найден внутри секрета.
var ErrKeyNotFound = errors.New("ключ не найден")

// ErrUsage — ошибка в аргументах команды.
var ErrUsage = errors.New("ошибка использования")

// Command описывает запуск внешней программы.
type Command struct {
	// Dir — рабочий каталог (пусто — текущий).
	Dir string
	// Env — дополнительные переменные окружения в формате "KEY=VALUE".
	Env []string
	// Stdin — данные, передаваемые на вход программы.
	Stdin []byte
}

// Runner выполняет внешние команды. Интерфейс вынесен отдельно, чтобы
// команды можно было тестировать без настоящих бинарей.
type Runner interface {
	Run(cmd Command, name string, args ...string) ([]byte, error)
}

// ContextRunner выполняет внешние команды с отменой по контексту.
//
// Отдельный интерфейс, а не метод Runner: не все исполнители умеют
// прерывать процесс, но там, где это нужно (сетевые проверки с
// таймаутом), реализация обязана уметь.
type ContextRunner interface {
	RunContext(ctx context.Context, cmd Command, name string, args ...string) ([]byte, error)
}

// Exec запускает настоящие внешние команды.
type Exec struct{}

// Run выполняет команду.
func (Exec) Run(cmd Command, name string, args ...string) ([]byte, error) {
	return Exec{}.run(context.Background(), cmd, name, args...)
}

// RunContext выполняет команду, прерывая её по отмене контекста.
//
// Прерывание нужно сетевым проверкам: git ls-remote на недоступном
// remote иначе висит до бесконечности.
func (e Exec) RunContext(ctx context.Context, cmd Command, name string, args ...string) ([]byte, error) {
	return e.run(ctx, cmd, name, args...)
}

// run выполняет команду и собирает её вывод.
func (Exec) run(ctx context.Context, cmd Command, name string, args ...string) ([]byte, error) {
	program := exec.CommandContext(ctx, name, args...)
	program.Dir = cmd.Dir
	if cmd.Env != nil {
		program.Env = append(os.Environ(), cmd.Env...)
	}
	if cmd.Stdin != nil {
		program.Stdin = bytes.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	program.Stdout = &stdout
	program.Stderr = &stderr
	if err := program.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}

// Require проверяет наличие внешней команды в PATH.
func Require(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("требуется %s: %w", name, err)
	}
	return nil
}

// LookPath ищет внешнюю команду, возвращая её полный путь.
func LookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("требуется %s: %w", name, err)
	}
	return path, nil
}

// RepoRoot ищет корень git-репозитория, начиная с dir.
func RepoRoot(runner Runner, dir string) (string, error) {
	out, err := runner.Run(Command{Dir: dir}, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("не удалось определить корень репозитория: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SecretExts — расширения файлов, в которых хранятся секреты.
var SecretExts = []string{".env", ".yaml", ".yml", ".json"}

// IsSecretExt сообщает, относится ли расширение к секретам.
func IsSecretExt(ext string) bool {
	for _, known := range SecretExts {
		if ext == known {
			return true
		}
	}
	return false
}

// IsSecretPath сообщает, является ли путь секретом (по расширению).
func IsSecretPath(path string) bool {
	return IsSecretExt(filepath.Ext(path))
}
