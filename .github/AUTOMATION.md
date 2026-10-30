# 依赖自动更新 → 自动合并 → 自动构建 → 自动发布

本文说明 `tobycroft/kcptun` 的自动化链路：文件职责、触发条件、需要开启的仓库设置、
首次验证步骤与常见问题排查。

所有事实均已核对，不是推测：

| 核对项 | 结论（来源） |
| --- | --- |
| 默认分支 | `master`（GitHub API `repos/tobycroft/kcptun` → `default_branch`） |
| tag 规律 | `vYYYYMMDD`，同日多次发布追加 `.N`：`v20260515 / v20261029 / v20261029.1 / v20261029.2 / v20261030 / v20261030.1` |
| Release 标题 | 最新两个为 `kcptun v20261030`、`kcptun v20261030.1`（更早只写 tag 本身，本流水线沿用**最新**风格） |
| 资产清单 | 20 个裸二进制（`client_<os>_<arch>` / `server_<os>_<arch>[.exe]`）+ `checksums.txt`，共 21 个，**不打包 tar.gz/zip** |
| 平台矩阵 | linux(386/amd64/arm GOARM=7/arm64)、darwin(amd64/arm64)、windows(386/amd64/arm64)、freebsd(amd64) |
| 构建参数 | `-trimpath`、`CGO_ENABLED=0`、`-ldflags "-s -w -X main.VERSION=<tag>"`、Go 1.27.3 |
| 校验和格式 | `sha256sum` 风格：`<64位哈希><两个空格>./<文件名>`，client 在前 server 在后 |
| 既有工作流 | 主干上 `.github/workflows/` 不存在（API 返回 404），因此本流水线是新增，不与既有流程冲突 |
| 既有标签 | `dependencies`、`go` 标签仓库中已存在，可直接复用；`automerge-skip` 由工作流自动创建 |
| 仓库状态 | 非 fork、公开、无 open PR/issue（因此新工作流不会与存量运行纠缠） |

> 一键脚本仓库 `TuuzNet/kcptun-script` 已有自己的 `release.yml`（发布 `kcptun.sh` 本身），
> 本流水线不涉及、也不修改它。

---

## 1. 文件清单与职责

| 文件 | 职责 |
| --- | --- |
| `.github/dependabot.yml` | 依赖更新入口：`gomod`（每日、分组、标签、提交信息前缀）与 `github-actions`（每周一） |
| `.github/workflows/ci.yml` | 编译 + 静态检查 + 测试；job 名 `mod` / `build-test` 就是分支保护里的必需状态检查，也是自动合并的放行条件 |
| `.github/workflows/automerge-dependabot.yml` | CI 全绿后自动 approve 并以 **squash** 合并 Dependabot PR；失败/冲突时保留 PR 并打标签 |
| `.github/workflows/dependency-upgrade.yml` | 手动（`workflow_dispatch`）一次性升级到最新：`go get -u ./...` + `go mod tidy` + 校验 + 开 PR |
| `.github/workflows/release.yml` | 依赖合并到主干后自动打 tag、构建 21 个产物、生成校验和与说明、创建 GitHub Release；支持 `dry_run` 预演 |
| `.github/scripts/build-dist.sh` | 多平台构建与 `checksums.txt` 生成（本地可执行同一脚本做核对） |
| `.github/scripts/upgrade-deps.sh` | 手动升级的执行体，含 go 指令行策略与各环节失败定位（`failed_step`） |
| `.github/scripts/open-upgrade-pr.py` | 为升级分支开 PR（成功=普通 PR，失败=draft + `automerge-skip`） |
| `.github/scripts/gen-notes.py` | 生成 Release 说明：依赖版本变化表、commit 范围、是否 Dependabot 触发、构建参数与产物说明 |
| `.gitignore` | 追加忽略升级流程产生的临时快照 `before.txt` / `after.txt` / `dep-changes.md` |

链路关系：

```
Dependabot PR ──▶ CI(mod/build-test) ──▶ automerge(squash 合并到 master)
                                                  │
                              push master（依赖类提交）
                                                  ▼
        release.yml：Plan tag(vYYYYMMDD[.N]) → build-dist.sh(21 资产) → gen-notes.py → 推 tag + 创建 Release
                                                  ▲
手动升级：workflow_dispatch ─▶ upgrade-deps.sh ─▶ PR ─▶（人工合并后走上同一条发布路）
```

---

## 2. 需要一次性开启的仓库设置

1. **Dependabot**：公开仓库无需额外开关，`.github/dependabot.yml` 合入主干即生效。
   可在 `Settings → Codes and security → Dependabots` 确认配置文件被识别（绿色对号）。
2. **Allow auto-merge**（`gh repo edit tobycroft/kcptun --enable-auto-merge`）：
   本流水线是**自己调用 merge API** 完成 squash 合并的，因此这一项并非强依赖；
   只有当你想改用手点 Dependabot 的 “Enable auto-merge” 按钮时才必须开启。
3. **分支保护（可选，但推荐）**。若开启，必须同时做两件事，否则自动合并会被拒：
   - 必需状态检查填 `mod` 与 `build-test`（就是 `ci.yml` 的两个 job 名）；
   - 把 “Require a pull request before merging” 的审批数设为 `0`，
     或者配置 secret `AUTOMERGE_TOKEN`（见下）。

   ```bash
   echo '{"required_status_checks":{"strict":false,"contexts":["mod","build-test"]},"required_pull_request_reviews":{"required_approving_review_count":0},"enforce_admins":false,"restrictions":null}' \
     | gh api -X PUT repos/tobycroft/kcptun/branches/master/protection --input -
   ```
4. **secret `AUTOMERGE_TOKEN`（可选）**：仅当分支保护要求 ≥1 个 approval 时才需要。
   值必须来自**另一个账号/机器人账号**的 PAT（fine-grained：Contents: Read & write → 仓库 `kcptun`；
   或 classic：`repo`）。GitHub 平台限制：工作流不能用 `GITHUB_TOKEN` approve 自己触发的 PR，
   `GITHUB_TOKEN` 也不能给自己创建的 PR 投 approval。未配置该 secret 时，approve 步骤会打印提示并跳过。
5. **权限**：所有工作流都显式声明最小权限且只用 `secrets.*`，仓库内**不含任何硬编码 token**：
   - `ci.yml`：`contents: read`
   - `automerge-dependabot.yml`：`contents: read` + `pull-requests: write`
   - `dependency-upgrade.yml`：`contents: write` + `pull-requests: write`
   - `release.yml`：`contents: write`（推 tag、创建 Release、上传资产）

---

## 3. 触发条件

| 工作流 | 触发 | 备注 |
| --- | --- | --- |
| Dependabot | `gomod` 每日 08:00（Asia/Shanghai）、`github-actions` 每周一 | `target-branch: master`；分组：`go-kcp-core`（`github.com/xtaci/*`）、`go-all-minor-patch`（其余 minor/patch）；major 不分组，逐个 PR 由人工确认 |
| `CI` | push master、所有针对 master 的 PR、手动 | 也用于给非 Dependabot 改动把关 |
| `Automerge Dependabot` | CI 的 `mod` / `build-test` 在 `dependabot/*` 分支上 completed | 只处理 `dependabot[bot]` 提交、目标为 `master`、`mergeable=true`、无 `automerge-skip`/`do-not-merge`/`hold` 标签的 PR；合并后为合并提交补打 `dependencies` / `go` 标签（squash 会丢标签，供 Release 判定来源） |
| `Dependency Upgrade` | 手动 `workflow_dispatch` | 输入：`mode`（`all`=`go get -u ./...`；`direct`=只升级直接依赖）、`go_directive`（默认 `no`，不动 go 指令行）、`target_branch`（留空自动生成） |
| `Release` | ① push tag `v*`；② push master 且提交来自 `dependabot[bot]` 或信息含 `Bump` / `chore(deps)`；③ 手动（可指定 tag） | 手动触发时 `dry_run=true` 只构建不发布 |

自动合并的**降级策略**（不会静默关闭 PR）：CI 任一必需检查失败、检查缺失、PR 有冲突或
`mergeable` 非 true、被人工打了暂缓标签 → 保留 PR、打 `automerge-skip` 标签并写明原因；
PR 已关闭/合并则直接跳过不做任何操作。

---

## 4. 产物与命名规范（与既有 Release 完全一致）

`build-dist.sh` 复刻上面的矩阵，并在结束时自检：资产数 = 20、`VERSION` 已注入二进制。
`release.yml` 还会断言资产总数 = 21（与 v20261030 / v20261030.1 相同），不符则整条链路失败。

本地核对（不产生任何远程副作用）：

```bash
VERSION=v20261030.1 bash .github/scripts/build-dist.sh
# 期望：20 个 client_*/server_* + dist/checksums.txt；末尾打印 “VERSION=... 已注入”
```

已在本地实测：与 v20261030.1 已发布资产相比，20 个产物中 19 个**字节数完全相同**
（唯一 512 字节差异来自 Windows PE 段对齐），哈希不同仅因二进制内嵌的 git VCS 元数据
（提交号随新提交变化），属于预期的可追溯信息，不是构建方式差异。

关于 `kcptun-linux-*.tar.gz`：仅 `v20261029.1` 提供过，`v20261030` / `v20261030.1` 已不再提供，
而 `kcptun.sh` 实际只下载 `server_<arch>` 与 `checksums.txt`（不依赖 tar 包，还会主动清理遗留的
`kcptun-*.tar.gz`）。因此流水线默认不产出 tar 包，以复刻**最新**资产矩阵；
中文 README「Linux x86 / x86_64 另外提供 `kcptun-linux-*.tar.gz`」这一句与最新 Release 不一致，
建议后续要么补上该资产（需先确认包内文件结构），要么删掉该说明。

---

## 5. 首次验证步骤（建议按顺序做）

1. **提交并推送本目录**（`.github/` 全部文件 + `.gitignore`）到 `master`。
   Dependabot 配置文件只有在默认分支上才会被读取。
2. **验证 CI**：Actions 页面应出现 `CI` 运行（push master 触发）。
   检查点：`mod`、`build-test` 两个 job 全绿；`Verify go.mod / go.sum are tidy` 未报错。
3. **Release 预演（不发布）**：Actions → `Release` → Run workflow → 勾选 `dry_run=true`，
   `tag` 留 `v20261030.2`（当天已用掉 `v20261030.1`，符合既有 `.N` 规则）。
   检查点：
   - `Plan tag` 输出 `目标 tag：v20261030.2`、`上一个 tag：v20261030.1`；
   - `Collect assets` 输出 21 个资产、`checksums.txt` 前两行是 `client_darwin_amd64`；
   - `Generate release notes` 打印出依赖变化表（无变化时会写「本次没有第三方依赖版本变化」）；
   - 产物上传为 `staging-v20261030.2` artifact；
   - **`publish` job 被跳过**，仓库没有新 tag、没有新 Release。
4. **依赖升级演练**：Actions → `Dependency Upgrade` → `mode=direct` 跑一次。
   检查点：日志出现 `go get -u` / `go mod tidy` / 校验各段；
   若已有最新则「无变化，未创建 PR」；有变化则生成一个带 `dependencies` / `go` 标签的 PR，
   PR 描述里有依赖版本变化表与 commit 范围。
5. **自动合并演练**：等 Dependabot 开出第一个 PR（或手动改一个依赖版本触发）。
   检查点：PR 上出现 `mod` / `build-test` 两个检查 → 全绿后自动 approve + squash 合并 →
   合并提交带有 `dependencies` / `go` 标签 → `Release` 被 master 推送触发并发布新版本。
6. **人工兜底**：合并后如产物有问题，可用 `Release` 手动触发并填指定 tag 重新发布；
   tag 与 Release 已存在时工作流会跳过而不是重复创建。

---

## 6. 常见问题排查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| Dependabot 一直不开 PR | 配置不在默认分支；或 PR 数已达 `open-pull-requests-limit` | 确认 `.github/dependabot.yml` 已在 `master`；`Dependabot → Security updates` 里看配额 |
| PR 里只升级了一半依赖 | 分组规则：`github.com/xtaci/*` 单独成组，major 不分组 | 属预期；major 升级需人工确认后合并 |
| `Automerge` 没跑 | 触发只看 `mod` / `build-test` 在 `dependabot/*` 分支完成；CI 未跑或非这两个 job | 看 PR 的 Checks 列表，确认 job 名仍是 `mod` / `build-test`（改名需同步 `REQUIRED_CHECKS`） |
| `不自动合并：必需检查 mod 未通过（结论：failure）` | CI 真的失败 | 按 `automerge-skip` 标签与 PR 评论修代码/回滚该依赖 |
| `PR 当前不可合并（mergeable=false）` | 与主干冲突 | 在 PR 上点 “Update branch” 或本地 rebase 后重推，CI 重新全绿会自动合并 |
| `Resource not accessible by integration` / 合并被拒 | 分支保护要求 approval，而未配置可 approve 的账号 | 设置 `AUTOMERGE_TOKEN`（另一账号的 PAT），或把 required_approving_review_count 设为 0 |
| `gh: pull request is not mergeable` | 与上一条同类，或 squash 被仓库关闭 | `gh repo edit tobycroft/kcptun --enable-squash-merge` |
| `Release` 未被 master 推送触发 | 合并提交信息里没有 `Bump` / `chore(deps)`，作者也不是 dependabot[bot] | 手动触发 `Release` 并指定 tag；今后保持 squash 标题前缀 `chore(deps):` |
| 发布只出了一半资产就中断 | tag 已存在但 Release 缺失 | 再次手动触发同一 tag，工作流会走「补齐资产」分支（`gh release upload --clobber`） |
| `go get -u=patch` 报错 `can't query version "patch" of module ...` | 该写法会把主模块自身纳入查询，主模块无发布版本 | 已避开：`mode=all` 用 `go get -u ./...`，`mode=direct` 只对直接依赖执行 `go get -u` |
| `setup-go` 找不到 Go 版本 | 三处版本号写死为 `1.27.3` | 升级工具链时同步 `ci.yml`、`release.yml`、`dependency-upgrade.yml` 与 `go.mod` |
| 想看流水线是否会误发布 | — | 用 `Release` 的 `dry_run=true`，只构建不推 tag |

---

## 7. 维护约定

- **不要**新增与 `Release` / `CI` 功能重叠的第二条发布链路；需要调整就改这两个文件。
- 产物矩阵（平台、命名、`checksums.txt` 格式）变更时，必须同步更新本文件的表格，
  并核对 `kcptun-script/kcptun.sh` 里 `server_<file_suffix>` 与 `checksums.txt` 的取值逻辑。
- 版本号只做一件事：`vYYYYMMDD` + 同日递增 `.N`。不要引入 `v1.2.3`、日期带分隔符等新格式。
- YAML 语法校验：`actionlint .github/workflows/*.yml`（本目录所有文件已通过，0 issue）。
- 建议在 `Settings → Actions → General → Workflow permissions` 保持 “Read repository contents and packages permissions”
  （默认值），由各工作流内的 `permissions:` 自行提权，避免全局给写权限。
