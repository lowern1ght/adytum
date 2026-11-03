package staleness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner подменяет git-вызовы.
type fakeRunner struct {
	// responses — ответы по подстроке в аргументах.
	responses map[string]fakeResponse
	// calls — список выполненных команд.
	calls [][]string
}

// fakeResponse — ответ на команду.
type fakeResponse struct {
	out string
	err error
}

// Run выполняет команду и возвращает заранее заданный ответ.
func (f *fakeRunner) Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	joined := strings.Join(args, " ")
	for key, response := range f.responses {
		if strings.Contains(joined, key) {
			return []byte(response.out), response.err
		}
	}
	return nil, errors.New("незапланированный вызов: " + joined)
}

// newChecker собирает проверку с подменённым временем и кэшем.
func newChecker(t *testing.T, runner Runner, now time.Time) *Checker {
	t.Helper()
	return &Checker{
		Runner:   runner,
		Root:     "/repo",
		CacheDir: t.TempDir(),
		Now:      func() time.Time { return now },
		Timeout:  time.Second,
	}
}

func TestCheckReportsFreshRepository(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "abc123\n"},
		"ls-remote": {out: "abc123\trefs/heads/master\n"},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if result.Err != nil {
		t.Fatalf("Check: %v", result.Err)
	}
	if result.Stale() {
		t.Errorf("репозиторий помечен устаревшим: %+v", result)
	}
	if result.Behind != 0 {
		t.Errorf("Behind = %d, ожидалось 0", result.Behind)
	}
}

func TestCheckReportsStaleRepository(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {out: "3\n"},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if result.Err != nil {
		t.Fatalf("Check: %v", result.Err)
	}
	if !result.Stale() {
		t.Fatalf("репозиторий не помечен устаревшим: %+v", result)
	}
	if result.Behind != 3 {
		t.Errorf("Behind = %d, ожидалось 3", result.Behind)
	}
	if result.RemoteHEAD != "remote1" {
		t.Errorf("RemoteHEAD = %q, ожидалось remote1", result.RemoteHEAD)
	}
}

// TestCheckCountsBehindWithoutRemoteObjects проверяет подсчёт по
// локальному слежению за веткой.
//
// Объекты удалённого коммита обычно отсутствуют локально: rev-list по
// прямому диапазону падает, и разрыв считается по origin/master.
func TestCheckCountsBehindWithoutRemoteObjects(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {out: "2\n"},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if !result.Stale() {
		t.Fatalf("репозиторий не помечен устаревшим: %+v", result)
	}
	if result.Behind != 2 {
		t.Errorf("Behind = %d, ожидалось 2", result.Behind)
	}
}

// TestCheckAssumesOneCommitWhenCountUnavailable проверяет оценку «хотя
// бы один коммит», когда посчитать нечем.
//
// Молча показывать «устарело на 0» нельзя: агент решит, что данные
// свежие, и не запустит sync.
func TestCheckAssumesOneCommitWhenCountUnavailable(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {err: errors.New("unknown revision")},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if !result.Stale() {
		t.Fatalf("репозиторий не помечен устаревшим: %+v", result)
	}
	if result.Behind < 1 {
		t.Errorf("Behind = %d, ожидалось не меньше 1", result.Behind)
	}
}

func TestCheckIgnoresNetworkFailure(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {err: errors.New("network unreachable")},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if result.Err == nil {
		t.Fatal("ожидалась ошибка сетевого запроса")
	}
	if result.Stale() {
		t.Errorf("сетевая ошибка не должна означать устаревание: %+v", result)
	}
	if result.Behind != 0 {
		t.Errorf("Behind = %d, ожидалось 0", result.Behind)
	}
}

func TestCheckIgnoresMissingRemote(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: ""},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if result.Err == nil {
		t.Fatal("ожидалась ошибка отсутствия remote")
	}
	if result.Stale() {
		t.Errorf("отсутствие remote не должно означать устаревание: %+v", result)
	}
}

// TestCheckQueriesMasterBranch проверяет, что запрашивается именно
// ветка master.
//
// Префикс «-» в шаблоне ls-remote означает исключение: с ним ветка
// не возвращается вовсе, и проверка молча ничего не находит.
func TestCheckQueriesMasterBranch(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "abc123\n"},
		"ls-remote": {out: "abc123\trefs/heads/master\n"},
	}}
	checker := newChecker(t, runner, time.Now())

	checker.Check()

	found := false
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "ls-remote") {
			found = true
			if !strings.Contains(joined, "refs/heads/master") {
				t.Errorf("запрос ветки = %q, ожидался refs/heads/master", joined)
			}
			if strings.Contains(joined, "-refs/heads/master") {
				t.Errorf("запрос ветки = %q: префикс «-» исключает ветку", joined)
			}
		}
	}
	if !found {
		t.Fatal("ls-remote не вызывался")
	}
}

func TestCheckUsesCacheWithinTTL(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {out: "2\n"},
	}}
	start := time.Now()
	checker := newChecker(t, runner, start)

	first := checker.Check()
	if !first.Stale() {
		t.Fatalf("ожидался устаревший репозиторий: %+v", first)
	}
	callsAfterFirst := len(runner.calls)

	// Второй вызов в пределах TTL не должен ходить в сеть.
	second := checker.Check()
	if second.Behind != first.Behind {
		t.Errorf("Behind = %d, ожидалось %d из кэша", second.Behind, first.Behind)
	}
	if len(runner.calls) != callsAfterFirst {
		t.Errorf("кэш не сработал: вызовов %d, ожидалось %d", len(runner.calls), callsAfterFirst)
	}
}

func TestCheckRefreshesCacheAfterTTL(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {out: "5\n"},
	}}
	start := time.Now()
	checker := newChecker(t, runner, start)
	checker.Check()
	callsAfterFirst := len(runner.calls)

	// Сдвигаем время за пределы TTL: кэш должен протухнуть.
	checker.Now = func() time.Time { return start.Add(TTL + time.Minute) }
	checker.Check()
	if len(runner.calls) == callsAfterFirst {
		t.Error("после истечения TTL проверка не должна брать результат из кэша")
	}
}

func TestCheckWritesCacheFile(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {out: "remote1\trefs/heads/master\n"},
		"rev-list":  {out: "1\n"},
	}}
	checker := newChecker(t, runner, time.Now())
	checker.Check()

	entries, err := os.ReadDir(checker.CacheDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("файлов кэша = %d, ожидался 1", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "staleness-") {
		t.Errorf("имя файла кэша = %q", entries[0].Name())
	}
}

// TestCheckCachesFailure проверяет, что неудачная проверка тоже
// кэшируется.
//
// Иначе каждая команда в офлайн-машине ждала бы сетевого таймаута.
func TestCheckCachesFailure(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {err: errors.New("network unreachable")},
	}}
	checker := newChecker(t, runner, time.Now())

	checker.Check()
	callsAfterFirst := len(runner.calls)

	checker.Check()
	if len(runner.calls) != callsAfterFirst {
		t.Errorf("неудачная проверка не закэширована: вызовов %d, ожидалось %d", len(runner.calls), callsAfterFirst)
	}
}

// TestCheckRestoresFailureFromCache проверяет, что ошибка переживает
// кэш.
//
// Тип error не переживает JSON: ошибка хранится строкой.
func TestCheckRestoresFailureFromCache(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {out: "local1\n"},
		"ls-remote": {err: errors.New("network unreachable")},
	}}
	checker := newChecker(t, runner, time.Now())

	first := checker.Check()
	if first.Err == nil {
		t.Fatal("ожидалась ошибка сетевого запроса")
	}

	second := checker.Check()
	if second.Err == nil {
		t.Fatal("ошибка не восстановлена из кэша")
	}
	if second.Err.Error() != first.Err.Error() {
		t.Errorf("ошибка из кэша = %q, ожидалось %q", second.Err.Error(), first.Err.Error())
	}
}

func TestParseLsRemotePicksMasterBranch(t *testing.T) {
	output := "aaa111\trefs/heads/feature\nbbb222\trefs/heads/master\n"
	if got := parseLsRemote(output); got != "bbb222" {
		t.Errorf("parseLsRemote = %q, ожидалось bbb222", got)
	}
}

func TestParseLsRemoteReturnsEmptyWhenBranchMissing(t *testing.T) {
	output := "aaa111\trefs/heads/feature\n"
	if got := parseLsRemote(output); got != "" {
		t.Errorf("parseLsRemote = %q, ожидалась пустая строка", got)
	}
}

func TestCheckHandlesMissingLocalHead(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"rev-parse": {err: errors.New("no commits")},
	}}
	checker := newChecker(t, runner, time.Now())

	result := checker.Check()
	if result.Err == nil {
		t.Fatal("ожидалась ошибка отсутствия HEAD")
	}
	if result.Stale() {
		t.Errorf("ошибка не должна означать устаревание: %+v", result)
	}
}

func TestCachePathIsPerRepository(t *testing.T) {
	first := &Checker{Root: "/repo-a", CacheDir: filepath.Join(t.TempDir(), "cache")}
	second := &Checker{Root: "/repo-b", CacheDir: filepath.Join(t.TempDir(), "cache")}
	if first.cachePath() == second.cachePath() {
		t.Error("разные репозитории должны давать разные файлы кэша")
	}
	if filepath.Dir(first.cachePath()) != first.CacheDir {
		t.Errorf("файл кэша вне каталога кэша: %s", first.cachePath())
	}
}
