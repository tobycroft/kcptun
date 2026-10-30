#!/usr/bin/env python3
"""为手动依赖升级分支创建 PR（dependency-upgrade.yml 调用）。

从环境变量读取：
    GITHUB_API_URL / GITHUB_REPOSITORY   API 地址与仓库
    GH_TOKEN                             工作流凭据
    UPGRADE_BRANCH / MAIN_BRANCH         源分支 / 目标分支
    MODE / GO_DIRECTIVE                  触发输入，用于描述
    VALIDATION_OK / FAILED_STEP          校验结果与失败环节
    COMPARE_BASE / COMMIT_HEAD           commit 范围
    GO_DIRECTIVE_CHANGED                 go/toolchain 指令行是否变化
"""

import json
import os
import sys
import urllib.error
import urllib.request

LABELS_OK = ["dependencies", "go"]
LABELS_FAILED = ["dependencies", "go", "automerge-skip"]


def env(key, default=""):
	return os.environ.get(key, default) or default


def api(token, path, payload=None, method=None):
	url = env("GITHUB_API_URL", "https://api.github.com") + path
	data = json.dumps(payload).encode("utf-8") if payload is not None else None
	request = urllib.request.Request(
		url,
		data=data,
		method=method or ("POST" if payload is not None else "GET"),
		headers={
			"Authorization": f"Bearer {token}",
			"Accept": "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"User-Agent": "kcptun-dependency-upgrade",
		},
	)
	try:
		with urllib.request.urlopen(request) as response:
			body = response.read().decode("utf-8")
			return response.status, (json.loads(body) if body else {})
	except urllib.error.HTTPError as error:
		return error.code, error.read().decode("utf-8")


def main():
	token = env("GH_TOKEN")
	repo = env("GITHUB_REPOSITORY")
	branch = env("UPGRADE_BRANCH")
	base = env("MAIN_BRANCH", "master")
	validation_ok = env("VALIDATION_OK", "false") == "true"
	failed_step = env("FAILED_STEP", "unknown")
	changes_file = "dep-changes.md"
	if os.path.exists(changes_file):
		with open(changes_file, encoding="utf-8") as handle:
			changes = handle.read().strip()
	else:
		changes = "（未生成 dep-changes.md）"

	if validation_ok:
		title = f"chore(deps): 全量升级 Go 依赖到最新（{env('MODE', 'all')}）"
		draft = False
		outcome = "✅ gofmt / go vet / 编译 / go test 全部通过，可合并（合入主干后自动打包发布）"
	else:
		title = f"[校验失败] chore(deps): 全量升级 Go 依赖到最新（{env('MODE', 'all')}）"
		draft = True
		outcome = f"❌ 校验未通过，失败环节：`{failed_step}`；本 PR 仅保留 diff 供人工排查，禁止直接合并"

	body = "\n".join(
		[
			"手动触发（workflow_dispatch）的全量依赖升级结果。",
			"",
			f"- 升级范围 `mode`：{env('MODE', 'all')}",
			f"- Go 版本声明 `go_directive`：{env('GO_DIRECTIVE', 'no')}（go/toolchain 指令行是否变化：{env('GO_DIRECTIVE_CHANGED', 'false')}）",
			f"- 校验结果：{outcome}",
			f"- commit 范围：`{env('COMPARE_BASE')}...{env('COMMIT_HEAD')}`",
			"",
			"## 依赖版本变化",
			"",
			changes,
			"",
			"## 后续动作",
			"",
			f"- 校验通过时：合入 `{base}` 后由 Release 工作流自动打 tag、构建多平台产物并发布；",
			"- 校验失败时：在本 PR 上修复兼容性问题（必要时回滚部分依赖），"
			"或改用 `mode=direct` 缩小升级范围后重跑本工作流。",
		]
	)

	payload = {
		"title": title,
		"head": branch,
		"base": base,
		"body": body,
		"draft": draft,
	}
	status, created = api(token, f"/repos/{repo}/pulls", payload)
	if status != 201:
		print(f"::error::创建 PR 失败（HTTP {status}）：{created}")
		return 1

	pr_number = created["number"]
	pr_url = created["html_url"]
	labels = LABELS_OK if validation_ok else LABELS_FAILED
	api(token, f"/repos/{repo}/issues/{pr_number}/labels", {"labels": labels})

	summary = env("GITHUB_STEP_SUMMARY")
	if summary:
		with open(summary, "a", encoding="utf-8") as handle:
			handle.write("### 依赖升级结果\n\n")
			handle.write(f"- PR：{pr_url}\n")
			handle.write(f"- 升级范围：{env('MODE', 'all')}；go 指令行处理方式：{env('GO_DIRECTIVE', 'no')}\n")
			handle.write(f"- 标签：{', '.join(labels)}\n")
			handle.write(f"- 校验：{outcome}\n")

	with open("/tmp/pr-result.json", "w", encoding="utf-8") as handle:
		json.dump({"number": pr_number, "url": pr_url, "draft": draft, "labels": labels}, handle, ensure_ascii=False)
	print(f"已创建 {'draft ' if draft else ''}PR：{pr_url}（labels={','.join(labels)}）")

	if not validation_ok:
		# PR 已保留 diff，但仍让 job 失败，便于在 Actions 列表里直接看出问题
		print("::error::校验未通过，已创建 draft PR 保留改动，需人工处理")
		return 1
	return 0


if __name__ == "__main__":
	sys.exit(main())
