// XTerm XTQMODKEYS/XTMODKEYS fallback for terminals without Kitty keyboard.
// Query before changing a mode, and restore exactly the reported prior value.
// Never consume arbitrary input or modify terminal configuration files.
export function installKeyboardProtocol(renderer, stdout, term) {
  if (!stdout?.isTTY || !/^xterm(?:-|$)/.test(term || "") || renderer.capabilities?.kitty_keyboard) return () => {}
  let previous
  let stopped = false
  let changed = false
  const restore = () => {
    if (!changed) return
    changed = false
    stdout.write(`\x1b[>4;${previous}m`)
  }
  const input = (sequence) => {
    if (stopped || previous !== undefined) return false
    const match = /^\x1b\[>4;([0-3])m$/.exec(sequence)
    if (!match) return false
    previous = Number(match[1])
    renderer.removeInputHandler(input)
    if (!renderer.capabilities?.kitty_keyboard && previous !== 2) {
      changed = true
      stdout.write("\x1b[>4;2m")
    }
    return true
  }
  const capabilities = () => {
    if (renderer.capabilities?.kitty_keyboard) restore()
  }
  renderer.prependInputHandler(input)
  renderer.on("capabilities", capabilities)
  stdout.write("\x1b[?4m")
  return () => {
    if (stopped) return
    stopped = true
    renderer.removeInputHandler(input)
    renderer.off("capabilities", capabilities)
    restore()
  }
}
