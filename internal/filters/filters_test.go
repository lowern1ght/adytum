package filters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanScriptPassesEmptyFileThrough(t *testing.T) {
	script := CleanScript()

	if !strings.Contains(script, "tr -d '[:space:]'") {
		t.Error("clean-фильтр должен проверять файл на пустоту")
	}
	if !strings.Contains(script, "sops encrypt --filename-override") {
		t.Error("clean-фильтр должен шифровать через sops")
	}
}

func TestCleanScriptHasNoKeyFile(t *testing.T) {
	// Шифрованию приватный ключ не нужен: получатели берутся из
	// .sops.yaml. Ключ в скрипте означал бы лишний секрет на диске.
	if strings.Contains(CleanScript(), "SOPS_AGE_KEY_FILE") {
		t.Error("clean-фильтр не должен содержать путь к приватному ключу")
	}
}

func TestSmudgeScriptFallsBackToCiphertext(t *testing.T) {
	script := SmudgeScript("/keys/pc.txt")

	if !strings.Contains(script, "cat \"$tmp_in\"") {
		t.Error("smudge-фильтр должен возвращать исходный шифротекст")
	}
	if !strings.Contains(script, "2>/dev/null") {
		t.Error("smudge-фильтр должен скрывать ошибки sops")
	}
	if !strings.Contains(script, "/keys/pc.txt") {
		t.Error("smudge-фильтр должен содержать путь к ключу машины")
	}
}

func TestSmudgeScriptKeepsFailedDecryptOutput(t *testing.T) {
	// Наивный вариант 'sops decrypt || cat' даёт пустой файл: sops
	// съедает stdin и падает. Обёртка обязана читать ввод во временный
	// файл и возвращать его содержимое.
	script := SmudgeScript("/keys/pc.txt")

	if !strings.Contains(script, "cat > \"$tmp_in\"") {
		t.Error("smudge-фильтр должен буферизовать stdin")
	}
	if !strings.Contains(script, "cat \"$tmp_out\"") {
		t.Error("smudge-фильтр должен выводить расшифрованное содержимое")
	}
}

func TestInstallWritesScripts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")

	scripts, err := Install(dir, "/keys/pc.txt")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	for _, path := range []string{scripts.Clean, scripts.Smudge} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Errorf("скрипт %s пуст", path)
		}
	}
	if scripts.Clean != filepath.Join(dir, CleanName) {
		t.Errorf("Clean = %q, ожидалось %q", scripts.Clean, filepath.Join(dir, CleanName))
	}
	if scripts.Smudge != filepath.Join(dir, SmudgeName) {
		t.Errorf("Smudge = %q, ожидалось %q", scripts.Smudge, filepath.Join(dir, SmudgeName))
	}
}

func TestInstallOverwritesExistingScripts(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, "/keys/old.txt"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := Install(dir, "/keys/new.txt"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, SmudgeName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "/keys/new.txt") {
		t.Error("повторная установка должна обновлять скрипт")
	}
}

func TestConfigValuesUseForwardSlashes(t *testing.T) {
	// Обратные слэши в значении config превращаются в экранирование:
	// shell не найдёт файл.
	values := ConfigValues(Scripts{
		Clean:  `C:\Users\me\.adytum\bin\adytum-clean.sh`,
		Smudge: `C:\Users\me\.adytum\bin\adytum-smudge.sh`,
	})

	for _, key := range []string{"filter.adytum.clean", "filter.adytum.smudge"} {
		if strings.Contains(values[key], `\`) {
			t.Errorf("%s содержит обратные слэши: %s", key, values[key])
		}
		if !strings.Contains(values[key], "%f") {
			t.Errorf("%s должен передавать имя файла: %s", key, values[key])
		}
	}
	if values["filter.adytum.required"] != "true" {
		t.Errorf("filter.adytum.required = %q, ожидалось true", values["filter.adytum.required"])
	}
}
