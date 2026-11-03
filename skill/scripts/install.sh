#!/usr/bin/env bash
# Установка adytum на новой машине.
#
# Скрипт ставит зависимости (sops, age), собирает CLI, клонирует
# репозиторий секретов, инициализирует его и создаёт короткий алиас ady.
#
# Использование:
#   bash install.sh [каталог-репозитория]
#
# Переменные окружения:
#   ADYTUM_REPO   путь к репозиторию секретов (по умолчанию ~/adytum)
#   ADYTUM_BIN    куда положить бинарник (по умолчанию ~/.local/bin)

set -euo pipefail

REPO_DIR="${1:-${ADYTUM_REPO:-$HOME/adytum}}"
BIN_DIR="${ADYTUM_BIN:-$HOME/.local/bin}"
SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

say() { printf '\n== %s\n' "$1"; }
die() { printf 'ошибка: %s\n' "$1" >&2; exit 1; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 не найден в PATH: $2"
}

say "проверка зависимостей"
require_command git "установите git"
require_command go "установите Go 1.22+"

if ! command -v sops >/dev/null 2>&1; then
  say "установка sops"
  if command -v brew >/dev/null 2>&1; then
    brew install sops
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update && sudo apt-get install -y sops
  elif command -v winget >/dev/null 2>&1; then
    winget install --id getsops.sops -e --accept-source-agreements --accept-package-agreements
  else
    die "sops не найден и пакетный менеджер не определён: https://github.com/getsops/sops#installation"
  fi
fi

if ! command -v age-keygen >/dev/null 2>&1; then
  say "установка age"
  if command -v brew >/dev/null 2>&1; then
    brew install age
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update && sudo apt-get install -y age
  elif command -v winget >/dev/null 2>&1; then
    winget install --id FiloSottile.age -e --accept-source-agreements --accept-package-agreements
  else
    die "age не найден и пакетный менеджер не определён: https://github.com/FiloSottile/age#installation"
  fi
fi

say "сборка adytum"
mkdir -p "$BIN_DIR"
(cd "$SOURCE_DIR" && go build -o "$BIN_DIR/adytum" ./cmd/adytum)
printf 'установлено: %s\n' "$BIN_DIR/adytum"

say "алиас ady"
cat >"$BIN_DIR/ady" <<EOF
#!/usr/bin/env bash
exec "$BIN_DIR/adytum" "\$@"
EOF
chmod +x "$BIN_DIR/ady"
printf 'создано: %s\n' "$BIN_DIR/ady"

say "репозиторий секретов"
if [ -d "$REPO_DIR/.git" ]; then
  printf 'уже склонирован: %s\n' "$REPO_DIR"
else
  if [ -n "${ADYTUM_REMOTE:-}" ]; then
    git clone "$ADYTUM_REMOTE" "$REPO_DIR"
  else
    die "переменная ADYTUM_REMOTE не задана: укажите адрес приватного репозитория"
  fi
fi

say "инициализация репозитория"
cd "$REPO_DIR"
if [ -f "$HOME/.adytum/identity" ]; then
  printf 'идентичность машины уже задана: %s\n' "$(cat "$HOME/.adytum/identity")"
else
  printf 'создайте идентичность машины: adytum identity <label>\n'
fi
"$BIN_DIR/adytum" init

say "готово"
cat <<EOF
Дальше:
  1. Получите у владельца приватный ключ этой машины и положите его в
     ~/.adytum/keys/<label>.txt (права 0600), затем выполните:
       adytum identity <label>
  2. Синхронизируйтесь с репозиторием:
       adytum sync
  3. Для работы агентских сессий включите разблокировку:
       adytum unlock

Путь к бинарнику: $BIN_DIR
Если $BIN_DIR не в PATH, добавьте его в профиль оболочки.
EOF
