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
import type { FormItemRule } from 'element-plus'
import { createHash } from 'node:crypto'
import { it } from 'vitest'

import { adminToken } from '../src/api/token'
import { useAdminPermissionsStore } from '../src/stores/adminPermissions'
import { pinia } from '../src/stores/pinia'
import { adminApi, apiErrorText, contentErrorText } from '../src/api/admin'
import { ADMIN_MENUS, PERM, menuPermission } from '../src/constants/permissions'
import { centsToYuanText, formatCents, isHttpsUrl, parseYuanToCents, priceText } from '../src/constants/content'
import {
  PASSWORD_MAX_LENGTH,
  PASSWORD_MIN_LENGTH,
  emptyPasswordChangeForm,
  newPasswordError,
  passwordChangeError,
  passwordRule,
  type PasswordChangeForm,
} from '../src/constants/account'
import { ApiError } from '../src/api/http'
import RolesView from '../src/views/system/RolesView.vue'
import ConfigView from '../src/views/system/ConfigView.vue'
import WhitelistView from '../src/views/devices/WhitelistView.vue'
import AssetsView from '../src/views/assets/AssetsView.vue'
import UsersView from '../src/views/users/UsersView.vue'
import CoursesView from '../src/views/content/CoursesView.vue'
import GoodsView from '../src/views/content/GoodsView.vue'

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
  /** Authorization 头（用于校验个人账号接口同样带 bearer token） */
  auth: string
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
  g.fetch = async (input: unknown, init?: { method?: string; body?: unknown; headers?: unknown }) => {
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
    let auth = ''
    try {
      auth = init?.headers ? (new Headers(init.headers as HeadersInit).get('Authorization') ?? '') : ''
    } catch {
      /* 保留空串 */
    }
    const apiMatch = /\/api\/v1(\/[^?]*)/.exec(url)
    const ctx: Ctx = {
      method,
      url,
      path: apiMatch ? apiMatch[1] : url,
      query: new URLSearchParams(url.includes('?') ? url.slice(url.indexOf('?') + 1) : ''),
      body,
      raw: init?.body ?? null,
      auth,
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

/** 在标签输入框里回车提交（TagListInput 的提交方式之一）。 */
function pressEnter(input: HTMLElement): void {
  input.dispatchEvent(new (globalThis as any).KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
}

/** 按文本定位表格行，避免 clickText 在全文档范围误命中状态标签。 */
function rowByText(needle: string, root: ParentNode = document.body): Element | undefined {
  return Array.from(root.querySelectorAll('.el-table__row')).find((row) => text(row).includes(needle))
}

/** 行内按钮（编辑 / 上架 / 下架 / 删除）。 */
function rowButton(row: Element | undefined, needle: string): HTMLButtonElement | undefined {
  if (!row) return undefined
  return Array.from(row.querySelectorAll('.el-button')).find((btn) =>
    (btn.textContent ?? '').replace(/\s+/g, '').includes(needle),
  ) as HTMLButtonElement | undefined
}

/** 点击对话框里的单选（上架 / 下架），clickText 的候选选择器不含 .el-radio。 */
function clickRadio(label: string, root: ParentNode): boolean {
  const radios = Array.from(root.querySelectorAll('.el-radio')) as HTMLElement[]
  const target = radios.find((radio) => (radio.textContent ?? '').replace(/\s+/g, '').includes(label))
  const input = target?.querySelector('input') as HTMLInputElement | null | undefined
  if (!input) return false
  input.click()
  return true
}

/** 表格某一行的单元格文本（用于校验排序列等具体列值）。 */
function cellsOf(row: Element | undefined): string[] {
  if (!row) return []
  return Array.from(row.querySelectorAll('td')).map((cell) => text(cell).trim())
}

/** 最近一条 ElMessage 的文本（校验提示时避免命中上一条残留 toast）。 */
function lastMessageText(): string {
  const items = Array.from(document.querySelectorAll('.el-message'))
  return text(items[items.length - 1] ?? document.body)
}

/** 弹窗遮罩是否可见（v-show 关闭后元素仍在 DOM 里，display 为 none）。 */
function overlayVisible(): boolean {
  const overlay = document.querySelector('.el-overlay') as HTMLElement | null
  if (!overlay) return false
  return (globalThis as any).getComputedStyle(overlay).display !== 'none'
}

/** 直接调用 el-form 规则拿到校验文案（用于验证规则工厂与同步校验共用同一份提示）。 */
function ruleMessage(rule: FormItemRule): string {
  let message = ''
  const validator = rule.validator as unknown as (
    r: unknown,
    v: unknown,
    callback: (error?: string | Error) => void,
  ) => void
  validator(undefined, undefined, (error) => {
    message = typeof error === 'string' ? error : (error?.message ?? '')
  })
  return message
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

/* ------------------------------------------------------------------ *
 * 教程课程 / 商品（本期新增页面）
 * ------------------------------------------------------------------ */

const COURSE_LIST = [
  {
    courseId: 'wrist-warmup-01',
    title: '腕部热身 5 分钟',
    summary: '起床后唤醒腕部',
    coverUrl: 'https://cos.example.com/courses/c1.png',
    videoUrl: 'https://cos.example.com/courses/c1.mp4',
    durationLabel: '5 分钟',
    level: 'beginner',
    tags: ['腕部协调', '热身'],
    status: 'on',
    sort: 1,
  },
  {
    courseId: 'grip-basic-02',
    title: '握力基础训练',
    summary: '',
    coverUrl: '',
    videoUrl: '',
    durationLabel: '',
    level: '',
    tags: [],
    status: 'off',
    sort: 9,
  },
]

const GOODS_LIST = [
  {
    goodsId: 'wrist-band-pro',
    name: '智能挥腕环 Pro',
    summary: '专业版挥腕环',
    priceCents: 19900,
    priceLabel: '¥199.00',
    coverUrl: 'https://cos.example.com/goods/g1.png',
    detailUrl: 'https://shop.example.com/g1',
    specs: ['海蓝', 'M 码'],
    status: 'on',
    sort: 1,
  },
  {
    goodsId: 'grip-ball',
    name: '握力球',
    summary: '',
    priceCents: 1234,
    priceLabel: '',
    coverUrl: '',
    detailUrl: '',
    specs: [],
    status: 'off',
    sort: 2,
  },
]

async function checkContentHelpers(): Promise<void> {
  console.log('\n[10] 内容页纯函数：元/分整数换算、价格文案、https 校验')
  check('19.90 元 → 1990 分（整数运算，无浮点误差）', parseYuanToCents('19.90') === 1990)
  check('19.9 元 → 1990 分', parseYuanToCents('19.9') === 1990)
  check('0.07 元 → 7 分', parseYuanToCents('0.07') === 7)
  check('1.1 元 → 110 分', parseYuanToCents('1.1') === 110)
  check('三位小数被拒绝', parseYuanToCents('1.999') === null)
  check('非数字被拒绝', parseYuanToCents('abc') === null)
  check('空串被拒绝', parseYuanToCents('   ') === null)
  check('分 → 回填文案 1990 → "19.90"', centsToYuanText(1990) === '19.90')
  check('分 → 展示文案 1234 → "¥12.34"', formatCents(1234) === '¥12.34')
  check('分缺失时价格为「—」', formatCents(null) === '—')
  check('priceLabel 优先展示', priceText('¥199.00', 1000) === '¥199.00')
  check('priceLabel 为空时按分换算', priceText('', 1000) === '¥10.00')
  check(
    'isHttpsUrl 只接受 https',
    isHttpsUrl('https://cos.example.com/a.png') && !isHttpsUrl('http://cos.example.com/a.png') && !isHttpsUrl(''),
  )
  check('contentErrorText 对 403 给出「没有权限」提示', contentErrorText(new ApiError(403, '')).includes('没有内容管理权限'))
  check('contentErrorText 对 404 提示后端未就绪', contentErrorText(new ApiError(404, '')).includes('尚未上线'))
}

async function checkContentPermissions(): Promise<void> {
  console.log('\n[11] 内容页权限：菜单与路由 meta.permission 均以 content:read 为准')
  check('课程菜单权限含 content:read', menuPermission('/content/courses').includes(PERM.contentRead))
  check('商品菜单权限含 content:read', menuPermission('/content/goods').includes(PERM.contentRead))
  check('菜单表新增 2 个内容页', ADMIN_MENUS.filter((menu) => menu.path.startsWith('/content/')).length === 2)

  handler = (ctx) => {
    if (ctx.path === '/admin/me/permissions') return ok({ roles: ['operator'], permissions: ['content:read'] })
    if (ctx.path === '/admin/courses') return ok({ items: [] })
    if (ctx.path === '/admin/goods') return ok({ items: [] })
    return fail(404, 'not mocked')
  }
  adminToken.set('smoke-token')
  const { router } = await import('../src/router')
  // 路由守卫读的是共享 pinia 实例上的权限 store，这里先重置再按 content:read 重新加载
  const perms = useAdminPermissionsStore(pinia)
  perms.reset()
  await perms.load()
  check('权限 store 已加载 content:read', perms.can(PERM.contentRead))

  await router.push('/content/courses')
  await flush(12)
  check('有 content:read 可进入 /content/courses', router.currentRoute.value.path === '/content/courses')

  await router.push('/content/goods')
  await flush(12)
  check('有 content:read 可进入 /content/goods', router.currentRoute.value.path === '/content/goods')

  // 挂载真实布局，验证菜单按 content:read 显隐
  const container = document.createElement('div')
  document.body.appendChild(container)
  const app = createApp({ render: () => h(RouterView) })
  app.use(createPinia())
  app.use(router)
  app.use(ElementPlus)
  app.mount(container)
  mounted.push(app)
  await flush(14)
  const menuText = text(container.querySelector('.menu') ?? container)
  check('菜单显示「教程课程」', menuText.includes('教程课程'))
  check('菜单显示「商品管理」', menuText.includes('商品管理'))
  check('菜单隐藏无权限的「游戏目录」', !menuText.includes('游戏目录'))

  await router.push('/games')
  await flush(12)
  check('无 game 权限时其他受控页仍跳 /forbidden', router.currentRoute.value.path === '/forbidden')
  adminToken.clear()
}

async function checkCoursesView(): Promise<void> {
  console.log('\n[12] 教程课程页：列表渲染 / URL 校验 / 上下架 / 删除')
  resetDom()
  let created: any = null
  const patches: Array<{ path: string; body: any }> = []
  const deletes: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/courses' && ctx.method === 'GET') return ok({ items: COURSE_LIST })
    if (ctx.path === '/admin/courses' && ctx.method === 'POST') {
      created = ctx.body
      return ok({ item: { ...ctx.body, status: ctx.body?.status ?? 'off', sort: ctx.body?.sort ?? 0 } })
    }
    if (ctx.method === 'PATCH' && ctx.path.includes('/status')) {
      patches.push({ path: ctx.path, body: ctx.body })
      return ok({ item: {} })
    }
    if (ctx.method === 'DELETE') {
      deletes.push(ctx.path)
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const container = await mountView(CoursesView)
  await flush(12)
  const body = text(container)
  check('渲染课程标题', body.includes('腕部热身 5 分钟'))
  check('渲染简介', body.includes('起床后唤醒腕部'))
  check('渲染时长文案', body.includes('5 分钟'))
  check('难度英文码翻译为中文（beginner → 入门）', body.includes('入门'))
  check('渲染标签', body.includes('腕部协调'))
  check('渲染上架/下架状态标签', body.includes('已上架') && body.includes('已下架'))
  check('空字段展示「—」而非空白/NaN', text(rowByText('握力基础训练') ?? container).includes('—'))

  const offCourseRow = rowByText('握力基础训练')
  check('排序列展示后端 sort 值', cellsOf(offCourseRow).includes('9'), JSON.stringify(cellsOf(offCourseRow)))

  check('点击新增课程', clickText('＋ 新增课程', container))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('新增对话框打开', dialog !== null)
  if (!dialog) return

  const idInput = inputByLabel('课程 ID', dialog) as HTMLInputElement | null
  const titleInput = inputByLabel('标题', dialog) as HTMLInputElement | null
  check('找到课程 ID / 标题输入框', idInput !== null && titleInput !== null)
  if (!idInput || !titleInput) return
  setInput(idInput, 'balance-pro-03')
  setInput(titleInput, '平衡进阶训练')
  await flush()

  // 封面地址填 http://（后台不做上传，只接受 https://）→ 校验拦住且不提交
  const coverInput = inputByLabel('封面地址', dialog) as HTMLInputElement | null
  check('找到封面地址输入框', coverInput !== null)
  if (coverInput) setInput(coverInput, 'http://cos.example.com/courses/c3.png')
  await flush()
  calls.length = 0
  check('点击确认新增（http:// 地址）', clickText('确认新增', dialog))
  await flush(12)
  check('非 https 地址给出中文提示', text(dialog).includes('需以 https:// 开头'), text(dialog).slice(-200))
  check('校验失败时未提交', findCall('POST', '/admin/courses') === undefined)
  if (coverInput) setInput(coverInput, ' https://cos.example.com/courses/c3.png ')
  await flush()

  // 标签：点击添加 → 输入 → 回车（TagListInput）
  check('点击添加标签', clickText('＋ 添加标签', dialog))
  await flush()
  const tagInput = inputByLabel('标签', dialog) as HTMLInputElement | null
  check('标签输入框出现', tagInput !== null)
  if (tagInput) {
    setInput(tagInput, '平衡')
    pressEnter(tagInput)
  }
  await flush()
  check('标签以可删除的 tag 呈现', text(dialog).includes('平衡'))

  check('对话框内选择「上架」', clickRadio('上架', dialog))
  await flush()
  const sortInput = inputByLabel('排序', dialog) as HTMLInputElement | null
  if (sortInput) setInput(sortInput, '3')
  await flush()

  check('点击确认新增（字段合法）', clickText('确认新增', dialog))
  await flush(12)
  check('POST /admin/courses 已发出', findCall('POST', '/admin/courses') !== undefined)
  check(
    '提交载荷字段与契约一致',
    created?.courseId === 'balance-pro-03' &&
      created?.title === '平衡进阶训练' &&
      created?.coverUrl === 'https://cos.example.com/courses/c3.png' &&
      Array.isArray(created?.tags) &&
      created.tags.includes('平衡') &&
      created?.status === 'on' &&
      Number.isInteger(created?.sort),
    JSON.stringify(created),
  )
  check('URL 提交前已 trim', created?.coverUrl === 'https://cos.example.com/courses/c3.png', String(created?.coverUrl))

  // 上架 → 下架：二次确认 + PATCH
  const onRow = rowByText('腕部热身 5 分钟')
  const offBtn = rowButton(onRow, '下架')
  check('找到行内「下架」按钮', offBtn !== undefined)
  offBtn?.click()
  await flush()
  check('下架二次确认弹窗出现', document.querySelector('.el-message-box') !== null)
  const confirmBtn = document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null
  confirmBtn?.click()
  await flush(12)
  check(
    'PATCH /admin/courses/wrist-warmup-01/status 载荷 status=off',
    patches.some((p) => p.path === '/admin/courses/wrist-warmup-01/status' && p.body?.status === 'off'),
    JSON.stringify(patches),
  )

  // 下架 → 上架：无需确认，直接 PATCH
  resetDom()
  handler = (ctx) => {
    if (ctx.path === '/admin/courses' && ctx.method === 'GET') return ok({ items: COURSE_LIST })
    if (ctx.method === 'PATCH' && ctx.path.includes('/status')) {
      patches.push({ path: ctx.path, body: ctx.body })
      return ok({ item: {} })
    }
    if (ctx.method === 'DELETE') {
      deletes.push(ctx.path)
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const container2 = await mountView(CoursesView)
  await flush(12)
  rowButton(rowByText('握力基础训练'), '上架')?.click()
  await flush(12)
  check(
    '未上架课程点击「上架」直接 PATCH status=on',
    patches.some((p) => p.path === '/admin/courses/grip-basic-02/status' && p.body?.status === 'on'),
    JSON.stringify(patches),
  )

  rowButton(rowByText('握力基础训练'), '删除')?.click()
  await flush()
  check('删除二次确认弹窗出现', document.querySelector('.el-message-box') !== null)
  ;(document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null)?.click()
  await flush(12)
  check('DELETE /admin/courses/grip-basic-02 已发出', deletes.some((d) => d.includes('/admin/courses/grip-basic-02')), JSON.stringify(deletes))

  // 编辑：回填 + PUT
  resetDom()
  let updated: any = null
  const putPaths: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/courses' && ctx.method === 'GET') return ok({ items: COURSE_LIST })
    if (ctx.method === 'PUT') {
      updated = ctx.body
      putPaths.push(ctx.path)
      return ok({ item: ctx.body })
    }
    return fail(404, 'not mocked')
  }
  const container3 = await mountView(CoursesView)
  await flush(12)
  rowButton(rowByText('腕部热身 5 分钟'), '编辑')?.click()
  await flush()
  const editDialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('编辑对话框打开', editDialog !== null)
  if (editDialog) {
    check('编辑时课程 ID 只读（upsert 唯一标识）', (inputByLabel('课程 ID', editDialog) as HTMLInputElement | null)?.disabled === true)
    check('编辑时回填标签', text(editDialog).includes('腕部协调'))
    const editTitle = inputByLabel('标题', editDialog) as HTMLInputElement | null
    if (editTitle) setInput(editTitle, '腕部热身 6 分钟')
    await flush()
    check('点击保存修改', clickText('保存修改', editDialog))
    await flush(12)
    check('PUT /admin/courses/wrist-warmup-01 已发出', putPaths.some((p) => p.includes('/admin/courses/wrist-warmup-01')), JSON.stringify(putPaths))
    check('编辑提交标题已更新', updated?.title === '腕部热身 6 分钟', JSON.stringify(updated))
  }
}

async function checkGoodsView(): Promise<void> {
  console.log('\n[13] 商品页：价格元→分换算 / 规格拆分 / 上下架 / 删除')
  resetDom()
  let created: any = null
  const patches: Array<{ path: string; body: any }> = []
  const deletes: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/goods' && ctx.method === 'GET') return ok({ items: GOODS_LIST })
    if (ctx.path === '/admin/goods' && ctx.method === 'POST') {
      created = ctx.body
      return ok({ item: { ...ctx.body, status: ctx.body?.status ?? 'off' } })
    }
    if (ctx.method === 'PATCH' && ctx.path.includes('/status')) {
      patches.push({ path: ctx.path, body: ctx.body })
      return ok({ item: {} })
    }
    if (ctx.method === 'DELETE') {
      deletes.push(ctx.path)
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const container = await mountView(GoodsView)
  await flush(12)
  const body = text(container)
  check('渲染商品名', body.includes('智能挥腕环 Pro'))
  check('价格优先用后端 priceLabel', body.includes('¥199.00'))
  check('priceLabel 为空时由 priceCents 换算 1234 → ¥12.34', body.includes('¥12.34'))
  check('渲染规格标签', body.includes('海蓝') && body.includes('M 码'))
  check('渲染上架/下架状态标签', body.includes('已上架') && body.includes('已下架'))
  const offGoodsRow = rowByText('握力球')
  check('排序列展示后端 sort 值', cellsOf(offGoodsRow).includes('2'), JSON.stringify(cellsOf(offGoodsRow)))

  check('点击新增商品', clickText('＋ 新增商品', container))
  await flush()
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('新增对话框打开', dialog !== null)
  if (!dialog) return

  const idInput = inputByLabel('商品 ID', dialog) as HTMLInputElement | null
  const nameInput = inputByLabel('商品名称', dialog) as HTMLInputElement | null
  const priceInput = inputByLabel('价格（元）', dialog) as HTMLInputElement | null
  check('找到商品 ID / 名称 / 价格输入框', idInput !== null && nameInput !== null && priceInput !== null)
  if (!idInput || !nameInput || !priceInput) return
  setInput(idInput, 'grip-ball-lite')
  setInput(nameInput, '握力球 Lite')
  await flush()

  // 三位小数 → 校验拦住，不提交
  setInput(priceInput, '19.999')
  await flush()
  calls.length = 0
  check('点击确认新增（19.999 元）', clickText('确认新增', dialog))
  await flush(12)
  check('价格格式错误给出中文提示', text(dialog).includes('最多两位小数'), text(dialog).slice(-200))
  check('价格非法时未提交', findCall('POST', '/admin/goods') === undefined)

  // 合法价格：19.90 元 → 1990 分
  setInput(priceInput, '19.90')
  await flush()
  check('价格换算实时预览为「1990 分」', text(dialog).includes('1990 分'), text(dialog).slice(-200))

  // 详情地址 http:// → 拦住
  const detailInput = inputByLabel('详情地址', dialog) as HTMLInputElement | null
  check('找到详情地址输入框', detailInput !== null)
  if (detailInput) setInput(detailInput, 'http://shop.example.com/g2')
  await flush()
  calls.length = 0
  check('点击确认新增（http:// 详情地址）', clickText('确认新增', dialog))
  await flush(12)
  check('详情地址非 https 给出中文提示', text(dialog).includes('需以 https:// 开头'), text(dialog).slice(-200))
  check('URL 非法时未提交', findCall('POST', '/admin/goods') === undefined)
  if (detailInput) setInput(detailInput, 'https://shop.example.com/g2')
  await flush()

  // 规格：粘贴多行文本 → 按行拆分
  check('点击添加规格', clickText('＋ 添加规格', dialog))
  await flush()
  const specInput = inputByLabel('规格', dialog) as HTMLInputElement | null
  check('规格输入框出现', specInput !== null)
  if (specInput) {
    setInput(specInput, '红色，L 码')
    pressEnter(specInput)
  }
  await flush()
  check('规格按逗号拆分并可删除', text(dialog).includes('红色') && text(dialog).includes('L 码'))

  check('对话框内选择「上架」', clickRadio('上架', dialog))
  await flush()
  check('点击确认新增（字段合法）', clickText('确认新增', dialog))
  await flush(12)
  check('POST /admin/goods 已发出', findCall('POST', '/admin/goods') !== undefined)
  check(
    'priceCents 为整数 1990（元→分无浮点误差）',
    created?.priceCents === 1990 && Number.isInteger(created?.priceCents),
    JSON.stringify(created),
  )
  check(
    '提交载荷字段与契约一致',
    created?.goodsId === 'grip-ball-lite' &&
      created?.name === '握力球 Lite' &&
      created?.detailUrl === 'https://shop.example.com/g2' &&
      Array.isArray(created?.specs) &&
      created.specs.join(',') === '红色,L 码' &&
      created?.status === 'on',
    JSON.stringify(created),
  )

  // 编辑：价格由分回填成元
  resetDom()
  let updated: any = null
  const putPaths: string[] = []
  handler = (ctx) => {
    if (ctx.path === '/admin/goods' && ctx.method === 'GET') return ok({ items: GOODS_LIST })
    if (ctx.method === 'PUT') {
      updated = ctx.body
      putPaths.push(ctx.path)
      return ok({ item: ctx.body })
    }
    return fail(404, 'not mocked')
  }
  const container2 = await mountView(GoodsView)
  await flush(12)
  rowButton(rowByText('智能挥腕环 Pro'), '编辑')?.click()
  await flush()
  const editDialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('编辑对话框打开', editDialog !== null)
  if (editDialog) {
    check('编辑时商品 ID 只读', (inputByLabel('商品 ID', editDialog) as HTMLInputElement | null)?.disabled === true)
    check('价格由分回填为元（19900 分 → 199.00）', (inputByLabel('价格（元）', editDialog) as HTMLInputElement | null)?.value === '199.00')
    check('编辑时回填规格', text(editDialog).includes('海蓝'))
    check('点击保存修改（未改价格）', clickText('保存修改', editDialog))
    await flush(12)
    check('PUT /admin/goods/wrist-band-pro 已发出', putPaths.some((p) => p.includes('/admin/goods/wrist-band-pro')), JSON.stringify(putPaths))
    check('未改价格时金额不变（仍是 19900 分）', updated?.priceCents === 19900, JSON.stringify(updated))
  }

  // 上架/下架 + 删除
  resetDom()
  handler = (ctx) => {
    if (ctx.path === '/admin/goods' && ctx.method === 'GET') return ok({ items: GOODS_LIST })
    if (ctx.method === 'PATCH' && ctx.path.includes('/status')) {
      patches.push({ path: ctx.path, body: ctx.body })
      return ok({ item: {} })
    }
    if (ctx.method === 'DELETE') {
      deletes.push(ctx.path)
      return ok({ deleted: true })
    }
    return fail(404, 'not mocked')
  }
  const container3 = await mountView(GoodsView)
  await flush(12)
  rowButton(rowByText('智能挥腕环 Pro'), '下架')?.click()
  await flush()
  check('下架二次确认弹窗出现', document.querySelector('.el-message-box') !== null)
  ;(document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null)?.click()
  await flush(12)
  check(
    'PATCH /admin/goods/wrist-band-pro/status 载荷 status=off',
    patches.some((p) => p.path === '/admin/goods/wrist-band-pro/status' && p.body?.status === 'off'),
    JSON.stringify(patches),
  )
  rowButton(rowByText('握力球'), '删除')?.click()
  await flush()
  ;(document.querySelector('.el-message-box__btns .el-button--primary') as HTMLButtonElement | null)?.click()
  await flush(12)
  check('DELETE /admin/goods/grip-ball 已发出', deletes.some((d) => d.includes('/admin/goods/grip-ball')), JSON.stringify(deletes))
}

async function checkContentRobustness(): Promise<void> {
  console.log('\n[14] 内容页异常兜底：403 提示、后端未就绪、空态不白屏')
  resetDom()
  handler = () => fail(403, '当前账号无权查看课程')
  const container = await mountView(CoursesView)
  await flush(12)
  check('课程 403 时弹出错误提示', text(document.body).includes('无权查看课程'), text(document.body).slice(-200))
  check('课程 403 时页面仍渲染（无白屏）', text(container).includes('＋ 新增课程'))
  check('课程 403 时展示空态文案', text(container).includes('课程列表加载失败'))

  resetDom()
  handler = () => {
    throw new Error('network down')
  }
  const goodsContainer = await mountView(GoodsView)
  await flush(12)
  check('后端未就绪时商品页提示连接失败', text(document.body).includes('无法连接后端服务'), text(document.body).slice(-200))
  check('后端未就绪时商品页仍渲染（无白屏）', text(goodsContainer).includes('＋ 新增商品'))
  check('后端未就绪时商品页展示空态文案', text(goodsContainer).includes('商品列表加载失败'))

  resetDom()
  handler = (ctx) => {
    if (ctx.path === '/admin/courses' && ctx.method === 'GET') return ok({ items: [] })
    return ok({})
  }
  const emptyContainer = await mountView(CoursesView)
  await flush(12)
  check('列表为空时给出友好空态', text(emptyContainer).includes('暂无课程'))
  resetDom()
}

/* ------------------------------------------------------------------ *
 * 修改密码（个人账号自助功能）
 * ------------------------------------------------------------------ */

/** 挂载真实布局壳（顶栏用户菜单 → 修改密码弹窗）。 */
async function mountShell(): Promise<HTMLElement> {
  adminToken.set('smoke-token')
  const { router } = await import('../src/router')
  const container = document.createElement('div')
  document.body.appendChild(container)
  const app = createApp({ render: () => h(RouterView) })
  app.use(createPinia())
  app.use(router)
  app.use(ElementPlus)
  await router.push('/dashboard')
  app.mount(container)
  mounted.push(app)
  await flush(14)
  return container
}

/** 打开顶栏用户下拉并点击「修改密码」。 */
async function openPasswordEntry(container: HTMLElement): Promise<boolean> {
  ;(container.querySelector('.user') as HTMLElement | null)?.click()
  await flush()
  const entry = Array.from(document.querySelectorAll('.el-dropdown-menu__item')).find((item) =>
    (item.textContent ?? '').includes('修改密码'),
  ) as HTMLElement | undefined
  if (!entry) return false
  entry.click()
  await flush()
  return true
}

async function checkChangePasswordApi(): Promise<void> {
  console.log('\n[15] 修改密码接口：PUT /api/v1/me/password 路径、载荷与错误文案')
  resetDom()
  adminToken.set('smoke-token')
  let auth = ''
  handler = (ctx) => {
    if (ctx.path === '/me/password' && ctx.method === 'PUT') {
      auth = ctx.auth
      return ok({ updated: true })
    }
    return fail(404, 'not mocked')
  }

  const data = await adminApi.changePassword({ oldPassword: 'old-secret', newPassword: 'new-secret' })
  const call = calls[0]
  check('方法为 PUT', call?.method === 'PUT', String(call?.method))
  check('路径恰为 /api/v1/me/password', call?.url === '/api/v1/me/password', String(call?.url))
  check(
    '载荷字段恰为 oldPassword / newPassword',
    Object.keys(call?.body ?? {}).sort().join(',') === 'newPassword,oldPassword',
    JSON.stringify(call?.body),
  )
  check(
    '载荷取值原样透传',
    call?.body?.oldPassword === 'old-secret' && call?.body?.newPassword === 'new-secret',
    JSON.stringify(call?.body),
  )
  check('沿用既有 bearer access token', auth === 'Bearer smoke-token', JSON.stringify(auth))
  check('信封 {code:0,data:{updated:true}} 解包为 true', data?.updated === true, JSON.stringify(data))

  // 口令不做 trim（空格是合法字符，前端不得「顺手」裁剪）
  calls.length = 0
  await adminApi.changePassword({ oldPassword: '  spaced old  ', newPassword: '  spaced new  ' })
  check(
    '口令不做 trim',
    calls[0]?.body?.oldPassword === '  spaced old  ' && calls[0]?.body?.newPassword === '  spaced new  ',
    JSON.stringify(calls[0]?.body),
  )

  // 后端口径的中文错误必须原样透出（用户看到的提示以后端 message 为准）
  const errorOf = async (status: number, message: string): Promise<{ status: number; text: string }> => {
    handler = () => fail(status, message)
    try {
      await adminApi.changePassword({ oldPassword: 'old-secret', newPassword: 'new-secret' })
      return { status: 0, text: '' }
    } catch (e) {
      return { status: e instanceof ApiError ? e.status : -1, text: apiErrorText(e, '密码修改失败') }
    }
  }
  // 原密码错误刻意是 400 而不是 401：http.ts 对任何 401 都会清掉本地令牌，
  // 用 401 会让用户输错一次原密码就被踢回登录页（见 backend/API_CONTRACT.md）。
  const wrongOld = await errorOf(400, '原密码不正确')
  check('400 原密码不正确：状态与文案透出', wrongOld.status === 400 && wrongOld.text === '原密码不正确', JSON.stringify(wrongOld))
  const same = await errorOf(400, '新密码不能与原密码相同')
  check('400 新旧相同：文案透出', same.status === 400 && same.text === '新密码不能与原密码相同', JSON.stringify(same))
  const tooShort = await errorOf(400, '新密码长度需为 6-64 位')
  check('400 长度不合规：文案透出', tooShort.text.includes('6-64'), JSON.stringify(tooShort))
  const malformed = await errorOf(400, '请求参数格式不正确')
  check('400 参数格式不正确：文案透出', malformed.text === '请求参数格式不正确', JSON.stringify(malformed))

  handler = () => {
    throw new Error('network down')
  }
  let offline = ''
  try {
    await adminApi.changePassword({ oldPassword: 'a', newPassword: 'b' })
  } catch (e) {
    offline = apiErrorText(e, '密码修改失败')
  }
  check('后端未就绪时给出中文兜底提示', offline.includes('无法连接后端服务'), offline)
  // 改密接口的错误一律是 400（含「原密码不正确」），因此 http.ts 不会清本地 token：
  // 输错原密码后当前会话必须继续可用，否则用户会被静默踢回登录页。
  check('改密失败不影响当前登录态（400 不触发清令牌）', adminToken.get() === 'smoke-token', String(adminToken.get()))
  adminToken.set('smoke-token')
}

function checkPasswordValidation(): void {
  console.log('\n[16] 改密同步校验：必填 / 长度 6-64 / 两次一致 / 新旧不同')
  const valid: PasswordChangeForm = { oldPassword: 'old-secret', newPassword: 'new-secret', confirmPassword: 'new-secret' }
  check('合法表单通过校验', passwordChangeError(valid) === '', passwordChangeError(valid))
  check('纯空白原密码视为未填', passwordChangeError({ ...valid, oldPassword: '   ' }).includes('请填写原密码'))
  check('新密码为空被拒', passwordChangeError({ ...valid, newPassword: '' }).includes('请填写新密码'))
  check('确认新密码为空被拒', passwordChangeError({ ...valid, confirmPassword: '' }).includes('请填写确认新密码'))
  check('新密码 5 位被拒（后端 6-64）', passwordChangeError({ ...valid, newPassword: 'abcde', confirmPassword: 'abcde' }).includes(`长度需为 ${PASSWORD_MIN_LENGTH}-${PASSWORD_MAX_LENGTH} 位`))
  check('新密码 65 位被拒', passwordChangeError({ ...valid, newPassword: 'a'.repeat(65), confirmPassword: 'a'.repeat(65) }).includes('长度需为 6-64 位'))
  check('新密码 6 位边界通过', passwordChangeError({ ...valid, newPassword: 'abcdef', confirmPassword: 'abcdef' }) === '')
  check('新密码 64 位边界通过', passwordChangeError({ ...valid, newPassword: 'a'.repeat(64), confirmPassword: 'a'.repeat(64) }) === '')
  check('两次输入不一致被拒', passwordChangeError({ ...valid, confirmPassword: 'new-secret-x' }).includes('不一致'))
  check(
    '新密码与原密码相同被拒（省掉一次往返）',
    passwordChangeError({ oldPassword: 'same-secret', newPassword: 'same-secret', confirmPassword: 'same-secret' }).includes('新密码不能与原密码相同'),
  )
  check('newPasswordError 只校验长度', newPasswordError('new-secret') === '' && newPasswordError('12345').includes('6-64'))
  check('长度常量与后端契约一致', PASSWORD_MIN_LENGTH === 6 && PASSWORD_MAX_LENGTH === 64, `${PASSWORD_MIN_LENGTH}-${PASSWORD_MAX_LENGTH}`)
  const empty = emptyPasswordChangeForm()
  check('emptyPasswordChangeForm 三个字段均为空', empty.oldPassword === '' && empty.newPassword === '' && empty.confirmPassword === '')

  // el-form 规则工厂与同步校验共用同一份中文文案（避免两处写歪）
  const form: PasswordChangeForm = { oldPassword: '', newPassword: '123', confirmPassword: '456' }
  check('规则：新密码过短给出长度提示', ruleMessage(passwordRule('newPassword', form)).includes('长度需为 6-64 位'), ruleMessage(passwordRule('newPassword', form)))
  check('规则：原密码为空给出必填提示', ruleMessage(passwordRule('oldPassword', form)).includes('请填写原密码'))
  check('规则：两次不一致给出不一致提示', ruleMessage(passwordRule('confirmPassword', form)).includes('不一致'))
  form.newPassword = 'new-secret'
  form.confirmPassword = 'new-secret'
  check('规则：合法值不再报错', ruleMessage(passwordRule('confirmPassword', form)) === '', ruleMessage(passwordRule('confirmPassword', form)))
}

async function checkChangePasswordDialog(): Promise<void> {
  console.log('\n[17] 修改密码弹窗：入口 / 校验拦截 / 提交 / 清空敏感字段')
  resetDom()
  let putBody: any = null
  let failNext = false
  handler = (ctx) => {
    // 只给 device:read：账号相关权限码一个都没有，改密入口仍必须可用（不做 RBAC 门禁）
    if (ctx.path === '/admin/me/permissions') return ok({ roles: ['operator'], permissions: ['device:read'] })
    if (ctx.path === '/me/password' && ctx.method === 'PUT') {
      putBody = ctx.body
      return failNext ? fail(400, '原密码不正确') : ok({ updated: true })
    }
    return ok({ items: [] })
  }
  const container = await mountShell()
  check('顶栏用户区域渲染', container.querySelector('.user') !== null)
  check('改密入口不依赖任何权限码（权限仅 device:read）', text(container.querySelector('.header') ?? container).includes('管理员'))

  check('打开用户下拉并点击「修改密码」', await openPasswordEntry(container))
  const items = Array.from(document.querySelectorAll('.el-dropdown-menu__item')).map((i) => (i.textContent ?? '').trim())
  check('下拉同时保留「退出登录」', items.some((t) => t.includes('退出登录')), JSON.stringify(items))
  check('未点击退出登录（未跳转登录页）', !text(document.body).includes('登录运营后台'))
  const dialog = document.querySelector('.el-dialog') as HTMLElement | null
  check('修改密码对话框打开', dialog !== null)
  if (!dialog) return
  check('对话框标题为「修改密码」', text(dialog.querySelector('.el-dialog__title') ?? dialog).includes('修改密码'))

  const oldInput = inputByLabel('原密码', dialog) as HTMLInputElement | null
  const newInput = inputByLabel('新密码', dialog) as HTMLInputElement | null
  const confirmInput = inputByLabel('确认新密码', dialog) as HTMLInputElement | null
  check('原密码 / 新密码 / 确认新密码三个字段齐备', oldInput !== null && newInput !== null && confirmInput !== null)
  check(
    '三个字段均为 type=password',
    [oldInput, newInput, confirmInput].every((input) => input?.type === 'password'),
    [oldInput?.type, newInput?.type, confirmInput?.type].join(','),
  )
  check('底部为「取消 / 确定」按钮', countsByText('取消', dialog) === 1 && countsByText('确定', dialog) === 1)
  if (!oldInput || !newInput || !confirmInput) return

  const fill = async (oldPwd: string, newPwd: string, confirmPwd: string): Promise<void> => {
    setInput(oldInput, oldPwd)
    setInput(newInput, newPwd)
    setInput(confirmInput, confirmPwd)
    await flush()
  }

  // show-password：Element Plus 只在字段有值时渲染切换图标，故先填值再断言
  await fill('old-secret', 'new-secret', 'new-secret')
  check(
    '三个字段均带 show-password 切换图标',
    dialog.querySelectorAll('.el-input__password').length === 3,
    String(dialog.querySelectorAll('.el-input__password').length),
  )
  ;(dialog.querySelectorAll('.el-input__password')[0] as HTMLElement | undefined)?.click()
  await flush()
  check('点击切换图标后原密码转为明文（type=text）', oldInput.type === 'text', oldInput.type)
  ;(dialog.querySelectorAll('.el-input__password')[0] as HTMLElement | undefined)?.click()
  await flush()
  check('再次点击恢复掩码（type=password）', oldInput.type === 'password', oldInput.type)

  // 1) 三个字段都为空 → 必填拦截，不发请求
  await fill('', '', '')
  calls.length = 0
  check('点击「确定」（全空）', clickText('确定', dialog))
  await flush()
  check('提示「请填写原密码」', lastMessageText().includes('请填写原密码'), lastMessageText())
  check('校验失败不发送请求', findCall('PUT', '/me/password') === undefined)

  // 2) 新密码太短
  await fill('old-secret', '123', '123')
  calls.length = 0
  check('点击「确定」（新密码 3 位）', clickText('确定', dialog))
  await flush()
  check('提示新密码长度 6-64', lastMessageText().includes('长度需为 6-64 位'), lastMessageText())
  check('长度非法不发送请求', findCall('PUT', '/me/password') === undefined)

  // 3) 两次输入不一致
  await fill('old-secret', 'new-secret', 'new-secret-x')
  calls.length = 0
  check('点击「确定」（两次不一致）', clickText('确定', dialog))
  await flush()
  check('提示两次输入不一致', lastMessageText().includes('不一致'), lastMessageText())
  check('不一致不发送请求', findCall('PUT', '/me/password') === undefined)

  // 4) 新密码与原密码相同
  await fill('same-secret', 'same-secret', 'same-secret')
  calls.length = 0
  check('点击「确定」（新旧相同）', clickText('确定', dialog))
  await flush()
  check('提示新密码不能与原密码相同', lastMessageText().includes('新密码不能与原密码相同'), lastMessageText())
  check('新旧相同不发送请求', findCall('PUT', '/me/password') === undefined)

  // 5) 合法表单 + 后端 400（原密码不正确）：展示后端中文 message、保留弹窗、清空口令
  failNext = true
  await fill('old-secret', 'new-secret', 'new-secret')
  calls.length = 0
  check('点击「确定」（字段合法）', clickText('确定', dialog))
  await flush(12)
  const put = findCall('PUT', '/me/password')
  check('发出 PUT /api/v1/me/password', put?.url === '/api/v1/me/password', String(put?.url))
  check(
    '载荷仅含 oldPassword / newPassword',
    JSON.stringify(putBody) === JSON.stringify({ oldPassword: 'old-secret', newPassword: 'new-secret' }),
    JSON.stringify(putBody),
  )
  check('失败时展示后端中文 message', lastMessageText().includes('原密码不正确'), lastMessageText())
  const failedOverlayVisible = overlayVisible()
  check('失败时不关闭对话框', failedOverlayVisible)
  check(
    '失败后清空三个口令字段',
    oldInput.value === '' && newInput.value === '' && confirmInput.value === '',
    `${oldInput.value}|${newInput.value}|${confirmInput.value}`,
  )
  // 原密码不正确是 400 而非 401，本地令牌不会被清掉，用户无需重新登录
  check('改密失败后登录态未被清除', adminToken.get() === 'smoke-token', String(adminToken.get()))
  adminToken.set('smoke-token')

  // 6) 成功：关闭弹窗 + 成功提示（说明其他设备在令牌过期前仍登录）
  failNext = false
  await fill('old-secret', 'new-secret', 'new-secret')
  calls.length = 0
  check('点击「确定」（后端成功）', clickText('确定', dialog))
  await flush(12)
  check('成功时提示已修改', lastMessageText().includes('密码已修改'), lastMessageText())
  check('成功提示说明其他设备在令牌过期前仍有效', lastMessageText().includes('令牌过期'), lastMessageText())
  check('成功后关闭对话框', !overlayVisible())

  // 7) 重新打开：口令已清空（不残留明文）
  adminToken.set('smoke-token')
  check('重新打开修改密码入口', await openPasswordEntry(container))
  const dialog2 = document.querySelector('.el-dialog') as HTMLElement | null
  check('修改密码对话框再次打开', dialog2 !== null && overlayVisible())
  if (!dialog2) return
  check(
    '重新打开时三个口令均为空',
    ['原密码', '新密码', '确认新密码'].every((label) => (inputByLabel(label, dialog2) as HTMLInputElement | null)?.value === ''),
  )

  // 8) 提交中：按钮 loading / 取消禁用 / 双击不重复提交
  let release: () => void = () => {}
  let puts = 0
  handler = (ctx) => {
    if (ctx.path === '/admin/me/permissions') return ok({ roles: ['operator'], permissions: ['device:read'] })
    if (ctx.path === '/me/password' && ctx.method === 'PUT') {
      puts += 1
      return new Promise<MockResult>((resolve) => {
        release = () => resolve(ok({ updated: true }))
      })
    }
    return ok({ items: [] })
  }
  const oldInput2 = inputByLabel('原密码', dialog2) as HTMLInputElement | null
  const newInput2 = inputByLabel('新密码', dialog2) as HTMLInputElement | null
  const confirmInput2 = inputByLabel('确认新密码', dialog2) as HTMLInputElement | null
  if (!oldInput2 || !newInput2 || !confirmInput2) return
  setInput(oldInput2, 'old-secret')
  setInput(newInput2, 'new-secret')
  setInput(confirmInput2, 'new-secret')
  await flush()
  check('点击「确定」（慢速接口）', clickText('确定', dialog2))
  await flush(6)
  const primary = dialog2.querySelector('.el-dialog__footer .el-button--primary') as HTMLElement | null
  const cancelBtn = dialog2.querySelector('.el-dialog__footer .el-button:not(.el-button--primary)') as HTMLButtonElement | null
  check('请求中确定按钮为 loading', primary?.className.includes('is-loading') === true, String(primary?.className))
  check('请求中取消按钮禁用', cancelBtn?.disabled === true)
  clickText('确定', dialog2)
  await flush(4)
  check('请求中重复点击不会重复提交', puts === 1, String(puts))
  release()
  await flush(20)
  check('请求完成后关闭对话框', !overlayVisible())
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
  await checkContentHelpers()
  await checkContentPermissions()
  await checkCoursesView()
  await checkGoodsView()
  await checkContentRobustness()
  await checkChangePasswordApi()
  checkPasswordValidation()
  await checkChangePasswordDialog()

  console.log(`\n通过 ${passed} 项，失败 ${failures.length} 项`)
  if (failures.length) {
    console.log('失败清单：')
    failures.forEach((f) => console.log(` - ${f}`))
    throw new Error(`冒烟失败 ${failures.length} 项：${failures.join(' / ')}`)
  }
}, 120_000)
