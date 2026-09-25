# LightMark 产品需求文档（PRD）

## 1. 产品概述

LightMark 是一款**本地单机**的「文件 / 网页标记管理器」。

用户在本地部署一个桌面应用，开机自启动、常驻系统托盘，通过一个简洁清爽的窗口，快速记录、分类、搜索并打开自己关心的本地文件与网页文章，实现个人知识资产的轻量收纳。

核心价值：**轻量、快、不臃肿**，本地优先、数据私有。

## 2. 目标用户与场景

- **目标用户**：个人开发者 / 知识工作者，习惯本地工具、注重隐私与响应速度。
- **典型场景**：
  - 收藏一篇值得回看的网页文章，并打上主题标签。
  - 记录一个本地 Markdown 文档路径，用 Typora 一键打开。
  - 根据文件名或备注快速检索到三个月前标记过的资料。
  - 未来通过桌宠形态的 AI 助手，对已收藏内容做问答（RAG）。

## 3. 设计原则

| 原则 | 说明 |
|---|---|
| 本地优先 | 数据只存本机，无云端依赖，可完全离线使用 |
| 轻量 | 单可执行文件，启动快、内存占用低 |
| 简单 | 打开即用，录入与检索路径最短 |
| 可扩展 | 预留 AI 接口，后续以桌宠形式接入 LangGraph / RAG |

## 4. 功能需求

### 4.1 记录管理（P0）

记录对象为两类：**本地文件** 与 **网页文章**。

**记录字段：**

| 字段 | 说明 |
|---|---|
| id | 唯一标识 |
| type | 类型：`file`（本地文件）/ `url`（网页） |
| path / url | 本地文件绝对路径，或网页 URL |
| title | 文件名 / 网页 Title |
| note | 备注（纯文本 / 支持基础 Markdown） |
| category | 所属分类 |
| tags | 标签（多选） |
| star | 星标 / 置顶 |
| created_at / updated_at | 创建 / 修改时间 |
| invalid | 失效标记（见 4.4） |

**操作：** 增、删、改、查；删除进入回收站可恢复。

### 4.2 快速录入（P0）

- **拖拽**：将本地文件拖入窗口，自动补全路径与文件名。
- **粘贴 URL**：粘贴网页链接，自动抓取网页 Title 与 favicon。
- 支持批量录入。

### 4.3 分类与标签（P0）

- **分类**：单层或树形，一条记录属于一个分类。
- **标签**：多选，一条记录可挂多个标签。
- 分类、标签支持重命名、合并、删除。

### 4.4 失效检测（P0）

- 本地文件被移动 / 删除 / 重命名，网页返回 404 或不可达，标记为「失效」。
- 检测时机：启动时 + 手动触发全量校验。
- 失效记录支持「重新定位」：选择新文件路径或更新 URL。

### 4.5 搜索与筛选（P0）

- **搜索范围**：文件名 / Title、备注、标签、URL。
- **全文搜索**：基于 SQLite FTS5，支持中文分词。
- **筛选 / 排序**：按类型、分类、标签、星标、时间排序筛选。

### 4.6 打开文件 / 网页（P0）

- 调用本机默认关联应用打开（md → Typora，pdf → 系统默认等）。
- 网页使用系统默认浏览器打开。
- 不额外开发文件打开方式。

### 4.7 回收站与撤销（P0）

- 删除记录进入回收站，可恢复或彻底删除。
- 支持撤销上一步操作。

### 4.8 备份与导入导出（P0）

- 一键导出（JSON 全量 + SQLite 快照）。
- 支持从导出的 JSON 恢复 / 导入。
- 可选：定时自动备份。

### 4.9 系统集成（P0）

- 系统托盘常驻，最小化到托盘。
- 开机自启动（Windows 注册表 HKCU `Run` 键，无需管理员权限）。
- 托盘菜单：打开主窗口、退出。

### 4.10 界面要求（P0）

- 页面简洁、清爽，配色舒服、舒畅。
- 信息密度适中，列表 + 详情布局。
- 支持浅色 / 深色主题（可选）。

### 4.11 增强功能（P1）

- 最近访问 / 打开历史。
- 网页 favicon / 缩略图缓存，列表可视化。
- 重复检测：同 URL / 同路径录入时提示去重。
- 文件类型图标。

### 4.12 AI 小助手（预留，接口先行）

- 后端预留 `REST + WebSocket` 接口，供未来桌宠调用。
- 形态：桌宠（桌面悬浮层，独立 WebView）。
- 技术栈（后续）：LangGraph 编排 + RAG。
- 前置依赖：见「附录 B」——当前 Ollama 缺少 embedding 模型，需补拉。

## 5. 非功能需求

| 类别 | 要求 |
|---|---|
| 性能 | 万条级记录下搜索 < 200ms；冷启动 < 2s |
| 轻量 | 单可执行文件；内存占用低，不常驻重服务 |
| 安全 | 本地 API 仅绑定 `127.0.0.1`，不暴露局域网 |
| 可靠性 | 数据本地持久化，崩溃可恢复；支持备份 |
| 兼容 | Windows 10 / 11（依赖 WebView2，系统一般自带） |

## 6. 技术方案

### 6.1 总体架构

- **桌面壳**：Wails v2（Go 后端 + WebView 窗口 + 系统托盘），编译为单个 `.exe`。
- **后端**：Go，内置本地 HTTP 服务，仅监听 `127.0.0.1`。
- **前端**：Vue3 + Vite（轻量 UI，不引入重型组件库）。
- **存储**：SQLite + FTS5，单文件，存放于用户数据目录。
- **搜索**：FTS5 全文索引 + 中文分词（jieba-go，或 simple tokenizer 边缘处理）。
- **AI 预留**：后端 `REST + WebSocket`；RAG 用 `sqlite-vec` 做向量库，LangGraph 编排。

### 6.2 技术选型对比

| 项 | 选型 | 备选 / 说明 |
|---|---|---|
| 窗口框架 | Wails | 单二进制，满足 Go 后端 + 桌面窗口；放弃 Tauri（Rust 后端与 Go 冲突） |
| 数据库 | SQLite | 不用 Docker 里的 pg/mysql/mongo，避免容器在线依赖 |
| 向量库 | sqlite-vec | 后续 RAG 用；与 SQLite 同库，零运维 |

### 6.3 目录结构

```
light_mark/
├─ go.mod / main.go        # Wails 入口、窗口、托盘、自启
├─ internal/
│  ├─ store/               # SQLite 初始化、迁移、DAO
│  ├─ search/              # FTS5 索引 + 中文分词
│  ├─ fileops/             # 文件/URL 打开、title/favicon 抓取、失效校验
│  └─ api/                 # REST + WebSocket（含 AI 预留）
├─ frontend/               # Vue3 + Vite
└─ PRD.md
```

## 7. 数据模型（概要）

```sql
CREATE TABLE records (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL,            -- 'file' | 'url'
  path_or_url TEXT NOT NULL,            -- 本地路径 或 URL
  title       TEXT NOT NULL,            -- 文件名 / 网页 Title
  note        TEXT,                     -- 备注
  category_id TEXT,                     -- 关联分类
  star        INTEGER DEFAULT 0,
  invalid     INTEGER DEFAULT 0,
  created_at  INTEGER,
  updated_at  INTEGER
);

CREATE TABLE tags (
  id   TEXT PRIMARY KEY,
  name TEXT UNIQUE
);

CREATE TABLE record_tags (
  record_id TEXT,
  tag_id    TEXT
);

CREATE TABLE categories (
  id       TEXT PRIMARY KEY,
  name     TEXT,
  parent_id TEXT,                       -- 树形分类
  sort     INTEGER
);

-- FTS5 全文索引表
CREATE VIRTUAL TABLE records_fts USING fts5(
  title, note, tags, url, content='records'
);
```

## 8. 接口设计概要（含 AI 预留）

| 模块 | 接口 | 说明 |
|---|---|---|
| 记录 | `GET/POST/PUT/DELETE /api/records` | CRUD |
| 搜索 | `GET /api/search?q=` | FTS5 全文搜索 |
| 分类标签 | `GET/POST/PUT/DELETE /api/categories`、`/api/tags` | 分类标签管理 |
| 录入辅助 | `POST /api/meta/fetch?url=` | 抓取网页 Title / favicon |
| 失效检测 | `POST /api/records/validate` | 全量 / 单条校验 |
| 备份 | `GET /api/export`、`POST /api/import` | 导入导出 |
| AI 预留 | `WS /ws`、`POST /api/ai/chat` | 桌宠接入点 |

## 9. 里程碑

1. **M1 骨架**：Wails 初始化、SQLite 建表迁移、CRUD API、最小列表页。
2. **M2 核心体验**：拖拽/粘贴录入、搜索 + 筛选、分类标签、本机打开。
3. **M3 可靠性**：失效检测、回收站、备份导入导出、托盘 + 自启。
4. **M4 打磨 + AI 预留**：favicon、最近访问、WebSocket 接口桩。

## 10. 风险与注意事项

- **中文搜索分词**：FTS5 对中文需额外处理，否则「项目文档」搜「文档」命中不了，M2 必须验证。
- **WebView2 依赖**：Windows 10/11 一般自带，需在目标机器确认。
- **embedding 模型缺失**：做 RAG 前必须 `ollama pull nomic-embed-text`（或 `bge-m3`）。
- **端口占用**：本地 API 采用固定端口 + 冲突检测，或随机端口。

## 附录 A：环境信息（Docker 数据库）

本机 Docker 已运行 `mongo:6.0`、`postgres:15`、`redis:7.2`、`mysql:8.0`。**本项目不使用这些数据库**，统一采用 SQLite，避免引入容器在线依赖。

## 附录 B：环境信息（Ollama 模型）

```bash
ollama list
# qwen3-vl:8b                  6.1 GB   # 视觉模型，可做图片理解
# qwen3.5:9b                   6.6 GB
# qwen3:14b                    9.3 GB   # 对话 / 总结
# deepseek-r1:14b              9.0 GB   # 推理
# qwen2.5:7b                   4.7 GB
# qwen2.5:14b-instruct-q4_K_M  9.0 GB
# minicpm-v:8b                 5.5 GB   # 视觉
# qwen2.5-coder:14b            9.0 GB   # 代码
```

**缺口**：当前无 embedding 模型，RAG 向量化前需补拉 `nomic-embed-text` 或 `bge-m3`。
