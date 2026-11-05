package adytum

import (
	"fmt"
	"net/url"
	"strings"
)

// tokenEnv — переменная окружения с токеном GitHub.
//
// Токен не попадает в аргументы git: аргументы видны в списке
// процессов всем пользователям машины, а окружение — только владельцу
// процесса.
const tokenEnv = "ADYTUM_GIT_TOKEN"

// credentialHelper — помощник учётных данных git.
//
// Помощник отдаёт токен из переменной окружения и ничего не
// сохраняет: учётные данные живут только в момент обращения к
// remote. Значения токена в строке нет — там только имя переменной.
//
// Обращается помощник только к своему хосту: токен не должен уйти на
// чужой remote.
const credentialHelper = `!f() { req=$(cat); case "$req" in *host=%s*) echo username=x-access-token; echo password=$` + tokenEnv + `;; esac; }; f`

// remoteAuth готовит команду git для работы с приватным remote.
//
// Пустой credential.helper сбрасывает список помощников: токен не
// должен попасть в ~/.git-credentials. Второй помощник отдаёт токен
// по запросу git — и только тому хосту, у которого настроен remote.
//
// Токена или хоста нет — команда выполняется без авторизации, как и
// раньше.
//
// Возвращается команда, аргументы и токен: токен нужен, чтобы
// вычистить его из текста ошибки git.
func (a *App) remoteAuth(args ...string) (Command, []string, string) {
	token, err := a.githubToken()
	host := a.remoteHost()
	if err != nil || token == "" || host == "" {
		return Command{Dir: a.Root}, args, ""
	}
	command := Command{
		Dir: a.Root,
		Env: []string{
			tokenEnv + "=" + token,
			// Без помощника git спросил бы пароль в терминале и
			// завис: у CLI нет интерактивного режима.
			"GIT_TERMINAL_PROMPT=0",
		},
	}
	options := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=" + fmt.Sprintf(credentialHelper, host),
	}
	return command, append(options, args...), token
}

// remoteHost возвращает хост remote origin.
//
// Пустой результат означает, что remote не настроен или его адрес
// не разобрать: в обоих случаях помощник учётных данных не нужен.
func (a *App) remoteHost() string {
	remote, err := a.gitOutput("remote", "get-url", "origin")
	if err != nil || remote == "" {
		return ""
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Hostname()
}

// redactToken убирает токен из текста ошибки.
//
// Git печатает URL с учётными данными в сообщениях об отказе по
// авторизации: без очистки токен попал бы в вывод команды.
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), token, "***")
	return fmt.Errorf("%s", message)
}
