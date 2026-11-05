package adytum

import (
	"time"

	"github.com/lowern1ght/adytum/internal/machine"
	"github.com/lowern1ght/adytum/internal/store"
) // profile — профиль машины: роль, режим pull-only и разделы.
// Профиль читается из ~/.adytum/config. Если файла нет, роль
// определяется по окружению: машина под systemd без графической сессии
// считается фоновой.
func (a *App) profile() (machine.Config, error) {
	config, err := machine.Load()
	if err != nil {
		return machine.Config{}, err
	}
	if a.Role != "" {
		config.Role = machine.Role(a.Role)
	} else if config.Role == machine.RoleUnknown {
		config.Role = machine.Detect()
	}
	return config, nil
}

// role возвращает профиль машины.
//
// Ошибка чтения настройки не должна ломать команду: роль влияет
// только на подсказки и набор проверок, а не на доступ к секретам.
func (a *App) role() machine.Role {
	config, err := a.profile()
	if err != nil {
		return machine.Detect()
	}
	return config.Role
}

// pullOnly сообщает, работает ли машина только на чтение.
//
// В режиме pull-only машина не публикует изменения: таймер забирает
// чужие правки и никогда не отправляет свои.
func (a *App) pullOnly() bool {
	config, err := a.profile()
	if err != nil {
		return false
	}
	return config.PullOnly
}

// unlockTTL возвращает срок жизни маркера разблокировки.
//
// Настройка машины важнее дефолта: у фоновой машины маркер обновляет
// таймер, и дефолт в несколько часов закрывал бы гейт после одного
// пропущенного тика.
func (a *App) unlockTTL() time.Duration {
	config, err := a.profile()
	if err != nil {
		return store.DefaultUnlockTTL
	}
	return config.TTL(store.DefaultUnlockTTL)
}
