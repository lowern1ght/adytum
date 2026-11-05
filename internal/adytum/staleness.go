package adytum

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/staleness"
	"github.com/lowern1ght/adytum/internal/store"
)

// stalenessChecker собирает проверку свежести для текущего репозитория.
//
// Кэш проверки живёт в ~/.adytum/cache: сетевой запрос на каждый вызов
// CLI не нужен, а десять минут устаревания для агента незаметны.
//
// Сетевой запрос идёт с учётными данными remote: на реплике, которая
// только читает, проверка без них всегда падала бы.
func (a *App) stalenessChecker() *staleness.Checker {
	cacheDir := ""
	if home, err := store.Home(); err == nil {
		cacheDir = filepath.Join(home, "cache")
	}
	env, options, token := a.remoteAuthEnv()
	return &staleness.Checker{
		Runner:   &gitRunner{runner: a.Runner},
		Root:     a.Root,
		CacheDir: cacheDir,
		Now:      a.Now,
		Timeout:  staleness.Timeout,
		AuthEnv:  env,
		AuthArgs: options,
		Token:    token,
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
func (g *gitRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	command := Command{Dir: dir, Env: env}
	if contextRunner, ok := g.runner.(ContextRunner); ok {
		out, err := contextRunner.RunContext(ctx, command, name, args...)
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
		out, err := g.runner.Run(command, name, args...)
		done <- result{out: out, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s %s: превышено время ожидания (%s)", name, strings.Join(args, " "), ctx.Err())
	case res := <-done:
		return res.out, res.err
	}
}
