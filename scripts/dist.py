#!/usr/bin/env python3

# 生成 dormwatt 的发布归档.
#
# 归档内容:
#   dormwatt         桌面版, macOS 使用 cgo, Linux/Windows 使用纯 Go 托盘实现.
#   dormwatt-server  服务端静态版, 始终使用 -tags notray 与 CGO_ENABLED=0.
#   README.md
#
# 不带 --target 时构建当前平台; CI 用 --target <goos> <goarch> 指定矩阵项.
# 版本注入只在发布构建中发生, 日常 go build 仍显示 dev-build.

import argparse
import os
import shutil
import sys
import time

import release_common as rc

PROJECT_NAME = "dormwatt"
MAIN_PACKAGE = "./cmd/dormwatt"
PACKAGE_VERSION = "0.0.0"
SMOKE_ARGS = ["--version"]

DEFAULT_TARGETS = [
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
]

PLATFORM_BY_GOOS = {"darwin": "macos", "linux": "linux", "windows": "windows"}
ARCH_BY_GOARCH = {"amd64": "x86_64", "arm64": "aarch64"}


def naming_tokens(goos, goarch):
    target_platform = PLATFORM_BY_GOOS.get(goos)
    target_arch = ARCH_BY_GOARCH.get(goarch)
    if not target_platform or not target_arch:
        rc.die(f"无法识别目标平台 {goos or '?'}-{goarch or '?'}, 请检查 GOOS / GOARCH")
    return target_platform, target_arch


def host_target():
    goos = rc.try_run(["go", "env", "GOOS"]) or ""
    goarch = rc.try_run(["go", "env", "GOARCH"]) or ""
    return goos, goarch


def cgo_enabled(goos):
    """托盘版是否需要 cgo.

    macOS 的 systray 只有 cgo 实现; Linux 使用 DBus, Windows 使用原生 API,
    这两种目标的托盘版可以在 CGO_ENABLED=0 下构建.
    """
    raw = os.environ.get("CGO_ENABLED")
    if raw is not None:
        return raw.strip() == "1"
    return goos == "darwin"


def binary_name(name, goos):
    return name + (".exe" if goos == "windows" else "")


def build(goos, goarch, cgo, tags, output, display_version, commit, build_time):
    module = rc.run(["go", "list", "-m"])
    env = os.environ.copy()
    env["GOOS"] = goos
    env["GOARCH"] = goarch
    env["CGO_ENABLED"] = "1" if cgo else "0"
    ldflags = " ".join(
        [
            "-s",
            "-w",
            f"-X {module}/internal/version.Version={display_version}",
            f"-X {module}/internal/version.Commit={commit}",
            f"-X {module}/internal/version.BuildTime={build_time}",
        ]
    )
    args = [
        "go",
        "build",
        "-trimpath",
        "-buildvcs=false",
        "-ldflags",
        ldflags,
    ]
    if tags:
        args.extend(["-tags", tags])
    args.extend(["-o", output, MAIN_PACKAGE])
    rc.run(args, env=env, capture=False)


def build_one(goos, goarch, display_version, archive_version, commit, build_time, host_goos, host_goarch):
    target_platform, target_arch = naming_tokens(goos, goarch)
    is_host = (goos, goarch) == (host_goos, host_goarch)
    print(f"构建 {PROJECT_NAME} {display_version} ({target_platform}-{target_arch})")

    staging = rc.stage_dir(os.path.join("dist", "stage", f"{target_platform}-{target_arch}"))
    tray_name = binary_name(PROJECT_NAME, goos)
    server_name = binary_name(f"{PROJECT_NAME}-server", goos)
    tray_path = os.path.join(staging, tray_name)
    server_path = os.path.join(staging, server_name)

    build(goos, goarch, cgo_enabled(goos), "", tray_path, display_version, commit, build_time)
    build(goos, goarch, False, "notray", server_path, display_version, commit, build_time)

    if is_host:
        rc.smoke_check([tray_path], SMOKE_ARGS, display_version)
        rc.smoke_check([server_path], SMOKE_ARGS, display_version)
    else:
        for path in (tray_path, server_path):
            rc.check_binary_format(path, target_platform)
            rc.check_binary_arch(path, target_platform, target_arch)
            rc.check_version_string(path, display_version)

    shutil.copy2("README.md", os.path.join(staging, "README.md"))
    os.chmod(os.path.join(staging, "README.md"), 0o644)
    rc.archive(
        staging,
        [tray_name, server_name, "README.md"],
        PROJECT_NAME,
        archive_version,
        target=(target_platform, target_arch),
    )


def main():
    rc.configure_output_encoding()
    rc.repo_root()

    parser = argparse.ArgumentParser(description="生成 dormwatt 发布产物")
    parser.add_argument("--target", nargs=2, metavar=("GOOS", "GOARCH"), help="目标 GOOS 与 GOARCH")
    args = parser.parse_args()

    display_version, archive_version = rc.resolve_versions(
        rc.script_path("build-version.py"), PACKAGE_VERSION
    )
    host_goos, host_goarch = host_target()
    if args.target:
        goos, goarch = args.target
    else:
        goos, goarch = host_goos, host_goarch
    if (goos, goarch) not in DEFAULT_TARGETS:
        rc.die(f"不在支持的目标矩阵内: {goos}-{goarch}")

    commit = rc.try_run(["git", "rev-parse", "--short=7", "HEAD"]) or "unknown"
    build_time = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    build_one(goos, goarch, display_version, archive_version, commit, build_time, host_goos, host_goarch)


if __name__ == "__main__":
    main()
