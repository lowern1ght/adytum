// Package staleness проверяет, отстаёт ли локальная копия секретов от
// remote.
//
// Проверка дешёвая: один git ls-remote с таймаутом. Результат
// кэшируется на 10 минут, чтобы сетевой запрос не выполнялся на каждый
// вызов CLI.
package staleness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TTL — срок жизни кэша проверки.
const TTL = 10 * time.Minute

// Timeout — предел ожидания сетевого ответа.
const Timeout = 3 * time.Second

// DefaultBranch — ветка, которую сравниваем с remote.
const DefaultBranch = "master"

// Result — результат проверки свежести.
type Result struct {
	// Behind — на сколько коммитов remote опережает локальный HEAD.
	// Отрицательное значение означает, что remote отстаёт.
	Behind int
	// LocalHEAD — локальный коммит HEAD.
	LocalHEAD string
	// RemoteHEAD — коммит на remote.
	RemoteHEAD string
	// Err — причина, по которой проверка не удалась. Непустая ошибка
	// означает «не знаем», а не «устарело».
	Err error
}

// Stale сообщает, отстали ли секреты.
func (r Result) Stale() bool {
	return r.Err == nil && r.Behind > 0
}

// Checker проверяет свежесть секретов.
type Checker struct {
	// Runner выполняет git-команды.
	Runner Runner
	// Root — корень репозитория.
	Root string
	// CacheDir — каталог для кэша проверки.
	CacheDir string
	// Now — текущее время (подменяется в тестах).
	Now func() time.Time
	// Timeout — предел ожидания сетевого ответа.
	Timeout time.Duration
	// AuthEnv — переменные окружения для сетевого запроса.
	//
	// Токен передаётся через окружение: аргументы команды видит любой
	// пользователь машины через список процессов.
	AuthEnv []string
	// AuthArgs — дополнительные аргументы git для сетевого запроса.
	AuthArgs []string
	// Token — токен, который не должен попасть в текст ошибки.
	Token string
}

// Runner выполняет git-команду с ограничением по времени.
//
// env — переменные окружения команды: пустой список означает
// окружение вызывающего.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}

// Check возвращает состояние локальной копии относительно remote.
//
// Ошибка сети или отсутствие remote не считаются расхождением: в этом
// случае Err заполнен, а Behind равен нулю.
func (c *Checker) Check() Result {
	if cached, ok := c.readCache(); ok {
		return cached
	}
	result := c.checkRemote()
	c.writeCache(result)
	return result
}

// checkRemote выполняет сетевую проверку.
//
// Запрос идёт с теми же учётными данными, что и остальные операции с
// remote: без них приватный репозиторий недоступен, и проверка
// всегда падала бы на реплике, которая только читает.
func (c *Checker) checkRemote() Result {
	local, err := c.localHead()
	if err != nil {
		return Result{Err: err}
	}
	result := Result{LocalHEAD: local}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := append(append([]string{}, c.AuthArgs...), "ls-remote", "origin", "refs/heads/"+DefaultBranch)
	out, err := c.Runner.Run(ctx, c.Root, c.AuthEnv, "git", args...)
	if err != nil {
		result.Err = redactToken(err, c.Token)
		return result
	}
	remote := parseLsRemote(string(out))
	if remote == "" {
		result.Err = fmt.Errorf("remote не вернул ветку %s", DefaultBranch)
		return result
	}
	result.RemoteHEAD = remote
	if remote == local {
		return result
	}
	result.Behind = c.behindCount(local, remote)
	return result
}

// localHead возвращает коммит локального HEAD.
func (c *Checker) localHead() (string, error) {
	out, err := c.Runner.Run(context.Background(), c.Root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// behindCount считает, на сколько коммитов remote опережает локальный HEAD.
//
// Точное число требует наличия удалённых объектов локально: если их
// нет, считается разрыв до последней известной ветки origin. Если и она
// недоступна, известно лишь, что ветки расходятся, — минимум один
// коммит.
func (c *Checker) behindCount(local, remote string) int {
	if count, ok := c.revListCount(local + ".." + remote); ok {
		// Удалённый коммит разрешается локально: это точное число
		// коммитов, которых нет в локальной копии.
		return count
	}
	if count, ok := c.revListCount(local + "..refs/remotes/origin/" + DefaultBranch); ok && count > 0 {
		// Объекты remote локально отсутствуют: считаем по локальному
		// слежению за веткой — это нижняя граница расхождения.
		return count
	}
	return 1
}

// revListCount возвращает число коммитов в диапазоне.
//
// Диапазон разбирается как есть: он передаётся git'у без кавычек.
func (c *Checker) revListCount(rangeSpec string) (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	out, err := c.Runner.Run(ctx, c.Root, nil, "git", "rev-list", "--count", rangeSpec)
	if err != nil {
		return 0, false
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count); err != nil {
		return 0, false
	}
	return count, true
}

// redactToken убирает токен из текста ошибки.
//
// Git печатает URL с учётными данными в сообщениях об отказе по
// авторизации: без очистки токен попал бы в лог агентской сессии.
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), token, "***")
	return fmt.Errorf("%s", message)
}

// parseLsRemote разбирает вывод git ls-remote.
//
// Формат строки: <sha>\t<ref>.
func parseLsRemote(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if !strings.HasSuffix(fields[1], "/"+DefaultBranch) {
			continue
		}
		return fields[0]
	}
	return ""
}

// cachePath возвращает путь к файлу кэша.
func (c *Checker) cachePath() string {
	if c.CacheDir == "" {
		return ""
	}
	name := fmt.Sprintf("staleness-%x.json", hashRoot(c.Root))
	return filepath.Join(c.CacheDir, name)
}

// cacheEntry — запись кэша.
//
// Ошибка хранится строкой: тип error не переживает JSON, а проверять
// кэш нужно и для неуспешных попыток.
type cacheEntry struct {
	// CheckedAt — время проверки.
	CheckedAt time.Time `json:"checked_at"`
	// Behind — число отстающих коммитов.
	Behind int `json:"behind"`
	// LocalHEAD — локальный коммит HEAD.
	LocalHEAD string `json:"local_head"`
	// RemoteHEAD — коммит на remote.
	RemoteHEAD string `json:"remote_head"`
	// Error — текст ошибки проверки, если она была.
	Error string `json:"error,omitempty"`
}

// readCache возвращает кэшированный результат, если он свежий.
func (c *Checker) readCache() (Result, bool) {
	path := c.cachePath()
	if path == "" {
		return Result{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Result{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return Result{}, false
	}
	now := c.now()
	if now.Sub(entry.CheckedAt) > TTL {
		return Result{}, false
	}
	result := Result{
		Behind:     entry.Behind,
		LocalHEAD:  entry.LocalHEAD,
		RemoteHEAD: entry.RemoteHEAD,
	}
	if entry.Error != "" {
		result.Err = fmt.Errorf("%s", entry.Error)
	}
	return result, true
}

// writeCache сохраняет результат проверки.
func (c *Checker) writeCache(result Result) {
	path := c.cachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	entry := cacheEntry{
		CheckedAt:  c.now(),
		Behind:     result.Behind,
		LocalHEAD:  result.LocalHEAD,
		RemoteHEAD: result.RemoteHEAD,
	}
	if result.Err != nil {
		entry.Error = result.Err.Error()
	}
	content, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, content, 0o600)
}

// now возвращает текущее время.
func (c *Checker) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// hashRoot даёт короткий стабильный хэш пути репозитория.
//
// Используется для имени файла кэша: у каждого репозитория свой кэш.
func hashRoot(root string) uint64 {
	var hash uint64 = 14695981039346656037
	for index := 0; index < len(root); index++ {
		hash ^= uint64(root[index])
		hash *= 1099511628211
	}
	return hash
}
