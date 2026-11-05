package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lowern1ght/adytum/internal/adytum"
)

// newTestApp собирает App с пустым хранилищем.
//
// Корень нужен для проверки существования файла секрета: имя сервиса
// с точками отличается от ключа только наличием такого файла.
func newTestApp(t *testing.T) *adytum.App {
	t.Helper()
	root := t.TempDir()
	app := adytum.New(adytum.Exec{})
	if err := app.RequireRoot(root); err != nil {
		t.Fatalf("RequireRoot: %v", err)
	}
	return app
}

// writeSecret создаёт файл секрета в хранилище.
func writeSecret(t *testing.T, app *adytum.App, relative string) {
	t.Helper()
	path := filepath.Join(app.Root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("KEY=1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestSplitKeyReadsKeyFromServiceName проверяет обычное чтение ключа.
func TestSplitKeyReadsKeyFromServiceName(t *testing.T) {
	app := newTestApp(t)
	writeSecret(t, app, "nova/vcenter.env")

	project, service, key, err := splitKey(app, "nova/vcenter.VSPHERE_USER")
	if err != nil {
		t.Fatalf("splitKey: %v", err)
	}
	if project != "nova" || service != "vcenter" || key != "VSPHERE_USER" {
		t.Errorf("разобрано %q/%q/%q, ожидалось nova/vcenter/VSPHERE_USER", project, service, key)
	}
}

// TestSplitKeyKeepsDottedServiceName проверяет имя сервиса с точками.
//
// Имя файла отличает сервис от ключа: пока файла нет, адрес с точками
// читается как ключ, а существующий файл — как имя сервиса.
func TestSplitKeyKeepsDottedServiceName(t *testing.T) {
	app := newTestApp(t)
	writeSecret(t, app, "assistant/vpn.amnezia.json")

	project, service, key, err := splitKey(app, "assistant/vpn.amnezia")
	if err != nil {
		t.Fatalf("splitKey: %v", err)
	}
	if project != "assistant" || service != "vpn.amnezia" || key != "" {
		t.Errorf("разобрано %q/%q/%q, ожидалось assistant/vpn.amnezia без ключа", project, service, key)
	}
}

// TestSplitKeyRejectsDottedKey проверяет отказ на ключе с точками.
//
// Ключ с точками не поддерживается: такой адрес молча читался бы как
// другой секрет, и агент получил бы не то значение.
func TestSplitKeyRejectsDottedKey(t *testing.T) {
	app := newTestApp(t)
	writeSecret(t, app, "nova/vcenter.env")

	_, _, _, err := splitKey(app, "nova/vcenter.prod.KEY")
	if err == nil {
		t.Fatal("ожидался отказ на ключе с точками")
	}
	if !strings.Contains(err.Error(), "точками") {
		t.Errorf("ошибка = %q, ожидалось упоминание точек", err.Error())
	}
}

// TestSplitKeyTreatsExtensionAsServiceName проверяет адрес с расширением.
//
// Расширение файла — часть имени, а не ключ.
func TestSplitKeyTreatsExtensionAsServiceName(t *testing.T) {
	app := newTestApp(t)
	writeSecret(t, app, "nova/vcenter.env")

	project, service, key, err := splitKey(app, "nova/vcenter.env")
	if err != nil {
		t.Fatalf("splitKey: %v", err)
	}
	if project != "nova" || service != "vcenter.env" || key != "" {
		t.Errorf("разобрано %q/%q/%q, ожидалось nova/vcenter.env без ключа", project, service, key)
	}
}

// TestRunGetRejectsMultipleTargets проверяет отказ на нескольких
// адресах.
//
// get читает один секрет: молча проигнорированный второй адрес
// выглядел бы как «секрет прочитан», а взят был бы первый.
func TestRunGetRejectsMultipleTargets(t *testing.T) {
	app := newTestApp(t)

	err := runGet(app, []string{"nova/vcenter", "personal/github"})
	if err == nil {
		t.Fatal("ожидался отказ на нескольких адресах")
	}
	if !strings.Contains(err.Error(), "адреса") {
		t.Errorf("ошибка = %q, ожидалось упоминание числа адресов", err.Error())
	}
}

// TestRunUnlockRejectsExtraArgument проверяет отказ на лишнем аргументе.
//
// unlock ничего не принимает: проигнорированный аргумент выглядит
// как «я всё сделал правильно».
func TestRunUnlockRejectsExtraArgument(t *testing.T) {
	app := newTestApp(t)

	err := runUnlock(app, []string{"pc"})
	if err == nil {
		t.Fatal("ожидался отказ на лишнем аргументе")
	}
	if !strings.Contains(err.Error(), "pc") {
		t.Errorf("ошибка = %q, ожидалось упоминание аргумента", err.Error())
	}
}
