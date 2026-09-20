<script setup lang="ts">
import { nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { adminApi, contentErrorText, type ContentStatus, type CourseItem } from '../../api/admin'
import {
  CONTENT_ID_HINT,
  CONTENT_ID_PATTERN,
  COURSE_LEVEL_SUGGESTIONS,
  contentIdError,
  contentStatusLabel,
  httpsUrlRule,
  levelLabel,
  requiredError,
  sortError,
  urlError,
} from '../../constants/content'
import TagListInput from '../../components/TagListInput.vue'

interface CourseForm {
  courseId: string
  title: string
  summary: string
  coverUrl: string
  videoUrl: string
  durationLabel: string
  level: string
  tags: string[]
  status: ContentStatus
  sort: number
}

const list = ref<CourseItem[]>([])
const loading = ref(false)
const error = ref('')
const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const busyId = ref('')
/** 对话框内的校验/接口错误（el-alert 常显，不依赖 el-form 的异步校验结果） */
const dialogError = ref('')

const form = reactive<CourseForm>(emptyCourseForm())
const formRef = ref<FormInstance>()

const rules: FormRules = {
  courseId: [
    { required: true, message: '请输入课程 ID', trigger: 'blur' },
    { pattern: CONTENT_ID_PATTERN, message: `课程 ID 需${CONTENT_ID_HINT}`, trigger: 'blur' },
  ],
  title: [{ required: true, message: '请输入课程标题', trigger: 'blur' }],
  // 后台不做上传：封面/视频只填 COS 地址，这里做 https:// 前缀校验并给中文提示
  coverUrl: [httpsUrlRule({ label: '封面地址' })],
  videoUrl: [httpsUrlRule({ label: '视频地址' })],
}

onMounted(() => {
  void load()
})

function emptyCourseForm(): CourseForm {
  return {
    courseId: '',
    title: '',
    summary: '',
    coverUrl: '',
    videoUrl: '',
    durationLabel: '',
    level: '',
    tags: [],
    status: 'off',
    sort: 0,
  }
}

function pick(row: CourseItem): CourseForm {
  return {
    courseId: row.courseId,
    title: row.title || '',
    summary: row.summary || '',
    coverUrl: row.coverUrl || '',
    videoUrl: row.videoUrl || '',
    durationLabel: row.durationLabel || '',
    level: row.level || '',
    tags: [...(row.tags ?? [])],
    status: row.status === 'on' ? 'on' : 'off',
    sort: Number.isFinite(Number(row.sort)) ? Math.trunc(Number(row.sort)) : 0,
  }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const data = await adminApi.listCourses()
    list.value = data?.items ?? []
    error.value = ''
  } catch (e) {
    list.value = []
    error.value = contentErrorText(e, '课程列表加载失败')
    // 明确提示，避免只有一条静态红条被人忽略；后端未就绪时这里是 404/0
    ElMessage.error(error.value)
  } finally {
    loading.value = false
  }
}

async function openAdd(): Promise<void> {
  editing.value = false
  Object.assign(form, emptyCourseForm())
  dialogError.value = ''
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
}

async function openEdit(row: CourseItem): Promise<void> {
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
    contentIdError(form.courseId, '课程 ID') ||
    requiredError(form.title, '课程标题') ||
    urlError(form.coverUrl, '封面地址') ||
    urlError(form.videoUrl, '视频地址') ||
    sortError(form.sort) ||
    ''
  )
}

function statusType(status: string): 'success' | 'info' {
  return status === 'on' ? 'success' : 'info'
}

async function submit(): Promise<void> {
  const message = validateDialog()
  dialogError.value = message
  if (message) {
    ElMessage.error(message)
    return
  }
  // 触发 el-form 自身的失焦校验提示（部分 Element Plus 版本会静默放行，故不作为唯一防线）
  void formRef.value?.validate().catch(() => false)
  saving.value = true
  try {
    const payload = {
      courseId: form.courseId.trim(),
      title: form.title.trim(),
      summary: form.summary,
      coverUrl: form.coverUrl.trim(),
      videoUrl: form.videoUrl.trim(),
      durationLabel: form.durationLabel,
      level: form.level,
      tags: [...form.tags],
      status: form.status,
      sort: Math.trunc(form.sort),
    }
    if (editing.value) await adminApi.updateCourse(payload.courseId, payload)
    else await adminApi.upsertCourse(payload)
    ElMessage.success(editing.value ? '课程已更新' : '课程已创建')
    dialogVisible.value = false
    await load()
  } catch (e) {
    dialogError.value = contentErrorText(e, '课程保存失败')
    ElMessage.error(dialogError.value)
  } finally {
    saving.value = false
  }
}

async function toggleStatus(row: CourseItem): Promise<void> {
  const target: ContentStatus = row.status === 'on' ? 'off' : 'on'
  if (target === 'off') {
    try {
      await ElMessageBox.confirm(`确认下架课程「${row.title}」？下架后用户端将不再展示该课程。`, '下架确认', {
        type: 'warning',
        confirmButtonText: '下架',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  busyId.value = row.courseId
  try {
    await adminApi.setCourseStatus(row.courseId, target)
    ElMessage.success(target === 'on' ? '课程已上架' : '课程已下架')
    await load()
  } catch (e) {
    ElMessage.error(contentErrorText(e, '状态切换失败'))
  } finally {
    busyId.value = ''
  }
}

async function remove(row: CourseItem): Promise<void> {
  try {
    await ElMessageBox.confirm(`删除后不可恢复。确认删除课程「${row.title}」（${row.courseId}）？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await adminApi.deleteCourse(row.courseId)
    ElMessage.success('课程已删除')
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
        <span class="preview-ic">📚</span>
        <strong>上架说明</strong>
      </div>
      <p class="preview-text">
        课程列表含已下架项，用户端只展示「已上架」的课程，并按「排序」字段升序排列。封面与视频请先上传到 COS，
        再把 https:// 地址粘贴到下面的表单里（后台不提供文件上传）。
      </p>
    </el-card>

    <div class="page-card">
      <el-card shadow="never">
        <div class="table-toolbar">
          <div class="toolbar-info">
            <strong>共 {{ list.length }} 门课程</strong>
            <span class="form-tip">新增/编辑为 upsert 保存；上架、下架与排序均即时对用户端生效。</span>
          </div>
          <el-button type="primary" @click="openAdd">＋ 新增课程</el-button>
        </div>
        <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon style="margin-bottom: 12px" />
        <div class="table-scroll">
          <el-table v-loading="loading" :data="list" style="width: 100%">
            <el-table-column label="课程 ID" width="170">
              <template #default="{ row }">
                <span class="id-text">{{ row.courseId }}</span>
              </template>
            </el-table-column>
            <el-table-column label="课程" min-width="240">
              <template #default="{ row }">
                <div class="item-cell">
                  <div class="item-title">{{ row.title }}</div>
                  <div v-if="row.summary" class="item-summary">{{ row.summary }}</div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="时长" width="120">
              <template #default="{ row }">
                {{ row.durationLabel || '—' }}
              </template>
            </el-table-column>
            <el-table-column label="难度" width="110">
              <template #default="{ row }">
                <el-tag v-if="row.level" size="small" type="info" effect="plain">{{ levelLabel(row.level) }}</el-tag>
                <span v-else class="muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="标签" min-width="200">
              <template #default="{ row }">
                <div v-if="row.tags && row.tags.length" class="tags-wrap">
                  <el-tag v-for="tag in row.tags" :key="tag" size="small" type="primary" effect="plain" class="tag-item">
                    {{ tag }}
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
                <el-button link type="primary" :loading="busyId === row.courseId" @click="toggleStatus(row)">
                  {{ row.status === 'on' ? '下架' : '上架' }}
                </el-button>
                <el-button link type="danger" @click="remove(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty
                :description="error ? '课程列表加载失败，请确认后端接口已就绪' : '暂无课程，点击右上角「新增课程」建档'"
                :image-size="90"
              />
            </template>
          </el-table>
        </div>
      </el-card>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? '编辑课程' : '新增课程'"
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
            <el-form-item label="课程 ID" prop="courseId">
              <el-input
                v-model="form.courseId"
                :disabled="editing"
                placeholder="如 wrist-warmup-01"
                :maxlength="64"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="标题" prop="title">
              <el-input v-model="form.title" placeholder="如 腕部热身 5 分钟" :maxlength="80" clearable />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="简介">
          <el-input
            v-model="form.summary"
            type="textarea"
            :rows="2"
            placeholder="面向用户展示的一句话课程介绍"
            :maxlength="200"
            show-word-limit
          />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="时长文案">
              <el-input v-model="form.durationLabel" placeholder="如 12 分钟 / 3 节" :maxlength="30" clearable />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="难度">
              <el-select
                v-model="form.level"
                filterable
                allow-create
                default-first-option
                clearable
                placeholder="选择或输入难度"
                style="width: 100%"
              >
                <el-option v-for="level in COURSE_LEVEL_SUGGESTIONS" :key="level" :label="level" :value="level" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="标签">
          <TagListInput v-model="form.tags" add-text="＋ 添加标签" placeholder="如 腕部协调，回车添加" />
          <p class="form-tip">用于用户端筛选与推荐；可粘贴多行文本（每行一个，也支持逗号分隔）自动拆分。</p>
        </el-form-item>
        <el-form-item label="封面地址" prop="coverUrl">
          <el-input v-model="form.coverUrl" placeholder="https://…（COS 图片地址，选填）" clearable />
        </el-form-item>
        <el-form-item label="视频地址" prop="videoUrl">
          <el-input v-model="form.videoUrl" placeholder="https://…（COS 视频地址，选填）" clearable />
          <p class="form-tip">后台不提供上传：请先把图片/视频传到 COS，再把 https:// 地址粘贴到这里。</p>
        </el-form-item>
        <el-row :gutter="16">
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
          <el-col :span="12">
            <el-form-item label="状态">
              <el-radio-group v-model="form.status">
                <el-radio value="on">上架</el-radio>
                <el-radio value="off">下架</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
        </el-row>
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

.tags-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.muted {
  color: #b6c0cf;
}
</style>
