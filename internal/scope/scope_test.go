package scope

import (
	"os"
	"path/filepath"
	"testing"
)

// withHome подставляет временный домашний каталог.
//
// Файл настройки лежит в домашнем каталоге, иначе тесты трогали бы
// настоящую машину.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func TestLoadReadsMachineScope(t *testing.T) {
	withHome(t)

	if err := Save(Config{MachineScope: []string{"nova", "comuki", "personal"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(config.MachineScope) != 3 {
		t.Fatalf("machine_scope = %v, ожидалось 3 раздела", config.MachineScope)
	}
	if !config.Allows("nova") {
		t.Error("раздел nova не разрешён")
	}
	if config.Allows("unknown") {
		t.Error("раздел unknown не должен быть разрешён")
	}
}

func TestLoadWithoutFileReturnsEmptyScope(t *testing.T) {
	withHome(t)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(config.MachineScope) != 0 {
		t.Errorf("machine_scope = %v, ожидалось пусто", config.MachineScope)
	}
	if config.Allows("nova") {
		t.Error("без настройки ни один раздел не разрешён")
	}
}

func TestLoadRejectsBrokenFile(t *testing.T) {
	home := withHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".adytum"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".adytum", FileName), []byte("machine_scope: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("ожидалась ошибка разбора битой настройки")
	}
}

func TestSaveSortsAndDeduplicates(t *testing.T) {
	withHome(t)

	if err := Save(Config{MachineScope: []string{"personal", "nova", "nova", "  ", "comuki"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(config.MachineScope) != 3 {
		t.Fatalf("machine_scope = %v, ожидалось 3 раздела", config.MachineScope)
	}
	if config.MachineScope[0] != "comuki" {
		t.Errorf("первый раздел = %q, ожидался comuki", config.MachineScope[0])
	}
}

func TestSectionExtractsTopLevelDirectory(t *testing.T) {
	cases := map[string]string{
		"personal/github.env":     "personal",
		"nova/vcenter.env":        "nova",
		"assistant/openclaw.json": "assistant",
		"root.env":                "",
		"./nova/gitlab.env":       "nova",
	}
	for path, expected := range cases {
		if got := Section(path); got != expected {
			t.Errorf("Section(%q) = %q, ожидалось %q", path, got, expected)
		}
	}
}

func TestAllowsRejectsEmptySection(t *testing.T) {
	config := Config{MachineScope: []string{"nova"}}

	if config.Allows("") {
		t.Error("пустой раздел не должен быть разрешён")
	}
}
