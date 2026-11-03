// Command adytum — CLI для age/sops-шифрованного git-репозитория секретов.
//
// adytum — тонкая обёртка над sops, git и age: вся криптография и работа
// с репозиторием делаются внешними инструментами, CLI отвечает за
// сценарии (init, sync, get, set, new, run, unlock, grant, revoke,
// reencrypt, import, status).
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lowern1ght/adytum/internal/adytum"
)

// version — версия CLI.
const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "adytum:", err)
		os.Exit(1)
	}
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

	app, err := adytum.New(adytum.Exec{})
	if err != nil {
		return err
	}

	switch command {
	case "init":
		return app.Init()
	case "sync":
		return app.Sync()
	case "get":
		return runGet(app, rest)
	case "set":
		return runSet(app, rest)
	case "new":
		return runNew(app, rest)
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
		return app.Reencrypt()
	case "import":
		return runImport(app, rest)
	case "status":
		return runStatus(app, rest)
	case "diff-convert":
		return runDiffConvert(app, rest)
	default:
		usage(os.Stderr)
		return fmt.Errorf("неизвестная команда %q", command)
	}
}

// runGet выполняет get <project>/<service>[.<KEY>].
func runGet(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum get <project>/<service>[.<KEY>]")
	}
	target := args[0]
	project, service, err := adytum.SplitTarget(target)
	if err != nil {
		return err
	}
	key := ""
	if project, service, key, err = splitKey(target); err != nil {
		return err
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

// runSet выполняет set <project>/<service>.
func runSet(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum set <project>/<service>")
	}
	project, service, err := adytum.SplitTarget(args[0])
	if err != nil {
		return err
	}
	return app.Set(project, service)
}

// runNew выполняет new <project>/<service>.
func runNew(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum new <project>/<service>")
	}
	project, service, err := adytum.SplitTarget(args[0])
	if err != nil {
		return err
	}
	return app.New(project, service)
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

// runGrant выполняет grant <label>.
func runGrant(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum grant <label>")
	}
	return app.Grant(args[0])
}

// runRevoke выполняет revoke <label>.
func runRevoke(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum revoke <label>")
	}
	return app.Revoke(args[0])
}

// runImport выполняет import <dir>.
func runImport(app *adytum.App, args []string) error {
	if len(args) == 0 {
		return errors.New("указание: adytum import <dir>")
	}
	return app.Import(args[0])
}

// runStatus выполняет status [--json].
func runStatus(app *adytum.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("неожиданный аргумент %q", arg)
		}
	}
	return app.StatusJSON(jsonOutput)
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
  sync                      получить изменения, обновить дерево, отправить свои
  get <project>/<service>   напечатать расшифрованный секрет (или один ключ)
  set <project>/<service>   отредактировать секрет в $EDITOR
  new <project>/<service>   создать новый секрет из шаблона и закоммитить
  run --env <p>/<s> -- cmd  выполнить команду с секретами в окружении
  unlock                    разрешить агентским сессиям операции get и run
  identity <label>          указать, какой локальный ключ принадлежит этой машине
  grant <label>             создать age-идентичность и выдать доступ
  revoke <label>            отозвать доступ и перешифровать
  reencrypt                 перешифровать секреты по текущим правилам
  import <dir>              перенести plaintext-хранилище в репозиторий
  status [--json]           показать состояние репозитория

Идентичность машины: `+"`~/.adytum/keys/<label>.txt`"+`, маркер разблокировки: `+"`~/.adytum/unlocked`"+`.
`)
}
