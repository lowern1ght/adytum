package adytum

import (
	"os"
	"strings"
	"testing"
)

// helperEnv — переменная окружения, помечающая вспомогательный
// процесс.
//
// Без неё тест идёт обычным порядком: вспомогательный процесс не
// должен прерывать прогон остальных тестов.
const helperEnv = "ADYTUM_TEST_HELPER"

// helperModeEnv — переменная окружения, задающая режим
// вспомогательного процесса.
const helperModeEnv = "ADYTUM_TEST_HELPER_MODE"

// TestExecSucceedsWhenCommandOnlyWritesToStderr проверяет, что успех
// команды определяется кодом возврата.
//
// Git и sops пишут в stderr предупреждения и прогресс: команда, всё
// это выведшая и завершившаяся с кодом 0, выполнена. Ошибка должна
// возникать по коду возврата, а не по непустому stderr.
func TestExecSucceedsWhenCommandOnlyWritesToStderr(t *testing.T) {
	out, err := Exec{}.Run(Command{Env: []string{helperEnv + "=1"}}, os.Args[0], "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("вывод = %q, ожидался пустой", out)
	}
}

// TestExecFailsOnNonZeroExitCode проверяет, что ненулевой код
// возврата даёт ошибку с текстом stderr.
func TestExecFailsOnNonZeroExitCode(t *testing.T) {
	env := []string{helperEnv + "=1", helperModeEnv + "=stderr"}

	_, err := Exec{}.Run(Command{Env: env}, os.Args[0], "-test.run=TestHelperProcess")
	if err == nil {
		t.Fatal("ожидалась ошибка при ненулевом коде возврата")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("ошибка = %q, ожидалось сообщение из stderr", err.Error())
	}
}

// TestExecRunsProgramInWorkingDirectory проверяет, что команда
// выполняется в заданном каталоге.
func TestExecRunsProgramInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	env := []string{helperEnv + "=1", helperModeEnv + "=pwd"}

	out, err := Exec{}.Run(Command{Dir: dir, Env: env}, os.Args[0], "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(out), dir) {
		t.Errorf("рабочий каталог = %q, ожидался %q", out, dir)
	}
}

// TestHelperProcess — вспомогательный процесс для проверок Exec.
//
// Режим задаётся переменной окружения: stderr — писать в stderr и
// завершиться с кодом 3, pwd — напечатать рабочий каталог.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	switch os.Getenv(helperModeEnv) {
	case "stderr":
		os.Stderr.WriteString("reason\n")
		os.Exit(3)
	case "pwd":
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		os.Stdout.WriteString(dir)
		os.Exit(0)
	}
	os.Exit(0)
}
