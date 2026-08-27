# 自托管静态资源

## icons.svg

Material Symbols（Outlined，weight 400）的 SVG 雪碧图，只包含后台模板实际用到的
45 个图标，由 npm 包 `@material-symbols/svg-400` 提取生成。

用 Apache License 2.0 授权，版权归 Google LLC。

雪碧图会被内联进页面而不是作为独立文件引用：跨文件的 `<use href="...#id">` 在各浏览器
上的支持并不一致，内联则一定可用，也省掉一次请求。整份雪碧图约 16KB。

两个图标名在上游没有同名条目，映射到语义等价的图标：

| 模板中的名字          | 实际图标           |
| --------------------- | ------------------ |
| `image_not_supported` | `hide_image`       |
| `new_releases`        | `browser_updated`  |

新增图标时，把名字加进模板，再从同一个 npm 包取出对应 `<symbol>` 补进 `icons.svg`，
`TestEveryReferencedIconExistsInTheSprite` 会守住两者不同步的情况。
