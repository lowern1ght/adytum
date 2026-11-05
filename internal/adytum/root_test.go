package adytum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeFixture готовит каталог хранилища с реестром.
func storeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, StoreMarker), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

// TestRequireRootUsesExplicitPath проверяет, что явный путь важнее
// всего остального.
//
// Флаг --repo — прямое указание владельца: спорить с ним нельзя.
func TestRequireRootUsesExplicitPath(t *testing.T) {
	explicit := storeFixture(t)
	other := storeFixture(t)
	t.Setenv(RepoEnv, other)

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	if err := app.RequireRoot(explicit); err != nil {
		t.Fatalf("RequireRoot: %v", err)
	}
	expected, _ := filepath.Abs(explicit)
	if app.Root != expected {
		t.Errorf("Root = %q, ожидался %q", app.Root, expected)
	}
}

// TestRequireRootUsesEnvironmentVariable проверяет, что переменная
// окружения задаёт каталог хранилища.
//
// Нужна машинам, у которых рабочий каталог не связан с хранилищем:
// systemd-юнитам и фоновым скриптам.
func TestRequireRootUsesEnvironmentVariable(t *testing.T) {
	root := storeFixture(t)
	t.Setenv(RepoEnv, root)

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	if err := app.RequireRoot(""); err != nil {
		t.Fatalf("RequireRoot: %v", err)
	}
	expected, _ := filepath.Abs(root)
	if app.Root != expected {
		t.Errorf("Root = %q, ожидался %q", app.Root, expected)
	}
}

// TestRequireRootSearchesUpward проверяет, что каталог хранилища
// находится от вложенного каталога.
//
// Агент часто работает в подкаталоге хранилища: искать корень вверх
// удобнее, чем требовать точный путь.
func TestRequireRootSearchesUpward(t *testing.T) {
	root := storeFixture(t)
	nested := filepath.Join(root, "personal", "github")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(previous) })

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	if err := app.RequireRoot(""); err != nil {
		t.Fatalf("RequireRoot: %v", err)
	}
	expected, _ := filepath.Abs(root)
	if app.Root != expected {
		t.Errorf("Root = %q, ожидался %q", app.Root, expected)
	}
}

// TestRequireRootFailsWithAdvice проверяет, что без хранилища команда
// объясняет, где его искать.
//
// Агент без хранилища не знает, что делать: подсказка с обоими
// способами указания пути избавляет от вопросов владельцу.
func TestRequireRootFailsWithAdvice(t *testing.T) {
	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	t.Setenv(RepoEnv, "")

	err := app.RequireRoot("")
	if err == nil {
		t.Fatal("ожидалась ошибка: хранилище не найдено")
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if !strings.Contains(adviser.Advice().Text, "--repo") {
		t.Errorf("подсказка = %q, ожидалось упоминание --repo", adviser.Advice().Text)
	}
}

// TestRequireRootRejectsMissingPath проверяет, что несуществующий путь
// отвергается с внятной ошибкой.
func TestRequireRootRejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "нет-такого")

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	err := app.RequireRoot(missing)
	if err == nil {
		t.Fatal("ожидалась ошибка для несуществующего пути")
	}
	if !strings.Contains(err.Error(), "хранилище не найдено") {
		t.Errorf("ошибка = %q, ожидалось «хранилище не найдено»", err.Error())
	}
}
