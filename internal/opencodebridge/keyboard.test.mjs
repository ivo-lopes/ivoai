import assert from "node:assert/strict"
import { EventEmitter } from "node:events"
import test from "node:test"
import { installKeyboardProtocol } from "./assets/keyboard.mjs"

function fixture(kitty = false, tty = true) {
  const renderer = new EventEmitter()
  renderer.capabilities = { kitty_keyboard: kitty }
  const handlers = new Set()
  renderer.prependInputHandler = (handler) => handlers.add(handler)
  renderer.removeInputHandler = (handler) => handlers.delete(handler)
  const writes = []
  const stdout = { isTTY: tty, write: (text) => writes.push(text) }
  return { renderer, stdout, handlers, writes, input: (text) => [...handlers].some((fn) => fn(text)) }
}

for (const previous of [0, 1, 2, 3]) {
  test(`negotiates xterm Shift+Enter and restores mode ${previous}`, () => {
    const f = fixture()
    const dispose = installKeyboardProtocol(f.renderer, f.stdout, "xterm-256color")
    assert.deepEqual(f.writes, ["\x1b[?4m"])
    assert.equal(f.input("regular input"), false)
    assert.equal(f.input("\r"), false)
    assert.equal(f.input("\x1b[13;2u"), false)
    assert.equal(f.input(`\x1b[>4;${previous}m`), true)
    if (previous !== 2) assert.equal(f.writes.at(-1), "\x1b[>4;2m")
    dispose()
    dispose()
    assert.equal(f.handlers.size, 0)
    assert.equal(f.renderer.listenerCount("capabilities"), 0)
    assert.deepEqual(f.writes, previous === 2 ? ["\x1b[?4m"] : ["\x1b[?4m", "\x1b[>4;2m", `\x1b[>4;${previous}m`])
  })
}

test("Kitty, non-TTY and unsupported terminal are not changed", () => {
  for (const [kitty, tty, term] of [[true, true, "xterm-kitty"], [false, false, "xterm"], [false, true, "dumb"]]) {
    const f = fixture(kitty, tty)
    installKeyboardProtocol(f.renderer, f.stdout, term)()
    assert.deepEqual(f.writes, [])
    assert.equal(f.handlers.size, 0)
  }
})

test("unsupported query does not assume support or swallow input", () => {
  const f = fixture()
  const dispose = installKeyboardProtocol(f.renderer, f.stdout, "xterm")
  assert.equal(f.input("\x1b[>4;999m"), false)
  dispose()
  assert.deepEqual(f.writes, ["\x1b[?4m"])
})

test("late Kitty negotiation takes priority over legacy mode", () => {
  const f = fixture()
  const dispose = installKeyboardProtocol(f.renderer, f.stdout, "xterm")
  f.input("\x1b[>4;1m")
  f.renderer.capabilities.kitty_keyboard = true
  f.renderer.emit("capabilities")
  dispose()
  assert.deepEqual(f.writes, ["\x1b[?4m", "\x1b[>4;2m", "\x1b[>4;1m"])
})
