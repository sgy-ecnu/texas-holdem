// 受控本地 provider server（仅用于「按座位选模型」真实链路验收）。
// 它不是真实模型：只统计出站请求数并返回 OpenAI 兼容的最小合法响应，
// 用来证明「mock 座位不出站、真实模型座位才出站」以及额度语义。
// 绝不读取任何凭据；Authorization 头即使带占位值也不记录。
import { createServer } from 'node:http'

const PORT = Number(process.env.PROVIDER_PORT || 4390)
let count = 0

function sendJSON(res, status, payload) {
  const body = JSON.stringify(payload)
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': Buffer.byteLength(body) })
  res.end(body)
}

const server = createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/__count') {
    sendJSON(res, 200, { count })
    return
  }
  if (req.method === 'POST' && req.url === '/__reset') {
    count = 0
    res.writeHead(204)
    res.end()
    return
  }
  if (req.method === 'POST' && (req.url === '/chat/completions' || req.url === '/v1/chat/completions')) {
    let raw = ''
    req.on('data', (chunk) => (raw += chunk))
    req.on('end', () => {
      count++
      let kind = 'call'
      try {
        const body = JSON.parse(raw)
        const content = (body.messages && body.messages[0] && body.messages[0].content) || ''
        const marker = 'Observation:'
        const at = content.lastIndexOf(marker)
        if (at >= 0) {
          const obs = JSON.parse(content.slice(at + marker.length).trim())
          kind = obs && obs.Legal && obs.Legal.CanCheck ? 'check' : 'call'
        }
      } catch {
        // 解析失败也计一次出站请求，返回 fold 让牌局继续。
        kind = 'fold'
      }
      sendJSON(res, 200, {
        choices: [{ message: { content: JSON.stringify({ kind }) } }],
        usage: { prompt_tokens: 1, completion_tokens: 1 },
      })
    })
    return
  }
  res.writeHead(404)
  res.end()
})

server.listen(PORT, '127.0.0.1', () => {
  process.stdout.write(`provider-ready port=${PORT}\n`)
})
