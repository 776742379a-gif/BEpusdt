# TronGrid

> 系统默认 Tron gRPC 节点是 `grpc.trongrid.io:50051`。生产环境建议配置正式 TronGrid API Key；匿名访问的可用性、免费额度和费用以 TronGrid 当前服务政策及控制台为准。
>
> 空 Key 会被过滤，不再发送会触发 HTTP 401 的空鉴权字段。多个 Key 中的空项和前后空格也会被过滤。
>
> 使用现有 dRPC Key 的配置方法见 [TRON dRPC 接入](../tron-drpc.md)。

## 获取 Api Key

1. 访问 https://www.trongrid.io/register 使用邮箱注册账号并完成登录。
2. 登录后找到`API Keys`选项，点击`Create API Key`填写名称后提交即可。

## 配置 Api Key

你的`Api Key`应该是类似这个样子：`648870c0-xxxx-xxxx-xxxx-c7ac4ec263b0`  
拿到之后登录 BEpusdt 后台，进入`系统管理`->`区块节点`->`Tron 网置`，将 Api Key 填入`TronGrid Api Key`输入框，保存即可生效。

如图：

![tron-grid-config](./1.png)
