import { describe, expect, it } from 'vitest'
import { WebUnsupportedSensorTransport } from '../src/platform/web/WebUnsupportedSensorTransport'

describe('WebUnsupportedSensorTransport', () => {
  it('浏览器 BLE 入口返回可读的终止错误，不访问任何原生桥接', async () => {
    const transport = new WebUnsupportedSensorTransport()
    await expect(transport.scan()).rejects.toMatchObject({
      code: 'unsupported',
      message: '当前浏览器环境不支持训练设备 BLE，请在桌面端应用（Windows / macOS）或 Android 应用中操作。',
    })
  })
})
