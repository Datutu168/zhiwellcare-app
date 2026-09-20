import { mkdir, readFile, stat, writeFile } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
// 发布清单只引用公开资产，签名私钥始终由 Tauri 构建阶段在仓库外使用。
const options = parseArgs(process.argv.slice(2))
const release = JSON.parse(await readFile(resolve(root, 'release-version.json'), 'utf8'))

// 兼容原有调用方式：不带 --artifact 时按 Windows NSIS 安装包的默认路径取（03-pack-windows.bat 就是这么调的）。
// macOS 用 --artifact 指向 bundle/macos/ZhiWellCare.app.tar.gz（createUpdaterArtifacts 生成的更新包）。
const artifactInputs = options.artifact?.length
  ? options.artifact
  : [`src-tauri/target/release/bundle/nsis/ZhiWellCare_${release.productVersion}_x64-setup.exe`]
const signatureInputs = options.signature ?? []

const notes = options.notes ? (await readFile(resolve(root, options.notes), 'utf8')).trim() : ''
const publishedAt = options.publishedAt ?? new Date().toISOString()
const output = resolve(root, options.output ?? 'release-output/latest.json')

// 下载地址前缀：UPDATE_ASSET_BASE 可指向自有域名 / COS / CDN；未配置时回退 GitHub Releases。
const assetBase = (process.env.UPDATE_ASSET_BASE ?? 'https://github.com/NovSyang/zhiwellcare-app/releases/download').replace(/\/+$/, '')

const platforms = {}
for (const [index, artifactInput] of artifactInputs.entries()) {
  const artifactPath = resolve(root, artifactInput)
  const signatureInput = signatureInputs[index] ?? `${artifactInput}.sig`
  // 找不到产物时给出可执行的提示，而不是抛裸 ENOENT（打包脚本误点菜单时最容易遇到）。
  const artifactStat = await stat(artifactPath).catch(() => null)
  if (!artifactStat) {
    throw new Error(`找不到更新包：${artifactInput}\n  请先完成构建，或用 --artifact 指定实际的更新包路径。`)
  }
  if (artifactStat.size <= 0) throw new Error(`更新包为空：${artifactInput}`)
  const signature = (await readFile(resolve(root, signatureInput), 'utf8').catch(() => '')).trim()
  if (!signature) throw new Error(`读不到 Tauri 签名文件（内容为空或文件不存在）：${signatureInput}\n  自动更新需要配置 TAURI_SIGNING_PRIVATE_KEY；暂不做自动更新时可把 tauri.conf.json 的 createUpdaterArtifacts 设为 false。`)

  // 平台键：优先用显式 --platform（可逗号分隔多个，通用包会同时登记 aarch64 与 x86_64），
  // 否则按产物路径/扩展名推断。
  const keys = options.platform?.[index]
    ? options.platform[index].split(',').map((key) => key.trim()).filter(Boolean)
    : inferPlatformKeys(artifactInput)
  if (!keys.length) throw new Error(`无法判断更新包的平台，请显式传 --platform：${artifactInput}`)

  const artifactName = basename(artifactPath)
  const url = `${assetBase}/v${release.productVersion}/${encodeURIComponent(artifactName)}`
  for (const key of keys) {
    platforms[key] = { signature, url }
  }
}

// 与既有清单合并：Windows 与 macOS 往往分两次构建，各自只知道自己那一个产物，
// 合并后指向同一份 latest.json，客户端按自己的平台键取值。
// 版本号不同的旧清单直接丢弃（避免把上个版本的自更新地址留在新版本清单里）。
const existing = await readManifest(output)
const mergedPlatforms = existing && existing.version === release.productVersion
  ? { ...existing.platforms, ...platforms }
  : platforms

const manifest = {
  version: release.productVersion,
  notes,
  pub_date: publishedAt,
  platforms: mergedPlatforms,
}

await mkdir(dirname(output), { recursive: true })
await writeFile(output, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8')
console.log(`Tauri 更新清单已生成：${output}`)
console.log(`  平台键：${Object.keys(mergedPlatforms).join(', ')}`)

async function readManifest(path) {
  try {
    const raw = await readFile(path, 'utf8')
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && parsed.platforms ? parsed : null
  } catch {
    return null
  }
}

/** 按产物路径/扩展名推断 Tauri 更新清单的平台键。 */
function inferPlatformKeys(input) {
  const value = input.toLowerCase()
  if (value.endsWith('.app.tar.gz')) {
    // Tauri 在 macOS 上的更新包固定叫 <productName>.app.tar.gz，名字里没有架构信息：
    // 架构藏在目录里（target/<triple>/release/bundle/macos/ 或 target/universal-apple-darwin/...），
    // 因此先看路径，再看是否就在 Mac 上执行（本机构建默认就是本机架构）。
    if (value.includes('universal')) return ['darwin-aarch64', 'darwin-x86_64']
    if (value.includes('aarch64') || value.includes('arm64')) return ['darwin-aarch64']
    if (value.includes('x86_64') || value.includes('x64')) return ['darwin-x86_64']
    if (process.platform === 'darwin') {
      return [process.arch === 'arm64' ? 'darwin-aarch64' : 'darwin-x86_64']
    }
    return []
  }
  if (value.endsWith('.exe') || value.endsWith('.nsis.zip')) return ['windows-x86_64']
  return []
}

// 支持重复传参（多个 --artifact / --signature / --platform），因此这里按累积处理而不是覆盖。
function parseArgs(args) {
  const result = {}
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index]?.replace(/^--/, '').replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())
    if (!key || index + 1 >= args.length) throw new Error(`发布参数无效：${args[index] ?? ''}`)
    const value = args[index + 1]
    if (key === 'artifact' || key === 'signature' || key === 'platform') {
      result[key] = [...(result[key] ?? []), value]
    } else {
      result[key] = value
    }
  }
  return result
}
