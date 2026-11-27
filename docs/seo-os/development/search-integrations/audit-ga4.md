# GA4 集成审计

| 分类 | 结果 |
| --- | --- |
| A | accountSummaries 分页、数字 Property ID、Data API 与 Organic Search、Hostname 和路径过滤沿用现有实现。 |
| B | 缺少账户共享、Property 详情和数据流范围验证。上游采样、阈值与 other 行目前只压成一个 truncated，缺少细分和配额信息。 |
| C | GA4 访问权限以实际 API 响应为依据。accountSummaries 原生结果不返回每个调用者的角色。 |
| D | 独立真实 OAuth 与 Property 目录、时区和 runReport 尚待验收。 |
| E | GA4 日期由 Property 时区定义。用户互动秒数和 Key Events 的定义应明确展示。 |
| F | 由管理员确认绑定并由服务端重新验证该账户的 Property 与网站范围。 |

[Admin API](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/accountSummaries/list) · [Data API 元数据](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/ResponseMetaData) · [Property Quota](https://developers.google.com/analytics/devguides/reporting/data/v1/quotas)
