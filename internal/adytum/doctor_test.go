package adytum

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestApp собирает App для проверок самодиагностики.
func newTestApp(t *testing.T, runner Runner, root string) *App {
	t.Helper()
	var out bytes.Buffer
	return &App{
		Runner: runner,
		Root:   root,
		Now:    func() time.Time { return time.Now() },
		Out:    &out,
		Err:    &out,
	}
}

// TestDoctorReportsFailedChecksWithHints проверяет, что непрошедшие
// проверки снабжены подсказкой по исправлению.
//
// Проверка инструментов зависит от машины, поэтому берётся любая
// непрошедшая проверка: у remote в пустом репозитории подсказка
// должна быть всегда.
func TestDoctorReportsFailedChecksWithHints(t *testing.T) {
	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	report := app.Doctor()

	if report.OK {
		t.Error("отчёт без инструментов не может быть успешным")
	}
	for _, check := range report.Checks {
		if check.OK {
			continue
		}
		if check.Fix == "" {
			t.Errorf("у непрошедшей проверки %q нет подсказки", check.Name)
		}
	}
}

// TestDoctorChecksIdentityAndFilters проверяет состав проверок.
//
// Отчёт должен охватывать всё, из-за чего ломается работа с
// секретами: инструменты, идентичность, фильтры, правила шифрования,
// remote, разблокировку и свежесть.
func TestDoctorChecksIdentityAndFilters(t *testing.T) {
	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	report := app.Doctor()

	names := map[string]bool{}
	for _, check := range report.Checks {
		names[check.Name] = true
	}
	for _, expected := range []string{
		"sops", "age", "identity", "git-фильтры", ".sops.yaml",
		"remote", "разблокировка", "свежесть секретов",
	} {
		if !names[expected] {
			t.Errorf("проверка %q отсутствует в отчёте", expected)
		}
	}
}

// TestDoctorJSONIsMachineReadable проверяет формат JSON-отчёта.
//
// Формат машинный: без ANSI-последовательностей и символов Unicode.
func TestDoctorJSONIsMachineReadable(t *testing.T) {
	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	app.JSON = true

	var out bytes.Buffer
	app.Out = &out
	if err := app.DoctorPrint(); err != nil {
		t.Fatalf("DoctorPrint: %v", err)
	}

	content := out.String()
	if strings.Contains(content, "\033[") {
		t.Error("JSON-отчёт содержит ANSI-последовательности")
	}
	var report DoctorReport
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		t.Fatalf("JSON не разбирается: %v", err)
	}
	if len(report.Checks) == 0 {
		t.Error("в JSON-отчёте нет проверок")
	}
}

// TestDoctorTextPrintsFixHints проверяет, что текстовый отчёт
// показывает подсказки по исправлению.
func TestDoctorTextPrintsFixHints(t *testing.T) {
	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	var out bytes.Buffer
	app.Out = &out
	app.Err = &out
	err := app.DoctorText()

	content := out.String()
	if !strings.Contains(content, "adytum init") {
		t.Errorf("в отчёте нет подсказки установки: %s", content)
	}
	if err == nil {
		t.Error("отчёт с непроходимыми проверками должен возвращать ошибку")
	}
}

// TestDoctorChecksFilterScripts проверяет, что доктор замечает
// отсутствие скриптов-фильтров.
//
// Без скриптов git падает на каждой операции с секретами: молча
// настроенный, но не установленный фильтр опаснее отсутствующего.
func TestDoctorChecksFilterScripts(t *testing.T) {
	// Домашний каталог изолируется: на машине разработчика скрипты
	// могут быть установлены, и проверка прошла бы вхолостую.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"config --get filter.adytum.clean":    {out: "clean.sh %f"},
		"config --get filter.adytum.smudge":   {out: "smudge.sh %f"},
		"config --get filter.adytum.required": {out: "true"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	report := app.Doctor()

	for _, check := range report.Checks {
		if check.Name != "git-фильтры" {
			continue
		}
		if check.OK {
			t.Error("проверка фильтров не заметила отсутствие скриптов")
		}
		if check.Fix != "adytum init" {
			t.Errorf("подсказка = %q, ожидалось adytum init", check.Fix)
		}
		return
	}
	t.Fatal("проверка git-фильтров не найдена")
}

// TestDoctorUsesExistingScripts проверяет, что установленные скрипты
// считаются исправным состоянием.
func TestDoctorUsesExistingScripts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	binDir := filepath.Join(home, ".adytum", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, name := range []string{"adytum-clean.sh", "adytum-smudge.sh"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"config --get filter.adytum.clean":    {out: "clean.sh %f"},
		"config --get filter.adytum.smudge":   {out: "smudge.sh %f"},
		"config --get filter.adytum.required": {out: "true"},
	}}
	app := newTestApp(t, runner, t.TempDir())

	for _, check := range app.Doctor().Checks {
		if check.Name == "git-фильтры" && !check.OK {
			t.Errorf("установленные скрипты посчитаны за отсутствующие: %+v", check)
		}
	}
}
