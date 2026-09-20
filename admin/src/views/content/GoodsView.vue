<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { adminApi, contentErrorText, type ContentStatus, type GoodsItem } from '../../api/admin'
import {
  CONTENT_ID_HINT,
  CONTENT_ID_PATTERN,
  centsToYuanText,
  contentIdError,
  contentStatusLabel,
  httpsUrlRule,
  parseYuanToCents,
  priceText,
  requiredError,
  sortError,
  urlError,
  yuanError,
  yuanRule,
} from '../../constants/content'
import TagListInput from '../../components/TagListInput.vue'

interface GoodsForm {
  goodsId: string
  name: string
  summary: string
  /** 界面按「元」输入（字符串，避免 v-model 把 19.90 吃成 19.9 或引入浮点误差） */
  priceYuan: string
  coverUrl: string
  detailUrl: string
  specs: string[]
  status: ContentStatus
  sort: number
}

const list = ref<GoodsItem[]>([])
const loading = ref(false)
const error = ref('')
const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const busyId = ref('')
/** 对话框内的校验/接口错误（el-alert 常显，不依赖 el-form 的异步校验结果） */
const dialogError = ref('')

const form = reactive<GoodsForm>(emptyGoodsForm())
const formRef = ref<FormInstance>()

const rules: FormRules = {
  goodsId: [
    { required: true, message: '请输入商品 ID', trigger: 'blur' },
    { pattern: CONTENT_ID_PATTERN, message: `商品 ID 需${CONTENT_ID_HINT}`, trigger: 'blur' },
  ],
  name: [{ required: true, message: '请输入商品名称', trigger: 'blur' }],
  priceYuan: [yuanRule({ label: '价格' })],
  // 后台不做上传：封面/详情只填 COS 地址，这里做 https:// 前缀校验并给中文提示
  coverUrl: [httpsUrlRule({ label: '封面地址' })],
  detailUrl: [httpsUrlRule({ label: '详情地址' })],
}

/** 实时预览最终提交给后端的「分」，让运营确认换算无误。 */
const centsPreview = computed(() => parseYuanToCents(form.priceYuan))

onMounted(() => {
  void load()
})

function emptyGoodsForm(): GoodsForm {
  return {
    goodsId: '',
    name: '',
    summary: '',
    priceYuan: '',
    coverUrl: '',
    detailUrl: '',
    specs: [],
    status: 'off',
    sort: 0,
  }
}

function pick(row: GoodsItem): GoodsForm {
  return {
    goodsId: row.goodsId,
    name: row.name || '',
    summary: row.summary || '',
    // 后端存分：回填时按分换算成元，保证「打开即保存」不会改变金额
    priceYuan: centsToYuanText(row.priceCents),
    coverUrl: row.coverUrl || '',
    detailUrl: row.detailUrl || '',
    specs: [...(row.specs ?? [])],
    status: row.status === 'on' ? 'on' : 'off',
    sort: Number.isFinite(Number(row.sort)) ? Math.trunc(Number(row.sort)) : 0,
  }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const data = await adminApi.listGoods()
    list.value = data?.items ?? []
    error.value = ''
  } catch (e) {
    list.value = []
    error.value = contentErrorText(e, '商品列表加载失败')
    ElMessage.error(error.value)
  } finally {
    loading.value = false
  }
}

async function openAdd(): Promise<void> {
  editing.value = false
  Object.assign(form, emptyGoodsForm())
  dialogError.value = ''
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

async function openEdit(row: GoodsItem): Promise<void> {
  editing.value = true
  Object.assign(form, pick(row))
  dialogError.value = ''
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

/** 提交前的同步校验：返回第一条中文错误提示（合法返回 ''）。 */
function validateDialog(): string {
  return (
    contentIdError(form.goodsId, '商品 ID') ||
    requiredError(form.name, '商品名称') ||
    yuanError(form.priceYuan, '价格') ||
    urlError(form.coverUrl, '封面地址') ||
    urlError(form.detailUrl, '详情地址') ||
    sortError(form.sort) ||
    ''
  )
}

function statusType(status: string): 'success' | 'info' {
  return status === 'on' ? 'success' : 'info'
}

/** 列表价格：优先后端 priceLabel，缺失时由 priceCents 换算。 */
function rowPrice(row: GoodsItem): string {
  return priceText(row.priceLabel, row.priceCents)
}

async function submit(): Promise<void> {
  const message = validateDialog()
  dialogError.value = message
  if (message) {
    ElMessage.error(message)
    return
  }
  // 元 → 分：全程整数运算（字符串取位 + 整数乘法），避免 19.9 * 100 = 1989.9999…
  const priceCents = parseYuanToCents(form.priceYuan)
  if (priceCents === null) {
    dialogError.value = '价格格式不正确：请填写最多两位小数的数字（如 19.90）'
    ElMessage.error(dialogError.value)
    return
  }
  // 触发 el-form 自身的失焦校验提示（部分 Element Plus 版本会静默放行，故不作为唯一防线）
  void formRef.value?.validate().catch(() => false)
  saving.value = true
  try {
    const payload = {
      goodsId: form.goodsId.trim(),
      name: form.name.trim(),
      summary: form.summary,
      priceCents,
      coverUrl: form.coverUrl.trim(),
      detailUrl: form.detailUrl.trim(),
      specs: [...form.specs],
      status: form.status,
      sort: Math.trunc(form.sort),
    }
    if (editing.value) await adminApi.updateGoods(payload.goodsId, payload)
    else await adminApi.upsertGoods(payload)
    ElMessage.success(editing.value ? '商品已更新' : '商品已创建')
    dialogVisible.value = false
    await load()
  } catch (e) {
    dialogError.value = contentErrorText(e, '商品保存失败')
    ElMessage.error(dialogError.value)
  } finally {
    saving.value = false
  }
}

async function toggleStatus(row: GoodsItem): Promise<void> {
  const target: ContentStatus = row.status === 'on' ? 'off' : 'on'
  if (target === 'off') {
    try {
      await ElMessageBox.confirm(`确认下架商品「${row.name}」？下架后用户端将不再展示该商品。`, '下架确认', {
        type: 'warning',
        confirmButtonText: '下架',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  busyId.value = row.goodsId
  try {
    await adminApi.setGoodsStatus(row.goodsId, target)
    ElMessage.success(target === 'on' ? '商品已上架' : '商品已下架')
    await load()
  } catch (e) {
    ElMessage.error(contentErrorText(e, '状态切换失败'))
  } finally {
    busyId.value = ''
  }
}

async function remove(row: GoodsItem): Promise<void> {
  try {
    await ElMessageBox.confirm(`删除后不可恢复。确认删除商品「${row.name}」（${row.goodsId}）？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await adminApi.deleteGoods(row.goodsId)
    ElMessage.success('商品已删除')
    await load()
  } catch (e) {
    ElMessage.error(contentErrorText(e, '删除失败'))
  }
}
</script>

<template>
  <div>
    <el-card shadow="never" class="page-card preview-card">
      <div class="preview-head">
        <span class="preview-ic">🛍️</span>
        <strong>价格与素材说明</strong>
      </div>
      <p class="preview-text">
        后端以「分」为单位存储价格（整数，字段 priceCents）；本页按「元」输入，提交时自动换算，例如 19.90 元 →
        1990。封面与详情链接请先上传到 COS，再把 https:// 地址粘贴进来（后台不提供文件上传）。
      </p>
    </el-card>

    <div class="page-card">
      <el-card shadow="never">
        <div class="table-toolbar">
          <div class="toolbar-info">
            <strong>共 {{ list.length }} 个商品</strong>
            <span class="form-tip">列表价格优先展示后端 priceLabel，缺失时由 priceCents 换算；下架项仍保留在列表中。</span>
          </div>
          <el-button type="primary" @click="openAdd">＋ 新增商品</el-button>
        </div>
        <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon style="margin-bottom: 12px" />
        <div class="table-scroll">
          <el-table v-loading="loading" :data="list" style="width: 100%">
            <el-table-column label="商品 ID" width="170">
              <template #default="{ row }">
                <span class="id-text">{{ row.goodsId }}</span>
              </template>
            </el-table-column>
            <el-table-column label="商品" min-width="240">
              <template #default="{ row }">
                <div class="item-cell">
                  <div class="item-title">{{ row.name }}</div>
                  <div v-if="row.summary" class="item-summary">{{ row.summary }}</div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="价格" width="130" align="right">
              <template #default="{ row }">
                <span class="price-text">{{ rowPrice(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="规格" min-width="200">
              <template #default="{ row }">
                <div v-if="row.specs && row.specs.length" class="tags-wrap">
                  <el-tag v-for="spec in row.specs" :key="spec" size="small" type="info" effect="plain" class="tag-item">
                    {{ spec }}
                  </el-tag>
                </div>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="排序" width="80" align="center">
              <template #default="{ row }">
                {{ Number.isFinite(Number(row.sort)) ? Number(row.sort) : 0 }}
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90" align="center">
              <template #default="{ row }">
                <el-tag :type="statusType(row.status)" size="small" :effect="row.status === 'on' ? 'dark' : 'plain'">
                  {{ contentStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="176" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
                <el-button link type="primary" :loading="busyId === row.goodsId" @click="toggleStatus(row)">
                  {{ row.status === 'on' ? '下架' : '上架' }}
                </el-button>
                <el-button link type="danger" @click="remove(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty
                :description="error ? '商品列表加载失败，请确认后端接口已就绪' : '暂无商品，点击右上角「新增商品」建档'"
                :image-size="90"
              />
            </template>
          </el-table>
        </div>
      </el-card>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? '编辑商品' : '新增商品'"
      width="720px"
      append-to-body
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-alert
          v-if="dialogError"
          :title="dialogError"
          type="error"
          :closable="false"
          show-icon
          style="margin-bottom: 14px"
        />
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="商品 ID" prop="goodsId">
              <el-input
                v-model="form.goodsId"
                :disabled="editing"
                placeholder="如 wrist-band-pro"
                :maxlength="64"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="商品名称" prop="name">
              <el-input v-model="form.name" placeholder="如 智能挥腕环 Pro" :maxlength="80" clearable />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="简介">
          <el-input
            v-model="form.summary"
            type="textarea"
            :rows="2"
            placeholder="面向用户展示的一句话商品介绍"
            :maxlength="200"
            show-word-limit
          />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="价格（元）" prop="priceYuan">
              <el-input v-model="form.priceYuan" placeholder="如 19.90" clearable>
                <template #append>元</template>
              </el-input>
              <p class="form-tip">
                最多两位小数。提交换算为「分」：
                <strong>{{ centsPreview === null ? '—' : `${centsPreview} 分` }}</strong>
              </p>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="排序">
              <el-input-number
                v-model="form.sort"
                :min="0"
                :max="99999"
                :step="1"
                :precision="0"
                controls-position="right"
                style="width: 100%"
              />
              <p class="form-tip">数字越小越靠前，用户端按该值升序展示。</p>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="规格">
          <TagListInput v-model="form.specs" add-text="＋ 添加规格" placeholder="如 颜色：海蓝，回车添加" />
          <p class="form-tip">可粘贴多行文本（每行一个规格）自动拆分，重复项自动忽略。</p>
        </el-form-item>
        <el-form-item label="封面地址" prop="coverUrl">
          <el-input v-model="form.coverUrl" placeholder="https://…（COS 图片地址，选填）" clearable />
        </el-form-item>
        <el-form-item label="详情地址" prop="detailUrl">
          <el-input v-model="form.detailUrl" placeholder="https://…（商品详情页 / 图文链接，选填）" clearable />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status">
            <el-radio value="on">上架</el-radio>
            <el-radio value="off">下架</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ editing ? '保存修改' : '确认新增' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.preview-card :deep(.el-card__body) {
  padding: 12px 16px;
}

.preview-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
}

.preview-ic {
  font-size: 15px;
}

.preview-text {
  margin: 6px 0 0;
  color: #55637b;
  font-size: 13px;
  line-height: 1.7;
}

.toolbar-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.table-scroll {
  overflow-x: auto;
}

.id-text {
  font-family: Consolas, 'Courier New', monospace;
  font-size: 13px;
}

.item-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item-title {
  font-weight: 600;
}

.item-summary {
  color: #8593a8;
  font-size: 12px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
}

.price-text {
  font-weight: 600;
  color: #0436a7;
}

.tags-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.muted {
  color: #b6c0cf;
}
</style>
