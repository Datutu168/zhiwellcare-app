import { createHash } from 'node:crypto'
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
// 清单中的摘要和大小由最终签名 APK 直接计算，避免人工填写错误。
const options = parseArgs(process.argv.slice(2))
const release = JSON.parse(await readFile(resolve(root, 'release-version.json'), 'utf8'))
const apkInput = options.apk ?? 'android/app/build/outputs/apk/release/app-release.apk'
const apkPath = resolve(root, apkInput)
const apkBytes = await readFile(apkPath)
const apkStat = await stat(apkPath)
const apkName = basename(apkPath)
const sha256 = createHash('sha256').update(apkBytes).digest('hex')
const notes = await readNotes(options.notes)
const publishedAt = options.publishedAt ?? new Date().toISOString()
const output = resolve(root, options.output ?? 'release-output/android-latest.json')

// 下载地址前缀：UPDATE_ASSET_BASE 可指向自有域名 / COS / CDN；未配置时回退 GitHub Releases。
const assetBase = (process.env.UPDATE_ASSET_BASE ?? 'https://github.com/NovSyang/zhiwellcare-app/releases/download').replace(/\/+$/, '')

const manifest = {
  schemaVersion: 1,
  version: release.productVersion,
  displayVersion: release.displayVersion,
  versionCode: release.android.versionCode,
  publishedAt,
  notes,
  apk: {
    url: `${assetBase}/v${release.productVersion}/${encodeURIComponent(apkName)}`,
    sha256,
    size: apkStat.size,
  },
}

await mkdir(dirname(output), { recursive: true })
await writeFile(output, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8')
await writeFile(resolve(dirname(output), 'SHA256SUMS.txt'), `${sha256}  ${apkName}\n`, 'utf8')
console.log(`Android 更新清单已生成：${output}`)

async function readNotes(path) {
  if (!path) return []
  return (await readFile(resolve(root, path), 'utf8')).split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
}

function parseArgs(args) {
  const result = {}
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index]?.replace(/^--/, '').replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())
    if (!key || index + 1 >= args.length) throw new Error(`发布参数无效：${args[index] ?? ''}`)
    result[key] = args[index + 1]
  }
  return result
}
