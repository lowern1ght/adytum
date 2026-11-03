package adytum

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/staleness"
	"github.com/lowern1ght/adytum/internal/store"
)

// stalenessChecker собирает проверку свежести для текущего репозитория.
//
// Кэш проверки живёт в ~/.adytum/cache: сетевой запрос на каждый вызов
// CLI не нужен, а десять минут устаревания для агента незаметны.
func (a *App) stalenessChecker() *staleness.Checker {
	cacheDir := ""
	if home, err := store.Home(); err == nil {
		cacheDir = filepath.Join(home, "cache")
	}
	return &staleness.Checker{
		Runner:   &gitRunner{runner: a.Runner},
		Root:     a.Root,
		CacheDir: cacheDir,
		Now:      a.Now,
		Timeout:  staleness.Timeout,
	}
}

// gitRunner адаптирует Runner к интерфейсу staleness.Runner.
type gitRunner struct {
	runner Runner
}

// Run выполняет git-команду с таймаутом.
//
// Таймаут реализован через контекст: команда, не успевшая за Deadline,
// прерывается вместе с процессом.
func (g *gitRunner) Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	if contextRunner, ok := g.runner.(ContextRunner); ok {
		out, err := contextRunner.RunContext(ctx, Command{Dir: dir}, name, args...)
		if err != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("%s %s: превышено время ожидания (%s)", name, strings.Join(args, " "), ctx.Err())
		}
		return out, err
	}
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := g.runner.Run(Command{Dir: dir}, name, args...)
		done <- result{out: out, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s %s: превышено время ожидания (%s)", name, strings.Join(args, " "), ctx.Err())
	case res := <-done:
		return res.out, res.err
	}
}

// ensureCacheDir создаёт каталог кэша проверки свежести.
//
// Каталог может не существовать на свежей машине: проверка молча
// откажется работать, но кэш писать не сможет.
func ensureCacheDir() error {
	home, err := store.Home()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(home, "cache"), 0o700)
}
