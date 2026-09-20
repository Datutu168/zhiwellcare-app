/**
 * 管理后台冒烟测试：用 jsdom 真实挂载页面 + mock 后端响应，验证渲染、交互与请求载荷。
 *
 * 文件名刻意用 `.smoke.ts` 而不是 `.test.ts`：
 * 仓库根的 `npm test`（vitest 默认 include `**\/*.test.ts`）不会收集到它，
 * 避免把 admin 侧的 jsdom/环境假设带进根套件（根 vite.config.ts 里另有 admin/** 排除兜底）。
 *
 * 运行：cd admin && npm run smoke
 * （等价：cd admin && npx vitest run --config vitest.config.ts；
 *   环境与 include 见 admin/vitest.config.ts，本套件不产生任何构建产物。）
 */
import { createApp, h, type App } from 'vue'
import { RouterView, createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import { createHash } from 'node:crypto'
import { it } from 'vitest'

import { adminToken } from '../src/api/token'
import { useAdminPermissionsStore } from '../src/stores/adminPermissions'
import { apiErrorText } from '../src/api/admin'
import { ApiError } from '../src/api/http'
import RolesView from '../src/views/system/RolesView.vue'
import ConfigView from '../src/views/system/ConfigView.vue'
import WhitelistView from '../src/views/devices/WhitelistView.vue'
import AssetsView from '../src/views/assets/AssetsView.vue'
import UsersView from '../src/views/users/UsersView.vue'

/* ------------------------------------------------------------------ *
 * mock fetch
 * ------------------------------------------------------------------ */

interface Ctx {
  method: string
  url: string
  path: string
  query: URLSearchParams
  body: any
  raw: any
}
interface MockResult {
  status?: number
  code?: number
  message?: string
  data?: unknown
  text?: string
}
type Handler = (ctx: Ctx) => MockResult | Promise<MockResult>

const calls: Ctx[] = []
let handler: Handler = () => ({ status: 404, message: '未配置 mock' })

const ok = (data: unknown): MockResult => ({ data })
const fail = (status: number, message: string): MockResult => ({ status, code: status, message })

function installFetch(): void {
  const g = globalThis as unknown as Record<string, unknown>
  g.fetch = async (input: unknown, init?: { method?: string; body?: unknown }) => {
    const url = typeof input === 'string' ? input : String(input)
    const method = (init?.method ?? 'GET').toUpperCase()
    let body: any = init?.body ?? null
    if (typeof body === 'string') {
      try {
        body = JSON.parse(body)
      } catch {
        /* 保留原始字符串 */
      }
    }
    const apiMatch = /\/api\/v1(\/[^?]*)/.exec(url)
    const ctx: Ctx = {
      method,
      url,
      path: apiMatch ? apiMatch[1] : url,
      query: new URLSearchParams(url.includes('?') ? url.slice(url.indexOf('?') + 1) : ''),
      body,
      raw: init?.body ?? null,
    }
    calls.push(ctx)
    const result = await handler(ctx)
    const status = result.status ?? 200
    const text =
      result.text !== undefined
        ? result.text
        : JSON.stringify({ code: result.code ?? 0, message: result.message ?? '', data: result.data ?? null })
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => JSON.parse(text),
      text: async () => text,
    } as unknown as Response
  }
}

function findCall(method: string, pathPart: string): Ctx | undefined {
  return calls.find((c) => c.method === method && c.path.includes(pathPart))
}

/* ------------------------------------------------------------------ *
 * 断言 & DOM 助手
 * ------------------------------------------------------------------ */

let passed = 0
const failures: string[] = []

function check(name: string, condition: boolean, extra?: string): void {
  if (condition) {
    passed += 1
    console.log(`  OK   ${name}`)
  } else {
    failures.push(name)
    console.log(`  FAIL ${name}${extra ? ` — ${extra}` : ''}`)
  }
}

const flush = async (times = 8): Promise<void> => {
  for (let i = 0; i < times; i += 1) await new Promise((resolve) => setTimeout(resolve, 0))
}

const mounted: App[] = []

async function mountView(component: unknown): Promise<HTMLElement> {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const app = createApp(component as never)
  app.use(createPinia())
  app.use(ElementPlus)
  app.mount(container)
  mounted.push(app)
  await flush()
  return container
}

function resetDom(): void {
  while (mounted.length) mounted.pop()?.unmount()
  document.body.innerHTML = ''
  calls.length = 0
}

function text(root: ParentNode = document.body): string {
  return (root.textContent ?? '').replace(/\s+/g, ' ')
}

function clickText(needle: string, root: ParentNode = document.body): boolean {
  const nodes = Array.from(
    root.querySelectorAll('button, .el-button, .el-tag, .el-menu-item, .el-select__wrapper, .el-tabs__item'),
  )
  const target = nodes.find((node) => (node.textContent ?? '').replace(/\s+/g, '').includes(needle.replace(/\s+/g, '')))
  if (!target) return false
  ;(target as HTMLElement).click()
  return true
}

function countsByText(needle: string, root: ParentNode = document.body): number {
  return Array.from(root.querySelectorAll('button, .el-button')).filter((node) =>
    (node.textContent ?? '').replace(/\s+/g, '').includes(needle.replace(/\s+/g, '')),
  ).length
}

function setInput(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  input.value = value
  input.dispatchEvent(new (globalThis as any).Event('input', { bubbles: true }))
}

function inputByLabel(label: string, root: ParentNode = document.body): HTMLInputElement | HTMLTextAreaElement | null {
  const items = Array.from(root.querySelectorAll('.el-form-item'))
  const item = items.find((it) => ((it.querySelector('.el-form-item__label')?.textContent ?? '').trim()).startsWith(label))
  if (!item) return null
  return item.querySelector('input, textarea') as HTMLInputElement | null
}

/* ------------------------------------------------------------------ *
 * mock 数据
 * ------------------------------------------------------------------ */

const PERMISSION_CATALOG = [
  { code: 'device:read', name: '设备查看', group: '设备', description: '查看设备型号' },
  { code: 'device:write', name: '设备编辑', group: '设备', description: '新增/编辑设备型号' },
  { code: 'role:read', name: '角色查看', group: '系统', description: '查看角色列表' },
  { code: 'role:write', name: '角色编辑', group: '系统', description: '新增/修改角色' },
]

const ROLE_LIST = [
  {
    code: 'admin',
    name: '超级管理员',
    description: '内置角色',
    builtin: true,
    permissions: ['device:read', 'device:write', 'role:read', 'role:write'],
    userCount: 1,
  },
  {
    code: 'operator',
    name: '运营专员',
    description: '设备与游戏维护',
    builtin: false,
    permissions: ['device:read'],
    userCount: 2,
  },
]

/* ------------------------------------------------------------------ *
 * 各检查项
 * ------------------------------------------------------------------ */

async function checkPermissionStore(): Promise<void> {
  console.log('\n[1] 权限 store（通配 / 未加载放行 / 失败降级）')
  handler = (ctx) => {
    if (ctx.path === '/admin/me/permissions') return ok({ roles: ['operator'], permissions: ['device:*'] })
    return fail(404, 'not mocked')
  }
  const store = useAdminPermissionsStore(createPinia())
  await store.load()
  check('加载成功 roles', store.roles.join(',') === 'operator')
  check('前缀通配 device:* 命中 device:whitelist:read', store.can('device:whitelist:read'))
  check('前缀通配不误命中 role:read', !store.can('role:read'))
  check('allows 任一命中即通过', store.allows(['role:read', 'device:read']))
  check('allows 全不命中即拒绝', !store.allows(['role:read', 'config:read']))

  const store2 = useAdminPermissionsStore(createPinia())
  handler = () => fail(500, '后端炸了')
  await store2.load()
  check('加载失败 loaded=false', !store2.loaded)
  check('加载失败时 allows 放行（后端 403 兜底）', store2.allows(['role:read']))
  check('加载失败时记录错误信息', store2.error.includes('后端炸了'))

  const store3 = useAdminPermissionsStore(createPinia())
  handler = () => ok({ roles: ['admin'], permissions: [] })
  await store3.load()
  check('role=admin 视为全权限', store3.can('任意:码') && store3.isSuperAdmin)
}

function checkErrorText(): void {
  console.log('\n[2] 错误文案映射')
  check('403 文案友好', apiErrorText(new ApiError(403, '')).includes('403'))
  check('404 提示后端未就绪', apiErrorText(new ApiError(404, '')).includes('尚未上线'))
  check('409 提示内置/占用', apiErrorText(new ApiError(409, '')).includes('内置角色'))
  check('0 提示后端未启动', apiErrorText(new ApiError(0, '')).includes('后端'))
  check('非 ApiError 取 message', apiErrorText(new Error('自定义错误')) === '自定义错误')
}

async function checkRouterGuardAndMenu(): Promise<void> {
  console.log('\n[3] 路由守卫 + 菜单按权限显隐')
  handler = (ctx) => {
    if (ctx.path === '/admin/me/permissions') return ok({ roles: ['operator'], permissions: ['role:read'] })
    if (ctx.path === '/admin/roles') return ok({ items: ROLE_LIST })
    if (ctx.path === '/admin/permissions') return ok({ items: PERMISSION_CATALOG })
    return fail(404, 'not mocked')
  }
  adminToken.set('smoke-token')
  const { router } = await import('../src/router')

  await router.push('/system/roles')
  await flush()
  check('有 role:read 可进入 /system/roles', router.currentRoute.value.path === '/system/roles')

  await router.push('/system/config')
  await flush()
  check('无 config 权限跳转 /forbidden', router.currentRoute.value.path === '/forbidden')
  check('forbidden 带 from 参数', String(router.currentRoute.value.query.from ?? '').includes('/system/config'))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const app = createApp({ render: () => h(RouterView) })
  app.use(createPinia())
  app.use(router)
  app.use(ElementPlus)
  await router.push('/dashboard')
  app.mount(container)
  mounted.push(app)
  await flush()
  const menuText = text(container.querySelector('.menu') ?? container)
  check('菜单显示「角色权限」', menuText.includes('角色权限'))
  check('菜单隐藏「配置中心」', !menuText.includes('配置中心'))
  check('菜单隐藏「用户管理」', !menuText.includes('用户管理'))
  check('菜单显示「仪表盘」', menuText.includes('仪表盘'))

  handler = () => fail(500, '权限接口炸了')
  const store = useAdminPermissionsStore(createPinia())
  await store.load()
  check('权限接口失败时菜单降级全显示（后端 403 兜底）', store.allows(['role:read']))
  adminToken.clear()
}

async function checkRolesView(): Promise<void> {
  console.log('\n[4] 角色权限页：列表 + 按组勾选新建')
  resetDom()
  let created: any = null
  handler = (ctx) => {
    if (ctx.path === '/admin/roles' && ctx.method === 'GET') return ok({ items: ROLE_LIST })
    if (ctx.path === '/admin/permissions') return ok({ items: PERMISSION_CATALOG })
    if (ctx.path === '/admin/roles' && ctx.method === 'POST') {
      created = ctx.body
      return ok({ role: { ...ctx.body, builtin: false, userCount: 0 } })
    }
    return fail(404, 'not mocked')
  }
  const container = await mountView(RolesView)
  const body = text()
  check('渲染角色名', body.includes('运营专员'))
  check('渲染内置标记', body.includes('内置'))
  check('渲染用户数', body.includes('2'))

  check('点击新建角色', clickText('＋ 新建角色'))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('对话框打开', dialog !== null)
  if (!dialog) return

  const codeInput = inputByLabel('角色 code', dialog) as HTMLInputElement | null
  const nameInput = inputByLabel('角色名称', dialog) as HTMLInputElement | null
  check('找到 code / 名称输入框', codeInput !== null && nameInput !== null)
  if (codeInput) setInput(codeInput, 'auditor')
  if (nameInput) setInput(nameInput, '审计员')
  await flush()

  check('渲染按 group 分组的复选框', dialog.querySelectorAll('.perm-group').length === 2)
  check('分组标题含「设备」「系统」', text(dialog).includes('设备') && text(dialog).includes('系统'))

  // 点击「设备」分组标题前的复选框 → 整组全选（device:read + device:write）
  const groupChecks = Array.from(dialog.querySelectorAll('.perm-group-head input[type=checkbox]')) as HTMLInputElement[]
  check('找到分组全选复选框', groupChecks.length === 2)
  groupChecks[0]?.click()
  await flush()
  check('整组全选后已选数量 2 个', text(dialog).includes('已选 2 /'))

  // 再单独取消 device:write（验证半选/单项取消）
  const itemChecks = Array.from(dialog.querySelectorAll('.perm-group-body input[type=checkbox]')) as HTMLInputElement[]
  check('找到权限项复选框', itemChecks.length === 4)
  itemChecks[1]?.click()
  await flush()
  check('单项取消后已选 1 个', text(dialog).includes('已选 1 /'))
  check('组复选框呈半选态', dialog.querySelectorAll('.el-checkbox__input.is-indeterminate').length >= 1)

  check('点击确认新建', clickText('确认新建', dialog))
  await flush()
  check('POST /admin/roles 已发出', findCall('POST', '/admin/roles') !== undefined)
  check('code/name 正确', created?.code === 'auditor' && created?.name === '审计员', JSON.stringify(created))
  check(
    'permissions 为勾选后的 device:read',
    Array.isArray(created?.permissions) && created.permissions.length === 1 && created.permissions[0] === 'device:read',
    JSON.stringify(created?.permissions),
  )

  // 内置角色禁止删除：确认按钮 disabled
  const builtinRow = Array.from(document.querySelectorAll('.el-table__row')).find((row) => text(row).includes('超级管理员'))
  const delBtn = builtinRow?.querySelector('.el-button--danger') as HTMLButtonElement | undefined
  check('内置角色删除按钮 disabled', delBtn !== undefined && delBtn.disabled === true)

  // 删除非内置角色 → 弹出确认 → 调用 DELETE
  let deleted = ''
  handler = (ctx) => {
    if (ctx.path === '/admin/roles' && ctx.method === 'GET') return ok({ items: ROLE_LIST })
    if (ctx.path === '/admin/permissions') return ok({ items: PERMISSION_CATALOG })
    if (ctx.method === 'DELETE') {
      deleted = ctx.path
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const closeBtn = document.querySelector('.el-dialog__headerbtn') as HTMLElement | null
  closeBtn?.click()
  await flush()
  const opRow = Array.from(document.querySelectorAll('.el-table__row')).find((row) => text(row).includes('运营专员'))
  const opDel = Array.from(opRow?.querySelectorAll('.el-button--danger') ?? [])[0] as HTMLButtonElement | undefined
  opDel?.click()
  await flush()
  check('删除二次确认弹窗出现', document.querySelector('.el-message-box') !== null)
  const confirmBtn = document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null
  confirmBtn?.click()
  await flush()
  check('DELETE /admin/roles/operator 已发出', deleted.includes('/admin/roles/operator'), deleted)
}

async function checkConfigView(): Promise<void> {
  console.log('\n[5] 配置中心：JSON 校验与提交')
  resetDom()
  let putBody: any = null
  const items = [
    { key: 'feature.game_upload_enabled', value: { enabled: true, level: 3 }, description: '游戏上传开关', updatedAt: '2025-01-02T03:04:05Z' },
    { key: 'ui.theme', value: 'sea', description: '', updatedAt: '2025-01-02T03:04:05Z' },
  ]
  handler = (ctx) => {
    if (ctx.path === '/admin/config' && ctx.method === 'GET') return ok({ items })
    if (ctx.path.startsWith('/admin/config/') && ctx.method === 'PUT') {
      putBody = ctx.body
      return ok({ key: ctx.path.replace('/admin/config/', ''), value: ctx.body?.value })
    }
    return fail(404, 'not mocked')
  }
  const container = await mountView(ConfigView)
  const body = text(container)
  check('渲染配置 key', body.includes('feature.game_upload_enabled'))
  check('渲染类型 object', body.includes('object'))
  check('渲染值摘要', body.includes('"enabled": true'), body.slice(0, 400))

  const editCount = countsByText('编辑', container)
  check('存在编辑按钮', editCount === 2, `count=${editCount}`)
  check('点击第一行编辑', clickText('编辑', container))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('编辑对话框打开', dialog !== null)
  if (!dialog) return
  const textarea = dialog.querySelector('textarea') as HTMLTextAreaElement | null
  check('JSON 文本框存在且回填格式化 JSON', textarea !== null && (textarea.value ?? '').includes('"enabled": true'))

  if (textarea) setInput(textarea, '{ invalid json ')
  await flush()
  calls.length = 0
  check('点击保存修改（非法 JSON）', clickText('保存修改', dialog))
  await flush()
  check('非法 JSON 提示出现', text(dialog).includes('JSON 解析失败'))
  check('非法 JSON 不提交', findCall('PUT', '/admin/config/') === undefined)

  if (textarea) setInput(textarea, '{"enabled":false,"list":[1,2]}')
  await flush()
  check('点击格式化', clickText('格式化', dialog))
  await flush()
  check('格式化后为缩进 JSON', (textarea?.value ?? '').includes('\n  "enabled"'))
  check('点击保存修改（合法 JSON）', clickText('保存修改', dialog))
  await flush()
  const put = findCall('PUT', '/admin/config/feature.game_upload_enabled')
  check('PUT 已发出', put !== undefined)
  check(
    '提交的是解析后的对象',
    JSON.stringify(putBody?.value) === JSON.stringify({ enabled: false, list: [1, 2] }),
    JSON.stringify(putBody),
  )
}

async function checkAssetsView(): Promise<void> {
  console.log('\n[6] 资产发布：upload-url → 直传 → 登记')
  resetDom()
  const fileContent = 'zhiwellcare-smoke-payload'
  const expectedSha = createHash('sha256').update(fileContent).digest('hex')
  const assets: any[] = []
  let uploadTicketBody: any = null
  let registerBody: any = null
  let putToStorage: any = null

  handler = (ctx) => {
    if (ctx.path === '/admin/assets' && ctx.method === 'GET') return ok({ items: assets })
    if (ctx.path === '/admin/assets/upload-url') {
      uploadTicketBody = ctx.body
      return ok({
        key: 'assets/web/console.zip',
        uploadUrl: 'https://s3.local/bucket/assets/web/console.zip?X-Amz-Signature=abc',
        method: 'PUT',
        expiresInSeconds: 900,
      })
    }
    if (ctx.method === 'PUT' && ctx.url.startsWith('https://s3.local/')) {
      putToStorage = ctx.raw
      return { status: 200, text: '' }
    }
    if (ctx.path === '/admin/assets' && ctx.method === 'POST') {
      registerBody = ctx.body
      assets.push({ id: 'asset-1', ...ctx.body, status: 'draft', createdAt: '2025-01-02T03:04:05Z', publishedAt: null })
      return ok({ asset: assets[0] })
    }
    if (ctx.path.includes('/status') && ctx.method === 'PATCH') {
      if (assets[0]) assets[0].status = ctx.body?.status
      return ok({ asset: assets[0] })
    }
    return fail(404, 'not mocked')
  }

  const container = await mountView(AssetsView)
  check('渲染三个 kind Tab', ['Web 包', '游戏资源', '固件'].every((k) => text(container.querySelector('.el-tabs__nav') ?? container).includes(k)))

  const fileInput = container.querySelector('input[type=file]') as HTMLInputElement | null
  check('存在文件选择输入框', fileInput !== null)
  if (!fileInput) return
  const file = new (globalThis as any).File([fileContent], 'console-1.2.0.zip', { type: 'application/zip' })
  Object.defineProperty(fileInput, 'files', { value: [file], configurable: true })
  fileInput.dispatchEvent(new (globalThis as any).Event('change', { bubbles: true }))
  await flush()
  check('选中文件名展示', text(container).includes('console-1.2.0.zip'))
  check('自动识别 Content-Type', (inputByLabel('Content-Type', container) as HTMLInputElement | null)?.value === 'application/zip')
  check('从文件名推断版本号', (inputByLabel('版本号', container) as HTMLInputElement | null)?.value === '1.2.0')

  const refInput = inputByLabel('refId', container) as HTMLInputElement | null
  check('找到 refId 输入框', refInput !== null)
  if (refInput) setInput(refInput, 'web-console')
  await flush()

  check('点击上传并登记', clickText('上传并登记', container))
  await flush(20)

  check('POST upload-url 已发出', findCall('POST', '/admin/assets/upload-url') !== undefined)
  check(
    'upload-url 载荷含 kind/refId/version/filename/contentType',
    uploadTicketBody?.kind === 'web' &&
      uploadTicketBody?.refId === 'web-console' &&
      uploadTicketBody?.version === '1.2.0' &&
      uploadTicketBody?.filename === 'console-1.2.0.zip' &&
      uploadTicketBody?.contentType === 'application/zip',
    JSON.stringify(uploadTicketBody),
  )
  check('直传 PUT 到预签名地址且 body 为文件本体', putToStorage === file)
  check('登记 POST /admin/assets 已发出', findCall('POST', '/admin/assets') !== undefined)
  check('size 取 file.size', registerBody?.size === file.size, String(registerBody?.size))
  check('sha256 为小写 hex 且与 node crypto 一致', registerBody?.sha256 === expectedSha, `${registerBody?.sha256} vs ${expectedSha}`)
  check('objectKey 取 upload-url 返回的 key', registerBody?.objectKey === 'assets/web/console.zip')
  check('登记后列表出现该资产', text(container).includes('console-1.2.0.zip'))

  check('点击发布', clickText('发布', container))
  await flush()
  const patch = calls.find((c) => c.method === 'PATCH' && c.path.includes('/status'))
  check('PATCH status=published 已发出', patch?.body?.status === 'published', JSON.stringify(patch?.body))

  // 直传失败时的错误提示（CORS / 签名错误场景）
  resetDom()
  handler = (ctx) => {
    if (ctx.path === '/admin/assets' && ctx.method === 'GET') return ok({ items: [] })
    if (ctx.path === '/admin/assets/upload-url') return ok({ key: 'k', uploadUrl: 'https://s3.local/broken', method: 'PUT' })
    if (ctx.method === 'PUT') return { status: 403, text: '<Error>SignatureDoesNotMatch</Error>' }
    return fail(404, 'not mocked')
  }
  const container2 = await mountView(AssetsView)
  const input2 = container2.querySelector('input[type=file]') as HTMLInputElement
  const file2 = new (globalThis as any).File(['x'], 'fw.bin', { type: 'application/octet-stream' })
  Object.defineProperty(input2, 'files', { value: [file2], configurable: true })
  input2.dispatchEvent(new (globalThis as any).Event('change', { bubbles: true }))
  await flush()
  const ref2 = inputByLabel('refId', container2) as HTMLInputElement | null
  if (ref2) setInput(ref2, 'wobble-wrist-band')
  const ver2 = inputByLabel('版本号', container2) as HTMLInputElement | null
  if (ver2) setInput(ver2, '1.0.1')
  await flush()
  clickText('上传并登记', container2)
  await flush(20)
  check('直传 403 时给出错误提示', text(document.body).includes('直传失败'), text(document.body).slice(-200))
  check('直传失败后不登记资产', calls.filter((c) => c.method === 'POST' && c.path === '/admin/assets').length === 0)
}

async function checkUsersView(): Promise<void> {
  console.log('\n[7] 用户管理：角色列 + 分配角色')
  resetDom()
  let assigned: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/users' && ctx.method === 'GET') {
      return ok({
        items: [
          { id: 'u-1', phone: '13800008888', nickname: '老王', role: 'user', status: 1, createdAt: '2025-01-01T00:00:00Z', lastLoginAt: null },
          { id: 'u-2', phone: '13800009999', nickname: '小李', role: 'admin', status: 0, createdAt: '2025-01-01T00:00:00Z', lastLoginAt: null },
        ],
        total: 2,
      })
    }
    if (ctx.path === '/admin/roles' && ctx.method === 'GET') return ok({ items: ROLE_LIST })
    if (ctx.path.endsWith('/roles') && ctx.method === 'GET') {
      const id = ctx.path.replace('/admin/users/', '').replace('/roles', '')
      return ok({ roles: id === 'u-1' ? ['viewer'] : [], permissions: ['device:read'] })
    }
    if (ctx.path.endsWith('/roles') && ctx.method === 'PUT') {
      assigned = ctx.body?.roles ?? []
      return ok({ roles: assigned })
    }
    return fail(404, 'not mocked')
  }
  const container = await mountView(UsersView)
  await flush(12)
  check('用户列表渲染（含明文手机号）', text(container).includes('13800008888'))
  check('渲染账号类型列', text(container).includes('账号类型'))
  check('角色列懒加载显示已分配角色 code', text(container).includes('viewer'), text(container).slice(0, 300))
  check('未分配角色的行显示 —', (container.querySelectorAll('.el-table__row')[1]?.textContent ?? '').includes('—'))

  check('点击分配角色', clickText('分配角色', container))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('分配角色对话框打开', dialog !== null)
  if (!dialog) return
  check('对话框标题含用户名', text(dialog).includes('老王'))

  const selectWrapper = dialog.querySelector('.el-select__wrapper') as HTMLElement | null
  check('找到角色多选框', selectWrapper !== null)
  selectWrapper?.click()
  await flush()
  const options = Array.from(document.querySelectorAll('.el-select-dropdown__item'))
  check('角色下拉选项渲染', options.length >= 2, String(options.length))
  const operatorOption = options.find((o) => (o.textContent ?? '').includes('operator'))
  operatorOption && (operatorOption as HTMLElement).click()
  await flush()
  check('点击保存角色', clickText('保存角色', dialog))
  await flush(12)
  check('PUT /admin/users/u-1/roles 已发出', findCall('PUT', '/admin/users/u-1/roles') !== undefined)
  check('提交角色包含 viewer 与 operator', assigned.includes('viewer') && assigned.includes('operator'), JSON.stringify(assigned))
}

async function checkWhitelistView(): Promise<void> {
  console.log('\n[8] 白名单 / 映射：增删改查')
  resetDom()
  const whitelist = [{ deviceKey: 'ZWK-0001', modelId: 'wobble-wrist-band', note: '首批', enabled: true }]
  const mappings = [{ deviceKey: 'ZWK-0002', modelId: 'kart-racing', note: '手动映射' }]
  let addedWhitelist: any = null
  handler = (ctx) => {
    if (ctx.path === '/admin/device-whitelist' && ctx.method === 'GET') return ok({ items: whitelist })
    if (ctx.path === '/admin/device-mappings' && ctx.method === 'GET') return ok({ items: mappings })
    if (ctx.path === '/admin/devices') return ok([{ modelId: 'wobble-wrist-band', name: '不倒翁挥腕环' }])
    if (ctx.path === '/admin/device-whitelist' && ctx.method === 'POST') {
      addedWhitelist = ctx.body
      return ok({ item: { ...ctx.body, enabled: true } })
    }
    if (ctx.method === 'DELETE') return ok({ deleted: true })
    return fail(404, 'not mocked')
  }
  const container = await mountView(WhitelistView)
  await flush(12)
  check('白名单 Tab 渲染数据', text(container).includes('ZWK-0001'))
  check('型号名从 /admin/devices 映射展示', text(container).includes('不倒翁挥腕环'))
  check('启用状态展示', text(container).includes('已启用'))

  check('切换到映射 Tab', clickText('设备映射（1）', container))
  await flush()
  check('映射 Tab 渲染数据', text(container).includes('ZWK-0002'))

  check('回到白名单 Tab', clickText('设备白名单（1）', container))
  await flush()
  check('点击新增白名单', clickText('新增白名单', container))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('新增对话框打开', dialog !== null)
  if (!dialog) return
  const keyInput = inputByLabel('设备 key', dialog) as HTMLInputElement | null
  check('找到设备 key 输入框', keyInput !== null)
  if (keyInput) setInput(keyInput, 'ZWK-9999')
  const modelSelectInput = inputByLabel('设备型号 modelId', dialog) as HTMLInputElement | null
  check('找到 modelId 下拉', modelSelectInput !== null)
  if (modelSelectInput) {
    modelSelectInput.dispatchEvent(new (globalThis as any).Event('input', { bubbles: true }))
    ;(dialog.querySelector('.el-select__wrapper') as HTMLElement | null)?.click()
    await flush()
    const opt = Array.from(document.querySelectorAll('.el-select-dropdown__item')).find((o) => (o.textContent ?? '').includes('wobble-wrist-band'))
    opt && (opt as HTMLElement).click()
  }
  await flush()
  check('点击确认新增', clickText('确认新增', dialog))
  await flush(12)
  check('POST /admin/device-whitelist 载荷正确', addedWhitelist?.deviceKey === 'ZWK-9999' && addedWhitelist?.modelId === 'wobble-wrist-band', JSON.stringify(addedWhitelist))

  // 编辑（upsert）+ 删除（二次确认）
  resetDom()
  let edited: any = null
  const deletes: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/device-whitelist' && ctx.method === 'GET') return ok({ items: whitelist })
    if (ctx.path === '/admin/device-mappings' && ctx.method === 'GET') return ok({ items: [] })
    if (ctx.path === '/admin/devices') return ok([])
    if (ctx.path === '/admin/device-whitelist' && ctx.method === 'POST') {
      edited = ctx.body
      return ok({ item: { ...whitelist[0], ...ctx.body } })
    }
    if (ctx.method === 'DELETE') {
      deletes.push(ctx.path)
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const container3 = await mountView(WhitelistView)
  await flush(12)
  check('点击编辑', clickText('编辑', container3))
  await flush()
  const editDialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('编辑对话框打开', editDialog !== null)
  if (editDialog) {
    const keyInput2 = inputByLabel('设备 key', editDialog) as HTMLInputElement | null
    check('编辑时设备 key 只读（唯一标识）', keyInput2?.disabled === true)
    const noteInput = inputByLabel('备注', editDialog) as HTMLTextAreaElement | null
    check('找到备注输入框', noteInput !== null)
    if (noteInput) setInput(noteInput, '编辑后备注')
    const switchInput = editDialog.querySelector('.el-switch__input') as HTMLInputElement | null
    check('白名单有启用开关', switchInput !== null)
    switchInput?.click()
    await flush()
    check('点击确认修改', clickText('确认修改', editDialog))
    await flush(12)
    check('编辑走 upsert（POST 同一 deviceKey）', edited?.deviceKey === 'ZWK-0001', JSON.stringify(edited))
    check('编辑提交 note 与 enabled', edited?.note === '编辑后备注' && edited?.enabled === false, JSON.stringify(edited))
    check('编辑不再触发删除', deletes.length === 0, JSON.stringify(deletes))
  }

  check('点击删除', clickText('删除', container3))
  await flush()
  check('删除二次确认出现', document.querySelector('.el-message-box') !== null)
  ;(document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null)?.click()
  await flush(12)
  check('DELETE /admin/device-whitelist/ZWK-0001 已发出', deletes.some((d) => d.includes('/admin/device-whitelist/ZWK-0001')), JSON.stringify(deletes))
}

async function checkRobustness(): Promise<void> {
  console.log('\n[9] 接口异常兜底：403 提示 + 上传中进度')
  resetDom()
  // 列表接口 403 → 友好提示 + 空状态，不白屏
  handler = () => fail(403, '当前账号无权查看用户列表')
  const container = await mountView(UsersView)
  await flush(12)
  const pageText = text(container)
  check('403 时弹出错误提示', text(document.body).includes('无权查看用户列表'), text(document.body).slice(-200))
  check('403 时页面仍正常渲染（无白屏）', pageText.includes('用户管理') || pageText.includes('查询'))
  check('403 时展示空状态', pageText.includes('暂无用户'))

  // 上传中：进度条与 loading 可见
  resetDom()
  let release: () => void = () => {}
  handler = (ctx) => {
    if (ctx.path === '/admin/assets' && ctx.method === 'GET') return ok({ items: [] })
    if (ctx.path === '/admin/assets/upload-url') return ok({ key: 'k', uploadUrl: 'https://s3.local/slow', method: 'PUT' })
    if (ctx.method === 'PUT') {
      return new Promise<MockResult>((resolve) => {
        release = () => resolve({ status: 200, text: '' })
      })
    }
    if (ctx.path === '/admin/assets' && ctx.method === 'POST') return ok({ asset: { id: 'a-9' } })
    return fail(404, 'not mocked')
  }
  const slow = await mountView(AssetsView)
  const file = new (globalThis as any).File(['slow-payload'], 'firmware.bin', { type: 'application/octet-stream' })
  const input = slow.querySelector('input[type=file]') as HTMLInputElement
  Object.defineProperty(input, 'files', { value: [file], configurable: true })
  input.dispatchEvent(new (globalThis as any).Event('change', { bubbles: true }))
  await flush()
  const refInput = inputByLabel('refId', slow) as HTMLInputElement | null
  if (refInput) setInput(refInput, 'wobble-wrist-band')
  const verInput = inputByLabel('版本号', slow) as HTMLInputElement | null
  if (verInput) setInput(verInput, '1.0.1')
  await flush()
  check('点击上传并登记（慢速直传）', clickText('上传并登记', slow))
  await flush(6)
  check('上传中显示进度条', slow.querySelector('.el-progress') !== null)
  check('上传中显示阶段提示', text(slow).includes('正在上传'))
  check('上传中按钮为 loading', (slow.querySelector('.el-button--primary') as HTMLElement | null)?.className.includes('is-loading') === true)
  release()
  await flush(20)
  check('直传完成后继续登记', findCall('POST', '/admin/assets') !== undefined)
  check('上传结束后进度条消失', slow.querySelector('.el-progress') === null)
  resetDom()
}

/* ------------------------------------------------------------------ */

it('管理后台新增页面冒烟：渲染 / 交互 / 请求载荷', async () => {
  installFetch()
  adminToken.set('smoke-token')
  await checkPermissionStore()
  checkErrorText()
  await checkRouterGuardAndMenu()
  await checkRolesView()
  await checkConfigView()
  await checkAssetsView()
  await checkUsersView()
  await checkWhitelistView()
  await checkRobustness()

  console.log(`\n通过 ${passed} 项，失败 ${failures.length} 项`)
  if (failures.length) {
    console.log('失败清单：')
    failures.forEach((f) => console.log(` - ${f}`))
    throw new Error(`冒烟失败 ${failures.length} 项：${failures.join(' / ')}`)
  }
}, 120_000)
