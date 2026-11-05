package adytum

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// BootstrapJSON печатает результат bootstrap-remote в машинночитаемом виде.
//
// Формат машинный: без цвета и символов.
func (a *App) BootstrapJSON(result BootstrapResult) error {
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	a.println(string(encoded))
	return nil
}

// BootstrapReport печатает результат bootstrap-remote человеку.
//
// Токен не печатается: в результате его нет, а в сообщения об ошибках
// он вычищается.
func (a *App) BootstrapReport(result BootstrapResult) {
	printer := a.ui()
	printer.OK("репозиторий: %s", result.URL)
	if result.Created {
		printer.Plain("создан приватный репозиторий %s/%s", result.Owner, result.Repo)
	} else {
		printer.Plain("репозиторий %s/%s уже существует", result.Owner, result.Repo)
	}
	if result.Pushed {
		printer.OK("изменения отправлены")
	}
	printer.Hint("проверьте состояние: adytum doctor")
}

// SchedulePrint устанавливает таймер и печатает его состояние.
//
// Формат выбирается флагом --json.
func (a *App) SchedulePrint() error {
	result, err := a.Schedule()
	if a.JSON {
		if printErr := a.scheduleJSON(result); printErr != nil {
			return printErr
		}
		if err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		if result.Reason != "" {
			a.ui().Fail("%s", result.Reason)
		}
		return err
	}
	printer := a.ui()
	printer.OK("таймер %s установлен, период %s", result.Unit, result.Interval)
	printer.Plain("юнит: %s", TimerService)
	printer.Hint("таймер только забирает изменения: adytum sync --pull-only")
	if result.Linger != "" {
		printer.Plain("linger: %s", result.Linger)
	}
	if strings.HasPrefix(result.Linger, "не удалось") {
		printer.Hint("без linger таймер не стартует: systemctl enable-linger %s", os.Getenv("USER"))
	}
	return nil
}

// scheduleJSON печатает состояние таймера в машинночитаемом виде.
func (a *App) scheduleJSON(result ScheduleResult) error {
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	a.println(string(encoded))
	return nil
}

// ScheduleStatusReport печатает текущее состояние таймера.
//
// Используется, когда таймер уже установлен и его нужно только
// показать.
func (a *App) ScheduleStatusReport() error {
	result := a.ScheduleStatus()
	if a.JSON {
		return a.scheduleJSON(result)
	}
	if !result.Active {
		return fmt.Errorf("таймер %s не активен", result.Unit)
	}
	a.ui().OK("таймер %s активен, период %s", result.Unit, result.Interval)
	return nil
}
