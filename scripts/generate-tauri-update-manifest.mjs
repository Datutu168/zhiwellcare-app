import { mkdir, readFile, stat, writeFile } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
// 发布清单只引用公开资产，签名私钥始终由 Tauri 构建阶段在仓库外使用。
const options = parseArgs(process.argv.slice(2))
const release = JSON.parse(await readFile(resolve(root, 'release-version.json'), 'utf8'))
// Tauri 2 的 createUpdaterArtifacts=true 会直接为 NSIS 安装程序生成同名 .sig 文件。
const artifactInput = options.artifact ?? `src-tauri/target/release/bundle/nsis/ZhiWellCare_${release.productVersion}_x64-setup.exe`
const artifactPath = resolve(root, artifactInput)
const signaturePath = resolve(root, options.signature ?? `${artifactInput}.sig`)
const artifactStat = await stat(artifactPath)
if (artifactStat.size <= 0) throw new Error('Windows 更新包为空。')
const signature = (await readFile(signaturePath, 'utf8')).trim()
if (!signature) throw new Error('Tauri .sig 文件为空。')
const notes = options.notes ? (await readFile(resolve(root, options.notes), 'utf8')).trim() : ''
const publishedAt = options.publishedAt ?? new Date().toISOString()
const output = resolve(root, options.output ?? 'release-output/latest.json')
const artifactName = basename(artifactPath)

// 下载地址前缀：UPDATE_ASSET_BASE 可指向自有域名 / COS / CDN；未配置时回退 GitHub Releases。
const assetBase = (process.env.UPDATE_ASSET_BASE ?? 'https://github.com/NovSyang/zhiwellcare-app/releases/download').replace(/\/+$/, '')

const manifest = {
  version: release.productVersion,
  notes,
  pub_date: publishedAt,
  platforms: {
    'windows-x86_64': {
      signature,
      url: `${assetBase}/v${release.productVersion}/${encodeURIComponent(artifactName)}`,
    },
  },
}

await mkdir(dirname(output), { recursive: true })
await writeFile(output, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8')
console.log(`Tauri 更新清单已生成：${output}`)

function parseArgs(args) {
  const result = {}
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index]?.replace(/^--/, '').replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())
    if (!key || index + 1 >= args.length) throw new Error(`发布参数无效：${args[index] ?? ''}`)
    result[key] = args[index + 1]
  }
  return result
}
