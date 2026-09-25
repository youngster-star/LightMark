<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { OnFileDrop, EventsOn } from '../wailsjs/runtime'
import {
  ListRecords,
  CreateRecord,
  ForceCreateRecord,
  UpdateRecord,
  DeleteRecord,
  RestoreRecord,
  PurgeRecord,
  StarRecord,
  UndoLastAction,
  OpenRecord,
  FetchURLMeta,
  ListCategories,
  CreateCategory,
  UpdateCategory,
  DeleteCategory,
  ListTags,
  CreateTag,
  RenameTag,
  DeleteTag,
  MergeTag,
  ValidateAllRecords,
  EmptyTrash,
  ExportToFile,
  ImportFromFile,
  ExportSnapshot,
  ChooseSnapshotPath,
  SetAutoStart,
  IsAutoStartEnabled,
  ChooseExportPath,
  ChooseImportPath,
  ChooseFilePath,
  FetchFaviconData,
} from '../wailsjs/go/main/App'
import { model } from '../wailsjs/go/models'

const records = ref<model.Record[]>([])
const categories = ref<model.Category[]>([])
const tags = ref<model.Tag[]>([])

const query = ref('')
const typeFilter = ref('')
const categoryFilter = ref('')
const tagFilter = ref('')
const starOnly = ref(false)
const showTrash = ref(false)
const sortBy = ref('updated_at')
const loading = ref(false)
const error = ref('')
// notice 绿色成功/信息提示（与红色 error 分离，避免成功消息误导为错误）
const notice = ref('')
// listLimit 渲染上限：万条级全量渲染 DOM 会卡顿，超出部分提示细化筛选
const listLimit = 200
// loadSeq 请求序号：防止慢响应覆盖新的筛选结果（竞态防护）
let loadSeq = 0
// expandedId 当前展开详情的记录 ID（空表示无展开）
const expandedId = ref('')

const showForm = ref(false)
const form = ref({
  id: '',
  type: 'file',
  pathOrUrl: '',
  title: '',
  note: '',
  categoryId: '',
  tags: '',
  star: false,
  favicon: '',
})
// lastAutoTitle 记录最近一次自动识别的标题，用于判断用户是否手动改过标题
const lastAutoTitle = ref('')

const showManage = ref(false)
const autoStart = ref(false)

// ---- 主题 ----
type ThemeSetting = 'system' | 'light' | 'dark'
const theme = ref<ThemeSetting>(
  (localStorage.getItem('lightmark-theme') as ThemeSetting) || 'system',
)
const prefersDark = window.matchMedia('(prefers-color-scheme: dark)')

// applyTheme 根据主题设置切换 html.dark 类；跟随系统时读取系统深色状态
function applyTheme() {
  const dark = theme.value === 'dark' || (theme.value === 'system' && prefersDark.matches)
  document.documentElement.classList.toggle('dark', dark)
}

// 切换主题设置时持久化到 localStorage 并立即生效
watch(theme, (t) => {
  localStorage.setItem('lightmark-theme', t)
  applyTheme()
})

// 跟随系统模式下，系统深浅色变化时实时切换
prefersDark.addEventListener('change', () => {
  if (theme.value === 'system') {
    applyTheme()
  }
})

async function load() {
  // 请求序号：仅采纳最后一次请求的结果
  const seq = ++loadSeq
  loading.value = true
  error.value = ''
  try {
    const list = (await ListRecords({
      type: typeFilter.value,
      categoryId: categoryFilter.value,
      tag: tagFilter.value,
      starOnly: starOnly.value,
      trashed: showTrash.value,
      query: query.value,
      sortBy: sortBy.value,
      sortDesc: sortBy.value !== 'title',
    })) ?? []
    if (seq !== loadSeq) return
    records.value = list
  } catch (e) {
    if (seq !== loadSeq) return
    error.value = String(e)
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function loadMeta() {
  try {
    categories.value = (await ListCategories()) ?? []
    tags.value = (await ListTags()) ?? []
  } catch (e) {
    error.value = String(e)
  }
}

function resetForm() {
  form.value = {
    id: '',
    type: 'file',
    pathOrUrl: '',
    title: '',
    note: '',
    categoryId: '',
    tags: '',
    star: false,
    favicon: '',
  }
  lastAutoTitle.value = ''
}

function basename(p: string): string {
  return p.split(/[\\/]/).pop() || p
}

// relativeTime 将毫秒时间戳转为简洁的相对时间描述
function relativeTime(ts: number): string {
  if (!ts) return ''
  const diff = Date.now() - ts
  const min = 60 * 1000
  if (diff < min) return '刚刚'
  if (diff < 60 * min) return Math.floor(diff / min) + ' 分钟前'
  if (diff < 24 * 60 * min) return Math.floor(diff / (60 * min)) + ' 小时前'
  if (diff < 30 * 24 * 60 * min) return Math.floor(diff / (24 * 60 * min)) + ' 天前'
  return new Date(ts).toLocaleDateString()
}

// undo 撤销最近一次记录操作（新增/删除/恢复/修改）
async function undo() {
  error.value = ''
  try {
    const msg = await UndoLastAction()
    await Promise.all([load(), loadMeta()])
    notice.value = msg || '已撤销'
  } catch (e) {
    error.value = String(e)
  }
}

// toggleExpand 点击记录展开/收起详情（备注全文）。
// 延时 250ms 执行：双击打开会先触发 click，双击时取消未触发的展开，避免状态闪烁。
let expandTimer = 0
function toggleExpand(r: model.Record) {
  window.clearTimeout(expandTimer)
  const id = r.id
  expandTimer = window.setTimeout(() => {
    expandedId.value = expandedId.value === id ? '' : id
  }, 250)
}

// cancelExpand 双击打开前取消未触发的展开计时器
function cancelExpand() {
  window.clearTimeout(expandTimer)
}

// fileIcon 按扩展名映射文件类型图标字符
function fileIcon(p: string): string {
  const ext = (p.split('.').pop() || '').toLowerCase()
  const map: Record<string, string> = {
    md: '📝', txt: '📄', pdf: '📕',
    doc: '📘', docx: '📘', rtf: '📘',
    xls: '📊', xlsx: '📊', csv: '📊', ppt: '📽️', pptx: '📽️',
    png: '🖼️', jpg: '🖼️', jpeg: '🖼️', gif: '🖼️', webp: '🖼️', svg: '🖼️',
    zip: '🗜️', rar: '🗜️', '7z': '🗜️', gz: '🗜️', tar: '🗜️',
    mp4: '🎬', mkv: '🎬', avi: '🎬', mp3: '🎵', wav: '🎵', flac: '🎵',
    html: '🌐', htm: '🌐', json: '🧾', yaml: '🧾', yml: '🧾', xml: '🧾',
    go: '⚙️', py: '⚙️', js: '⚙️', ts: '⚙️', java: '⚙️', c: '⚙️', cpp: '⚙️', rs: '⚙️',
    exe: '🧩', msi: '🧩',
  }
  return map[ext] || '📄'
}

async function addFileRecord(path: string): Promise<boolean> {
  error.value = ''
  try {
    await CreateRecord({
      id: '',
      type: 'file',
      pathOrUrl: path,
      title: basename(path),
      note: '',
      categoryId: '',
      star: false,
      tags: [],
      favicon: '',
    })
    return true
  } catch (e) {
    // 批量拖拽时逐条报错会互相覆盖，改为返回失败由调用方汇总
    console.warn('拖拽录入失败:', String(e))
    return false
  }
}

// addFileRecords 批量录入拖拽文件，汇总成功/失败数量后统一提示
async function addFileRecords(paths: string[]) {
  let ok = 0
  let fail = 0
  for (const p of paths) {
    if (await addFileRecord(p)) {
      ok++
    } else {
      fail++
    }
  }
  await Promise.all([load(), loadMeta()])
  if (fail > 0) {
    error.value = `成功录入 ${ok} 条，${fail} 条失败（可能重复或无效）`
  } else if (ok > 0) {
    notice.value = `成功录入 ${ok} 条`
  }
}

async function fetchTitle() {
  if (form.value.type !== 'url' || !form.value.pathOrUrl.trim()) {
    return
  }
  error.value = ''
  try {
    const meta = await FetchURLMeta(form.value.pathOrUrl.trim())
    // 标题为空或仍是上次自动识别值时才覆盖，保留用户手动编辑
    if (meta.title && (form.value.title === '' || form.value.title === lastAutoTitle.value)) {
      form.value.title = meta.title
      lastAutoTitle.value = meta.title
    }
    // favicon 抓取并缓存为 dataURL，失败不影响录入
    if (meta.favicon && !form.value.favicon) {
      try {
        form.value.favicon = await FetchFaviconData(meta.favicon)
      } catch {
        /* 忽略图标抓取失败 */
      }
    }
  } catch (e) {
    error.value = '抓取标题失败: ' + String(e)
  }
}

// pickFile 打开文件选择对话框选择本地路径：路径只能通过对话框选择/重选，不可手动编写。
// 文件名自动识别：标题为空或仍是上次自动识别值时跟随新文件更新，用户改过则保留。
async function pickFile() {
  error.value = ''
  try {
    const path = await ChooseFilePath()
    if (!path) return
    form.value.pathOrUrl = path
    const name = basename(path)
    if (!form.value.title.trim() || form.value.title === lastAutoTitle.value) {
      form.value.title = name
    }
    lastAutoTitle.value = name
  } catch (e) {
    error.value = String(e)
  }
}

async function save() {
  if (!form.value.title.trim() || !form.value.pathOrUrl.trim()) {
    error.value = form.value.type === 'file' ? '请填写标题并选择文件' : '请填写标题与网址'
    return
  }
  error.value = ''
  const tagList = form.value.tags
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
  const common = {
    pathOrUrl: form.value.pathOrUrl.trim(),
    title: form.value.title.trim(),
    note: form.value.note,
    categoryId: form.value.categoryId,
    star: form.value.star,
    tags: tagList,
    favicon: form.value.favicon,
  }
  try {
    if (form.value.id) {
      await UpdateRecord({
        id: form.value.id,
        type: form.value.type,
        ...common,
      })
    } else {
      try {
        // 先带查重保存：服务端发现重复会以 DUPLICATE| 前缀错误阻止
        await CreateRecord({
          id: '',
          type: form.value.type,
          ...common,
        })
      } catch (e1) {
        const str = String(e1)
        // 重复错误：弹出确认，用户确认后走强制保存；其余错误直接上抛
        if (!str.startsWith('DUPLICATE|')) {
          throw e1
        }
        const dupMsg = str.slice('DUPLICATE|'.length)
        if (!window.confirm(dupMsgText(dupMsg)) ) {
          return
        }
        await ForceCreateRecord({
          id: '',
          type: form.value.type,
          ...common,
        })
      }
    }
    resetForm()
    showForm.value = false
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

function openEdit(r: model.Record) {
  form.value = {
    id: r.id,
    type: r.type,
    pathOrUrl: r.pathOrUrl,
    title: r.title,
    note: r.note,
    categoryId: r.categoryId,
    tags: (r.tags ?? []).map((t) => t.name).join(','),
    star: r.star,
    favicon: r.favicon,
  }
  // 文件类型记录以原路径基名作为自动标题基线，重定位时标题可跟随
  lastAutoTitle.value = r.type === 'file' ? basename(r.pathOrUrl) : ''
  showForm.value = true
}

async function remove(r: model.Record) {
  error.value = ''
  try {
    await DeleteRecord(r.id)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

async function restore(r: model.Record) {
  error.value = ''
  try {
    await RestoreRecord(r.id)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

// purge 彻底删除回收站中的单条记录（不可恢复，需确认）
async function purge(r: model.Record) {
  if (!window.confirm(`彻底删除「${r.title}」？该操作不可恢复。`)) return
  error.value = ''
  try {
    await PurgeRecord(r.id)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

async function open(r: model.Record) {
  error.value = ''
  try {
    await OpenRecord(r.id)
    // 打开后刷新列表，更新最近打开时间与次数
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

// dupMsgText 将后端重复错误转为确认弹窗文案
function dupMsgText(msg: string): string {
  return `${msg}，仍要保存吗？`
}

// toggleStar 用专用绑定切换星标：不进入撤销栈，避免覆盖删除等可撤销操作
async function toggleStar(r: model.Record) {
  error.value = ''
  try {
    await StarRecord(r.id, !r.star)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 分类管理 ----
async function addCategory() {
  const name = window.prompt('分类名称')
  if (!name) return
  error.value = ''
  try {
    await CreateCategory(name.trim(), '')
    await loadMeta()
  } catch (e) {
    error.value = String(e)
  }
}

async function renameCategory(c: model.Category) {
  const name = window.prompt('新名称', c.name)
  if (!name) return
  error.value = ''
  try {
    await UpdateCategory(c.id, name.trim(), c.parentId, c.sort)
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

async function removeCategory(c: model.Category) {
  if (!window.confirm(`删除分类「${c.name}」？其下记录将变为未分类。`)) return
  error.value = ''
  try {
    await DeleteCategory(c.id)
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 标签管理 ----
async function addTag() {
  const name = window.prompt('标签名称')
  if (!name) return
  error.value = ''
  try {
    await CreateTag(name.trim())
    await loadMeta()
  } catch (e) {
    error.value = String(e)
  }
}

async function renameTag(t: model.Tag) {
  const name = window.prompt('新名称', t.name)
  if (!name) return
  error.value = ''
  try {
    await RenameTag(t.id, name.trim())
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

async function removeTag(t: model.Tag) {
  if (!window.confirm(`删除标签「${t.name}」？`)) return
  error.value = ''
  try {
    await DeleteTag(t.id)
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

// mergeTag 将指定标签的记录关联合并到另一个标签（输入目标标签名），源标签被删除
async function mergeTag(t: model.Tag) {
  const name = window.prompt(`将「${t.name}」合并到哪个标签？（输入目标标签名）`)
  if (!name) return
  const dst = tags.value.find((x) => x.name === name.trim())
  if (!dst) {
    error.value = `不存在名为「${name.trim()}」的标签`
    return
  }
  if (dst.id === t.id) {
    error.value = '不能合并到标签自身'
    return
  }
  error.value = ''
  try {
    await MergeTag(t.id, dst.id)
    if (tagFilter.value) {
      tagFilter.value = ''
    }
    await Promise.all([load(), loadMeta()])
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 失效检测 ----
async function validateAll() {
  error.value = ''
  try {
    const n = await ValidateAllRecords()
    await load()
    if (n > 0) {
      notice.value = `发现 ${n} 条失效记录`
    } else {
      notice.value = '所有记录均有效'
    }
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 回收站 ----
async function emptyTrash() {
  if (!window.confirm('清空回收站将彻底删除其中的记录，且不可恢复。确定？')) return
  error.value = ''
  try {
    await EmptyTrash()
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 备份 ----
async function exportBackup() {
  error.value = ''
  try {
    const path = await ChooseExportPath()
    if (!path) return
    await ExportToFile(path)
    notice.value = '已导出备份'
  } catch (e) {
    error.value = String(e)
  }
}

async function importBackup() {
  error.value = ''
  try {
    const path = await ChooseImportPath()
    if (!path) return
    const res = await ImportFromFile(path)
    await Promise.all([load(), loadMeta()])
    notice.value = `导入完成：${res.records} 条记录、${res.categories} 个分类、${res.tags} 个标签`
  } catch (e) {
    error.value = String(e)
  }
}

// exportSnapshot 导出当前 SQLite 数据库完整快照（与 JSON 备份互补，可整库恢复）
async function exportSnapshot() {
  error.value = ''
  try {
    const path = await ChooseSnapshotPath()
    if (!path) return
    await ExportSnapshot(path)
    notice.value = '已导出数据库快照'
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 自启动 ----
async function loadAutoStart() {
  try {
    autoStart.value = await IsAutoStartEnabled()
  } catch (e) {
    error.value = String(e)
  }
}

async function toggleAutoStart() {
  error.value = ''
  try {
    await SetAutoStart(autoStart.value)
  } catch (e) {
    error.value = String(e)
  }
}

onMounted(async () => {
  applyTheme()
  OnFileDrop((_x, _y, paths) => {
    addFileRecords(paths)
  }, false)
  // 后台启动校验发现失效记录时通知刷新列表
  EventsOn('records-invalidated', (n: number) => {
    notice.value = `启动校验发现 ${n} 条失效记录`
    load()
  })
  await loadMeta()
  await load()
  await loadAutoStart()
})
</script>

<template>
  <div class="app">
    <header class="topbar">
      <h1 class="brand">LightMark</h1>
      <div class="search">
        <input
          v-model="query"
          class="input grow"
          placeholder="搜索文件名、备注、标签…"
          @keyup.enter="load"
        />
        <button class="btn primary" @click="load">搜索</button>
      </div>
      <button class="btn primary" @click="showForm = !showForm">+ 新增</button>
      <button class="btn" @click="showManage = !showManage">管理</button>
    </header>

    <div class="filters">
      <select v-model="typeFilter" class="select" @change="load">
        <option value="">全部类型</option>
        <option value="file">文件</option>
        <option value="url">网页</option>
      </select>
      <select v-model="categoryFilter" class="select" @change="load">
        <option value="">全部分类</option>
        <option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}</option>
      </select>
      <select v-model="sortBy" class="select" @change="load">
        <option value="updated_at">按更新时间</option>
        <option value="opened_at">按最近打开</option>
        <option value="created_at">按创建时间</option>
        <option value="title">按标题</option>
      </select>
      <select v-model="tagFilter" class="select" @change="load">
        <option value="">全部标签</option>
        <option v-for="t in tags" :key="t.id" :value="t.name">{{ t.name }}</option>
      </select>
      <label class="trash-toggle">
        <input v-model="starOnly" type="checkbox" @change="load" />
        仅星标
      </label>
      <label class="trash-toggle">
        <input v-model="showTrash" type="checkbox" @change="load" />
        回收站
      </label>
      <button class="btn small" @click="validateAll">校验失效</button>
      <button class="btn small" title="撤销最近一次新增/删除/恢复/修改" @click="undo">撤销</button>
      <button v-if="showTrash" class="btn small danger" @click="emptyTrash">清空回收站</button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="notice" class="notice">{{ notice }}</p>

    <section v-if="showManage" class="card manage">
      <div class="manage-col">
        <h3>分类</h3>
        <div class="manage-list">
          <div v-for="c in categories" :key="c.id" class="manage-item">
            <span class="manage-name">{{ c.name }}</span>
            <button class="btn small" @click="renameCategory(c)">重命名</button>
            <button class="btn small danger" @click="removeCategory(c)">删除</button>
          </div>
        </div>
        <button class="btn small" @click="addCategory">+ 添加分类</button>
      </div>
      <div class="manage-col">
        <h3>标签</h3>
        <div class="manage-list">
          <div v-for="t in tags" :key="t.id" class="manage-item">
            <span class="tag">{{ t.name }}</span>
            <button class="btn small" @click="renameTag(t)">重命名</button>
            <button class="btn small" @click="mergeTag(t)">合并</button>
            <button class="btn small danger" @click="removeTag(t)">删除</button>
          </div>
        </div>
        <div class="manage-actions">
          <button class="btn small" @click="addTag">+ 添加标签</button>
        </div>
      </div>
      <div class="manage-col">
        <h3>数据与设置</h3>
        <div class="settings-list">
          <label class="setting-row">
            主题
            <select v-model="theme" class="select">
              <option value="system">跟随系统</option>
              <option value="light">浅色</option>
              <option value="dark">深色</option>
            </select>
          </label>
          <button class="btn small" @click="exportBackup">导出备份</button>
          <button class="btn small" @click="importBackup">导入备份</button>
          <button class="btn small" @click="exportSnapshot">导出数据库快照</button>
          <label class="trash-toggle">
            <input v-model="autoStart" type="checkbox" @change="toggleAutoStart" />
            开机自启动
          </label>
        </div>
      </div>
    </section>

    <form v-if="showForm" class="form card" @submit.prevent="save">
      <div class="form-row">
        <select v-model="form.type" class="select">
          <option value="file">文件</option>
          <option value="url">网页</option>
        </select>
        <input v-model="form.title" class="input" placeholder="标题 / 文件名（自动识别，可修改）" />
        <!-- 文件路径只读展示：只能通过「选择文件」对话框选择/重选 -->
        <input
          v-if="form.type === 'file'"
          :value="form.pathOrUrl"
          class="input grow"
          readonly
          placeholder="尚未选择文件，点击右侧「选择文件」"
        />
        <input
          v-else
          v-model="form.pathOrUrl"
          class="input grow"
          placeholder="网址 https://…"
          @blur="fetchTitle"
        />
        <button v-if="form.type === 'file'" type="button" class="btn" @click="pickFile">
          选择文件…
        </button>
        <button v-if="form.type === 'url'" type="button" class="btn" @click="fetchTitle">
          获取标题
        </button>
      </div>
      <div class="form-row">
        <select v-model="form.categoryId" class="select">
          <option value="">未分类</option>
          <option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <input v-model="form.tags" class="input grow" placeholder="标签（逗号分隔）" />
        <label class="star-toggle">
          <input v-model="form.star" type="checkbox" />
          星标
        </label>
      </div>
      <textarea v-model="form.note" class="textarea" placeholder="备注（可选）"></textarea>
      <div class="form-actions">
        <button type="submit" class="btn primary">{{ form.id ? '保存修改' : '保存' }}</button>
        <button type="button" class="btn" @click="resetForm(); showForm = false">取消</button>
      </div>
    </form>

    <main class="list">
      <div v-if="loading" class="empty">加载中…</div>
      <div v-else-if="records.length === 0" class="empty">
        暂无记录，拖拽文件或点击「新增」开始
      </div>
      <template v-else>
        <p v-if="records.length > listLimit" class="limit-hint">
          结果过多（{{ records.length }} 条），仅显示前 {{ listLimit }} 条，请细化筛选
        </p>
        <div
          v-for="r in records.slice(0, listLimit)"
          :key="r.id"
          class="item card"
          @dblclick="cancelExpand(); open(r)"
        >
          <div class="item-main" @click="toggleExpand(r)" :style="{ cursor: r.note ? 'pointer' : 'default' }">
            <!-- 网页记录展示缓存的 favicon；文件记录按扩展名展示类型图标 -->
            <img v-if="r.type === 'url' && r.favicon" class="favicon" :src="r.favicon" alt="" />
            <span v-else-if="r.type === 'url'" class="favicon fallback">{{ (r.title || '?').charAt(0) }}</span>
            <span v-else class="favicon fallback">{{ fileIcon(r.pathOrUrl) }}</span>
            <span class="badge" :class="r.type">{{ r.type === 'url' ? '网页' : '文件' }}</span>
            <span v-if="r.invalid" class="badge invalid">失效</span>
            <span class="title">{{ r.title }}</span>
            <span class="path">{{ r.pathOrUrl }}</span>
            <span v-if="r.openCount > 0" class="opened">
              打开{{ r.openCount }}次 · {{ relativeTime(r.openedAt) }}
            </span>
          </div>
          <div v-if="expandedId === r.id && r.note" class="item-note">{{ r.note }}</div>
          <div class="item-tags">
            <span v-for="t in r.tags" :key="t.id" class="tag">{{ t.name }}</span>
          </div>
          <div class="item-actions">
            <button class="btn small" :class="{ starred: r.star }" @click="toggleStar(r)">
              {{ r.star ? '★' : '☆' }}
            </button>
            <template v-if="showTrash">
              <button class="btn small" @click="restore(r)">恢复</button>
              <!-- 回收站视图：删除改为彻底删除（软删除对已删记录无意义） -->
              <button class="btn small danger" @click="purge(r)">彻底删除</button>
            </template>
            <template v-else>
              <button class="btn small" @click="open(r)">打开</button>
              <button class="btn small" @click="openEdit(r)">
                {{ r.invalid ? '重定位' : '编辑' }}
              </button>
              <button class="btn small danger" @click="remove(r)">删除</button>
            </template>
          </div>
        </div>
      </template>
    </main>
  </div>
</template>

<style scoped>
.app {
  max-width: 920px;
  margin: 0 auto;
  padding: 20px;
  box-sizing: border-box;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 12px;
}
.brand {
  font-size: 20px;
  margin: 0 8px 0 0;
}
.search {
  display: flex;
  flex: 1;
  gap: 8px;
}
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 14px 0;
}
.trash-toggle,
.star-toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;
}
.error {
  color: var(--danger);
  margin: 8px 0;
}
/* 成功/信息提示（绿色），与错误提示区分 */
.notice {
  color: #3f9d63;
  margin: 8px 0;
}
/* 结果超限提示 */
.limit-hint {
  color: var(--muted);
  font-size: 13px;
  margin: 0;
}
.form {
  padding: 14px;
  margin-bottom: 14px;
}
.form-row {
  display: flex;
  gap: 10px;
  margin-bottom: 10px;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 10px;
}
.manage {
  display: flex;
  gap: 24px;
  padding: 14px;
  margin-bottom: 14px;
}
.manage-col {
  flex: 1;
}
.manage-col h3 {
  font-size: 14px;
  margin-bottom: 8px;
}
.manage-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
  align-items: center;
}
.manage-item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.manage-name {
  font-size: 13px;
}
.manage-actions {
  display: flex;
  gap: 8px;
}
.settings-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.setting-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.empty {
  text-align: center;
  color: var(--muted);
  padding: 40px 0;
}
.item {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 14px;
  cursor: default;
}
/* 详情展开行占满整行 */
.item-note {
  flex-basis: 100%;
  color: var(--muted);
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  background: var(--hover-bg);
  border-radius: 6px;
  padding: 8px 10px;
}
.item-main {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.title {
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.path {
  color: var(--muted);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.opened {
  color: var(--muted);
  font-size: 12px;
  flex-shrink: 0;
}
.favicon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  border-radius: 3px;
}
.favicon.fallback {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--tag-bg);
  color: var(--tag-fg);
  font-size: 10px;
  font-weight: 600;
}
.item-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.item-actions {
  display: flex;
  gap: 8px;
}
.btn.starred {
  color: var(--star);
}
</style>
