package filters

import (
	"os"
	"os/exec"
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

// TestCleanScriptPassesCiphertextThrough проверяет, что clean-фильтр не
// шифрует уже зашифрованное.
//
// Машина-не-получатель получает blob через smunge-фолбэк как есть:
// повторное шифрование дало бы другой blob, дерево оставалось бы
// вечно грязным, а операции git падали бы.
func TestCleanScriptPassesCiphertextThrough(t *testing.T) {
	script := CleanScript()

	if !strings.Contains(script, "sops_[A-Za-z0-9_]+=") {
		t.Error("clean-фильтр должен распознавать метаданные sops в env-файле")
	}
	if !strings.Contains(script, "sops:") {
		t.Error("clean-фильтр должен распознавать метаданные sops в yaml")
	}
	if !strings.Contains(script, `"sops":`) {
		t.Error("clean-фильтр должен распознавать метаданные sops в json")
	}
}

// TestCleanScriptDetectsCiphertextBeforeEncrypting проверяет порядок
// проверок.
//
// Распознавание шифротекста должно идти до шифрования: иначе sops
// получил бы шифротекст на вход и упал бы.
func TestCleanScriptDetectsCiphertextBeforeEncrypting(t *testing.T) {
	script := CleanScript()

	detect := strings.Index(script, "grep -qE")
	encrypt := strings.Index(script, "sops encrypt")
	if detect < 0 || encrypt < 0 {
		t.Fatalf("в скрипте нет проверки шифротекста или шифрования: %s", script)
	}
	if detect > encrypt {
		t.Error("проверка шифротекста должна идти до шифрования")
	}
}

// findShell ищет POSIX-шелл для запуска скриптов фильтров.
//
// На Windows шелл из Git-for-Windows лежит вовне не в PATH: поиск теста
// пропускает, но тест не падает. Если шелла нет вовсе — тест
// пропускается.
func findShell(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	for _, candidate := range []string{
		`C:\Program Files\Git\usr\bin\sh.exe`,
		`C:\Program Files (x86)\Git\usr\bin\sh.exe`,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("POSIX-шелл недоступен на этой машине")
	return ""
}

// TestCleanScriptIsIdempotentOnCiphertext проверяет, что clean-фильтр не
// шифрует уже зашифрованное.
//
// Машина-не-получатель получает blob как есть через
// smudge-фолбэк: повторное шифрование шифротекста дало бы
// другой blob, и дерево осталась бы вечно грязным.
//
// Сторона проверяет, что шифрование вовсе не
// вызывается: стуб sops падает с ошибкой, и шифротекст
// выводится без изменений.
func TestCleanScriptIsIdempotentOnCiphertext(t *testing.T) {
	sh := findShell(t)

	dir := t.TempDir()
	stubDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stubDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stub := filepath.Join(stubDir, "sops")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'sops не должен быть вызван' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ciphertext := "TOKEN=ENC[AES256_GCM,data:abc,iv:iv,tag:tag,type:str]\nsops_age__list_0__map_recipient=age1example\nsops_version=3.8.1\n"

	scriptPath := filepath.Join(dir, CleanName)
	if err := os.WriteFile(scriptPath, []byte(CleanScript()), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	command := exec.Command(sh, scriptPath, "secret.env")
	command.Stdin = strings.NewReader(ciphertext)
	// В PATH должны каталог шелла: поддельные утилиты
	// (mktemp, tr, grep) лежат рядом с ним, а не в папке Git-for-Windows.
	searchPath := stubDir + string(os.PathListSeparator) + filepath.Dir(sh) + string(os.PathListSeparator) + os.Getenv("PATH")
	command.Env = append(os.Environ(), "PATH="+searchPath)

	out, err := command.Output()
	if err != nil {
		t.Fatalf("фильтр отработал ошибку: %v", err)
	}
	if string(out) != ciphertext {
		t.Errorf("шифротекст изменён:\nполучено: %q\nожидалось: %q", ciphertext, string(out))
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

	scripts, err := Install(dir, "/keys/pc.txt", nil)
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
	if _, err := Install(dir, "/keys/old.txt", nil); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := Install(dir, "/keys/new.txt", nil); err != nil {
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

func TestInstallWritesScopeAwareSmudge(t *testing.T) {
	// Настроенный machine_scope меняет поведение smunge-фильтра:
	// файл вне списка разделов остаётся шифротекстом.
	dir := filepath.Join(t.TempDir(), "bin")

	scripts, err := Install(dir, "/keys/pc.txt", []string{"personal"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	content, err := os.ReadFile(scripts.Smudge)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "personal") {
		t.Errorf("smudge-фильтр не знает разделов machine_scope: %q", string(content))
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
