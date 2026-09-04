export const IVOAISessionBridge = async () => ({
  "chat.headers": async (input, output) => {
    if (!input?.sessionID) return
    output.headers["X-IVOAI-OpenCode-Session"] = input.sessionID
    if (input?.message?.id) output.headers["X-IVOAI-OpenCode-Message"] = input.message.id
  },
})
