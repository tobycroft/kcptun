#!/usr/bin/env python3
"""生成 GitHub Release 说明（release.yml 调用）。

用法：
    python3 .github/scripts/gen-notes.py \
        --new-tag v20261031 \
        --prev-tag v20261030.1 \
        --prev-go-mod /tmp/prev-go.mod \
        --cur-go-mod go.mod \
        --commits /tmp/commits.json \
        --prs /tmp/prs.json \
        --go-version "go1.27.3" \
        --output release-notes.md

产出的说明包含：本次依赖升级清单（包名 + 旧/新版本）、commit 范围、
是否由 Dependabot 触发、构建参数与产物说明。风格对齐既有 Release
（v20261029.1 / v20261030 的说明结构）。
"""

import argparse
import json
import re
import sys

MODULE_RE = re.compile(r"^(?P<path>\S+)\s+(?P<version>v\S+)")
GO_DIRECTIVE_RE = re.compile(r"^(?P<key>go|toolchain)\s+(?P<value>\S+)")


def parse_directives(path):
	"""读取 go.mod 中的 go / toolchain 指令行。"""
	directives = {}
	try:
		with open(path, encoding="utf-8") as handle:
			for line in handle:
				match = GO_DIRECTIVE_RE.match(line.strip())
				if match:
					directives[match.group("key")] = match.group("value")
	except OSError:
		pass
	return directives


def parse_gomod(path):
	"""解析 go.mod 的 require 指令，返回 {模块路径: (版本, 是否 indirect)}。

	只用于对比版本变化，无需展开依赖图，因此直接读 go.mod 文本，
	同时兼容单行 require 与 require 块两种写法。
	"""
	result = {}
	with open(path, encoding="utf-8") as handle:
		lines = handle.readlines()

	index = 0
	while index < len(lines):
		stripped = lines[index].strip()
		index += 1
		if stripped.startswith("require ("):
			while index < len(lines):
				inner = lines[index].strip()
				index += 1
				if inner == ")":
					break
				match = MODULE_RE.match(inner)
				if match:
					result[match.group("path")] = (
						match.group("version"),
						"// indirect" in inner,
					)
			continue
		if stripped.startswith("require "):
			match = MODULE_RE.match(stripped[len("require "):].strip())
			if match:
				result[match.group("path")] = (match.group("version"), "// indirect" in stripped)
			continue
	return result


def snapshot(path):
	"""读取 go list -m all 形式的快照文件：每行 '<模块> <版本>'。

	供不依赖 go.mod 文本对比的场景使用（例如需要含传递依赖的完整清单）。
	"""
	result = {}
	try:
		with open(path, encoding="utf-8") as handle:
			for line in handle:
				line = line.strip()
				if not line:
					continue
				parts = line.split()
				if len(parts) >= 2:
					result[parts[0]] = parts[1]
	except FileNotFoundError:
		pass
	return result


def read_module_path(path):
	"""从 go.mod 读出本模块路径（用于从差异表中排除本模块自身）。"""
	try:
		with open(path, encoding="utf-8") as handle:
			for line in handle:
				stripped = line.strip()
				if stripped.startswith("module "):
					return stripped[len("module "):].strip()
	except OSError:
		pass
	return ""


def load_json(path, default):
	try:
		with open(path, encoding="utf-8") as handle:
			return json.load(handle)
	except (FileNotFoundError, ValueError):
		return default


def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--new-tag", required=True)
	parser.add_argument("--prev-tag", default="")
	parser.add_argument("--prev-go-mod", default="")
	parser.add_argument("--cur-go-mod", default="go.mod")
	parser.add_argument("--commits", default="")
	parser.add_argument("--prs", default="")
	parser.add_argument("--go-version", default="")
	parser.add_argument("--head-sha", default="")
	parser.add_argument("--repo", default="")
	parser.add_argument("--asset-count", type=int, default=21)
	parser.add_argument("--output", default="release-notes.md")
	args = parser.parse_args()

	# ---------- 依赖变化 ----------
	# 无可对比的上一个 tag（首次自动化发布）时不列清单，避免把全部依赖当成「新增」
	has_baseline = bool(args.prev_tag)
	prev_mod = parse_gomod(args.prev_go_mod) if (has_baseline and args.prev_go_mod) else {}
	cur_mod = parse_gomod(args.cur_go_mod)
	self_path = read_module_path(args.cur_go_mod)

	header = ["依赖", "旧版本", "新版本"]
	rows = []
	for path in sorted(set(prev_mod) | set(cur_mod)):
		if path == self_path or not has_baseline:
			continue
		old = prev_mod[path][0] if path in prev_mod else "(新增)"
		new = cur_mod[path][0] if path in cur_mod else "(移除)"
		if old == new:
			continue
		if path in cur_mod:
			kind = "间接" if cur_mod[path][1] else "直接"
		else:
			kind = "间接" if prev_mod[path][1] else "直接"
		rows.append([path, old, new, kind])

	# ---------- commit 范围与 Dependabot 判定 ----------
	commits = load_json(args.commits, [])
	dependabot_commits = [c for c in commits if (c.get("author") or {}).get("login") == "dependabot[bot]"]
	prs = load_json(args.prs, [])
	dependabot_prs = [p for p in prs if (p.get("user") or {}).get("login") == "dependabot[bot]"]
	# 不能只比 startswith("Bump ")：本仓库 dependabot.yml 设了 commit-message.prefix: chore，
	# PR 标题形如 "chore(deps): bump the actions-all group with 4 updates"，
	# 因此改为大小写无关地识别 bump 关键词（仍限定作者为 dependabot[bot]）。
	bump_prs = [p for p in dependabot_prs if "bump" in str(p.get("title", "")).lower()]

	compare = ""
	range_text = f"本次推送的提交（基线 commit `{args.head_sha[:8]}`）"
	if args.prev_tag:
		range_text = f"`{args.prev_tag}...{args.new_tag}`"
	if args.repo:
		if args.prev_tag:
			compare = f"https://github.com/{args.repo}/compare/{args.prev_tag}...{args.new_tag}"
		commit_range = f"{range_text}（共 {len(commits)} 个提交）"
	else:
		commit_range = range_text

	if bump_prs:
		origin = (
			f"✅ 本次发布由 **Dependabot 触发的自动合并** 产生"
			f"（自动合并 PR：{len(bump_prs)} 个；依赖相关提交：{len(dependabot_commits)} 个）"
		)
	elif dependabot_commits:
		origin = f"⚠️ 本次发布包含 {len(dependabot_commits)} 个 Dependabot 提交，但未识别到对应 PR 标题，请人工确认"
	else:
		origin = "本次发布**非** Dependabot 触发（人工合并或手动发布）"

	# ---------- 组装 ----------
	lines = ["## 更新内容", ""]
	lines.append("### 依赖升级")
	lines.append("")
	prev_directives = parse_directives(args.prev_go_mod) if (has_baseline and args.prev_go_mod) else {}
	cur_directives = parse_directives(args.cur_go_mod)
	directive_changes = [
		f"`{key}`: {prev_directives.get(key, '(无)')} → {cur_directives[key]}"
		for key in ("go", "toolchain")
		if has_baseline and cur_directives.get(key) != prev_directives.get(key)
	]
	if directive_changes:
		lines.append("Go 版本声明变化：")
		lines.append("")
		for item in directive_changes:
			lines.append(f"- {item}")
		lines.append("")
	if rows:
		lines.append("| " + " | ".join(header) + " | 类型 |")
		lines.append("| --- | --- | --- | --- |")
		for row in rows:
			lines.append("| " + " | ".join(row) + " |")
		direct_count = len([r for r in rows if r[3] == "直接"])
		lines.append("")
		lines.append(
			f"共 {len(rows)} 项版本变化（直接依赖 {direct_count} 项，间接依赖 {len(rows) - direct_count} 项）。"
		)
	else:
		if has_baseline:
			lines.append("本次没有第三方依赖版本变化。")
		else:
			lines.append(
				f"本次为首次自动化发布，上一个 tag 不可用（仓库现有 tag 范围之外），"
				"所以下面不列依赖变化清单；若需依赖对比，请手动触发并先打基线 tag。"
			)
	lines.append("")

	lines.append("### 来源")
	lines.append("")
	lines.append(f"- {origin}")
	lines.append(f"- 提交范围：{commit_range}")
	if compare:
		lines.append(f"- 差异对比：{compare}")
	if prs:
		lines.append("- 本次范围内的 PR：")
		for pr in prs[:30]:
			mark = "（Dependabot）" if (pr.get("user") or {}).get("login") == "dependabot[bot]" else ""
			lines.append(f"  - [#{pr.get('number')}]({pr.get('html_url')}) {pr.get('title')} {mark}".rstrip())
	lines.append("")

	if args.go_version:
		lines.append("### 构建")
		lines.append("")
		lines.append(
			f"- 全部平台二进制使用 {args.go_version} 重新构建"
			"（`-trimpath`、`CGO_ENABLED=0`、`-ldflags \"-s -w -X main.VERSION={0}\"`）".format(args.new_tag)
		)
		lines.append("")

	lines.append("### 产物")
	lines.append("")
	lines.append("- `client_*` / `server_*`：各平台可执行文件（与既往 Release 资产命名一致）")
	lines.append("- `checksums.txt`：全部产物的 SHA-256 校验值")
	lines.append(f"- 资产总数：{args.asset_count}")
	lines.append("")
	lines.append("### 兼容性")
	lines.append("")
	lines.append("- 客户端与服务端配置文件格式与上一版本一致，无需修改")
	lines.append("- 建议两端使用同版本二进制（丢包测试 `losstest` 需要两端版本一致）")

	with open(args.output, "w", encoding="utf-8") as handle:
		handle.write("\n".join(lines) + "\n")

	print(f"已生成 {args.output}（依赖变化 {len(rows)} 项，提交 {len(commits)} 个）")
	return 0


if __name__ == "__main__":
	sys.exit(main())
