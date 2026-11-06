package adytum

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keysRegistry — реестр с секретами всех типов.
const keysRegistry = `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]

entries:
  nova/env:
    type: env
    description: env-секрет
    scope: default
  nova/yaml:
    type: yaml
    description: yaml-секрет
    scope: default
  nova/json:
    type: json
    description: json-секрет
    scope: default
  nova/blob:
    type: binary
    description: бинарный секрет
    scope: default
`

// newListKeysTestApp собирает App с активной разблокировкой.
//
// Разблокировка обязательна: команда расшифровывает секрет, и без
// маркера проверка ничего не сказала бы о составе ключей.
func newListKeysTestApp(t *testing.T, root string) (*App, *bytes.Buffer) {
	t.Helper()

	app := newUnlockedApp(t, root)
	var out bytes.Buffer
	app.Out = &out
	app.Err = &out
	return app, &out
}

// writeKeysStore готовит хранилище с реестром и файлами секретов.
func writeKeysStore(t *testing.T, root string, files map[string]string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(keysRegistry), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
}

// TestListKeysShowsEnvKeysInFileOrder проверяет имена ключей env-секрета.
//
// Порядок файла сохраняется: он подсказывает, в каком порядке секрет
// наполнялся, и не даёт выводу зависеть от сортировки.
func TestListKeysShowsEnvKeysInFileOrder(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/env.env": "GITLAB_HOST=gitlab.example.com\nGITLAB_TOKEN=s3cret\nGITLAB_SDK_PROJECT_ID=42\n",
	})

	app, out := newListKeysTestApp(t, root)
	if err := app.ListKeys("nova/env"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	output := out.String()
	for _, name := range []string{"GITLAB_HOST", "GITLAB_TOKEN", "GITLAB_SDK_PROJECT_ID"} {
		if !strings.Contains(output, name) {
			t.Errorf("в выводе нет ключа %s: %q", name, output)
		}
	}
	host := strings.Index(output, "GITLAB_HOST")
	token := strings.Index(output, "GITLAB_TOKEN")
	if host < 0 || token < 0 || host > token {
		t.Errorf("порядок ключей нарушен: %q", output)
	}
	if !strings.Contains(output, "3 ключа") {
		t.Errorf("в выводе нет счётчика ключей: %q", output)
	}
	// Значения в выводе быть не должно: ради имён агент и спрашивает.
	for _, value := range []string{"gitlab.example.com", "s3cret", "42"} {
		if strings.Contains(output, value) {
			t.Errorf("в выводе просочилось значение %q: %q", value, output)
		}
	}
}

// TestListKeysShowsYamlKeys проверяет имена ключей yaml-секрета.
func TestListKeysShowsYamlKeys(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/yaml.yaml": "url: https://example.com\nport: 443\n",
	})

	app, out := newListKeysTestApp(t, root)
	if err := app.ListKeys("nova/yaml"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	output := out.String()
	for _, name := range []string{"url", "port"} {
		if !strings.Contains(output, name) {
			t.Errorf("в выводе нет ключа %s: %q", name, output)
		}
	}
	if strings.Contains(output, "example.com") || strings.Contains(output, "443") {
		t.Errorf("в выводе просочилось значение: %q", output)
	}
}

// TestListKeysShowsJSONKeys проверяет имена ключей json-секрета.
func TestListKeysShowsJSONKeys(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/json.json": "{\n  \"token\": \"abc\",\n  \"count\": 2\n}\n",
	})

	app, out := newListKeysTestApp(t, root)
	if err := app.ListKeys("nova/json"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	output := out.String()
	for _, name := range []string{"token", "count"} {
		if !strings.Contains(output, name) {
			t.Errorf("в выводе нет ключа %s: %q", name, output)
		}
	}
	if strings.Contains(output, "abc") {
		t.Errorf("в выводе просочилось значение: %q", output)
	}
}

// TestListKeysDescribesBinarySecret проверяет описание бинарного секрета.
//
// У бинарного секрета ключей нет: показывается размер файла и тип
// содержимого — этого хватает, чтобы понять, что за файл лежит в
// хранилище.
func TestListKeysDescribesBinarySecret(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/blob.yaml": "\x00\x01\x02\x03binary-payload",
	})

	app, out := newListKeysTestApp(t, root)
	if err := app.ListKeys("nova/blob"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "байт") {
		t.Errorf("в выводе нет размера файла: %q", output)
	}
	if !strings.Contains(output, "application/octet-stream") {
		t.Errorf("в выводе нет типа содержимого: %q", output)
	}
	if strings.Contains(output, "binary-payload") {
		t.Errorf("в выводе просочилось содержимое: %q", output)
	}
}

// TestListKeysRefusesUndecryptableSecret проверяет отказ для секрета,
// который эта машина не расшифровывает.
//
// Отказ такой же, как у get: раздел машине не выдан. Значит, и состав
// ключей недоступен — иначе команда обходила бы разграничение доступа.
func TestListKeysRefusesUndecryptableSecret(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/env.env": "sops_version=3.9.0\nGITLAB_TOKEN=ENC[AES256_GCM,data:abc]\n",
	})

	app := newUnlockedApp(t, root)
	// Расшифровка идёт через sops с ключом машины: без идентичности
	// команда падает раньше, чем дойдёт до раздела.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	writeMachineIdentity(t, home, "pc", "age1pcpublickey")
	app.Runner = &fakeRunner{responses: map[string]fakeResponse{
		"decrypt": {err: errText("age: no identity matched any of the recipients")},
	}}

	err = app.ListKeys("nova/env")
	if err == nil {
		t.Fatal("ожидался отказ для недоступного раздела")
	}
	if !strings.Contains(err.Error(), "недоступна") {
		t.Errorf("ошибка = %q, ожидался отказ по доступу", err.Error())
	}
}

// TestListKeysHidesValueFromParseError проверяет, что ошибка разбора не
// цитирует значение.
//
// Парсер yaml показывает то, что не смог разобрать, — а это может
// оказаться самим секретом. Ошибка разбора поэтому остаётся без текста
// парсера.
func TestListKeysHidesValueFromParseError(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/yaml.yaml": "s3cret\n",
	})

	app, out := newListKeysTestApp(t, root)
	err := app.ListKeys("nova/yaml")
	if err == nil {
		t.Fatal("ожидалась ошибка разбора")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("в ошибке просочилось значение: %q", err.Error())
	}
	if strings.Contains(out.String(), "s3cret") {
		t.Errorf("в выводе просочилось значение: %q", out.String())
	}
}

// TestListKeysReportsUnknownEntry проверяет отказ для неизвестной записи.
func TestListKeysReportsUnknownEntry(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{})

	app, _ := newListKeysTestApp(t, root)
	if err := app.ListKeys("nova/other"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестной записи")
	}
}

// TestListKeysRequiresEntry проверяет отказ без указания записи.
//
// Список без записи лишён смысла: имена ключей есть только у
// конкретного секрета.
func TestListKeysRequiresEntry(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{})

	app, _ := newListKeysTestApp(t, root)
	err := app.ListKeys("")
	if err == nil {
		t.Fatal("ожидалась ошибка без указания записи")
	}
	if !strings.Contains(err.Error(), "--keys") {
		t.Errorf("ошибка = %q, ожидалось упоминание --keys", err.Error())
	}
}

// TestListKeysJSONIsMachineReadable проверяет JSON-вывод.
//
// Агент читает вывод программно: состав ключей нужен в виде списка,
// а не в виде текста с отступами.
func TestListKeysJSONIsMachineReadable(t *testing.T) {
	root := t.TempDir()
	writeKeysStore(t, root, map[string]string{
		"nova/env.env": "GITLAB_HOST=gitlab.example.com\nGITLAB_TOKEN=s3cret\n",
	})

	app, out := newListKeysTestApp(t, root)
	app.JSON = true
	if err := app.ListKeys("nova/env"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	var report ListKeysReport
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("JSON не разбирается: %v (%q)", err, out.String())
	}
	if len(report.Keys) != 2 {
		t.Errorf("ключей = %d, ожидалось 2 (%q)", len(report.Keys), out.String())
	}
	if strings.Contains(out.String(), "s3cret") {
		t.Errorf("в JSON просочилось значение: %q", out.String())
	}
}
