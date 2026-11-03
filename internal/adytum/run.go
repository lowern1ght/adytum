package adytum

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/lowern1ght/adytum/internal/envfile"
)

// Run выполняет команду с секретами, подставленными в окружение.
//
// Команда запускается через shell, а не напрямую: плейсхолдеры
// вида $GITLAB_TOKEN в аргументах раскрываются окружением в момент
// запуска, и агент ни разу не видит значение секрета.
//
// Ключи из env-файлов добавляются в окружение процесса. Значения
// никогда не печатаются.
func (a *App) Run(envs []string, command []string) error {
	if len(envs) == 0 {
		return fmt.Errorf("%w: нужен хотя бы один --env", ErrUsage)
	}
	if len(command) == 0 {
		return fmt.Errorf("%w: не указана команда", ErrUsage)
	}
	if err := a.requireUnlock(); err != nil {
		return err
	}
	environment, err := a.runEnvironment(envs)
	if err != nil {
		return err
	}
	if err := runThroughShell(command, environment, a.Root, a.out(), a.errOut()); err != nil {
		return err
	}
	a.staleHint(a.errOut())
	return nil
}

// runEnvironment собирает окружение с секретами.
func (a *App) runEnvironment(envs []string) ([]string, error) {
	environment := os.Environ()
	for _, target := range envs {
		project, service, err := SplitTarget(target)
		if err != nil {
			return nil, err
		}
		path, _, err := ResolveSecret(a.Root, project, service)
		if err != nil {
			return nil, err
		}
		plain, err := a.readPlaintext(path)
		if err != nil {
			return nil, err
		}
		values, err := envValues(path, plain)
		if err != nil {
			return nil, err
		}
		for key, value := range values {
			environment = envfile.SetEnv(environment, key, value)
		}
	}
	return environment, nil
}

// envValues разбирает секрет в пары переменных окружения.
//
// Поддерживаются все форматы хранилища: dotenv (KEY=VALUE), yaml и json
// с плоскими ключами верхнего уровня. Вложенные структуры пропускаются:
// переменная окружения хранит одно значение.
func envValues(path string, plain []byte) (map[string]string, error) {
	return envfile.Document(path, plain)
}

// runThroughShell выполняет команду через shell.
//
// Так аргументы вида $GITLAB_TOKEN раскрываются окружением уже в
// момент запуска процесса.
func runThroughShell(command []string, environment []string, dir string, stdout, stderr io.Writer) error {
	shell, flag := shellCommand()
	script := joinCommand(command)
	process := exec.Command(shell, flag, script)
	process.Dir = dir
	process.Env = environment
	process.Stdin = os.Stdin
	process.Stdout = stdout
	process.Stderr = stderr
	if err := process.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("команда завершилась с кодом %d", exit.ExitCode())
		}
		return err
	}
	return nil
}

// shellCommand возвращает оболочку и её флаг для выполнения команды.
//
// Предпочтение отдаётся POSIX-оболочке: контракт агентов
// (`curl -H "Bearer $GITLAB_TOKEN" ...`) опирается на раскрытие
// переменных через `$ИМЯ`, а cmd.exe раскрывает только `%ИМЯ%`.
//
// На Windows POSIX-оболочка берётся из поставки Git-for-Windows: без
// неё остаётся cmd /c.
func shellCommand() (string, string) {
	if _, err := exec.LookPath("sh"); err == nil {
		return "sh", "-c"
	}
	if runtime.GOOS == "windows" {
		return "cmd", "/c"
	}
	return "sh", "-c"
}

// joinCommand собирает командную строку для shell.
//
// Аргументы соединяются пробелами в кавычках: границы слов должны
// пережить сборку, иначе `curl -H "Bearer $TOKEN"` распадётся на
// три аргумента.
//
// Единственный аргумент трактуется как готовая командная строка и
// передаётся shell без изменений: так работает контракт плагина,
// где команда приходит одним куском.
//
// Кавычки экранируются, а `$` — нет: переменная должна раскрыться в
// окружении запуска.
func joinCommand(command []string) string {
	if len(command) == 1 {
		return command[0]
	}
	quoted := make([]string, 0, len(command))
	for _, argument := range command {
		quoted = append(quoted, quoteArgument(argument))
	}
	return strings.Join(quoted, " ")
}

// quoteArgument оборачивает аргумент в двойные кавычки.
//
// Экранируются только те символы, которые внутри кавычек меняют
// смысл: обратный слэш, двойная кавычка и обратная кавычка.
func quoteArgument(argument string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, symbol := range argument {
		switch symbol {
		case '\\', '"', '`':
			builder.WriteByte('\\')
		}
		builder.WriteRune(symbol)
	}
	builder.WriteByte('"')
	return builder.String()
}
