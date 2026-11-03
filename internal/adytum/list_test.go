package adytum

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newListTestApp собирает App для проверок реестра.
func newListTestApp(t *testing.T, root string) (*App, *bytes.Buffer) {
	t.Helper()

	var out bytes.Buffer
	app := &App{
		Runner: &fakeRunner{},
		Root:   root,
		Out:    &out,
		Err:    &out,
	}
	return app, &out
}

// TestListWithoutRegistryDoesNotFail проверяет деградацию без реестра.
//
// Пустое хранилище без реестра — обычное состояние первого запуска:
// команда должна сказать, что создать, а не упасть.
func TestListWithoutRegistryDoesNotFail(t *testing.T) {
	app, out := newListTestApp(t, t.TempDir())

	if err := app.List(""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(out.String(), "реестра нет") {
		t.Errorf("вывод = %q, ожидалось сообщение об отсутствии реестра", out.String())
	}
}

// TestListWithoutRegistryJSONIsMachineReadable проверяет JSON-вывод без
// реестра: агент должен получить разбираемый документ, а не пустоту.
func TestListWithoutRegistryJSONIsMachineReadable(t *testing.T) {
	app, out := newListTestApp(t, t.TempDir())
	app.JSON = true

	if err := app.List(""); err != nil {
		t.Fatalf("List: %v", err)
	}
	var report ListReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("JSON не разбирается: %v (%q)", err, out.String())
	}
	if len(report.Errors) == 0 {
		t.Error("в отчёте нет сообщения об отсутствии реестра")
	}
}

// TestListPrintsEntriesAndDescriptions проверяет таблицу записей и
// подробности одной записи.
//
// Значения ключей в выводе нет: описания — да.
func TestListPrintsEntriesAndDescriptions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(demoRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app, out := newListTestApp(t, root)
	if err := app.List(""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(out.String(), "nova/demo") {
		t.Errorf("в таблице нет записи: %q", out.String())
	}

	out.Reset()
	if err := app.List("nova/demo"); err != nil {
		t.Fatalf("List: %v", err)
	}
	detail := out.String()
	if !strings.Contains(detail, "токен доступа") {
		t.Errorf("в подробностях нет описания ключа: %q", detail)
	}
	if strings.Contains(detail, "s3cret") {
		t.Errorf("в выводе просочилось значение секрета: %q", detail)
	}
}

// TestListReportsUnknownEntry проверяет отказ для неизвестной записи.
func TestListReportsUnknownEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(demoRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app, _ := newListTestApp(t, root)
	if err := app.List("nova/other"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестной записи")
	}
}
