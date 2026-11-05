package adytum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeWithSecret готовит хранилище с одним файлом секрета.
func storeWithSecret(t *testing.T, relative string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

// TestRequireEncryptedPathAcceptsCoveredFile проверяет, что файл под
// правилом фильтра проходит проверку.
func TestRequireEncryptedPathAcceptsCoveredFile(t *testing.T) {
	root := storeWithSecret(t, "personal/github.env")
	app := newTestApp(t, &fakeRunner{}, root)

	if err := app.requireEncryptedPath("personal/github.env"); err != nil {
		t.Fatalf("requireEncryptedPath: %v", err)
	}
}

// TestRequireEncryptedPathRefusesUncoveredFile проверяет, что файл вне
// правил фильтра не проходит.
//
// Без правила в .gitattributes шифротекст не создаётся: файл уехал бы
// в remote открытым.
func TestRequireEncryptedPathRefusesUncoveredFile(t *testing.T) {
	root := storeWithSecret(t, "personal/github.env")
	runner := &fakeRunner{attrValue: attrUnspecified}
	app := newTestApp(t, runner, root)

	err := app.requireEncryptedPath("personal/github.env")
	if err == nil {
		t.Fatal("ожидался отказ для файла вне фильтра")
	}
	if !strings.Contains(err.Error(), "открытым") {
		t.Errorf("ошибка = %q, ожидалось упоминание открытого содержимого", err.Error())
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if !strings.Contains(adviser.Advice().Text, ".gitattributes") {
		t.Errorf("подсказка = %q, ожидалось упоминание .gitattributes", adviser.Advice().Text)
	}
}

// TestRequireEncryptedSecretsRefusesUncoveredFile проверяет, что
// коммит файла вне фильтра не проходит.
//
// Коммит забирает всё: один непокрытый файл ушёл бы в remote открытым.
func TestRequireEncryptedSecretsRefusesUncoveredFile(t *testing.T) {
	root := storeWithSecret(t, "personal/github.env")
	runner := &fakeRunner{attrValue: attrUnspecified}
	app := newTestApp(t, runner, root)

	err := app.requireEncryptedSecrets()
	if err == nil {
		t.Fatal("ожидался отказ для файла вне фильтра")
	}
	if !strings.Contains(err.Error(), "personal/github.env") {
		t.Errorf("ошибка = %q, ожидалось упоминание файла", err.Error())
	}
}

// TestRequireEncryptedSecretsAcceptsExplicitException проверяет, что
// файл с явным исключением (-filter) проходит.
//
// Исключение — решение владельца: пустой файл или маска не секрет.
func TestRequireEncryptedSecretsAcceptsExplicitException(t *testing.T) {
	root := storeWithSecret(t, "personal/openapi.json")
	runner := &fakeRunner{attrValue: attrUnset}
	app := newTestApp(t, runner, root)

	if err := app.requireEncryptedSecrets(); err != nil {
		t.Fatalf("requireEncryptedSecrets: %v", err)
	}
}

// TestDoctorReportsFilterCoverage проверяет, что doctor замечает
// раздел, не покрытый фильтром.
//
// Без такой проверки владелец узнаёт о проблеме только тогда, когда
// plaintext уже лежит в remote.
func TestDoctorReportsFilterCoverage(t *testing.T) {
	root := storeWithSecret(t, "personal/github.env")
	runner := &fakeRunner{attrValue: attrUnspecified}
	app := newTestApp(t, runner, root)

	check := app.checkFilterCoverage()
	if check.OK {
		t.Error("проверка не заметила файл вне фильтра")
	}
	if !strings.Contains(check.Detail, "personal/github.env") {
		t.Errorf("деталь = %q, ожидалось упоминание файла", check.Detail)
	}
	if !strings.Contains(check.Fix, ".gitattributes") {
		t.Errorf("подсказка = %q, ожидалось упоминание .gitattributes", check.Fix)
	}
}

// TestDoctorFilterCoveragePassesForCoveredFiles проверяет, что
// покрытые файлы считаются исправным состоянием.
func TestDoctorFilterCoveragePassesForCoveredFiles(t *testing.T) {
	root := storeWithSecret(t, "personal/github.env")
	app := newTestApp(t, &fakeRunner{}, root)

	check := app.checkFilterCoverage()
	if !check.OK {
		t.Errorf("покрытые файлы сочтены непокрытыми: %+v", check)
	}
}
