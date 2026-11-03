package adytum

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lowern1ght/adytum/internal/store"
)

// demoRegistry — реестр с одним секретом и одним ключом.
const demoRegistry = `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]

entries:
  nova/demo:
    type: env
    description: демонстрационный секрет
    scope: default
    keys:
      TOKEN: токен доступа
`

// newUnlockedApp собирает App с активной разблокировкой.
//
// Разблокировка обязательна для операций с секретами: без неё set
// и get закрыты, и проверка ничего не сказала бы о записи значения.
func newUnlockedApp(t *testing.T, root string) *App {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := store.WriteUnlock(time.Now()); err != nil {
		t.Fatalf("WriteUnlock: %v", err)
	}

	var out bytes.Buffer
	return &App{
		Runner: &fakeRunner{},
		Root:   root,
		Now:    func() time.Time { return time.Now() },
		Out:    &out,
		Err:    &out,
	}
}

// writeDemoStore готовит хранилище: реестр и файл секрета.
func writeDemoStore(t *testing.T, root string) string {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(demoRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(root, "nova", "demo.env")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("TOKEN=old\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// withStdin подменяет stdin файлом с заданным содержимым.
//
// Значение приходит из stdin, а не из аргументов: иначе оно осталось бы
// в списке процессов.
func withStdin(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	previous := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = previous
		file.Close()
	})
}

// TestSetWritesKeyFromStdin проверяет запись значения из stdin.
//
// Путь значения важнее самого значения: в аргументах команды секрет
// остался бы в истории оболочки и в списке процессов.
func TestSetWritesKeyFromStdin(t *testing.T) {
	root := t.TempDir()
	path := writeDemoStore(t, root)
	withStdin(t, "s3cret\n")

	app := newUnlockedApp(t, root)
	err := app.Set("nova/demo", SetOptions{Key: "TOKEN", FromStdin: true})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "TOKEN=s3cret") {
		t.Errorf("значение не записано: %q", string(content))
	}
}

// TestSetKeepsOtherKeys проверяет, что запись одного ключа не трогает
// остальные.
func TestSetKeepsOtherKeys(t *testing.T) {
	root := t.TempDir()
	path := writeDemoStore(t, root)
	if err := os.WriteFile(path, []byte("TOKEN=old\nHOST=example.com\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	withStdin(t, "s3cret")

	app := newUnlockedApp(t, root)
	if err := app.Set("nova/demo", SetOptions{Key: "TOKEN", FromStdin: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "HOST=example.com") {
		t.Errorf("соседний ключ потерян: %q", string(content))
	}
}

// TestSetRejectsStdinWithoutKey проверяет запрет на чтение всех ключей
// из stdin.
//
// В stdin один поток: на все ключи его не хватит, и значения разъехались
// бы по файлу без всякой связи с порядком ключей.
func TestSetRejectsStdinWithoutKey(t *testing.T) {
	root := t.TempDir()
	writeDemoStore(t, root)
	withStdin(t, "s3cret")

	app := newUnlockedApp(t, root)
	err := app.Set("nova/demo", SetOptions{FromStdin: true})
	if err == nil {
		t.Fatal("ожидалась ошибка для stdin без ключа")
	}
	if !strings.Contains(err.Error(), "--stdin") {
		t.Errorf("ошибка = %q, ожидалось упоминание --stdin", err.Error())
	}
}

// TestSetPointsToNewForUnknownEntry проверяет подсказку для секрета,
// которого нет в реестре.
//
// Агент не может придумать описание секрета: это дело владельца.
func TestSetPointsToNewForUnknownEntry(t *testing.T) {
	root := t.TempDir()
	writeDemoStore(t, root)
	withStdin(t, "s3cret")

	app := newUnlockedApp(t, root)
	err := app.Set("nova/other", SetOptions{Key: "TOKEN", FromStdin: true})
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестной записи")
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatalf("ошибка без подсказки: %v", err)
	}
	if !strings.Contains(adviser.Advice().Text, "adytum new") {
		t.Errorf("подсказка = %q, ожидалось упоминание adytum new", adviser.Advice().Text)
	}
}

// TestUnsetRemovesKey проверяет удаление ключа из файла секрета.
func TestUnsetRemovesKey(t *testing.T) {
	root := t.TempDir()
	path := writeDemoStore(t, root)
	if err := os.WriteFile(path, []byte("TOKEN=old\nHOST=example.com\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newUnlockedApp(t, root)
	if err := app.Unset("nova/demo", "TOKEN"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(content), "TOKEN") {
		t.Errorf("ключ не удалён: %q", string(content))
	}
	if !strings.Contains(string(content), "HOST=example.com") {
		t.Errorf("соседний ключ потерян: %q", string(content))
	}
}
