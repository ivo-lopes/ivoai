// One startup snapshot supports bridges without an operational stream.
export async function initialOperationalStatus(bridge, token, signal, fetcher = fetch) {
  const response = await fetcher(bridge + "/status", {
    headers: {Authorization: "Bearer " + token}, signal,
  })
  if (!response.ok || !response.body) throw new Error("operational status unavailable")
  const reader = response.body.getReader(), decoder = new TextDecoder()
  let body = "", bytes = 0
  try {
    for (;;) {
      const {value, done} = await reader.read()
      if (done) break
      bytes += value.byteLength
      if (bytes > 524288) throw new Error("operational status exceeded limit")
      body += decoder.decode(value, {stream: true})
    }
    body += decoder.decode()
    const status = JSON.parse(body)
    if (!status || typeof status !== "object" || Array.isArray(status)) throw new Error("invalid operational status")
    return status
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
}

// Bounded operational SSE decoder. Never consumes provider/tool transcripts.
export async function readOperationalStream(stream, consume) {
  const reader = stream.getReader(), decoder = new TextDecoder()
  let pending = ""
  try {
    for (;;) {
      const {value, done} = await reader.read()
      if (done) return
      pending += decoder.decode(value, {stream: true}).replace(/\r\n/g, "\n")
      if (pending.length > 524288) throw new Error("operational frame exceeded limit")
      let end
      while ((end = pending.indexOf("\n\n")) >= 0) {
        const frame = pending.slice(0, end); pending = pending.slice(end + 2)
        if (!frame.split("\n").includes("event: operational")) continue
        const data = frame.split("\n").filter(x => x.startsWith("data:")).map(x => x.slice(5).trimStart()).join("\n")
        const value = JSON.parse(data)
        const replay = value.replay
        if (replay?.schema_version !== 1 || !Number.isSafeInteger(replay.latest_sequence) || !Array.isArray(replay.events) || replay.events.length > 128 || !value.status || replay.session_id !== value.status.session_id) throw new Error("invalid operational envelope")
        await consume(value)
      }
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
}

export function mergeActivity(previous, frame) {
  const replay = frame.replay
  const retained = previous.session_id === replay.session_id && !replay.gap ? (previous.activity || []) : []
  const events = new Map(retained.map(event => [event.sequence, event]))
  for (const event of replay.events) if (Number.isSafeInteger(event.sequence)) events.set(event.sequence, event)
  return {...frame.status, activity: [...events.values()].sort((a,b) => a.sequence-b.sequence).slice(-32), activity_gap: replay.gap || (previous.session_id === replay.session_id && previous.activity_gap === true)}
}

// Clock injection keeps connection/heartbeat timeout tests instantaneous.
export function createStreamWatchdog(onStale, clock = {setTimeout, clearTimeout}) {
  let timer, disposed = false
  const arm = delay => {
    if (disposed) return
    clock.clearTimeout(timer)
    timer = clock.setTimeout(() => {
      if (disposed) return
      disposed = true
      onStale()
    }, delay)
  }
  arm(5000)
  return {
    touch() { arm(45000) },
    dispose() { disposed = true; clock.clearTimeout(timer) },
  }
}
