# kos-updater

KOS 系统应用项目：扩展包 — 组件页与更新页的版本、能力与兼容性投影。

- 设计：`tsic/kos-design` 仓的 `kos-updater/`（范围、契约、验收、决策记录）
- 设计依据：`tsic/kos-design@05bf60a`
- 交付物：经 kos-sdk 打包的扩展包（`KOSD-C-17`、`KOSD-C-18`）；本产品是系统应用扩展，不是独立进程

## 边界

- 两页都是服务端数据投影（`KOSD-C-18`、`KOSD-C-53`）：本仓声明页面与读模型，页面外壳由 kosd 的页面服务渲染；本仓不交付 HTML/JS/CSS 客户端资产。
- 只读版本与兼容性；预检呈现、被阻塞升级与入口角色由后续增量交付，本包不为它们声明条目，也不提供修改入口。
- 不提供应用级升级入口与更新提示（应用升级归 kos-appstore）；不提供强制驱逐 node-local 应用的路径（`ADM-C-55`）。
- 不提供未声明或未安装贡献的页面（`KOSD-F-16`）：两页随系统组件出现与消失。

## 包布局

| 路径 | 内容 | 契约 |
|---|---|---|
| `pkg/updater/declaration.go` | 组件页与更新页的声明：条目、来源类型、只读原因 | `UPDATER-C-1`、`UPDATER-C-2`、`KOSD-C-18`、`KOSD-C-53` |
| `pkg/updater/readings.go` | 页面读模型骨架：按来源解析、观测时间与不可观察项 | `KOSD-C-13`、`KOSD-C-53` |
| `pkg/updater/readings_items.go` | 各项的取值：能力与缺口、可安装组件、扩展状态、被阻塞的 Provider、版本发现、扩展兼容性 | `UPDATER-C-1`、`UPDATER-C-2`、`UPDATER-C-3`、`UPDATER-C-7` |
| `pkg/updater/values.go` | 各项的 JSON 形状 | `UPDATER-C-1`、`UPDATER-C-2`、`UPDATER-C-3` |
| `pkg/updater/manifest.go` | 同一份声明生成的 kos-sdk 清单：`KOSD-C-18` 的封闭字段集合 | `SDK-C-3` |
| `pkg/updater/package.go` | 经 kos-sdk 打包：清单、UI 资产与包摘要 | `SDK-C-4`、`KOSD-C-17` |
| `cmd/kos-updater-pack/` | 打包入口：写出扩展包与摘要 | `KOSD-C-17`、`SDK-C-4` |
| `tests/acceptance/UPDATER-TC-55/` | 两个扩展的相容结论与组件状态一致 | `UPDATER-AC-28`、`UPDATER-TC-55` |
| `tests/acceptance/UPDATER-TC-56/` | 陈旧标记与不可观察来源都不省略条目 | `UPDATER-AC-1`、`UPDATER-TC-56` |
| `tests/acceptance/KOSD-TC-155/` | 单独装载、卸载与重装；包结构与摘要 | `KOSD-AC-72`、`KOSD-TC-155` |

## 声明要点

- 组件页 5 项：已装能力与验证结果（对象源）、能力缺口（计算源）、可安装组件（计算源）、扩展启用状态与缺失依赖（对象源）、升级被阻塞的 Provider 与依赖者（对象源）。组件页只读，无修改入口。
- 更新页 2 项：KOS 版本与集群版本（计算源）、扩展与框架版本兼容性（计算源）。两项都只读；本增量不声明预检与升级入口。
- 每个已安装扩展给出目标框架版本范围、当前框架版本与相容结论；范围不含当前版本时结论为不相容，并点名扩展与范围。该结论与系统组件的 `FrameworkCompatible` 条件、组件阶段一致。
- 来源不可观察时条目保留并标为不可用、给出原因；观测时间超出 2 分钟陈旧阈值时条目保留并带陈旧标记（`KOSD-C-13`）。两者都不省略条目。
- 扩展的框架范围从组件声明的扩展包读出：与安装路径的预检读取同一来源。

## 构建与测试

```sh
make lint        # gofmt 检查 + go vet
make test        # 单元与验收用例
make acceptance  # 只跑 tests/acceptance/，-v
make pack        # 写出 dist/kos-updater.tar 与 dist/kos-updater.tar.digest
```

`make pack` 读 `assets/`（缺失即无 UI 资产），校验声明并产出带 `sha256:<hex>` 摘要的包；同一清单与资产总是产出同一结构与同一摘要。

## 依赖

- kosd：控制面 API、扩展声明与安装路径、页面服务与页面条目形状、Provider 与组件投影。
- kos-sdk：清单校验与打包工具。
- kosadm：平台升级与扩展升级（本产品只呈现，不执行）。
