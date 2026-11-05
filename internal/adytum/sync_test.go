package adytum

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSyncPullOnlyNeverPushes проверяет, что режим pull-only ничего не
// отправляет.
//
// Таймер автосинхронизации работает в фоне: публиковать локальные
// правки без участия человека он не должен.
func TestSyncPullOnlyNeverPushes(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url":     {out: "https://github.com/owner/secrets.git"},
		"rev-parse --abbrev": {out: "master"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	if err := app.Sync(true); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "push") {
			t.Errorf("pull-only выполнил push: %q", joined)
		}
	}
}

// TestSyncPullOnlyResetsToOrigin проверяет, что pull-only обновляет
// ветку до состояния origin.
//
// Рабочее дерево реплики всегда «грязное»: smunge-фильтр кладёт
// шифротекст вместо расшифрованного файла, и обычный pull на нём
// абортится («would be overwritten»). Реплика забирает изменения
// принудительно.
func TestSyncPullOnlyResetsToOrigin(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url":     {out: "https://github.com/owner/secrets.git"},
		"rev-parse --abbrev": {out: "master"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	if err := app.Sync(true); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	fetched := false
	reset := false
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "fetch origin") {
			fetched = true
		}
		if strings.Contains(joined, "reset --hard origin/master") {
			reset = true
		}
	}
	if !fetched {
		t.Error("pull-only не выполнил fetch origin")
	}
	if !reset {
		t.Error("pull-only не выполнил reset --hard origin/master")
	}
}

// TestSyncPushesWhenNotPullOnly проверяет обычный режим: изменения
// отправляются.
func TestSyncPushesWhenNotPullOnly(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url":     {out: "https://github.com/owner/secrets.git"},
		"rev-parse --abbrev": {out: "master"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	if err := app.Sync(false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	pushed := false
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "push") {
			pushed = true
		}
	}
	if !pushed {
		t.Error("обычный sync не выполнил push")
	}
}

// TestSyncWithoutRemoteAdvisesBootstrap проверяет подсказку про
// bootstrap-remote.
//
// Агент без remote не знает, что делать дальше: подсказка ведёт его
// к нужной команде.
func TestSyncWithoutRemoteAdvisesBootstrap(t *testing.T) {
	runner := &fakeRunner{}
	app := newTestApp(t, runner, t.TempDir())

	var out bytes.Buffer
	app.Out = &out
	app.Err = &out
	if err := app.Sync(true); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if !strings.Contains(out.String(), AdviceRemote) {
		t.Errorf("нет подсказки %q: %s", AdviceRemote, out.String())
	}
}

// TestPullUsesCredentialsFileWhenPresent проверяет, что при наличии
// ~/.git-credentials токен не нужен.
//
// Файл учётных данных — обычный способ авторизации: его наличие
// избавляет от одноразовых URL с токеном.
func TestPullUsesCredentialsFileWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, ".git-credentials"), []byte("https://user:pass@github.com\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url": {out: "https://github.com/owner/secrets.git"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	if err := app.pull(); err != nil {
		t.Fatalf("pull: %v", err)
	}

	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "x-access-token") {
			t.Errorf("токен подставлен при наличии .git-credentials: %q", joined)
		}
	}
}

// TestPullKeepsTokenOutOfArguments проверяет, что токен не попадает в
// аргументы git.
//
// Аргументы команды видит любой пользователь машины через список
// процессов: токен в них — это токен в логах всей системы.
func TestPullKeepsTokenOutOfArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url": {out: "https://github.com/owner/secrets.git"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	command, args, token := app.remoteAuth("pull", "--rebase")

	if token == "" {
		t.Skip("токен недоступен на этой машине")
	}
	for _, arg := range args {
		if strings.Contains(arg, token) {
			t.Errorf("токен попал в аргументы: %q", arg)
		}
	}
	envToken := ""
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, tokenEnv+"=") {
			envToken = strings.TrimPrefix(entry, tokenEnv+"=")
		}
	}
	if envToken != token {
		t.Errorf("токен в окружении = %q, ожидался %q", envToken, token)
	}
}

// TestPullErrorHidesToken проверяет, что токен не попадает в текст
// ошибки.
//
// Git печатает URL с учётными данными: без очистки токен ушёл бы в
// лог агентской сессии.
func TestPullErrorHidesToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url": {out: "https://github.com/owner/secrets.git"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	_, _, token := app.remoteAuth("pull", "--rebase")
	if token == "" {
		t.Skip("токен недоступен на этой машине")
	}

	err := redactToken(errText("fatal: Authentication failed for 'https://x-access-token:"+token+"@github.com/o/r.git'"), token)
	if strings.Contains(err.Error(), token) {
		t.Errorf("токен остался в ошибке: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("ожидалась замена токена на маску: %q", err.Error())
	}
}

// TestStalenessUsesRemoteAuth проверяет, что проверка свежести идёт с
// теми же учётными данными, что и остальные операции с remote.
//
// На реплике, которая только читает, проверка без учётных данных
// всегда падала бы: приватный репозиторий недоступен анонимно.
func TestStalenessUsesRemoteAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"remote get-url": {out: "https://github.com/owner/secrets.git"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	env, options, token := app.remoteAuthEnv()
	if token == "" {
		t.Skip("токен недоступен на этой машине")
	}
	if len(options) == 0 {
		t.Error("для remote не заданы опции credential.helper")
	}
	if len(env) == 0 {
		t.Error("токен не передан через окружение")
	}
	for _, entry := range env {
		if strings.Contains(entry, token) {
			t.Errorf("токен попал в аргументы окружения: %q", entry)
		}
	}
}

// TestAuthURLHidesTokenFromErrors проверяет, что токен не попадает в
// текст ошибки проверки свежести.
//
// Git печатает URL с учётными данными: без очистки токен ушёл бы в
// лог агентской сессии.
func TestAuthURLHidesTokenFromErrors(t *testing.T) {
	const token = "ghp_fake_token"

	err := redactToken(errText("fatal: Authentication failed for 'https://x-access-token:"+token+"@github.com/o/r.git'"), token)

	if strings.Contains(err.Error(), token) {
		t.Errorf("токен остался в ошибке: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("ожидалась замена токена на маску: %q", err.Error())
	}
}

// TestRequireUnlockBlocksGetWithoutMarker проверяет, что get закрыт
// без маркера разблокировки.
//
// Значение секрета не должно попасть в лог агентской сессии.
func TestRequireUnlockBlocksGetWithoutMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	err := app.requireUnlock()
	if err == nil {
		t.Fatal("ожидался отказ без маркера разблокировки")
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if adviser.Advice().Text != AdviceUnlock {
		t.Errorf("подсказка = %q, ожидалось %q", adviser.Advice().Text, AdviceUnlock)
	}
}

// TestUnlockAllowsGetWithMarker проверяет, что с маркером операция
// проходит.
func TestUnlockAllowsGetWithMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	app.Now = func() time.Time { return time.Now() }
	if err := app.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if err := app.requireUnlock(); err != nil {
		t.Errorf("с маркером операция закрыта: %v", err)
	}
}
