# Clowder — LLM 聚合代理设计文档

## 命名

**clowder** — 英文「一群猫」的专用量词。聚合 = 一群猫聚在一起。

## 目标

聚合所有免费/试用 LLM provider，暴露统一的 OpenAI-compatible API，自动处理 key 轮换、故障转移、provider 切换。

---

## 架构总览

```
Client (OpenAI SDK / curl)
  │
  ▼
┌─────────────────────────────────────────────┐
│         clowder proxy (Go, HTTP)             │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │         Model Router                 │   │
│  │  - model → providers 映射            │   │
│  │  - auto: 选最快健康 provider         │   │
│  │  - 失败 fallback: key→provider       │   │
│  └──────────────┬───────────────────┬───┘   │
│                 │                   │        │
│  ┌──────────────▼───┐  ┌────────────▼────┐  │
│  │  ftl pool        │  │  [future] pool  │  │
│  │  sequential      │  │  round-robin    │  │
│  │  ↓ register.py   │  │  ↓ register.py  │  │
│  │  ↓ check.py      │  │  ↓ check.py     │  │
│  └──────────────────┘  └─────────────────┘  │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │  SQLite (key 池 + 请求统计)          │   │
│  └──────────────────────────────────────┘   │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │  Web Dashboard (Go embed)            │   │
│  │  - 公开看板（public 开关）          │   │
│  │  - 管理面板（admin key 登录）       │   │
│  └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

---

## Provider 外挂脚本机制

### 目录结构

```
providers/
├── featherless/
│   ├── manifest.yaml      # provider 元信息
│   ├── pyproject.toml     # uv 项目定义
│   ├── register.py        # 注册脚本
│   ├── check.py           # key 健康检查
│   └── .python-version    # python 版本（如用 uv）
├── [future-provider-1]/
│   └── ...
└── [future-provider-2]/
    └── ...
```

### manifest.yaml 格式

Provider 不再区分 script/static 类型。注册、check 和手动 key 导入分别由 manifest 脚本与数据库决定。

**注册脚本 provider**（批量注册试用账号，如 featherless）

```yaml
name: featherless
display_name: Featherless.ai
description: Featherless.ai free trial accounts
base_url: https://api.featherless.ai
enabled: true
format: openai-compatible

key_strategy: sequential   # sequential（用完再换）| round-robin

# 模型黑白名单（白名单优先于黑名单；同时命中时保留）
# 模型列表优先实时拉取 /models：拉到 → 按黑白名单过滤；
# 拉不到 → fallback 到白名单；无白名单 → 模型列表置空（不参与路由）
model_whitelist:
  - deepseek-ai/DeepSeek-V4-Pro
  - moonshotai/Kimi-K2.6
  - zai-org/GLM-5.1
  - MiniMaxAI/MiniMax-M2.7
  - Qwen/Qwen3.6-27B
model_blacklist: []

# 脚本命令（按需配置）
scripts:
  register: "uv run register.py"
  check: "uv run check.py"
  register_timeout: 300   # 秒，超时 kill 整个进程树，默认 300
  check_timeout: 60       # 秒，默认 60

# 路由偏好
routing:
  priority: 100  # 越小越优先，默认 100，低优先级不死不调用高优先级
  weight: 1      # 同优先级下按权重分配
  hedge: false   # TTFT 超时切换时是否参与双发；源/目标 provider 都为 true 才双发，否则取消+切换

# 规则（规则引擎语法，自动限定本 provider 作用域；全局规则见 rules.yaml）
rules:
  - name: trial-ended
    when: { status_code: 403, body_contains: "trial ended" }
    do: [ { mark_key: invalid } ]
    route: next_key
  - name: rate-limit
    when: { status_code: 429 }
    do: [ { mark_key: rate_limited } ]
    route: next_key
```

脚本执行约束：同一 provider 的 register 串行执行（避免触发反滥用），不同 provider 可并行。register 失败（非 0 退出 / 超时 / 输出不合法）按 provider 维度**指数退避重试**：间隔 `base × 2^连续失败次数`，上限 `pool.register_backoff_max_seconds`（0 = 不设上限），成功一次清零计数；退避期间该 provider 的自动注册暂停，手动 `clowder register` 不受限制（会重置退避计时）。

**手动 key provider**（如 OpenRouter、Groq 等）

```yaml
name: openrouter
display_name: OpenRouter
base_url: https://openrouter.ai/api/v1
enabled: true
format: openai-compatible

key_strategy: round-robin  # round-robin | sequential

# key 通过 `clowder import <provider> <file>` 或管理后台添加，导入后存入数据库。
# 运行时不会自动读取 providers/openrouter/keys.txt。

routing:
  priority: 100
  weight: 1
```

### 注册脚本协议

**register.py**：
- 输入：无（或通过环境变量传入代理配置）
- stdout：JSON 单行（必须api_key + 可选 metadata）
- 退出码 0 = 成功，非 0 = 失败（stderr = 错误信息）

```json
{
  "api_key": "ftl_xxxx",
  "metadata": {
    "email": "user@example.com",
    "password": "secret",
    "tokens_total": 100000,
    "tokens_used": 12345,
    "trial_end": "2026-06-14T00:00:00Z"
  }
}
```

**check.py**：
- 输入：`sys.argv[1]` = api_key
- stdout：JSON 单行（status + 可选 metadata）
- 退出码 0 = 成功

```json
{
  "status": "active",
  "metadata": {
    "tokens_total": 100000,
    "tokens_used": 12345,
    "trial_end": "2026-06-14T00:00:00Z"
  }
}
```

status 取值：`active` | `invalid` | `rate_limited` | `error`

---

## 错误处理：规则引擎（IFTTT-style）

所有错误处理由统一的规则引擎驱动。规则分三层来源：

1. **内置规则**（不可删，priority 5000，无 capture）：`transport: dns_failure / conn_refused / tls_error` → provider 级 unhealthy + `next_provider`（比通用 default-transport-retry 更优先，可被用户规则覆盖）
2. **默认规则**（随发布写入 rules.yaml，可删可改）：见下表
3. **用户规则**：手写 / 错误聚合页一键生成 / 预设规则入口添加（新建默认 priority 100、route 默认 passthrough）

| 默认规则 | when | route | priority | capture |
|---------|------|-------|----------|---------|
| default-4xx-passthrough | status_code: 4xx | passthrough | 8000 | 响应侧 |
| default-5xx-retry | status_code: 5xx | next_provider | 8000 | 响应侧 |
| default-transport-retry | transport: true（任意传输错误） | next_provider | 8000 | 响应侧 |
| default-ttft-timeout | ttft_over_ms: 7000 | next_provider | 8000 | 响应侧 |
| default-catch-all | error: true（任意错误） | passthrough | 10000 | 响应侧 |

（「响应侧」= `capture: { fields: [response_headers, response_body, status_code] }`）

auto 下 `next_provider` = 按 TTFT 排序的下一个 (provider, model) 组合；具体 canonical 模型 = 下一个支持该模型的 provider。

**错误聚合（无需 record 字段）**：每次失败 attempt 都往 requests 表写 `error_fingerprint`，错误聚合面板直接按指纹**全量聚合**，可按命中规则筛选（只看兜底规则命中的 = 未分类错误）；规则的 `capture` 动作只决定是否保存报文样本。默认兜底规则都带响应侧 capture，未分类错误自带样本，「聚合观察 → 一键补规则」闭环不变；补了具体规则后想继续留样本，在新规则上声明 capture 即可。

### 规则结构

一条规则 = **when（触发器） + if（可选条件） + do（副作用动作，异步并行） + route（路由决策，仅上游结果为错误时生效）**，另有可选 `description`（描述）、`priority`（默认 100，越小越优先）。新建规则 route 默认 `passthrough`。

```yaml
# rules.yaml（全局，admin 页可编辑，聚合页一键生成的规则也写入这里）
rules:
  - name: ftl-trial-ended
    description: "featherless 试用到期，烧 key 换下一个"   # 可选
    priority: 100                  # 可选，默认 100
    when:                          # 触发器 1：上游结果（字段直接写在 when 下，无包装键）
      provider: featherless        # 可选作用域（省略 = 所有 provider）
      status_code: 403
      body_contains: "trial ended"
    do:                            # 副作用动作，后台并行执行，不阻塞请求主线
      - mark_key: invalid
    route: next_key                # 请求主线：换 key 继续

  - name: quota-maybe-exhausted
    when: { status_code: 401, body_contains: "quota" }
    do:
      - check_key: { chain: true } # 对当前 key 跑 check；chain: true = 结果回灌引擎，
                                   # 命中 when:check 规则执行其预定义的后续动作
    route: next_key

  - name: model-gone
    when: { status_code: 404 }
    do:
      - mark_model_unhealthy: {}
      - refresh_models: {}         # 顺便刷新该 provider 模型列表
    route: next_provider

  - name: too-many-key-switches    # if 条件示例：同链换 key 超 2 次后改换 provider
    when: { status_code: 429 }
    if:
      key_switches: { gte: 2 }
    route: next_provider

  # 触发器 2：check 结果。when:check 规则 = check 后续动作的集中预定义（唯一定义点），
  # 其他规则里的 check_key 通过 chain 开关决定是否联动执行这些后续动作
  - name: on-check-invalid
    when:
      check: { status: invalid }
    do:
      - mark_key: invalid

  - name: on-check-rate-limited
    when:
      check: { status: rate_limited }
    do:
      - mark_key: rate_limited

  # 触发器 3：定时任务（cron 表达式）
  - name: nightly-audit
    when:
      schedule: { cron: "0 4 * * *" }
      scope: { provider: featherless }   # 可选，省略 = 全部 provider
    do:
      - check_keys: { chain: true }      # scope 内全部 key 跑 check，结果逐个回灌
      - refresh_models: {}

  # 预设规则（通过规则页「添加预设」入口加入，可删可改）：429 指数退避
  - name: preset-429-backoff
    description: "429 指数退避后换 key"
    when: { status_code: 429 }
    do:
      - backoff_key: {}                  # 特殊内置动作，见下
    route: next_key
```

**触发器（when，三种）**：
- **上游结果**（默认形态：匹配字段直接写在 when 下，无包装键）：每次上游 attempt 结束（成功或失败）都进引擎匹配。字段：provider / model / status_code（精确值如 403，或 4xx / 5xx 通配）/ body_contains / body_regex / transport（true = 任意传输错误，或具体类别 dns_failure / conn_refused / tls_error / connect_timeout / stream_cut / malformed）/ ttft_over_ms（毫秒）/ error（true = 任意错误结果：非 2xx、传输错误、TTFT 超时）。**结局互斥**：一次 attempt 只有一种结局（TTFT 超时 / 传输错误 / 收到响应 / 任意错误），故 `ttft_over_ms`（上游未返回首字节）与 `status_code` / `body` / `transport` / `error`（已出结局）以及另一个 `ttft_over_ms` 全部互斥，AND 语义下永不命中——可视化编辑中互斥字段灰显禁用，校验告警。唯一携带请求主线的触发器：**结果为错误时 route 生效；成功响应命中时 route 忽略（即透传——此时首字节已写出，重试窗口已关），只执行 do**。成功响应的 body 匹配走 **tee 旁路**：仅当存在可能命中成功响应的规则（无 error / transport / ttft_over_ms / 4xx / 5xx 条件）时才镜像响应体到内存 buffer（上限 `capture.response_buffer_kb`，超出截断，截断部分匹配不到），流结束后**异步**匹配，不阻塞透传；无此类规则时零开销
- `check`：check 脚本返回某状态。来源不限：定时 check、手动一键 check、其他规则 do 里 `chain: true` 的 check。`when: check` 规则即 **check 后续动作的集中预定义**；`chain: false` 则只记录结果不回灌
- `schedule`：定时触发（cron 表达式），可限定 provider / key 作用域

check / schedule 触发没有请求主线，`route` 字段忽略。

**条件（if，可选）**：基于重试链上下文的数值比较，when 命中且 if 全满足才算命中，否则继续匹配下一条：
- `attempt_no`：链内第几次尝试
- `key_switches`：本链已换 key 次数
- `provider_switches`：本链已换 provider 次数
- 运算符：`eq / ne / gt / gte / lt / lte`

**副作用动作（do，可多个，并行）**：
- `mark_key: invalid | rate_limited | active`（永久禁用 / 速率退避 / 恢复）
- `mark_model_unhealthy` / `mark_provider_unhealthy`
- `refresh_models`：重拉该 provider 模型列表
- `check_key` / `check_keys`：跑 check；`chain: true` 时结果回灌引擎触发 `when: check` 规则
- `capture`：抓取本次请求报文（**抓包的唯一入口，无全局开关**）；可选 `fields` 裁剪（request_headers / request_body / response_headers / response_body / status_code，默认全抓）。仅上游结果触发有报文可抓，check / schedule 下为 no-op（校验时警告）。抓到的包同时挂「命中规则」与「错误指纹」两个维度，规则页按规则查看、聚合页按指纹查看
- `backoff_key`（特殊内置动作）：**先读**该 key 当前 `rate_limit_count`，计算退避时长 `base × 2^count`（上限 `backoff.max_seconds`，0 = 不设上限），标记 rate_limited 至到期时间并 count+1；**若 key 已处于退避中（rate_limit_until 未到期）则为 no-op：不叠加 count、不延长退避**（同一波并发 429 只推进一档）；该 key 后续成功一次请求即清零 count

**路由决策（route，单选或 race 多发）**：
- `next_key`：同 provider 换 key 重试
- `next_provider`：换 provider（auto 下 = 下一个 TTFT 最快的 (provider, model) 组合）
- `retry_same`：原 key 重试（配合退避）
- `passthrough`：透传上游响应给客户端，不重试（**新建规则的默认 route**）。**仅适用于上游已返回（status_code / body 等）的场景**；TTFT 超时（上游未返回）无响应可透，不可选 passthrough（可视化编辑灰显禁用、校验告警），应改用 next_key / next_provider / retry_same / wait。**链路全部走完仍无成功时，clowder 在兜底出口对 TTFT / 连接超时合成 504（JSON，注明超时的 provider/model）返回客户端**
- `wait`（继续等待）：继续等待上游首字节，不切换 / 不透传。**仅适用于含 TTFT 超时（ttft_over_ms）条件的上游触发器**——命中时复用 hedge 的「慢路不自动取消」机制（dial 预判 `willWaitOnTTFT`），取消默认的 TTFT 自动取消，让上游继续跑；when 不含 ttft_over_ms（无可等的超时场景）或为 status_code / body / transport / error 等已出结局条件时不可选 wait（与 passthrough 互补，灰显禁用、校验告警）
- `race: [retry_same, next_key]`：多路并发，复用 hedge 的双发机制（`route` 可写简写列表 `[retry_same, next_key]` 或等价的 `{race: [...]}`）

**race 双发语义**：
- 仅在重试窗口内合法（首字节未写出给客户端）；首字节先到者赢，其余路立即取消
- 每路各算一次 attempt，计入 `max_retries`；单请求在途路数上限 `max_parallel_attempts`（默认 2，超出部分顺序降级）
- 某路失败时照常进规则引擎：**do 照常执行**（标记类副作用不浪费）；但若仍有其他在途路，其 route 挂起不执行（避免重试指数扩散），全部路都失败后才按最后一路的 route 继续
- 消耗多倍配额，默认不用，由规则显式声明

**并行语义**：route 决定请求主线（同步，立即执行）；do 列表扔进后台 goroutine 并行跑，互不阻塞——「一个走标记流程、一个继续试下一个模型」天然成立；需要主线级并发时用 `race`。

**匹配语义**（所有来源的规则入同一池，命中即停，只执行一条）：
1. 候选 = when 类型相同且全部匹配字段命中、if 全满足的规则（manifest `rules` 自动附加 provider 条件）
2. **priority 小者胜**（默认 100，越小越优先）。内置具体 transport 规则 priority 5000，比通用 `default-transport-retry`(8000) 更优先，又可被用户规则(默认 100)覆盖；用 priority 显式表达「越具体越优先」，不再用特异性计数
3. priority 相同 → 按来源顺序：manifest rules → rules.yaml → 内置
4. 仍相同（来源也相同）→ 按定义顺序（先定义者胜）

默认兜底规则（8000/10000）条件最宽、priority 最大，天然垃圾收集一切未被更具体规则命中的错误。

**规则校验（保存时 + 规则页持续检查）**：
- `body_regex` 在保存时编译（Go regexp / RE2），无效 pattern 直接报错拒绝保存，不留到运行时
- 两条规则**触发条件（when + if）完全相同且 priority 也相同** → 警告（匹配结果取决于来源/定义顺序，行为不可预期）。触发条件不同的规则即使 priority 相同也不冲突（匹配不同事件）
- 规则引用 `check_key` / `check_keys` 但作用域内某 provider 未定义 check 脚本 → 警告「该规则对 provider X 不会执行」
- check / schedule 触发器下使用 `capture`（无报文可抓）→ 警告
- 规则引用的 provider / model 不存在（已删除或被黑白名单排除）→ 规则页标记为无效规则并警告
- 删除 provider / 修改黑白名单时，先提示将影响哪些规则，**可勾选一并删除这些规则**，确认后才生效

### 三维路由语义（route 的效果）

1. **next_key**：同 provider 换 key，不换 provider
2. **next_provider**：跳过当前 provider（auto 下按 TTFT 跳下一个组合）
3. **全 key 失败**：自动标记该 provider 的该 model 为 unhealthy；auto 下某 provider 内所有 model 均死 → provider 标记死
4. **两层健康模型**：
   - **provider 级**：DNS / 拒连 / 证书错误等基础设施故障 → 整个 provider unhealthy
   - **model 级**：只影响单个模型的故障（如 404、全 key 失败）→ 仅 (provider, model) unhealthy，不拖累其他模型
5. **健康状态机**（provider 级与 model 级各自独立运转）：

```
[healthy] ──连接失败─────→ [unhealthy: 30s]
[healthy] ──全key失败────→ [unhealthy: 60s]
[unhealthy] ──时间到─────→ [probing]
[probing] ──1次成功──────→ [healthy]
[probing] ──1次失败──────→ [unhealthy: 2× backoff, max 5min]
```

### Key 选择与并发语义（sequential）

- 内存中维护 per-provider 的 current key 指针，SQLite 只做持久化，不做并发协调。
- key 切换走 **singleflight**：同一时刻只有一个 goroutine 执行切换，其余并发请求等待并复用结果，避免一波并发 401 烧掉一串 key。
- 「用完」判定：规则 `mark_key: invalid` = 永久用完；`mark_key: rate_limited` = 临时退避，**不算用完**，退避期间临时跳到下一个 key，退避结束后指针可回到原 key。

---

## 流式生命周期与重试窗口

请求分三个阶段，重试能力逐级收窄：

```
[连接期] ──────→ [首字节前] ──────→ [流传输期]
 可自由重试        可自由重试          不可重试
（换 key/provider）（换 key/provider）  （只能断流）
```

1. **重试窗口 = 向客户端写出首字节之前**。收到上游首个 SSE chunk 前，失败可按规则引擎的 route 决策换 key / 换 provider。
2. **首字节写出后**上游断流：向客户端发送 error event 后断开，不做重试（HTTP 状态码已发出，无法回退）。
3. **TTFT 超时**：由默认规则 `default-ttft-timeout`（`ttft_over_ms: 7000`，可改可删）驱动：
   - 默认行为：**取消**原上游请求，`next_provider` 切换。
   - **hedge 双发**：仅当原 provider 与切换目标 provider 的 manifest 都声明 `hedge: true` 时，不取消原请求而是双路并发，取最先返回首字节的一路，另一路立即取消。用低延迟换配额消耗，默认关闭。
4. **非流式客户端请求**：默认以流式向上游发起、聚合完整后返回给客户端（`upstream_streaming`，全局配置 + provider 可覆写，默认开），用于统一采集 TTFT 数据。

---

## Model 规范化（Canonical Name）

客户端请求的 model 名需要映射到各 provider 的实际模型名。

### 全局映射表（models.yaml）

独立配置文件维护（不进代码，改模型无需重编译）。每个 canonical name 对应一组**别名数组**（不绑定具体 provider）：某 provider 的模型列表中任一模型名命中别名，即视为该 provider 支持此 canonical model。匹配顺序：**精确匹配 → 正则匹配**，都不命中返回 404（错误信息中提示最接近的候选）。**禁用相似度模糊匹配**，避免静默路由到错误模型。

```yaml
# models.yaml
deepseek-v4-pro:
  - deepseek-ai/DeepSeek-V4-Pro
  - accounts/fireworks/models/deepseek-v4-pro
  - deepseek/deepseek-chat

kimi-k2.6:
  - moonshotai/Kimi-K2.6
  - moonshot/kimi-k2

# 正则规则（精确匹配未命中时按顺序尝试）
patterns:
  - match: "^deepseek.*v4.*"
    canonical: deepseek-v4-pro
```

### 模型列表来源与黑白名单

优先实时拉取 /models（启动时 + 定期刷新），经 provider 的黑白名单过滤后生效：
- `model_whitelist` 非空：取拉取结果 ∩ 白名单；白名单优先于黑名单（同时命中两者的模型保留）
- 仅有黑名单：拉取结果 − 黑名单
- 拉取失败：fallback 到白名单作为模型列表；无白名单 → 模型列表置空（该 provider 不参与路由）
- 修改黑白名单 / 删除 provider 前，提示将影响哪些规则，可勾选一并删除（见规则校验）

### /v1/models 端点

返回 canonical name 聚合列表（去重）+ `auto`，只包含当前至少有一个健康 provider 支持的模型。不暴露 provider 原始模型名。

---

## Model 路由策略

### v1：纯可用性 + 延迟

「延迟」统一指 **TTFT**（依赖 `upstream_streaming` 聚合模式采集）。

1. 收到请求，解析 `model` 字段
2. `model="auto"`：
   a. 收集所有健康的 (provider, model) 组合（两个维度都健康）
   b. 按 TTFT 排序选最快的组合，冷启动同下
   c. 选出 provider → 按 `key_strategy` 选 key → 发请求时 model 用该 provider 的原生模型名
3. `model=具体 canonical 名`：查规范化映射，找出所有支持该 model 的健康 provider，按 `priority` 排序（同优先级按 `weight` + TTFT）
4. 选中 provider → 按 `key_strategy` 选 key（sequential / round-robin）→ 转发
5. 失败按规则引擎 route 决策重试（同 provider 换 key → 换 provider → auto 下换组合），重试次数上限 `max_retries`
   - auto 且 model 级失败：跳下一个 TTFT 最快的 (provider, model) 组合
   - auto 且某个 provider 内所有 model 均失败：该 provider 标记为 provider 级 unhealthy

**冷启动**（无 TTFT 数据时）：全部无数据 → 随机选；部分有数据 → 优先选**无数据且健康**的 provider/model（探索优先，尽快补齐数据）。

### v2（未来）：复杂度路由

集成轻量级分类器（如 RouteLLM 的 MF router 或 semantic-router），在 2ms 内判断 prompt 复杂度，将简单请求路由到小模型、复杂请求路由到大模型。

---

## 数据存储（SQLite）

```sql
-- Key 池
CREATE TABLE keys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider    TEXT NOT NULL,
    api_key     TEXT NOT NULL UNIQUE,
    email       TEXT,
    password    TEXT,
    status      TEXT NOT NULL DEFAULT 'idle',  -- idle|active|invalid|rate_limited
    rate_limit_until TEXT,         -- 临时禁用到期时间
    rate_limit_count INTEGER DEFAULT 0, -- 速率限制触发次数（用于指数退避，成功一次请求即清零）
    first_request_at TEXT,
    last_used_at    TEXT,
    created_at      TEXT DEFAULT (datetime('now')),
    metadata    TEXT            -- JSON string (tokens_total, tokens_used, trial_end, etc.)
);

-- 请求日志（一行 = 一次上游 attempt；同一客户端请求的重试链共享 trace_id）
CREATE TABLE requests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id    TEXT NOT NULL,   -- 客户端请求唯一 ID，重试链按此分组
    attempt_no  INTEGER NOT NULL DEFAULT 1, -- 链内第几次尝试
    key_id      INTEGER,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    status_code INTEGER,
    ttft_ms  INTEGER,
    latency_ms INTEGER, -- 端到端延迟
    matched_rule TEXT,          -- 命中的规则名（重试链/日志页展示），成功为 NULL
    error_msg   TEXT,
    error_fingerprint TEXT,     -- 错误指纹（聚合用），成功为 NULL
    created_at  TEXT DEFAULT (datetime('now'))
);
CREATE INDEX idx_requests_trace ON requests(trace_id);
CREATE INDEX idx_requests_fingerprint ON requests(error_fingerprint);

-- 抓包（capture 动作产生，统一存储；按规则与指纹两个维度查询）
CREATE TABLE captures (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  INTEGER NOT NULL,  -- 关联 requests.id
    matched_rule TEXT NOT NULL,    -- 命中的规则名（规则页按此查看抓包）
    fingerprint TEXT,              -- 错误指纹；成功响应的抓包为 NULL
    provider    TEXT NOT NULL,
    model       TEXT,
    request_headers  TEXT,         -- JSON
    request_body     TEXT,
    response_headers TEXT,         -- JSON
    response_body    TEXT,
    redacted    INTEGER DEFAULT 0, -- 是否已脱敏
    created_at  TEXT DEFAULT (datetime('now'))
);

-- Provider 健康快照（内存为主，可选持久化）
-- 两层健康：model='' 表示 provider 级状态，否则为 (provider, model) 级状态
CREATE TABLE provider_health (
    provider   TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    status     TEXT,            -- healthy|unhealthy|probing
    avg_ttft_ms INTEGER,
    error_count    INTEGER DEFAULT 0,
    last_error_at  TEXT,
    last_success_at     TEXT,
    PRIMARY KEY (provider, model)
);
```

---

## 配置文件

`~/.clowder/config.yaml`：

```yaml
bind_addr: "0.0.0.0"
port: 12100
api_key: "clowder-local-<random>"

# Web 面板：公开看板与管理面板独立开关
# public: 公开只读看板；false 且 admin 开启时，看板页面也需登录后才能看
dashboard:
  public: true
  admin: true                 # 管理面板（admin_key 登录解锁）
  admin_key: "clowder-admin-<random>"

providers_dir: "providers/"   # 相对于 binary 或绝对路径

# 全局 key 池配置，支持提供商配置覆写
pool:
  min_active_keys: 5         # 某提供商keys低于此数触发自动注册至max_total_keys
  max_total_keys: 20         # 总 key 数上限
  auto_register: false       # 是否自动注册
  register_interval: 300     # 自动注册检查间隔（秒）
  register_backoff_base_seconds: 60   # 注册脚本失败退避 = base × 2^连续失败次数，成功清零
  register_backoff_max_seconds: 3600  # 退避上限；0 = 不设上限

# 路由配置（TTFT 超时由默认规则 default-ttft-timeout 控制，不在此配置）
routing:
  max_retries: 10              # 单请求切换模型/key/提供商的最大次数
  max_parallel_attempts: 2    # race 双发时单请求并发在途路数上限
  upstream_streaming: true    # 非流式请求也以流式向上游发起并聚合返回（用于采集 TTFT），provider 可覆写

# backoff_key 动作参数
backoff:
  base_seconds: 5             # 退避时长 = base × 2^rate_limit_count
  max_seconds: 600            # 退避时长上限；0 = 不设上限

# 抓包存储策略（抓不抓、抓哪些字段完全由规则的 capture 动作决定）
capture:
  redact: false               # 脱敏开关：开启后 key 类字段只留前 8 位；默认完整存
  max_records: 1000           # 环形保留条数；淘汰时每个错误指纹至少保留 1 条
  response_buffer_kb: 256     # 成功响应 body 匹配的 tee 镜像内存上限，超出截断（截断部分匹配不到）
```

所有配置项均可在管理面板设置页在线调整：改动**写回对应 yaml 并触发 reload**，保证文件始终是唯一事实来源。

---

## CLI

```bash
clowder start                    # 启动代理（后台）
clowder stop                     # 停止
clowder status                   # 状态一览
clowder register <provider> [N]  # 手动注册
clowder keys [provider]          # 列出 key
clowder providers                # 列出 provider 及状态
clowder reload                   # 热加载配置 + 重新扫描 provider
clowder dashboard                # 启动 web 看板（也可随 start 启动）
```

---

## Web 看板

### 公开页面 `/dashboard`

`dashboard.public` 控制公开性：开启时无需认证；**关闭且管理面板开启时，看板也需登录后才能看**；两者都关 = 不提供 Web UI。公开状态下的只读 `/api/*` **只返回模型 / 路由 / 提供商状态数据，绝不返回 api_key / email / password 等敏感字段**。`/v1/*` 代理端点始终需 `api_key` 认证。

- 各 provider 状态卡片（名称、健康状态、活跃 key 数、平均 TTFT）
- **模型路由图**：左侧模型卡片（图标 + canonical name），右侧对应 provider（图标 + 实际模型名），SVG 曲线连接，当前最优 provider 的连线加粗；每个模型独立连自己的 provider 集合
- **auto 路由顺序**：当前 auto 模式下的 top 3 provider 排序展示
- 近 24h 请求量/成功率/延迟趋势图（v2）

### 管理面板（admin key 认证）

与看板同一个 SPA：右上角登录（输入 config 中的 `dashboard.admin_key`，签发 session cookie）后导航栏多出管理页面（无独立 `/admin` 入口）。管理类 `/api/admin/*` 接口校验 session。Clerk 等第三方认证延后，不在当前计划。

- Key 管理：列表、手动注册、禁用/启用
- **Provider 详情页**：启用/禁用、编辑优先级；key 列表（状态 / metadata / 最近使用）；**注册机状态**（上次注册时间、注册耗时、成功/失败）；**一键 check 单个 key**（结果实时显示，同时进规则引擎的 check 触发器）
- 请求日志：分页查看；**点开单条请求展示完整重试链**（按 trace_id 聚合：每次 attempt 的 provider / model / key / 命中规则 / 耗时，时间线展示）
- **错误聚合**：按「provider + status + 错误体指纹」归并同种错误（**全量**：每次失败 attempt 都记指纹，无需规则声明），每组显示 provider、出现过的模型列表、命中规则、首次/最近出现时间、累计次数；**可按命中规则筛选**（只看兜底规则命中的 = 未分类错误）；点开可查看该组抓包样本（命中规则带 capture 才有样本）；**一键「添加为已知错误」**：从指纹预填 when（provider + status_code + body 特征），选择 do（标记 key 永久禁用 / 速率限制 / model 死等）和 route，写入 rules.yaml + reload
- **抓包查看**：capture 动作抓到的包统一存储；错误聚合页按指纹看样本，规则页按规则看该规则抓到的包
- **规则管理**：rules.yaml 可视化编辑（when / if / do / route / priority / description）；每条规则可展开查看其抓到的包；**添加预设入口**（按钮弹窗列出预设规则，与现有规则完全相同的置灰不可重复添加）；**规则校验警告**（特异性+优先级冲突、check 缺失、引用不存在的 provider/model 等无效规则高亮提示）
- **设置页**：**所有 yaml 配置项可视化编辑**（config.yaml 全量 + models.yaml 映射；rules.yaml 由规则页负责、provider manifest 由 provider 详情页负责），改动写回对应文件 + reload

**错误指纹算法**：错误响应体去掉数字 / UUID / 时间戳等变量部分后归一化哈希，结合 provider + status code 作为分组键。

技术栈：Go `embed` 内嵌构建产物（`dist/`）+ Vue 3 / shadcn-vue (Lyra) + pnpm + `/api/*` JSON API

---

## 技术决策

| 项目 | 决策 |
|------|------|
| 语言 | Go（单 binary 部署，性能好） |
| HTTP 路由 | `net/http` 标准库（或轻量 router 如 chi） |
| 数据库 | SQLite（mattn/go-sqlite3 或 modernc.org/sqlite） |
| 前端 | Vue 3 + shadcn-vue (Lyra) + pnpm |
| 嵌入 | Go embed dist/ |
| Provider 注册 | `os/exec` 调用外部脚本，JSON stdout 协议 |
| 依赖管理 | 每个 provider 独立 uv 项目，`uv run` 执行 |
| 配置 | YAML（gopkg.in/yaml.v3） |
| 日志 | `log/slog`（Go 1.21+） |
| 流式响应 | 直接透传 SSE，不缓冲 |

---

## 开发阶段

### Phase 1：核心代理
- [ ] Go 项目骨架 + config 加载
- [ ] Provider 接口 + manifest 解析
- [ ] SQLite schema + key 池管理
- [ ] 脚本调用（register/check）
- [ ] **规则引擎核心**（when/if/do/route + schedule 调度器，加载 manifest rules + rules.yaml + 内置规则 + 预置规则）
- [ ] HTTP 代理 + 未分类兜底重试
- [ ] Model 路由（v1 可用性）
- [ ] CLI 命令（start/stop/status/register/keys）

### Phase 2：Web 看板
- [ ] Go embed 静态文件
- [ ] `/dashboard` 公开状态页
- [ ] `/api/*` REST 接口

### Phase 3：管理后台
- [ ] admin key 登录（与看板同 SPA，登录后解锁管理页面；Clerk 延后）
- [ ] Key 管理 UI
- [ ] Provider 详情页（key 列表 + 注册机状态 + 一键 check）
- [ ] 请求重试链查看
- [ ] 错误聚合 + 抓包查看 + 一键添加已知错误规则
- [ ] 规则管理 UI（rules.yaml 可视化编辑）
- [ ] 设置页：所有 yaml 配置可视化编辑（写回 + reload）

### Phase 4：智能路由
- [ ] 复杂度分类器集成
- [ ] 模型分类（fast/thinking/code/writing）
- [ ] 延迟加权路由

---

## 补充事项（正文未詳述的零散决定）

- 不设单请求总超时，只限 `max_retries`
- requests 表不做清理
- `clowder start` 用 Go 自我后台化，`stop` 靠 pid 文件
- 错误指纹 = 错误响应体去除数字/UUID/时间戳后归一化哈希，结合 provider + status 作为聚合分组键
- 面板改动任何配置/规则 = 写回对应 yaml + reload，文件是唯一事实来源

## 待办 / 未决

- [ ] 部署形态：先本地开发，部署后面再说
- [ ] 第一个新 provider 加谁？（除了 featherless）
