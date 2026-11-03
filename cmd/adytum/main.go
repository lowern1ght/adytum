// Command adytum — CLI для age/sops-шифрованного git-репозитория секретов.
//
// adytum — тонкая обёртка над sops, git и age: вся криптография и работа
// с репозиторием делаются внешними инструментами, CLI отвечает за
// сценарии (init, sync, get, set, new, run, unlock, grant, revoke,
// reencrypt, import, status, doctor, bootstrap-remote, schedule).
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lowern1ght/adytum/internal/adytum"
	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/ui"
)

// version — версия CLI.
const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		// Отчёт самопроверки уже напечатан: повторять его в stderr
		// незачем, нужен только код возврата.
		if !errors.Is(err, adytum.ErrDoctorFailed) {
			fmt.Fprintln(os.Stderr, "adytum:", err)
			printAdvice(err)
		}
		os.Exit(1)
	}
}

// printAdvice печатает подсказку «что делать» для ошибки.
//
// Подсказка объясняет агенту, куда двигаться дальше: без неё ошибка
// выглядит тупиком, и агент начинает искать обходные пути.
func printAdvice(err error) {
	adviser, ok := err.(adytum.Adviser)
	if !ok {
		return
	}
	advice := adviser.Advice()
	printer := ui.New(os.Stderr, false, false)
	printer.Lock("%s", advice.Text)
}

// commandHelpText — справка по каждой команде.
//
// Справка нужна до инициализации репозитория, поэтому описание команд
// хранится отдельно от основного списка.
var commandHelpText = map[string]string{
	"init":             "подготовить склонированный репозиторий: git-фильтры, .gitattributes, .sops.yaml\n               публичные ключи машин подставляются из ~/.adytum/recipients",
	"sync":             "получить изменения, обновить рабочее дерево, отправить локальные коммиты\n               adytum sync --pull-only   только забрать изменения, ничего не отправляя",
	"get":              "напечатать расшифрованный секрет\n               adytum get <project>/<service>            весь файл\n               adytum get <project>/<service>.<KEY>      одно значение\n               adytum get --json <project>/<service>     машинночитаемый вывод",
	"list":             "показать реестр секретов\n               adytum list              таблица записей\n               adytum list <entry>      подробно об одном секрете",
	"set":              "занести значение в секрет\n               adytum set <project>/<service> <KEY>   один ключ\n               adytum set <project>/<service>         все ключи записи\n               adytum set <project>/<service> <KEY> --stdin   значение из stdin",
	"unset":            "удалить ключ из секрета\n               adytum unset <project>/<service> <KEY>",
	"edit":             "открыть секрет в $EDITOR\n               adytum edit <project>/<service>",
	"new":              "создать новый секрет: файл, запись в реестре и коммит\n               adytum new <project>/<service> [--type env|yaml|json|binary|kubeconfig]\n                                    [--description \"описание\"] [--scope <scope>]",
	"run":              "выполнить команду с секретами в окружении\n               adytum run --env <project>/<service> -- <команда> [аргументы...]",
	"unlock":           "разрешить агентским сессиям операции get и run на 8 часов",
	"identity":         "указать, какой локальный ключ принадлежит этой машине\n               adytum identity <label>",
	"grant":            "создать age-идентичность и выдать доступ\n               adytum grant <label> [--scope <scope>]",
	"revoke":           "отозвать доступ и перешифровать файлы\n               adytum revoke <label>",
	"reencrypt":        "перешифровать секреты по правилам реестра\n               adytum reencrypt [--yes]",
	"import":           "перенести plaintext-хранилище в репозиторий\n               adytum import <dir>",
	"status":           "показать ветку, remote, получателей, разблокировку и git-фильтры\n               adytum status [--json]",
	"doctor":           "проверить окружение: sops, age, идентичность, фильтры, .sops.yaml, remote\n               adytum doctor [--json]",
	"bootstrap-remote": "создать приватный репозиторий данных на GitHub и настроить remote\n               adytum bootstrap-remote [--json]",
	"schedule":         "включить таймер автосинхронизации (systemd, раз в 5 минут, только pull)\n               adytum schedule [--json]",
}

// hasHelpFlag сообщает, есть ли среди аргументов флаг справки.
func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

// usageCommand печатает справку по одной команде.
func usageCommand(out *os.File, command, help string) {
	fmt.Fprintf(out, "adytum %s — %s\n\n", command, help)
	fmt.Fprintf(out, "Использование:\n  adytum %s [аргументы]\n", command)
}

// run разбирает аргументы и выполняет команду.
func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	command := args[0]
	rest := args[1:]

	switch command {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	case "version", "--version":
		fmt.Println("adytum", version)
		return nil
	}

	// Справка по конкретной команде не требует репозитория: её можно
	// запросить до инициализации машины. Проверяем только сами флаги
	// справки: --json и прочие аргументы команду не отменяют.
	if hasHelpFlag(rest) {
		if help, ok := commandHelpText[command]; ok {
			usageCommand(os.Stdout, command, help)
			return nil
		}
	}

	app, err := adytum.New(adytum.Exec{})
	if err != nil {
		return err
	}

	switch command {
	case "init":
		return app.Init()
	case "sync":
		return runSync(app, rest)
	case "get":
		return runGet(app, rest)
	case "set":
		return runSet(app, rest)
	case "unset":
		return runUnset(app, rest)
	case "edit":
		return runEdit(app, rest)
	case "new":
		return runNew(app, rest)
	case "list":
		return runList(app, rest)
	case "run":
		return runRun(app, rest)
	case "unlock":
		return app.Unlock()
	case "identity":
		return runIdentity(app, rest)
	case "grant":
		return runGrant(app, rest)
	case "revoke":
		return runRevoke(app, rest)
	case "reencrypt":
		return runReencrypt(app, rest)
	case "import":
		return runImport(app, rest)
	case "status":
		return runStatus(app, rest)
	case "doctor":
		return runDoctor(app, rest)
	case "bootstrap-remote":
		return runBootstrapRemote(app, rest)
	case "schedule":
		return runSchedule(app, rest)
	case "diff-convert":
		return runDiffConvert(app, rest)
	default:
		usage(os.Stderr)
		return fmt.Errorf("неизвестная команда %q", command)
	}
}

// runSync выполняет sync [--pull-only].
func runSync(app *adytum.App, args []string) error {
	pullOnly := false
	for _, arg := range args {
		switch arg {
		case "--pull-only":
			pullOnly = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	return app.Sync(pullOnly)
}

// runGet выполняет get <project>/<service>[.<KEY>].
func runGet(app *adytum.App, args []string) error {
	jsonOutput := false
	targets := []string{}
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "-h", "--help":
			usageCommand(os.Stdout, "get", commandHelpText["get"])
			return nil
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("неожиданный аргумент %q", arg)
			}
			targets = append(targets, arg)
		}
	}
	if len(targets) == 0 {
		return errors.New("указание: adytum get <project>/<service>[.<KEY>]")
	}
	target := targets[0]
	project, service, key, err := splitKey(target)
	if err != nil {
		return err
	}
	if jsonOutput {
		return app.GetJSON(project, service, key)
	}
	return app.Get(project, service, key)
}

// splitKey отделяет ключ от пути вида <project>/<service>.<KEY>.
//
// Расширения файлов (.env, .yaml, .json) ключами не считаются.
func splitKey(target string) (string, string, string, error) {
	project, service, err := adytum.SplitTarget(target)
	if err != nil {
		return "", "", "", err
	}
	dot := strings.LastIndex(service, ".")
	if dot <= 0 {
		return project, service, "", nil
	}
	name, key := service[:dot], service[dot+1:]
	if key == "" || adytum.IsSecretExt("."+key) {
		return project, service, "", nil
	}
	return project, name, key, nil
}

// runSet выполняет set <project>/<service> [KEY] [--stdin].
func runSet(app *adytum.App, args []string) error {
	options := adytum.SetOptions{}
	targets := []string{}
	for _, arg := range args {
		switch arg {
		case "--stdin":
			options.FromStdin = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("неожиданный аргумент %q", arg)
			}
			targets = append(targets, arg)
		}
	}
	if len(targets) == 0 {
		return errors.New("указание: adytum set <project>/<service> [KEY] [--stdin]")
	}
	if len(targets) > 2 {
		return errors.New("указание: adytum set <project>/<service> [KEY] [--stdin]")
	}
	options.Key = ""
	if len(targets) == 2 {
		options.Key = targets[1]
	}
	return app.Set(targets[0], options)
}

// runUnset выполняет unset <project>/<service> <KEY>.
func runUnset(app *adytum.App, args []string) error {
	if len(args) != 2 {
		return errors.New("указание: adytum unset <project>/<service> <KEY>")
	}
	return app.Unset(args[0], args[1])
}

// runEdit выполняет edit <project>/<service>.
func runEdit(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum edit <project>/<service>")
	}
	project, service, err := adytum.SplitTarget(args[0])
	if err != nil {
		return err
	}
	return app.Edit(project, service)
}

// runNew выполняет new <project>/<service> [--type T] [--description D] [--scope S].
func runNew(app *adytum.App, args []string) error {
	options := adytum.NewOptions{}
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--type" || arg == "--description" || arg == "--scope":
			index++
			if index >= len(args) {
				return fmt.Errorf("%s требует значение", arg)
			}
			value := args[index]
			switch arg {
			case "--type":
				options.Type = registry.Type(value)
			case "--description":
				options.Description = value
				options.DescriptionSet = true
			case "--scope":
				options.Scope = value
			}
		case strings.HasPrefix(arg, "--type="):
			options.Type = registry.Type(strings.TrimPrefix(arg, "--type="))
		case strings.HasPrefix(arg, "--description="):
			options.Description = strings.TrimPrefix(arg, "--description=")
			options.DescriptionSet = true
		case strings.HasPrefix(arg, "--scope="):
			options.Scope = strings.TrimPrefix(arg, "--scope=")
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("неожиданный аргумент %q", arg)
			}
			targets = append(targets, arg)
		}
	}
	if len(targets) == 0 {
		return errors.New("указание: adytum new <project>/<service>")
	}
	return app.New(targets[0], options)
}

// runList выполняет list [<entry>] [--json].
func runList(app *adytum.App, args []string) error {
	targets := []string{}
	for _, arg := range args {
		switch arg {
		case "--json":
			app.JSON = true
		case "--pretty":
			app.Pretty = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("неожиданный аргумент %q", arg)
			}
			targets = append(targets, arg)
		}
	}
	if len(targets) > 1 {
		return errors.New("указание: adytum list [<entry>]")
	}
	name := ""
	if len(targets) == 1 {
		name = targets[0]
	}
	return app.List(name)
}

// runRun выполняет run --env <project>/<service>... -- <команда> [аргументы...].
func runRun(app *adytum.App, args []string) error {
	envs := []string{}
	command := []string{}
	index := 0
	for ; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			index++
			break
		}
		if arg == "--env" || arg == "-e" {
			index++
			if index >= len(args) {
				return errors.New("--env требует значение <project>/<service>")
			}
			envs = append(envs, args[index])
			continue
		}
		if strings.HasPrefix(arg, "--env=") {
			envs = append(envs, strings.TrimPrefix(arg, "--env="))
			continue
		}
		return fmt.Errorf("неожиданный аргумент %q", arg)
	}
	command = args[index:]
	if len(command) == 0 {
		return errors.New("указание: adytum run --env <project>/<service> -- <команда> [аргументы...]")
	}
	return app.Run(envs, command)
}

// runIdentity выполняет identity <label>.
func runIdentity(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum identity <label>")
	}
	return app.Identity(args[0])
}

// runGrant выполняет grant <label> [--scope S].
func runGrant(app *adytum.App, args []string) error {
	label := ""
	scope := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--scope":
			index++
			if index >= len(args) {
				return errors.New("--scope требует значение")
			}
			scope = args[index]
		case strings.HasPrefix(arg, "--scope="):
			scope = strings.TrimPrefix(arg, "--scope=")
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("неожиданный аргумент %q", arg)
			}
			if label != "" {
				return errors.New("указание: adytum grant <label>")
			}
			label = arg
		}
	}
	if label == "" {
		return errors.New("указание: adytum grant <label>")
	}
	return app.Grant(label, scope)
}

// runRevoke выполняет revoke <label>.
func runRevoke(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum revoke <label>")
	}
	return app.Revoke(args[0])
}

// runReencrypt выполняет reencrypt [--yes].
func runReencrypt(app *adytum.App, args []string) error {
	confirmed := false
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			confirmed = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	return app.Reencrypt(confirmed)
}

// runImport выполняет import <dir>.
func runImport(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum import <dir>")
	}
	return app.Import(args[0])
}

// runStatus выполняет status [--json] [--pretty].
func runStatus(app *adytum.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--pretty":
			app.Pretty = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	if jsonOutput {
		return app.StatusJSON()
	}
	return app.Status()
}

// runDoctor выполняет doctor [--json] [--pretty].
func runDoctor(app *adytum.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--pretty":
			app.Pretty = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	app.JSON = jsonOutput
	return app.DoctorPrint()
}

// runBootstrapRemote выполняет bootstrap-remote [--json].
func runBootstrapRemote(app *adytum.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--pretty":
			app.Pretty = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	result, err := app.BootstrapRemote()
	if jsonOutput {
		return app.BootstrapJSON(result)
	}
	if err != nil {
		return err
	}
	app.BootstrapReport(result)
	return nil
}

// runSchedule выполняет schedule [--json].
func runSchedule(app *adytum.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--pretty":
			app.Pretty = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	app.JSON = jsonOutput
	return app.SchedulePrint()
}

// runDiffConvert выполняет внутреннюю команду diff-convert <file>.
func runDiffConvert(app *adytum.App, args []string) error {
	if len(args) != 1 {
		return errors.New("указание: adytum diff-convert <file>")
	}
	return app.DiffConvert(args[0])
}

// usage печатает справку по командам.
func usage(out *os.File) {
	fmt.Fprint(out, `adytum — age/sops-шифрованный git-репозиторий секретов.

Использование:
  adytum <команда> [аргументы]

Команды:
  init                      подготовить склонированный репозиторий (фильтры, .sops.yaml)
  sync [--pull-only]        получить изменения, обновить дерево, отправить свои
  get <project>/<service>   напечатать расшифрованный секрет (или один ключ)
  list [<entry>]            показать реестр секретов
  set <p>/<s> [KEY]         занести значение в секрет (--stdin — из stdin)
  unset <p>/<s> <KEY>       удалить ключ из секрета
  edit <project>/<service>  отредактировать секрет в $EDITOR
  new <project>/<service>   создать новый секрет и запись в реестре
  run --env <p>/<s> -- cmd  выполнить команду с секретами в окружении
  unlock                    разрешить агентским сессиям операции get и run
  identity <label>          указать, какой локальный ключ принадлежит этой машине
  grant <label>             создать age-идентичность и выдать доступ
  revoke <label>            отозвать доступ и перешифровать
  reencrypt                 перешифровать секреты по текущим правилам
  import <dir>              перенести plaintext-хранилище в репозиторий
  status [--json]           показать состояние репозитория
  doctor [--json]           проверить окружение и исправить найденное
  bootstrap-remote [--json] создать приватный репозиторий данных на GitHub
  schedule [--json]         включить таймер автосинхронизации (systemd)

Идентичность машины: `+"`~/.adytum/keys/<label>.txt`"+`, маркер разблокировки: `+"`~/.adytum/unlocked`"+`.
`)
}
