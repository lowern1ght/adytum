// adytum — плагин opencode для доступа к секретам репозитория adytum.
//
// Плагин даёт агентам два инструмента: adytum_get и adytum_run. Оба
// требуют активной разблокировки (adytum unlock): без неё операции
// отклоняются с понятным сообщением.
//
// Формат экспорта повторяет локальные плагины opencode: один файл
// работает и в V1 (server), и в V2 (setup).
import { execFile } from "node:child_process"
import { existsSync, readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

// ADYTUM_BIN — исполняемый файл CLI. Переопределяется переменной
// окружения, иначе используется adytum из PATH.
const ADYTUM_BIN = process.env.ADYTUM_BIN || "adytum"

// FALLBACK_UNLOCK_TTL_MS — срок жизни маркера, когда CLI не смог
// сообщить свой.
//
// Используется только со старым CLI, который не умеет отдавать
// статус в JSON: без этого fallback плагин отказывал бы в работе
// там, где раньше работал.
const FALLBACK_UNLOCK_TTL_MS = 8 * 60 * 60 * 1000

// unlockPath — путь к маркеру разблокировки.
function unlockPath() {
  return join(homedir(), ".adytum", "unlocked")
}

// unlockTTL_MS — срок жизни маркера в миллисекундах.
//
// TTL задаёт машина, и он меняется: жёстко зашитое значение в
// плагине расходилось с настройкой машины. Источник — отчёт CLI;
// запрос выполняется один раз за вызов.
let cachedTTL = null

async function unlockTTLMS() {
  if (cachedTTL !== null) return cachedTTL
  const result = await runAdytum(["status", "--json"])
  if (result.ok) {
    try {
      const payload = JSON.parse(result.output)
      const ttl = payload?.unlock?.ttl
      if (typeof ttl === "string" && ttl.length > 0) {
        const parsed = parseDurationMS(ttl)
        if (parsed !== null) {
          cachedTTL = parsed
          return cachedTTL
        }
      }
    } catch {
      // битый JSON — как будто ответа не было
    }
  }
  cachedTTL = FALLBACK_UNLOCK_TTL_MS
  return cachedTTL
}

// parseDurationMS переводит длительность Go ("24h0m0s") в миллисекунды.
//
// Формат совпадает с тем, что печатает time.Duration: значение нужно
// разобрать без внешних зависимостей.
function parseDurationMS(text) {
  const pattern = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/
  const match = pattern.exec(text.trim())
  if (!match) return null
  const hours = Number(match[1] || 0)
  const minutes = Number(match[2] || 0)
  const seconds = Number(match[3] || 0)
  if (!hours && !minutes && !seconds) return null
  return hours * 3600000 + minutes * 60000 + seconds * 1000
}

// unlockedState возвращает состояние разблокировки.
//
// Маркер отсутствует, просрочен или нечитаем — операции запрещены.
async function unlockedState() {
  const ttl = await unlockTTLMS()
  const path = unlockPath()
  if (!existsSync(path)) return { unlocked: false, reason: "маркер разблокировки не найден" }
  let stamp
  try {
    stamp = Date.parse(readFileSync(path, "utf8").trim())
  } catch {
    return { unlocked: false, reason: "маркер разблокировки нечитаем" }
  }
  if (Number.isNaN(stamp)) return { unlocked: false, reason: "маркер разблокировки повреждён" }
  const remaining = stamp + ttl - Date.now()
  if (remaining <= 0) return { unlocked: false, reason: "срок разблокировки истёк" }
  return { unlocked: true, remaining }
}

// unlockMessage — текст отказа для агента.
//
// Формулировка совпадает с подсказкой CLI: агент видит одну и ту же
// формулировку независимо от точки входа.
function unlockMessage(reason) {
  return [`adytum: операция отклокирована — ${reason}.`, "🔒 попроси владельца: adytum unlock"].join(" ")
}

// runAdytum выполняет CLI adytum и возвращает stdout.
//
// Значения секретов не попадают в лог: наружу уходит только текст
// команды или её ошибка.
function runAdytum(args) {
  return new Promise((resolve) => {
    execFile(
      ADYTUM_BIN,
      args,
      { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 },
      (error, stdout, stderr) => {
        if (error) {
          const detail = (stderr || error.message || "").trim()
          resolve({ ok: false, output: detail })
          return
        }
        resolve({ ok: true, output: stdout })
      },
    )
  })
}

// getArgs строит аргументы CLI для чтения секрета.
//
// Ключ передаётся точкой в адресе (<project>/<service>.<KEY>): так его
// понимает CLI, и отдельный аргумент молча игнорировался бы.
function getArgs(input) {
  const target = [input.project, input.service].filter(Boolean).join("/")
  const args = ["get", input.key ? `${target}.${input.key}` : target]
  return args
}

// runArgs строит аргументы CLI для запуска команды с секретами.
function runArgs(input) {
  const args = ["run"]
  for (const entry of input.env || []) args.push("--env", entry)
  args.push("--", input.command)
  return args
}

// Описание инструментов для V2-контекста (JSON Schema).
const TOOL_DEFINITIONS = [
  {
    name: "adytum_get",
    description: "Прочитать секрет из репозитория adytum (требует adytum unlock)",
    input: {
      type: "object",
      properties: {
        project: { type: "string", description: "проект, например personal" },
        service: { type: "string", description: "имя сервиса, например github" },
        key: { type: "string", description: "ключ внутри секрета; если не задан — весь файл" },
      },
      required: ["project", "service"],
      additionalProperties: false,
    },
  },
  {
    name: "adytum_run",
    description: "Выполнить команду с секретами в окружении (требует adytum unlock)",
    input: {
      type: "object",
      properties: {
        command: { type: "string", description: "команда для выполнения" },
        env: {
          type: "array",
          items: { type: "string" },
          description: "список секретов в виде project/service",
        },
      },
      required: ["command"],
      additionalProperties: false,
    },
  },
]

// setupV2 регистрирует инструменты в V2-контексте opencode.
async function setupV2(ctx) {
  await ctx.tool.transform((editor) => {
    editor.add({
      ...TOOL_DEFINITIONS[0],
      async execute(input) {
        const state = await unlockedState()
        if (!state.unlocked) return { content: unlockMessage(state.reason) }
        if (!input.project || !input.service) return { content: "adytum: укажите проект и сервис" }
        const result = await runAdytum(getArgs(input))
        return { content: result.ok ? result.output : `adytum: ${result.output}` }
      },
    })
    editor.add({
      ...TOOL_DEFINITIONS[1],
      async execute(input) {
        const state = await unlockedState()
        if (!state.unlocked) return { content: unlockMessage(state.reason) }
        if (!input.command) return { content: "adytum: укажите команду" }
        const result = await runAdytum(runArgs(input))
        return { content: result.ok ? result.output : `adytum: ${result.output}` }
      },
    })
  })
}

// AdytumPlugin — V1-реализация: возвращает карту инструментов.
//
// Схемы аргументов собираются через helper из @opencode-ai/plugin,
// который доступен в окружении opencode.
export const AdytumPlugin = async () => {
  const { tool } = await import("@opencode-ai/plugin")
  return {
    tool: {
      adytum_get: tool({
        description: "Прочитать секрет из репозитория adytum (требует adytum unlock)",
        args: {
          project: tool.schema.string().describe("проект, например personal"),
          service: tool.schema.string().describe("имя сервиса, например github"),
          key: tool.schema.string().optional().describe("ключ внутри секрета; если не задан — весь файл"),
        },
        async execute(args) {
          const state = await unlockedState()
          if (!state.unlocked) return unlockMessage(state.reason)
          if (!args.project || !args.service) return "adytum: укажите проект и сервис"
          const result = await runAdytum(getArgs(args))
          return result.ok ? result.output : `adytum: ${result.output}`
        },
      }),
      adytum_run: tool({
        description: "Выполнить команду с секретами в окружении (требует adytum unlock)",
        args: {
          command: tool.schema.string().describe("команда для выполнения"),
          env: tool.schema.array(tool.schema.string()).optional().describe("список секретов в виде project/service"),
        },
        async execute(args) {
          const state = await unlockedState()
          if (!state.unlocked) return unlockMessage(state.reason)
          if (!args.command) return "adytum: укажите команду"
          const result = await runAdytum(runArgs(args))
          return result.ok ? result.output : `adytum: ${result.output}`
        },
      }),
    },
  }
}

export default process.env.ADYTUM_PLUGIN_API === "v1"
  ? AdytumPlugin
  : {
      id: "adytum",
      server: AdytumPlugin,
      setup: setupV2,
    }
