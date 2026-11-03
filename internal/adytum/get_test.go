package adytum

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// yamlRegistry — реестр с секретом типа yaml.
const yamlRegistry = `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]

entries:
  nova/demo:
    type: yaml
    description: демонстрационный секрет
    scope: default
    keys:
      token: токен доступа
`

// newGetTestApp собирает App для чтения секретов.
func newGetTestApp(t *testing.T, root string) (*App, *bytes.Buffer) {
	t.Helper()

	app := newUnlockedApp(t, root)
	var out bytes.Buffer
	app.Out = &out
	app.Err = &out
	return app, &out
}

// TestGetReadsKeyByRegistryType проверяет чтение ключа по типу из
// реестра.
//
// Тип задаёт формат: yaml-секрет читается как yaml, env — как
// dotenv. Без типа файл пришлось бы угадывать по расширению.
func TestGetReadsKeyByRegistryType(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(yamlRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(root, "nova", "demo.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("token: s3cret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app, out := newGetTestApp(t, root)
	if err := app.Get("nova", "demo", "token"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.TrimSpace(out.String()) != "s3cret" {
		t.Errorf("вывод = %q, ожидалось s3cret", out.String())
	}
}

// TestGetReportsUnknownKey проверяет подсказку для неизвестного ключа.
//
// Ключа нет, а секрет есть: состав ключей показывает реестр, поэтому
// подсказка ведёт к `adytum list`.
func TestGetReportsUnknownKey(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(yamlRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(root, "nova", "demo.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("token: s3cret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app, _ := newGetTestApp(t, root)
	err := app.Get("nova", "demo", "absent")
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестного ключа")
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatalf("ошибка без подсказки: %v", err)
	}
	if !strings.Contains(adviser.Advice().Text, "adytum list") {
		t.Errorf("подсказка = %q, ожидалось упоминание adytum list", adviser.Advice().Text)
	}
}
