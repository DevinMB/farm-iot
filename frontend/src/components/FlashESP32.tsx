import { useState } from 'react'
import { ESPLoader, Transport } from 'esptool-js'

const BOARDS = {
  heltec_v3: {
    label: 'Heltec WiFi LoRa 32 V3',
    parts: [
      { path: '/firmware/heltec_v3/bootloader.bin', offset: 0x00000 },
      { path: '/firmware/heltec_v3/partitions.bin', offset: 0x08000 },
      { path: '/firmware/heltec_v3/firmware.bin',   offset: 0x10000 },
    ],
  },
} as const

type BoardKey = keyof typeof BOARDS
type Stage = 'idle' | 'connecting' | 'flashing' | 'configuring' | 'done' | 'error'

interface Props {
  nodeId: string
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnySerialPort = any

function sleep(ms: number) {
  return new Promise<void>(r => setTimeout(r, ms))
}

function toByteString(buf: ArrayBuffer): string {
  return Array.from(new Uint8Array(buf), b => String.fromCharCode(b)).join('')
}

export default function FlashESP32({ nodeId }: Props) {
  const [board, setBoard]       = useState<BoardKey>('heltec_v3')
  const [stage, setStage]       = useState<Stage>('idle')
  const [progress, setProgress] = useState(0)
  const [log, setLog]           = useState<string[]>([])
  const [error, setError]       = useState<string | null>(null)

  function addLog(msg: string) {
    setLog(prev => [...prev.slice(-20), msg])
  }

  async function flash() {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (!(navigator as any).serial) {
      setError('Web Serial not supported — use Chrome or Edge.')
      setStage('error')
      return
    }

    setError(null)
    setLog([])
    setProgress(0)
    setStage('connecting')

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let port: AnySerialPort = null

    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      port = await (navigator as any).serial.requestPort()

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const transport = new Transport(port as any, false)
      const terminal = {
        clean:     () => {},
        writeLine: (line: string) => addLog(line),
        write:     (s: string)    => { if (s.trim()) addLog(s.trim()) },
      }
      const loader = new ESPLoader({
        transport,
        baudrate:    921600,
        romBaudrate: 115200,
        terminal,
      })

      addLog('Connecting to ESP32…')
      const chipName = await loader.main()
      addLog(`Connected: ${chipName} ✓`)

      // Fetch all firmware parts
      addLog('Downloading firmware…')
      const boardDef = BOARDS[board]
      const fileArray = await Promise.all(
        boardDef.parts.map(async ({ path, offset }) => {
          const res = await fetch(path)
          if (!res.ok) throw new Error(`Could not fetch ${path} (${res.status})`)
          return { data: toByteString(await res.arrayBuffer()), address: offset }
        })
      )
      addLog(`Firmware ready (${fileArray.reduce((s, f) => s + f.data.length, 0)} bytes)`)

      setStage('flashing')
      await loader.writeFlash({
        fileArray,
        flashSize:  'keep',
        flashMode:  'keep',
        flashFreq:  'keep',
        eraseAll:   false,
        compress:   true,
        reportProgress: (_fi: number, written: number, total: number) => {
          setProgress(total > 0 ? Math.round((written / total) * 100) : 0)
        },
      })
      addLog('Flash complete ✓')
      setProgress(100)

      // Reset board out of bootloader
      await loader.hardReset()
      await transport.disconnect()

      // Wait for firmware to boot then send config over serial
      setStage('configuring')
      addLog('Waiting for ESP32 to boot…')
      await sleep(5000)

      await port.open({ baudRate: 115200 })
      addLog('Sending node_id…')

      const writer = port.writable.getWriter()
      const enc = new TextEncoder()
      await writer.write(enc.encode(`set node_id ${nodeId}\n`))
      await sleep(400)
      await writer.write(enc.encode(`start\n`))
      await sleep(400)
      writer.releaseLock()
      await port.close()

      addLog('Done — ESP32 will reboot and start transmitting!')
      setStage('done')

    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'NotFoundError') {
        setStage('idle')
        return
      }
      const msg = err instanceof Error ? err.message : String(err)
      setError(msg)
      setStage('error')
      try { if (port) await port.close() } catch { /* ignore */ }
    }
  }

  const busy = stage === 'connecting' || stage === 'flashing' || stage === 'configuring'

  return (
    <div className="flash-esp32">
      <div className="flash-header">
        <span className="flash-title">Flash ESP32</span>
        <select
          className="form-select"
          value={board}
          onChange={e => setBoard(e.target.value as BoardKey)}
          disabled={busy}
          style={{ width: 'auto', fontSize: '0.8rem' }}
        >
          {(Object.keys(BOARDS) as BoardKey[]).map(k => (
            <option key={k} value={k}>{BOARDS[k].label}</option>
          ))}
        </select>
      </div>

      {stage === 'idle' || stage === 'error' ? (
        <button className="btn btn-primary" onClick={flash} style={{ width: '100%' }}>
          Connect &amp; Flash
        </button>
      ) : stage === 'done' ? (
        <div className="flash-done">✓ ESP32 configured and transmitting</div>
      ) : (
        <div className="flash-progress-wrap">
          <div className="flash-stage-label">
            {stage === 'connecting'  && 'Connecting…'}
            {stage === 'flashing'    && `Flashing… ${progress > 0 ? `${progress}%` : ''}`}
            {stage === 'configuring' && 'Configuring…'}
          </div>
          <div className="flash-bar-bg">
            <div
              className="flash-bar-fill"
              style={{
                width: stage === 'flashing'    ? `${progress}%`
                     : stage === 'configuring' ? '100%'
                     : '8%'
              }}
            />
          </div>
        </div>
      )}

      {error && <div className="flash-error">{error}</div>}
      <div className="flash-hint">Chrome / Edge only · ESP32 connected via USB</div>

      {log.length > 0 && (
        <div className="flash-log">
          {log.map((line, i) => <div key={i}>{line}</div>)}
        </div>
      )}
    </div>
  )
}
