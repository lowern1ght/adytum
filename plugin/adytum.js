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

// UNLOCK_TTL_MS — срок жизни маркера разблокировки в миллисекундах.
// Должен совпадать со сроком в store.UnlockTTL (8 часов).
const UNLOCK_TTL_MS = 8 * 60 * 60 * 1000

// ADYTUM_BIN — исполняемый файл CLI. Переопределяется переменной
// окружения, иначе используется adytum из PATH.
const ADYTUM_BIN = process.env.ADYTUM_BIN || "adytum"

// unlockPath — путь к маркеру разблокировки.
function unlockPath() {
  return join(homedir(), ".adytum", "unlocked")
}

// unlockedState возвращает состояние разблокировки.
//
// Маркер отсутствует, просрочен или нечитаем — операции запрещены.
function unlockedState() {
  const path = unlockPath()
  if (!existsSync(path)) return { unlocked: false, reason: "маркер разблокировки не найден" }
  let stamp
  try {
    stamp = Date.parse(readFileSync(path, "utf8").trim())
  } catch {
    return { unlocked: false, reason: "маркер разблокировки нечитаем" }
  }
  if (Number.isNaN(stamp)) return { unlocked: false, reason: "маркер разблокировки повреждён" }
  const remaining = stamp + UNLOCK_TTL_MS - Date.now()
  if (remaining <= 0) return { unlocked: false, reason: "срок разблокировки истёк" }
  return { unlocked: true, remaining }
}

// unlockMessage — текст отказа для агента.
function unlockMessage(reason) {
  return [
    `adytum: операция отклонена — ${reason}.`,
    "Попроси владельца выполнить `adytum unlock` и повторить запрос.",
  ].join(" ")
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
function getArgs(input) {
  const target = [input.project, input.service].filter(Boolean).join("/")
  const args = ["get", target]
  if (input.key) args.push(input.key)
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
        const state = unlockedState()
        if (!state.unlocked) return { content: unlockMessage(state.reason) }
        if (!input.project || !input.service) return { content: "adytum: укажите проект и сервис" }
        const result = await runAdytum(getArgs(input))
        return { content: result.ok ? result.output : `adytum: ${result.output}` }
      },
    })
    editor.add({
      ...TOOL_DEFINITIONS[1],
      async execute(input) {
        const state = unlockedState()
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
          const state = unlockedState()
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
          const state = unlockedState()
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
