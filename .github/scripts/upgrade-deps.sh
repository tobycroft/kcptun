#!/usr/bin/env bash
# No trouble

# 手动触发（workflow_dispatch）的全量依赖升级脚本，由
# .github/workflows/dependency-upgrade.yml 调用。
#
# 输入（环境变量）：
#   MODE               all | direct
#   GO_DIRECTIVE       no | tidy-default | toolchain <x.y.z>
#   UPGRADE_BRANCH     推送用的分支名
#   MAIN_BRANCH        主干分支名
#   GITHUB_REPOSITORY  owner/repo（用于 push 与 compare 链接）
#   GITHUB_TOKEN       推送用凭据
#
# 流程：升级 → go.mod/go.sum 同步校验 → 依赖清单 → 推分支 → 静态检查/编译/测试。
# 先推分支再校验，保证校验失败时 diff 仍然保留在分支上，由工作流开成
# 标记为失败的 draft PR；失败环节通过 trap 写入输出 failed_step。
set -euo pipefail

OUT="${GITHUB_OUTPUT:-/tmp/kcptun-upgrade-output}"
PHASE=init

record() {
	echo "$1=$2" >>"${OUT}"
	echo "[output] $1=$2"
}

on_error() {
	record failed_step "${PHASE}"
	record validation_ok false
	echo "::error::升级流程在环节「${PHASE}」失败"
}
trap on_error ERR

snap() {
	# 快照每个模块的版本，用于生成「包名 + 旧/新版本」清单
	go list -m -json all 2>/dev/null |
		jq -r 'select(.Path != null and .Version != null) | "\(.Path) \(.Version)"' |
		LC_ALL=C sort -u >"$1"
}

changed() {
	# 排除临时快照文件后的工作区改动
	git status --porcelain -- . ':(exclude)before.txt' ':(exclude)after.txt' ':(exclude)dep-changes.md'
}

record validation_ok false
record branch_pushed false
record failed_step none

PHASE=snapshot-before
snap before.txt
echo "== Go: $(go version) =="
echo '== 升级前 go.mod =='
cat go.mod

# ---------- 1. 升级依赖 ----------
# 注：不用 `go get -u=patch ./...`。本仓库的 go get 会把主模块自身也纳入查询，
# 而主模块无发布版本，会直接报错「can't query version "patch" of module ...」。
PHASE="go-get-${MODE:-all}"
case "${MODE:-all}" in
direct)
	echo '::group::go get -u（仅直接依赖）'
	direct_mods="$(go list -m -f '{{if and (not .Indirect) (not .Main)}}{{.Path}}{{end}}' all | LC_ALL=C sort -u)"
	if [ -z "${direct_mods}" ]; then
		echo '::error::未能从 go.mod 取得直接依赖列表'
		false
	fi
	printf '%s\n' "${direct_mods}"
	# 不加引号：需要按行/空白拆成多个模块参数
	# shellcheck disable=SC2086
	go get -u ${direct_mods}
	echo '::endgroup::'
	;;
*)
	echo '::group::go get -u ./...（直接 + 间接，均为最新非 major 版本）'
	go get -u ./...
	echo '::endgroup::'
	;;
esac

PHASE=go-mod-tidy
echo '::group::go mod tidy'
go mod tidy
echo '::endgroup::'

# ---------- 2. Go 指令行 / toolchain 处理 ----------
# go get -u 与 go mod tidy 都不会修改 go 指令行本身；
# 是否升级 Go 版本由 GO_DIRECTIVE 显式决定，默认保持不变。
PHASE=go-directive
case "${GO_DIRECTIVE:-no}" in
toolchain\ *)
	go mod edit -toolchain="${GO_DIRECTIVE#toolchain }"
	go mod tidy
	echo "已显式设置 toolchain ${GO_DIRECTIVE#toolchain }"
	;;
tidy-default)
	echo '保持 go mod tidy 的默认结果（可能引入 toolchain 行）'
	;;
*)
	# 默认不动 go 指令行；若 tidy 意外加入 toolchain 行则还原，保持 go.mod 稳定
	if grep -q '^toolchain ' go.mod; then
		go mod edit -toolchain=none
		go mod tidy
		echo '已还原 tidy 引入的 toolchain 行（go 指令行保持不变）'
	fi
	;;
esac

# 先取到变量再用 here-string 匹配：避开 pipefail + grep -q 的 SIGPIPE 误判
mod_diff="$(git --no-pager diff -- go.mod)"
if grep -qE '^[+-](go|toolchain) ' <<<"${mod_diff}"; then
	record go_directive_changed true
else
	record go_directive_changed false
fi

# ---------- 3. 校验 go.mod / go.sum 同步 ----------
PHASE=mod-sum-sync
if [ -n "$(git status --porcelain -- go.mod go.sum)" ]; then
	echo '::error::go mod tidy 之后 go.mod / go.sum 仍有未提交改动，说明两者不同步'
	git --no-pager diff -- go.mod go.sum
	false
fi
record mod_sum_sync true

# ---------- 4. 依赖变化清单 ----------
PHASE=dep-changes
snap after.txt
{
	echo '| 包名 | 旧版本 | 新版本 |'
	echo '| --- | --- | --- |'
	join -a1 -a2 -e '(无)' -o auto \
		<(grep -v '^github.com/tobycroft/kcptun ' before.txt | LC_ALL=C sort -k1,1) \
		<(grep -v '^github.com/tobycroft/kcptun ' after.txt | LC_ALL=C sort -k1,1) |
		awk '$2 != $3 { printf "|%s|%s|%s|\n", $1, $2, $3 }'
} >dep-changes.md
if [ "$(grep -c '^|' dep-changes.md)" -le 2 ]; then
	echo '没有依赖版本发生变化。' >dep-changes.md
fi
cat dep-changes.md

# ---------- 5. 提交并推送（校验失败也保留 diff） ----------
PHASE=push-branch
if [ -z "$(changed)" ]; then
	echo '依赖已是最新，go.mod / go.sum 无变化，无需开 PR'
	trap - ERR
	PHASE=done
	exit 0
fi

base_sha="$(git rev-parse HEAD)"
git checkout -B "${UPGRADE_BRANCH}"
git -c user.name='kcptun-upgrade-bot' -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
	commit -am "chore(deps): 全量升级 Go 依赖（${MODE:-all}）"
git push -f "https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" \
	"HEAD:refs/heads/${UPGRADE_BRANCH}"
record branch_pushed true
record commit_sha "$(git rev-parse HEAD)"
record compare_base "$(printf '%.8s' "${base_sha}")"
record commit_head "$(git rev-parse --short HEAD)"

# ---------- 6. 静态检查 + 编译 + 测试 ----------
PHASE=gofmt
unformatted="$(gofmt -l .)"
if [ -n "${unformatted}" ]; then
	echo "::error::以下文件未通过 gofmt：${unformatted}"
	false
fi

PHASE=go-vet
go vet ./...

PHASE=go-build
CGO_ENABLED=0 go build -trimpath -o /tmp/upgrade-client ./client
CGO_ENABLED=0 go build -trimpath -o /tmp/upgrade-server ./server

PHASE=go-test
go test -timeout 10m ./...

trap - ERR
record validation_ok true
record failed_step none
echo '依赖升级完成，全部校验通过'
