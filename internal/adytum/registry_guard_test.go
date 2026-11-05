package adytum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lowern1ght/adytum/internal/registry"
)

// registryWithUnknownScope — реестр, где запись ссылается на scope,
// которого нет в разделе scopes.
//
// Такой реестр — источник тихой эскалации: правило для scope не
// создаётся, и файл уезжает под правило по умолчанию.
const registryWithUnknownScope = `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]

entries:
  vironima/deploy:
    type: env
    description: ключи деплоя
    scope: vironima
    keys:
      TOKEN: токен
`

// registryWithEmptyScope — реестр, где scope есть, но машин в нём
// нет, а записи на него ссылаются.
const registryWithEmptyScope = `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]
  personal: []

entries:
  personal/github:
    type: env
    description: GitHub PAT
    scope: personal
    keys:
      TOKEN: токен
`

// writeRegistryFixture кладёт реестр в каталог хранилища.
func writeRegistryFixture(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "registry.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestReencryptRefusesUnknownScope проверяет, что перешифровка
// отказывается работать с несогласованным реестром.
//
// Правило по умолчанию (^.*$) покрывает всё, чего не покрыл частный
// scope: файл раздела уехал бы получателям чужого раздела. Отказ
// тихой эскалации важнее удобства.
func TestReencryptRefusesUnknownScope(t *testing.T) {
	root := t.TempDir()
	writeRegistryFixture(t, root, registryWithUnknownScope)

	runner := &fakeRunner{}
	app := newTestApp(t, runner, root)

	err := app.Reencrypt(true)
	if err == nil {
		t.Fatal("ожидался отказ на scope, которого нет в реестре")
	}
	if !strings.Contains(err.Error(), "vironima") {
		t.Errorf("ошибка = %q, ожидалось упоминание проблемного scope", err.Error())
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if !strings.Contains(adviser.Advice().Text, "registry.yaml") {
		t.Errorf("подсказка = %q, ожидалось упоминание registry.yaml", adviser.Advice().Text)
	}
	if _, err := os.Stat(SopsConfigPath(root)); err == nil {
		t.Error(".sops.yaml создан при отказе: правила успели бы уехать под default")
	}
}

// TestReencryptRefusesEmptyScopeWithEntries проверяет, что перешифровка
// отказывается, когда scope пуст, а записи на него ссылаются.
//
// Пустой scope без записей — обычное состояние: доступ туда просто не
// выдавали. Пустой scope с записями — опасное: их файлы уедут под
// правило по умолчанию.
func TestReencryptRefusesEmptyScopeWithEntries(t *testing.T) {
	root := t.TempDir()
	writeRegistryFixture(t, root, registryWithEmptyScope)

	runner := &fakeRunner{}
	app := newTestApp(t, runner, root)

	err := app.Reencrypt(true)
	if err == nil {
		t.Fatal("ожидался отказ на пустом scope с записями")
	}
	if !strings.Contains(err.Error(), "personal") {
		t.Errorf("ошибка = %q, ожидалось упоминание пустого scope", err.Error())
	}
}

// TestReencryptAllowsEmptyScopeWithoutEntries проверяет, что scope без
// машин и без записей не мешает работе.
//
// Такой scope — обычное состояние нового хранилища: раздел есть,
// доступ в него ещё не выдавали.
func TestReencryptAllowsEmptyScopeWithoutEntries(t *testing.T) {
	root := t.TempDir()
	writeRegistryFixture(t, root, `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]
  personal: []

entries:
  nova/demo:
    type: env
    description: демонстрационный секрет
    scope: default
    keys:
      TOKEN: токен
`)

	runner := &fakeRunner{}
	app := newTestApp(t, runner, root)

	if err := app.Reencrypt(true); err != nil {
		t.Fatalf("Reencrypt: %v", err)
	}
}

// TestScopesEmptiedByRevokeDetectsLastMachine проверяет, что отзыв
// последней машины scope с записями распознаётся.
//
// Это состояние опасное: файлы записей уедут под правило по умолчанию.
func TestScopesEmptiedByRevokeDetectsLastMachine(t *testing.T) {
	loaded, err := registry.Parse([]byte(registryWithEmptyScope))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	emptied := scopesEmptiedByRevoke(loaded, "pc")
	if len(emptied) != 1 || emptied[0] != "personal" {
		t.Errorf("emptied = %v, ожидался [personal]", emptied)
	}
}

// TestScopesEmptiedByRevokeIgnoresOtherMachines проверяет, что scope с
// оставшимися машинами не считается опустевшим.
func TestScopesEmptiedByRevokeIgnoresOtherMachines(t *testing.T) {
	loaded, err := registry.Parse([]byte(`version: 1

recipients:
  pc: age1pcpublickey
  recovery: age1recoverykey

scopes:
  default: [pc, recovery]

entries:
  nova/demo:
    type: env
    description: демонстрационный секрет
    scope: default
    keys:
      TOKEN: токен
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	emptied := scopesEmptiedByRevoke(loaded, "pc")
	if len(emptied) != 0 {
		t.Errorf("emptied = %v, ожидался пустой список", emptied)
	}
}

// TestRevokeRefusesToEmptyScopeWithEntries проверяет, что отзыв
// последней машины раздела с записями отказывается.
//
// Отзыв оставил бы раздел без получателей, и его файлы уехали бы под
// правило по умолчанию — к чужим машинам. Отказ безопаснее тихой
// эскалации доступа.
func TestRevokeRefusesToEmptyScopeWithEntries(t *testing.T) {
	root := t.TempDir()
	writeRegistryFixture(t, root, `version: 1

recipients:
  pc: age1pcpublickey

scopes:
  default: [pc]
  personal: [pc]

entries:
  personal/github:
    type: env
    description: GitHub PAT
    scope: personal
    keys:
      TOKEN: токен
`)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stateDir := filepath.Join(home, ".adytum")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "recipients"), []byte("pc=age1pcpublickey\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newTestApp(t, &fakeRunner{}, root)

	err := app.Revoke("pc")
	if err == nil {
		t.Fatal("ожидался отказ от отзыва последней машины раздела с записями")
	}
	if !strings.Contains(err.Error(), "personal") {
		t.Errorf("ошибка = %q, ожидалось упоминание раздела", err.Error())
	}
}
