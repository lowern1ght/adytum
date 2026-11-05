package adytum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMachineIdentity кладёт ключ машины в ~/.adytum/keys.
//
// Публичный ключ нужен, чтобы определить, входит ли машина в
// получателей правила шифрования.
func writeMachineIdentity(t *testing.T, home, label, public string) {
	t.Helper()
	keysDir := filepath.Join(home, ".adytum", "keys")
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "# created: 2026-01-01T00:00:00Z\n# public key: " + public + "\nAGE-SECRET-KEY-1TEST\n"
	if err := os.WriteFile(filepath.Join(keysDir, label+".txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".adytum", "identity"), []byte(label+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestRecipientStateOfDetectsMachineInRule проверяет, что машина из
// правила распознаётся.
func TestRecipientStateOfDetectsMachineInRule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeMachineIdentity(t, home, "pc", "age1pcpublickey")

	root := t.TempDir()
	sops := "creation_rules:\n  - path_regex: '^nova[\\\\/]'\n    age:\n      - age1pcpublickey\n"
	if err := os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte(sops), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newTestApp(t, &fakeRunner{}, root)

	state := app.recipientStateOf(filepath.Join(root, "nova", "vcenter.env"))
	if state != recipientPresent {
		t.Errorf("состояние = %v, ожидалось recipientPresent", state)
	}
}

// TestRecipientStateOfDetectsMachineOutsideRule проверяет, что машина
// вне правила распознаётся как чужеродная для раздела.
func TestRecipientStateOfDetectsMachineOutsideRule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeMachineIdentity(t, home, "pc", "age1pcpublickey")

	root := t.TempDir()
	sops := "creation_rules:\n  - path_regex: '^nova[\\\\/]'\n    age:\n      - age1otherpublickey\n"
	if err := os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte(sops), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newTestApp(t, &fakeRunner{}, root)

	state := app.recipientStateOf(filepath.Join(root, "nova", "vcenter.env"))
	if state != recipientAbsent {
		t.Errorf("состояние = %v, ожидалось recipientAbsent", state)
	}
}

// TestCorruptedSecretErrorWarnsAboutWriter проверяет текст аварийной
// ошибки.
//
// Отличие от штатного «раздел не выдан» обязано быть заметным: файл
// повреждён или перешифрован без машины, и чинить это надо на
// писателе.
func TestCorruptedSecretErrorWarnsAboutWriter(t *testing.T) {
	err := corruptedSecretError(nil)

	if !strings.Contains(err.Error(), "повреждён") {
		t.Errorf("ошибка = %q, ожидалось упоминание повреждения", err.Error())
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if !strings.Contains(adviser.Advice().Text, "писател") {
		t.Errorf("подсказка = %q, ожидалось упоминание писателя", adviser.Advice().Text)
	}
}

// TestNotRecipientErrorStaysExpected проверяет, что штатная ситуация
// «раздел не выдан» остаётся спокойной.
//
// Аварийная подсказка на каждый чужой раздел научила бы агента
// чинить то, что не сломано.
func TestNotRecipientErrorStaysExpected(t *testing.T) {
	err := notRecipientError(nil)

	if !strings.Contains(err.Error(), "недоступна") {
		t.Errorf("ошибка = %q, ожидалось сообщение о недоступности", err.Error())
	}
	adviser, ok := err.(Adviser)
	if !ok {
		t.Fatal("ошибка не содержит подсказки")
	}
	if !strings.Contains(adviser.Advice().Text, "ожидаемо") {
		t.Errorf("подсказка = %q, ожидалось упоминание штатности", adviser.Advice().Text)
	}
}
