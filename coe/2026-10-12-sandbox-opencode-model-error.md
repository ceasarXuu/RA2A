# Problem P-001: Linux矩阵OpenCode原生模型错误
- Status: open
- Objective: 保持当前provider/model及正式环境不变，定位并修复matrix-006 OpenCode失败；无法证明外部服务恢复时如实报告。
- Symptoms: basic-codex-opencode收件accepted，native user1/final0/NATIVE_MODEL_ERROR，后续依赖止步。
- Environment: Linux，OpenCode1.18.34，opencode-go/space-bunny-free；矩阵启动c851e87。
- Known facts: 私有真实会话历史已随正常清理删除；现有公开摘要不含原生error类型或HTTP状态，不足以归因。
- Fix criteria: 证实具体机制，最小修复后同provider/model原生收件与final通过；正式配置认证不变、预算≤10。
- Current conclusion: 已确证供应商拒绝当前模型；RA2A收件正常。错误报告缺少定位信息已修正，模型可用性尚需Owner指定可用模型。
- Resolution basis: 未满足。

## Hypothesis H-001: 默认模型或供应商认证拒绝
- Status: unverified
- Claim: 已选模型space-bunny-free或其鉴权被供应商拒绝，非RA2A收件失败。
- Falsifiable predictions: 若为真，原生error含ModelNotFound/认证错误或明确HTTP400/401/403/404；若为假，仅网络/SDK协议错误。
- Diagnostic evidence plan: 一个新私有输入先reserve；记录原生error固定name、statusCode、isRetryable及消息白名单类别，不保存message全文/headers/body/token；以message-id,parentID关联到该输入。零额度源码路径调查独立核对。
- Evidence gate: pending
- Next step: 固定官方源码核验错误结构，并统一安排单条诊断。

## Hypothesis H-002: 代理或SDK请求/响应处理故障
- Status: unverified
- Claim: 私有HTTPS代理或该模型SDK路径导致网络/响应解析错误。
- Falsifiable predictions: 若为真，error为连接超时/TLS/响应解析，或relay计数出现对应失败；若为假，明确供应商业务拒绝。
- Diagnostic evidence plan: 与H-001同一条输入分别观察原生error白名单与relay聚合计数；核对固定SDK及proxy环境源码。Pi同域成功仅作为可达性背景，不外推OpenCode模型成功。
- Evidence gate: pending
- Next step: 采集区分信号，不改变provider/model。

## Evidence E-001: 原始矩阵失败资格
- Related hypotheses: H-001,H-002
- Direction: neutral
- Type: observation
- Source: .cache/sandbox-discovery/matrix-006/summary.json
- Prediction or plan link: 两种候选均不能由收件ACK或通用错误标签区分。
- Matched signal: HTTP200 accepted，native users1/finals0/errorClasses=[NATIVE_MODEL_ERROR]。
- Interpretation: RA2A输入已到原生记录；不证明模型、鉴权或代理具体故障。

## Evidence E-002: 唯一预算输入确证供应商模型拒绝
- Related hypotheses: H-001,H-002
- Direction: supports H-001 / contradicts H-002 网络故障分支
- Type: controlled experiment
- Source: .cache/sandbox-repair/opencode-001/result.json、ledger.jsonl；固定OpenCode1.18.34原生message-v2错误结构。
- Prediction or plan link: E-001之后先按H-001/H-002白名单信号取证，不改变provider/model。
- Matched signal: 自投唯一输入，RA2A confirmed/accepted，原生user1/final0；关联APIError、httpStatus400、retryable=false、category=MODEL_NOT_AVAILABLE。OC reserved1/limit1；无重试。
- Interpretation: 本次不是RA2A投递丢失；服务端拒绝所选opencode-go/space-bunny-free。错误原文只在内存分类，不保存header/body/message/auth/thinking；不能由HTTP400外推具体服务政策或所有模型均不可用。
- Outcome: 新增脱敏nativeErrors；敏感信息拒绝导出测试通过。已向Owner请求可用provider/model，只改沙盒、不静默切换正式设置。自有资源REAPED、凭据副本REMOVED、8项正式auth/config终点哈希不变。

## Hypothesis H-003: OpenAI内置OAuth路由被目录api门禁误拒绝
- Status: confirmed
- Claim: 显式选择本机已有OpenAI OAuth模型时，catalog缺api不代表无可核验端点；固定原生插件实际重写至ChatGPT路由。
- Diagnostic evidence plan: 子调查先指出catalog.api缺省与OAuth区别；随后只读auth类型/字段名、固定1.18.34内置插件路由，禁止打印token。若不是内置OpenAI SDK/OAuth或出现未核域名，保持failclosed。

## Evidence E-003: 内置OAuth端点源码与私有覆盖测试
- Related hypotheses: H-003
- Direction: supports
- Source: 固定官方 https://raw.githubusercontent.com/anomalyco/opencode/v1.18.34/packages/opencode/src/plugin/openai/codex.ts；本机auth字段名与catalog只读核验；test_profiles.py。
- Matched signal: 当前openai凭据type=oauth，catalog.npm=@ai-sdk/openai/api缺省；原生插件将/v1/responses或/chat/completions重写chatgpt.com/backend-api/codex/responses，刷新auth.openai.com，gpt-6-sol在固定允许集合。不修改原生插件，仅投影最小凭据副本与三项明确域名。
- Outcome: 新增--opencode-model显式沙盒参数，无自动回退；未知provider/model拒绝。内置OAuth投影九项相关测试通过，正式配置/认证不变。Owner是否选用openai/gpt-6-sol待答复，未新增收费输入；参数可运行不等于真实上游验收通过。

## Evidence E-004: Owner模型批准及轻量档投影
- Related hypotheses: H-001,H-003
- Direction: supports
- Source: Owner明确「批准，模型用6.1-sol light」；本机模型声明与固定OpenCode1.18.34 provider transform/config schema；11项profiles测试。
- Matched signal: openai目录已有gpt-6.1-sol；supported reasoning effort包含low且说明为lighter reasoning，无literal light。本机Codex准确配置为gpt-6.1-sol/low。显式OpenCode私有模型options.reasoningEffort=low；Codex私有配置保留low；正式认证/config hash投影前后相同。
- Interpretation: 批准已落实，无收费输入；真实模型最终完成待本轮矩阵验证。Pi本机只有opencode-go认证且目录无6.1-sol，本轮沿用其已有原生配置，不能宣称四类统一同模型。
