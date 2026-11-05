package adytum

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lowern1ght/adytum/internal/filters"
	"github.com/lowern1ght/adytum/internal/machine"
	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/staleness"
	"github.com/lowern1ght/adytum/internal/store"
	"github.com/lowern1ght/adytum/internal/ui"
)

// App — состояние CLI: исполнитель команд и корень репозитория.
type App struct {
	// Runner выполняет внешние команды.
	Runner Runner
	// Root — корень репозитория (определяется один раз при старте).
	Root string
	// Now — текущее время (подменяется в тестах).
	Now func() time.Time
	// Out — поток основного вывода.
	Out io.Writer
	// Err — поток подсказок и предупреждений.
	Err io.Writer
	// Pretty — принудительный цвет (флаг --pretty).
	Pretty bool
	// JSON — машинночитаемый вывод (флаг --json).
	JSON bool
	// Role — переопределение роли машины (флаг --role).
	//
	// Переопределение нужно там, где роль выводится из окружения и
	// может быть определена неверно: doctor на машине без systemd.
	Role string
}

// New создаёт App без корня репозитория.
//
// Корень определяется отдельно: не всем командам он нужен, а искать
// его впустую — значит падать там, где хранилище не требуется.
func New(runner Runner) *App {
	return &App{
		Runner: runner,
		Now:    time.Now,
		Out:    os.Stdout,
		Err:    os.Stderr,
	}
}

// ui возвращает принтер основного вывода.
//
// Цвет включается только на терминале и только если не задан NO_COLOR;
// флаг --pretty включает его принудительно.
func (a *App) ui() *ui.Printer {
	return ui.New(a.out(), a.Pretty, a.JSON)
}

// uiErr возвращает принтер потока подсказок.
func (a *App) uiErr() *ui.Printer {
	return ui.New(a.errOut(), a.Pretty, a.JSON)
}

// out возвращает поток основного вывода.
func (a *App) out() io.Writer {
	if a.Out == nil {
		return os.Stdout
	}
	return a.Out
}

// errOut возвращает поток подсказок.
func (a *App) errOut() io.Writer {
	if a.Err == nil {
		return os.Stderr
	}
	return a.Err
}

// print печатает в основной поток вывода.
func (a *App) print(args ...any) {
	fmt.Fprint(a.out(), args...)
}

// printf печатает в основной поток вывода.
func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.out(), format, args...)
}

// println печатает строку в основной поток вывода.
func (a *App) println(args ...any) {
	fmt.Fprintln(a.out(), args...)
}

// hint печатает подсказку следующего шага в поток подсказок.
//
// Подсказка всегда идёт в stderr: stdout команд get и run содержит
// полезную нагрузку (значение секрета, вывод программы), и примесь
// текста там ломает разбор.
func (a *App) hint(format string, args ...any) {
	a.uiErr().Hint(format, args...)
}

// Init готовит уже склонированный репозиторий к работе.
//
// Проверяет наличие sops и git, ставит git-фильтры, создаёт .gitattributes
// и .sops.yaml, если их ещё нет.
//
// Фильтры ставятся скриптами-обёртками в ~/.adytum/bin: напрямую
// вызванный sops падает на пустом файле, а на машине без доступа
// к разделу — съедает stdin и оставляет после себя пустой файл.
// Обёртки эти две ситуации переживают.
func (a *App) Init() error {
	if err := Require("sops"); err != nil {
		return err
	}
	if err := Require("git"); err != nil {
		return err
	}
	keyFile, err := store.IdentityFile()
	if err != nil {
		// На свежей машине метка ещё не задана: если ключ ровно один,
		// он и есть идентичность этой машины.
		if err := a.adoptSoleIdentity(); err != nil {
			return err
		}
		keyFile, err = store.IdentityFile()
		if err != nil {
			return fmt.Errorf("не найдена идентичность машины: %w", err)
		}
	}
	if _, err := os.Stat(keyFile); err != nil {
		return fmt.Errorf("файл идентичности %s не найден: выполните `adytum grant <label>`", keyFile)
	}

	createdAttrs, err := ensureGitattributes(a.Root)
	if err != nil {
		return err
	}
	createdSops, err := ensureSopsConfig(a.Root)
	if err != nil {
		return err
	}
	if err := a.fillPlaceholders(); err != nil {
		return err
	}
	if err := a.warnUnresolvedSlots(); err != nil {
		return err
	}
	scripts, err := a.installFilters(keyFile)
	if err != nil {
		return err
	}
	for key, value := range gitConfigValues(scripts) {
		if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "config", key, value); err != nil {
			return fmt.Errorf("не удалось задать git config %s: %w", key, err)
		}
	}

	a.printf("репозиторий: %s\n", a.Root)
	if createdAttrs {
		a.println("создан .gitattributes")
	}
	if createdSops {
		a.println("создан .sops.yaml")
	}
	a.printf("скрипты фильтров: %s\n", scripts.Smudge)
	a.println("git-фильтры adytum установлены")
	return nil
}

// installFilters записывает скрипты-обёртки git-фильтров.
//
// Скрипты живут в ~/.adytum/bin: они переживают переустановку CLI и
// одинаково работают в Linux и в Git-for-Windows.
//
// Разделы machine_scope попадают в smunge-фильтр: файлы вне списка
// остаются в рабочем дереве шифротекстом.
func (a *App) installFilters(keyFile string) (filters.Scripts, error) {
	binDir, err := store.BinDir()
	if err != nil {
		return filters.Scripts{}, err
	}
	config, err := machine.Load()
	if err != nil {
		return filters.Scripts{}, err
	}
	scripts, err := filters.Install(binDir, keyFile, config.MachineScope)
	if err != nil {
		return filters.Scripts{}, err
	}
	return scripts, nil
}

// Sync синхронизирует репозиторий с remote.
//
// Сначала подтягивает изменения, затем обновляет рабочее дерево, чтобы
// smunge-фильтр применился к изменённым файлам, и только потом отправляет
// локальные коммиты.
//
// В режиме pullOnly изменения только забираются: таймер автосинхронизации
// не должен публиковать локальные правки без участия человека.
func (a *App) Sync(pullOnly bool) error {
	remote, _ := a.gitOutput("remote", "get-url", "origin")
	if remote == "" {
		a.println("remote не настроен: синхронизация только с локальным репозиторием")
		a.hint("%s", AdviceRemote)
	} else if pullOnly {
		if err := a.fetchReset(); err != nil {
			return fmt.Errorf("не удалось получить изменения: %w", err)
		}
	} else if err := a.pull(); err != nil {
		a.hint("pull не завершён: разберитесь с конфликтами вручную и повторите: git status")
		return fmt.Errorf("не удалось получить изменения: %w", err)
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "checkout", "--", "."); err != nil {
		return fmt.Errorf("не удалось обновить рабочее дерево: %w", err)
	}
	if pullOnly {
		a.println("получено (pull-only: отправка изменений пропущена)")
		return nil
	}
	// push выполняем без --force: отправляем только то, что уже закоммичено.
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "push"); err != nil {
		if remote == "" {
			a.println("remote не настроен: изменения сохранены только локально")
			return nil
		}
		return fmt.Errorf("не удалось отправить изменения: %w", err)
	}
	a.println("репозиторий синхронизирован")
	a.hint("проверьте состояние: adytum status")
	return nil
}

// fetchReset обновляет ветку до состояния origin без слияния.
//
// Рабочее дерево реплики всегда «грязное»: smunge-фильтр кладёт
// шифротекст вместо расшифрованного файла, и git считает файл
// изменённым. Обычный pull на таком дереве абортится («would be
// overwritten»), поэтому реплика забирает изменения принудительно:
// fetch + reset --hard.
//
// reset безопасен: у реплики нет своих правок — она только читает.
func (a *App) fetchReset() error {
	command, args, token := a.remoteAuth("fetch", "origin")
	if _, err := a.Runner.Run(command, "git", args...); err != nil {
		return redactToken(err, token)
	}
	branch, err := a.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" {
		return fmt.Errorf("не удалось определить текущую ветку")
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "reset", "--hard", "origin/"+branch); err != nil {
		return fmt.Errorf("не удалось обновить ветку %s: %w", branch, err)
	}
	return nil
}

// pull забирает изменения с remote.
//
// Для приватного remote нужен доступ. Он берётся из ~/.git-credentials,
// если файл есть; иначе токен из локального хранилища передаётся
// помощнику учётных данных через переменную окружения: в аргументах
// git токена нет, а значит, он не виден в списке процессов.
//
// smunge-фильтр при этом не должен ломать pull на машине без полного
// доступа: недоступные разделы остаются шифротекстом, а операция
// проходит.
func (a *App) pull() error {
	if a.hasGitCredentials() {
		if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "pull", "--rebase"); err != nil {
			return err
		}
		return nil
	}
	command, args, token := a.remoteAuth("pull", "--rebase")
	if _, err := a.Runner.Run(command, "git", args...); err != nil {
		return redactToken(err, token)
	}
	return nil
}

// hasGitCredentials сообщает, есть ли файл учётных данных git.
func (a *App) hasGitCredentials() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".git-credentials"))
	return err == nil && !info.IsDir() && info.Size() > 0
}

// githubToken достаёт токен GitHub из локального хранилища.
//
// Значение токена никогда не печатается.
func (a *App) githubToken() (string, error) {
	store, err := a.tokenStore()
	if err != nil {
		return "", err
	}
	return store.Token()
}

// sopsFor собирает обёртку sops с путём к приватному ключу машины.
func (a *App) sopsFor() (Sops, error) {
	keyFile, err := store.IdentityFile()
	if err != nil {
		return Sops{}, err
	}
	return Sops{Runner: a.Runner, Root: a.Root, KeyFile: keyFile}, nil
}

// looksEncrypted сообщает, содержит ли файл метаданные sops.
//
// Признаки ищутся в начале строки: иначе значение вида
// "url=https://sops:example" приняли бы за метаданные. Файлы в рабочем
// дереве после checkout уже расшифрованы smunge-фильтром, но файл может
// остаться шифротекстом (например, если фильтры не настроены).
func looksEncrypted(content []byte) bool {
	lines := bytes.Split(content, []byte("\n"))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		for _, marker := range [][]byte{
			[]byte("sops_version="),
			[]byte("sops_lastmodified="),
			[]byte("sops_mac="),
			[]byte("sops_unencrypted_suffix="),
			[]byte("\"sops\":"),
			[]byte("sops:"),
		} {
			if bytes.HasPrefix(trimmed, marker) {
				return true
			}
		}
	}
	return false
}

// readPlaintext читает файл и возвращает его открытое содержимое.
//
// Если файл зашифрован, расшифровка выполняется в памяти: plaintext
// никогда не попадает во временные файлы.
//
// Ошибка расшифровки разбирается по существу: «машины нет в правиле»
// — ожидаемое состояние, «машина в правиле, но расшифровка не удалась»
// — порча файла или перешифровка без этой машины.
func (a *App) readPlaintext(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !looksEncrypted(content) {
		return content, nil
	}
	sops, err := a.sopsFor()
	if err != nil {
		return nil, err
	}
	plain, err := sops.DecryptFile(path)
	if err != nil {
		if isNotRecipient(err.Error()) {
			if a.recipientStateOf(path) == recipientPresent {
				return nil, corruptedSecretError(err)
			}
			return nil, notRecipientError(err)
		}
		return nil, err
	}
	return plain, nil
}

// writePlaintext записывает открытое содержимое в рабочее дерево.
//
// Файл остаётся plaintext: шифрование при коммите делает clean-фильтр
// git. Права 0600 — plaintext не должен быть доступен другим пользователям.
func (a *App) writePlaintext(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

// stageFile добавляет файл в индекс.
//
// Clean-фильтр git при этом шифрует содержимое: в индекс попадает
// шифротекст, в рабочем дереве остаётся plaintext.
func (a *App) stageFile(path string) error {
	relative := relativeTo(a.Root, path)
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "add", relative); err != nil {
		return fmt.Errorf("не удалось добавить %s в индекс: %w", relative, err)
	}
	return nil
}

// requireUnlock проверяет маркер разблокировки.
//
// Без маркера операции get и run закрыты: их вывод может попасть в лог
// агентской сессии.
func (a *App) requireUnlock() error {
	state, err := store.Unlock(a.Now(), a.unlockTTL())
	if err != nil {
		return err
	}
	if state.Unlocked {
		return nil
	}
	return unlockError(a.role())
}

// staleHint предупреждает, что локальная копия секретов отстала.
//
// Проверка молчит при любой неудаче: недоступность remote не должна
// мешать работе, иначе подсказка превратится в новый источник ошибок.
//
// Поток вывода выбирает вызывающий: stdout команд get и run несёт
// полезную нагрузку (значение секрета, вывод программы), и примесь
// текста там ломает разбор.
func (a *App) staleHint(out io.Writer) {
	result := a.stalenessChecker().Check()
	if !result.Stale() {
		return
	}
	printer := ui.New(out, a.Pretty, false)
	printer.Warn("secrets устарели: origin/%s впереди на %d → adytum sync",
		staleness.DefaultBranch, result.Behind)
}

// Edit открывает секрет в редакторе.
//
// Файл расшифровывается, открывается в $EDITOR, результат записывается
// обратно в рабочее дерево и добавляется в индекс: шифротекст в
// репозитории обеспечит clean-фильтр git.
//
// Редактор — для неструктурированных секретов: json, kubeconfig,
// бинарные файлы. Обычные ключи удобнее задавать через `adytum set`:
// значения не попадают в историю оболочки.
func (a *App) Edit(project, service string) error {
	path, _, err := ResolveSecret(a.Root, project, service)
	if err != nil {
		return err
	}
	plain, err := a.readPlaintext(path)
	if err != nil {
		return err
	}
	edited, err := editInEditor(plain, filepath.Ext(path))
	if err != nil {
		return err
	}
	if edited == string(plain) {
		a.println("изменений нет")
		return nil
	}
	if err := a.requireEncryptedPath(relativeTo(a.Root, path)); err != nil {
		return err
	}

	if err := a.writePlaintext(path, []byte(edited)); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}
	a.printf("обновлён %s\n", relativeTo(a.Root, path))
	return nil
}

// Unlock создаёт маркер разблокировки для агентских сессий.
//
// Маркер живёт столько, сколько задано профилем машины (по умолчанию
// сутки) и разрешает плагину выполнять операции get и run.
func (a *App) Unlock() error {
	ttl := a.unlockTTL()
	expires, err := store.WriteUnlock(a.Now(), ttl)
	if err != nil {
		return err
	}
	a.printf("разблокировка активна до %s\n", expires.Format("2006-01-02T15:04:05Z07:00"))
	a.hint("теперь доступны adytum get и adytum run")
	return nil
}

// Grant добавляет нового получателя.
//
// Генерируется новая age-идентичность, приватный ключ кладётся в
// ~/.adytum/keys/<label>.txt, публичный ключ попадает в реестр, а
// метка — в scope, определяющий видимые разделы. Затем .sops.yaml
// пересобирается из реестра и файлы перешифровываются под новый набор
// получателей.
//
// Реестр — единственный источник правды: без записи в нём выдача
// доступа была бы стёрта ближайшим `adytum reencrypt`.
func (a *App) Grant(label string, scopeName string) error {
	if label == "" {
		return fmt.Errorf("%w: укажите метку: adytum grant <label>", ErrUsage)
	}
	scope, err := a.resolveGrantScope(scopeName)
	if err != nil {
		return err
	}
	// Проверка идёт до генерации ключа: сломанный реестр должен
	// остановить команду, а не оставить после себя половину работы.
	// scope исключён: выдача доступа в пустой scope как раз и чинит
	// его.
	if err := a.requireConsistentRegistry(scope); err != nil {
		return err
	}

	if err := Require("age-keygen"); err != nil {
		return err
	}
	if _, err := store.EnsureHome(); err != nil {
		return err
	}
	keyFile, err := store.KeyFile(label)
	if err != nil {
		return err
	}
	if _, err := os.Stat(keyFile); err == nil {
		return fmt.Errorf("идентичность %s уже существует: %s", label, keyFile)
	}
	if _, err := a.Runner.Run(Command{}, "age-keygen", "-o", keyFile); err != nil {
		return fmt.Errorf("не удалось сгенерировать age-ключ: %w", err)
	}
	if err := os.Chmod(keyFile, 0o600); err != nil {
		return err
	}
	public, err := store.PublicKeyFromIdentity(keyFile)
	if err != nil {
		return err
	}

	recipients, err := store.LoadRecipients()
	if err != nil {
		return err
	}
	recipients[label] = public
	if err := store.SaveRecipients(recipients); err != nil {
		return err
	}

	if err := a.updateRegistry(func(document *registry.Document) error {
		if err := document.SetRecipient(label, public); err != nil {
			return err
		}
		return document.AddToScope(scope, label)
	}); err != nil {
		return err
	}
	if _, err := a.reencryptAll(); err != nil {
		return err
	}

	if err := a.commitAndPush(fmt.Sprintf("[.stbl](feat/adytum): grant access to %s", label)); err != nil {
		return err
	}
	a.printf("метка %s создана\n", label)
	a.printf("scope: %s\n", scope)
	a.printf("публичный ключ (передайте машине получателя): %s\n", public)
	a.printf("приватный ключ: %s\n", keyFile)
	return nil
}

// resolveGrantScope определяет scope, в который попадёт новая машина.
//
// По умолчанию — scope «default»: он покрывает все разделы, кроме
// закрытых. Выдача доступа к закрытому разделу должна быть явной.
func (a *App) resolveGrantScope(requested string) (string, error) {
	if err := a.ensureRegistryFile(); err != nil {
		return "", err
	}
	loaded, err := a.loadRegistry()
	if err != nil {
		return "", err
	}
	if requested != "" {
		if _, found := loaded.Scope(requested); !found {
			return "", fmt.Errorf("%w: scope %s не определён в реестре", ErrUsage, requested)
		}
		return requested, nil
	}
	if _, found := loaded.Scope(registry.DefaultScope); found {
		return registry.DefaultScope, nil
	}
	names := loaded.ScopeNames()
	if len(names) == 0 {
		return "", fmt.Errorf("%w: в реестре нет scopes", ErrUsage)
	}
	return names[0], nil
}

// Revoke убирает получателя.
//
// Публичный ключ удаляется из реестра и из scopes, .sops.yaml
// пересобирается, файлы перешифровываются без получателя. Значения,
// которые снятая машина успела увидеть, нужно ротировать вручную —
// перешифровка их не отменяет.
func (a *App) Revoke(label string) error {
	if label == "" {
		return fmt.Errorf("%w: укажите метку: adytum revoke <label>", ErrUsage)
	}
	recipients, err := store.LoadRecipients()
	if err != nil {
		return err
	}
	if _, found := recipients[label]; !found {
		return fmt.Errorf("метка %s не найдена в реестре получателей", label)
	}
	if err := a.requireConsistentRegistry(""); err != nil {
		return err
	}

	if err := a.checkRevokeScope(label); err != nil {
		return err
	}

	if err := a.updateRegistry(func(document *registry.Document) error {
		document.RemoveRecipient(label)
		for _, scope := range document.ScopeNames() {
			document.RemoveFromScope(scope, label)
		}
		return nil
	}); err != nil {
		return err
	}
	delete(recipients, label)
	if err := store.SaveRecipients(recipients); err != nil {
		return err
	}
	if _, err := a.reencryptAll(); err != nil {
		return err
	}

	if err := a.commitAndPush(fmt.Sprintf("[.stbl](feat/adytum): revoke access for %s", label)); err != nil {
		return err
	}
	a.printf("метка %s отозвана\n", label)
	a.println("ВАЖНО: ротируйте значения, которые эта машина уже видела — перешифровка их не отменяет")
	return nil
}

// updateRegistry применяет правку к реестру и сохраняет файл.
//
// Правка применяется к загруженному документу: комментарии и порядок
// ключей сохраняются.
func (a *App) updateRegistry(edit func(document *registry.Document) error) error {
	if err := a.ensureRegistryFile(); err != nil {
		return err
	}
	document, err := registry.Load(a.RegistryPath())
	if err != nil {
		return err
	}
	if err := edit(document); err != nil {
		return err
	}
	return document.Save(a.RegistryPath())
}

// requireConsistentRegistry проверяет, что реестр согласован.
//
// Проверка идёт до правки: сломанный реестр должен остановить команду,
// а не остаться в файле в промежуточном состоянии.
//
// except — scope, который команда сейчас чинит: его собственные
// проблемы не считаются проблемами.
func (a *App) requireConsistentRegistry(except string) error {
	loaded, err := a.loadRegistry()
	if err != nil {
		return a.registryError(err)
	}
	return requireRegistryConsistent(loaded, except)
}

// ensureRegistryFile создаёт файл реестра, если его ещё нет.
//
// Хранилище без реестра — обычное состояние первого запуска: файл
// создаётся из шаблона, а записи добавляются по мере создания
// секретов. Без этого первая же команда, которой нужен реестр,
// падала бы на пустом хранилище.
func (a *App) ensureRegistryFile() error {
	path := a.RegistryPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(path, registry.Template(), 0o644); err != nil {
		return fmt.Errorf("не удалось создать %s: %w", path, err)
	}
	return nil
}

// Import переносит plaintext-хранилище в репозиторий.
//
// Файлы копируются с сохранением структуры и добавляются в индекс:
// шифротекст обеспечит clean-фильтр git. Исходный каталог не меняется.
func (a *App) Import(dir string) error {
	source, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s не является каталогом", source)
	}
	var copied []string
	err = filepath.Walk(source, func(path string, entry os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !IsSecretPath(relative) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(a.Root, filepath.FromSlash(relative))
		// Импорт кладёт открытое содержимое в хранилище: файл вне
		// фильтра уехал бы в remote как есть.
		if err := a.requireEncryptedPath(relative); err != nil {
			return err
		}

		if err := a.writePlaintext(target, content); err != nil {
			return err
		}
		if err := a.stageFile(target); err != nil {
			return err
		}
		copied = append(copied, relative)
		return nil
	})
	if err != nil {
		return err
	}
	if len(copied) == 0 {
		a.println("нечего импортировать")
		return nil
	}
	if err := a.commitAndPush("[.stbl](feat/adytum): import secrets from local store"); err != nil {
		return err
	}
	a.printf("импортировано файлов: %d\n", len(copied))
	return nil
}

// DiffConvert — textconv-драйвер для git diff.
//
// Git передаёт путь к файлу с зашифрованными данными. Драйвер
// расшифровывает их, чтобы diff показывал читаемый текст.
func (a *App) DiffConvert(path string) error {
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !looksEncrypted(encrypted) {
		a.print(string(encrypted))
		return nil
	}
	sops, err := a.sopsFor()
	if err != nil {
		return err
	}
	plain, err := sops.Decrypt(path, encrypted)
	if err != nil {
		return err
	}
	a.print(string(plain))
	return nil
}

// Identity назначает метку локальной идентичности этой машины.
//
// Метка нужна, чтобы знать, какой приватный ключ использовать при
// расшифровке: sops получает его через SOPS_AGE_KEY_FILE.
func (a *App) Identity(label string) error {
	if label == "" {
		return fmt.Errorf("%w: укажите метку", ErrUsage)
	}
	keyFile, err := store.KeyFile(label)
	if err != nil {
		return err
	}
	if _, err := os.Stat(keyFile); err != nil {
		return fmt.Errorf("файл идентичности %s не найден", keyFile)
	}
	if err := store.SetIdentityLabel(label); err != nil {
		return err
	}
	a.printf("идентичность машины: %s\n", label)
	return nil
}

// adoptSoleIdentity назначает метку, если ключ в ~/.adytum/keys ровно один.
//
// Это избавляет от лишнего шага на свежей машине с единственным ключом.
func (a *App) adoptSoleIdentity() error {
	dir, err := store.KeysDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("не найдена идентичность машины: выполните `adytum grant <label>`")
		}
		return err
	}
	var labels []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		labels = append(labels, strings.TrimSuffix(entry.Name(), ".txt"))
	}
	if len(labels) != 1 {
		return fmt.Errorf("не найдена идентичность машины: выполните `adytum identity <label>`")
	}
	return store.SetIdentityLabel(labels[0])
}

// warnUnresolvedSlots предупреждает о ролях без публичного ключа.
//
// Пока слот не заполнен, sops не сможет зашифровать файлы: битый
// получатель в правилах ломает шифрование целиком.
func (a *App) warnUnresolvedSlots() error {
	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err != nil {
		return nil
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		return nil
	}
	recipients, err := store.LoadRecipients()
	if err != nil {
		return nil
	}
	labels := config.UnresolvedPlaceholders(recipients)
	if len(labels) == 0 {
		return nil
	}
	a.printf("ВНИМАНИЕ: не заполнены слоты получателей: %s\n", strings.Join(labels, ", "))
	a.println("выполните `adytum grant <label>`, чтобы sops смог шифровать файлы")
	return nil
}

// fillPlaceholders подставляет реальные публичные ключи вместо заглушек.
//
// Заглушки age1REPLACE_ME_* заменяются ключами из ~/.adytum/recipients.
// Неизвестные метки остаются заглушками: их должен завести владелец
// через `adytum grant`.
func (a *App) fillPlaceholders() error {
	path := SopsConfigPath(a.Root)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		return err
	}
	recipients, err := store.LoadRecipients()
	if err != nil {
		return err
	}
	if !config.ReplacePlaceholders(recipients) {
		return nil
	}
	return a.writeSopsConfig(config)
}

// addRecipientToRules ставит получателя в правила .sops.yaml.
//
// Если в правиле есть заглушка с такой же меткой (слот роли), она
// заменяется. Иначе получатель добавляется — но только в правила, не
// ограниченные разделом personal: туда попадают лишь владелец и
// recovery-машина.
func (a *App) addRecipientToRules(label, public string) error {
	path := SopsConfigPath(a.Root)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		return err
	}
	for _, rule := range config.Rules() {
		if sopsconf.IsPersonalRule(rule.PathRegex) && !hasSlotFor(config, rule.PathRegex, label) {
			continue
		}
		if _, err := config.ReplaceOrAddRecipient(rule.PathRegex, public, label); err != nil {
			return err
		}
	}
	return a.writeSopsConfig(config)
}

// hasSlotFor сообщает, есть ли в правиле слот роли с такой меткой.
func hasSlotFor(config *sopsconf.File, pathRegex, label string) bool {
	for _, rule := range config.Rules() {
		if rule.PathRegex != pathRegex {
			continue
		}
		for _, recipient := range rule.Age {
			if !strings.HasPrefix(recipient, sopsconf.Placeholder+"_") {
				continue
			}
			if strings.EqualFold(strings.TrimPrefix(recipient, sopsconf.Placeholder+"_"), label) {
				return true
			}
		}
	}
	return false
}

// removeRecipientFromRules убирает публичный ключ из всех правил.
func (a *App) removeRecipientFromRules(public string) (int, error) {
	path := SopsConfigPath(a.Root)
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		return 0, err
	}
	removed := config.RemoveRecipient(public)
	if removed == 0 {
		return 0, nil
	}
	if err := a.writeSopsConfig(config); err != nil {
		return 0, err
	}
	return removed, nil
}

// writeSopsConfig сохраняет изменённый .sops.yaml.
func (a *App) writeSopsConfig(config *sopsconf.File) error {
	content, err := config.Bytes()
	if err != nil {
		return err
	}
	return os.WriteFile(SopsConfigPath(a.Root), content, 0o644)
}

// secretPaths перечисляет файлы секретов в репозитории.
func (a *App) secretPaths() ([]string, error) {
	var paths []string
	err := filepath.Walk(a.Root, func(path string, entry os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsSecretPath(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// commitAndPush коммитит изменения и отправляет их в remote.
//
// Отсутствие remote не считается ошибкой: локальный репозиторий без
// настроенного remote — обычное состояние при первом развёртывании.
func (a *App) commitAndPush(message string) error {
	// Коммит забирает всё: файл вне фильтра ушёл бы в remote открытым.
	if err := a.requireEncryptedSecrets(); err != nil {
		return err
	}

	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "add", "-A"); err != nil {
		return err
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("не удалось закоммитить: %w", err)
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "push"); err != nil {
		if remote, _ := a.gitOutput("remote", "get-url", "origin"); remote == "" {
			a.println("remote не настроен: изменения сохранены только локально")
			return nil
		}
		return fmt.Errorf("не удалось отправить изменения: %w", err)
	}
	return nil
}

// gitOutput выполняет git-команду и возвращает stdout без пробелов.
func (a *App) gitOutput(args ...string) (string, error) {
	out, err := a.Runner.Run(Command{Dir: a.Root}, "git", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// relativeTo строит путь относительно корня репозитория со слэшами.
func relativeTo(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

// orUnknown подставляет заглушку для пустого значения.
func orUnknown(value string) string {
	if value == "" {
		return "не задан"
	}
	return value
}
