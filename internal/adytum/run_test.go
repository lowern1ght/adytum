package adytum

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/lowern1ght/adytum/internal/envfile"
)

// fakeRunner подменяет внешние команды.
type fakeRunner struct {
	// responses — ответы по подстроке в аргументах.
	responses map[string]fakeResponse
	// calls — список выполненных команд.
	calls [][]string
}

// fakeResponse — ответ на команду.
type fakeResponse struct {
	out string
	err error
}

// Run выполняет команду и возвращает заранее заданный ответ.
func (f *fakeRunner) Run(cmd Command, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	joined := strings.Join(args, " ")
	for key, response := range f.responses {
		if strings.Contains(joined, key) {
			return []byte(response.out), response.err
		}
	}
	return nil, nil
}

// TestRunThroughShellExpandsEnvironment проверяет, что команда
// выполняется через shell.
//
// Контракт агентов: `adytum run --env project/service -- curl -H
// "Bearer $TOKEN" ...`. Раскрытие переменной делает shell, поэтому
// команда не должна запускаться напрямую через exec.
func TestRunThroughShellExpandsEnvironment(t *testing.T) {
	var stdout bytes.Buffer
	environment := append(os.Environ(), "ADYTUM_TEST_TOKEN=s3cret")

	err := runThroughShell(
		[]string{"printf", "%s", "$ADYTUM_TEST_TOKEN"},
		environment,
		t.TempDir(),
		&stdout,
		&stdout,
	)
	if err != nil {
		t.Fatalf("runThroughShell: %v", err)
	}
	if got := stdout.String(); got != "s3cret" {
		t.Errorf("вывод = %q, ожидалось s3cret", got)
	}
}

// TestRunThroughShellReportsExitCode проверяет, что код возврата
// команды не теряется.
func TestRunThroughShellReportsExitCode(t *testing.T) {
	var stdout bytes.Buffer

	err := runThroughShell(
		[]string{"exit", "3"},
		os.Environ(),
		t.TempDir(),
		&stdout,
		&stdout,
	)
	if err == nil {
		t.Fatal("ожидалась ошибка для команды с ненулевым кодом возврата")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("ошибка = %q, ожидался код возврата 3", err.Error())
	}
}

// TestShellCommandUsesShell проверяет, что выбирается настоящая
// оболочка, а не прямая запуск программы.
func TestShellCommandUsesShell(t *testing.T) {
	shell, flag := shellCommand()

	if shell == "" {
		t.Fatal("оболочка не выбрана")
	}
	if flag != "-c" && flag != "/c" {
		t.Errorf("флаг оболочки = %q, ожидалось -c или /c", flag)
	}
	// POSIX-оболочка предпочтительна: контракт агентов опирается на
	// раскрытие переменных через $ИМЯ.
	if shell == "cmd" {
		t.Skip("POSIX-оболочка недоступна: контракт $ПЕРЕМЕННАЯ не работает")
	}
	if shell != "sh" {
		t.Errorf("оболочка = %q, ожидалось sh", shell)
	}
}

// TestJoinCommandKeepsPlaceholders проверяет, что плейсхолдер
// переменной доходит до shell нетронутым.
//
// Экранирование превратило бы $TOKEN в литерал, и секрет не попал бы
// в заголовок запроса.
func TestJoinCommandKeepsPlaceholders(t *testing.T) {
	command := []string{"curl", "-H", "Bearer $GITLAB_TOKEN", "https://example/api"}

	script := joinCommand(command)

	if !strings.Contains(script, "$GITLAB_TOKEN") {
		t.Errorf("скрипт = %q, ожидался плейсхолдер $GITLAB_TOKEN", script)
	}
}

// TestJoinCommandKeepsArgumentBoundaries проверяет, что границы слов
// переживают сборку.
//
// Значение с пробелом должно остаться одним аргументом: без кавычек
// `Bearer $TOKEN` распалось бы на два слова.
func TestJoinCommandKeepsArgumentBoundaries(t *testing.T) {
	command := []string{"curl", "-H", "Bearer secret value", "https://example/api"}

	script := joinCommand(command)

	if !strings.Contains(script, `"Bearer secret value"`) {
		t.Errorf("скрипт = %q, ожидался аргумент в кавычках", script)
	}
}

// TestJoinCommandKeepsSingleArgumentVerbatim проверяет, что одиночный
// аргумент передаётся shell как есть.
//
// Так работает контракт плагина: команда приходит одним куском, и
// дополнительные кавычки сломали бы её разбор.
func TestJoinCommandKeepsSingleArgumentVerbatim(t *testing.T) {
	command := []string{`curl -H "Bearer $GITLAB_TOKEN" https://example/api`}

	script := joinCommand(command)

	if script != command[0] {
		t.Errorf("скрипт = %q, ожидалось %q", script, command[0])
	}
}

// TestEnvValuesReadsDotenv проверяет разбор env-файла.
func TestEnvValuesReadsDotenv(t *testing.T) {
	values, err := envValues("personal/github.env", []byte("TOKEN=abc\nHOST=example.com\n"))
	if err != nil {
		t.Fatalf("envValues: %v", err)
	}
	if values["TOKEN"] != "abc" {
		t.Errorf("TOKEN = %q, ожидалось abc", values["TOKEN"])
	}
	if values["HOST"] != "example.com" {
		t.Errorf("HOST = %q, ожидалось example.com", values["HOST"])
	}
}

// TestEnvValuesReadsYaml проверяет разбор yaml-секрета.
func TestEnvValuesReadsYaml(t *testing.T) {
	values, err := envValues("nova/gitlab.yaml", []byte("url: https://example.com\nport: 443\n"))
	if err != nil {
		t.Fatalf("envValues: %v", err)
	}
	if values["url"] != "https://example.com" {
		t.Errorf("url = %q", values["url"])
	}
	if values["port"] != "443" {
		t.Errorf("port = %q, ожидалось 443", values["port"])
	}
}

// TestEnvValuesReadsJSON проверяет разбор json-секрета.
func TestEnvValuesReadsJSON(t *testing.T) {
	values, err := envValues("personal/token.json", []byte(`{"token":"abc","count":2}`))
	if err != nil {
		t.Fatalf("envValues: %v", err)
	}
	if values["token"] != "abc" {
		t.Errorf("token = %q, ожидалось abc", values["token"])
	}
	if values["count"] != "2" {
		t.Errorf("count = %q, ожидалось 2", values["count"])
	}
}

// TestEnvValuesRejectsUnknownFormat проверяет отказ на неизвестном
// формате: молча проглоченный секрет хуже явной ошибки.
func TestEnvValuesRejectsUnknownFormat(t *testing.T) {
	if _, err := envValues("personal/token.bin", []byte("data")); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного формата")
	}
}

// TestSetEnvReplacesExistingValue проверяет перезапись переменной.
//
// Повторный --env с одним и тем же ключом должен давать последнее
// значение, иначе порядок аргументов влиял бы на результат.
func TestSetEnvReplacesExistingValue(t *testing.T) {
	environment := []string{"PATH=/usr/bin", "TOKEN=old"}

	updated := envfile.SetEnv(environment, "TOKEN", "new")

	found := ""
	for _, entry := range updated {
		if strings.HasPrefix(entry, "TOKEN=") {
			found = entry
		}
	}
	if found != "TOKEN=new" {
		t.Errorf("TOKEN = %q, ожидалось TOKEN=new", found)
	}
}

// TestSetEnvAddsNewValue проверяет добавление новой переменной.
func TestSetEnvAddsNewValue(t *testing.T) {
	updated := envfile.SetEnv([]string{"PATH=/usr/bin"}, "TOKEN", "new")

	if len(updated) != 2 {
		t.Fatalf("переменных = %d, ожидалось 2", len(updated))
	}
	if updated[1] != "TOKEN=new" {
		t.Errorf("последняя переменная = %q, ожидалось TOKEN=new", updated[1])
	}
}

// TestRedactTokenRemovesSecretFromError проверяет очистку токена из
// текста ошибки.
//
// Git печатает URL с учётными данными при отказе по авторизации:
// без очистки токен попал бы в вывод команды.
func TestRedactTokenRemovesSecretFromError(t *testing.T) {
	err := redactToken(errText("fatal: Authentication failed for 'https://x-access-token:ghp_secret@github.com/o/r.git'"), "ghp_secret")

	if strings.Contains(err.Error(), "ghp_secret") {
		t.Errorf("токен остался в ошибке: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("ожидалась замена токена на маску: %q", err.Error())
	}
}

// TestIsNotRecipientDetectsAccessError проверяет распознавание ошибки
// доступа к разделу.
func TestIsNotRecipientDetectsAccessError(t *testing.T) {
	messages := []string{
		"Failed to get the data key required to decrypt the SOPS file.",
		"age: no identity matched any of the recipients",
	}
	for _, message := range messages {
		if !isNotRecipient(message) {
			t.Errorf("isNotRecipient(%q) = false, ожидалось true", message)
		}
	}
	if isNotRecipient("sops metadata not found") {
		t.Error("isNotRecipient вернул true для посторонней ошибки")
	}
}

// errText создаёт ошибку с заданным текстом.
type errText string

// Error возвращает текст ошибки.
func (e errText) Error() string {
	return string(e)
}
