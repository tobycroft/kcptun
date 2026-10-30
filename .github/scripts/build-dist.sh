#!/usr/bin/env bash
# No trouble

# 多平台产物构建脚本，由 .github/workflows/release.yml 调用，
# 也可在本地直接执行，验证产物是否与既有 Release 完全一致：
#
#   VERSION=v20261030.1 bash .github/scripts/build-dist.sh
#   ls dist && cat dist/checksums.txt
#
# 产物矩阵与命名严格复刻现有 Release（已用 GitHub API 核对 v20261029.1 /
# v20261030 / v20261030.1 的资产清单，均为 20 个裸二进制 + checksums.txt）：
#   client/server × linux(386,amd64,arm,arm64) darwin(amd64,arm64)
#                  windows(386,amd64,arm64) freebsd(amd64)
#   命名：<binary>_<os>_<arch>[.exe]，裸二进制不带版本/日期后缀
#   checksums.txt：SHA-256，路径带 ./ 前缀（与既有一致，
#                  kcptun-script 的 get_kcptun_checksum 两种写法都能解析）
#   构建参数：CGO_ENABLED=0、-trimpath、-ldflags "-s -w -X main.VERSION=<tag>"
#                  （与 v20261030.1 实际发布所用的构建参数一致）
#
# 输入（环境变量）：
#   VERSION   注入 ldflags 的版本号，例如 v20261030.1
#   OUT_DIR   产物目录，默认 dist
#   GO        go 命令路径，默认 go
set -euo pipefail

VERSION="${VERSION:?VERSION is required, e.g. VERSION=v20261030.1}"
OUT_DIR="${OUT_DIR:-dist}"
GO="${GO:-go}"
LDFLAGS="-s -w -X main.VERSION=${VERSION}"

# 形如 <GOOS>:<GOARCH>:<GOARM>，GOARM 仅 linux/arm 使用
MATRIX=(
	linux:386:
	linux:amd64:
	linux:arm:7
	linux:arm64:
	darwin:amd64:
	darwin:arm64:
	windows:386:
	windows:amd64:
	windows:arm64:
	freebsd:amd64:
)

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

built=0
for item in "${MATRIX[@]}"; do
	GOOS="${item%%:*}"
	rest="${item#*:}"
	GOARCH="${rest%%:*}"
	GOARM="${rest#*:}"

	for bin in client server; do
		suffix=''
		if [ "${GOOS}" = windows ]; then
			suffix='.exe'
		fi
		out="${OUT_DIR}/${bin}_${GOOS}_${GOARCH}${suffix}"
		CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" GOARM="${GOARM}" \
			"${GO}" build -trimpath -ldflags "${LDFLAGS}" -o "${out}" "./${bin}"
		chmod 755 "${out}"
		built=$((built + 1))
		echo "built ${out}"
	done
done

expected=$((${#MATRIX[@]} * 2))
if [ "${built}" -ne "${expected}" ]; then
	echo "::error::产物数量不符：期望 ${expected} 个，实际 ${built} 个"
	exit 1
fi

# SHA-256 校验和：在产物目录内执行，输出格式与既有 checksums.txt 完全一致
(
	cd "${OUT_DIR}"
	find . -maxdepth 1 -type f ! -name checksums.txt -print0 | LC_ALL=C sort -z |
		xargs -0 sha256sum >checksums.txt
	chmod 644 checksums.txt
)

echo
echo "== 产物清单（${built} 个二进制 + checksums.txt）=="
ls -1 "${OUT_DIR}"

# 版本注入自检：二进制内应能直接找到 VERSION 字符串
# 注意：先落盘再 grep，避免 pipefail 下 grep -q 提前退出不回信号使误判为失败
if command -v strings >/dev/null 2>&1; then
	strings "${OUT_DIR}/server_linux_amd64" >/tmp/kcptun-version-check.txt
	if grep -qx "${VERSION}" /tmp/kcptun-version-check.txt; then
		echo "VERSION=${VERSION} 已注入"
	else
		echo "::warning::未在二进制中匹配到版本字符串 ${VERSION}，请人工确认 ldflags 是否生效"
	fi
fi
