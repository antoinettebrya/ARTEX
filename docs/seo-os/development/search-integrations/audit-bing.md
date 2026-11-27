# Bing Webmaster 集成审计

| 分类 | 结果 |
| --- | --- |
| A | 保留 JSON/HTTP API Key 模式、已验证站点检查、旧 WCF 日期解析、站点/查询/页面数据和提交额度检查。 |
| B | 现有代码缺少账户级 Key、官方 OAuth token flow 与 Bearer。 |
| C | Microsoft 公布 SOAP/POX 退役。API Key 使用已公布的 ssl.bing.com JSON 路径，Bearer 使用 OAuth 文档中的 www.bing.com JSON 路径。新 REST 能力覆盖待真实比对。 |
| D | 独立 OAuth 和 API Key 的公网上游、真实只读报告及受控提交尚待验收。 |
| E | 三类报告须标注各自粒度。GetUrlInfo 返回抓取或发现证据时，不推定索引。 |
| F | API Key 仅保存在密文与请求内存。鉴权方式由授权配置固定，不因失败自动切换。 |

[Microsoft OAuth](https://learn.microsoft.com/en-us/bingwebmaster/oauth2) · [认证方式](https://learn.microsoft.com/en-us/bingwebmaster/getting-access) · [协议及退役](https://learn.microsoft.com/en-gb/bingwebmaster/api-protocols)
