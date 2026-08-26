# 管段冲洗消毒放行工作台

本项目面向供水管网现场负责人和水质检测复核人员，覆盖管段放行批次创建、冲洗消毒方案冻结、现场轮次记录、多点水质判定、异常整改复检、复核签发和凭据核验。服务使用 Go 单进程架构，页面由 Go embed 提供，无需 Node 构建链；业务数据和审计事件保存在本地文件仓储中。

## 业务流程

批次沿 `draft → frozen → executing → sampling/remediation → review → released` 单一主流程推进：

- `POST /api/batches` 创建或按 `batchId`、`expectedVersion` 保存草稿，字段错误会在 `fieldErrors` 中返回；草稿创建和修改均记录前后摘要。
- `GET /api/batches` 支持按管段、供水来源、状态和创建人组合检索，使用与筛选条件绑定的稳定分页游标返回当前环节、下一操作和待办计数；选择摘要后仍由 `GET /api/batches/{id}` 恢复完整工作区。
- 草稿可成对填写 `diameterMm`、`lengthM`，详情返回理论容积、绝对差值和偏差百分比。偏差超过 10% 时仍可保存草稿，但不能冻结方案。
- `POST /api/batches/{id}/freeze` 先以 `action=precheck` 无副作用试算执行水量、换水倍数、余氯边界余量和采样点，再以 `action=confirm` 携带 `confirmationSummary` 确认冻结；草稿版本或方案变化会使旧摘要失效。
- `POST /api/batches/{id}/rounds` 按连续序号记录冻结后的非重叠真实时段；`idempotencyKey` 可安全重放相同请求。详情同步返回累计水量、有效时长、分类统计、完成比例和结束执行阻断项。
- `samples` 同时兼容单条 `sample` 和互斥的 `samples` 数组。批量请求携带 `idempotencyKey`，完成全量校验后只递增一次版本，并返回每条样本的浊度、余氯上下限和菌落分项判定。异常样本通过 `corrective` 和 `reinspect` 定向闭环，点位历史、参数变化和原始见证信息始终保留。
- 整改详情按影响点返回待复检、复检不合格或复检合格任务，以及完成比例、下一批点位和阻断原因；每个复检样本只归属于一个整改项。
- `GET /api/batches/{id}/summary` 返回断点续录、执行进度、采样覆盖和带 `reviewToken` 的规范化证据快照。`review` 必须携带该令牌和 `expectedVersion`，证据变化时返回变化类别；`timeline` 返回连续审计清单，`verify` 复算不可变放行凭据。

草稿创建、草稿保存、冻结确认、轮次和批量样本请求均携带非空 `idempotencyKey`；所有修改操作携带操作者，已有批次操作还必须携带当前 `expectedVersion`。预检和列表查询不修改批次版本或审计时间线。

## 构建与运行

```text
go build ./cmd/server
go run ./cmd/server -addr=127.0.0.1:19091
```

监听地址可通过 `-addr=127.0.0.1:<port>` 指定；未指定时读取端口号形式的 `PORT` 并绑定 `127.0.0.1:<PORT>`，默认地址为 `127.0.0.1:19091`。浏览器访问服务根路径即可使用工作台，JSON 接口位于 `/api/batches`。

## 测试与自检

```text
go test ./...
go run ./cmd/server -addr=127.0.0.1:19091 -selfcheck
```

`-selfcheck` 会启动临时回环服务，通过真实 HTTP 请求完成建批、方案冻结、轮次、采样、复核和签发，再自动关闭服务。
