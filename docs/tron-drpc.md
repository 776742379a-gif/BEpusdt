# TRON dRPC 接入

本修复基于生产二进制对应的上游 commit `94ba4b7c5b33564635b5dd4580fc1b9e8e919678`（`v1.23.5-94ba4b7`），不引入上游后续版本的功能或迁移。

2026-10-14 只读核对的生产镜像基线为 `v03413/bepusdt@sha256:b372607193f3d9cbac932fddab42f1ad4678db89b7394ca2d5d2a6accdde64d2`。部署时须重新确认仍使用此基线。容器原参数：名称 `BEpusdt`、重启策略 `always`、本机 `127.0.0.1:8080` 映射容器 `8080`，挂载 `/www/docker/bepusdt/data:/var/lib/bepusdt`。原镜像必须保留作为回滚来源。

## 配置

在系统管理的区块节点页面中填写：

| 字段 | 值 |
| --- | --- |
| Tron RPC (`rpc_endpoint_tron`) | `grpc.drpc.live:443` |
| dRPC API Key (`rpc_endpoint_tron_drpc_api_key`) | 现有 dRPC Key；多个 Key 用半角逗号分隔 |

客户端对这个节点强制使用 TLS 并验证服务器证书，每次调用携带 `x-drpc-key` 和 `x-drpc-chain: tron`。没有有效配置 Key 时拒绝创建连接。dRPC Key 仅发往 dRPC 官方 gRPC 节点，TronGrid Key 不会混入 dRPC 请求。

TronGrid 配置继续使用原来的地址和独立 Key 字段。两种配置都过滤空 Key；修复前未配置 TronGrid Key 会产生 `['']`，客户端发出空 `tron-pro-api-key` 字段，已只读复现其返回 HTTP 401。修改 Key 后连接缓存会失效，后续调用使用新 Key。Key 不写入连接日志。

区块解析、USDT 合约、收款地址、金额匹配、确认数、订单状态和商城回调沿用原有业务逻辑。dRPC 的配额及费用以账号套餐为准。

官方协议：[dRPC gRPC Guide](https://drpc.org/docs/grpc-guide)。HTTP JSON-RPC URL 不适用于这里的原生 TRON gRPC 客户端。

## 本地验证与部署验收

- 执行 `go test ./...` 和 `go vet ./...`。
- 只读验证实际客户端：在进程环境中提供 `BEPUSDT_TRON_DRPC_KEY`，执行 `go run ./scripts/tron-rpc-check`。可选环境变量 `BEPUSDT_TRON_CHECK_TXID` 用于核对已知成功交易回执；程序仅输出区块高度、回执匹配及成功状态，不输出 Key 或交易哈希。Key 不要作为命令行参数或写入仓库。
- 在 `web` 执行 `pnpm install --frozen-lockfile --shamefully-hoist` 和 `pnpm run build:prod`，将构建产物复制到 `static/secure` 后构建程序。项目工作区仅批准已锁定的 `esbuild` 和 `vue-demi` 构建脚本，安装布局与原 Dockerfile 一致。
- 构建前确认来源 commit 和改动清单，不使用浮动的上游 `latest` 替代这个修复。
- 生产部署须另获授权。部署前保留当前镜像 digest、容器参数和回滚方式，使用 SQLite 原生在线备份保存配置数据库并检查完整性。
- 精确配置 TRON 节点和 dRPC Key 后核对配置已刷新、实际扫描日志、最新区块推进以及交易回执读取。BSC/Polygon 配置不随 TRON 切换改变。
- 完整验收需一笔另行安排的小额 TRC20 付款，证明自动识别、成功回调和商城余额入账/商品交付。只读节点测试不能代替这项验收。

此前已由客服补单的订单不得作为新的自动到账样本，也不得再次补单。

## 2026-10-14 本地验证记录

- 使用与生产二进制一致的 Go 1.24.3：`go test ./...`、`go vet ./...` 均通过。
- 回归覆盖：空 Key 不发送鉴权字段、有效 Key 去除空格、单次鉴权字段、dRPC/TronGrid 凭据隔离、dRPC 每次请求的 TLS 与网络字段、拒绝不可信证书、拒绝缺失 Key、Key 轮换重建连接以及移除 Key 后停止复用旧凭据。
- 原有通知测试补充数据库句柄关闭，解决 Windows 下临时 SQLite 文件无法清理的问题；通知实现未修改。
- 后台 TypeScript 检查、Vite 生产构建、RPC 配置页面 ESLint 与 Prettier 检查均通过。格式化后的改动仅调整行布局，已构建页面包含 dRPC 配置字段。
- 修复后的实际 Go 客户端使用现有 dRPC Key 完成只读测试：最新区块、指定区块及已知成功交易回执均正确返回；回执哈希匹配且交易成功。
- Linux/amd64 程序使用 Go 1.24.3、`CGO_ENABLED=0` 构建，包含后台页面。输出为 `dist/bepusdt-linux-amd64`，版本标识 `v1.23.5-94ba4b7-drpc-local`；SHA-256：`695409ed2ce3c66afeb3c477e71570eee8fe5ed187ce3c397e3da74497b46ba2`。
- 本地 Docker daemon 未运行，因此没有生成或运行容器镜像。本次没有生产部署、节点配置写入、订单变更或完整自动付款验收。
- 状态：本地任务分支 `codex/tron-drpc-auth`，尚未 commit、push 或创建 PR；部署前仍须确认正式 commit 和备份。
