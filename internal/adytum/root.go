package adytum

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/ui"
)

// RepoEnv — переменная окружения с путём к хранилищу.
//
// Нужна машинам без локального клона хранилища и тем, кто запускает
// adytum не из каталога хранилища.
const RepoEnv = "ADYTUM_REPO"

// StoreMarker — файл, по которому каталог распознаётся как хранилище.
//
// Реестр лежит в корне хранилища: по нему каталог отличается от
// обычного репозитория с исходниками.
const StoreMarker = "registry.yaml"

// RequireRoot определяет корень хранилища и записывает его в App.
//
// Порядок поиска: явный путь (--repo), переменная окружения
// ADYTUM_REPO, затем поиск вверх от текущего каталога по маркеру
// хранилища.
func (a *App) RequireRoot(explicit string) error {
	candidates := []string{explicit}
	if env := strings.TrimSpace(os.Getenv(RepoEnv)); env != "" {
		candidates = append(candidates, env)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		root, err := filepath.Abs(candidate)
		if err != nil {
			return fmt.Errorf("неверный путь к хранилищу %s: %w", candidate, err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("хранилище не найдено: %s", candidate)
		}
		if !info.IsDir() {
			return fmt.Errorf("хранилище не найдено: %s не каталог", candidate)
		}
		a.Root = root
		return nil
	}

	if cwd, err := os.Getwd(); err == nil {
		if root, found := searchUpward(cwd); found {
			a.Root = root
			return nil
		}
	}
	// Запасной вариант: корень git-репозитория. Он нужен хранилищу,
	// в котором реестр ещё не создан. Репозиторий без признаков
	// хранилища не подходит: иначе командой, запущенной из исходников
	// инструмента, управляли бы чужим каталогом.
	if root, err := RepoRoot(a.Runner, "."); err == nil && looksLikeStore(root) {
		a.Root = root
		return nil
	}
	return advise(
		fmt.Errorf("хранилище секретов не найдено"),
		ui.KindArrow,
		"укажите каталог: --repo <путь> или "+RepoEnv+"=<путь>",
	)
}

// looksLikeStore сообщает, похож ли каталог на хранилище секретов.
//
// Признак один: правила шифрования в .gitattributes. Хранилище без
// них не хранит секретов, а обычный репозиторий с исходниками
// инструмента правил шифрования не содержит.
func looksLikeStore(root string) bool {
	content, err := os.ReadFile(GitattributesPath(root))
	if err != nil {
		return false
	}
	return strings.Contains(string(content), "filter="+FilterName)
}

// searchUpward ищет каталог хранилища, поднимаясь от start к корню.
//
// Поиск идёт по маркеру хранилища, а не по корню git-репозитория:
// каталог хранилища может лежать внутри другого репозитория.
func searchUpward(start string) (string, bool) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(current, StoreMarker)); err == nil {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}
