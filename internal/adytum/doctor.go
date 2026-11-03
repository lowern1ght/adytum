package adytum

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/staleness"
	"github.com/lowern1ght/adytum/internal/store"
)

// Check — результат одной проверки самодиагностики.
type Check struct {
	// Name — название проверки.
	Name string `json:"name"`
	// OK — проверка пройдена.
	OK bool `json:"ok"`
	// Detail — подробности: версия, путь, значение.
	Detail string `json:"detail"`
	// Fix — что сделать, если проверка не пройдена.
	Fix string `json:"fix,omitempty"`
}

// DoctorReport — полный отчёт самодиагностики.
type DoctorReport struct {
	// Checks — результаты проверок.
	Checks []Check `json:"checks"`
	// OK — все ли проверки пройдены.
	OK bool `json:"ok"`
}

// Doctor выполняет самопроверку окружения.
//
// Проверяются: наличие и версии внешних инструментов, идентичность
// машины, git-фильтры, конфигурация sops, remote, маркер
// разблокировки, таймер автосинхронизации и свежесть секретов.
func (a *App) Doctor() DoctorReport {
	report := DoctorReport{OK: true}
	for _, check := range a.doctorChecks() {
		report.Checks = append(report.Checks, check)
		if !check.OK {
			report.OK = false
		}
	}
	return report
}

// doctorChecks собирает список проверок.
//
// Порядок важен: сначала то, без чего остальное не работает.
func (a *App) doctorChecks() []Check {
	checks := []Check{
		a.checkTool("sops", "sops", "--version"),
		a.checkTool("age", "age", "--version"),
		a.checkIdentity(),
		a.checkFilters(),
		a.checkSopsConfig(),
		a.checkRemote(),
		a.checkUnlock(),
		a.checkTimer(),
		a.checkStaleness(),
	}
	checks = append(checks, a.registryChecks()...)
	return checks
}

// checkTool проверяет наличие внешнего инструмента и его версию.
func (a *App) checkTool(name, binary string, versionArgs ...string) Check {
	check := Check{Name: name}
	if err := Require(binary); err != nil {
		check.Fix = "установите " + binary + " и повторите"
		return check
	}
	out, err := a.Runner.Run(Command{}, binary, versionArgs...)
	if err != nil {
		check.OK = true
		check.Detail = "установлен"
		return check
	}
	version := strings.TrimSpace(string(out))
	if index := strings.IndexAny(version, "\r\n"); index > 0 {
		version = version[:index]
	}
	check.OK = true
	check.Detail = version
	return check
}

// checkIdentity проверяет метку и приватный ключ машины.
func (a *App) checkIdentity() Check {
	check := Check{Name: "identity"}
	label, err := store.IdentityLabel()
	if err != nil {
		check.Fix = "adytum identity <label>"
		return check
	}
	keyFile, err := store.KeyFile(label)
	if err != nil {
		check.Fix = "adytum identity <label>"
		return check
	}
	if _, err := os.Stat(keyFile); err != nil {
		check.Detail = label
		check.Fix = "файл ключа не найден: " + keyFile
		return check
	}
	check.OK = true
	check.Detail = label
	return check
}

// checkFilters проверяет регистрацию git-фильтров adytum.
//
// Проверяются не только значения конфигурации, но и сами скрипты:
// без них git падает на каждой операции с секретами.
func (a *App) checkFilters() Check {
	check := Check{Name: "git-фильтры"}
	required := []string{"filter.adytum.clean", "filter.adytum.smudge", "filter.adytum.required"}
	var missing []string
	for _, key := range required {
		value, err := a.gitOutput("config", "--get", key)
		if err != nil || value == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		check.Detail = "не настроены: " + strings.Join(missing, ", ")
		check.Fix = "adytum init"
		return check
	}
	binDir, err := store.BinDir()
	if err != nil {
		check.OK = true
		check.Detail = "настроены"
		return check
	}
	var absent []string
	for _, name := range []string{"adytum-clean.sh", "adytum-smudge.sh"} {
		if _, err := os.Stat(filepath.Join(binDir, name)); err != nil {
			absent = append(absent, name)
		}
	}
	if len(absent) > 0 {
		check.Detail = "нет скриптов: " + strings.Join(absent, ", ")
		check.Fix = "adytum init"
		return check
	}
	check.OK = true
	check.Detail = "настроены"
	return check
}

// checkSopsConfig проверяет .sops.yaml и список получателей.
func (a *App) checkSopsConfig() Check {
	check := Check{Name: ".sops.yaml"}
	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err != nil {
		check.Fix = "adytum init"
		return check
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		check.Detail = err.Error()
		check.Fix = "исправьте .sops.yaml"
		return check
	}
	recipients := config.Recipients()
	if len(recipients) == 0 {
		check.Detail = "получатели не заданы"
		check.Fix = "adytum grant <label>"
		return check
	}
	recipients2, err := store.LoadRecipients()
	if err == nil {
		if unresolved := config.UnresolvedPlaceholders(recipients2); len(unresolved) > 0 {
			check.Detail = "не заполнены слоты: " + strings.Join(unresolved, ", ")
			check.Fix = "adytum grant <label>"
			return check
		}
	}
	check.OK = true
	check.Detail = fmt.Sprintf("получателей: %d", len(recipients))
	return check
}

// checkRemote проверяет настройку и доступность remote.
func (a *App) checkRemote() Check {
	check := Check{Name: "remote"}
	remote, err := a.gitOutput("remote", "get-url", "origin")
	if err != nil || remote == "" {
		check.Fix = "adytum bootstrap-remote"
		return check
	}
	check.OK = true
	check.Detail = remote
	return check
}

// checkUnlock проверяет состояние маркера разблокировки.
func (a *App) checkUnlock() Check {
	check := Check{Name: "разблокировка"}
	state, err := store.Unlock(a.Now())
	if err != nil {
		check.Fix = "adytum unlock"
		return check
	}
	if !state.Unlocked {
		check.Detail = "не активна"
		check.Fix = "adytum unlock"
		return check
	}
	check.OK = true
	check.Detail = "активна, осталось " + state.Remaining.Round(time.Minute).String()
	return check
}

// checkTimer проверяет таймер автосинхронизации.
func (a *App) checkTimer() Check {
	check := Check{Name: "таймер"}
	active, err := a.timerActive()
	if err != nil {
		check.Detail = "не настроен"
		check.Fix = "adytum schedule"
		return check
	}
	if !active {
		check.Detail = "не активен"
		check.Fix = "adytum schedule"
		return check
	}
	check.OK = true
	check.Detail = "активен"
	return check
}

// checkStaleness проверяет свежесть локальной копии секретов.
func (a *App) checkStaleness() Check {
	check := Check{Name: "свежесть секретов"}
	result := a.stalenessChecker().Check()
	if result.Err != nil {
		check.Detail = "не удалось проверить: " + result.Err.Error()
		check.Fix = "проверьте доступ к remote: adytum sync"
		return check
	}
	if result.Stale() {
		check.Detail = fmt.Sprintf("origin/%s впереди на %d", staleness.DefaultBranch, result.Behind)
		check.Fix = "adytum sync"
		return check
	}
	check.OK = true
	check.Detail = "актуально"
	return check
}

// timerActive сообщает, активен ли таймер автосинхронизации.
func (a *App) timerActive() (bool, error) {
	if _, err := a.Runner.Run(Command{}, "systemctl", "--user", "is-active", TimerUnit); err == nil {
		return true, nil
	}
	if _, err := a.Runner.Run(Command{}, "systemctl", "is-active", TimerUnit); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("таймер не найден")
}

// DoctorJSON печатает отчёт самодиагностики в формате JSON.
//
// Формат машинный: без цвета и символов, каждая проверка — отдельный
// объект с подсказкой по исправлению.
func (a *App) DoctorJSON() error {
	report := a.Doctor()
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	a.println(string(encoded))
	return nil
}

// DoctorText печатает отчёт самодиагностики таблицей.
//
// Каждая строка — проверка, её результат и подсказка, что делать при
// неудаче.
func (a *App) DoctorText() error {
	report := a.Doctor()
	printer := a.ui()
	for _, check := range report.Checks {
		if check.OK {
			printer.OK("%-18s %s", check.Name, check.Detail)
			continue
		}
		printer.Fail("%-18s %s", check.Name, check.Detail)
		if check.Fix != "" {
			printer.Hint("%s", check.Fix)
		}
	}
	if report.OK {
		printer.OK("все проверки пройдены")
		return nil
	}
	// Итог — часть отчёта, а не отдельная ошибка: он должен идти
	// сразу за таблицей, а не после сообщения об ошибке в stderr.
	printer.Fail("найдены проблемы: поправьте их по подсказкам выше")
	return ErrDoctorFailed
}

// DoctorPrint печатает отчёт в выбранном формате.
//
// Формат выбирается флагом --json.
func (a *App) DoctorPrint() error {
	if a.JSON {
		return a.DoctorJSON()
	}
	return a.DoctorText()
}
