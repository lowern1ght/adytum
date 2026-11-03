package adytum

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/store"
)

// App — состояние CLI: исполнитель команд и корень репозитория.
type App struct {
	// Runner выполняет внешние команды.
	Runner Runner
	// Root — корень репозитория (определяется один раз при старте).
	Root string
	// Now — текущее время (подменяется в тестах).
	Now func() time.Time
}

// New создаёт App и определяет корень репозитория.
func New(runner Runner) (*App, error) {
	root, err := RepoRoot(runner, ".")
	if err != nil {
		return nil, err
	}
	return &App{Runner: runner, Root: root, Now: time.Now}, nil
}

// Init готовит уже склонированный репозиторий к работе.
//
// Проверяет наличие sops и git, ставит git-фильтры, создаёт .gitattributes
// и .sops.yaml, если их ещё нет.
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
	for key, value := range gitConfigValues(keyFile) {
		if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "config", key, value); err != nil {
			return fmt.Errorf("не удалось задать git config %s: %w", key, err)
		}
	}

	fmt.Printf("репозиторий: %s\n", a.Root)
	if createdAttrs {
		fmt.Println("создан .gitattributes")
	}
	if createdSops {
		fmt.Println("создан .sops.yaml")
	}
	fmt.Println("git-фильтры adytum установлены")
	return nil
}

// Sync синхронизирует репозиторий с remote.
//
// Сначала подтягивает изменения, затем обновляет рабочее дерево, чтобы
// smunge-фильтр применился к изменённым файлам, и только потом отправляет
// локальные коммиты.
func (a *App) Sync() error {
	remote, _ := a.gitOutput("remote", "get-url", "origin")
	if remote == "" {
		fmt.Println("remote не настроен: синхронизация только с локальным репозиторием")
	} else {
		if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "pull", "--rebase"); err != nil {
			return fmt.Errorf("не удалось получить изменения: %w", err)
		}
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "checkout", "--", "."); err != nil {
		return fmt.Errorf("не удалось обновить рабочее дерево: %w", err)
	}
	// push выполняем без --force: отправляем только то, что уже закоммичено.
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "push"); err != nil {
		if remote == "" {
			fmt.Println("remote не настроен: изменения сохранены только локально")
			return nil
		}
		return fmt.Errorf("не удалось отправить изменения: %w", err)
	}
	fmt.Println("репозиторий синхронизирован")
	return nil
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
	return sops.DecryptFile(path)
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

// Get печатает расшифрованное содержимое секрета.
//
// Если задан key, печатается только значение этого ключа. Значение
// уходит только в stdout.
func (a *App) Get(project, service, key string) error {
	path, _, err := ResolveSecret(a.Root, project, service)
	if err != nil {
		return err
	}
	plain, err := a.readPlaintext(path)
	if err != nil {
		return err
	}
	if key == "" {
		fmt.Print(string(plain))
		return nil
	}
	entries, err := parseEntries(plain)
	if err != nil {
		return err
	}
	value, found := lookup(entries, key)
	if !found {
		return fmt.Errorf("ключ %s не найден в %s", key, filepath.Base(path))
	}
	fmt.Println(value)
	return nil
}

// Set открывает секрет в редакторе.
//
// Файл расшифровывается, открывается в $EDITOR, результат записывается
// обратно в рабочее дерево и добавляется в индекс: шифротекст в
// репозитории обеспечит clean-фильтр git.
func (a *App) Set(project, service string) error {
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
		fmt.Println("изменений нет")
		return nil
	}
	if err := a.writePlaintext(path, []byte(edited)); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}
	fmt.Printf("обновлён %s\n", relativeTo(a.Root, path))
	return nil
}

// New создаёт новый секрет из шаблона и коммитит его.
func (a *App) New(project, service string) error {
	if project == "" || service == "" {
		return fmt.Errorf("%w: укажите проект и сервис: adytum new <project>/<service>", ErrUsage)
	}
	path := filepath.Join(a.Root, project, service+".env")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("секрет %s уже существует", relativeTo(a.Root, path))
	}
	if err := a.writePlaintext(path, []byte(newEntryTemplate)); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}
	message := fmt.Sprintf("[.stbl](feat/adytum): add %s/%s secret entry", project, service)
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("не удалось закоммитить: %w", err)
	}
	fmt.Printf("создан %s\n", relativeTo(a.Root, path))
	return nil
}

// Unlock создаёт маркер разблокировки для агентских сессий.
//
// Маркер живёт 8 часов (store.UnlockTTL) и разрешает плагину выполнять
// операции get и run.
func (a *App) Unlock() error {
	expires, err := store.WriteUnlock(a.Now())
	if err != nil {
		return err
	}
	fmt.Printf("разблокировка активна до %s\n", expires.Format("2006-01-02T15:04:05Z07:00"))
	return nil
}

// Grant добавляет нового получателя.
//
// Генерируется новая age-идентичность, приватный ключ кладётся в
// ~/.adytum/keys/<label>.txt, публичный ключ занимает слот роли в
// .sops.yaml, после чего файлы перешифровываются под новый набор
// получателей.
func (a *App) Grant(label string) error {
	if label == "" {
		return fmt.Errorf("%w: укажите метку: adytum grant <label>", ErrUsage)
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

	// .sops.yaml нужен до выдачи доступа: без правил шифрования sops
	// не сможет ни зашифровать, ни перешифровать файлы.
	if _, err := ensureSopsConfig(a.Root); err != nil {
		return err
	}
	if err := a.addRecipientToRules(label, public); err != nil {
		return err
	}
	if err := a.Reencrypt(); err != nil {
		return err
	}
	if err := a.commitAndPush(fmt.Sprintf("[.stbl](feat/adytum): grant access to %s", label)); err != nil {
		return err
	}
	fmt.Printf("метка %s создана\n", label)
	fmt.Printf("публичный ключ (передайте машине получателя): %s\n", public)
	fmt.Printf("приватный ключ: %s\n", keyFile)
	return nil
}

// Revoke убирает получателя.
//
// Публичный ключ удаляется из .sops.yaml, файлы перешифровываются без
// него. Значения, которые снятая машина успела увидеть, нужно ротировать
// вручную — перешифровка их не отменяет.
func (a *App) Revoke(label string) error {
	if label == "" {
		return fmt.Errorf("%w: укажите метку: adytum revoke <label>", ErrUsage)
	}
	recipients, err := store.LoadRecipients()
	if err != nil {
		return err
	}
	public, found := recipients[label]
	if !found {
		return fmt.Errorf("метка %s не найдена в реестре получателей", label)
	}
	removed, err := a.removeRecipientFromRules(public)
	if err != nil {
		return err
	}
	if removed == 0 {
		return fmt.Errorf("получатель %s не найден в .sops.yaml", public)
	}
	delete(recipients, label)
	if err := store.SaveRecipients(recipients); err != nil {
		return err
	}
	if err := a.Reencrypt(); err != nil {
		return err
	}
	if err := a.commitAndPush(fmt.Sprintf("[.stbl](feat/adytum): revoke access for %s", label)); err != nil {
		return err
	}
	fmt.Printf("метка %s отозвана\n", label)
	fmt.Println("ВАЖНО: ротируйте значения, которые эта машина уже видела — перешифровка их не отменяет")
	return nil
}

// Reencrypt перешифровывает все секреты по текущим правилам .sops.yaml.
//
// Файлы перешифровываются через clean-фильтр git: команда renormalize
// прогоняет содержимое рабочего дерева через sops encrypt заново, с
// новым набором получателей из .sops.yaml.
func (a *App) Reencrypt() error {
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "add", "--renormalize", "."); err != nil {
		return fmt.Errorf("не удалось перешифровать файлы: %w", err)
	}
	fmt.Println("файлы перешифрованы по текущим правилам .sops.yaml")
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
		fmt.Println("нечего импортировать")
		return nil
	}
	if err := a.commitAndPush("[.stbl](feat/adytum): import secrets from local store"); err != nil {
		return err
	}
	fmt.Printf("импортировано файлов: %d\n", len(copied))
	return nil
}

// Status печатает состояние репозитория.
//
// Показывает ветку, remote, получателей из .sops.yaml, состояние
// маркера разблокировки и наличие git-фильтров.
func (a *App) Status() error {
	branch, _ := a.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	remote, _ := a.gitOutput("remote", "get-url", "origin")

	fmt.Printf("ветка:    %s\n", orUnknown(branch))
	fmt.Printf("remote:   %s\n", orUnknown(remote))

	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err != nil {
		fmt.Println("получатели: .sops.yaml не найден")
	} else {
		config, parseErr := sopsconf.Parse(content)
		if parseErr != nil {
			fmt.Printf("получатели: не удалось разобрать .sops.yaml: %v\n", parseErr)
		} else {
			recipients := config.Recipients()
			if len(recipients) == 0 {
				fmt.Println("получатели: не заданы")
			}
			for _, recipient := range recipients {
				fmt.Printf("получатель: %s\n", recipient)
			}
		}
	}

	state, err := store.Unlock(a.Now())
	switch {
	case err != nil:
		fmt.Printf("разблокировка: ошибка чтения маркера: %v\n", err)
	case state.Unlocked:
		fmt.Printf("разблокировка: активна, осталось %s\n", state.Remaining.Round(time.Minute))
	case state.ExpiresAt.IsZero():
		fmt.Println("разблокировка: не активна (нет маркера)")
	default:
		fmt.Println("разблокировка: истекла")
	}

	for _, key := range []string{"filter.adytum.clean", "filter.adytum.smudge", "filter.adytum.required"} {
		value, err := a.gitOutput("config", "--get", key)
		if err != nil || value == "" {
			fmt.Printf("git %s: не настроен\n", key)
			continue
		}
		fmt.Printf("git %s: %s\n", key, value)
	}
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
		fmt.Print(string(encrypted))
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
	fmt.Print(string(plain))
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
	fmt.Printf("идентичность машины: %s\n", label)
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
	fmt.Printf("ВНИМАНИЕ: не заполнены слоты получателей: %s\n", strings.Join(labels, ", "))
	fmt.Println("выполните `adytum grant <label>`, чтобы sops смог шифровать файлы")
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
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "add", "-A"); err != nil {
		return err
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("не удалось закоммитить: %w", err)
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "push"); err != nil {
		if remote, _ := a.gitOutput("remote", "get-url", "origin"); remote == "" {
			fmt.Println("remote не настроен: изменения сохранены только локально")
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
